package edge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// UserConfigSite describes a hand-written site block found in user/*.conf,
// user/*.caddy, or the unmanaged portion of Caddyfile.
type UserConfigSite struct {
	Hosts     []string `json:"hosts"`
	Primary   string   `json:"primary"`
	File      string   `json:"file"`
	Engine    string   `json:"engine"`
	Upstreams []string `json:"upstreams,omitempty"`
	TLS       string   `json:"tls,omitempty"`
	RawBlock  string   `json:"raw_block,omitempty"`

	startLine int
	endLine   int
}

// ContainsHost reports whether normalized host matches any hostname in this block.
func (s UserConfigSite) ContainsHost(normHost string) bool {
	if normHost == "" {
		return false
	}
	for _, h := range s.Hosts {
		if h == normHost {
			return true
		}
	}
	return false
}

// UserConfigBackup holds the pre-mutation state of one user config file for rollback.
type UserConfigBackup struct {
	Path    string
	Existed bool
	Mode    os.FileMode
	Content []byte
}

// RestoreUserConfigBackups restores every backed-up file to its original state.
func RestoreUserConfigBackups(backups []UserConfigBackup) error {
	var firstErr error
	for i := len(backups) - 1; i >= 0; i-- {
		b := backups[i]
		if !b.Existed {
			if err := os.Remove(b.Path); err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
				firstErr = err
			}
			continue
		}
		mode := b.Mode
		if mode == 0 {
			mode = 0o644
		}
		if err := WriteFileAtomic(b.Path, b.Content, mode); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// UserConfigRemovalResult summarizes hosts removed across user proxy config files.
type UserConfigRemovalResult struct {
	RemovedHosts  map[string][]string // host -> source file paths
	ModifiedFiles []string
	RemovedFiles  []string
	Backups       []UserConfigBackup
}

type userConfigSource struct {
	path        string
	isCaddyfile bool
	engine      string
}

func collectUserConfigSources(paths Paths, engineName string) ([]userConfigSource, error) {
	ep := paths.EnginePathsFor(engineName)
	ext := ".conf"
	if ep.Binary == "caddy" || engineName == EngineCaddy || engineName == "" {
		if ep.PanelExt == ".caddy" {
			ext = ".caddy"
		}
	}

	var sources []userConfigSource
	if ep.UserDir != "" {
		entries, err := os.ReadDir(ep.UserDir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read user dir %s: %w", ep.UserDir, err)
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name() < entries[j].Name()
		})
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ext) {
				continue
			}
			sources = append(sources, userConfigSource{
				path:   filepath.Join(ep.UserDir, entry.Name()),
				engine: engineName,
			})
		}
	}

	if (engineName == EngineCaddy || engineName == "") && paths.Caddyfile != "" {
		if _, err := os.Stat(paths.Caddyfile); err == nil {
			sources = append(sources, userConfigSource{
				path:        paths.Caddyfile,
				isCaddyfile: true,
				engine:      EngineCaddy,
			})
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("stat %s: %w", paths.Caddyfile, err)
		}
	}
	return sources, nil
}

// ScanUserConfigSites returns all site blocks found in user/*.conf, user/*.caddy,
// and unmanaged Caddyfile sections for the given proxy engine.
func ScanUserConfigSites(paths Paths, engineName string) ([]UserConfigSite, error) {
	sources, err := collectUserConfigSources(paths, engineName)
	if err != nil {
		return nil, err
	}

	var all []UserConfigSite
	for _, src := range sources {
		raw, err := os.ReadFile(src.path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", src.path, err)
		}
		text := string(raw)
		if src.isCaddyfile {
			prefix, _, suffix, hasManaged := splitCaddyfileManagedRegion(text)
			if hasManaged {
				all = append(all, parseUserConfigSites(prefix, src.path, src.engine)...)
				all = append(all, parseUserConfigSites(suffix, src.path, src.engine)...)
				continue
			}
		}
		all = append(all, parseUserConfigSites(text, src.path, src.engine)...)
	}
	return all, nil
}

// FindUserConfigSites returns all sites containing query (normalized hostname).
func FindUserConfigSites(sites []UserConfigSite, query string) []UserConfigSite {
	target := normalizeUserConfigHostToken(query)
	if target == "" {
		return nil
	}
	var out []UserConfigSite
	for _, s := range sites {
		if s.ContainsHost(target) {
			out = append(out, s)
		}
	}
	return out
}

