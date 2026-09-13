package mcp

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"panel/internal/db"
	"panel/internal/handlers"

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
	// MCP group with dedicated token authentication
	mcpGroup := app.Group("/mcp", s.AuthMiddleware)

	mcpGroup.Get("/sse", s.HandleSSE)
	mcpGroup.Post("/messages", s.HandleMessages)
	mcpGroup.Post("/", s.HandleDirectJSONRPC)
	mcpGroup.Get("/", s.HandleGetInfoOrSSE)
}

// AuthMiddleware extracts and validates Bearer token, X-API-Key, or query token.
func (s *Server) AuthMiddleware(c *fiber.Ctx) error {
	var token string
	authHeader := c.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		token = strings.TrimSpace(authHeader[7:])
	}
	if token == "" {
		token = strings.TrimSpace(c.Get("X-API-Key"))
	}
	if token == "" {
		token = strings.TrimSpace(c.Query("token"))
	}
	if token == "" {
		token = c.Cookies("nd_session")
	}

	if token == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"jsonrpc": "2.0",
			"error": fiber.Map{
				"code":    ErrCodeInvalidRequest,
				"message": "Unauthorized: API token or session required via Authorization Bearer header or ?token query",
			},
		})
	}

	ctx := c.UserContext()

	// 1. Try validating as an API Token
	user, err := s.p.DB.ValidateAPIToken(ctx, token)
	if err == nil && user.ID > 0 {
		c.Locals("auth_user", user)
		return c.Next()
	}

	// 2. Try validating as a Session Token
	userID, expiresAt, serr := s.p.DB.GetSession(ctx, token)
	if serr == nil && time.Now().Before(expiresAt) {
		if u, uerr := s.p.DB.GetUserByID(ctx, userID); uerr == nil {
			c.Locals("auth_user", u)
			return c.Next()
		}
	}

	// 3. Fallback: Check MCP_API_TOKEN environment variable
	envToken := os.Getenv("MCP_API_TOKEN")
	if envToken != "" && token == envToken {
		users, lerr := s.p.DB.ListUsers(ctx)
		if lerr == nil {
			for _, u := range users {
				if u.Role == db.RoleAdmin {
					c.Locals("auth_user", u)
					return c.Next()
				}
			}
			if len(users) > 0 {
				c.Locals("auth_user", users[0])
				return c.Next()
			}
		}
	}

	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
		"jsonrpc": "2.0",
		"error": fiber.Map{
			"code":    ErrCodeInvalidRequest,
			"message": "Unauthorized: invalid or expired token",
		},
	})
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
		resp.Result = ToolsListResult{
			Tools: AllTools(),
		}
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
