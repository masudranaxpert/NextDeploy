package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"nd/internal/edge"
	"nd/internal/ui"
)

func runEdgeDomain(ops *edgeOps, args []string) error {
	if len(args) == 0 || hasHelpFlag(args[:1]) {
		printEdgeCommandHelp(ops.stdout, "domain")
		return nil
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "add":
		return runEdgeDomainAdd(ops, rest)
	case "set":
		return runEdgeDomainSet(ops, rest)
	case "rm", "remove", "delete":
		return runEdgeDomainRemove(ops, rest)
	case "list", "ls":
		return runEdgeDomainList(ops, rest)
	case "show":
		return runEdgeDomainShow(ops, rest)
	case "enable":
		return runEdgeDomainSetEnabled(ops, rest, true)
	case "disable":
		return runEdgeDomainSetEnabled(ops, rest, false)
	case "check":
		return runEdgeDomainCheck(ops, rest)
	case "prune":
		return runEdgeDomainPrune(ops, rest)
	case "restore":
		return runEdgeDomainRestore(ops, rest)
	case "export":
		return runEdgeDomainExport(ops, rest)
	case "import":
		return runEdgeDomainImport(ops, rest)
	default:
		return fmt.Errorf("unknown domain subcommand %q (run 'nd edge domain --help')", sub)
	}
}

type parsedDomainFlags struct {
	aliases          []string
	aliasesSet       bool
	clearAliases     bool
	www              bool
	wwwSet           bool
	redirectWWW      string
	redirectWWWSet   bool
	tlsMode          string
	tlsSet           bool
	certFile         string
	keyFile          string
	forceHTTPS       bool
	forceHTTPSSet    bool
	originScheme     string
	upstreamsRaw     string
	upstreamsSet     bool
	lb               string
	lbSet            bool
	noVerify         bool
	noVerifySet      bool
	sni              string
	sniSet           bool
	hostHeader       string
	hostHeaderSet    bool
	websocket        bool
	websocketSet     bool
	maxBodyBytes     int64
	maxBodySet       bool
	timeoutSec       int
	timeoutSet       bool
	connectSec       int
	connectSet       bool
	realIP           string
	realIPSet        bool
	headersUp        []edge.ManualHeader
	clearHeadersUp   bool
	headersDown      []edge.ManualHeader
	clearHeadersDown bool
	keepalive        int
	keepaliveSet     bool
	note             string
	noteSet          bool
	disabled         bool
	enabledSet       bool
	dryRun           bool
	jsonOut          bool
	positionals      []string
}

func parseDomainFlags(args []string) (parsedDomainFlags, error) {
	var f parsedDomainFlags
	nextVal := func(i *int, flag string) (string, error) {
		if *i+1 >= len(args) {
			return "", fmt.Errorf("%s requires a value", flag)
		}
		*i++
		return args[*i], nil
	}

	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			f.positionals = append(f.positionals, a)
			continue
		}
		name, val, hasEq := strings.Cut(a, "=")
		readVal := func() (string, error) {
			if hasEq {
				return val, nil
			}
			return nextVal(&i, name)
		}
		parseBoolFlag := func(def bool) (bool, error) {
			if !hasEq {
				return def, nil
			}
			b, err := strconv.ParseBool(val)
			if err != nil {
				return false, fmt.Errorf("invalid boolean for %s: %q", name, val)
			}
			return b, nil
		}

		switch name {
		case "--www":
			b, err := parseBoolFlag(true)
			if err != nil {
				return f, err
			}
			f.www, f.wwwSet = b, true
		case "--no-www":
			f.www, f.wwwSet = false, true
		case "--alias":
			v, err := readVal()
			if err != nil {
				return f, err
			}
			f.aliases = append(f.aliases, strings.TrimSpace(v))
			f.aliasesSet = true
		case "--clear-aliases":
			f.clearAliases = true
		case "--redirect-www":
			v, err := readVal()
			if err != nil {
				return f, err
			}
			v = strings.ToLower(strings.TrimSpace(v))
			if v == "none" || v == "off" {
				v = ""
			}
			f.redirectWWW, f.redirectWWWSet = v, true
		case "--tls":
			v, err := readVal()
			if err != nil {
				return f, err
			}
			f.tlsMode, f.tlsSet = strings.ToLower(strings.TrimSpace(v)), true
		case "--cert":
			v, err := readVal()
			if err != nil {
				return f, err
			}
			f.certFile = strings.TrimSpace(v)
		case "--key":
			v, err := readVal()
			if err != nil {
				return f, err
			}
			f.keyFile = strings.TrimSpace(v)
		case "--force-https":
			b, err := parseBoolFlag(true)
			if err != nil {
				return f, err
			}
			f.forceHTTPS, f.forceHTTPSSet = b, true
		case "--no-force-https":
			f.forceHTTPS, f.forceHTTPSSet = false, true
		case "--origin-http":
			if f.originScheme == "https" {
				return f, errors.New("cannot specify both --origin-http and --origin-https")
			}
			f.originScheme = "http"
		case "--origin-https":
			if f.originScheme == "http" {
				return f, errors.New("cannot specify both --origin-http and --origin-https")
			}
			f.originScheme = "https"
		case "--upstreams", "--upstream":
			v, err := readVal()
			if err != nil {
				return f, err
			}
			f.upstreamsRaw, f.upstreamsSet = strings.TrimSpace(v), true
		case "--lb":
			v, err := readVal()
			if err != nil {
				return f, err
			}
			f.lb, f.lbSet = strings.ToLower(strings.TrimSpace(v)), true
		case "--no-verify":
			b, err := parseBoolFlag(true)
			if err != nil {
				return f, err
			}
			f.noVerify, f.noVerifySet = b, true
		case "--verify":
			f.noVerify, f.noVerifySet = false, true
		case "--sni":
			v, err := readVal()
			if err != nil {
				return f, err
			}
			f.sni, f.sniSet = strings.TrimSpace(v), true
		case "--host-header":
			v, err := readVal()
			if err != nil {
				return f, err
			}
			f.hostHeader, f.hostHeaderSet = strings.TrimSpace(v), true
		case "--websocket":
			b, err := parseBoolFlag(true)
			if err != nil {
				return f, err
			}
			f.websocket, f.websocketSet = b, true
		case "--no-websocket":
			f.websocket, f.websocketSet = false, true
		case "--max-body":
			v, err := readVal()
			if err != nil {
				return f, err
			}
			sz, err := edge.ParseByteSize(v)
			if err != nil {
				return f, err
			}
			f.maxBodyBytes, f.maxBodySet = sz, true
		case "--timeout":
			v, err := readVal()
			if err != nil {
				return f, err
			}
			sec, err := edge.ParsePositiveDurationSec(v)
			if err != nil {
				return f, err
			}
			f.timeoutSec, f.timeoutSet = sec, true
		case "--connect-timeout":
			v, err := readVal()
			if err != nil {
				return f, err
			}
			sec, err := edge.ParsePositiveDurationSec(v)
			if err != nil {
				return f, err
			}
			f.connectSec, f.connectSet = sec, true
		case "--real-ip":
			v, err := readVal()
			if err != nil {
				return f, err
			}
			v = strings.ToLower(strings.TrimSpace(v))
			if v == "auto" {
				v = ""
			}
			f.realIP, f.realIPSet = v, true
		case "--header-up":
			v, err := readVal()
			if err != nil {
				return f, err
			}
			hdr, err := edge.ParseHeaderPair(v)
			if err != nil {
				return f, err
			}
			f.headersUp = append(f.headersUp, hdr)
		case "--clear-headers-up":
			f.clearHeadersUp = true
		case "--header-down":
			v, err := readVal()
			if err != nil {
				return f, err
			}
			hdr, err := edge.ParseHeaderPair(v)
			if err != nil {
				return f, err
			}
			f.headersDown = append(f.headersDown, hdr)
		case "--clear-headers-down":
			f.clearHeadersDown = true
		case "--keepalive":
			v, err := readVal()
			if err != nil {
				return f, err
			}
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n < 0 {
				return f, fmt.Errorf("invalid --keepalive %q", v)
			}
			f.keepalive, f.keepaliveSet = n, true
		case "--note":
			v, err := readVal()
			if err != nil {
				return f, err
			}
			f.note, f.noteSet = v, true
		case "--disabled":
			f.disabled, f.enabledSet = true, true
		case "--enabled":
			f.disabled, f.enabledSet = false, true
		case "--dry-run":
			f.dryRun = true
		case "--json":
			f.jsonOut = true
		default:
			return f, fmt.Errorf("unknown flag: %s", a)
		}
	}
	return f, nil
}

