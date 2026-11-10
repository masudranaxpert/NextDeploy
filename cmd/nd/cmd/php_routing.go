package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"nd/internal/client"
	"nd/internal/config"
	"nd/internal/ui"
)

type phpRoutingRule struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	MatchType     string `json:"match_type"`
	Pattern       string `json:"pattern"`
	Target        string `json:"target,omitempty"`
	Status        int    `json:"status,omitempty"`
	Enabled       bool   `json:"enabled"`
	UnlessFile    bool   `json:"unless_exists,omitempty"`
	PreserveQuery bool   `json:"preserve_query,omitempty"`
	Note          string `json:"note,omitempty"`
}

type phpSiteRouting struct {
	FallbackMode  string           `json:"fallback_mode,omitempty"`
	FallbackEntry string           `json:"fallback_entry,omitempty"`
	Rules         []phpRoutingRule `json:"rules,omitempty"`
}

func resolveRoutingAppID(args []string, isRuleArg func(string) bool) (string, []string, error) {
	// If the first non-flag argument matches a subcommand value (e.g. "strict_404" or "redir"),
	// check linked project first before treating it as app_id.
	if len(args) > 0 && isRuleArg != nil && isRuleArg(args[0]) {
		if pc, err := config.LoadProject("."); err == nil && pc.AppID != "" {
			return pc.AppID, args, nil
		}
	}
	return resolveAppID(args)
}

func runPHPPower(cl *client.Client, args []string, action string) error {
	appID, _, err := resolveAppID(args)
	if err != nil {
		return err
	}
	data, status, err := cl.Do("POST", "/api/v1/php/sites/"+appID+"/"+action, nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("%s failed: %s", action, client.JSONError(data))
	}
	if action == "stop" {
		ui.Success("Stopped site %s (Caddy route removed)", appID)
	} else {
		ui.Success("Started site %s (Caddy route live)", appID)
	}
	return nil
}

