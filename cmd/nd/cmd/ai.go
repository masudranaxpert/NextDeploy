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

// RunAI implements `nd ai [status|enable|disable]`: the optional AI assistant, the same as
// Settings → AI assistant in the panel.
func RunAI(cl *client.Client, args []string) error {
	if hasHelpFlag(args) {
		printAIHelp()
		return nil
	}
	jsonOut, clean := extractJSONFlag(args)
	sub := "status"
	if len(clean) > 0 && !strings.HasPrefix(clean[0], "-") {
		sub, clean = strings.ToLower(clean[0]), clean[1:]
	}
	if sub == "help" {
		printAIHelp()
		return nil
	}
	if err := requireAdminRole(cl, "nd ai"); err != nil {
		return err
	}
	switch sub {
	case "status", "show":
		if len(clean) > 0 {
			return fmt.Errorf("unexpected argument %q", clean[0])
		}
		return runAIStatus(cl, jsonOut)
	case "enable", "install", "on":
		version, wait, err := parseAIEnableFlags(clean)
		if err != nil {
			return err
		}
		return runAIEnable(cl, version, wait, jsonOut)
	case "disable", "uninstall", "off":
		purge, yes, err := parseAIDisableFlags(clean)
		if err != nil {
			return err
		}
		return runAIDisable(cl, purge, yes, jsonOut)
	default:
		return fmt.Errorf("unknown ai command %q. Run 'nd ai --help' for usage", sub)
	}
}

func printAIHelp() {
	fmt.Println("Usage: nd ai [status|enable|disable] [--json]")
	fmt.Println("\nThe AI assistant is optional. Enabling it downloads the omp engine (about 250 MB)")
	fmt.Println("into the panel's data directory; disabling it removes the engine again.")
	fmt.Println("\n  status                      Whether AI is on, the engine version and disk use")
	fmt.Println("  enable [--version vX.Y.Z]   Turn AI on and download the engine (default: the version")
	fmt.Println("                              this panel is tested with); --no-wait returns at once")
	fmt.Println("  disable [--yes]             Turn AI off and delete the engine and the tools it downloaded.")
	fmt.Println("                              Chats, logins and API keys are kept")
	fmt.Println("  disable --purge --yes       Also delete chats, logins and stored API keys")
}

func parseAIEnableFlags(args []string) (version string, wait bool, err error) {
	wait = true
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--version" && i+1 < len(args):
			version = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(a, "--version="):
			version = strings.TrimSpace(strings.TrimPrefix(a, "--version="))
		case a == "--no-wait":
			wait = false
		default:
			return "", false, fmt.Errorf("unknown argument: %s", a)
		}
	}
	return version, wait, nil
}

func parseAIDisableFlags(args []string) (purge, yes bool, err error) {
	for _, a := range args {
		switch a {
		case "--purge":
			purge = true
		case "--yes", "-y":
			yes = true
		default:
			return false, false, fmt.Errorf("unknown argument: %s", a)
		}
	}
	return purge, yes, nil
}

type aiPluginStatus struct {
	Enabled    bool   `json:"enabled"`
	Installed  bool   `json:"installed"`
	Installing bool   `json:"installing"`
	Wanted     bool   `json:"wanted"`
	Version    string `json:"version"`
	Target     string `json:"target"`
	Source     string `json:"source"`
	DiskMB     int64  `json:"disk_mb"`
	Arch       string `json:"arch"`
	Error      string `json:"error"`
	Progress   int    `json:"progress"`
}

func aiCall(cl *client.Client, method, path string, body any) (aiPluginStatus, []byte, error) {
	var resp struct {
		Plugin aiPluginStatus `json:"plugin"`
	}
	data, status, err := cl.Do(method, path, body)
	if err != nil {
		return resp.Plugin, nil, err
	}
	if status == 404 {
		return resp.Plugin, nil, errors.New("this panel version has no optional AI plugin; update the panel first")
	}
	if status >= 400 {
		return resp.Plugin, nil, fmt.Errorf("error: %s", client.JSONError(data))
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return resp.Plugin, nil, fmt.Errorf("parse error: %w", err)
	}
	return resp.Plugin, data, nil
}