func runEdgeDomainAdd(ops *edgeOps, args []string) error {
	if len(args) == 0 || hasHelpFlag(args) {
		printEdgeCommandHelp(ops.stdout, "domain add")
		return nil
	}
	f, err := parseDomainFlags(args)
	if err != nil {
		return err
	}
	if len(f.positionals) < 2 {
		return errors.New("usage: nd edge domain add <domain> <upstream>[,<upstream>...] [flags]")
	}
	if len(f.positionals) > 2 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(f.positionals[2:], " "))
	}

	rawDomain := f.positionals[0]
	rawUps := f.positionals[1]

	ctx := context.Background()
	lock, err := edge.AcquireLockBlocking(ctx, edgeConfigLockPath(ops.paths))
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	cfg, err := edge.LoadConfig(ops.paths.ConfigFile)
	if err != nil {
		return fmt.Errorf("load config (run 'nd edge install' first): %w", err)
	}
	manualFile, err := edge.LoadManualDomains(ops.paths.DomainsFile)
	if err != nil {
		return err
	}

	now := ops.now()
	tlsMode := edge.ManualTLSAuto
	if f.tlsSet {
		tlsMode = f.tlsMode
	}
	normDomain, err := edge.NormalizeManualDomain(rawDomain, tlsMode, cfg.ShieldHostname)
	if err != nil {
		return err
	}
	ups, err := edge.ParseManualUpstreamList(rawUps, f.originScheme)
	if err != nil {
		return err
	}

	entry := edge.ManualDomainEntry{
		ID:        edge.ManualEntryID(normDomain),
		Domain:    normDomain,
		Upstreams: ups,
		TLS:       tlsMode,
		Websocket: edge.BoolPtr(true),
		Keepalive: edge.IntPtr(edge.DefaultManualKeepalive),
		Enabled:   !f.disabled,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := applyParsedDomainFlags(&entry, f); err != nil {
		return err
	}
	if err := edge.NormalizeAndValidateEntry(&entry, cfg.ShieldHostname); err != nil {
		return err
	}

	claims := loadClaimsSnapshot(ops, cfg, now)
	if err := edge.CheckManualHostConflicts(entry, manualFile, claims, ops.paths, cfg.EngineName(), ""); err != nil {
		return err
	}

	if f.dryRun {
		return printManualDryRun(ops, cfg, []edge.ManualDomainEntry{entry}, entry.Domain, f.jsonOut, now)
	}

	cleanupCert, err := prepareManualCertIfNeeded(ops, cfg, &entry, f.certFile, f.keyFile, now)
	if err != nil {
		return err
	}

	candidate := edge.CloneManualDomainsFile(manualFile)
	candidate.Domains = append(candidate.Domains, entry)
	edge.SortManualDomains(candidate.Domains)

	reloadedViaDaemon, err := applyCandidateManualDomains(ctx, ops, cfg, candidate, now)
	if err != nil {
		if cleanupCert != nil {
			cleanupCert()
		}
		return err
	}
	if err := edge.SaveManualDomains(ops.paths.DomainsFile, candidate); err != nil {
		return err
	}
	if reloadedViaDaemon {
		signalDaemonReload(ctx, ops)
	}

	if f.jsonOut {
		b, _ := json.MarshalIndent(entry, "", "  ")
		fmt.Fprintln(ops.stdout, string(b))
		return nil
	}
	ui.Success("Added manual domain %s (%s) -> %s", entry.Domain, entry.TLS, formatUpstreamSummary(entry.Upstreams))
	return nil
}

