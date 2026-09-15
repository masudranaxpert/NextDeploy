package mcp

// AllTools returns the full list of tools supported by the NextDeploy MCP server (25 tools).
// Descriptions use prompt-style decision boundaries to eliminate agent confusion.
func AllTools() []Tool {
	return []Tool{
		{
			Name: "app_list",
			Description: "List all NextDeploy applications with status, dev mode state, domains, and compose service names. " +
				"Use this to discover available applications before inspecting or deploying.",
			InputSchema: ToolInputSchema{
				Type:       "object",
				Properties: map[string]ToolProperty{},
			},
		},
		{
			Name: "app_get",
			Description: "Get detailed configuration, domains, compose services, and running containers for a specific application. " +
				"Pass include_health:true to also run live HTTP probes against configured domains. Use this when you need deep details or health verification.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id":         {Type: "string", Description: "The application ID"},
					"include_health": {Type: "boolean", Description: "Optional. If true, performs live container state checks and HTTP health probes against configured domains."},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name: "app_create",
			Description: "Create and provision a brand new NextDeploy application directly via MCP. " +
				"Use when initializing a new project without opening the web UI.",
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
			Name: "app_delete",
			Description: "Permanently delete an application and all its running Docker containers, images, volumes, and workspace files. " +
				"Strictly restricted to the application owner or Admin role.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id": {Type: "string", Description: "The application ID to permanently delete"},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name: "workspace_manifest",
			Description: "Inspect workspace files, sizes, timestamps, and SHA-256 hashes. " +
				"Filters out node_modules, .git, and respects .gitignore. Use this FIRST to see project structure or detect changed files. " +
				"Pass local_files: {path: hash} for instant server-side differential check (saves 98%+ tokens). " +
				"Pass depth:1, hash:false for fast directory listing (equivalent to ls).",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id":        {Type: "string", Description: "The application ID"},
					"path":          {Type: "string", Description: "Optional subdirectory inside workspace to scope the manifest"},
					"depth":         {Type: "integer", Description: "Optional directory depth (e.g. 1 for immediate directory listing like ls, default 0 for full recursive scan)"},
					"hash":          {Type: "boolean", Description: "Whether to compute sha256 hash for files (default true). Set false for fastest size+mtime scan."},
					"full_hash":     {Type: "boolean", Description: "Whether to return full 64-character SHA-256 instead of 16-character short SHA (default false)."},
					"include_locks": {Type: "boolean", Description: "Whether to include package lock files (package-lock.json, yarn.lock, pnpm-lock.yaml, etc.). Default false."},
					"local_files":   {Type: "object", Description: "Optional map of {path: hash}. When provided, returns differential sync list {to_upload, to_delete, in_sync_count} saving 98%+ tokens."},
					"exclude":       {Type: "array", Description: "Optional list of additional glob patterns to exclude (e.g. ['*.log', 'dist/*'])"},
					"max_entries":   {Type: "integer", Description: "Maximum entries to return (default 5000, max 20000). Sets truncated:true if exceeded."},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name: "file_read",
			Description: "Read the contents of a single file in the workspace. " +
				"Use when you need to inspect full or partial file content. To inspect directory structure, use workspace_manifest instead.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id": {Type: "string", Description: "The application ID"},
					"path":   {Type: "string", Description: "Relative file path inside workspace"},
					"offset": {Type: "integer", Description: "Optional 1-based line number to start reading from (default 1)"},
					"limit":  {Type: "integer", Description: "Optional maximum number of lines to read (default 0 reads to end of file)"},
					"full":   {Type: "boolean", Description: "Optional. If true, reads full file content even if large (>256KB). Default false (truncates with notice to prevent context exhaustion)."},
				},
				Required: []string{"app_id", "path"},
			},
		},
		{
			Name: "file_write",
			Description: "Create or overwrite a single file in the workspace. " +
				"Use this for single-file changes. For modifying multiple files or mixing writes and deletes in one call, use workspace_apply instead. For unified diffs, use file_patch.",
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
			Name: "file_delete",
			Description: "Delete a single file or directory inside the workspace. " +
				"For deleting multiple files alongside writes atomically, use workspace_apply instead.",
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
			Name: "file_patch",
			Description: "Apply a unified diff patch to the workspace using git apply. " +
				"Use when editing small sections of large files without transmitting entire file contents. For full file writes, use file_write or workspace_apply.",
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
			Name: "file_search",
			Description: "Search workspace files by text content (case-insensitive grep) or filename pattern (glob). " +
				"Use this to find code symbols, functions, or specific files across the codebase without listing everything.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id":      {Type: "string", Description: "The application ID"},
					"query":       {Type: "string", Description: "Optional text to search for inside files (case-insensitive grep). If omitted, only matching filenames are returned."},
					"pattern":     {Type: "string", Description: "Optional filename or glob pattern to filter files (e.g. *.go, *.json, Dockerfile*). Defaults to *."},
					"path":        {Type: "string", Description: "Optional subdirectory inside workspace to scope the search"},
					"names_only":  {Type: "boolean", Description: "If true, returns only matching file paths without line content snippets (equivalent to grep -l), saving 80%+ tokens."},
					"max_results": {Type: "integer", Description: "Maximum number of results to return (default 50, max 200)"},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name: "workspace_apply",
			Description: "Atomically apply a batch of file writes and/or file deletions in a single round-trip. " +
				"Use this when updating multiple files at once. For editing a single file, file_write is simpler. Supports dry_run:true to preview changes.",
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
			Name: "env_list",
			Description: "List environment variable keys (without revealing sensitive values) configured for an application. " +
				"Use this to see which environment keys exist before updating them. To read actual values, use env_reveal.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id": {Type: "string", Description: "The application ID"},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name: "env_reveal",
			Description: "Reveal sensitive environment variable values (passwords, tokens) for an application. " +
				"Use only when secret values are strictly required. Requires explicit 'Allow env_reveal' token permission.",
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
			Name: "env_set",
			Description: "Set or update environment variables for an application and synchronize workspace .env. " +
				"Provide 'key' and 'value' for a single variable, or 'variables' (object) to update multiple variables in one atomic call.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id":    {Type: "string", Description: "The application ID"},
					"key":       {Type: "string", Description: "Single environment variable key name (used when setting one variable)"},
					"value":     {Type: "string", Description: "Single environment variable value (used with key)"},
					"variables": {Type: "object", Description: "Optional key-value map of multiple environment variables to set in a single batch (e.g. {\"PORT\": \"3000\", \"NODE_ENV\": \"production\"})"},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name: "compose_get",
			Description: "Get the effective docker-compose.yml configuration for an application. " +
				"Pass summary:true to get only services and port mappings (saving 95%+ tokens). Pass service to inspect a single service.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id":  {Type: "string", Description: "The application ID"},
					"summary": {Type: "boolean", Description: "If true, returns concise service and port summary object instead of full YAML file."},
					"service": {Type: "string", Description: "Optional specific service name to return only that service's configuration block."},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name: "deploy",
			Description: "Deploy or redeploy the application stack (docker compose up). " +
				"Returns immediately with a job_id for non-blocking execution, or pass wait_seconds (e.g. 180) to wait synchronously. " +
				"Pass rebuild:true to force image pull and container rebuild. For Git apps: automatically preserves local workspace edits (skips git pull if dirty).",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id":       {Type: "string", Description: "The application ID"},
					"rebuild":      {Type: "boolean", Description: "Optional. If true, forces image pull and full container rebuild (redeploy mode). Default false."},
					"wait_seconds": {Type: "integer", Description: "Optional. If > 0, waits synchronously up to wait_seconds (max 300) for completion and returns final status and output. Default 0 (returns job_id immediately)."},
					"summary_only": {Type: "boolean", Description: "Optional. When wait_seconds > 0, return concise summary without raw build logs on success (default true). Set false for full output tail."},
					"git_pull":     {Type: "boolean", Description: "Optional. If true, force-pulls from Git remote before deploying (discards local workspace edits). Default: auto-detect via dirty-check."},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name: "restart",
			Description: "Restart all containers or a specific service container for an application without rebuilding. " +
				"Use for quick restarts after config or env updates when a full compose up is not required.",
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
			Name: "stop",
			Description: "Stop the application container stack (docker compose down) or a specific service container. " +
				"Use when taking an application offline or stopping specific services.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id":  {Type: "string", Description: "The application ID"},
					"service": {Type: "string", Description: "Optional service name to stop only that container"},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name: "deploy_status",
			Description: "Check the current status and output of a deployment job using its job_id. " +
				"Use this to poll progress of an asynchronous deploy until running becomes false.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"job_id":       {Type: "string", Description: "The job ID returned from deploy, restart, or stop"},
					"app_id":       {Type: "string", Description: "Optional fallback app ID to check current or latest deploy run"},
					"summary_only": {Type: "boolean", Description: "Optional. Return concise summary and status without bloating context with full build logs (default true). If deploy failed, returns error tail."},
				},
			},
		},
		{
			Name: "container_logs",
			Description: "Fetch recent runtime stdout/stderr logs from an application container or compose service. " +
				"Filters out repetitive healthcheck probes by default. Use level:'error' to isolate errors.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id":        {Type: "string", Description: "The application ID"},
					"service":       {Type: "string", Description: "Optional service or container name (defaults to primary)"},
					"tail":          {Type: "integer", Description: "Number of lines to tail (default 30)"},
					"filter_health": {Type: "boolean", Description: "If true (default), automatically filters out /healthz and polling access logs."},
					"level":         {Type: "string", Description: "Optional log level filter ('error' or 'warn') to return only matching lines."},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name: "deploy_log_tail",
			Description: "Fetch deployment build history and logs. " +
				"Supports streaming via long-polling: pass job_id, since_offset, and wait_seconds (e.g. 20) to stream build output in real time. For runtime app logs, use container_logs instead.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id":       {Type: "string", Description: "Optional application ID"},
					"job_id":       {Type: "string", Description: "Optional deployment job ID (returned from deploy)"},
					"since_offset": {Type: "integer", Description: "Byte offset in log output to read from (default 0). Pass next_offset from previous call for continuous streaming."},
					"wait_seconds": {Type: "integer", Description: "Seconds to wait for new log output if none currently available (long-polling, default 0, max 30)."},
					"limit":        {Type: "integer", Description: "Number of historical deploy logs to fetch when falling back to app history (default 5)"},
				},
			},
		},
		{
			Name: "container_exec",
			Description: "Execute a shell command inside an application container. " +
				"Use for running migrations, tests, or CLI commands inside the container. Requires explicit 'Allow container_exec' token permission. For host server commands, use server_exec.",
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
			Name: "server_exec",
			Description: "Execute a shell command directly on the host server. " +
				"Strictly restricted to Admin role and 'Allow server_exec' token permission. For container commands, use container_exec instead.",
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
			Name: "git_pull",
			Description: "Pull latest changes from the configured Git repository for an application without deploying. " +
				"Safe by default: refuses to pull if workspace has uncommitted local edits unless force:true is passed. If you want to pull and deploy together, simply use deploy.",
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
	}
}
