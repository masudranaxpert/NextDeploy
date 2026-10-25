package main

import (
	"compress/gzip"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
)

// The panel embeds web/dist. If a file the page asks for is missing from it (for example new
// chunk files that were never committed), users get a blank screen and a 404 for the chunk.
func TestEmbeddedUIIsComplete(t *testing.T) {
	root := "web/dist"
	index, err := os.ReadFile(filepath.Join(root, "index.html"))
	if err != nil {
		t.Skip("web/dist is not built")
	}
	ref := regexp.MustCompile(`[A-Za-z0-9_.-]+-[A-Za-z0-9_-]{8}\.(?:js|css)`)
	seen := map[string]bool{}
	var queue []string
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			queue = append(queue, name)
		}
	}
	for _, m := range regexp.MustCompile(`/assets/([A-Za-z0-9_.-]+\.(?:js|css))`).FindAllSubmatch(index, -1) {
		add(string(m[1]))
	}
	if len(queue) == 0 {
		t.Fatal("index.html references no script")
	}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		b, err := os.ReadFile(filepath.Join(root, "assets", name))
		if err != nil {
			t.Errorf("web/dist/assets/%s is referenced by the page but missing: run `npm run build` and commit all of web/dist (git add -A web/dist)", name)
			continue
		}
		if filepath.Ext(name) == ".js" {
			for _, m := range ref.FindAll(b, -1) {
				add(string(m))
			}
		}
	}
}

func TestGzipAssetsServesCompressedOnce(t *testing.T) {
	fsys := fstest.MapFS{"app-abc123.js": {Data: []byte(strings.Repeat("console.log('x');", 500))}}
	app := fiber.New()
	app.Use(compress.New())
	app.Use("/assets", gzipAssets(fsys))
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/assets/app-abc123.js", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if got := res.Header.Get("Content-Encoding"); got != "gzip" {
			t.Fatalf("Content-Encoding = %q, want a single gzip", got)
		}
		zr, err := gzip.NewReader(res.Body)
		if err != nil {
			t.Fatalf("body is not valid gzip (compressed twice?): %v", err)
		}
		body, _ := io.ReadAll(zr)
		if !strings.HasPrefix(string(body), "console.log") || len(body) != 17*500 {
			t.Fatalf("body mismatch: %d bytes", len(body))
		}
		if !strings.Contains(res.Header.Get("Cache-Control"), "immutable") {
			t.Error("asset is not marked immutable")
		}
	}
	req := httptest.NewRequest("GET", "/assets/app-abc123.js", nil)
	res, _ := app.Test(req)
	if res.Header.Get("Content-Encoding") == "gzip" {
		t.Error("gzip sent to a client that did not ask for it")
	}
}