func runEdgeDomainSet(ops *edgeOps, args []string) error {
	if len(args) == 0 || hasHelpFlag(args) {
		printEdgeCommandHelp(ops.stdout, "domain set")
		return nil
	}
	f, err := parseDomainFlags(args)
	if err != nil {
		return err
	}
	if len(f.positionals) != 1 {
		return errors.New("usage: nd edge domain set <domain> [flags]")
	}
	targetQuery := f.positionals[0]

	ctx := context.Background()
	lock, err := edge.AcquireLockBlocking(ctx, edgeConfigLockPath(ops.paths))
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	cfg, err := edge.LoadConfig(ops.paths.ConfigFile)
	if err != nil {
		return err
	}
	manualFile, err := edge.LoadManualDomains(ops.paths.DomainsFile)
	if err != nil {
		return err
	}
	idx, existing := edge.FindManualDomain(manualFile, targetQuery)
	if idx < 0 || existing == nil {
		return fmt.Errorf("manual domain %q not found", targetQuery)
	}

	now := ops.now()
	updated := *existing
	if f.upstreamsSet {
		ups, err := edge.ParseManualUpstreamList(f.upstreamsRaw, f.originScheme)
		if err != nil {
			return err
		}
		updated.Upstreams = ups
	} else if f.originScheme != "" {
		reschemed := make([]edge.ManualUpstream, len(updated.Upstreams))
		for i, u := range updated.Upstreams {
			u.Scheme = f.originScheme
			reschemed[i] = u
		}
		updated.Upstreams = reschemed
	}

	if err := applyParsedDomainFlags(&updated, f); err != nil {
		return err
	}
	if f.enabledSet {
		updated.Enabled = !f.disabled
	}
	updated.UpdatedAt = now

	if updated.TLS != edge.ManualTLSManual {
		updated.CertFile = ""
		updated.KeyFile = ""
	}
	if err := edge.NormalizeAndValidateEntry(&updated, cfg.ShieldHostname); err != nil {
		return err
	}

	claims := loadClaimsSnapshot(ops, cfg, now)
	if err := edge.CheckManualHostConflicts(updated, manualFile, claims, ops.paths, cfg.EngineName(), updated.ID); err != nil {
		return err
	}

	if f.dryRun {
		return printManualDryRun(ops, cfg, []edge.ManualDomainEntry{updated}, updated.Domain, f.jsonOut, now)
	}

	var cleanupCert func()
	if f.certFile != "" || f.keyFile != "" {
		cleanupCert, err = prepareManualCertIfNeeded(ops, cfg, &updated, f.certFile, f.keyFile, now)
		if err != nil {
			return err
		}
	}

	candidate := edge.CloneManualDomainsFile(manualFile)
	candidate.Domains[idx] = updated

	reloadedViaDaemon, err := applyCandidateManualDomains(ctx, ops, cfg, candidate, now)
	if err != nil {
		if cleanupCert != nil {
			cleanupCert()
		}
		return err
	}
	if err := edge.SaveManualDomains(ops.paths.DomainsFile, candidate); err != nil {
		return err
	}
	if reloadedViaDaemon {
		signalDaemonReload(ctx, ops)
	}

	if f.jsonOut {
		b, _ := json.MarshalIndent(updated, "", "  ")
		fmt.Fprintln(ops.stdout, string(b))
		return nil
	}
	ui.Success("Updated manual domain %s", updated.Domain)
	return nil
}

func applyParsedDomainFlags(entry *edge.ManualDomainEntry, f parsedDomainFlags) error {
	if f.wwwSet {
		entry.WWW = f.www
	}
	if f.clearAliases {
		entry.Aliases = nil
	}
	if f.aliasesSet {
		entry.Aliases = append([]string(nil), f.aliases...)
	}
	if f.redirectWWWSet {
		entry.RedirectWWW = f.redirectWWW
	}
	if f.tlsSet {
		entry.TLS = f.tlsMode
	}
	if f.certFile != "" {
		entry.CertFile = f.certFile
	}
	if f.keyFile != "" {
		entry.KeyFile = f.keyFile
	}
	if (f.certFile != "" || f.keyFile != "") && (f.certFile == "" || f.keyFile == "") {
		return errors.New("--cert and --key must be supplied together")
	}
	if f.forceHTTPSSet {
		entry.ForceHTTPS = edge.BoolPtr(f.forceHTTPS)
	}
	if f.lbSet {
		entry.LB = f.lb
	}
	if f.noVerifySet {
		entry.Origin.NoVerify = f.noVerify
	}
	if f.sniSet {
		entry.Origin.SNI = f.sni
	}
	if f.hostHeaderSet {
		entry.Origin.HostHeader = f.hostHeader
	}
	if f.websocketSet {
		entry.Websocket = edge.BoolPtr(f.websocket)
	}
	if f.maxBodySet {
		entry.MaxBodyBytes = f.maxBodyBytes
	}
	if f.timeoutSet {
		entry.TimeoutSec = f.timeoutSec
	}
	if f.connectSet {
		entry.ConnectTimeoutSec = f.connectSec
	}
	if f.realIPSet {
		entry.RealIP = f.realIP
	}
	if f.clearHeadersUp {
		entry.HeadersUp = nil
	}
	if len(f.headersUp) > 0 {
		entry.HeadersUp = append(entry.HeadersUp, f.headersUp...)
	}
	if f.clearHeadersDown {
		entry.HeadersDown = nil
	}
	if len(f.headersDown) > 0 {
		entry.HeadersDown = append(entry.HeadersDown, f.headersDown...)
	}
	if f.keepaliveSet {
		entry.Keepalive = edge.IntPtr(f.keepalive)
	}
	if f.noteSet {
		entry.Note = f.note
	}
	return nil
}

func prepareManualCertIfNeeded(ops *edgeOps, cfg *edge.Config, entry *edge.ManualDomainEntry, srcCert, srcKey string, now time.Time) (func(), error) {
	if entry.TLS != edge.ManualTLSManual || (srcCert == "" && srcKey == "") {
		return nil, nil
	}
	ep := ops.paths.EnginePathsFor(cfg.EngineName())
	certsDir := ep.ManualCertsDir
	if certsDir == "" {
		certsDir = filepath.Join(ops.paths.CaddyNDDir, "certs")
	}
	dstCert, dstKey, err := edge.ImportManualTLSCertPair(certsDir, entry.Domain, srcCert, srcKey, now)
	if err != nil {
		return nil, err
	}
	entry.CertFile = dstCert
	entry.KeyFile = dstKey
	return func() {
		_ = os.Remove(dstCert)
		_ = os.Remove(dstKey)
	}, nil
}

