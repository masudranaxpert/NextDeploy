package handlers

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/caddy"
	"panel/internal/workspace"

	"github.com/gofiber/fiber/v2"
)

const maxURLDownloadBytes = 512 << 20

type urlLimitReporter struct {
	max int64
}

func (l *urlLimitReporter) Header() http.Header       { return http.Header{} }
func (l *urlLimitReporter) Write([]byte) (int, error) { return 0, fmt.Errorf("response exceeds size limit (%d bytes)", l.max) }
func (l *urlLimitReporter) WriteHeader(int)           {}

// isDisallowedRemoteIP returns true if ip is loopback, private RFC1918, link-local / cloud metadata,
// CGNAT (100.64.0.0/10), multicast, unspecified, or unique-local IPv6.
func isDisallowedRemoteIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		// 100.64.0.0/10 (Carrier-Grade NAT / Tailscale / internal overlay)
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return true
		}
		// 169.254.0.0/16 (AWS/GCP/Azure Cloud Instance Metadata)
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
		// 0.0.0.0/8
		if ip4[0] == 0 {
			return true
		}
	}
	return false
}

func validateRemoteURLTarget(u *url.URL) error {
	if u == nil {
		return errors.New("invalid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("only http and https URLs are allowed")
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if host == "" {
		return fmt.Errorf("missing host")
	}
	if host == "localhost" || host == "caddy" || host == "panel" ||
		strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") ||
		strings.HasSuffix(host, ".docker.internal") {
		return fmt.Errorf("internal or local hostnames are not allowed")
	}
	if ip := net.ParseIP(host); ip != nil && isDisallowedRemoteIP(ip) {
		return fmt.Errorf("private, loopback, or metadata IP addresses are not allowed")
	}
	return nil
}

func fetchRemoteURL(ctx context.Context, rawURL string) (*http.Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if err := validateRemoteURLTarget(u); err != nil {
		return nil, err
	}

	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 15 * time.Second,
	}

	transport := &http.Transport{
		Proxy: nil, // Do not allow environment proxy bypass into internal networks
		DialContext: func(dialCtx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(dialCtx, host)
			if err != nil {
				return nil, err
			}
			if len(ips) == 0 {
				return nil, fmt.Errorf("could not resolve host %s", host)
			}
			for _, ipAddr := range ips {
				if isDisallowedRemoteIP(ipAddr.IP) {
					return nil, fmt.Errorf("blocked connection to private/reserved IP (%s)", ipAddr.IP.String())
				}
			}
			// Connect specifically to the validated public IP to prevent DNS rebinding
			return dialer.DialContext(dialCtx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
	}

	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return validateRemoteURLTarget(req.URL)
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	resp.Body = http.MaxBytesReader(&urlLimitReporter{max: maxURLDownloadBytes}, resp.Body, maxURLDownloadBytes)
	return resp, nil
}

func (p *Panel) BrowseUrlUpload(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).JSON(fiber.Map{"ok": false, "message": "not found"})
	}
	if p.IsGitApp(c.UserContext(), id) {
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

	resp, err := fetchRemoteURL(c.UserContext(), req.URL)
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
	if isGeneratedComposeRel(relPath) {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": generatedComposeManagedMsg()})
	}

	var incoming int64
	if resp.ContentLength > 0 {
		incoming = resp.ContentLength
	}
	if err := p.CheckStorageQuota(c.UserContext(), id, incoming); err != nil {
		return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{"ok": false, "message": err.Error()})
	}

	if _, err := p.Store.SaveUploadedFile(id, relPath, resp.Body); err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "message": fmt.Sprintf("Failed to save file: %v", err)})
	}
	p.InvalidateAfterAppWorkspaceChange(id)

	return c.JSON(fiber.Map{"ok": true, "message": "Downloaded to server."})
}

func (p *Panel) BrowseUpload(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).JSON(fiber.Map{"ok": false, "message": "not found"})
	}
	if p.IsGitApp(c.UserContext(), id) {
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

	var incoming int64
	for _, file := range files {
		incoming += file.Size
	}
	if err := p.CheckStorageQuota(c.UserContext(), id, incoming); err != nil {
		return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{"ok": false, "message": err.Error()})
	}

	for _, file := range files {
		relPath := file.Filename
		if destDir != "" {
			relPath = destDir + "/" + file.Filename
		}
		if isGeneratedComposeRel(relPath) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "message": generatedComposeManagedMsg()})
		}

		f, err := file.Open()
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"ok": false, "message": fmt.Sprintf("Failed to open %s: %v", file.Filename, err)})
		}
		_, saveErr := p.Store.SaveUploadedFile(id, relPath, f)
		_ = f.Close()
		if saveErr != nil {
			return c.Status(500).JSON(fiber.Map{"ok": false, "message": fmt.Sprintf("Failed to save %s: %v", file.Filename, saveErr)})
		}
	}
	p.InvalidateAfterAppWorkspaceChange(id)

	return c.JSON(fiber.Map{"ok": true, "message": "Files uploaded successfully"})
}