func runPHPRules(cl *client.Client, args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Println("Usage: nd php rules <subcommand> [args...]")
		fmt.Println("\nManage per-site Caddy URL routing rules and Apache .htaccess conversion:")
		fmt.Println("  list [app_id]                                   List fallback mode and routing rules")
		fmt.Println("  scan [app_id] [--apply] [--mode replace|append] Scan htdocs/ for .htaccess files")
		fmt.Println("  fallback [app_id] <default|strict_404|custom> [entry]  Set fallback behavior")
		fmt.Println("  add [app_id] <rewrite|redir|block> <exact|prefix|regex> <pattern> [target] [--status N] [--qsa] [--unless-exists] [--note ...]")
		fmt.Println("  import [app_id] <file.json|file.htaccess|-> [--mode replace|append] [--htaccess] [--dry-run] Batch apply rules")
		fmt.Println("  export [app_id] [-o rules.json]                 Export current routing rules as JSON")
		fmt.Println("  rm [app_id] <rule_id_or_index>                  Remove a routing rule")
		return nil
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "list", "ls":
		appID, _, err := resolveAppID(rest)
		if err != nil {
			return err
		}
		routing, err := fetchPHPRouting(cl, appID)
		if err != nil {
			return err
		}
		mode := routing.FallbackMode
		if mode == "" {
			mode = "default"
		}
		fmt.Printf("Fallback Mode: %s", ui.Bold(mode))
		if routing.FallbackEntry != "" {
			fmt.Printf(" (%s)", routing.FallbackEntry)
		}
		fmt.Println()
		if len(routing.Rules) == 0 {
			fmt.Println("No custom routing rules configured.")
			return nil
		}
		fmt.Printf("%-4s %-12s %-8s %-8s %-28s %-24s %-7s\n", "#", "ID", "KIND", "MATCH", "PATTERN", "TARGET/CODE", "ENABLED")
		fmt.Println(strings.Repeat("─", 96))
		for i, r := range routing.Rules {
			dest := r.Target
			if r.Kind == "block" {
				dest = fmt.Sprintf("HTTP %d", r.Status)
			} else if r.Kind == "redir" && r.Status > 0 {
				dest = fmt.Sprintf("%s (%d)", r.Target, r.Status)
			}
			en := "yes"
			if !r.Enabled {
				en = "no"
			}
			fmt.Printf("%-4d %-12s %-8s %-8s %-28s %-24s %-7s\n", i+1, r.ID, r.Kind, r.MatchType, r.Pattern, dest, en)
		}
		return nil

	case "scan":
		appID, rem, err := resolveAppID(rest)
		if err != nil {
			return err
		}
		apply := false
		mode := "replace"
		for i := 0; i < len(rem); i++ {
			a := rem[i]
			if a == "--apply" {
				apply = true
			} else if a == "--mode" && i+1 < len(rem) {
				mode = rem[i+1]
				i++
			} else if strings.HasPrefix(a, "--mode=") {
				mode = strings.TrimPrefix(a, "--mode=")
			}
		}
		data, status, err := cl.Do("POST", "/api/v1/php/sites/"+appID+"/routing/scan-htaccess", map[string]any{
			"apply": apply,
			"mode":  mode,
		})
		if err != nil {
			return err
		}
		if status >= 400 {
			return fmt.Errorf("scan failed: %s", client.JSONError(data))
		}
		var resp struct {
			Result struct {
				FilesScanned  int              `json:"files_scanned"`
				Files         []string         `json:"files"`
				Rules         []phpRoutingRule `json:"rules"`
				FallbackMode  string           `json:"fallback_mode"`
				FallbackEntry string           `json:"fallback_entry"`
				Diagnostics   []struct {
					File      string `json:"file"`
					Line      int    `json:"line"`
					Severity  string `json:"severity"`
					Directive string `json:"directive"`
					Message   string `json:"message"`
				} `json:"diagnostics"`
			} `json:"result"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}
		r := resp.Result
		fmt.Printf("Scanned %d .htaccess file(s): %s\n", r.FilesScanned, strings.Join(r.Files, ", "))
		fmt.Printf("Converted %d rule(s)", len(r.Rules))
		if r.FallbackMode != "" {
			fmt.Printf(" | suggested fallback: %s (%s)", r.FallbackMode, r.FallbackEntry)
		}
		fmt.Println()
		for _, d := range r.Diagnostics {
			if d.Severity == "warning" {
				fmt.Printf("  [WARNING] %s:%d (%s) %s\n", d.File, d.Line, d.Directive, d.Message)
			}
		}
		if apply {
			ui.Success("Applied %d .htaccess rule(s) to %s", len(r.Rules), appID)
		} else if len(r.Rules) > 0 {
			fmt.Println("Run with --apply to save and sync Caddy routes.")
		}
		return nil

	case "fallback":
		appID, rem, err := resolveRoutingAppID(rest, func(s string) bool {
			return s == "default" || s == "strict_404" || s == "custom"
		})
		if err != nil || len(rem) == 0 {
			return fmt.Errorf("usage: nd php rules fallback [app_id] <default|strict_404|custom> [entry]")
		}
		routing, err := fetchPHPRouting(cl, appID)
		if err != nil {
			return err
		}
		routing.FallbackMode = rem[0]
		if len(rem) > 1 {
			routing.FallbackEntry = rem[1]
		} else if rem[0] != "custom" {
			routing.FallbackEntry = ""
		}
		if err := savePHPRouting(cl, appID, routing); err != nil {
			return err
		}
		ui.Success("Updated fallback mode for %s to %s", appID, rem[0])
		return nil

	case "add":
		appID, rem, err := resolveRoutingAppID(rest, func(s string) bool {
			return s == "rewrite" || s == "redir" || s == "block"
		})
		if err != nil || len(rem) < 3 {
			return fmt.Errorf("usage: nd php rules add [app_id] <rewrite|redir|block> <exact|prefix|regex> <pattern> [target] [--status N] [--qsa] [--unless-exists] [--note ...]")
		}
		rule := phpRoutingRule{
			Kind:      rem[0],
			MatchType: rem[1],
			Pattern:   rem[2],
			Enabled:   true,
		}
		var pos []string
		for i := 3; i < len(rem); i++ {
			a := rem[i]
			switch {
			case a == "--status" && i+1 < len(rem):
				if n, err := strconv.Atoi(rem[i+1]); err == nil {
					rule.Status = n
				}
				i++
			case strings.HasPrefix(a, "--status="):
				if n, err := strconv.Atoi(strings.TrimPrefix(a, "--status=")); err == nil {
					rule.Status = n
				}
			case a == "--qsa":
				rule.PreserveQuery = true
			case a == "--unless-exists":
				rule.UnlessFile = true
			case a == "--note" && i+1 < len(rem):
				rule.Note = rem[i+1]
				i++
			case strings.HasPrefix(a, "--note="):
				rule.Note = strings.TrimPrefix(a, "--note=")
			default:
				pos = append(pos, a)
			}
		}
		if len(pos) > 0 {
			rule.Target = pos[0]
		}
		routing, err := fetchPHPRouting(cl, appID)
		if err != nil {
			return err
		}
		rule.ID = fmt.Sprintf("r-%d", len(routing.Rules)+1)
		routing.Rules = append(routing.Rules, rule)
		if err := savePHPRouting(cl, appID, routing); err != nil {
			return err
		}
		ui.Success("Added %s rule (%s %s) to %s", rule.Kind, rule.MatchType, rule.Pattern, appID)
		return nil

	case "rm", "delete":
		appID, rem, err := resolveAppID(rest)
		if err != nil || len(rem) == 0 {
			return fmt.Errorf("usage: nd php rules rm [app_id] <rule_id_or_index>")
		}
		target := rem[0]
		routing, err := fetchPHPRouting(cl, appID)
		if err != nil {
			return err
		}
		idxVal, idxErr := strconv.Atoi(target)
		var kept []phpRoutingRule
		removed := false
		for i, r := range routing.Rules {
			if r.ID == target || (idxErr == nil && idxVal == i+1) {
				removed = true
				continue
			}
			kept = append(kept, r)
		}
		if !removed {
			return fmt.Errorf("rule %q not found", target)
		}
		routing.Rules = kept
		if err := savePHPRouting(cl, appID, routing); err != nil {
			return err
		}
		ui.Success("Removed routing rule %s from %s", target, appID)
		return nil

	case "export":
		appID, rem, err := resolveAppID(rest)
		if err != nil {
			return err
		}
		outFile := ""
		for i := 0; i < len(rem); i++ {
			if (rem[i] == "-o" || rem[i] == "--output") && i+1 < len(rem) {
				outFile = rem[i+1]
				i++
			} else if strings.HasPrefix(rem[i], "-o=") {
				outFile = strings.TrimPrefix(rem[i], "-o=")
			}
		}
		routing, err := fetchPHPRouting(cl, appID)
		if err != nil {
			return err
		}
		pretty, _ := json.MarshalIndent(routing, "", "  ")
		if outFile != "" {
			if err := os.WriteFile(outFile, append(pretty, '\n'), 0644); err != nil {
				return fmt.Errorf("could not write %s: %w", outFile, err)
			}
			ui.Success("Exported %d routing rule(s) to %s", len(routing.Rules), outFile)
			return nil
		}
		fmt.Println(string(pretty))
		return nil

	case "import", "batch":
		return runPHPRulesImport(cl, rest)
	}
	return fmt.Errorf("unknown rules command %q", sub)
}

type htaccessConvertResponse struct {
	Rules         []phpRoutingRule `json:"rules"`
	FallbackMode  string           `json:"fallback_mode"`
	FallbackEntry string           `json:"fallback_entry"`
}

func convertHtaccessViaAPI(cl *client.Client, appID, content, filePath string) (htaccessConvertResponse, error) {
	data, status, err := cl.Do("POST", "/api/v1/php/sites/"+appID+"/routing/convert-htaccess", map[string]string{
		"content":   content,
		"file_path": filePath,
	})
	if err != nil {
		return htaccessConvertResponse{}, err
	}
	if status >= 400 {
		return htaccessConvertResponse{}, fmt.Errorf("convert failed: %s", client.JSONError(data))
	}
	var resp struct {
		Result htaccessConvertResponse `json:"result"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return htaccessConvertResponse{}, fmt.Errorf("failed to parse convert response: %w", err)
	}
	return resp.Result, nil
}