func runEdgeDomainRemove(ops *edgeOps, args []string) error {
	if len(args) == 0 || hasHelpFlag(args) {
		printEdgeCommandHelp(ops.stdout, "domain rm")
		return nil
	}
	var yes, jsonOut bool
	var targets []string
	for _, a := range args {
		switch a {
		case "--yes", "-y":
			yes = true
		case "--json":
			jsonOut = true
		default:
			if strings.HasPrefix(a, "-") {
				return fmt.Errorf("unknown flag: %s", a)
			}
			targets = append(targets, a)
		}
	}
	if len(targets) == 0 {
		return errors.New("usage: nd edge domain rm <domain>... [--yes]")
	}

	ok, err := confirmDestructiveAction(ops, yes, fmt.Sprintf("Remove %d manual domain(s) (%s) and move to trash?", len(targets), strings.Join(targets, ", ")))
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(ops.stdout, "Aborted.")
		return nil
	}

	ctx := context.Background()
	lock, err := edge.AcquireLockBlocking(ctx, edgeConfigLockPath(ops.paths))
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	cfg, err := edge.LoadConfig(ops.paths.ConfigFile)
	if err != nil {
		return err
	}
	manualFile, err := edge.LoadManualDomains(ops.paths.DomainsFile)
	if err != nil {
		return err
	}

	now := ops.now()
	candidate := edge.CloneManualDomainsFile(manualFile)
	trashFile, err := edge.LoadManualTrash(ops.paths.DomainsTrashFile, now)
	if err != nil {
		return err
	}

	var removed []edge.ManualDomainEntry
	for _, q := range targets {
		idx, found := edge.FindManualDomain(candidate, q)
		if idx < 0 || found == nil {
			return fmt.Errorf("manual domain %q not found", q)
		}
		removed = append(removed, *found)
		edge.MoveToTrash(trashFile, *found, "manual", now)
		candidate.Domains = append(candidate.Domains[:idx], candidate.Domains[idx+1:]...)
	}

	reloadedViaDaemon, err := applyCandidateManualDomains(ctx, ops, cfg, candidate, now)
	if err != nil {
		return err
	}
	if err := edge.SaveManualDomains(ops.paths.DomainsFile, candidate); err != nil {
		return err
	}
	_ = edge.SaveManualTrash(ops.paths.DomainsTrashFile, trashFile, now)

	cleanupAfterRemoval(ops, cfg, candidate, removed, now)
	if reloadedViaDaemon {
		signalDaemonReload(ctx, ops)
	}

	if jsonOut {
		b, _ := json.MarshalIndent(map[string]any{"removed": removed}, "", "  ")
		fmt.Fprintln(ops.stdout, string(b))
		return nil
	}
	for _, e := range removed {
		ui.Success("Removed manual domain %s (moved to trash; restore within 30d with 'nd edge domain restore %s')", e.Domain, e.Domain)
	}
	return nil
}

func runEdgeDomainSetEnabled(ops *edgeOps, args []string, enabled bool) error {
	cmdName := "domain enable"
	if !enabled {
		cmdName = "domain disable"
	}
	if len(args) == 0 || hasHelpFlag(args) {
		printEdgeCommandHelp(ops.stdout, cmdName)
		return nil
	}
	jsonOut, cleanArgs := extractJSONFlag(args)
	if len(cleanArgs) != 1 || strings.HasPrefix(cleanArgs[0], "-") {
		return fmt.Errorf("usage: nd edge %s <domain>", cmdName)
	}
	query := cleanArgs[0]

	ctx := context.Background()
	lock, err := edge.AcquireLockBlocking(ctx, edgeConfigLockPath(ops.paths))
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	cfg, err := edge.LoadConfig(ops.paths.ConfigFile)
	if err != nil {
		return err
	}
	manualFile, err := edge.LoadManualDomains(ops.paths.DomainsFile)
	if err != nil {
		return err
	}

	idx, existing := edge.FindManualDomain(manualFile, query)
	if idx < 0 || existing == nil {
		return fmt.Errorf("manual domain %q not found", query)
	}

	now := ops.now()
	candidate := edge.CloneManualDomainsFile(manualFile)
	candidate.Domains[idx].Enabled = enabled
	candidate.Domains[idx].UpdatedAt = now

	reloadedViaDaemon, err := applyCandidateManualDomains(ctx, ops, cfg, candidate, now)
	if err != nil {
		return err
	}
	if err := edge.SaveManualDomains(ops.paths.DomainsFile, candidate); err != nil {
		return err
	}
	if reloadedViaDaemon {
		signalDaemonReload(ctx, ops)
	}

	if jsonOut {
		b, _ := json.MarshalIndent(candidate.Domains[idx], "", "  ")
		fmt.Fprintln(ops.stdout, string(b))
		return nil
	}
	if enabled {
		ui.Success("Enabled manual domain %s", existing.Domain)
	} else {
		ui.Success("Disabled manual domain %s (config kept in domains.json)", existing.Domain)
	}
	return nil
}

