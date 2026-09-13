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
			Name:        "file_list",
			Description: "List files and directories within an application's workspace (strictly restricted to workspace)",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id": {Type: "string", Description: "The application ID"},
					"path":   {Type: "string", Description: "Relative path inside workspace (default empty for root)"},
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
				},
				Required: []string{"app_id", "path"},
			},
		},
		{
			Name:        "file_write",
			Description: "Create or overwrite a file in the workspace. Automatically validates compose files and guards against path traversal.",
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
			Name:        "deploy",
			Description: "Trigger an asynchronous deployment (docker compose up). Returns a job_id immediately so you can poll deploy_status without timing out.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id": {Type: "string", Description: "The application ID"},
				},
				Required: []string{"app_id"},
			},
		},
		{
			Name:        "redeploy",
			Description: "Trigger an asynchronous full redeployment with image pull and rebuild. Returns a job_id for polling.",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id": {Type: "string", Description: "The application ID"},
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
			Description: "Fetch deployment history and logs for an application (or live logs if deploy is ongoing)",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"app_id": {Type: "string", Description: "The application ID"},
					"limit":  {Type: "integer", Description: "Number of recent logs to fetch (default 5)"},
				},
				Required: []string{"app_id"},
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
			Description: "Execute a shell command inside an application container (or specific compose service) and return stdout/stderr and exit status",
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
			Description: "Execute a shell command in the NextDeploy host / panel environment (strictly restricted to Admin role)",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]ToolProperty{
					"command":         {Type: "string", Description: "Shell command to execute on the server"},
					"timeout_seconds": {Type: "integer", Description: "Command timeout in seconds (default 60, max 300)"},
				},
				Required: []string{"command"},
			},
		},
	}
}
