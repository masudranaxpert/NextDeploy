package cmd

import (
	"fmt"
	"os"
)

// RunCompletion generates shell completion scripts for bash, zsh, and powershell.
func RunCompletion(args []string) {
	shell := "bash"
	if len(args) > 0 {
		shell = args[0]
	}

	switch shell {
	case "bash":
		fmt.Print(bashCompletionScript)
	case "zsh":
		fmt.Print(zshCompletionScript)
	case "powershell", "ps1":
		fmt.Print(powershellCompletionScript)
	default:
		fmt.Fprintf(os.Stderr, "Unsupported shell: %s. Supported: bash, zsh, powershell\n", shell)
		os.Exit(1)
	}
}

const bashCompletionScript = `# bash completion for nd
_nd_completions() {
    local cur prev commands
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"
    commands="login logout whoami account accounts apps create delete link unlink info status open ps containers images git push pull diff deploy redeploy stop restart down logs exec run shell server-exec server-shell php db volume volumes backup backups proxy shield top stats clean audit env domain domains files file cat edit folder ls version help completion"

    if [ $COMP_CWORD -eq 1 ]; then
        COMPREPLY=( $(compgen -W "${commands}" -- ${cur}) )
        return 0
    fi

    case "${prev}" in
        -a|--app)
            # Cannot dynamically complete without server, complete nothing
            return 0
            ;;
        account|accounts)
            COMPREPLY=( $(compgen -W "list switch use rename delete remove rm current" -- ${cur}) )
            return 0
            ;;
        env)
            COMPREPLY=( $(compgen -W "list get reveal set unset rm" -- ${cur}) )
            return 0
            ;;
        domain|domains)
            COMPREPLY=( $(compgen -W "list add rm delete" -- ${cur}) )
            return 0
            ;;
        files|file)
            COMPREPLY=( $(compgen -W "list read write edit rm" -- ${cur}) )
            return 0
            ;;
        folder)
            COMPREPLY=( $(compgen -W "rm delete" -- ${cur}) )
            return 0
            ;;
        php)
            COMPREPLY=( $(compgen -W "list create info set stop start restart rules import-manifest fix-perms pma db cron" -- ${cur}) )
            return 0
            ;;
        db)
            COMPREPLY=( $(compgen -W "list create delete export import users" -- ${cur}) )
            return 0
            ;;
        backup|backups)
            COMPREPLY=( $(compgen -W "now list restore schedule drivelink" -- ${cur}) )
            return 0
            ;;
        proxy)
            COMPREPLY=( $(compgen -W "list add rm reload logs" -- ${cur}) )
            return 0
            ;;
        shield)
            COMPREPLY=( $(compgen -W "list test setup-script reassign" -- ${cur}) )
            return 0
            ;;
        volume|volumes)
            COMPREPLY=( $(compgen -W "list browse backup restore" -- ${cur}) )
            return 0
            ;;
        git)
            COMPREPLY=( $(compgen -W "status sync enable disable pull" -- ${cur}) )
            return 0
            ;;
        completion)
            COMPREPLY=( $(compgen -W "bash zsh powershell" -- ${cur}) )
            return 0
            ;;
    esac

    if [[ ${cur} == -* ]] ; then
        COMPREPLY=( $(compgen -W "--help --json -a --app -f --follow --force --prune --recreate -i --stdin -t --timeout -s --service -c --container -w --workdir -y" -- ${cur}) )
        return 0
    fi
}
complete -F _nd_completions nd
`

const zshCompletionScript = `#compdef nd
_nd() {
    local -a commands
    commands=(
        'login:Authenticate with an API token'
        'logout:Remove credentials and disconnect'
        'whoami:Show current user, server, and session status'
        'account:Manage multiple saved accounts'
        'accounts:Manage multiple saved accounts'
        'apps:List applications'
        'create:Create and link a new application'
        'delete:Delete an application'
        'link:Link current directory to an app'
        'unlink:Remove current directory app link'
        'info:Show application details'
        'status:Show application details'
        'open:Open application domain in browser'
        'ps:List containers and processes'
        'containers:List VPS host containers'
        'images:List Docker images'
        'git:Manage Git repository integration and auto-sync'
        'push:Sync files and deploy'
        'pull:Download remote workspace files to local directory'
        'diff:Compare local files against remote workspace'
        'deploy:Deploy application'
        'redeploy:Rebuild and redeploy application'
        'stop:Stop application containers'
        'restart:Restart application containers'
        'down:Stop and remove application containers'
        'logs:Show container logs'
        'exec:Execute command inside container'
        'run:Execute command inside container'
        'shell:Open an interactive shell in container'
        'server-exec:Execute command on host server'
        'server-shell:Open an interactive root shell on host server'
        'php:Manage PHP and WordPress sites'
        'db:Manage MariaDB databases and users'
        'volume:Manage persistent Docker volumes'
        'volumes:Manage persistent Docker volumes'
        'backup:Manage application backups and schedules'
        'backups:Manage application backups and schedules'
        'proxy:Manage Caddy reverse proxy routes'
        'shield:Manage Edge Shield reverse proxy nodes'
        'top:Show live resource usage'
        'stats:Show live resource usage'
        'clean:Clean unused Docker resources'
        'audit:Show audit security log'
        'env:Manage environment variables'
        'domain:Manage custom domains'
        'domains:Manage custom domains'
        'files:List or manage workspace files'
        'file:Read, write, edit, or delete remote files'
        'cat:View remote file contents'
        'edit:Edit remote file in editor'
        'folder:Manage or delete remote folders'
        'version:Print version'
        'help:Show help'
        'completion:Generate shell completion'
    )
    _describe -t commands 'nd command' commands
}
_nd "$@"
`

const powershellCompletionScript = `Register-ArgumentCompleter -Native -CommandName nd -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)
    $commands = @(
        'login', 'logout', 'whoami', 'account', 'accounts', 'apps', 'create', 'delete',
        'link', 'unlink', 'info', 'status', 'open', 'ps',
        'containers', 'images', 'git', 'push', 'pull', 'diff', 'deploy', 'redeploy', 'stop', 'restart', 'down',
        'logs', 'exec', 'run', 'shell', 'server-exec', 'server-shell', 'php', 'db',
        'volume', 'volumes', 'backup', 'backups', 'proxy', 'shield', 'top', 'stats', 'clean', 'audit',
        'env', 'domain', 'domains', 'files', 'file', 'cat', 'edit', 'folder', 'ls', 'version', 'help', 'completion'
    )
    $commands | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
        [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
    }
}
`
