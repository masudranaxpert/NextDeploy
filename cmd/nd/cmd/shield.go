package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"nd/internal/client"
	"nd/internal/ui"
)

// RunShield handles Edge Shield Node management under 'nd shield'.
func RunShield(cl *client.Client, args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		printShieldHelp()
		return nil
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "list", "ls":
		return runShieldList(cl, rest)
	case "add", "create":
		return runShieldAdd(cl, rest)
	case "rm", "delete", "remove":
		return runShieldRemove(cl, rest)
	case "update":
		return runShieldUpdate(cl, rest)
	case "rotate-token":
		return runShieldRotateToken(cl, rest)
	case "test":
		return runShieldTest(cl, rest)
	case "setup-script", "script":
		return runShieldSetupScript(cl, rest)
	case "reassign":
		return runShieldReassign(cl, rest)
	default:
		return fmt.Errorf("unknown shield command %q. Run 'nd shield --help' for usage", sub)
	}
}

func printShieldHelp() {
	fmt.Println("Usage: nd shield <command> [args...]")
	fmt.Println("\nManage Edge Shield reverse proxy nodes:")
	fmt.Println("  list, ls [--json]                                 List connected Edge Shield nodes")
	fmt.Println("  add <name> <ip>                                   Register a new Edge Shield node")
	fmt.Println("  update <node_id> [--name N] [--ip IP]             Update an Edge Shield node")
	fmt.Println("  rm <node_id>                                      Delete an Edge Shield node")
	fmt.Println("  rotate-token <node_id>                            Rotate a node's shield token (10m grace)")
	fmt.Println("  test <node_id>                                    Test connectivity to an Edge Shield node")
	fmt.Println("  setup-script [node_id|name] [flags]               Print the setup command for an edge VPS")
	fmt.Println("  reassign <source_node_id> [target_node_id]        Reassign domains to another shield node (or direct if omitted)")
}

type shieldNodeItem struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	IP              string `json:"ip"`
	Token           string `json:"token,omitempty"`
	Status          string `json:"status"`
	SyncState       string `json:"sync_state,omitempty"`
	EdgeVersion     string `json:"edge_version,omitempty"`
	CaddyVersion    string `json:"caddy_version,omitempty"`
	ConflictsCount  int    `json:"conflicts_count,omitempty"`
	AssignedDomains int    `json:"assigned_domains"`
}

func fetchShieldNodes(cl *client.Client) ([]shieldNodeItem, string, error) {
	data, status, err := cl.Do("GET", "/api/v1/shield-nodes", nil)
	if err != nil {
		return nil, "", err
	}
	if status >= 400 {
		return nil, "", fmt.Errorf("error: %s", client.JSONError(data))
	}

	var resp struct {
		OK       bool             `json:"ok"`
		Nodes    []shieldNodeItem `json:"nodes"`
		OriginIP string           `json:"origin_ip"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, "", fmt.Errorf("parse error: %w", err)
	}
	return resp.Nodes, resp.OriginIP, nil
}

func runShieldList(cl *client.Client, args []string) error {
	jsonOut, _ := extractJSONFlag(args)
	nodes, originIP, err := fetchShieldNodes(cl)
	if err != nil {
		return err
	}

	if jsonOut {
		b, _ := json.MarshalIndent(map[string]interface{}{
			"ok":        true,
			"nodes":     nodes,
			"origin_ip": originIP,
		}, "", "  ")
		fmt.Println(string(b))
		return nil
	}

	if len(nodes) == 0 {
		fmt.Println("No Edge Shield nodes configured.")
		return nil
	}

	fmt.Printf("%-6s %-20s %-16s %-12s %-12s %-8s %-9s\n", "ID", "NAME", "IP ADDRESS", "STATUS", "SYNC", "DOMAINS", "CONFLICTS")
	fmt.Println(strings.Repeat("─", 88))
	for _, n := range nodes {
		syncStr := n.SyncState
		if syncStr == "" {
			syncStr = "-"
		}
		fmt.Printf("%-6d %-20s %-16s %-12s %-12s %-8d %-9d\n", n.ID, n.Name, n.IP, n.Status, syncStr, n.AssignedDomains, n.ConflictsCount)
	}
	return nil
}

func runShieldAdd(cl *client.Client, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: nd shield add <name> <ip>")
	}
	name := strings.TrimSpace(args[0])
	ip := strings.TrimSpace(args[1])
	if name == "" || ip == "" {
		return fmt.Errorf("name and IP are required: nd shield add <name> <ip>")
	}

	data, status, err := cl.Do("POST", "/api/v1/shield-nodes", map[string]interface{}{
		"name": name,
		"ip":   ip,
	})
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("failed to add shield node: %s", client.JSONError(data))
	}

	var resp struct {
		OK           bool           `json:"ok"`
		Node         shieldNodeItem `json:"node"`
		SetupCommand string         `json:"setup_command"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("invalid response from server: %w", err)
	}

	ui.Success("Added Edge Shield node %q (%s) [ID: %d]", resp.Node.Name, resp.Node.IP, resp.Node.ID)
	if resp.SetupCommand != "" {
		fmt.Printf("\nRun this on your Edge Shield VPS:\n  %s\n\n", resp.SetupCommand)
	}
	return nil
}

