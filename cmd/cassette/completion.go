package main

import (
	"fmt"
	"io"
	"strings"
)

// completionCommands is the registry as completion sees it. It is assigned in
// init() rather than referencing `commands` directly, because the registry
// contains the completion command itself — referencing `commands` from
// cmdCompletion's initializer chain would be an initialization cycle.
var completionCommands []*Command

func init() { completionCommands = commands }

// cmdCompletion prints a shell completion script generated from the command
// registry, so the completion list can never drift from the actual commands.
func cmdCompletion(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		return completionUsage(stderr)
	}
	names := make([]string, 0, len(completionCommands))
	for _, c := range completionCommands {
		names = append(names, c.Name)
	}
	list := strings.Join(names, " ")

	switch args[0] {
	case "bash":
		fmt.Fprintf(stdout, `# bash completion for cassette — eval "$(cassette completion bash)"
_cassette() {
  local cur="${COMP_WORDS[COMP_CWORD]}"
  if [ "$COMP_CWORD" -eq 1 ]; then
    COMPREPLY=( $(compgen -W "%s" -- "$cur") )
  else
    COMPREPLY=( $(compgen -f -- "$cur") )
  fi
}
complete -F _cassette cassette
`, list)
	case "zsh":
		fmt.Fprintf(stdout, `#compdef cassette
# zsh completion for cassette — eval "$(cassette completion zsh)"
_cassette() {
  if (( CURRENT == 2 )); then
    compadd %s
  else
    _files
  fi
}
compdef _cassette cassette
`, list)
	case "fish":
		fmt.Fprintln(stdout, "# fish completion for cassette — cassette completion fish | source")
		for _, c := range completionCommands {
			fmt.Fprintf(stdout, "complete -c cassette -n '__fish_use_subcommand' -a %q -d %q\n", c.Name, c.summary())
		}
	default:
		return completionUsage(stderr)
	}
	return exitOK
}

const completionUsageText = "usage: cassette completion bash|zsh|fish"

func completionUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, completionUsageText)
	return exitUsage
}
