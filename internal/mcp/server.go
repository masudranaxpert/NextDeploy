package mcp

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"panel/internal/db"
	"panel/internal/handlers"
	"panel/internal/handlers/utils"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
)

// Server represents the NextDeploy Model Context Protocol (MCP) server.
type Server struct {
	p         *handlers.Panel
	handler   *Handler
	sessions  map[string]chan string
	sessionMu sync.RWMutex
}

// NewServer initializes an MCP server instance.
func NewServer(p *handlers.Panel) *Server {
	return &Server{
		p:        p,
		handler:  NewHandler(p),
		sessions: make(map[string]chan string),
	}
}

// RegisterRoutes registers the MCP server endpoints onto the Fiber router.
func (s *Server) RegisterRoutes(app *fiber.App) {
	mcpGroup := app.Group("/mcp")

	mcpGroup.Get("/sse", s.AuthMiddleware, s.HandleSSE)
	mcpGroup.Post("/messages", s.AuthMiddleware, s.HandleMessages)
	mcpGroup.Post("/", s.AuthMiddleware, s.HandleDirectJSONRPC)
	mcpGroup.Get("/", s.AuthMiddleware, s.HandleGetInfoOrSSE)
}

// IsEnabled reports whether the NextDeploy MCP server is enabled in system settings (default: false/disabled).
func (s *Server) IsEnabled(ctx context.Context) bool {
	return s.p.DB.GetSetting(ctx, "mcp_enabled") == "1"
}

// AuthMiddleware extracts and validates Bearer token or X-API-Key for MCP endpoints.
func (s *Server) AuthMiddleware(c *fiber.Ctx) error {
	path := c.Path()
	if path != "/mcp" && !strings.HasPrefix(path, "/mcp/") {
		return c.Next()
	}

	var token string
	authHeader := c.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		token = strings.TrimSpace(authHeader[7:])
	}
	if token == "" {
		token = strings.TrimSpace(c.Get("X-API-Key"))
	}

	if token == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"jsonrpc": "2.0",
			"error": fiber.Map{
				"code":    ErrCodeInvalidRequest,
				"message": "Unauthorized: API token required via Authorization Bearer header or X-API-Key",
			},
		})
	}

	ctx := c.UserContext()

	// Validate API Token against database
	user, apiToken, err := s.p.DB.ValidateAPIToken(ctx, token)
	if err != nil || user.ID == 0 {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"jsonrpc": "2.0",
			"error": fiber.Map{
				"code":    ErrCodeInvalidRequest,
				"message": "Unauthorized: invalid or expired API token",
			},
		})
	}

	// Policy check: CLI requests vs external AI MCP requests (tokens are unified and shared)
	isCLI := c.Get("X-NextDeploy-Client") == "cli" || strings.HasPrefix(c.Get("User-Agent"), "nd/") || apiToken.Kind == "cli"
	if isCLI {
		if s.p.DB.GetSetting(ctx, "cli_enabled") == "0" {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"jsonrpc": "2.0",
				"error": fiber.Map{
					"code":    ErrCodeInternal,
					"message": "NextDeploy CLI access is currently disabled in system settings.",
				},
			})
		}
	} else {
		// External MCP clients (Cursor, Claude, Antigravity, etc.)
		if !s.IsEnabled(ctx) {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"jsonrpc": "2.0",
				"error": fiber.Map{
					"code":    ErrCodeInternal,
					"message": "NextDeploy MCP server is currently disabled. Enable MCP in the NextDeploy Panel under MCP Settings (/mcp-docs).",
				},
			})
		}
	}

	c.Locals("auth_user", user)
	c.Locals("api_token", apiToken)
	c.SetUserContext(context.WithValue(c.UserContext(), apiTokenContextKey{}, apiToken))
	return c.Next()
}

