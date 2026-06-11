package utils

// Partial template paths for Fiber's html engine (no .html suffix; rooted at web/templates).
const (
	TmplPartialBrowser          = "partials/browser"
	TmplPartialMonitorStats     = "partials/monitor_stats"
	TmplPartialDeployProgress   = "partials/deploy_progress"
	TmplPartialFilePreviewModal = "partials/file_preview_modal"
	TmplPartialLogView          = "partials/log_view"
	TmplPartialTerminalOut      = "partials/terminal_out"

	TmplPartialGitTab              = "partials/git/git_tab"
	TmplPartialAppShowFiles        = "partials/app_show/files_tab"
	TmplPartialAppShowEnvironment  = "partials/app_show/environment_tab"
	TmplPartialAppShowDeployment   = "partials/app_show/deployment_tab"
	TmplPartialAppShowLogs         = "partials/app_show/logs_tab"
	TmplPartialAppShowTerminal     = "partials/app_show/terminal_tab"
	TmplPartialAppShowContainers   = "partials/app_show/containers_tab"
	TmplPartialAppShowVolumes      = "partials/app_show/volumes_tab"
	TmplPartialAppShowDomains      = "partials/app_show/domains_tab"
	TmplPartialAppShowBackup       = "partials/app_show/backup_tab"
	TmplPartialAppShowOverview     = "partials/app_show/overview_tab"
	TmplPartialAppShowHeaderTabs   = "partials/app_show/header_tabs"
	TmplPartialAppShowSwitchSource = "partials/app_show/switch_source_bundle"

	TmplPartialComposeFileCard = "partials/components/compose_file_card"
)
