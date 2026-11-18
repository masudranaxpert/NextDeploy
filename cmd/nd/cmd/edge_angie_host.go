package cmd

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"nd/internal/edge"
	"nd/internal/ui"
)

var angieOpenSSLRe = regexp.MustCompile(`(running|built) with OpenSSL (\d+)\.(\d+)`)

// angieOpenSSLWarning returns a warning when `angie -V` shows an OpenSSL older than 3.5.
// Cloudflare can open origin connections with post-quantum key exchange only (ML-KEM);
// OpenSSL before 3.5 cannot answer that, so such zones get HTTP 525 while Caddy (Go TLS)
// works. Seen on Ubuntu 20.04, whose Angie repo ships 1.10 built against OpenSSL 1.1.1.
func angieOpenSSLWarning(versionOutput string) string {
	matches := angieOpenSSLRe.FindAllStringSubmatch(versionOutput, -1)
	if len(matches) == 0 {
		return ""
	}
	m := matches[0]
	for _, cand := range matches { // the library actually loaded wins over the build-time one
		if cand[1] == "running" {
			m = cand
		}
	}
	major, _ := strconv.Atoi(m[2])
	minor, _ := strconv.Atoi(m[3])
	if major > 3 || (major == 3 && minor >= 5) {
		return ""
	}
	return "Angie uses OpenSSL " + m[2] + "." + m[3] + " (older than 3.5). Cloudflare zones that connect to the origin " +
		"with post-quantum key exchange only will fail with HTTP 525. Use an OS with OpenSSL 3.5+ " +
		"(Ubuntu 26.04, Debian 13), the official Angie Docker image, or set the zone's origin " +
		"post-quantum setting to \"supported\"."
}

func warnAngieOpenSSL(ctx context.Context, ops *edgeOps) {
	out, err := ops.runCmd(ctx, "angie", "-V")
	if err != nil {
		return
	}
	if w := angieOpenSSLWarning(out); w != "" {
		ui.Warn("%s", w)
	}
}

// angieLogrotatePath is the logrotate script shipped by the Angie package.
var angieLogrotatePath = "/etc/logrotate.d/angie"

// fixAngieLogrotatePID points the packaged logrotate script at the pid file nd edge renders.
// Some packages signal /run/angie.pid; with nd's /run/angie/angie.pid the USR1 is never sent,
// so Angie keeps writing into the rotated (and later compressed) file.
func fixAngieLogrotatePID() error {
	raw, err := os.ReadFile(angieLogrotatePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	const legacy = "/run/angie.pid"
	if edge.DefaultAngiePIDFile == legacy || !strings.Contains(string(raw), legacy) {
		return nil
	}
	fixed := strings.ReplaceAll(string(raw), legacy, edge.DefaultAngiePIDFile)
	return edge.WriteFileAtomic(angieLogrotatePath, []byte(fixed), 0o644)
}

// switchProbeWait bounds how long a refused connection is retried after the new engine
// starts: a unit can report "started" a moment before the proxy binds :443 (e.g. Angie run
// from a container), and rolling back for that would cost a needless second cutover.
var switchProbeWait = 15 * time.Second

func probeSwitchHostReady(ctx context.Context, ops *edgeOps, host string) error {
	deadline := time.Now().Add(switchProbeWait)
	for {
		err := probeSwitchHost(ctx, ops, host)
		if err == nil || !errors.Is(err, syscall.ECONNREFUSED) || time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(500 * time.Millisecond):
		}
	}
}