// ProcessRPC handles a single JSON-RPC 2.0 message.
func (s *Server) ProcessRPC(ctx context.Context, u db.User, req JSONRPCRequest) JSONRPCResponse {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
	}

	switch req.Method {
	case "initialize":
		resp.Result = InitializeResult{
			ProtocolVersion: "2024-11-05",
			Capabilities: ServerCapabilities{
				Tools: &ToolsCapability{ListChanged: false},
			},
			ServerInfo: Implementation{
				Name:    "nextdeploy-mcp",
				Version: "1.0.0",
			},
		}
		return resp

	case "notifications/initialized":
		resp.Result = map[string]interface{}{}
		return resp

	case "ping":
		resp.Result = map[string]interface{}{}
		return resp

	case "tools/list":
		tok, _ := ctx.Value(apiTokenContextKey{}).(db.APIToken)
		all := AllTools()
		filtered := all[:0:len(all)]
		for _, t := range all {
			if t.Name == "env_reveal" && !tok.AllowEnvReveal {
				continue
			}
			if t.Name == "server_exec" && !tok.AllowServerExec {
				continue
			}
			if t.Name == "container_exec" && !tok.AllowContainerExec {
				continue
			}
			filtered = append(filtered, t)
		}
		resp.Result = ToolsListResult{Tools: filtered}
		return resp

	case "tools/call":
		var params CallToolParams
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				resp.Error = &RPCError{
					Code:    ErrCodeInvalidParams,
					Message: fmt.Sprintf("Invalid tools/call params: %v", err),
				}
				return resp
			}
		}
		toolRes, err := s.handler.CallTool(ctx, u, params)
		if err != nil {
			resp.Error = &RPCError{
				Code:    ErrCodeInternal,
				Message: err.Error(),
			}
			return resp
		}
		resp.Result = toolRes
		return resp

	default:
		resp.Error = &RPCError{
			Code:    ErrCodeMethodNotFound,
			Message: fmt.Sprintf("Method %q not supported", req.Method),
		}
		return resp
	}
}

// HandleDirectJSONRPC processes JSON-RPC requests directly over HTTP POST.
func (s *Server) HandleDirectJSONRPC(c *fiber.Ctx) error {
	u, ok := c.Locals("auth_user").(db.User)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}

	var req JSONRPCRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return c.JSON(JSONRPCResponse{
			JSONRPC: "2.0",
			Error: &RPCError{
				Code:    ErrCodeParseError,
				Message: fmt.Sprintf("Parse error: %v", err),
			},
		})
	}

	resp := s.ProcessRPC(c.UserContext(), u, req)
	return c.JSON(resp)
}

// HandleGetInfoOrSSE handles GET /mcp (SSE stream if requested, otherwise metadata).
func (s *Server) HandleGetInfoOrSSE(c *fiber.Ctx) error {
	accept := c.Get("Accept")
	if strings.Contains(accept, "text/event-stream") {
		return s.HandleSSE(c)
	}

	return c.JSON(fiber.Map{
		"server":      "NextDeploy MCP Server",
		"version":     "1.0.0",
		"protocol":    "2024-11-05",
		"sse":         "/mcp/sse",
		"messages":    "/mcp/messages",
		"tools_count": len(AllTools()),
	})
}

// HandleSSE manages Server-Sent Events connections.
func (s *Server) HandleSSE(c *fiber.Ctx) error {
	sessionID := generateSessionID()
	msgChan := make(chan string, 32)

	s.sessionMu.Lock()
	s.sessions[sessionID] = msgChan
	s.sessionMu.Unlock()

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("Transfer-Encoding", "chunked")

	c.Context().SetBodyStreamWriter(fasthttp.StreamWriter(func(w *bufio.Writer) {
		defer func() {
			s.sessionMu.Lock()
			delete(s.sessions, sessionID)
			close(msgChan)
			s.sessionMu.Unlock()
		}()

		// Send initial endpoint event as required by the MCP SSE specification
		endpointURI := fmt.Sprintf("/mcp/messages?sessionId=%s", sessionID)
		_, _ = fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", endpointURI)
		_ = w.Flush()

		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case msg, ok := <-msgChan:
				if !ok {
					return
				}
				_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
				if err := w.Flush(); err != nil {
					return
				}
			case <-ticker.C:
				// Heartbeat comment to keep connection alive
				_, _ = fmt.Fprintf(w, ": keepalive\n\n")
				if err := w.Flush(); err != nil {
					return
				}
			}
		}
	}))

	return nil
}

