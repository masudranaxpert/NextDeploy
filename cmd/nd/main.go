// nd — NextDeploy CLI
// Usage: nd <command> [args]
package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"time"

	"nd/cmd"
	"nd/internal/client"
	"nd/internal/config"
)

// version is set at build time via -ldflags "-X main.version=1.2.3"
var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	args := os.Args[1:]
	if len(args) == 0 {
		cmd.PrintHelp()
		return 0
	}

	command := args[0]
	rest := args[1:]

	// Commands that don't need auth
	switch command {
	case "login":
		if err := cmd.RunLogin(rest); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		return 0
	case "logout":
		if err := cmd.RunLogout(rest); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		return 0
	case "version", "--version", "-v":
		fmt.Printf("nd version %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
		return 0
	case "help", "--help", "-h":
		cmd.PrintHelp()
		return 0
	}

	// All other commands require a saved config or env variables
	cfg, err := config.Load()
	if err != nil || cfg.ServerURL == "" || cfg.Token == "" {
		fmt.Fprintln(os.Stderr, "Not logged in. Run: nd login <server_url> or set ND_SERVER_URL and ND_TOKEN")
		return 1
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

	// Handle graceful termination on Ctrl+C (SIGINT)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	go func() {
		<-sigChan
		cl.Disconnect()
		os.Exit(130)
	}()

	switch command {
	case "whoami":
		err = cmd.RunWhoami(cl, cfg)
	case "apps", "list":
		err = cmd.RunApps(cl, rest)
	case "link":
		err = cmd.RunLink(cl, rest)
	case "unlink":
		err = cmd.RunUnlink(cl, rest)
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
	case "exec", "run":
		err = cmd.RunExec(cl, rest)
	case "server-exec":
		err = cmd.RunServerExec(cl, rest)
	case "env":
		err = cmd.RunEnv(cl, rest)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		cmd.PrintHelp()
		return 1
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	return 0
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
