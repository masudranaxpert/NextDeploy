package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"panel/internal/agent/omprpc"
	"panel/internal/dockerapi"
)

var containerRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

func objSchema(props string, required ...string) map[string]json.RawMessage {
	out := map[string]json.RawMessage{
		"type":       json.RawMessage(`"object"`),
		"properties": json.RawMessage(props),
	}
	if len(required) > 0 {
		b, _ := json.Marshal(required)
		out["required"] = b
	}
	return out
}

func jsonResult(v any) omprpc.HostToolResultPayload {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return omprpc.TextResult(fmt.Sprintf("encode error: %v", err))
	}
	if string(b) == "null" {
		b = []byte("[]")
	}
	return omprpc.TextResult(truncateOutput(string(b)))
}

// extraHostTools expose the rest of the panel to the agent: Docker, deploy
// history, backups, databases (metadata only) and the audit trail.
func (m *Manager) extraHostTools() []omprpc.HostTool {
	return []omprpc.HostTool{
		{
			Definition: omprpc.HostToolDefinition{
				Name:        "list_containers",
				Description: "Lists all Docker containers on the host with state, image, ports and compose project/service.",
				Parameters:  objSchema(`{}`),
			},
			Handler: func(ctx context.Context, _ *omprpc.HostToolCall, _ json.RawMessage) (omprpc.HostToolResultPayload, error) {
				rows, errMsg := dockerapi.ListContainers(ctx)
				if errMsg != "" && len(rows) == 0 {
					return omprpc.TextResult("Docker error: " + errMsg), nil
				}
				return jsonResult(rows), nil
			},
		},
		{
			Definition: omprpc.HostToolDefinition{
				Name:        "container_logs",
				Description: "Returns the last lines of a container's stdout/stderr (deploy, proxy, database or app issues).",
				Parameters: objSchema(`{
					"container": {"type": "string", "description": "Container name or ID"},
					"lines": {"type": "number", "description": "Tail length (default 200, max 2000)"}
				}`, "container"),
			},
			Handler: func(ctx context.Context, _ *omprpc.HostToolCall, args json.RawMessage) (omprpc.HostToolResultPayload, error) {
				var p struct {
					Container string `json:"container"`
					Lines     int    `json:"lines"`
				}
				if err := json.Unmarshal(args, &p); err != nil || !containerRefPattern.MatchString(p.Container) {
					return omprpc.TextResult("Invalid container reference"), nil
				}
				if p.Lines <= 0 {
					p.Lines = 200
				}
				if p.Lines > 2000 {
					p.Lines = 2000
				}
				out, err := dockerapi.FetchContainerLogsText(ctx, p.Container, p.Lines)
				if err != nil {
					return omprpc.TextResult(fmt.Sprintf("Could not read logs: %v", err)), nil
				}
				return omprpc.TextResult(truncateOutput(out)), nil
			},
		},
		{
			Definition: omprpc.HostToolDefinition{
				Name:        "container_action",
				Description: "Starts, stops or restarts a Docker container by name.",
				Parameters: objSchema(`{
					"container": {"type": "string", "description": "Container name"},
					"action": {"type": "string", "enum": ["start", "stop", "restart"]}
				}`, "container", "action"),
			},
			Handler: func(ctx context.Context, _ *omprpc.HostToolCall, args json.RawMessage) (omprpc.HostToolResultPayload, error) {
				var p struct {
					Container string `json:"container"`
					Action    string `json:"action"`
				}
				if err := json.Unmarshal(args, &p); err != nil || !containerRefPattern.MatchString(p.Container) {
					return omprpc.TextResult("Invalid container reference"), nil
				}
				if !m.requestApproval(ctx, "container_action", "Container "+p.Action, fmt.Sprintf("%s container %s?", strings.Title(p.Action), p.Container)) { //nolint:staticcheck
					return omprpc.TextResult("Action declined by the user."), nil
				}
				var err error
				switch p.Action {
				case "start":
					err = dockerapi.StartContainerByName(ctx, p.Container)
				case "stop":
					err = dockerapi.StopContainerByName(ctx, p.Container)
				case "restart":
					err = dockerapi.RestartContainerByName(ctx, p.Container)
				default:
					return omprpc.TextResult("action must be start, stop or restart"), nil
				}
				if err != nil {
					return omprpc.TextResult(fmt.Sprintf("Failed: %v", err)), nil
				}
				dockerapi.InvalidateContainerCache()
				return omprpc.TextResult(fmt.Sprintf("Container %s: %s done.", p.Container, p.Action)), nil
			},
		},
		{
			Definition: omprpc.HostToolDefinition{
				Name:        "list_deploy_logs",
				Description: "Recent deploy/build history of an app: action, success, commit and output tail. Use it to find why a deploy failed.",
				Parameters: objSchema(`{
					"app_id": {"type": "string"},
					"limit": {"type": "number", "description": "default 5, max 20"}
				}`, "app_id"),
			},
			Handler: func(ctx context.Context, _ *omprpc.HostToolCall, args json.RawMessage) (omprpc.HostToolResultPayload, error) {
				var p struct {
					AppID string `json:"app_id"`
					Limit int    `json:"limit"`
				}
				if err := json.Unmarshal(args, &p); err != nil || !ValidAppID(p.AppID) {
					return omprpc.TextResult("Invalid app_id"), nil
				}
				if p.Limit <= 0 || p.Limit > 20 {
					p.Limit = 5
				}
				logs, err := m.DB.ListDeployLogs(ctx, p.AppID, p.Limit)
				if err != nil {
					return omprpc.TextResult(fmt.Sprintf("Failed: %v", err)), nil
				}
				type row struct {
					ID        int64  `json:"id"`
					Action    string `json:"action"`
					OK        bool   `json:"ok"`
					Slot      string `json:"slot,omitempty"`
					Commit    string `json:"commit,omitempty"`
					CreatedAt string `json:"created_at"`
					Output    string `json:"output_tail"`
				}
				out := make([]row, 0, len(logs))
				for _, l := range logs { // the env snapshot is deliberately left out: it holds secrets
					tail := l.Output
					if len(tail) > 3000 {
						tail = tail[len(tail)-3000:]
					}
					out = append(out, row{l.ID, l.Action, l.OK, l.Slot, l.CommitSHA, l.CreatedAt.Format("2006-01-02 15:04:05"), tail})
				}
				return jsonResult(out), nil
			},
		},
		{
			Definition: omprpc.HostToolDefinition{
				Name:        "list_databases",
				Description: "Databases and DB users of a PHP/WordPress site (names only, never passwords). To run SQL use execute_bash with wp-cli or mysql inside the site.",
				Parameters:  objSchema(`{"app_id": {"type": "string"}}`, "app_id"),
			},
			Handler: func(ctx context.Context, _ *omprpc.HostToolCall, args json.RawMessage) (omprpc.HostToolResultPayload, error) {
				var p struct {
					AppID string `json:"app_id"`
				}
				if err := json.Unmarshal(args, &p); err != nil || !ValidAppID(p.AppID) {
					return omprpc.TextResult("Invalid app_id"), nil
				}
				dbs, err := m.DB.ListPHPDatabases(ctx, p.AppID)
				if err != nil {
					return omprpc.TextResult(fmt.Sprintf("Failed: %v", err)), nil
				}
				return jsonResult(dbs), nil
			},
		},
		{
			Definition: omprpc.HostToolDefinition{
				Name:        "list_backups",
				Description: "Recent backups of an app with type, status, size and any error.",
				Parameters: objSchema(`{
					"app_id": {"type": "string"},
					"limit": {"type": "number", "description": "default 10, max 50"}
				}`, "app_id"),
			},
			Handler: func(ctx context.Context, _ *omprpc.HostToolCall, args json.RawMessage) (omprpc.HostToolResultPayload, error) {
				var p struct {
					AppID string `json:"app_id"`
					Limit int    `json:"limit"`
				}
				if err := json.Unmarshal(args, &p); err != nil || !ValidAppID(p.AppID) {
					return omprpc.TextResult("Invalid app_id"), nil
				}
				if p.Limit <= 0 || p.Limit > 50 {
					p.Limit = 10
				}
				rows, err := m.DB.ListBackupHistory(ctx, p.AppID, p.Limit)
				if err != nil {
					return omprpc.TextResult(fmt.Sprintf("Failed: %v", err)), nil
				}
				type row struct {
					ID        int64  `json:"id"`
					Type      string `json:"type"`
					Volume    string `json:"volume,omitempty"`
					Status    string `json:"status"`
					Error     string `json:"error,omitempty"`
					SizeBytes int64  `json:"size_bytes"`
					CreatedAt string `json:"created_at"`
				}
				out := make([]row, 0, len(rows))
				for _, b := range rows {
					out = append(out, row{b.ID, b.BackupType, b.VolumeName, b.Status, b.ErrorMessage, b.SizeBytes, b.CreatedAt})
				}
				return jsonResult(out), nil
			},
		},
		{
			Definition: omprpc.HostToolDefinition{
				Name:        "get_audit_log",
				Description: "Recent panel audit trail (who did what, from which IP). Use it to spot suspicious logins or changes.",
				Parameters:  objSchema(`{"limit": {"type": "number", "description": "default 50, max 200"}}`),
			},
			Handler: func(ctx context.Context, _ *omprpc.HostToolCall, args json.RawMessage) (omprpc.HostToolResultPayload, error) {
				var p struct {
					Limit int `json:"limit"`
				}
				_ = json.Unmarshal(args, &p)
				if p.Limit <= 0 || p.Limit > 200 {
					p.Limit = 50
				}
				rows, err := m.DB.ListAuditLogs(ctx, p.Limit, 0)
				if err != nil {
					return omprpc.TextResult(fmt.Sprintf("Failed: %v", err)), nil
				}
				return jsonResult(rows), nil
			},
		},
	}
}