// RemoveHostsFromUserConfig removes all blocks or hostname entries matching any host
// in hostsToRemove across user config files and unmanaged Caddyfile regions.
func RemoveHostsFromUserConfig(paths Paths, engineName string, hostsToRemove []string) (*UserConfigRemovalResult, error) {
	targets := make(map[string]struct{}, len(hostsToRemove))
	for _, raw := range hostsToRemove {
		if norm := normalizeUserConfigHostToken(raw); norm != "" {
			targets[norm] = struct{}{}
		}
	}
	res := &UserConfigRemovalResult{
		RemovedHosts: make(map[string][]string),
	}
	if len(targets) == 0 {
		return res, nil
	}

	sources, err := collectUserConfigSources(paths, engineName)
	if err != nil {
		return nil, err
	}

	for _, src := range sources {
		info, statErr := os.Stat(src.path)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				continue
			}
			_ = RestoreUserConfigBackups(res.Backups)
			return nil, fmt.Errorf("stat %s: %w", src.path, statErr)
		}
		rawBytes, err := os.ReadFile(src.path)
		if err != nil {
			_ = RestoreUserConfigBackups(res.Backups)
			return nil, fmt.Errorf("read %s: %w", src.path, err)
		}

		origText := string(rawBytes)
		var newText string
		var removedHere []string

		if src.isCaddyfile {
			prefix, managed, suffix, hasManaged := splitCaddyfileManagedRegion(origText)
			if hasManaged {
				newPrefix, r1 := removeTargetsFromText(prefix, src.engine, targets)
				newSuffix, r2 := removeTargetsFromText(suffix, src.engine, targets)
				removedHere = append(r1, r2...)
				newText = spliceCaddyfileParts(newPrefix, managed, newSuffix)
			} else {
				newText, removedHere = removeTargetsFromText(origText, src.engine, targets)
			}
		} else {
			newText, removedHere = removeTargetsFromText(origText, src.engine, targets)
		}

		if len(removedHere) == 0 {
			continue
		}

		res.Backups = append(res.Backups, UserConfigBackup{
			Path:    src.path,
			Existed: true,
			Mode:    info.Mode().Perm(),
			Content: rawBytes,
		})

		for _, h := range removedHere {
			res.RemovedHosts[h] = append(res.RemovedHosts[h], src.path)
		}

		if !src.isCaddyfile && !HasNonCommentCaddyContent(newText) {
			if err := os.Remove(src.path); err != nil {
				_ = RestoreUserConfigBackups(res.Backups)
				return nil, fmt.Errorf("remove empty user config %s: %w", src.path, err)
			}
			res.RemovedFiles = append(res.RemovedFiles, src.path)
		} else {
			if err := WriteFileAtomic(src.path, []byte(newText), info.Mode().Perm()); err != nil {
				_ = RestoreUserConfigBackups(res.Backups)
				return nil, fmt.Errorf("write %s: %w", src.path, err)
			}
			res.ModifiedFiles = append(res.ModifiedFiles, src.path)
		}
	}

	return res, nil
}

func splitCaddyfileManagedRegion(raw string) (prefix, managed, suffix string, ok bool) {
	startIdx := strings.Index(raw, ManagedStartMarker)
	endIdx := strings.Index(raw, ManagedEndMarker)
	if startIdx >= 0 && endIdx > startIdx {
		afterEnd := endIdx + len(ManagedEndMarker)
		if afterEnd < len(raw) && raw[afterEnd] == '\n' {
			afterEnd++
		}
		return raw[:startIdx], raw[startIdx:afterEnd], raw[afterEnd:], true
	}
	return raw, "", "", false
}

