// nd — NextDeploy CLI
// Usage: nd <command> [args]
package main

import (
	"fmt"
	"os"
	"runtime"
	"time"

	"nd/cmd"
	"nd/internal/client"
	"nd/internal/config"

	"crypto/rand"
	"encoding/hex"
)

// version is set at build time via -ldflags "-X main.version=1.2.3"
var version = "dev"

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		cmd.PrintHelp()
		os.Exit(0)
	}

	command := args[0]
	rest := args[1:]

	// Commands that don't need auth
	switch command {
	case "login":
		if err := cmd.RunLogin(rest); err != nil {
			fatalf("%v", err)
		}
		return
	case "logout":
		if err := cmd.RunLogout(rest); err != nil {
			fatalf("%v", err)
		}
		return
	case "version", "--version", "-v":
		fmt.Printf("nd version %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
		return
	case "help", "--help", "-h":
		cmd.PrintHelp()
		return
	}

	// All other commands require a saved config
	cfg, err := config.Load()
	if err != nil || cfg.ServerURL == "" || cfg.Token == "" {
		fmt.Fprintln(os.Stderr, "Not logged in. Run: nd login <server_url>")
		os.Exit(1)
	}

	// Generate a stable session ID for this process lifetime
	sessID := processSessionID()
	cl := client.New(cfg, sessID)

	// Register/refresh CLI session and start heartbeat goroutine
	hostname, _ := os.Hostname()
	cl.Heartbeat(hostname, runtime.GOOS, runtime.GOARCH, version)
	go heartbeatLoop(cl, hostname, version)

	// Disconnect cleanly on exit
	defer cl.Disconnect()

	switch command {
	case "apps", "list":
		err = cmd.RunApps(cl, rest)
	case "status":
		err = cmd.RunStatus(cl, rest)
	case "push":
		err = cmd.RunPush(cl, rest)
	case "deploy":
		err = cmd.RunDeploy(cl, rest)
	case "stop":
		err = cmd.RunStop(cl, rest)
	case "restart":
		err = cmd.RunRestart(cl, rest)
	case "logs":
		err = cmd.RunLogs(cl, rest)
	case "env":
		err = cmd.RunEnv(cl, rest)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		cmd.PrintHelp()
		os.Exit(1)
	}

	if err != nil {
		fatalf("%v", err)
	}
}

func fatalf(format string, v ...any) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", v...)
	os.Exit(1)
}

// processSessionID generates a random session ID stable for this process run.
var _sessID string

func processSessionID() string {
	if _sessID != "" {
		return _sessID
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	_sessID = hex.EncodeToString(b)
	return _sessID
}

// heartbeatLoop sends a heartbeat every 2 minutes while the CLI is running.
func heartbeatLoop(cl *client.Client, hostname, ver string) {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		cl.Heartbeat(hostname, runtime.GOOS, runtime.GOARCH, ver)
	}
}
