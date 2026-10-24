package handlers

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"panel/internal/volumex"
	"panel/internal/workspace"

	"github.com/gofiber/fiber/v2"
)

type volRow struct {
	Name    string `json:"name"`
	IsDir   bool   `json:"is_dir"`
	RelPath string `json:"rel_path"`
}

func (p *Panel) VolumeBrowse(c *fiber.Ctx) error {
	if !WantsJSON(c) && c.Query("format") != "json" {
		return p.ServeSPA(c)
	}
	name := strings.TrimSpace(c.Query("name"))
	if !volumex.ValidVolumeName(name) {
		return c.Status(400).JSON(fiber.Map{"ok": false, "error": "invalid volume"})
	}
	allowed, err := p.isVolumeAccessAllowed(c, name)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"ok": false, "error": err.Error()})
	}
	if !allowed {
		return c.Status(403).JSON(fiber.Map{"ok": false, "error": "forbidden"})
	}
	rel := c.Query("path", "")

	entries, msg := volumex.ListDir(c.UserContext(), name, rel)
	parent := workspace.ParentRel(rel)
	rows := make([]volRow, 0, len(entries))
	for _, e := range entries {
		rp := e.Name
		if rel != "" {
			rp = rel + "/" + e.Name
		}
		rows = append(rows, volRow{Name: e.Name, IsDir: e.IsDir, RelPath: rp})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].IsDir != rows[j].IsDir {
			return rows[i].IsDir
		}
		return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
	})
	return c.JSON(fiber.Map{
		"ok":          msg == "",
		"volume":      name,
		"path":        rel,
		"parent_path": parent,
		"entries":     rows,
		"error":       msg,
	})
}

func (p *Panel) VolumeDownload(c *fiber.Ctx) error {
	name := strings.TrimSpace(c.Query("name"))
	if !volumex.ValidVolumeName(name) {
		return c.Status(400).SendString("invalid volume")
	}
	allowed, err := p.isVolumeWriteAllowed(c, name)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	if !allowed {
		return c.Status(403).SendString("forbidden")
	}

	ctx := c.UserContext()
	tmpPath, err := volumex.BackupToTemp(ctx, name)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}
	defer os.Remove(tmpPath)

	safe := strings.ReplaceAll(name, `"`, "")
	c.Set("Content-Type", "application/gzip")
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-backup.tar.gz"`, safe))
	return c.SendFile(tmpPath)
}
