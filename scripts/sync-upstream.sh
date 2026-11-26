#!/usr/bin/env bash
set -Eeuo pipefail

UPSTREAM_REF="${UPSTREAM_REF:-upstream/master}"

UPSTREAM_WINS=(CHANGELOG.md)

# Fork-owned styling: a conflicting hunk in these files keeps OUR lines so an upstream
# change to the dashboard can never silently bring the donate button back or strip the
# OverviewCards details, while every non-conflicting upstream change still lands.
FORK_UI=(
  src/app/globals.css
  "src/app/(dashboard)/dashboard/usage/components/OverviewCards.js"
  src/shared/components/Header.js
)

out() {
  local value=${2//$'\n'/ }
  printf '%s=%s\n' "$1" "$value" >> "${GITHUB_OUTPUT:-/dev/null}"
}

fail() {
  out reason "$1"
  echo "::error::$1"
  git merge --abort 2>/dev/null || true
  exit 1
}

trap 'fail "sync script error at line $LINENO"' ERR

contains() {
  local needle=$1 item
  shift
  for item in "$@"; do
    [ "$item" = "$needle" ] && return 0
  done
  return 1
}

version_at() {
  git show "$1:package.json" | node -p 'JSON.parse(require("fs").readFileSync(0, "utf8")).version'
}

keep_fork_hunks() {
  local file=$1 dir
  dir=$(mktemp -d)
  git show ":1:$file" > "$dir/base" 2>/dev/null || : > "$dir/base"
  git show ":2:$file" > "$dir/ours"
  git show ":3:$file" > "$dir/theirs"
  git merge-file --ours "$dir/ours" "$dir/base" "$dir/theirs"
  cp "$dir/ours" "$file"
  git add -- "$file"
  rm -rf "$dir"
}

resolve_conflicts() {
  local file unresolved=""
  local -a unmerged
  mapfile -t unmerged < <(git -c core.quotePath=false diff --name-only --diff-filter=U)
  for file in "${unmerged[@]}"; do
    if contains "$file" "${UPSTREAM_WINS[@]}"; then
      git checkout --theirs -- "$file"
      git add -- "$file"
    elif contains "$file" "${FORK_UI[@]}" && [ "$(git ls-files -u -- "$file" | wc -l)" -eq 3 ]; then
      keep_fork_hunks "$file"
    else
      unresolved+="$file "
    fi
  done
  [ -z "$unresolved" ] || fail "merge conflicts the sync cannot resolve: ${unresolved% }"
}

guard_fork_styling() {
  local css=src/app/globals.css
  local cards="src/app/(dashboard)/dashboard/usage/components/OverviewCards.js"
  local problems=""
  tr '\r\n' '  ' < "$css" \
    | grep -Ei 'button\[aria-label="Donate"\][^{]*\{[^}]*display:[[:space:]]*none[[:space:]]*!important' >/dev/null \
    || problems+="donate-hide rule missing from globals.css; "
  grep -qi 'idr' "$cards" || problems+="IDR detail missing from OverviewCards.js; "
  if grep -q 'Estimated, not actual billing' "$cards"; then
    problems+="EST. COST disclaimer re-appeared in OverviewCards.js; "
  fi
  printf '%s' "${problems%; }"
}

cd "$(git rev-parse --show-toplevel)"

behind=$(git rev-list --count "HEAD..$UPSTREAM_REF")
if [ "$behind" -eq 0 ]; then
  echo "Already up to date."
  out status up-to-date
  out version "$(version_at HEAD)"
  out version_changed false
  exit 0
fi
echo "Upstream is $behind commit(s) ahead."

before=$(version_at HEAD)

git merge --no-ff --no-commit "$UPSTREAM_REF" || true
git rev-parse -q --verify MERGE_HEAD >/dev/null || fail "git merge could not start; see the run log"

resolve_conflicts
[ -z "$(git diff --name-only --diff-filter=U)" ] || fail "unresolved merge conflicts remain"

problems=$(guard_fork_styling)
[ -z "$problems" ] || fail "fork styling guard failed: $problems"

git commit -q -m "chore: merge upstream decolua/9router ($(git rev-parse --short "$UPSTREAM_REF"))"

after=$(version_at HEAD)
out status merged
out version "$after"
if [ "$before" = "$after" ]; then
  out version_changed false
else
  out version_changed true
fi
echo "Merged upstream $(git rev-parse --short "$UPSTREAM_REF"); version $before -> $after."