func printAIStatus(st aiPluginStatus) {
	switch {
	case st.Installing:
		fmt.Printf("AI assistant: installing omp %s (%d%%)\n", st.Target, st.Progress)
	case st.Enabled:
		ver := st.Version
		if ver == "" {
			ver = st.Target
		}
		if st.Source == "system" {
			ver += " (bundled)"
		}
		fmt.Printf("AI assistant: %s\n", ui.Bold("on"))
		fmt.Printf("Engine:       omp %s\n", ver)
		fmt.Printf("Disk used:    %d MB\n", st.DiskMB)
	case st.Wanted:
		fmt.Println("AI assistant: on, but the engine is missing. Run 'nd ai enable' to download it.")
	default:
		fmt.Println("AI assistant: off (not installed). Run 'nd ai enable' to install it.")
	}
	if st.Error != "" && !st.Installing {
		fmt.Printf("Last error:   %s\n", st.Error)
	}
}

func runAIStatus(cl *client.Client, jsonOut bool) error {
	st, data, err := aiCall(cl, "GET", "/api/v1/ai/plugin", nil)
	if err != nil {
		return err
	}
	if jsonOut {
		fmt.Println(strings.TrimSpace(string(data)))
		return nil
	}
	printAIStatus(st)
	return nil
}

func runAIEnable(cl *client.Client, version string, wait, jsonOut bool) error {
	st, data, err := aiCall(cl, "POST", "/api/v1/ai/plugin/enable", map[string]string{"version": version})
	if err != nil {
		return err
	}
	if !wait || !st.Installing {
		if jsonOut {
			fmt.Println(strings.TrimSpace(string(data)))
		} else if st.Installing {
			fmt.Printf("Downloading omp %s in the background. Check with 'nd ai status'.\n", st.Target)
		} else {
			ui.Success("AI assistant is on")
		}
		return nil
	}
	if !jsonOut {
		fmt.Printf("Downloading omp %s...\n", st.Target)
	}
	last := -1
	deadline := time.Now().Add(25 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		cur, raw, err := aiCall(cl, "GET", "/api/v1/ai/plugin", nil)
		if err != nil {
			continue
		}
		if cur.Installing {
			if !jsonOut && cur.Progress/10 != last/10 {
				fmt.Printf("  %d%%\n", cur.Progress)
				last = cur.Progress
			}
			continue
		}
		if jsonOut {
			fmt.Println(strings.TrimSpace(string(raw)))
		}
		if cur.Enabled {
			if !jsonOut {
				ui.Success("AI assistant is on (omp %s)", cur.Version)
			}
			return nil
		}
		if cur.Error != "" {
			return fmt.Errorf("install failed: %s", cur.Error)
		}
		return errors.New("install finished but the AI assistant is not on; check 'nd ai status'")
	}
	return errors.New("download did not finish within 25 minutes; check 'nd ai status'")
}

func runAIDisable(cl *client.Client, purge, yes, jsonOut bool) error {
	if !yes {
		what := "the engine and the tools it downloaded (chats, logins and API keys are kept)"
		if purge {
			what = "the engine, the tools it downloaded, every chat, login and stored API key"
		}
		return fmt.Errorf("this deletes %s; re-run with --yes", what)
	}
	_, data, err := aiCall(cl, "POST", "/api/v1/ai/plugin/disable", map[string]bool{"purge": purge})
	if err != nil {
		return err
	}
	if jsonOut {
		fmt.Println(strings.TrimSpace(string(data)))
		return nil
	}
	if purge {
		ui.Success("AI assistant turned off and all of its data deleted")
	} else {
		ui.Success("AI assistant turned off; engine removed")
	}
	return nil
}
