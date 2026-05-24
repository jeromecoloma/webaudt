# webaudt

Terminal UI for monitoring `composer audit` and `npm audit` findings across a registry of local sites.

Written in Go. Pure single-binary install — no external runtime deps beyond `composer`/`npm` themselves (and only when you register a site of that type).

## Install

```sh
git clone <this repo> ~/src/webaudt
cd ~/src/webaudt
./install.sh
```

`install.sh` builds the binary, symlinks it into `~/.local/bin/webaudt`, and installs shell completions for bash and zsh.

Build manually:

```sh
go build -o bin/webaudt ./cmd/webaudt
```

Requires Go 1.21+ to build. The compiled binary has no Go runtime dependency.

## Quickstart

```sh
webaudt add ~/Sites/mysite.com       # auto-detects composer/npm/both
webaudt list                          # show registered sites
webaudt                               # open the TUI
webaudt refresh --all                 # re-run every audit
webaudt status --json                 # scriptable output
webaudt rm mysite                     # remove a site
```

## Configuration

`~/.config/webaudt/config.toml` is created with defaults on first run.

```toml
[settings]
cache_ttl = 3600
parallel_audits = 4
composer_bin = "composer"
npm_bin = "npm"
color = "auto"

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

## TUI keys

| Key       | Action                                  |
|-----------|-----------------------------------------|
| `1` / `2` | Focus sidebar / preview pane            |
| `Tab`     | Cycle pane focus                        |
| `j` / `k` | Move down / up in the sidebar           |
| `↑` / `↓` | Scroll the preview pane (when focused)  |
| `r`       | Refresh the highlighted site            |
| `R`       | Refresh every site                      |
| `?`       | Show key hints                          |
| `q` / Esc | Quit                                    |

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

Single Go binary. Tested on macOS (Apple Silicon). Linux is supported but not yet smoke-tested.

## License

MIT — see `LICENSE`.