func runPHPRulesImport(cl *client.Client, rest []string) error {
	appID, rem, err := resolveRoutingAppID(rest, func(s string) bool {
		if s == "-" {
			return true
		}
		if _, stErr := os.Stat(s); stErr == nil {
			return true
		}
		return strings.Contains(s, ".json") || strings.Contains(s, "htaccess")
	})
	if err != nil || len(rem) == 0 {
		return fmt.Errorf("usage: nd php rules import [app_id] <rules.json|.htaccess|-> [--mode replace|append] [--htaccess] [--dry-run]")
	}

	src := ""
	mode := "replace"
	asHtaccess := false
	dryRun := false
	for i := 0; i < len(rem); i++ {
		a := rem[i]
		switch {
		case a == "--htaccess":
			asHtaccess = true
		case a == "--dry-run":
			dryRun = true
		case a == "--mode" && i+1 < len(rem):
			mode = rem[i+1]
			i++
		case strings.HasPrefix(a, "--mode="):
			mode = strings.TrimPrefix(a, "--mode=")
		case src == "" && (a == "-" || !strings.HasPrefix(a, "-")):
			src = a
		}
	}
	if src == "" {
		return fmt.Errorf("source file or '-' required")
	}

	var rawBytes []byte
	if src == "-" {
		rawBytes, err = io.ReadAll(os.Stdin)
	} else {
		rawBytes, err = os.ReadFile(src)
	}
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", src, err)
	}

	var incoming phpSiteRouting
	trimmed := strings.TrimSpace(string(rawBytes))
	if asHtaccess || strings.Contains(strings.ToLower(src), "htaccess") || (!strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[")) {
		conv, cErr := convertHtaccessViaAPI(cl, appID, string(rawBytes), src)
		if cErr != nil {
			return cErr
		}
		incoming = phpSiteRouting{
			FallbackMode:  conv.FallbackMode,
			FallbackEntry: conv.FallbackEntry,
			Rules:         conv.Rules,
		}
	} else {
		if strings.HasPrefix(trimmed, "[") {
			if err := json.Unmarshal(rawBytes, &incoming.Rules); err != nil {
				return fmt.Errorf("invalid routing rules JSON array: %w", err)
			}
		} else if err := json.Unmarshal(rawBytes, &incoming); err != nil {
			return fmt.Errorf("invalid routing JSON: %w", err)
		}
		for i := range incoming.Rules {
			if incoming.Rules[i].ID == "" {
				incoming.Rules[i].ID = fmt.Sprintf("imp-%d", i+1)
			}
		}
	}

	if dryRun {
		pretty, _ := json.MarshalIndent(incoming, "", "  ")
		fmt.Println(string(pretty))
		return nil
	}

	next := incoming
	if strings.EqualFold(mode, "append") {
		cur, err := fetchPHPRouting(cl, appID)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		merged := append([]phpRoutingRule(nil), cur.Rules...)
		for _, r := range merged {
			seen[r.Kind+"|"+r.MatchType+"|"+r.Pattern+"|"+r.Target] = true
		}
		for _, r := range incoming.Rules {
			k := r.Kind + "|" + r.MatchType + "|" + r.Pattern + "|" + r.Target
			if !seen[k] {
				seen[k] = true
				merged = append(merged, r)
			}
		}
		next = cur
		next.Rules = merged
		if cur.FallbackMode == "" && incoming.FallbackMode != "" {
			next.FallbackMode = incoming.FallbackMode
			next.FallbackEntry = incoming.FallbackEntry
		}
	}

	if err := savePHPRouting(cl, appID, next); err != nil {
		return err
	}
	ui.Success("Batch applied %d routing rule(s) (%s) to %s", len(next.Rules), mode, appID)
	return nil
}

func fetchPHPRouting(cl *client.Client, appID string) (phpSiteRouting, error) {
	data, status, err := cl.Do("GET", "/api/v1/php/sites/"+appID+"/routing", nil)
	if err != nil {
		return phpSiteRouting{}, err
	}
	if status >= 400 {
		return phpSiteRouting{}, fmt.Errorf("failed to fetch routing: %s", client.JSONError(data))
	}
	var resp struct {
		Routing phpSiteRouting `json:"routing"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return phpSiteRouting{}, fmt.Errorf("failed to parse routing: %w", err)
	}
	return resp.Routing, nil
}

func savePHPRouting(cl *client.Client, appID string, routing phpSiteRouting) error {
	data, status, err := cl.Do("PUT", "/api/v1/php/sites/"+appID+"/routing", routing)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("failed to save routing: %s", client.JSONError(data))
	}
	return nil
}