func (p *Panel) BrowseMove(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).JSON(fiber.Map{"ok": false, "message": "not found"})
	}
	if p.IsGitApp(c.UserContext(), id) {
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
		if isGeneratedComposeRel(pth) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "message": generatedComposeManagedMsg()})
		}
		oldFull, err := p.Store.SafeFilePath(id, pth)
		if err != nil {
			continue
		}

		newRel := req.Dest + "/" + filepath.Base(pth)
		if req.Dest == "" {
			newRel = filepath.Base(pth)
		}
		if isGeneratedComposeRel(newRel) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "message": generatedComposeManagedMsg()})
		}
		newFull, err := p.Store.SafeFilePath(id, newRel)
		if err != nil {
			continue
		}

		_ = os.MkdirAll(filepath.Dir(newFull), 0750)
		_ = os.Rename(oldFull, newFull)
	}

	p.InvalidateAfterAppWorkspaceChange(id)
	return c.JSON(fiber.Map{"ok": true, "message": "Moved successfully"})
}

func (p *Panel) BrowseCopy(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).JSON(fiber.Map{"ok": false, "message": "not found"})
	}
	if p.IsGitApp(c.UserContext(), id) {
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

	// Calculate total size first and reject symlinks before copying
	var totalBytes int64
	for _, pth := range req.Paths {
		srcFull, err := p.Store.SafeFilePath(id, pth)
		if err != nil {
			continue
		}
		sz, err := dirSizeWithoutSymlinks(srcFull)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"ok": false, "message": err.Error()})
		}
		totalBytes += sz
	}
	if err := p.CheckStorageQuota(c.UserContext(), id, totalBytes); err != nil {
		return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{"ok": false, "message": err.Error()})
	}

	for _, pth := range req.Paths {
		if isGeneratedComposeRel(pth) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "message": generatedComposeManagedMsg()})
		}
		srcFull, err := p.Store.SafeFilePath(id, pth)
		if err != nil {
			continue
		}

		newRel := req.Dest + "/" + filepath.Base(pth)
		if req.Dest == "" {
			newRel = filepath.Base(pth)
		}
		if isGeneratedComposeRel(newRel) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "message": generatedComposeManagedMsg()})
		}
		dstFull, err := p.Store.SafeFilePath(id, newRel)
		if err != nil {
			continue
		}
		// Prevent copying a directory into itself or its own descendant
		cleanSrc := filepath.Clean(srcFull)
		cleanDst := filepath.Clean(dstFull)
		if cleanSrc == cleanDst || strings.HasPrefix(cleanDst, cleanSrc+string(filepath.Separator)) {
			return c.Status(400).JSON(fiber.Map{"ok": false, "message": "Cannot copy a directory into itself"})
		}

		if err := copyRecursively(srcFull, dstFull); err != nil {
			return c.Status(500).JSON(fiber.Map{"ok": false, "message": err.Error()})
		}
	}

	p.InvalidateAfterAppWorkspaceChange(id)
	return c.JSON(fiber.Map{"ok": true, "message": "Copied successfully"})
}

func (p *Panel) BrowseMkdir(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).JSON(fiber.Map{"ok": false, "message": "not found"})
	}
	if p.IsGitApp(c.UserContext(), id) {
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

	fullPath, err := p.Store.SafeFilePath(id, req.Path)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "Invalid path"})
	}

	if err := os.MkdirAll(fullPath, 0750); err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "message": err.Error()})
	}

	p.InvalidateAfterAppWorkspaceChange(id)
	return c.JSON(fiber.Map{"ok": true, "message": "Directory created"})
}

