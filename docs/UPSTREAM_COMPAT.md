# Upstream ARTEX compatibility policy

Our fork is a **native fork of [BaaRai/ARTEX](https://github.com/BaaRai/ARTEX)**.
The priority is to remain a drop-in alternative to upstream: running ARTEX
without selecting one of our optional features should behave like upstream.

## Rules

1. Keep the Go module path, AGPL-3.0 license, CLI interface, environment
   variables, PostgreSQL schema/migrations, and pre-existing API routes compatible.
   New functionality uses separate packages, settings, and additive endpoints.
2. **Default OFF.** Managed Proxy Pool is opt-in. Without it, existing
   `global_proxy` selection and capture behavior remain exactly as ARTEX
   defines them. WebFetch retains the original fallback behavior; enabling
   the managed pool adds a runtime network guard to deny direct fallback.
   Keep old `ARTEX_TARGET_USER_AGENT` semantics if no new target identity
   is set. Target-identity headers are disabled by default.
3. Do not alter authorization scope, existing rate/noise profiles,
   evidence formats, or recording behavior for an unconfigured install.
4. Merge upstream main regularly with normal Git merges, not a hard reset,
   force-push, or a code replacement. Test *both* upstream default and
   custom feature behavior after every merge.
5. Preserve upstream patches, migrations, and test updates. Resolve conflicts
   locally and review each one; never silently discard upstream changes.

## Checks

On each fork update, run:

```sh
go build ./...
go vet ./...
go test ./...
(cd web && npm ci --no-audit --no-fund && npm run build:static)
```

With a PostgreSQL test database, also run `go test -p 1 ./...`, keeping
package suites sequential because upstream tests share a database.
Some PostgreSQL tests skip automatically without `ARTEX_PG_DSN`.

The added compatibility regressions cover:

- Default WebFetch retry **before/after** enabling the managed pool, including
  a WebFetch tool created while the pool was disabled.
- Default `global_proxy` routing, clearing the setting, and rejecting an invalid
  proxy replacement without changing the current active upstream.
- The explicit managed pool's host allowlist, TLS/SOCKS checks, sticky selection,
  no-direct-fallback guarantee, and recording-proxy integration.
- The existing `ARTEX_TARGET_USER_AGENT` with the new optional request identity.

Run `scripts/check-upstream-compat.sh` for a **disposable trial merge**
against the latest upstream `main`. It fetches the upstream branch, creates
a temporary Git worktree, merges upstream there, then runs Go build, vet,
tests, and the frontend build if new upstream commits exist. It never
changes or pushes the current branch. Set `ARTEX_COMPAT_FORCE_TEST=1`
to run the checks even when upstream is already contained.

The fork retains upstream's existing GitHub Actions CI on pushes and PRs.
A separate scheduled workflow can be installed later, but was **not**
committed because the GitHub OAuth authorization available for this fork
does not have permission to create or update workflow files.

## Known boundary

The Managed Proxy Pool constrains requests that traverse ARTEX's recording
proxy, and WebFetch direct-fallback paths when enabled. Tools/scanners that
bypass those paths remain outside its guarantee; use independent network egress
restrictions for complete isolation. Changes to active upstream proxy or
recording capture can require agent rebuilds as in upstream.