func runEdgeDomainList(ops *edgeOps, args []string) error {
	if hasHelpFlag(args) {
		printEdgeCommandHelp(ops.stdout, "domain list")
		return nil
	}
	var jsonOut, includePanels bool
	for _, a := range args {
		switch a {
		case "--json":
			jsonOut = true
		case "--all":
			includePanels = true
		default:
			return fmt.Errorf("unknown flag: %s", a)
		}
	}

	manualFile, err := edge.LoadManualDomains(ops.paths.DomainsFile)
	if err != nil {
		return err
	}

	type listRow struct {
		Domain    string   `json:"domain"`
		Source    string   `json:"source"`
		TLS       string   `json:"tls"`
		Enabled   bool     `json:"enabled"`
		Upstreams []string `json:"upstreams"`
		Note      string   `json:"note,omitempty"`
	}

	var rows []listRow
	for _, d := range manualFile.Domains {
		ups := make([]string, len(d.Upstreams))
		for i, u := range d.Upstreams {
			ups[i] = u.URL()
		}
		rows = append(rows, listRow{
			Domain:    d.Domain,
			Source:    "manual",
			TLS:       d.TLS,
			Enabled:   d.Enabled,
			Upstreams: ups,
			Note:      d.Note,
		})
	}

	if includePanels {
		if cfg, err := edge.LoadConfig(ops.paths.ConfigFile); err == nil {
			now := ops.now()
			for _, p := range cfg.Panels {
				st, _ := edge.LoadPanelState(ops.paths.StateFile(p.PanelID), p.PanelID, p.PanelName, now)
				if st == nil {
					continue
				}
				for _, hs := range st.HostStates {
					rows = append(rows, listRow{
						Domain:    hs.Host,
						Source:    p.PanelName,
						TLS:       "auto",
						Enabled:   hs.State == edge.StateActive || hs.State == edge.StateIssuing || hs.State == edge.StateDNSDrift,
						Upstreams: []string{"panel:" + p.PanelName},
						Note:      hs.State,
					})
				}
			}
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Domain != rows[j].Domain {
			return rows[i].Domain < rows[j].Domain
		}
		return rows[i].Source < rows[j].Source
	})

	if jsonOut {
		b, _ := json.MarshalIndent(map[string]any{"domains": rows}, "", "  ")
		fmt.Fprintln(ops.stdout, string(b))
		return nil
	}
	if len(rows) == 0 {
		fmt.Fprintln(ops.stdout, ui.Dim("No manual domains configured. Add one with: nd edge domain add <domain> <upstream>"))
		return nil
	}

	tableRows := make([][]string, 0, len(rows))
	manualCount := 0
	for _, r := range rows {
		state := "enabled"
		if !r.Enabled {
			state = "disabled"
		}
		src := ui.Dim(r.Source)
		if r.Source == "manual" {
			manualCount++
			src = ui.Cyan("manual")
		}
		tlsLabel := r.TLS
		switch r.TLS {
		case edge.ManualTLSAuto:
			tlsLabel = ui.Cyan(r.TLS)
		case edge.ManualTLSManual:
			tlsLabel = ui.Green(r.TLS)
		case edge.ManualTLSNone:
			tlsLabel = ui.Yellow(r.TLS)
		}
		note := r.Note
		if note == "" {
			note = ui.Dim("-")
		}
		tableRows = append(tableRows, []string{
			ui.Bold(r.Domain),
			src,
			tlsLabel,
			ui.EdgeStatusBadge(state),
			strings.Join(r.Upstreams, ","),
			note,
		})
	}
	ui.PrintTable(ops.stdout, []string{"DOMAIN", "SOURCE", "TLS", "STATE", "UPSTREAMS", "NOTE"}, tableRows)
	fmt.Fprintf(ops.stdout, "\n%s\n", ui.Dim(fmt.Sprintf("Total: %d domain(s) (%d manual, %d panel)", len(rows), manualCount, len(rows)-manualCount)))
	return nil
}

func runEdgeDomainShow(ops *edgeOps, args []string) error {
	if len(args) == 0 || hasHelpFlag(args) {
		printEdgeCommandHelp(ops.stdout, "domain show")
		return nil
	}
	var jsonOut, showConfig bool
	var query string
	for _, a := range args {
		switch a {
		case "--json":
			jsonOut = true
		case "--config":
			showConfig = true
		default:
			if strings.HasPrefix(a, "-") {
				return fmt.Errorf("unknown flag: %s", a)
			}
			if query == "" {
				query = a
			} else {
				return fmt.Errorf("unexpected argument %q", a)
			}
		}
	}
	if query == "" {
		return errors.New("usage: nd edge domain show <domain> [--json] [--config]")
	}

	manualFile, err := edge.LoadManualDomains(ops.paths.DomainsFile)
	if err != nil {
		return err
	}
	idx, entry := edge.FindManualDomain(manualFile, query)
	if idx < 0 || entry == nil {
		return fmt.Errorf("manual domain %q not found", query)
	}

	if showConfig {
		cfg, _ := edge.LoadConfig(ops.paths.ConfigFile)
		rendered, err := renderManualSnippet(ops, cfg, []edge.ManualDomainEntry{*entry}, ops.now())
		if err != nil {
			return err
		}
		if jsonOut {
			b, _ := json.MarshalIndent(map[string]any{
				"domain": entry.Domain,
				"engine": engineNameFromConfig(cfg),
				"config": rendered,
			}, "", "  ")
			fmt.Fprintln(ops.stdout, string(b))
			return nil
		}
		fmt.Fprint(ops.stdout, rendered)
		return nil
	}

	if jsonOut {
		b, _ := json.MarshalIndent(entry, "", "  ")
		fmt.Fprintln(ops.stdout, string(b))
		return nil
	}

	stateStr := "enabled"
	if !entry.Enabled {
		stateStr = "disabled"
	}
	fmt.Fprintf(ops.stdout, "%s  %s\n", ui.HiCyan(entry.Domain), ui.EdgeStatusBadge(stateStr))
	fmt.Fprintf(ops.stdout, "  %-16s %s\n", ui.Dim("ID:"), entry.ID)
	fmt.Fprintf(ops.stdout, "  %-16s %s\n", ui.Dim("Hosts:"), strings.Join(entry.AllHosts(), ", "))
	fmt.Fprintf(ops.stdout, "  %-16s %s (lb: %s)\n", ui.Dim("Upstreams:"), ui.Cyan(formatUpstreamSummary(entry.Upstreams)), entry.EffectiveLB())
	fmt.Fprintf(ops.stdout, "  %-16s %s (force-https: %v)\n", ui.Dim("TLS:"), entry.TLS, entry.EffectiveForceHTTPS())
	if entry.CertFile != "" {
		fmt.Fprintf(ops.stdout, "  %-16s %s (key: %s)\n", ui.Dim("Manual Cert:"), entry.CertFile, entry.KeyFile)
	}
	fmt.Fprintf(ops.stdout, "  %-16s host=%s no-verify=%v sni=%q\n", ui.Dim("Origin Opts:"), entry.EffectiveHostHeader(), entry.Origin.NoVerify, entry.Origin.SNI)
	realIPDisplay := entry.RealIP
	if realIPDisplay == "" {
		realIPDisplay = "inherit"
	}
	fmt.Fprintf(ops.stdout, "  %-16s websocket=%v max-body=%d timeout=%ds connect=%ds keepalive=%d real-ip=%s\n",
		ui.Dim("Proxy Settings:"), entry.EffectiveWebsocket(), entry.MaxBodyBytes, entry.EffectiveTimeoutSec(), entry.EffectiveConnectTimeoutSec(), entry.EffectiveKeepalive(), realIPDisplay)
	if len(entry.HeadersUp) > 0 {
		pairs := make([]string, len(entry.HeadersUp))
		for i, h := range entry.HeadersUp {
			pairs[i] = h.Name + "=" + h.Value
		}
		fmt.Fprintf(ops.stdout, "  %-16s %s\n", ui.Dim("Headers Up:"), strings.Join(pairs, ", "))
	}
	if len(entry.HeadersDown) > 0 {
		pairs := make([]string, len(entry.HeadersDown))
		for i, h := range entry.HeadersDown {
			pairs[i] = h.Name + "=" + h.Value
		}
		fmt.Fprintf(ops.stdout, "  %-16s %s\n", ui.Dim("Headers Down:"), strings.Join(pairs, ", "))
	}
	if entry.Note != "" {
		fmt.Fprintf(ops.stdout, "  %-16s %s\n", ui.Dim("Note:"), entry.Note)
	}
	return nil
}

