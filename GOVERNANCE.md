# Governance

## Status

**Active.** Maintained by a single primary maintainer with the intent to grow
the maintainer pool as the project attracts contributors.

## Decision-making

webaudt currently operates as a BDFL (benevolent dictator for life) model with
the primary maintainer making final calls. As more maintainers are added, the
model will shift toward lazy consensus on the issue tracker:

- Proposals are made via GitHub issues or PRs.
- If no maintainer objects within 7 days, the proposal is accepted.
- Disagreements are resolved by discussion; the primary maintainer breaks ties.

## Adding maintainers

A contributor may be invited to become a maintainer after a sustained track
record (typically 5+ substantive PRs reviewed or merged over 3+ months) and at
the unanimous agreement of existing maintainers.

## Removing maintainers

A maintainer may step down at any time by opening a PR against `MAINTAINERS.md`.
Inactive maintainers (no activity for 12+ months) may be moved to an "emeritus"
list by agreement of the remaining maintainers.

## Project abandonment

If the primary maintainer becomes unable to continue and no other maintainer
takes over, the README will be updated to reflect "looking for maintainers"
status. If no maintainer steps forward within 6 months, the project will be
archived on GitHub with a pointer to any community fork that emerges.

## Changes to governance

Changes to this document require a PR with at least 7 days open for comment,
and approval from the primary maintainer (and any other current maintainers).
