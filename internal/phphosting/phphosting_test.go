package phphosting

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"panel/internal/caddy"
	"panel/internal/db"
)

func TestRenderPoolConfinesSite(t *testing.T) {
	s := DefaultSettings()
	s.Additional = "display_startup_errors = 1\n; comment\nzend.assertions = -1"
	out, err := RenderPool(PoolSpec{AppID: "shop-ab12", UID: 20007, RootDir: "public", Settings: s})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"[site-20007]", "user = 20007", "group = 20007",
		"listen = /run/php/shop-ab12.sock", "listen.mode = 0660",
		"chdir = /data/workspaces/shop-ab12/htdocs/public",
		"php_admin_value[open_basedir] = /data/workspaces/shop-ab12:/tmp:/usr/local/lib/php:/dev/urandom",
		"php_admin_value[memory_limit] = 256M",
		"php_value[display_startup_errors] = 1", "php_value[zend.assertions] = -1",
		"pm = dynamic",
		"pm.start_servers = 2",
		"pm.min_spare_servers = 1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("pool missing %q\n%s", want, out)
		}
	}
}

func TestRenderPoolOndemand(t *testing.T) {
	s := DefaultSettings()
	s.ProcessManager = "ondemand"
	out, err := RenderPool(PoolSpec{AppID: "shop-ab12", UID: 20007, RootDir: "public", Settings: s})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "pm = ondemand") || !strings.Contains(out, "pm.process_idle_timeout = 60s") {
		t.Errorf("expected ondemand pool settings, got:\n%s", out)
	}
}

func TestAdditionalDirectivesCannotEscape(t *testing.T) {
	bad := []string{
		"open_basedir = /", "disable_functions =", "auto_prepend_file = /etc/passwd",
		"extension = evil.so", "memory_limit = 5G", "error_log = /x", "SESSION.SAVE_PATH=/",
		"foo", "x = $HOME", "bad key = 1", "a = b ; c",
	}
	for _, b := range bad {
		if _, err := ParseAdditional(b); err == nil {
			t.Errorf("expected %q to be rejected", b)
		}
	}
	if d, err := ParseAdditional("opcache.enable=0\n# c\n\nmax_file_uploads = 50"); err != nil || len(d) != 2 {
		t.Fatalf("good directives rejected: %v %v", d, err)
	}
}

func TestSettingsValidateRespectsCeilings(t *testing.T) {
	s := DefaultSettings()
	if err := s.Validate(Ceilings{}); err != nil {
		t.Fatal(err)
	}
	s.MemoryLimit = "1G"
	if err := s.Validate(Ceilings{MemoryMB: 512}); err == nil {
		t.Fatal("memory_limit above the ceiling (quota or server RAM) must be rejected")
	}
	if err := s.Validate(Ceilings{MemoryMB: 2048}); err != nil {
		t.Fatal(err)
	}
	s.MemoryLimit = "5G"
	if err := s.Validate(Ceilings{MemoryMB: 1024}); err == nil {
		t.Fatal("5G on a 1 GB server must be rejected")
	}
	s = DefaultSettings()
	s.UploadMaxFilesize, s.PostMaxSize = "2G", "2G"
	if err := s.Validate(Ceilings{StorageMB: 1024}); err == nil {
		t.Fatal("upload size above the storage ceiling must be rejected")
	}
	s = DefaultSettings()
	s.UploadMaxFilesize, s.PostMaxSize = "64M", "32M"
	if err := s.Validate(Ceilings{}); err == nil {
		t.Fatal("post_max_size below upload_max_filesize must be rejected")
	}
	s = DefaultSettings()
	s.MaxChildren = 100
	if err := s.Validate(Ceilings{MemoryMB: 512}); err == nil {
		t.Fatal("100 workers on 512 MB must be rejected")
	}
	if got := (Ceilings{MemoryMB: 1024}).MemoryChoices(); got[len(got)-1] != "1G" {
		t.Fatalf("choices must stop at the ceiling: %v", got)
	}
	if got := (Ceilings{MemoryMB: 1024}).MaxChildrenFor(); got != 32 {
		t.Fatalf("1 GB -> 32 workers, got %d", got)
	}
	s = DefaultSettings()
	s.MemoryLimit = "999M"
	if err := s.Validate(Ceilings{}); err == nil {
		t.Fatal("unknown memory_limit must be rejected")
	}
	if SizeToMB("2G") != 2048 || SizeToMB("256M") != 256 || SizeToMB("x") != 0 {
		t.Fatal("SizeToMB broken")
	}
}