func runEdgeDomainRestore(ops *edgeOps, args []string) error {
	if len(args) == 0 || hasHelpFlag(args) {
		printEdgeCommandHelp(ops.stdout, "domain restore")
		return nil
	}
	var listOnly, jsonOut bool
	var query string
	for _, a := range args {
		switch a {
		case "--list":
			listOnly = true
		case "--json":
			jsonOut = true
		default:
			if strings.HasPrefix(a, "-") {
				return fmt.Errorf("unknown flag: %s", a)
			}
			if query == "" {
				query = a
			}
		}
	}

	now := ops.now()
	trashFile, err := edge.LoadManualTrash(ops.paths.DomainsTrashFile, now)
	if err != nil {
		return err
	}

	if listOnly {
		if jsonOut {
			b, _ := json.MarshalIndent(trashFile, "", "  ")
			fmt.Fprintln(ops.stdout, string(b))
			return nil
		}
		if len(trashFile.Entries) == 0 {
			fmt.Fprintln(ops.stdout, ui.Dim("Trash is empty."))
			return nil
		}
		tableRows := make([][]string, 0, len(trashFile.Entries))
		for _, item := range trashFile.Entries {
			rem := item.RemovedAt.Add(edge.ManualTrashRetention).Sub(now)
			days := int(rem.Hours() / 24)
			expStr := fmt.Sprintf("%dd", days)
			if days <= 5 {
				expStr = ui.Red(expStr)
			} else {
				expStr = ui.Yellow(expStr)
			}
			tableRows = append(tableRows, []string{
				ui.Bold(item.Entry.Domain),
				ui.Yellow(item.Reason),
				item.RemovedAt.Format(time.RFC3339),
				expStr,
			})
		}
		ui.PrintTable(ops.stdout, []string{"DOMAIN", "REASON", "REMOVED AT", "EXPIRES IN"}, tableRows)
		return nil
	}

	if query == "" {
		return errors.New("usage: nd edge domain restore <domain> | --list")
	}

	ctx := context.Background()
	lock, err := edge.AcquireLockBlocking(ctx, edgeConfigLockPath(ops.paths))
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	cfg, err := edge.LoadConfig(ops.paths.ConfigFile)
	if err != nil {
		return err
	}
	manualFile, err := edge.LoadManualDomains(ops.paths.DomainsFile)
	if err != nil {
		return err
	}
	trashFile, err = edge.LoadManualTrash(ops.paths.DomainsTrashFile, now)
	if err != nil {
		return err
	}

	restoredItem, ok := edge.TakeFromTrash(trashFile, query)
	if !ok {
		return fmt.Errorf("domain %q not found in trash", query)
	}
	entry := restoredItem.Entry
	entry.UpdatedAt = now
	if entry.TLS == edge.ManualTLSManual {
		if _, certErr := os.Stat(entry.CertFile); certErr != nil {
			return fmt.Errorf("manual certificate %s no longer exists; re-add with --cert and --key", entry.CertFile)
		}
	}

	claims := loadClaimsSnapshot(ops, cfg, now)
	if err := edge.CheckManualHostConflicts(entry, manualFile, claims, ops.paths, cfg.EngineName(), ""); err != nil {
		return err
	}

	candidate := edge.CloneManualDomainsFile(manualFile)
	candidate.Domains = append(candidate.Domains, entry)
	edge.SortManualDomains(candidate.Domains)

	reloadedViaDaemon, err := applyCandidateManualDomains(ctx, ops, cfg, candidate, now)
	if err != nil {
		return err
	}
	if err := edge.SaveManualDomains(ops.paths.DomainsFile, candidate); err != nil {
		return err
	}
	_ = edge.SaveManualTrash(ops.paths.DomainsTrashFile, trashFile, now)
	if reloadedViaDaemon {
		signalDaemonReload(ctx, ops)
	}

	if jsonOut {
		b, _ := json.MarshalIndent(entry, "", "  ")
		fmt.Fprintln(ops.stdout, string(b))
		return nil
	}
	ui.Success("Restored manual domain %s", entry.Domain)
	return nil
}

