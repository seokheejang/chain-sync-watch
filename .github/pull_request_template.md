## Summary

<!-- 1-3 bullets describing what changes and why -->

## Related phase / issue

<!-- e.g. docs/plans/phase-01-chain-domain.md, #42 -->

## Checklist

- [ ] Conventional Commits style message (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`, ...)
- [ ] `make fmt-check` / `make lint` pass
- [ ] `make test` pass (and coverage unchanged or improved where applicable)
- [ ] No internal URLs / IPs / hostnames / API keys in diffs, fixtures, or test data
- [ ] `private/` not staged (single-source check: `git status` shows no paths under `private/`)
- [ ] DDD boundaries respected (no infra imports in `internal/{chain,source,verification,diff}`)
- [ ] Plan docs updated if scope or design changed (`docs/plans/**`)
