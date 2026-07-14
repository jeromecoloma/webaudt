#!/usr/bin/env python3
"""PreToolUse guardrails for webaudt.

Enforced even under `claude --dangerously-skip-permissions` (deny rules and
PreToolUse hooks still fire when permission prompts are skipped).

Blocks, for Bash commands:
  - stack-specific local-data destruction (see STACK RULES below)
  - deletion/overwrite/reads of .env files (secrets, usually not in git)
  - destructive shell / git (rm -rf outside repo, force-push, reset --hard, ...)
  - AI attribution in commits / PRs (including via -F message files)
  - secret reads via shell (~/.ssh keys, ~/.aws)
  - shell writes into protected home paths and persistence points
  - edits to this guardrail itself (.claude/settings.json, .claude/hooks/)

Decision protocol: print a PreToolUse JSON deny on stdin-driven match, else allow.
"""
import json
import os
import re
import sys

# Portable: the current project dir (set by Claude Code), else cwd. Used to allow
# rm -rf *inside* the repo while blocking deletes of home/root elsewhere.
PROJECT_DIR = os.environ.get("CLAUDE_PROJECT_DIR") or os.getcwd()
_PROJ_REL = re.escape(PROJECT_DIR.lstrip("/")) or r"\0"  # sentinel if empty/root

# Home-path fragment that matches on macOS and Linux, literal or via ~/$HOME/${HOME}.
HOME = r"(~|\$\{?HOME\}?|/Users/[^/\s]+|/home/[^/\s]+)"

# Temp roots where recursive deletes are always fine.
_SAFE_ABS_PREFIXES = ("/tmp/", "/private/tmp/", "/var/folders/", "/private/var/folders/")

# Commands that read file contents into output/clipboard (secret-exfil vectors).
READERS = r"(cat|bat|less|more|head|tail|base64|xxd|od|strings|hexdump|cp|scp|rsync|grep|sed|awk)"

# Commands that can write/replace a file given as an argument.
WRITERS = r"(cp|mv|install|ln|tee|chmod|chown|mkdir|sed|perl|rsync|curl|wget|touch|truncate|dd|unzip)"


def deny(reason: str) -> None:
    print(json.dumps({
        "hookSpecificOutput": {
            "hookEventName": "PreToolUse",
            "permissionDecision": "deny",
            "permissionDecisionReason": reason,
        }
    }))
    sys.exit(0)


def _rm_is_dangerous(cmd: str) -> bool:
    """True when any rm in the command is recursive+force with a target that is
    home, root, wildcard, or an absolute/relative path escaping the repo
    (temp roots excluded). Handles -rf, -Rf, -r -f, --recursive/--force."""
    proj = PROJECT_DIR.rstrip("/") + "/"
    prefixes = {"sudo", "xargs", "command", "exec", "nice", "time", "env", "then", "do"}
    for seg in re.split(r"[|;&]+", cmd):
        toks = seg.split()
        # Only treat `rm` as an invocation when it starts the segment or follows
        # a command prefix / VAR=val — not when it appears mid-prose in a -m message.
        idx = None
        for i, t in enumerate(toks):
            if t == "rm" and (i == 0 or toks[i - 1] in prefixes or "=" in toks[i - 1]):
                idx = i
                break
        if idx is None:
            continue
        args = toks[idx + 1:]
        recursive = force = False
        targets = []
        for a in args:
            if a == "--":
                continue
            if a.startswith("--"):
                recursive |= a == "--recursive"
                force |= a == "--force"
            elif a.startswith("-") and len(a) > 1:
                recursive |= bool(re.search(r"[rR]", a))
                force |= "f" in a
            else:
                targets.append(a.strip("'\""))
        if not (recursive and force):
            continue
        for t in targets:
            if t in ("/", ".", "..", "./", "../", "*", "~"):
                return True
            if t.startswith(("~", "$HOME", "${HOME}", "..")):
                return True
            if t.startswith("/"):
                normalized = t.rstrip("/") + "/"
                if not normalized.startswith(proj) and not t.startswith(_SAFE_ABS_PREFIXES):
                    return True
    return False


