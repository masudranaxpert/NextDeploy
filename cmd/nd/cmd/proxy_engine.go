package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"nd/internal/client"
	"nd/internal/ui"
)

// runProxyEngine implements `nd proxy engine [status|preflight <engine>|set <engine>]`: the
// panel's reverse proxy engine (Caddy, Angie, Ferron), the same as the Proxy page in the UI.
func runProxyEngine(cl *client.Client, args []string) error {
	if hasHelpFlag(args) {
		printProxyEngineHelp()
		return nil
	}
	jsonOut, clean := extractJSONFlag(args)
	sub := "status"
	if len(clean) > 0 && !strings.HasPrefix(clean[0], "-") {
		sub, clean = strings.ToLower(clean[0]), clean[1:]
	}
	switch sub {
	case "status", "show", "get":
		return runProxyEngineStatus(cl, jsonOut)
	case "preflight", "check":
		target, _, err := parseEngineTarget(clean)
		if err != nil {
			return err
		}
		return runProxyEnginePreflight(cl, target, jsonOut)
	case "set", "switch":
		target, flags, err := parseEngineTarget(clean)
		if err != nil {
			return err
		}
		return runProxyEngineSet(cl, target, flags["--yes"] || flags["-y"], flags["--experimental"], jsonOut)
	case "-h", "--help", "help":
		printProxyEngineHelp()
		return nil
	default:
		return fmt.Errorf("unknown proxy engine command %q. Run 'nd proxy engine --help' for usage", sub)
	}
}

func printProxyEngineHelp() {
	fmt.Println("Usage: nd proxy engine [status|preflight <engine>|set <engine> [--yes] [--experimental]] [--json]")
	fmt.Println("\nShow or switch the panel's reverse proxy engine (caddy, angie, ferron):")
	fmt.Println("  status                      Active engine, version, running state and last switch")
	fmt.Println("  preflight <engine>          What would break on <engine>: blocks, warnings, config validation")
	fmt.Println("  set <engine> --yes          Switch (with automatic rollback); ferron also needs --experimental")
}

func parseEngineTarget(args []string) (string, map[string]bool, error) {
	flags := map[string]bool{}
	target := ""
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			switch a {
			case "--yes", "-y", "--experimental":
				flags[a] = true
			default:
				return "", nil, fmt.Errorf("unknown flag: %s", a)
			}
			continue
		}
		if target != "" {
			return "", nil, fmt.Errorf("unexpected argument %q", a)
		}
		target = strings.ToLower(strings.TrimSpace(a))
	}
	switch target {
	case "caddy", "angie", "ferron":
		return target, flags, nil
	case "":
		return "", nil, errors.New("engine name required: caddy, angie or ferron")
	default:
		return "", nil, fmt.Errorf("unknown engine %q (expected caddy, angie or ferron)", target)
	}
}

type proxyEngineStatusResp struct {
	Engine     string `json:"engine"`
	Version    string `json:"version"`
	Running    bool   `json:"running"`
	LastSwitch *struct {
		From    string `json:"from"`
		To      string `json:"to"`
		Status  string `json:"status"`
		Message string `json:"message"`
	} `json:"last_switch"`
}

func fetchProxyEngineStatus(cl *client.Client) (proxyEngineStatusResp, []byte, error) {
	var resp proxyEngineStatusResp
	data, status, err := cl.Do("GET", "/api/v1/proxy/engine", nil)
	if err != nil {
		return resp, nil, err
	}
	if status >= 400 {
		return resp, nil, fmt.Errorf("error: %s", client.JSONError(data))
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return resp, nil, fmt.Errorf("parse error: %w", err)
	}
	return resp, data, nil
}