func TestRootDirNormalisation(t *testing.T) {
	cases := map[string]string{"": "", "public": "public", "/public/": "public", "a/./b": "a/b"}
	for in, want := range cases {
		got, err := ValidateRootDir(in)
		if err != nil || got != want {
			t.Errorf("ValidateRootDir(%q)=%q,%v want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"../x", "a/../../b", "a;b", "a\nb", "a b", "a{env.X}", "public\"x"} {
		if _, err := ValidateRootDir(in); err == nil {
			t.Errorf("ValidateRootDir(%q) should fail", in)
		}
	}
	if got := DocRoot("x-1", "../../etc"); !strings.HasPrefix(got, "/data/workspaces/x-1/htdocs") {
		t.Errorf("DocRoot escaped: %s", got)
	}
}

func TestSQLQuoting(t *testing.T) {
	if got := SQLString(`a'b\c`); got != `'a\'b\\c'` {
		t.Fatalf("got %s", got)
	}
	if got := SQLIdent("a`b"); got != "`a``b`" {
		t.Fatalf("got %s", got)
	}
	for _, ok := range []string{"u1_shop", "a", "wp_db"} {
		if !ValidIdent(ok) {
			t.Errorf("%s should be valid", ok)
		}
	}
	for _, bad := range []string{"", "1abc", "A", "a-b", "a b", "a;drop", strings.Repeat("a", 49)} {
		if ValidIdent(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
	if len(RandomPassword(20)) != 20 || RandomPassword(20) == RandomPassword(20) {
		t.Fatal("RandomPassword broken")
	}
}

func TestCronValidation(t *testing.T) {
	if err := ValidateSchedule("*/5 * * * *", 5); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSchedule("* * * * *", 5); err == nil {
		t.Fatal("every minute must violate a 5 minute minimum")
	}
	if err := ValidateSchedule("0,2 * * * *", 5); err == nil {
		t.Fatal("two runs 2 minutes apart must violate the minimum")
	}
	if err := ValidateSchedule("* * * * *", 1); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSchedule("@hourly", 60); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSchedule("nonsense", 1); err == nil {
		t.Fatal("garbage must fail")
	}
	if ValidateCommand("  ") == nil || ValidateCommand("php artisan schedule:run") != nil {
		t.Fatal("ValidateCommand broken")
	}
}

func TestHostPathTranslation(t *testing.T) {
	mounts := []mountInfo{
		{Source: "/var/run/docker.sock", Destination: "/var/run/docker.sock"},
		{Source: "/home/me/nd/data", Destination: "/data"},
		{Source: "/mnt/big", Destination: "/data/workspaces"},
	}
	if got := translateHostPath(mounts, "/data/workspaces/app-1"); got != "/mnt/big/app-1" {
		t.Fatalf("longest mount must win, got %s", got)
	}
	if got := translateHostPath(mounts, "/data/other/x"); got != "/home/me/nd/data/other/x" {
		t.Fatalf("got %s", got)
	}
	if got := translateHostPath(mounts, "/elsewhere/x"); got != "/elsewhere/x" {
		t.Fatalf("unmapped path must stay, got %s", got)
	}
	if got := translateHostPath(nil, "/data/x"); got != "/data/x" {
		t.Fatalf("got %s", got)
	}
}

func TestCaddyLabels(t *testing.T) {
	doms := []db.AppDomain{{Domain: "example.com", EnableHTTPS: true, EnableWWW: true}}
	l := caddy.PHPSiteLabels(doms, "/data/workspaces/a-1/htdocs", "/data/workspaces/.nd-php/u1-v83/run/a-1.sock", caddy.SiteSecurity{})
	want := map[string]string{
		"caddy":                     "example.com, www.example.com",
		"caddy.root":                "* /data/workspaces/a-1/htdocs",
		"caddy.php_fastcgi":         "unix//data/workspaces/.nd-php/u1-v83/run/a-1.sock",
		"caddy.file_server":         `{{""}}`,
		"caddy.respond_0":           "@hidden 404",
		"caddy.@hidden.path_regexp": `(^|/)\.(git|svn|hg|env|nextdeploy-db|ht[a-z]*)(/|$|\.)`,
	}
	for k, v := range want {
		if l[k] != v {
			t.Errorf("label %s = %q, want %q", k, l[k], v)
		}
	}
	static := caddy.PHPSiteLabels(doms, "/x", "", caddy.SiteSecurity{})
	if _, ok := static["caddy.php_fastcgi"]; ok {
		t.Error("static sites must not have php_fastcgi")
	}
	two := caddy.PHPSiteLabels([]db.AppDomain{{Domain: "a.com", EnableHTTPS: true}, {Domain: "b.com", EnableHTTPS: true}}, "/x", "/s", caddy.SiteSecurity{})
	if two["caddy_0"] == "" || two["caddy_1"] == "" || two["caddy"] != "" {
		t.Errorf("multi-domain prefixes wrong: %v", two)
	}
	if labelsHash(l) == labelsHash(static) {
		t.Error("labelsHash must differ for different labels")
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote(`a'b`); got != `'a'\''b'` {
		t.Fatalf("got %s", got)
	}
}

func TestValidHost(t *testing.T) {
	for _, ok := range []string{"example.com", "a.b.example.co.uk", "*.example.com", "x-1.example.com", "demo-a7f9-app-45-250-230-64.sslip.io"} {
		if !ValidHost(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "localhost", "a b.com", "a.com {\n respond 200", "a.com,b.com", "http://a.com", "a..com", "-a.com", "a.com/x", "*.*.com", "{env.SECRET}.com"} {
		if ValidHost(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestGrantIdentEscapesWildcards(t *testing.T) {
	cases := map[string]string{
		"u2_shop": "`u2\\_shop`",
		"u2__x":   "`u2\\_\\_x`",
		"u1_a%b":  "`u1\\_a\\%b`",
		"plain":   "`plain`",
	}
	for in, want := range cases {
		if got := GrantIdent(in); got != want {
			t.Errorf("GrantIdent(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestSweepSymlinksRemovesOnlyEscapingLinks(t *testing.T) {
	root := t.TempDir()
	site := filepath.Join(root, "app1")
	other := filepath.Join(root, "app2")
	for _, d := range []string{filepath.Join(site, "htdocs", "inner"), other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(other, ".env"), []byte("SECRET=1"), 0o644)
	good := filepath.Join(site, "htdocs", "ok")
	bad := filepath.Join(site, "htdocs", "l")
	relBad := filepath.Join(site, "htdocs", "inner", "up")
	os.Symlink("inner", good)
	os.Symlink(other, bad)
	os.Symlink("../../../app2", relBad)
	m := &Manager{WorkspacesRoot: root}
	if n := m.SweepSymlinks("app1"); n != 2 {
		t.Fatalf("removed %d links, want 2", n)
	}
	if _, err := os.Lstat(good); err != nil {
		t.Error("a link inside the site was removed")
	}
	for _, p := range []string{bad, relBad} {
		if _, err := os.Lstat(p); err == nil {
			t.Errorf("%s still exists", p)
		}
	}
}

func TestUploadSizesNeverExceedMemoryOrStorage(t *testing.T) {
	last := func(c Ceilings) string { l := c.SizeChoices(); return l[len(l)-1] }
	if got := last(Ceilings{MemoryMB: 1024, StorageMB: 5120}); got != "1G" {
		t.Errorf("1 GB RAM, 5 GB disk: largest upload = %s, want 1G", got)
	}
	if got := last(Ceilings{MemoryMB: 8192, StorageMB: 512}); got != "512M" {
		t.Errorf("small disk: largest upload = %s, want 512M", got)
	}
	s := DefaultSettings()
	s.PostMaxSize = "2G"
	if err := s.Validate(Ceilings{MemoryMB: 1024, StorageMB: 5120}); err == nil {
		t.Error("a 2G post size on a 1 GB server must be refused")
	}
}

func TestDBMemoryDefaultFollowsTheHost(t *testing.T) {
	cases := map[int]int{0: 512, 1024: 512, 2048: 512, 8192: 2048, 16384: 4096, 65536: 4096}
	for host, want := range cases {
		if got := DBMemoryDefaultMB(host); got != want {
			t.Errorf("host %d MB: database memory %d, want %d", host, got, want)
		}
	}
}

func TestTuneDBKeepsTheBufferPoolFromCrowdingOutConnections(t *testing.T) {
	for _, mem := range []int{256, 512, 1024, 4096, 16384} {
		tn := TuneDB(mem)
		if tn.BufferPoolMB*2 > mem {
			t.Errorf("%d MB: buffer pool %d MB is more than half the container", mem, tn.BufferPoolMB)
		}
		if tn.MaxConnections < 100 || tn.MaxConnections > 500 {
			t.Errorf("%d MB: %d connections is outside 100..500", mem, tn.MaxConnections)
		}
		if tn.TmpTableMB < 16 || tn.TmpTableMB > 64 {
			t.Errorf("%d MB: temp table cap %d MB is outside 16..64", mem, tn.TmpTableMB)
		}
	}
	args := strings.Join(TuneDB(512).serverArgs(), " ")
	for _, want := range []string{"--innodb-buffer-pool-size=256M", "--wait-timeout=300", "--interactive-timeout=300", "--max-connections=100"} {
		if !strings.Contains(args, want) {
			t.Errorf("server flags %q lack %s", args, want)
		}
	}
}

func TestUserLimitClauseOnlyOnMariaDB(t *testing.T) {
	if got := userLimitClause("mariadb:11.4", 25, 60); got != "WITH MAX_USER_CONNECTIONS 25 MAX_STATEMENT_TIME 60" {
		t.Errorf("mariadb clause = %q", got)
	}
	if got := userLimitClause("mysql:8.4", 25, 60); got != "WITH MAX_USER_CONNECTIONS 25" {
		t.Errorf("mysql clause = %q (MySQL has no per-user statement time)", got)
	}
	if got := userLimitClause("mariadb:11.4", 10, 0); !strings.HasSuffix(got, "MAX_STATEMENT_TIME 0") {
		t.Errorf("a zero limit must be written out so an older limit is reset: %q", got)
	}
}

func TestSnapToBinaryRAM(t *testing.T) {
	cases := []struct {
		input  int
		maxMB  int
		expect int
	}{
		{2000, 16384, 2048},
		{1000, 16384, 1024},
		{4000, 16384, 4096},
		{3000, 16384, 3072},
		{8000, 16384, 8192},
		{500, 16384, 512},
		{2040, 16384, 2048},
		{2050, 16384, 2048},
		{200, 16384, 512}, // below min (512)
		{20000, 16384, 16384}, // clamped to host max
	}
	for _, c := range cases {
		if got := SnapToBinaryRAM(c.input, c.maxMB); got != c.expect {
			t.Errorf("SnapToBinaryRAM(%d, %d) = %d, want %d", c.input, c.maxMB, got, c.expect)
		}
	}
}