// HandleMessages receives JSON-RPC requests for an active SSE session.
func (s *Server) HandleMessages(c *fiber.Ctx) error {
	u, ok := c.Locals("auth_user").(db.User)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}

	sessionID := c.Query("sessionId")
	var req JSONRPCRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(JSONRPCResponse{
			JSONRPC: "2.0",
			Error: &RPCError{
				Code:    ErrCodeParseError,
				Message: fmt.Sprintf("Parse error: %v", err),
			},
		})
	}

	resp := s.ProcessRPC(c.UserContext(), u, req)

	// If linked to an active SSE session, send event through SSE channel
	if sessionID != "" {
		s.sessionMu.RLock()
		ch, exists := s.sessions[sessionID]
		s.sessionMu.RUnlock()

		if exists && ch != nil {
			b, _ := json.Marshal(resp)
			select {
			case ch <- string(b):
				return c.SendStatus(fiber.StatusAccepted)
			default:
				log.Printf("MCP SSE session %s queue full", sessionID)
			}
		}
	}

	// Always fallback to returning HTTP response directly
	return c.JSON(resp)
}

func generateSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("session_%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// MCPDocsPage renders the dedicated MCP documentation and setup page.
func (s *Server) MCPDocsPage(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := c.Locals("auth_user").(db.User)
	if !ok || u.ID == 0 {
		return c.Redirect("/login?next=/mcp-docs")
	}

	tokens, err := s.p.DB.ListAPITokensForUser(ctx, u.ID)
	if err != nil {
		log.Printf("error listing API tokens: %v", err)
	}

	protocol := "2024-11-05"
	scheme := "https"
	if c.Protocol() == "http" && strings.HasPrefix(c.Hostname(), "localhost") {
		scheme = "http"
	}
	baseURL := fmt.Sprintf("%s://%s", scheme, c.Hostname())

	return c.Render("pages/mcp_docs", handlers.WithUser(c, fiber.Map{
		"Nav":          "mcp",
		"Title":        "MCP Server & AI Integration",
		"Host":         c.Hostname(),
		"BaseURL":      baseURL,
		"Protocol":     protocol,
		"MCPEnabled":   s.IsEnabled(ctx),
		"Tokens":       tokens,
		"Tools":        AllTools(),
		"NewToken":     c.Query("new_token"),
		"NewTokenName": c.Query("token_name"),
		"Flash":        utils.ReadFlash(c),
	}), "layouts/shell")
}

// CreateAPITokenPost handles generating a new API token for the current user.
func (s *Server) CreateAPITokenPost(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := c.Locals("auth_user").(db.User)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}

	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		name = "AI Assistant Token"
	}

	allowEnvReveal := c.FormValue("allow_env_reveal") == "1" || c.FormValue("allow_env_reveal") == "on"
	allowServerExec := c.FormValue("allow_server_exec") == "1" || c.FormValue("allow_server_exec") == "on"
	allowContainerExec := c.FormValue("allow_container_exec") == "1" || c.FormValue("allow_container_exec") == "on"
	kind := strings.TrimSpace(c.FormValue("kind"))
	if kind != "cli" && kind != "mcp" {
		kind = "mcp"
	}
	rawToken, _, err := s.p.DB.CreateAPIToken(ctx, u.ID, name, kind, nil, allowEnvReveal, allowServerExec, allowContainerExec)
	if err != nil {
		utils.SetFlash(c, "Failed to create API token: "+err.Error())
		return c.Redirect("/mcp-docs")
	}

	auditMsg := "Created API token for MCP/API"
	if allowEnvReveal {
		auditMsg += " (env_reveal secrets allowed)"
	}
	if allowContainerExec {
		auditMsg += " (container_exec allowed)"
	}
	if allowServerExec {
		auditMsg += " (server_exec allowed)"
	}
	s.p.RecordAuditLog(c, "create_api_token", "api_token", name, auditMsg)
	return c.Redirect(fmt.Sprintf("/mcp-docs?new_token=%s&token_name=%s", rawToken, name))
}

