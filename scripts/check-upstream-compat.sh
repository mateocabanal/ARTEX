#!/usr/bin/env bash
# Trial-merge current upstream ARTEX into a disposable Git worktree and run
# compatibility smoke tests. Does not modify or push the current branch.
set -euo pipefail

repo="$(git rev-parse --show-toplevel)"
upstream="${ARTEX_UPSTREAM_URL:-https://github.com/BaaRai/ARTEX.git}"

for bin in git go npm; do
  if ! command -v "$bin" >/dev/null; then
    echo "Missing required tool: $bin" >&2
    exit 2
  fi
done

echo "Fetching upstream ARTEX main from $upstream"
git -C "$repo" fetch --no-tags "$upstream" main
sha="$(git -C "$repo" rev-parse FETCH_HEAD)"

if git -C "$repo" merge-base --is-ancestor "$sha" HEAD && [ "${ARTEX_COMPAT_FORCE_TEST:-0}" != "1" ]; then
  echo "Compatible baseline: fork already contains upstream $sha"
  echo "Set ARTEX_COMPAT_FORCE_TEST=1 to run the smoke suite anyway."
  exit 0
fi

tmp="$(mktemp -d)"
worktree="$tmp/checkout"
cleanup() {
  if [ -d "$worktree" ]; then
    git -C "$repo" worktree remove --force "$worktree" || true
  fi
  rm -rf "$tmp"
}
trap cleanup EXIT

git -C "$repo" worktree add --detach "$worktree" HEAD
echo "Trial-merging upstream $sha (only inside $worktree)"
git -C "$worktree" -c user.name='ARTEX compatibility check' \
  -c user.email='ci@localhost' merge --no-ff --no-edit "$sha"
git -C "$worktree" diff --check

echo "Checking Go build, vet, and tests..."
(
  cd "$worktree"
  go build ./...
  go vet ./...
  # Upstream tests share a test PostgreSQL instance, so serialize packages.
  go test -p 1 ./...
)

echo "Building Next.js frontend..."
(
  cd "$worktree/web"
  npm ci --no-audit --no-fund
  npm run build:static
)

echo "Compatibility smoke passed. The current fork branch was not modified."
