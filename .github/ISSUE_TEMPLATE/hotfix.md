---
name: Hotfix
about: Production incident requiring an immediate fix
labels: 'class:hotfix'
---

## What broke
[1 sentence: what failed and what impact it caused]

## What's the fix
[1 sentence: what change was or will be made]

## Risk
[Low | Medium | High] blast radius — [1 sentence on scope if the fix itself fails]

## Post-merge spec deadline
24h from merge. The bot will auto-create a `hotfix-followup` issue if no spec is linked by then.

---
<!-- Hotfix path skips Phases 0 and 1. Ship first, document after. -->
<!-- Required within 24h of merge: -->
<!--   1. Post-merge spec (what broke, what was done, what's deployed) -->
<!--   2. ADR if the fix revealed an architectural gap -->
<!-- Branch naming: hotfix/<klw-id>-<short-slug> -->