func spliceCaddyfileParts(prefix, managed, suffix string) string {
	var b strings.Builder
	p := strings.TrimRight(prefix, "\n")
	if p != "" {
		b.WriteString(p)
		b.WriteString("\n\n")
	}
	b.WriteString(managed)
	if !strings.HasSuffix(managed, "\n") {
		b.WriteByte('\n')
	}
	s := strings.TrimLeft(suffix, "\n")
	if s != "" {
		b.WriteByte('\n')
		b.WriteString(s)
		if !strings.HasSuffix(s, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func removeTargetsFromText(content, engineName string, targets map[string]struct{}) (string, []string) {
	sites := parseUserConfigSites(content, "", engineName)
	if len(sites) == 0 {
		return content, nil
	}

	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	type editAction struct {
		site       UserConfigSite
		matched    []string
		remainAny  bool
	}
	var actions []editAction
	var removedOrder []string
	seenRemoved := make(map[string]struct{})

	for _, s := range sites {
		var matched []string
		remainAny := false
		for _, h := range s.Hosts {
			if _, hit := targets[h]; hit {
				matched = append(matched, h)
				if _, ok := seenRemoved[h]; !ok {
					seenRemoved[h] = struct{}{}
					removedOrder = append(removedOrder, h)
				}
			} else {
				remainAny = true
			}
		}
		if len(matched) > 0 {
			actions = append(actions, editAction{
				site:      s,
				matched:   matched,
				remainAny: remainAny,
			})
		}
	}
	if len(actions) == 0 {
		return content, nil
	}

	// Process blocks bottom-up so line indices stay valid.
	for i := len(actions) - 1; i >= 0; i-- {
		act := actions[i]
		if !act.remainAny {
			lines = deleteLineRange(lines, act.site.startLine, act.site.endLine)
			continue
		}
		if act.site.Engine == EngineAngie {
			lines = removeHostsFromAngieBlockLines(lines, act.site.startLine, act.site.endLine, targets)
		} else {
			lines[act.site.startLine] = removeHostsFromSiteHeaderLine(lines[act.site.startLine], act.site.Engine, targets)
		}
	}

	joined := collapseExtraBlankLines(lines)
	return joined, removedOrder
}

func deleteLineRange(lines []string, start, end int) []string {
	if start < 0 || end >= len(lines) || start > end {
		return lines
	}
	out := make([]string, 0, len(lines)-(end-start+1))
	out = append(out, lines[:start]...)
	out = append(out, lines[end+1:]...)
	return out
}

func collapseExtraBlankLines(lines []string) string {
	var out []string
	blankRun := 0
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			if len(out) == 0 {
				continue
			}
			blankRun++
			if blankRun > 1 {
				continue
			}
			out = append(out, "")
			continue
		}
		blankRun = 0
		out = append(out, l)
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}

func removeHostsFromSiteHeaderLine(line, engineName string, targets map[string]struct{}) string {
	trimmed := strings.TrimSpace(line)
	indentLen := len(line) - len(strings.TrimLeft(line, " \t"))
	indent := line[:indentLen]

	comment := ""
	code := trimmed
	if idx := findInlineCommentIndex(code); idx >= 0 {
		comment = " " + strings.TrimSpace(code[idx:])
		code = strings.TrimSpace(code[:idx])
	}
	if !strings.HasSuffix(code, "{") {
		return line
	}
	header := strings.TrimSpace(strings.TrimSuffix(code, "{"))
	prefix := ""
	if engineName == EngineFerron && strings.HasPrefix(strings.ToLower(header), "http ") {
		prefix = header[:5]
		header = strings.TrimSpace(header[5:])
	}

	hasCommas := strings.Contains(header, ",")
	var kept []string
	for _, commaPart := range strings.Split(header, ",") {
		for _, tok := range strings.Fields(commaPart) {
			norm := normalizeUserConfigHostToken(tok)
			if norm != "" {
				if _, drop := targets[norm]; drop {
					continue
				}
			}
			kept = append(kept, tok)
		}
	}
	sep := " "
	if hasCommas {
		sep = ", "
	}
	return indent + prefix + strings.Join(kept, sep) + " {" + comment
}

func removeHostsFromAngieBlockLines(lines []string, start, end int, targets map[string]struct{}) []string {
	for idx := end; idx >= start; idx-- {
		trimmed := strings.TrimSpace(lines[idx])
		if !(strings.HasPrefix(trimmed, "server_name ") || strings.HasPrefix(trimmed, "server_name\t")) {
			continue
		}
		indentLen := len(lines[idx]) - len(strings.TrimLeft(lines[idx], " \t"))
		indent := lines[idx][:indentLen]

		comment := ""
		code := trimmed
		if cidx := findInlineCommentIndex(code); cidx >= 0 {
			comment = " " + strings.TrimSpace(code[cidx:])
			code = strings.TrimSpace(code[:cidx])
		}
		stmt := strings.TrimSpace(strings.TrimSuffix(code, ";"))
		fields := strings.Fields(stmt)
		if len(fields) <= 1 {
			continue
		}
		var kept []string
		for _, tok := range fields[1:] {
			norm := normalizeUserConfigHostToken(tok)
			if norm != "" {
				if _, drop := targets[norm]; drop {
					continue
				}
			}
			kept = append(kept, tok)
		}
		if len(kept) == 0 {
			lines = deleteLineRange(lines, idx, idx)
		} else {
			lines[idx] = indent + "server_name " + strings.Join(kept, " ") + ";" + comment
		}
	}
	return lines
}

// parseUserConfigSites extracts structured top-level site blocks from proxy configs.
func parseUserConfigSites(content, filePath, engineName string) []UserConfigSite {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")

	type activeBlock struct {
		startLine   int
		blockEngine string
		isAngieSrv  bool
		hosts       []string
		hostSeen    map[string]struct{}
		upstreams   []string
		upSeen      map[string]struct{}
		hasManual   bool
		hasInternal bool
		hasAutoTLS  bool
		hasListen43 bool
		allHTTPOnly bool
	}

	var sites []UserConfigSite
	var cur *activeBlock
	depth := 0

	addHost := func(b *activeBlock, tok string) {
		h := normalizeUserConfigHostToken(tok)
		if h == "" {
			return
		}
		if _, ok := b.hostSeen[h]; !ok {
			b.hostSeen[h] = struct{}{}
			b.hosts = append(b.hosts, h)
		}
	}
	addUpstream := func(b *activeBlock, target string) {
		u := strings.TrimSpace(strings.Trim(target, `"'`))
		u = strings.TrimSuffix(u, ";")
		if u == "" {
			return
		}
		if _, ok := b.upSeen[u]; !ok {
			b.upSeen[u] = struct{}{}
			b.upstreams = append(b.upstreams, u)
		}
	}

	for idx, rawLine := range lines {
		code := stripConfigLineComment(rawLine)
		trimmed := strings.TrimSpace(code)

		if depth == 0 && strings.HasSuffix(trimmed, "{") {
			header := strings.TrimSpace(strings.TrimSuffix(trimmed, "{"))
			low := strings.ToLower(header)

			if (engineName == EngineAngie || engineName == "") && (low == "server" || strings.HasPrefix(low, "server ")) {
				cur = &activeBlock{
					startLine:   idx,
					blockEngine: EngineAngie,
					isAngieSrv:  true,
					hostSeen:    make(map[string]struct{}),
					upSeen:      make(map[string]struct{}),
				}
			} else {
				isNonHostBlock := header == "" ||
					strings.HasPrefix(header, "(") ||
					strings.HasPrefix(low, "snippet ") ||
					strings.HasPrefix(low, "match ") ||
					strings.HasPrefix(low, "server") ||
					strings.HasPrefix(low, "upstream ") ||
					strings.HasPrefix(low, "globals")
				isFerronHTTP := (engineName == EngineFerron) && strings.HasPrefix(low, "http ")
				if !isNonHostBlock && (low != "http" && (!strings.HasPrefix(low, "http ") || isFerronHTTP)) {
					eng := engineName
					if eng == "" {
						eng = EngineCaddy
					}
					trimmedHeader := header
					if isFerronHTTP {
						trimmedHeader = strings.TrimSpace(header[len("http "):])
					}
					b := &activeBlock{
						startLine:   idx,
						blockEngine: eng,
						hostSeen:    make(map[string]struct{}),
						upSeen:      make(map[string]struct{}),
						allHTTPOnly: true,
					}
					for _, commaPart := range strings.Split(trimmedHeader, ",") {
						for _, tok := range strings.Fields(commaPart) {
							if normalizeUserConfigHostToken(tok) != "" && !strings.HasPrefix(strings.ToLower(strings.Trim(tok, `"'`)), "http://") {
								b.allHTTPOnly = false
							}
							addHost(b, tok)
						}
					}
					if len(b.hosts) > 0 {
						cur = b
					}
				}
			}
		}

		if cur != nil && trimmed != "" {
			low := strings.ToLower(trimmed)
			if cur.isAngieSrv {
				if strings.HasPrefix(trimmed, "server_name ") || strings.HasPrefix(trimmed, "server_name\t") {
					stmt := strings.TrimSuffix(trimmed, ";")
					for _, field := range strings.Fields(stmt)[1:] {
						addHost(cur, field)
					}
				} else if strings.HasPrefix(trimmed, "proxy_pass ") || strings.HasPrefix(trimmed, "proxy_pass\t") {
					fields := strings.Fields(strings.TrimSuffix(trimmed, ";"))
					if len(fields) >= 2 {
						addUpstream(cur, fields[1])
					}
				} else if strings.HasPrefix(trimmed, "ssl_certificate ") || strings.HasPrefix(trimmed, "ssl_certificate\t") {
					cur.hasManual = true
				} else if strings.HasPrefix(trimmed, "acme ") || strings.HasPrefix(trimmed, "acme\t") {
					cur.hasAutoTLS = true
				} else if strings.HasPrefix(trimmed, "listen ") && strings.Contains(trimmed, "443") {
					cur.hasListen43 = true
				}
			} else if cur.blockEngine == EngineFerron {
				if strings.HasPrefix(trimmed, "proxy ") || strings.HasPrefix(trimmed, "proxy\t") {
					fields := strings.Fields(trimmed)
					if len(fields) >= 2 {
						addUpstream(cur, fields[1])
					}
				} else if strings.HasPrefix(low, "tls ") || strings.HasPrefix(low, "cert ") {
					cur.hasManual = true
				} else if strings.Contains(low, "auto_tls") {
					cur.hasAutoTLS = true
				}
			} else {
				if strings.HasPrefix(trimmed, "reverse_proxy ") || strings.HasPrefix(trimmed, "reverse_proxy\t") {
					for _, tok := range strings.Fields(trimmed)[1:] {
						if tok == "{" || strings.HasPrefix(tok, "#") {
							break
						}
						if strings.HasPrefix(tok, "-") || strings.HasPrefix(tok, "@") {
							continue
						}
						addUpstream(cur, tok)
					}
				} else if strings.HasPrefix(low, "tls ") || low == "tls" {
					fields := strings.Fields(low)
					if len(fields) >= 2 && fields[1] == "internal" {
						cur.hasInternal = true
					} else if len(fields) >= 3 {
						cur.hasManual = true
					} else if len(fields) >= 2 && fields[1] == "off" {
						cur.allHTTPOnly = true
					}
				}
			}
		}

		depth += countBracesOutsideQuotes(code)
		if depth <= 0 {
			depth = 0
			if cur != nil {
				if len(cur.hosts) > 0 {
					tlsMode := ManualTLSAuto
					switch {
					case cur.hasManual:
						tlsMode = ManualTLSManual
					case cur.hasInternal:
						tlsMode = "internal"
					case cur.hasAutoTLS:
						tlsMode = ManualTLSAuto
					case cur.isAngieSrv:
						if cur.hasListen43 {
							tlsMode = "custom"
						} else {
							tlsMode = ManualTLSNone
						}
					case cur.allHTTPOnly:
						tlsMode = ManualTLSNone
					}
					sites = append(sites, UserConfigSite{
						Hosts:     cur.hosts,
						Primary:   cur.hosts[0],
						File:      filePath,
						Engine:    cur.blockEngine,
						Upstreams: cur.upstreams,
						TLS:       tlsMode,
						RawBlock:  strings.Join(lines[cur.startLine:idx+1], "\n") + "\n",
						startLine: cur.startLine,
						endLine:   idx,
					})
				}
				cur = nil
			}
		}
	}

	return sites
}

func findInlineCommentIndex(line string) int {
	inSingle, inDouble := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == '#' && !inSingle && !inDouble:
			if i == 0 || line[i-1] == ' ' || line[i-1] == '\t' {
				return i
			}
		}
	}
	return -1
}

func stripConfigLineComment(line string) string {
	if idx := findInlineCommentIndex(line); idx >= 0 {
		return line[:idx]
	}
	return line
}

func countBracesOutsideQuotes(line string) int {
	delta := 0
	inSingle, inDouble := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == '{' && !inSingle && !inDouble:
			delta++
		case c == '}' && !inSingle && !inDouble:
			delta--
		}
	}
	return delta
}
