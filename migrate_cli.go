package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/db"
	"panel/internal/handlers"
	"panel/internal/migrate"
	"panel/internal/workspace"
)

func runMigrateCLI(args []string) {
	if len(args) < 1 || strings.TrimSpace(args[0]) != "import" {
		fmt.Fprintln(os.Stderr, "usage: panel migrate import <bundle.nd-migrate> [--delete-after] [--no-deploy]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	deleteAfter := fs.Bool("delete-after", false, "remove bundle file after successful import")
	noDeploy := fs.Bool("no-deploy", false, "skip compose up after import")
	_ = fs.Parse(args[1:])
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: panel migrate import <bundle.nd-migrate> [--delete-after] [--no-deploy]")
		os.Exit(2)
	}
	bundlePath := strings.TrimSpace(fs.Arg(0))
	if bundlePath == "" {
		log.Fatal("bundle path required")
	}

	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "./data"
	}
	dbPath := filepath.Join(dataDir, "panel.db")
	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()

	root := os.Getenv("WORKSPACES_ROOT")
	if root == "" {
		root = filepath.Join(dataDir, "workspaces")
	}
	store := workspace.NewStore(root)
	panel := &handlers.Panel{DB: database, Store: store, WorkspacesRoot: root}

	adminID := int64(0)
	users, err := database.ListUsers(context.Background())
	if err == nil {
		for _, u := range users {
			if u.Role == db.RoleAdmin {
				adminID = u.ID
				break
			}
		}
	}
	if adminID == 0 {
		log.Fatal("no admin user found; complete setup first")
	}

	_ = os.MkdirAll(migrate.IncomingDir(), 0700)
	deps := panel.MigrateImportDeps(adminID)
	deps.DeployAfterImport = !*noDeploy
	deps.OnProgress = func(msg string) {
		log.Println(msg)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()
	if err := migrate.RunImport(ctx, bundlePath, *deleteAfter, deps); err != nil {
		log.Fatal(err)
	}
	log.Println("migration import finished successfully")
}
