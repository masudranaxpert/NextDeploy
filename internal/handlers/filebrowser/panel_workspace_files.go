package filebrowser

import (
	"archive/zip"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func (h *Handler) BrowseUrlUpload(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := h.p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).JSON(fiber.Map{"ok": false, "message": "not found"})
	}
	if h.p.IsGitApp(c.UserContext(), id) {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "upload disabled for git apps"})
	}

	var req struct {
		URL  string `json:"url" form:"url"`
		Dest string `json:"dest" form:"dest"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "invalid request format"})
	}

	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "url is required"})
	}

	resp, err := http.Get(req.URL)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": fmt.Sprintf("Failed to fetch URL: %v", err)})
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": fmt.Sprintf("Remote server returned status: %s", resp.Status)})
	}

	filename := filepath.Base(req.URL)
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if f, ok := params["filename"]; ok {
				filename = f
			}
		}
	}
	
	filename = filepath.Clean(filename)
	filename = filepath.Base(filename)

	if filename == "" || filename == "." || filename == "/" {
		filename = "downloaded_file"
	}

	destDir := strings.TrimSpace(req.Dest)
	relPath := filename
	if destDir != "" {
		relPath = destDir + "/" + filename
	}

	if _, err := h.p.Store.SaveUploadedFile(id, relPath, resp.Body); err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "message": fmt.Sprintf("Failed to save file: %v", err)})
	}

	return c.JSON(fiber.Map{"ok": true, "message": "Downloaded to server."})
}

func (h *Handler) BrowseUpload(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := h.p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).JSON(fiber.Map{"ok": false, "message": "not found"})
	}
	if h.p.IsGitApp(c.UserContext(), id) {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "upload disabled for git apps"})
	}

	destDir := strings.TrimSpace(c.FormValue("path"))
	form, err := c.MultipartForm()
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "Failed to parse form data"})
	}

	files := form.File["file"]
	if len(files) == 0 {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "No files uploaded"})
	}

	for _, file := range files {
		relPath := file.Filename
		if destDir != "" {
			relPath = destDir + "/" + file.Filename
		}
		
		f, err := file.Open()
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"ok": false, "message": fmt.Sprintf("Failed to open %s: %v", file.Filename, err)})
		}
		defer f.Close()

		if _, err := h.p.Store.SaveUploadedFile(id, relPath, f); err != nil {
			return c.Status(500).JSON(fiber.Map{"ok": false, "message": fmt.Sprintf("Failed to save %s: %v", file.Filename, err)})
		}
	}

	return c.JSON(fiber.Map{"ok": true, "message": "Files uploaded successfully"})
}

func (h *Handler) BrowseMove(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := h.p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).JSON(fiber.Map{"ok": false, "message": "not found"})
	}
	if h.p.IsGitApp(c.UserContext(), id) {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "move disabled for git apps"})
	}

	var req struct {
		Paths []string `json:"paths" form:"paths"`
		Dest  string   `json:"dest" form:"dest"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "invalid request format"})
	}

	if len(req.Paths) == 0 {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "No paths provided"})
	}

	for _, pth := range req.Paths {
		oldFull, err := h.p.Store.SafeFilePath(id, pth)
		if err != nil {
			continue
		}
		
		newRel := req.Dest + "/" + filepath.Base(pth)
		if req.Dest == "" {
			newRel = filepath.Base(pth)
		}
		newFull, err := h.p.Store.SafeFilePath(id, newRel)
		if err != nil {
			continue
		}

		_ = os.MkdirAll(filepath.Dir(newFull), 0750)
		_ = os.Rename(oldFull, newFull)
	}

	return c.JSON(fiber.Map{"ok": true, "message": "Moved successfully"})
}

