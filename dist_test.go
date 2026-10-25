package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
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
