#!/usr/bin/env bash
# install.sh — symlink bin/webaudt into ~/.local/bin and set up shell completion (idempotent).

set -eu

src_dir="$(cd "$(dirname "$0")" && pwd)"
src="$src_dir/bin/webaudt"
target_dir="${PREFIX:-$HOME/.local/bin}"
target="$target_dir/webaudt"

# ---- Bash 4+ check ----
runtime_bash=$(command -v bash || true)
if [ -n "$runtime_bash" ]; then
    runtime_major=$("$runtime_bash" -c 'echo "${BASH_VERSINFO[0]}"' 2>/dev/null || echo 0)
    if [ "${runtime_major:-0}" -lt 4 ]; then
        printf 'webaudt: WARNING — `bash` on your PATH is %s (need 4+).\n' "$("$runtime_bash" --version | head -n1)" >&2
        case "$(uname -s)" in
            Darwin) printf '  macOS: brew install bash\n\n' >&2 ;;
            Linux)  printf '  Install bash via your distro package manager.\n\n' >&2 ;;
        esac
    fi
fi

# ---- Install the binary symlink ----
mkdir -p "$target_dir"
if [ -L "$target" ]; then
    existing=$(readlink "$target")
    if [ "$existing" = "$src" ]; then
        printf '  webaudt: already linked at %s\n' "$target"
    else
        printf '  webaudt: replacing existing symlink (%s -> %s)\n' "$target" "$existing"
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
    *) printf '\n  Note: %s is not in $PATH. Add it to your shell rc:\n    export PATH="%s:$PATH"\n' "$target_dir" "$target_dir" ;;
esac

# ---- Shell completion ----

install_zsh_completion() {
    local zsh_comp_dir="$HOME/.zsh/completions"
    local zsh_link="$zsh_comp_dir/_webaudt"
    local zsh_src="$src_dir/completions/_webaudt"

    mkdir -p "$zsh_comp_dir"
    if [ -L "$zsh_link" ] && [ "$(readlink "$zsh_link")" = "$zsh_src" ]; then
        printf '  zsh:  already linked at %s\n' "$zsh_link"
    else
        ln -sf "$zsh_src" "$zsh_link"
        printf '  zsh:  installed -> %s\n' "$zsh_link"
    fi

    # Check that ~/.zshrc has the fpath + compinit lines.
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
    local bash_src="$src_dir/completions/webaudt.bash"
    local bashrc="$HOME/.bashrc"
    local line="source \"$bash_src\""
    if [ -f "$bashrc" ] && grep -Fq "$bash_src" "$bashrc"; then
        printf '  bash: already sourced from %s\n' "$bashrc"
    else
        printf '\n  bash: add this line to ~/.bashrc:\n    %s\n' "$line"
    fi
}

printf '\n  Shell completion:\n'
install_zsh_completion
install_bash_completion

printf '\n  Done. Run `webaudt doctor` to verify dependencies.\n'