// ToggleAPITokenEnvRevealPost toggles the env_reveal permission for an existing API token.
func (s *Server) ToggleAPITokenEnvRevealPost(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := c.Locals("auth_user").(db.User)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}

	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("invalid token id")
	}

	enabled, err := s.p.DB.ToggleAPITokenEnvReveal(ctx, int64(id), u.ID)
	if err != nil {
		utils.SetFlash(c, "Failed to update token permission: "+err.Error())
		return c.Redirect("/mcp-docs")
	}

	stateStr := "disabled"
	if enabled {
		stateStr = "enabled"
	}
	s.p.RecordAuditLog(c, "toggle_api_token_reveal", "api_token", fmt.Sprintf("%d", id), "Toggled env_reveal to "+stateStr)
	utils.SetFlash(c, fmt.Sprintf("Token env_reveal permission %s.", stateStr))
	return c.Redirect("/mcp-docs")
}

// ToggleAPITokenContainerExecPost toggles the container_exec permission for an existing API token.
func (s *Server) ToggleAPITokenContainerExecPost(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := c.Locals("auth_user").(db.User)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}

	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("invalid token id")
	}

	enabled, err := s.p.DB.ToggleAPITokenContainerExec(ctx, int64(id), u.ID)
	if err != nil {
		utils.SetFlash(c, "Failed to update token permission: "+err.Error())
		return c.Redirect("/mcp-docs")
	}

	stateStr := "disabled"
	if enabled {
		stateStr = "enabled"
	}
	s.p.RecordAuditLog(c, "toggle_api_token_container_exec", "api_token", fmt.Sprintf("%d", id), "Toggled container_exec to "+stateStr)
	utils.SetFlash(c, fmt.Sprintf("Token container_exec permission %s.", stateStr))
	return c.Redirect("/mcp-docs")
}

// ToggleAPITokenServerExecPost toggles the server_exec permission for an existing API token.
func (s *Server) ToggleAPITokenServerExecPost(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := c.Locals("auth_user").(db.User)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}

	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("invalid token id")
	}

	enabled, err := s.p.DB.ToggleAPITokenServerExec(ctx, int64(id), u.ID)
	if err != nil {
		utils.SetFlash(c, "Failed to update token permission: "+err.Error())
		return c.Redirect("/mcp-docs")
	}

	stateStr := "disabled"
	if enabled {
		stateStr = "enabled"
	}
	s.p.RecordAuditLog(c, "toggle_api_token_server_exec", "api_token", fmt.Sprintf("%d", id), "Toggled server_exec to "+stateStr)
	utils.SetFlash(c, fmt.Sprintf("Token server_exec permission %s.", stateStr))
	return c.Redirect("/mcp-docs")
}

// DeleteAPITokenPost removes a token owned by the current user.
func (s *Server) DeleteAPITokenPost(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := c.Locals("auth_user").(db.User)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}

	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("invalid token id")
	}

	if err := s.p.DB.DeleteAPIToken(ctx, int64(id), u.ID); err != nil {
		utils.SetFlash(c, "Failed to delete token: "+err.Error())
		return c.Redirect("/mcp-docs")
	}

	s.p.RecordAuditLog(c, "delete_api_token", "api_token", fmt.Sprintf("%d", id), "Revoked API token")
	utils.SetFlash(c, "API Token revoked successfully.")
	return c.Redirect("/mcp-docs")
}

// ToggleMCPStatusPost handles enabling or disabling the MCP server globally.
func (s *Server) ToggleMCPStatusPost(c *fiber.Ctx) error {
	ctx := c.UserContext()
	u, ok := c.Locals("auth_user").(db.User)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).SendString("Unauthorized")
	}

	if u.Role != db.RoleAdmin {
		utils.SetFlash(c, "Permission denied: Only administrators can toggle global MCP server availability.")
		return c.Redirect("/mcp-docs")
	}

	cur := s.p.DB.GetSetting(ctx, "mcp_enabled")
	newVal := "1"
	statusText := "enabled"
	if cur == "1" {
		newVal = "0"
		statusText = "disabled"
	}

	if err := s.p.DB.SetSetting(ctx, "mcp_enabled", newVal); err != nil {
		utils.SetFlash(c, "Failed to update MCP server status: "+err.Error())
		return c.Redirect("/mcp-docs")
	}

	s.p.RecordAuditLog(c, "toggle_mcp_status", "settings", "mcp_enabled", "MCP server "+statusText)
	utils.SetFlash(c, fmt.Sprintf("MCP Server successfully %s.", statusText))
	return c.Redirect("/mcp-docs")
}