func runShieldRemove(cl *client.Client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("node ID required: nd shield rm <node_id>")
	}
	nodeID := strings.TrimSpace(args[0])
	data, status, err := cl.Do("DELETE", "/api/v1/shield-nodes/"+nodeID, nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("failed to remove shield node: %s", client.JSONError(data))
	}
	ui.Success("Deleted Edge Shield node #%s", nodeID)
	return nil
}

func runShieldUpdate(cl *client.Client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: nd shield update <node_id> [--name <name>] [--ip <ip>]")
	}
	nodeID := strings.TrimSpace(args[0])
	var name, ip string
	for i := 1; i < len(args); i++ {
		a := args[i]
		if (a == "--name" || a == "-n") && i+1 < len(args) {
			name = strings.TrimSpace(args[i+1])
			i++
		} else if strings.HasPrefix(a, "--name=") {
			name = strings.TrimSpace(strings.TrimPrefix(a, "--name="))
		} else if a == "--ip" && i+1 < len(args) {
			ip = strings.TrimSpace(args[i+1])
			i++
		} else if strings.HasPrefix(a, "--ip=") {
			ip = strings.TrimSpace(strings.TrimPrefix(a, "--ip="))
		} else {
			return fmt.Errorf("unknown flag: %s", a)
		}
	}
	if name == "" && ip == "" {
		return fmt.Errorf("at least one of --name or --ip is required")
	}

	// Preserve unspecified field from existing node record.
	if name == "" || ip == "" {
		nodes, _, err := fetchShieldNodes(cl)
		if err != nil {
			return err
		}
		for _, n := range nodes {
			if strconv.FormatInt(n.ID, 10) == nodeID {
				if name == "" {
					name = n.Name
				}
				if ip == "" {
					ip = n.IP
				}
				break
			}
		}
	}

	data, status, err := cl.Do("PUT", "/api/v1/shield-nodes/"+nodeID, map[string]interface{}{
		"name": name,
		"ip":   ip,
	})
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("failed to update shield node: %s", client.JSONError(data))
	}
	ui.Success("Updated Edge Shield node #%s (%s -> %s)", nodeID, name, ip)
	return nil
}

func runShieldRotateToken(cl *client.Client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("node ID required: nd shield rotate-token <node_id>")
	}
	nodeID := strings.TrimSpace(args[0])
	data, status, err := cl.Do("POST", "/api/v1/shield-nodes/"+nodeID+"/rotate-token", nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("failed to rotate shield token: %s", client.JSONError(data))
	}

	var resp struct {
		OK                 bool   `json:"ok"`
		Token              string `json:"token"`
		TokenPrevExpiresAt string `json:"token_prev_expires_at"`
		SetupCommand       string `json:"setup_command"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("invalid response from server: %w", err)
	}

	ui.Success("Rotated token for Edge Shield node #%s (previous token remains valid for 10m)", nodeID)
	if resp.Token != "" {
		fmt.Printf("  New token: %s\n", resp.Token)
	}
	if resp.TokenPrevExpiresAt != "" {
		fmt.Printf("  Grace expires at: %s\n", resp.TokenPrevExpiresAt)
	}
	if resp.SetupCommand != "" {
		fmt.Printf("\nUpdate existing Shield VPS with:\n  nd edge set-token <panel_id> --token-stdin\nOr re-run setup:\n  %s\n\n", resp.SetupCommand)
	}
	return nil
}

func runShieldTest(cl *client.Client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("node ID required: nd shield test <node_id>")
	}
	nodeID := args[0]
	fmt.Printf("→ Testing connectivity to shield node %s...\n", nodeID)
	data, status, err := cl.Do("POST", "/api/v1/shield-nodes/"+nodeID+"/test", nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("test failed: %s", client.JSONError(data))
	}

	var resp struct {
		OK        bool   `json:"ok"`
		Reachable bool   `json:"reachable"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("invalid response from server: %w", err)
	}

	if !resp.OK || !resp.Reachable {
		errMsg := strings.TrimSpace(resp.Error)
		if errMsg == "" {
			errMsg = "node is unreachable or down"
		}
		return fmt.Errorf("shield node %s test failed: %s", nodeID, errMsg)
	}

	ui.Success("Shield node %s is healthy and reachable", nodeID)
	return nil
}