func (p *Panel) BrowseZip(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).JSON(fiber.Map{"ok": false, "message": "not found"})
	}
	if p.IsGitApp(c.UserContext(), id) {
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
	req.Name = filepath.Base(strings.TrimSpace(req.Name))
	if !strings.HasSuffix(req.Name, ".zip") {
		req.Name += ".zip"
	}

	destRel := req.Dest + "/" + req.Name
	if req.Dest == "" {
		destRel = req.Name
	}
	zipFull, err := p.Store.SafeFilePath(id, destRel)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "Invalid destination path"})
	}

	// Pre-check total uncompressed size and quota
	var estBytes int64
	for _, pth := range req.Paths {
		srcFull, err := p.Store.SafeFilePath(id, pth)
		if err != nil {
			continue
		}
		sz, err := dirSizeWithoutSymlinks(srcFull)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"ok": false, "message": err.Error()})
		}
		estBytes += sz
	}
	if estBytes > maxWorkspaceZipBytes {
		return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{"ok": false, "message": "Selected files exceed maximum archive size (512 MB)"})
	}
	if err := p.CheckStorageQuota(c.UserContext(), id, estBytes); err != nil {
		return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{"ok": false, "message": err.Error()})
	}

	outFile, err := os.Create(zipFull)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "message": "Failed to create archive"})
	}
	defer outFile.Close()

	zw := zip.NewWriter(outFile)
	defer zw.Close()

	var written int64
	for _, pth := range req.Paths {
		srcFull, err := p.Store.SafeFilePath(id, pth)
		if err != nil {
			continue
		}
		cleanZip := filepath.Clean(zipFull)

		_ = filepath.WalkDir(srcFull, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if filepath.Clean(path) == cleanZip {
				return nil
			}
			name := d.Name()
			if name == ".panel-meta" || name == workspace.ReservedDir || name == caddy.GeneratedCompose {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil // Never follow or archive symlinks
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(filepath.Dir(srcFull), path)
			if rel == "" || rel == "." {
				return nil
			}

			header, _ := zip.FileInfoHeader(info)
			header.Name = filepath.ToSlash(rel)
			if info.IsDir() {
				header.Name += "/"
				header.Method = zip.Store
			} else {
				header.Method = zip.Deflate
			}

			writer, err := zw.CreateHeader(header)
			if err != nil {
				return nil
			}

			if !info.IsDir() {
				file, err := os.Open(path)
				if err != nil {
					return nil
				}
				defer file.Close()
				n, _ := io.Copy(writer, io.LimitReader(file, maxWorkspaceZipBytes-written))
				written += n
			}
			return nil
		})
	}

	p.InvalidateAfterAppWorkspaceChange(id)
	return c.JSON(fiber.Map{"ok": true, "message": "Compressed successfully"})
}

func (p *Panel) BrowseUnzip(c *fiber.Ctx) error {
	id := c.Params("id")
	if _, err := p.DB.GetApp(c.UserContext(), id); err != nil {
		return c.Status(404).JSON(fiber.Map{"ok": false, "message": "not found"})
	}
	if p.IsGitApp(c.UserContext(), id) {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "unzip disabled for git apps"})
	}

	var req struct {
		Path string `json:"path" form:"path"`
		Dest string `json:"dest" form:"dest"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "invalid request format"})
	}

	zipFull, err := p.Store.SafeFilePath(id, req.Path)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"ok": false, "message": "Invalid zip path"})
	}

	f, err := os.Open(zipFull)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "message": "Failed to open zip archive"})
	}
	defer f.Close()

	stat, _ := f.Stat()
	if stat != nil {
		uncompressed, uerr := workspace.DeclaredUncompressedZipBytes(f, stat.Size())
		if uerr != nil {
			return c.Status(400).JSON(fiber.Map{"ok": false, "message": uerr.Error()})
		}
		if err := p.CheckStorageQuota(c.UserContext(), id, uncompressed); err != nil {
			return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{"ok": false, "message": err.Error()})
		}
	}
	if err := p.Store.ExtractZip(id, f, stat.Size()); err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "message": "Extract failed: " + err.Error()})
	}
	p.InvalidateAfterAppWorkspaceChange(id)

	return c.JSON(fiber.Map{"ok": true, "message": "Extracted successfully"})
}

func dirSizeWithoutSymlinks(root string) (int64, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return 0, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return 0, errors.New("symlinks are not permitted in workspace operations")
	}
	if !info.IsDir() {
		return info.Size(), nil
	}
	var total int64
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("symlinks are not permitted in workspace operations")
		}
		if !d.IsDir() {
			fi, err := d.Info()
			if err == nil {
				total += fi.Size()
			}
		}
		return nil
	})
	return total, err
}

func copyRecursively(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("symlinks are not permitted")
	}

	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			srcPath := filepath.Join(src, entry.Name())
			dstPath := filepath.Join(dst, entry.Name())
			if err := copyRecursively(srcPath, dstPath); err != nil {
				return err
			}
		}
		return nil
	}

	return copyFile(src, dst, info.Mode().Perm())
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