func (h *Handler) BrowseCopy(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := h.p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).JSON(fiber.Map{"ok": false, "message": "not found"})
	}
	if h.p.IsGitApp(c.UserContext(), id) {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "copy disabled for git apps"})
	}

	var req struct {
		Paths []string `json:"paths" form:"paths"`
		Dest  string   `json:"dest" form:"dest"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "invalid request format"})
	}

	if len(req.Paths) == 0 {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "No paths provided"})
	}

	for _, pth := range req.Paths {
		srcFull, err := h.p.Store.SafeFilePath(id, pth)
		if err != nil {
			continue
		}

		newRel := req.Dest + "/" + filepath.Base(pth)
		if req.Dest == "" {
			newRel = filepath.Base(pth)
		}
		dstFull, err := h.p.Store.SafeFilePath(id, newRel)
		if err != nil {
			continue
		}
		
		_ = copyRecursively(srcFull, dstFull)
	}

	return c.JSON(fiber.Map{"ok": true, "message": "Copied successfully"})
}

func (h *Handler) BrowseMkdir(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := h.p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).JSON(fiber.Map{"ok": false, "message": "not found"})
	}
	if h.p.IsGitApp(c.UserContext(), id) {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "mkdir disabled for git apps"})
	}

	var req struct {
		Path string `json:"path" form:"path"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "invalid request format"})
	}

	if req.Path == "" {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "Path required"})
	}

	fullPath, err := h.p.Store.SafeFilePath(id, req.Path)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "Invalid path"})
	}

	if err := os.MkdirAll(fullPath, 0750); err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "message": err.Error()})
	}

	return c.JSON(fiber.Map{"ok": true, "message": "Directory created"})
}

func (h *Handler) BrowseZip(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := h.p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).JSON(fiber.Map{"ok": false, "message": "not found"})
	}
	if h.p.IsGitApp(c.UserContext(), id) {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "zip disabled for git apps"})
	}

	var req struct {
		Paths []string `json:"paths" form:"paths"`
		Name  string   `json:"name" form:"name"`
		Dest  string   `json:"dest" form:"dest"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "invalid request format"})
	}

	if len(req.Paths) == 0 || req.Name == "" {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "Paths and archive name required"})
	}
	if !strings.HasSuffix(req.Name, ".zip") {
		req.Name += ".zip"
	}

	destRel := req.Dest + "/" + req.Name
	if req.Dest == "" {
		destRel = req.Name
	}
	zipFull, err := h.p.Store.SafeFilePath(id, destRel)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "Invalid destination path"})
	}

	outFile, err := os.Create(zipFull)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "message": "Failed to create archive"})
	}
	defer outFile.Close()

	zw := zip.NewWriter(outFile)
	defer zw.Close()

	for _, pth := range req.Paths {
		srcFull, err := h.p.Store.SafeFilePath(id, pth)
		if err != nil {
			continue
		}
		
		_ = filepath.Walk(srcFull, func(path string, info os.FileInfo, err error) error {
			if err != nil { return nil }
			rel, _ := filepath.Rel(filepath.Dir(srcFull), path)
			if rel == "" || rel == "." { return nil }

			header, _ := zip.FileInfoHeader(info)
			header.Name = filepath.ToSlash(rel)
			if info.IsDir() {
				header.Name += "/"
				header.Method = zip.Store
			} else {
				header.Method = zip.Deflate
			}

			writer, err := zw.CreateHeader(header)
			if err != nil { return nil }

			if !info.IsDir() {
				file, err := os.Open(path)
				if err != nil { return nil }
				defer file.Close()
				_, _ = io.Copy(writer, file)
			}
			return nil
		})
	}

	return c.JSON(fiber.Map{"ok": true, "message": "Compressed successfully"})
}

func (h *Handler) BrowseUnzip(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := h.p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).JSON(fiber.Map{"ok": false, "message": "not found"})
	}
	if h.p.IsGitApp(c.UserContext(), id) {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "unzip disabled for git apps"})
	}

	var req struct {
		Path string `json:"path" form:"path"`
		Dest string `json:"dest" form:"dest"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "invalid request format"})
	}

	zipFull, err := h.p.Store.SafeFilePath(id, req.Path)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "Invalid zip path"})
	}

	f, err := os.Open(zipFull)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "message": "Failed to open zip archive"})
	}
	defer f.Close()

	stat, _ := f.Stat()
	if err := h.p.Store.ExtractZip(id, f, stat.Size()); err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "message": "Extract failed: " + err.Error()})
	}

	return c.JSON(fiber.Map{"ok": true, "message": "Extracted successfully"})
}

func copyRecursively(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			srcPath := filepath.Join(src, entry.Name())
			dstPath := filepath.Join(dst, entry.Name())
			if err := copyRecursively(srcPath, dstPath); err != nil {
				return err
			}
		}
		return nil
	}

	return copyFile(src, dst, info.Mode())
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