func runEdgeDomainExport(ops *edgeOps, args []string) error {
	if hasHelpFlag(args) {
		printEdgeCommandHelp(ops.stdout, "domain export")
		return nil
	}
	var outFile string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--file" && i+1 < len(args) {
			outFile = args[i+1]
			i++
		} else if strings.HasPrefix(a, "--file=") {
			outFile = strings.TrimPrefix(a, "--file=")
		} else {
			return fmt.Errorf("unknown flag: %s", a)
		}
	}

	manualFile, err := edge.LoadManualDomains(ops.paths.DomainsFile)
	if err != nil {
		return err
	}
	data, err := edge.ExportManualDomainsJSON(manualFile)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if outFile != "" {
		if err := edge.WriteFileAtomic(outFile, data, 0o600); err != nil {
			return err
		}
		ui.Success("Exported %d manual domain(s) to %s", len(manualFile.Domains), outFile)
		return nil
	}
	_, err = ops.stdout.Write(data)
	return err
}

func runEdgeDomainImport(ops *edgeOps, args []string) error {
	if len(args) == 0 || hasHelpFlag(args) {
		printEdgeCommandHelp(ops.stdout, "domain import")
		return nil
	}
	var filePath string
	var replace, dryRun, yes, jsonOut bool
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--file" && i+1 < len(args):
			filePath = args[i+1]
			i++
		case strings.HasPrefix(a, "--file="):
			filePath = strings.TrimPrefix(a, "--file=")
		case a == "--replace":
			replace = true
		case a == "--dry-run":
			dryRun = true
		case a == "--yes", a == "-y":
			yes = true
		case a == "--json":
			jsonOut = true
		default:
			return fmt.Errorf("unknown flag: %s", a)
		}
	}
	if strings.TrimSpace(filePath) == "" {
		return errors.New("usage: nd edge domain import --file <path> [--replace] [--dry-run]")
	}

	raw, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", filePath, err)
	}

	ctx := context.Background()
	lock, err := edge.AcquireLockBlocking(ctx, edgeConfigLockPath(ops.paths))
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	cfg, err := edge.LoadConfig(ops.paths.ConfigFile)
	if err != nil {
		return err
	}
	imported, err := edge.ParseImportedManualDomains(raw, cfg.ShieldHostname)
	if err != nil {
		return err
	}
	existing, err := edge.LoadManualDomains(ops.paths.DomainsFile)
	if err != nil {
		return err
	}

	now := ops.now()
	var candidate *edge.ManualDomainsFile
	if replace {
		if !dryRun {
			ok, err := confirmDestructiveAction(ops, yes, fmt.Sprintf("Replace all %d existing manual domain(s) with %d imported domain(s)?", len(existing.Domains), len(imported.Domains)))
			if err != nil {
				return err
			}
			if !ok {
				fmt.Fprintln(ops.stdout, "Aborted.")
				return nil
			}
		}
		candidate = edge.CloneManualDomainsFile(imported)
	} else {
		candidate = edge.CloneManualDomainsFile(existing)
		for _, imp := range imported.Domains {
			if idx, _ := edge.FindManualDomain(candidate, imp.Domain); idx >= 0 {
				imp.CreatedAt = candidate.Domains[idx].CreatedAt
				imp.UpdatedAt = now
				candidate.Domains[idx] = imp
			} else {
				if imp.CreatedAt.IsZero() {
					imp.CreatedAt = now
				}
				imp.UpdatedAt = now
				candidate.Domains = append(candidate.Domains, imp)
			}
		}
		edge.SortManualDomains(candidate.Domains)
	}

	claims := loadClaimsSnapshot(ops, cfg, now)
	for _, entry := range candidate.Domains {
		if err := edge.CheckManualHostConflicts(entry, candidate, claims, ops.paths, cfg.EngineName(), entry.ID); err != nil {
			return err
		}
	}

	if dryRun {
		if jsonOut {
			b, _ := json.MarshalIndent(candidate, "", "  ")
			fmt.Fprintln(ops.stdout, string(b))
			return nil
		}
		fmt.Fprintf(ops.stdout, "Dry run: would have %d manual domain(s) (replace=%v)\n", len(candidate.Domains), replace)
		return nil
	}

	reloadedViaDaemon, err := applyCandidateManualDomains(ctx, ops, cfg, candidate, now)
	if err != nil {
		return err
	}
	if err := edge.SaveManualDomains(ops.paths.DomainsFile, candidate); err != nil {
		return err
	}
	if reloadedViaDaemon {
		signalDaemonReload(ctx, ops)
	}

	if jsonOut {
		b, _ := json.MarshalIndent(candidate, "", "  ")
		fmt.Fprintln(ops.stdout, string(b))
		return nil
	}
	ui.Success("Imported %d manual domain(s) (total active/stored: %d)", len(imported.Domains), len(candidate.Domains))
	return nil
}

// --- Shared Helpers for Manual Domain Mutations & Rendering ---

func applyCandidateManualDomains(
	ctx context.Context,
	ops *edgeOps,
	cfg *edge.Config,
	candidate *edge.ManualDomainsFile,
	now time.Time,
) (needDaemonReload bool, err error) {
	rec, err := buildReconcilerForOps(ops, cfg)
	if err != nil {
		return false, err
	}
	ep := ops.paths.EnginePathsFor(cfg.EngineName())
	panelsDir, _ := edge.ReadDirLink(ep.PanelsLink)
	claims, _ := edge.LoadClaims(ops.paths.ClaimsFile, panelsDir, now)
	meta, _ := edge.LoadApplyMeta(ops.paths.ApplyMeta)
	panelStates := loadPanelStatesForConfig(ops, cfg, now)

	lock, lockErr := edge.AcquireLock(ops.paths.LockFile)
	if errors.Is(lockErr, edge.ErrLockHeld) {
		if err := rec.ValidateManualCandidate(ctx, cfg, panelStates, claims, meta, candidate, now); err != nil {
			return false, err
		}
		return true, nil
	}
	if lockErr != nil {
		return false, lockErr
	}
	defer func() { _ = lock.Release() }()

	out, applyErr := rec.ReconcileOnceWithManual(ctx, cfg, panelStates, claims, meta, candidate, true, false, now)
	if applyErr != nil {
		return false, applyErr
	}
	if msg, isolated := out.IsolatedPanels[edge.ManualPanelID]; isolated {
		return false, errors.New(msg)
	}
	return false, nil
}

