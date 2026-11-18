package proxy

import (
	"os"
	"regexp"
	"strings"
)

var (
	braceEnvPattern  = regexp.MustCompile(`\$\{([a-zA-Z_][a-zA-Z0-9_]*)(?::?-([^}]*))?\}`)
	simpleEnvPattern = regexp.MustCompile(`\$([a-zA-Z_][a-zA-Z0-9_]*)`)
)

// ParseComposeEnv parses dotenv-formatted text blocks in priority order
// so later blocks override keys defined in earlier ones.
func ParseComposeEnv(blocks ...string) map[string]string {
	out := make(map[string]string)
	for _, block := range blocks {
		for _, rawLine := range strings.Split(block, "\n") {
			line := strings.TrimSpace(rawLine)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			line = strings.TrimPrefix(line, "export ")
			eq := strings.IndexByte(line, '=')
			if eq <= 0 {
				continue
			}
			key := strings.TrimSpace(line[:eq])
			val := strings.TrimSpace(line[eq+1:])
			if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
				val = val[1 : len(val)-1]
			}
			if key != "" {
				out[key] = val
			}
		}
	}
	return out
}

func lookupComposeVar(name string, envMaps ...map[string]string) (string, bool) {
	for i := len(envMaps) - 1; i >= 0; i-- {
		if envMaps[i] == nil {
			continue
		}
		if v, ok := envMaps[i][name]; ok {
			return v, true
		}
	}
	return os.LookupEnv(name)
}

// expandComposeEnv resolves common docker-compose variable patterns:
// ${VAR:-default}, ${VAR-default}, ${VAR}, and $VAR.
// Provided envMaps take precedence over process environment variables.
func expandComposeEnv(s string, envMaps ...map[string]string) string {
	if !strings.Contains(s, "$") {
		return s
	}

	s = braceEnvPattern.ReplaceAllStringFunc(s, func(match string) string {
		inner := match[2 : len(match)-1]
		var varName, op, defaultVal string
		if idx := strings.Index(inner, ":-"); idx != -1 {
			varName, op, defaultVal = inner[:idx], ":-", inner[idx+2:]
		} else if idx := strings.IndexByte(inner, '-'); idx != -1 {
			varName, op, defaultVal = inner[:idx], "-", inner[idx+1:]
		} else {
			varName = inner
		}
		val, exists := lookupComposeVar(varName, envMaps...)
		if op == "-" {
			if exists {
				return val
			}
			return defaultVal
		}
		if exists && val != "" {
			return val
		}
		return defaultVal
	})

	s = simpleEnvPattern.ReplaceAllStringFunc(s, func(match string) string {
		sub := simpleEnvPattern.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		if val, exists := lookupComposeVar(sub[1], envMaps...); exists && val != "" {
			return val
		}
		return ""
	})

	return s
}