func runProxyEngineStatus(cl *client.Client, jsonOut bool) error {
	resp, data, err := fetchProxyEngineStatus(cl)
	if err != nil {
		return err
	}
	if jsonOut {
		fmt.Println(strings.TrimSpace(string(data)))
		return nil
	}
	state := "stopped"
	if resp.Running {
		state = "running"
	}
	fmt.Printf("Engine:  %s %s (%s)\n", ui.Bold(resp.Engine), resp.Version, state)
	if ls := resp.LastSwitch; ls != nil && ls.To != "" {
		fmt.Printf("Last switch: %s -> %s: %s", ls.From, ls.To, ls.Status)
		if ls.Message != "" {
			fmt.Printf(" (%s)", ls.Message)
		}
		fmt.Println()
	}
	return nil
}

type proxyIssue struct {
	Capability    string `json:"capability"`
	Message       string `json:"message"`
	AffectedSites []struct {
		Label  string `json:"label"`
		Detail string `json:"detail"`
	} `json:"affected_sites"`
}

func runProxyEnginePreflight(cl *client.Client, target string, jsonOut bool) error {
	data, status, err := cl.Do("POST", "/api/v1/proxy/preflight", map[string]string{"target": target})
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("error: %s", client.JSONError(data))
	}
	if jsonOut {
		fmt.Println(strings.TrimSpace(string(data)))
		return nil
	}
	var resp struct {
		CanSwitch    bool         `json:"can_switch"`
		Experimental bool         `json:"requires_experimental_ack"`
		HardBlocks   []proxyIssue `json:"hard_blocks"`
		SoftWarnings []proxyIssue `json:"soft_warnings"`
		Checks       []struct {
			Name   string `json:"name"`
			OK     bool   `json:"ok"`
			Detail string `json:"detail"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("parse error: %w", err)
	}
	printIssues := func(title string, list []proxyIssue) {
		if len(list) == 0 {
			return
		}
		fmt.Println(title)
		for _, iss := range list {
			fmt.Printf("  - %s\n", iss.Message)
			for _, s := range iss.AffectedSites {
				if s.Detail != "" {
					fmt.Printf("      %s: %s\n", s.Label, s.Detail)
				} else {
					fmt.Printf("      %s\n", s.Label)
				}
			}
		}
	}
	printIssues("Blocks (switch not possible):", resp.HardBlocks)
	printIssues("Warnings:", resp.SoftWarnings)
	for _, c := range resp.Checks {
		mark := "PASS"
		if !c.OK {
			mark = "FAIL"
		}
		fmt.Printf("[%s] %s %s\n", mark, c.Name, c.Detail)
	}
	if resp.CanSwitch {
		hint := "nd proxy engine set " + target + " --yes"
		if resp.Experimental {
			hint += " --experimental"
		}
		ui.Success("%s can be used. Switch with: %s", target, hint)
		return nil
	}
	return fmt.Errorf("switch to %s is not possible; fix the blocks above first", target)
}

func runProxyEngineSet(cl *client.Client, target string, yes, experimental, jsonOut bool) error {
	if !yes {
		return fmt.Errorf("switching the proxy interrupts traffic for a moment; re-run with --yes (run 'nd proxy engine preflight %s' first)", target)
	}
	body := map[string]any{"target": target, "yes": true, "experimental_ack": experimental}
	data, status, err := cl.Do("POST", "/api/v1/proxy/switch", body)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("error: %s", client.JSONError(data))
	}
	if !jsonOut {
		fmt.Printf("Switching to %s (runs in the background on the panel)...\n", target)
	}
	// The switch runs asynchronously on the panel; follow it until it finishes.
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(3 * time.Second)
		st, raw, err := fetchProxyEngineStatus(cl)
		if err != nil {
			continue // the panel may be unreachable for a moment while its proxy moves
		}
		ls := st.LastSwitch
		if ls == nil || ls.To != target || ls.Status == "running" || ls.Status == "" {
			continue
		}
		if jsonOut {
			fmt.Println(strings.TrimSpace(string(raw)))
		}
		if ls.Status == "ok" {
			if !jsonOut {
				ui.Success("Proxy engine is now %s (%s)", target, ls.Message)
			}
			return nil
		}
		return fmt.Errorf("switch to %s failed (%s): %s", target, ls.Status, ls.Message)
	}
	return fmt.Errorf("switch to %s did not finish within 10 minutes; check 'nd proxy engine status'", target)
}
