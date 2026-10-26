package cmd

import (
	"encoding/json"
	"fmt"
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
	fmt.Println("  list, ls                                     List connected Edge Shield nodes")
	fmt.Println("  test <node_id>                               Test connectivity to an Edge Shield node")
	fmt.Println("  setup-script                                 Print the setup command for edge VPS")
	fmt.Println("  reassign <node_id>                           Reassign domains to a specific shield node")
}

func runShieldList(cl *client.Client, args []string) error {
	data, status, err := cl.Do("GET", "/api/v1/shield-nodes", nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("error: %s", client.JSONError(data))
	}

	var resp struct {
		OK    bool `json:"ok"`
		Nodes []struct {
			ID              int64  `json:"id"`
			Name            string `json:"name"`
			IP              string `json:"ip"`
			Status          string `json:"status"`
			AssignedDomains int    `json:"assigned_domains"`
		} `json:"nodes"`
		OriginIP string `json:"origin_ip"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("parse error: %w", err)
	}

	if len(resp.Nodes) == 0 {
		fmt.Println("No Edge Shield nodes configured.")
		return nil
	}

	fmt.Printf("%-6s %-20s %-16s %-12s %-10s\n", "ID", "NAME", "IP ADDRESS", "STATUS", "DOMAINS")
	fmt.Println(strings.Repeat("─", 68))
	for _, n := range resp.Nodes {
		fmt.Printf("%-6d %-20s %-16s %-12s %-10d\n", n.ID, n.Name, n.IP, n.Status, n.AssignedDomains)
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
	ui.Success("Shield node %s is healthy and reachable", nodeID)
	return nil
}

func runShieldSetupScript(cl *client.Client, args []string) error {
	serverURL := strings.TrimRight(cl.ServerURL(), "/")
	fmt.Println("Run this one-liner on your Edge Shield VPS:")
	fmt.Printf("\n  curl -sSL %s/api/v1/edge/setup.sh | bash\n\n", serverURL)
	return nil
}

func runShieldReassign(cl *client.Client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("node ID required: nd shield reassign <source_node_id> [target_node_id]")
	}
	nodeID := args[0]
	var payload map[string]interface{}
	if len(args) > 1 {
		if targetID, err := strconv.ParseInt(args[1], 10, 64); err == nil {
			payload = map[string]interface{}{"target_shield_id": targetID}
		}
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
