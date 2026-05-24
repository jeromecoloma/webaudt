#!/usr/bin/env bash
# install.sh — build (if needed) and symlink webaudt into ~/.local/bin.
# Also installs shell completion via cobra's built-in generator.

set -eu

src_dir="$(cd "$(dirname "$0")" && pwd)"
src="$src_dir/bin/webaudt"
target_dir="${PREFIX:-$HOME/.local/bin}"
target="$target_dir/webaudt"

# Build the binary if missing.
if [ ! -x "$src" ]; then
    if ! command -v go >/dev/null 2>&1; then
        printf 'webaudt: go is required to build (get it from https://go.dev/).\n' >&2
        exit 1
    fi
    printf '  Building webaudt...\n'
    (cd "$src_dir" && go build -o bin/webaudt ./cmd/webaudt)
fi

mkdir -p "$target_dir"
if [ -L "$target" ]; then
    existing=$(readlink "$target")
    if [ "$existing" = "$src" ]; then
        printf '  webaudt: already linked at %s\n' "$target"
    else
        printf '  webaudt: replacing symlink (%s -> %s)\n' "$target" "$existing"
        rm "$target"
        ln -s "$src" "$target"
    fi
elif [ -e "$target" ]; then
    printf 'webaudt: %s exists and is not a symlink; refusing to overwrite\n' "$target" >&2
    exit 1
else
    ln -s "$src" "$target"
    printf '  webaudt: installed -> %s\n' "$target"
fi

case ":$PATH:" in
    *":$target_dir:"*) ;;
    *) printf '\n  Note: %s is not in $PATH. Add to your shell rc:\n    export PATH="%s:$PATH"\n' "$target_dir" "$target_dir" ;;
esac

# ---- Shell completion via cobra's generator ----

install_zsh_completion() {
    local zsh_comp_dir="$HOME/.zsh/completions"
    local zsh_file="$zsh_comp_dir/_webaudt"
    mkdir -p "$zsh_comp_dir"
    if "$src" completion zsh >"$zsh_file" 2>/dev/null; then
        printf '  zsh:  installed -> %s\n' "$zsh_file"
    fi

    local zshrc="$HOME/.zshrc"
    if [ -f "$zshrc" ] && grep -q '\.zsh/completions' "$zshrc"; then
        :
    else
        printf '\n  Add to ~/.zshrc (before any existing `compinit` line):\n'
        printf '    fpath=(~/.zsh/completions $fpath)\n'
        printf '    autoload -Uz compinit && compinit\n'
        printf '  Then restart your shell or run: exec zsh\n'
    fi
}

install_bash_completion() {
    local bash_comp_dir="${BASH_COMPLETION_USER_DIR:-$HOME/.local/share/bash-completion/completions}"
    local bash_file="$bash_comp_dir/webaudt"
    mkdir -p "$bash_comp_dir"
    if "$src" completion bash >"$bash_file" 2>/dev/null; then
        printf '  bash: installed -> %s\n' "$bash_file"
    fi
}

printf '\n  Shell completion:\n'
install_zsh_completion
install_bash_completion

printf '\n  Done. Run `webaudt doctor` to verify.\n'
