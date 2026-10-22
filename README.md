# webaudt

**Status:** active — under regular development.

Terminal UI for monitoring `composer audit` and `npm audit` findings across a registry of local sites.

Written in Go. Pure single-binary install — no external runtime deps beyond `composer`/`npm` themselves (and only when you register a site of that type).

## Install

### Prebuilt binary (recommended — no Go required)

```sh
curl -fsSL https://raw.githubusercontent.com/jeromecoloma/webaudt/main/install.sh | bash
```

This detects your platform, downloads the matching archive from the latest
release, **verifies its sha256 against the release's `checksums.txt` before
unpacking it**, installs `webaudt` into `~/.local/bin`, and sets up bash and zsh
completions. It never uses `sudo`, and it aborts without installing anything if
the checksum does not match.

Options:

```sh
./install.sh --version v0.3.4   # pin a specific release
PREFIX=~/bin ./install.sh       # install somewhere else
./install.sh --help
```

Releases include macOS (arm64, x86_64) and Linux (arm64, x86_64) builds.

<details>
<summary>Prefer to install by hand?</summary>

Download the archive and `checksums.txt` for your platform from the
[latest release](https://github.com/jeromecoloma/webaudt/releases/latest), then
verify it before extracting:

```sh
shasum -a 256 -c checksums.txt --ignore-missing   # sha256sum -c on Linux
tar -xzf webaudt_<version>_macos_arm64.tar.gz
mv webaudt ~/.local/bin/
webaudt doctor
```

</details>

### From source

```sh
git clone https://github.com/jeromecoloma/webaudt.git ~/src/webaudt
cd ~/src/webaudt
./install.sh
```

Run inside a checkout, `install.sh` builds the binary, symlinks it into
`~/.local/bin/webaudt`, and installs shell completions — so a rebuild is picked
up immediately. (Use `--from-release` to download a prebuilt binary instead.)

Build manually:

```sh
go build -o bin/webaudt ./cmd/webaudt
```

Requires Go 1.26+ to build. The compiled binary has no Go runtime dependency.

## Quickstart

```sh
webaudt add ~/Sites/mysite.com       # auto-detects composer/npm/both
webaudt list                          # show registered sites
webaudt                               # open the TUI
webaudt refresh --all                 # re-run every audit
webaudt status --json                 # scriptable output
webaudt rm mysite                     # remove a site
webaudt doctor                        # verify composer/npm installs
```

Run `webaudt <cmd> --help` for per-command flags.

## Configuration

`~/.config/webaudt/config.toml` is created with defaults on first run.

```toml
[settings]
cache_ttl = 3600
parallel_audits = 4
composer_bin = "composer"
npm_bin = "npm"
color = "auto"
nerd_fonts = false   # set true for Laravel/PHP/Node/npm devicons in the detail pane

[[sites]]
name = "mysite"
path = "/home/user/Sites/mysite.com"
type = "both"
composer_path = "/home/user/Sites/mysite.com"
npm_path = "/home/user/Sites/mysite.com/www"
# composer_bin and npm_bin are auto-detected at `add` time and only persisted
# when they differ from the global default. Edit freely.
composer_bin = "/home/user/Sites/mysite.com/bin/composer"
```

The detail pane shows the current git branch (when the site path is a git
working tree), the project's Laravel version (from `composer.lock`, falling
back to the `composer.json` constraint) and Node version (from `.nvmrc`,
`.node-version`, then `volta.node` or `engines.node` in `package.json`) when
present. Set `nerd_fonts = true` — or `WEBAUDT_NERD_FONTS=1`,
which overrides the config — to prefix those lines and the composer/npm
ecosystem headers with devicons. Requires a [Nerd Font](https://www.nerdfonts.com/)
in your terminal; left off by default to avoid tofu boxes.

## TUI keys

| Key       | Action                                  |
|-----------|-----------------------------------------|
| `1` / `2` | Focus sidebar / preview pane            |
| `Tab`     | Cycle pane focus                        |
| `j` / `k` | Move down / up in the sidebar           |
| `↑` / `↓` | Scroll the preview pane (when focused)  |
| `r`       | Refresh the highlighted site            |
| `R`       | Refresh every site                      |
| `o`       | Open selected site in a new tmux window |
| `b`       | Toggle CVE overview pane                |
| `t`       | Open / switch CVE overview tab          |
| `?`       | Show key hints                          |
| `q` / Esc | Quit                                    |

The CVE overview pane sits below **Details** and is open by default. It has
two tabs:

- **Most vulnerable** — sites ranked by total advisory count (tie-break:
  critical → high → name).
- **Recent scans** — sites ordered by last-checked time, newest first.

Press `b` to hide/show it; `t` opens it (when closed) or cycles between
tabs (when open).

## `status --json`

Prints an array with one object per audited site (sites never checked are left
out). Each object is the cached audit result (`name`, `path`, `checked_at`,
`composer`, `npm`, ...) plus a `versions` block read from the project's files
when the command runs:

```json
"versions": {
  "laravel": { "version": "12.33.0", "source": "composer.lock" },
  "php":     { "version": "7.4", "source": "composer_bin" },
  "node":    { "version": "20", "source": ".nvmrc" },
  "branch":  "main"
}
```

Anything not found is `null`. Sources, in the order they are tried:

- `laravel`: `composer.lock`, then `composer.json require` (a constraint)
- `php`: `composer_bin` (a versioned PHP binary in the site's composer command,
  e.g. `/usr/bin/php7.4 composer`), then `composer.json platform`
  (`config.platform.php`), then `composer.json require` (a constraint)
- `node`: `.nvmrc`, `.node-version`, `package.json volta`, `package.json engines`

The exit code follows the table below, so a non-zero exit still comes with
valid JSON on stdout.

## Exit codes (`webaudt status` / `refresh`)

| Code | Meaning                          |
|------|----------------------------------|
| 0    | Clean                            |
| 1    | Moderate / low / info / unrated  |
| 2    | High                             |
| 3    | Critical                         |
| 10   | Audit failed                     |

## Severity buckets

webaudt normalizes both composer and npm output into six severity buckets, ordered worst → least severe:

1. **critical** — confirmed CVSS critical
2. **high** — confirmed CVSS high
3. **unknown** (pink/magenta `◆`) — advisory present but composer/npm gave **no severity rating** (typical for FriendsOfPHP advisories without a CVSS score). Treat as "review required".
4. **moderate**
5. **low**
6. **info**

## Portability

Single Go binary. Tested on macOS (Apple Silicon); Linux supported.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for dev setup and the PR process.
Bug reports and feature requests go through the issue templates in
`.github/ISSUE_TEMPLATE/`.

By participating you agree to abide by the [Code of Conduct](CODE_OF_CONDUCT.md).

## Security

To report a vulnerability, see [SECURITY.md](SECURITY.md). Please do not file
public issues for security reports.

## Governance

See [GOVERNANCE.md](GOVERNANCE.md) and [MAINTAINERS.md](MAINTAINERS.md).

## License

MIT — see `LICENSE`.