func runShieldSetupScript(cl *client.Client, args []string) error {
	var (
		target       string
		adopt        bool
		acmeEmail    string
		insecureHTTP bool
		force        bool
	)

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--adopt-existing":
			adopt = true
		case a == "--insecure-http":
			insecureHTTP = true
		case a == "--force":
			force = true
		case a == "--acme-email" && i+1 < len(args):
			acmeEmail = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(a, "--acme-email="):
			acmeEmail = strings.TrimSpace(strings.TrimPrefix(a, "--acme-email="))
		case (a == "--node" || a == "--id") && i+1 < len(args):
			target = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(a, "--node=") || strings.HasPrefix(a, "--id="):
			target = strings.TrimSpace(a[strings.Index(a, "=")+1:])
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag: %s", a)
		default:
			if target == "" {
				target = strings.TrimSpace(a)
			}
		}
	}

	nodes, _, err := fetchShieldNodes(cl)
	if err != nil {
		return err
	}
	if len(nodes) == 0 {
		return fmt.Errorf("no Edge Shield nodes configured; add one first with: nd shield add <name> <ip>")
	}

	var selected *shieldNodeItem
	if target == "" {
		if len(nodes) == 1 {
			selected = &nodes[0]
		} else {
			return fmt.Errorf("multiple shield nodes exist (%d); specify one: nd shield setup-script <node_id|name>", len(nodes))
		}
	} else {
		for i := range nodes {
			if strconv.FormatInt(nodes[i].ID, 10) == target || strings.EqualFold(nodes[i].Name, target) {
				selected = &nodes[i]
				break
			}
		}
		if selected == nil {
			return fmt.Errorf("shield node %q not found", target)
		}
	}

	if strings.TrimSpace(selected.Token) == "" {
		return fmt.Errorf("shield node %q has no visible token (admin permission required)", selected.Name)
	}

	serverURL := strings.TrimRight(cl.ServerURL(), "/")
	q := url.Values{}
	q.Set("token", selected.Token)
	if adopt {
		q.Set("adopt", "1")
	}
	if acmeEmail != "" {
		q.Set("acme_email", acmeEmail)
	}
	if insecureHTTP {
		q.Set("insecure_http", "1")
	}
	if force {
		q.Set("force", "1")
	}

	fmt.Printf("Run this one-liner on your Edge Shield VPS (%s / %s):\n", selected.Name, selected.IP)
	fmt.Printf("\n  curl -sSL \"%s/api/v1/edge/setup.sh?%s\" | bash\n\n", serverURL, q.Encode())
	return nil
}

func runShieldReassign(cl *client.Client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("node ID required: nd shield reassign <source_node_id> [target_node_id]")
	}
	nodeID := args[0]
	var payload map[string]interface{}
	if len(args) > 1 {
		targetID, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil || targetID <= 0 {
			return fmt.Errorf("invalid target_node_id %q: must be a positive integer", args[1])
		}
		payload = map[string]interface{}{"target_shield_id": targetID}
	}
	data, status, err := cl.Do("POST", "/api/v1/shield-nodes/"+nodeID+"/reassign", payload)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("reassign failed: %s", client.JSONError(data))
	}
	ui.Success("Reassigned domains for shield node %s", nodeID)
	return nil
}
