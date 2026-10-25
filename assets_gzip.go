package main

import (
	"bytes"
	"compress/gzip"
	"io/fs"
	"mime"
	"path"
	"strings"
	"sync"

	"github.com/gofiber/fiber/v2"
)

// gzipAssets serves the Vite files (named by content hash, so they never change) gzip-compressed
// from memory: each file is compressed once, at the best level, instead of on every request.
func gzipAssets(fsys fs.FS) fiber.Handler {
	var cache sync.Map // file name -> []byte
	return func(c *fiber.Ctx) error {
		if c.Method() != fiber.MethodGet && c.Method() != fiber.MethodHead {
			return c.Next()
		}
		name := strings.TrimPrefix(c.Path(), "/assets/")
		ext := path.Ext(name)
		if name == "" || name == c.Path() || strings.Contains(name, "..") || (ext != ".js" && ext != ".css" && ext != ".svg") {
			return c.Next()
		}
		if !strings.Contains(c.Get(fiber.HeaderAcceptEncoding), "gzip") {
			return c.Next()
		}
		var body []byte
		if v, ok := cache.Load(name); ok {
			body = v.([]byte)
		} else {
			raw, err := fs.ReadFile(fsys, name)
			if err != nil {
				return c.Next()
			}
			var buf bytes.Buffer
			zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
			_, _ = zw.Write(raw)
			_ = zw.Close()
			body = buf.Bytes()
			cache.Store(name, body)
		}
		c.Set(fiber.HeaderContentEncoding, "gzip")
		c.Set(fiber.HeaderVary, fiber.HeaderAcceptEncoding)
		c.Set(fiber.HeaderContentType, mime.TypeByExtension(ext))
		c.Set(fiber.HeaderCacheControl, "public, max-age=31536000, immutable")
		return c.Send(body)
	}
}