func buildReconcilerForOps(ops *edgeOps, cfg *edge.Config) (*edge.Reconciler, error) {
	eng, err := buildEdgeEngineWithOps(ops, cfg.EngineName(), cfg)
	if err != nil {
		return nil, err
	}
	ep := eng.Paths()
	cfIPs := edge.LoadOrInitCFIPStore(ops.paths.CFIPsFile)
	dnsGate := edge.NewDNSGate(edge.DNSGateOptions{
		CFIPs:     cfIPs,
		CachePath: ops.paths.DNSCacheFile,
		ACMEEmail: cfg.ACMEEmail,
		Off:       cfg.DNSGateOff,
		Resolver:  ops.dnsResolver,
	})
	var applier *edge.Applier
	if eng.Name() == edge.EngineCaddy && ops.caddy != nil {
		applier = edge.NewApplier(ops.paths, ops.caddy)
	} else {
		applier = edge.NewApplierForEngine(ops.paths, eng)
	}
	probe := edge.NewCertProbe(ep.CertsDir, ops.systemd)
	probe.ManualCertsDir = ep.ManualCertsDir
	rec := edge.NewReconcilerWithEngine(ops.paths, eng, dnsGate, applier, probe, ops.systemd, cfIPs)
	if extra := localPublicIPAddrsFromOps(ops); len(extra) > 0 {
		rec.SetExtraShieldIPs(extra)
	}
	return rec, nil
}

func localPublicIPAddrsFromOps(ops *edgeOps) []netip.Addr {
	if ops.localPublicIPs == nil {
		return nil
	}
	v4, v6 := ops.localPublicIPs()
	raw := make([]string, 0, len(v4)+len(v6))
	raw = append(raw, v4...)
	raw = append(raw, v6...)
	return edge.ParseIPStrings(raw)
}

func loadPanelStatesForConfig(ops *edgeOps, cfg *edge.Config, now time.Time) map[string]*edge.PanelState {
	states := make(map[string]*edge.PanelState, len(cfg.Panels))
	for _, p := range cfg.Panels {
		st, _ := edge.LoadPanelState(ops.paths.StateFile(p.PanelID), p.PanelID, p.PanelName, now)
		states[p.PanelID] = st
	}
	return states
}

func loadClaimsSnapshot(ops *edgeOps, cfg *edge.Config, now time.Time) *edge.ClaimsStore {
	engineName := edge.EngineCaddy
	if cfg != nil {
		engineName = cfg.EngineName()
	}
	ep := ops.paths.EnginePathsFor(engineName)
	panelsDir, _ := edge.ReadDirLink(ep.PanelsLink)
	claims, _ := edge.LoadClaims(ops.paths.ClaimsFile, panelsDir, now)
	return claims
}

func renderManualSnippet(ops *edgeOps, cfg *edge.Config, entries []edge.ManualDomainEntry, now time.Time) (string, error) {
	engName := engineNameFromConfig(cfg)
	eng, err := buildEdgeEngineWithOps(ops, engName, cfg)
	if err != nil {
		return "", err
	}
	ep := eng.Paths()
	acmeEmail := ""
	if cfg != nil {
		acmeEmail = cfg.ACMEEmail
	}
	res, err := edge.RenderManualForEngine(eng, entries, edge.ManualRenderOptions{
		Now:            now,
		ACMEEmail:      acmeEmail,
		ManualCertsDir: ep.ManualCertsDir,
		CertsDir:       ep.CertsDir,
	})
	if err != nil {
		return "", err
	}
	return res.Content, nil
}

func printManualDryRun(ops *edgeOps, cfg *edge.Config, entries []edge.ManualDomainEntry, domain string, jsonOut bool, now time.Time) error {
	rendered, err := renderManualSnippet(ops, cfg, entries, now)
	if err != nil {
		return err
	}
	if jsonOut {
		b, _ := json.MarshalIndent(map[string]any{
			"dry_run": true,
			"domain":  domain,
			"engine":  engineNameFromConfig(cfg),
			"entry":   entries[0],
			"config":  rendered,
		}, "", "  ")
		fmt.Fprintln(ops.stdout, string(b))
		return nil
	}
	fmt.Fprintf(ops.stdout, "# Dry run (%s) — no files changed\n%s", engineNameFromConfig(cfg), rendered)
	return nil
}

func cleanupAfterRemoval(ops *edgeOps, cfg *edge.Config, remaining *edge.ManualDomainsFile, removed []edge.ManualDomainEntry, now time.Time) {
	claims := loadClaimsSnapshot(ops, cfg, now)
	cfIPs := edge.LoadOrInitCFIPStore(ops.paths.CFIPsFile)
	dnsGate := edge.NewDNSGate(edge.DNSGateOptions{
		CFIPs:     cfIPs,
		CachePath: ops.paths.DNSCacheFile,
		Off:       cfg.DNSGateOff,
	})
	health, _ := edge.LoadDomainsHealth(ops.paths.DomainsHealthFile)
	edge.CleanupRemovedDomainArtifacts(ops.paths, remaining, claims, dnsGate, health, removed)
	_ = dnsGate.SaveCache()
	if ops.paths.DomainsHealthFile != "" {
		_ = edge.SaveDomainsHealth(ops.paths.DomainsHealthFile, health)
	}
}

func engineNameFromConfig(cfg *edge.Config) string {
	if cfg == nil {
		return edge.EngineCaddy
	}
	return cfg.EngineName()
}

func formatUpstreamSummary(ups []edge.ManualUpstream) string {
	parts := make([]string, len(ups))
	for i, u := range ups {
		parts[i] = u.URL()
	}
	return strings.Join(parts, ",")
}

func confirmDestructiveAction(ops *edgeOps, yes bool, prompt string) (bool, error) {
	if yes {
		return true, nil
	}
	in := ops.stdin
	if in == nil {
		in = os.Stdin
	}
	if !stdinIsTerminal(in) {
		return false, errors.New("non-interactive session: re-run with --yes to confirm")
	}
	fmt.Fprintf(ops.stdout, "%s [y/N]: ", prompt)
	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes", nil
}
