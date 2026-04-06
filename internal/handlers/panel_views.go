package handlers

// Partial template paths for Fiber's html engine (no .html suffix; rooted at web/templates).
const (
	tmplPartialBrowser          = "partials/browser"
	tmplPartialMonitorStats     = "partials/monitor_stats"
	tmplPartialDeployProgress   = "partials/deploy_progress"
	tmplPartialFilePreviewModal = "partials/file_preview_modal"
	tmplPartialLogView          = "partials/log_view"
	tmplPartialTerminalOut      = "partials/terminal_out"

	tmplPartialGitTab              = "partials/git/git_tab"
	tmplPartialAppShowFiles        = "partials/app_show/files_tab"
	tmplPartialAppShowEnvironment  = "partials/app_show/environment_tab"
	tmplPartialAppShowDeployment   = "partials/app_show/deployment_tab"
	tmplPartialAppShowLogs         = "partials/app_show/logs_tab"
	tmplPartialAppShowTerminal     = "partials/app_show/terminal_tab"
	tmplPartialAppShowContainers   = "partials/app_show/containers_tab"
	tmplPartialAppShowVolumes      = "partials/app_show/volumes_tab"
	tmplPartialAppShowDomains      = "partials/app_show/domains_tab"
	tmplPartialAppShowOverview     = "partials/app_show/overview_tab"
	tmplPartialAppShowHeaderTabs   = "partials/app_show/header_tabs"
	tmplPartialAppShowSwitchSource = "partials/app_show/switch_source_bundle"

	tmplPartialComposeFileCard = "partials/components/compose_file_card"

	tmplPageTemplates = "pages/templates"
)
