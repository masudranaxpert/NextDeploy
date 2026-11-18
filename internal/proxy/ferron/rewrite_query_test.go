package ferron

import (
	"regexp"
	"testing"

	"panel/internal/proxy"
)

// Ferron's rewrite matches the full URI (path + "?query"); Apache's RewriteRule matches only the
// path. Simulate Ferron with Go's regexp (same $n / ${n} replacement semantics as Rust regex).
func TestApacheStyleRegexRewriteKeepsQueryOutOfCaptures(t *testing.T) {
	cases := []struct {
		pattern, target string
		qsa             bool
		uri, want       string
	}{
		{`^/api/(.*)$`, `/api.php?path=$1`, true, `/api/users?q=1`, `/api.php?path=users&q=1`},
		{`^/api/(.*)$`, `/api.php?path=$1`, true, `/api/users`, `/api.php?path=users&`},
		{`^/api/(.*)$`, `/api.php?path=$1`, false, `/api/users?q=1`, `/api.php?path=users`},
		{`^/p/([^/]+)/(\d+)$`, `/post.php?slug=$1&id=$2`, true, `/p/hello/42?ref=x`, `/post.php?slug=hello&id=42&ref=x`},
		{`^/old/(.*)$`, `/new/$1`, false, `/old/a/b?x=1`, `/new/a/b?x=1`},
		{`blog/(\S+)`, `/b.php?s=$1`, true, `/x/blog/post?y=2`, `/b.php?s=post&y=2`},
	}
	for _, c := range cases {
		re, repl, err := buildFerronRewriteRule(proxy.RoutingRule{Kind: "rewrite", MatchType: "regex", Pattern: c.pattern, Target: c.target, PreserveQuery: c.qsa})
		if err != nil {
			t.Fatalf("%s: %v", c.pattern, err)
		}
		repl = translateCaptureRefs(repl)
		rx := regexp.MustCompile(re)
		if !rx.MatchString(c.uri) {
			t.Errorf("%s does not match %s (re=%s)", c.pattern, c.uri, re)
			continue
		}
		if got := rx.ReplaceAllString(c.uri, repl); got != c.want {
			t.Errorf("%s on %s = %q, want %q (re=%s repl=%s)", c.pattern, c.uri, got, c.want, re, repl)
		}
	}
}

func TestPathOnlyRegex(t *testing.T) {
	for in, want := range map[string]string{
		`/a/(.*)`:    `/a/([^?]*)`,
		`/a/([^/]+)`: `/a/([^?/]+)`,
		`/a/\.php`:   `/a/\.php`,
		`/x/[a-z.]+`: `/x/[a-z.]+`,
		`/s/(\S+)`:   `/s/([^\s?]+)`,
		`/c/[]a]`:    `/c/[]a]`,
	} {
		if got := pathOnlyRegex(in); got != want {
			t.Errorf("pathOnlyRegex(%q) = %q, want %q", in, got, want)
		}
	}
}