# (compiled regex, reason[, extra predicate]). First match wins.
RULES = [
    # ---- guardrail self-defense ----
    (re.compile(r">>?\s*['\"]?\S*\.claude/(settings|hooks)|"
                r"\b(rm|mv|cp|tee|ln|chmod|chown|touch|truncate|sed|perl)\b"
                r"[^|;&\n]*\s\S*\.claude/(settings\.json|hooks/)"),
     "Refusing to modify the guardrail itself (.claude/settings.json, "
     ".claude/hooks/). Ask the user to change it manually.", None),

    # =====================================================================
    # STACK RULES — Go CLI/TUI. webaudt's only irreplaceable local state is
    # the user's own ~/.config/webaudt/ (hand-written config.toml, site
    # registry). The cache under it is regenerable, so allow cache deletes.
    # Build outputs (bin/, dist/) are disposable — no rules needed.
    # =====================================================================

    # ---- webaudt user config / registry: hand-written, not in git ----
    (re.compile(r"\b(rm|mv|truncate|dd|tee)\b[^|;&\n]*\s['\"]?\S*"
                r"(\.config/webaudt|\$\{?XDG_CONFIG_HOME\}?/webaudt)\S*"),
     "Refusing to delete/overwrite the user's real webaudt config "
     "(~/.config/webaudt — config.toml and the site registry are "
     "hand-written and not in git). Point XDG_CONFIG_HOME at a temp dir to "
     "test config behaviour instead.",
     lambda c: not re.search(r"\.config/webaudt/cache\b|"
                             r"XDG_CONFIG_HOME=[^\s]*/(tmp|T)/", c)),
    (re.compile(r">>?\s*['\"]?\S*(\.config/webaudt|"
                r"\$\{?XDG_CONFIG_HOME\}?/webaudt)\S*/[^\s'\"]*\.(toml|json)"),
     "Refusing to redirect output into the user's real webaudt config "
     "(~/.config/webaudt). Ask the user to edit it manually, or test against "
     "a temp XDG_CONFIG_HOME.", None),

    # =====================================================================
    # CORE RULES — keep all of these.
    # =====================================================================

    # ---- local DB via CLI ----
    (re.compile(r"(?i)(\bdrop\s+(database|schema)\b|\bmysqladmin\b[^|;&\n]*\bdrop\b)"),
     "Refusing DROP DATABASE via CLI. The local dev DB is not disposable.", None),

    # ---- .env protection (secrets; usually not in git) ----
    (re.compile(r"\brm\b[^|;&\n]*\.env(\.local)?\b"),
     "Refusing to delete .env / .env.local — it is not in git and must be "
     "restored manually if lost.", None),
    (re.compile(r">>?\s*['\"]?\S*\.env(\.local)?(?=[\s'\"]|$)"),
     "Refusing to overwrite/append to .env / .env.local via redirect. "
     "Ask the user to edit env values manually.", None),
    (re.compile(r"\b" + READERS + r"\b[^|;&\n]*\s['\"]?\S*\.env(\.local)?(?=[\s'\"]|$)"),
     "Refusing to read .env / .env.local (contains secrets). Read specific "
     "values through the framework's config layer instead.", None),

    # ---- secret reads via shell (~/.ssh keys, ~/.aws creds) ----
    (re.compile(r"(\b" + READERS + r"\b[^|;&\n]*\s|<\s*)['\"]?"
                r"(" + HOME + r"/\.ssh/|" + HOME + r"/\.aws/)"),
     "Refusing to read ~/.ssh or ~/.aws contents (private keys / credentials).", None),

    # ---- destructive shell ----
    (re.compile(r"\brm\s"),
     "Refusing recursive force-delete targeting home/root/wildcards or paths "
     "outside the repo. Scope rm -rf to a path inside the repo or temp.",
     _rm_is_dangerous),
    (re.compile(r"\bfind\s+(~|\$\{?HOME\}?|/(?!tmp/|private/|var/folders/|" + _PROJ_REL + r"))"
                r"[^|;&\n]*(-delete\b|-exec\s+rm)"),
     "Refusing find -delete / -exec rm outside the repo and temp dirs.", None),
    (re.compile(r":\(\)\s*\{|"        # fork bomb
                r"\bdd\s+if=|"
                r"\bmkfs\b|"
                r">\s*/dev/(sd|disk|nvme)"),
     "Refusing a disk-destroying command (dd / mkfs / fork-bomb / raw device write).", None),

    # ---- git, destructive ----
    (re.compile(r"git\s+push\b.*(--force\b(?!-with-lease)|(?<!-)\s-f\b)"),
     "Refusing `git push --force`. Use `--force-with-lease` if a force push is truly needed.", None),
    (re.compile(r"git\s+push\b[^|;&\n]*\s\+\S+"),
     "Refusing plus-refspec push (`git push origin +branch` is a force push). "
     "Use --force-with-lease if truly needed.", None),
    (re.compile(r"git\s+push\b[^|;&\n]*(\s(-d|--delete)\b|\s:\S+)"),
     "Refusing remote branch deletion via git push.", None),
    (re.compile(r"git\s+reset\s+--hard\b"),
     "Refusing `git reset --hard` (discards working tree). Stash or commit first, "
     "or reset a specific path.", None),
    (re.compile(r"git\s+(checkout|restore)\b[^|;&\n]*\s(\.|\*)['\"]?\s*$"),
     "Refusing to discard the entire working tree (git checkout/restore .). "
     "Restore specific paths instead.",
     lambda c: "--staged" not in c),
    (re.compile(r"git\s+branch\s+(-D\b|--delete\s+--force\b)"),
     "Refusing force branch delete (-D). Use -d for merged branches.", None),
    (re.compile(r"git\s+clean\b.*-[a-zA-Z]*[fx]"),
     "Refusing `git clean -f/-x` (deletes untracked/ignored files incl. env files).", None),
    (re.compile(r"git\s+stash\s+(drop|clear)\b"),
     "Refusing to delete stashes (unrecoverable).", None),
    (re.compile(r"git\s+filter-(branch|repo)\b"),
     "Refusing history rewrite (filter-branch/filter-repo).", None),

    # ---- AI attribution ----
    (re.compile(r"(?i)(co-authored-by:\s*claude|generated with \[?claude|"
                r"🤖 generated with|noreply@anthropic\.com)"),
     "This repo forbids AI attribution in commits/PRs. Remove the "
     "Co-Authored-By / 'Generated with Claude' line from the message.", None),
    # Message-from-file bypasses the scan above — require inline or stdin.
    (re.compile(r"git\s+commit\b[^|;&\n]*\s(-F|--file)\b"),
     "Refusing commit message from a file (guardrail cannot scan it). "
     "Use -m, or -F - with a heredoc.",
     lambda c: not re.search(r"(-F|--file)[= ]+-(\s|$)", c)),
    (re.compile(r"gh\s+(pr|issue)\s+(create|edit|comment)\b[^|;&\n]*(\s-F\b|--body-file\b)"),
     "Refusing PR/issue body from a file (guardrail cannot scan it). "
     "Use --body, or --body-file - with a heredoc.",
     lambda c: not re.search(r"(--body-file|\s-F)[= ]+-(\s|$)", c)),

    # ---- protected home paths via shell writes ----
    # Redirect INTO a protected path: tie the target to the path so a read with
    # `2>/dev/null` (any stray `>`) does not false-positive.
    (re.compile(r">>?\s*\"?(" + HOME + r"/\.ssh|"
                + HOME + r"/\.claude/(settings|hooks)|"
                + HOME + r"/Library/LaunchAgents|"
                + HOME + r"/\.z(shrc|profile)|"
                + HOME + r"/\.gitconfig|"
                + HOME + r"/bin/)"),
     "Refusing to redirect output into a protected home path (~/.ssh, ~/.claude, "
     "LaunchAgents, dotfiles, ~/bin). Keep tooling repo-local.", None),
    # Write-capable command whose target is a protected path
    # (scoped to one command segment — no greedy `.*` across pipes).
    (re.compile(r"\b" + WRITERS + r"\b[^|;&\n]*?"
                r"(" + HOME + r"/\.ssh|"
                + HOME + r"/\.claude/(settings|hooks)|"
                r"Library/LaunchAgents|"
                + HOME + r"/\.z(shrc|profile)|"
                + HOME + r"/\.gitconfig|"
                + HOME + r"/bin/)"),
     "Refusing to write into a protected home path (~/.ssh, ~/.claude, "
     "LaunchAgents, dotfiles, ~/bin). Keep tooling repo-local.", None),

    # ---- persistence points ----
    (re.compile(r"git\s+config\b[^|;&\n]*\s(--global|--system)\b"),
     "Refusing global/system git config writes. Use repo-local `git config`, "
     "or `--global --get` to read.",
     lambda c: not re.search(r"--(get|list)\b|\s-l\b", c)),
    (re.compile(r"\bcrontab\b"),
     "Refusing crontab modification (persistence). `crontab -l` to list is fine.",
     lambda c: not re.search(r"crontab\s+-l\b", c)),
    (re.compile(r"\blaunchctl\s+(load|bootstrap|enable|submit)\b"),
     "Refusing launchctl load/bootstrap (persistence).", None),
]


def main() -> None:
    raw = sys.stdin.read()
    try:
        data = json.loads(raw)
    except Exception:
        sys.exit(0)  # malformed input — don't block

    if data.get("tool_name") != "Bash":
        sys.exit(0)

    cmd = (data.get("tool_input") or {}).get("command", "")
    if not cmd:
        sys.exit(0)

    for entry in RULES:
        pattern, reason = entry[0], entry[1]
        extra = entry[2] if len(entry) > 2 else None
        if pattern.search(cmd) and (extra is None or extra(cmd)):
            deny(reason)

    sys.exit(0)


if __name__ == "__main__":
    main()
