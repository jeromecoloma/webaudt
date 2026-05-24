# bash completion for webaudt
# Install: source this file from ~/.bashrc, e.g.
#   source /path/to/webaudt/completions/webaudt.bash

_webaudt_sites() {
    local cfg="${XDG_CONFIG_HOME:-$HOME/.config}/webaudt/config.toml"
    [[ -f "$cfg" ]] || return
    command -v yq >/dev/null 2>&1 || return
    yq -p toml -o json '.' "$cfg" 2>/dev/null | jq -r '(.sites // [])[].name' 2>/dev/null
}

_webaudt() {
    local cur prev words cword
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"

    local subcommands="add rm remove list ls refresh status doctor help --help --version"

    # Top-level subcommand.
    if (( COMP_CWORD == 1 )); then
        COMPREPLY=( $(compgen -W "$subcommands" -- "$cur") )
        return
    fi

    local cmd="${COMP_WORDS[1]}"

    # Flag-value completions.
    case "$prev" in
        --type)
            COMPREPLY=( $(compgen -W "composer npm both" -- "$cur") )
            return ;;
        --composer-path|--npm-path)
            COMPREPLY=( $(compgen -d -- "$cur") )
            return ;;
        --name)
            return ;;
    esac

    case "$cmd" in
        add)
            if [[ "$cur" == -* ]]; then
                COMPREPLY=( $(compgen -W "--name --type --composer-path --npm-path" -- "$cur") )
            else
                COMPREPLY=( $(compgen -d -- "$cur") )
            fi
            ;;
        rm|remove)
            COMPREPLY=( $(compgen -W "$(_webaudt_sites)" -- "$cur") )
            ;;
        refresh)
            if [[ "$cur" == -* ]]; then
                COMPREPLY=( $(compgen -W "--all" -- "$cur") )
            else
                COMPREPLY=( $(compgen -W "$(_webaudt_sites)" -- "$cur") )
            fi
            ;;
        status)
            if [[ "$cur" == -* ]]; then
                COMPREPLY=( $(compgen -W "--json" -- "$cur") )
            else
                COMPREPLY=( $(compgen -W "$(_webaudt_sites)" -- "$cur") )
            fi
            ;;
        *)
            ;;
    esac
}

complete -F _webaudt webaudt
