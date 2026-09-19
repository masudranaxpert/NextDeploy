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
    commands="login logout whoami apps create delete link unlink info status open ps containers images push pull diff deploy redeploy stop restart down logs exec run server-exec env files file cat edit folder ls version help completion"

    if [ $COMP_CWORD -eq 1 ]; then
        COMPREPLY=( $(compgen -W "${commands}" -- ${cur}) )
        return 0
    fi

    case "${prev}" in
        -a|--app)
            # Cannot dynamically complete without server, complete nothing
            return 0
            ;;
        env)
            COMPREPLY=( $(compgen -W "list set" -- ${cur}) )
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
        'server-exec:Execute command on host server'
        'env:Manage environment variables'
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
        'login', 'logout', 'whoami', 'apps', 'create', 'delete',
        'link', 'unlink', 'info', 'status', 'open', 'ps',
        'containers', 'images', 'push', 'pull', 'diff', 'deploy', 'redeploy', 'stop', 'restart', 'down',
        'logs', 'exec', 'run', 'server-exec', 'env', 'files', 'file', 'cat', 'edit', 'folder', 'ls', 'version', 'help', 'completion'
    )
    $commands | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
        [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
    }
}
`
