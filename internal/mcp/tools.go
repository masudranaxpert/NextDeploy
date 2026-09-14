package mcp

// AllTools returns the full list of tools supported by the NextDeploy MCP server.
func AllTools() []Tool {
	return []Tool{
		{
			Name:        "app_list",
			Description: "List all NextDeploy applications with status, dev mode state, domains, and services",
			InputSchema: ToolInputSchema{
				Type:       "object",
				Properties: map[string]ToolProperty{},
			},
		},
		{
			Name:        "app_get",
			Description: "Get detailed information, domains, and compose services for a specific application",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id": {Type: "string", Description: "The application ID"},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name:        "app_create",
			Description: "Create a new NextDeploy application directly via MCP. Allows full automated project provisioning without needing the web UI.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"name":            {Type: "string", Description: "Name or slug for the new application"},
					"source_type":     {Type: "string", Description: "Optional source type: 'upload' (default) or 'git'"},
					"repo_url":        {Type: "string", Description: "Optional Git repository URL (when source_type is 'git')"},
					"branch":          {Type: "string", Description: "Optional Git branch name (default 'main')"},
					"compose_content": {Type: "string", Description: "Optional initial docker-compose.yml content to seed into workspace"},
				},
				Required: []string{"name"},
			},
		},
		{
			Name: "workspace_manifest",
			Description: "Generate a compact manifest of all files in an application workspace with relative path, size, modification timestamp, and SHA-256 hash. " +
				"Filters out heavy/irrelevant paths (node_modules, .git, vendor, .venv, .nextdeploy) and respects workspace .gitignore. " +
				"Caches hashes by (path, size, mtime) for high performance. Pass hash:false for ultra-fast size+mtime scanning without hashing.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id":      {Type: "string", Description: "The application ID"},
					"path":        {Type: "string", Description: "Optional subdirectory inside workspace to scope the manifest"},
					"hash":        {Type: "boolean", Description: "Whether to compute sha256 hash for files (default true). Set false for fastest size+mtime scan."},
					"exclude":     {Type: "array", Description: "Optional list of additional glob patterns to exclude (e.g. ['*.log', 'dist/*'])"},
					"max_entries": {Type: "integer", Description: "Maximum entries to return (default 5000, max 20000). Sets truncated:true if exceeded."},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name:        "file_list",
			Description: "List files and directories within an application's workspace (strictly restricted to workspace)",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id":    {Type: "string", Description: "The application ID"},
					"path":      {Type: "string", Description: "Relative path inside workspace (default empty for root)"},
					"recursive": {Type: "boolean", Description: "If true, recursively lists all files and directories under path (default false)"},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name:        "file_read",
			Description: "Read the contents of a file inside the application workspace",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id": {Type: "string", Description: "The application ID"},
					"path":   {Type: "string", Description: "Relative file path inside workspace"},
					"offset": {Type: "integer", Description: "Optional 1-based line number to start reading from (default 1)"},
					"limit":  {Type: "integer", Description: "Optional maximum number of lines to read (default 0 reads to end of file)"},
				},
				Required: []string{"app_id", "path"},
			},
		},
		{
			Name: "file_write",
			Description: "Create or overwrite a file in the workspace. Validates compose files and guards against path traversal. " +
				"IMPORTANT for Git-connected apps: written files are preserved on next deploy — the deploy tool auto-detects local edits and skips git pull to protect them.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id":  {Type: "string", Description: "The application ID"},
					"path":    {Type: "string", Description: "Relative file path inside workspace"},
					"content": {Type: "string", Description: "Text content of the file"},
				},
				Required: []string{"app_id", "path", "content"},
			},
		},
		{
			Name:        "file_delete",
			Description: "Delete a file or folder inside the workspace (cannot delete system or meta files)",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id": {Type: "string", Description: "The application ID"},
					"path":   {Type: "string", Description: "Relative path to file or folder"},
				},
				Required: []string{"app_id", "path"},
			},
		},
		{
			Name:        "env_list",
			Description: "List environment variable keys (without revealing sensitive values) configured for an application",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id": {Type: "string", Description: "The application ID"},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name:        "env_reveal",
			Description: "Reveal sensitive environment variable values for an application. Requires explicit 'Allow env_reveal' permission enabled for the API token in NextDeploy Panel.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id": {Type: "string", Description: "The application ID"},
					"keys": {
						Type:        "array",
						Description: "Optional list of specific environment variable keys to reveal. If omitted, all keys are revealed.",
					},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name:        "env_set",
			Description: "Set or update an environment variable for an application and synchronize workspace .env",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id": {Type: "string", Description: "The application ID"},
					"key":    {Type: "string", Description: "Environment variable key name"},
					"value":  {Type: "string", Description: "Environment variable value"},
				},
				Required: []string{"app_id", "key", "value"},
			},
		},
		{
			Name:        "env_set_batch",
			Description: "Set or update multiple environment variables in a single operation and synchronize workspace .env and Caddy configuration once.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id": {Type: "string", Description: "The application ID"},
					"variables": {
						Type:        "object",
						Description: "Key-value map of environment variables (e.g. {\"PORT\": \"3000\", \"NODE_ENV\": \"production\"})",
					},
				},
				Required: []string{"app_id", "variables"},
			},
		},
		{
			Name:        "compose_get",
			Description: "Get the effective docker-compose.yml configuration for an application",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id": {Type: "string", Description: "The application ID"},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name: "deploy",
			Description: "Trigger an asynchronous deployment (docker compose up). " +
				"For Git-connected apps (Dev Mode off): automatically checks workspace state first — if workspace has local edits (dirty), git pull is SKIPPED to protect your files; if workspace is clean, latest code is pulled from Git. " +
				"Pass git_pull:true to force-pull from remote regardless (WARNING: discards all local workspace edits). " +
				"Returns a job_id immediately so you can poll deploy_status without timing out.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id":   {Type: "string", Description: "The application ID"},
					"git_pull": {Type: "boolean", Description: "Optional. If true, force-pulls from Git remote before deploying (discards local workspace edits). Default: auto-detect via dirty-check."},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name: "redeploy",
			Description: "Trigger an asynchronous full redeployment with image pull and rebuild. " +
				"Same Git-sync behavior as deploy: dirty workspace skips git pull (protects local edits); clean workspace auto-pulls. " +
				"Pass git_pull:true to force-pull from remote (discards local edits). Returns a job_id for polling.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id":   {Type: "string", Description: "The application ID"},
					"git_pull": {Type: "boolean", Description: "Optional. If true, force-pulls from Git remote before rebuilding (discards local workspace edits). Default: auto-detect via dirty-check."},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name:        "restart",
			Description: "Restart all containers or a specific service container for an application",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id":  {Type: "string", Description: "The application ID"},
					"service": {Type: "string", Description: "Optional service name to restart only that container"},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name:        "stop",
			Description: "Stop the application container stack (docker compose down)",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id": {Type: "string", Description: "The application ID"},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name:        "deploy_status",
			Description: "Check the status and log output of a deployment job",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"job_id": {Type: "string", Description: "The job ID returned from deploy, redeploy, restart, or stop"},
					"app_id": {Type: "string", Description: "Optional fallback app ID to check current or latest deploy run"},
				},
			},
		},
		{
			Name:        "container_logs",
			Description: "Fetch recent logs from an application container or specific compose service",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id":  {Type: "string", Description: "The application ID"},
					"service": {Type: "string", Description: "Optional service or container name (defaults to primary)"},
					"tail":    {Type: "integer", Description: "Number of lines to tail (default 100)"},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name:        "deploy_log_tail",
			Description: "Fetch deployment history and logs for an application (or live logs if deploy is ongoing). Supports streaming via long-polling: pass job_id, since_offset, and wait_seconds (e.g. 20) to receive new log output as soon as it arrives, or return empty when timeout elapses.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id":       {Type: "string", Description: "Optional application ID"},
					"job_id":       {Type: "string", Description: "Optional deployment job ID (returned from deploy or redeploy)"},
					"since_offset": {Type: "integer", Description: "Byte offset in log output to read from (default 0). Pass next_offset from previous call for continuous streaming."},
					"wait_seconds": {Type: "integer", Description: "Seconds to wait for new log output if none currently available (long-polling, default 0, max 30)."},
					"limit":        {Type: "integer", Description: "Number of historical deploy logs to fetch when falling back to app history (default 5)"},
				},
			},
		},
		{
			Name:        "dev_mode_set",
			Description: "Configure or toggle Development Mode (workspace bind mounting and live reload)",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id":  {Type: "string", Description: "The application ID"},
					"enabled": {Type: "boolean", Description: "Whether dev mode is enabled"},
					"service": {Type: "string", Description: "Target service name to mount workspace into"},
					"target":  {Type: "string", Description: "Absolute target path inside container (e.g. /app)"},
					"command": {Type: "string", Description: "Optional custom start command override for dev"},
				},
				Required: []string{"app_id", "enabled"},
			},
		},
		{
			Name:        "reset_dev_deps",
			Description: "Reset development dependency volumes (nddev_*) and recreate containers with fresh packages with zero database downtime",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id": {Type: "string", Description: "The application ID"},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name:        "container_exec",
			Description: "Execute a shell command inside an application container (or specific compose service). Requires explicit 'Allow container_exec' token permission.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id":          {Type: "string", Description: "The application ID"},
					"command":         {Type: "string", Description: "Shell command to execute inside the container"},
					"service":         {Type: "string", Description: "Optional compose service name or container name (defaults to primary running container)"},
					"work_dir":        {Type: "string", Description: "Optional working directory inside the container"},
					"timeout_seconds": {Type: "integer", Description: "Command timeout in seconds (default 60, max 300)"},
				},
				Required: []string{"app_id", "command"},
			},
		},
		{
			Name:        "server_exec",
			Description: "Execute a shell command on the NextDeploy host server (strictly restricted to Admin role and 'Allow server_exec' token permission)",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"command":         {Type: "string", Description: "Shell command to execute on the server"},
					"timeout_seconds": {Type: "integer", Description: "Command timeout in seconds (default 60, max 300)"},
				},
				Required: []string{"command"},
			},
		},
		{
			Name:        "git_pull",
			Description: "Pull latest changes from the configured Git repository for an application without deploying. Supports switching branches. Safe by default: refuses to pull if workspace has uncommitted local edits unless force:true is passed.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id": {Type: "string", Description: "The application ID"},
					"branch": {Type: "string", Description: "Optional branch name to switch to and pull. If omitted, pulls current configured branch."},
					"force":  {Type: "boolean", Description: "If true, discards uncommitted local workspace edits and force-pulls. Default false (protects local files)."},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name: "file_write_batch",
			Description: "Write or update multiple files in the application workspace in a single batch operation. Avoids multiple round trips. " +
				"IMPORTANT for Git-connected apps: written files are preserved on next deploy — the deploy tool auto-detects local edits and skips git pull to protect them.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id": {Type: "string", Description: "The application ID"},
					"files":  {Type: "array", Description: "Array of file objects, each containing 'path' (relative file path) and 'content' (file text content)"},
				},
				Required: []string{"app_id", "files"},
			},
		},
		{
			Name: "workspace_apply",
			Description: "Atomically apply a batch of file writes and deletes to an application workspace in a single round-trip. " +
				"All paths are validated beforehand against path traversal and forbidden system files. " +
				"Supports dry_run:true to preview what would change without modifying files.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id": {Type: "string", Description: "The application ID"},
					"writes": {
						Type:        "array",
						Description: "Optional list of file writes, each object with 'path' (relative file path) and 'content' (text content)",
					},
					"deletes": {
						Type:        "array",
						Description: "Optional list of relative file or directory paths to delete",
					},
					"dry_run": {Type: "boolean", Description: "If true, simulates validation and returns planned actions without modifying any files (default false)"},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name: "deploy_and_wait",
			Description: "Trigger an application deployment (docker compose up) and wait synchronously for completion, returning the final job status, duration, and tail logs. " +
				"Best suited for short builds (up to 300s). For long-running builds or continuous progress streaming, use deploy to obtain a job_id, followed by deploy_log_tail with wait_seconds (e.g. 20s) long-polling. " +
				"Same Git-sync behavior as deploy: dirty workspace skips git pull; clean workspace auto-pulls. " +
				"Pass git_pull:true to force-pull from remote (discards local edits). Max timeout 300s.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id":          {Type: "string", Description: "The application ID"},
					"git_pull":        {Type: "boolean", Description: "Optional. If true, force-pulls from Git remote before deploying (discards local workspace edits). Default: auto-detect via dirty-check."},
					"timeout_seconds": {Type: "integer", Description: "Maximum time to wait in seconds (default 180, max 300)"},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name:        "file_patch",
			Description: "Apply a unified diff patch to the application workspace using git apply. Automatically handles line recount, context, and whitespace.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id": {Type: "string", Description: "The application ID"},
					"patch":  {Type: "string", Description: "Unified diff patch content (output of git diff or standard unidiff)"},
				},
				Required: []string{"app_id", "patch"},
			},
		},
		{
			Name:        "app_health_check",
			Description: "Perform an end-to-end health check of an application, verifying container running states and making HTTP health probes against configured domains",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id": {Type: "string", Description: "The application ID"},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name:        "file_search",
			Description: "Fast workspace search. Find files matching a glob pattern (e.g. *.go, *.env) or search line-by-line for text content (grep). Automatically skips .git, node_modules, and binary files for maximum speed.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id":      {Type: "string", Description: "The application ID"},
					"query":       {Type: "string", Description: "Optional text to search for inside files (case-insensitive grep). If omitted, only matching filenames are returned."},
					"pattern":     {Type: "string", Description: "Optional filename or glob pattern to filter files (e.g. *.go, *.json, Dockerfile*). Defaults to *."},
					"path":        {Type: "string", Description: "Optional subdirectory inside workspace to scope the search"},
					"max_results": {Type: "integer", Description: "Maximum number of results to return (default 50, max 200)"},
				},
				Required: []string{"app_id"},
			},
		},
	}
}

