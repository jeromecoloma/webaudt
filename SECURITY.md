# Security Policy

## Supported versions

webaudt is pre-1.0. Only the latest tagged release (and `main`) receive
security fixes.

| Version | Supported          |
|---------|--------------------|
| latest  | :white_check_mark: |
| older   | :x:                |

## Reporting a vulnerability

**Do not open a public GitHub issue for security reports.**

Please use GitHub's [private security advisory][advisory] feature on this
repository, or email **frozynart@gmail.com** with:

- A description of the issue
- Steps to reproduce
- The affected version / commit
- Any suggested mitigation

[advisory]: https://github.com/jeromecoloma/webaudt/security/advisories/new

## What to expect

- **Acknowledgement** within 48 hours.
- **Initial assessment** within 7 days.
- **Fix or mitigation plan** within 30 days for confirmed vulnerabilities.
- **Coordinated disclosure** — we will work with you on a public disclosure
  timeline, typically within 90 days of the fix landing.

## In scope

- The `webaudt` binary itself
- Anything that could cause webaudt to execute attacker-controlled code on a
  user's machine (e.g. malicious `composer.json` / `package.json` triggering
  command injection)
- Privilege escalation, path traversal, or arbitrary file write via webaudt
- Sensitive data leaks (config, credentials) in logs or output

## Out of scope

- Vulnerabilities in `composer` or `npm` themselves (report to those projects)
- Vulnerabilities in dependencies of the audited sites (that's what webaudt
  surfaces — by design)
- Social engineering, physical attacks, or DoS via resource exhaustion on the
  local machine
