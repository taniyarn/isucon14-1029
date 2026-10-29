#!/usr/bin/env bash
# task push [DIR=<worktree>] [SET_UPSTREAM=1]   — push the worktree's current branch to origin, no permission prompt
# task pr:create DIR=<worktree> [TITLE=…] [BODY_FILE=…] [BASE=main]  — gh pr create from that worktree
# task pr:submit DIR=<worktree> TITLE=… BODY=…|BODY_FILE=… [MSG=<commit msg>] — add -A + commit + push + PR, one call
# task hub:log LINE="…" [FILE=journal|claims]  — the hub: append one line to docs/journal.md (claims.md) on main, push
#
# Why: lanes pushed with `git -C <worktree> push …`, which does not match the allow rule `Bash(git push:*)`, and lane 3
# sat 2 min on a permission prompt (2026-10-28 00:55:51–00:58:04). `task push:*` / `task pr:create:*` are allowed in
# .claude/settings.json, so a lane runs them bare from the repo root with DIR=../wt-<name>.
#
# push rules:
#   - never --force (no way to pass it; a rejected non-fast-forward push stops with the hint to rebase)
#   - a branch → `git push [-u] origin <branch>` (-u when it has no upstream yet, or SET_UPSTREAM=1)
#   - main, or a detached HEAD (the claims / journal flow: worktree --detach origin/main, commit, push HEAD:main) →
#     `git push origin HEAD:main`, only when every file changed in origin/main..HEAD is docs/claims.md or
#     docs/journal.md (CLAUDE.md rule 4 exceptions b / c); otherwise refused. A rejected push (someone pushed a claims
#     row meanwhile) is rebased onto origin/main once and retried.
# pr:submit / hub:log: 2026-10-28 ISUCON14 lanes ran add → commit → git -C push → gh pr create as 3–4 tool calls (~25
# times) and the hub printf → add → commit → pull → push as 5 (5 times); each call is a model round trip.
#   - submit: refuses main / a detached HEAD; uncommitted changes need MSG= (English commit message); an open PR of the
#     branch is reused (the push updated it); prints "PR #<n> <url>"
#   - log: in the main checkout on main only; fetch + merge --ff-only, append, commit that one file (other dirty files stay
#     out), push; a rejected push is rebased once (fetch + rebase); a conflict aborts the rebase and stops
# Tests: tools/lanes/test_push.py (temp bare repo, fake gh).
set -u
mode="${1:-push}"
dir="${DIR:-.}"
case "$dir" in /*) ;; *) dir="${USER_WORKING_DIR:-$PWD}/$dir";; esac
[ -d "$dir" ] || { echo "!! DIR=$dir does not exist"; exit 1; }
bf="${BODY_FILE:-}"  # relative to where task was run, like DIR
if [ -n "$bf" ]; then case "$bf" in /*) ;; *) bf="${USER_WORKING_DIR:-$PWD}/$bf";; esac; fi
cd "$dir" || exit 1
git rev-parse --git-dir >/dev/null 2>&1 || { echo "!! $dir is not a git checkout"; exit 1; }
for a in ${ARGS:-}; do case "$a" in -f|--force*|+*) echo "!! no force push (CLAUDE.md): $a"; exit 1;; esac; done

br=$(git branch --show-current)

if [ "$mode" = log ]; then
  case "${FILE:-journal}" in journal) f=docs/journal.md;; claims) f=docs/claims.md;; *) echo "!! FILE=journal|claims"; exit 1;; esac
  [ -n "${LINE:-}" ] || { echo "!! LINE=\"…\" required"; exit 1; }
  [ "$br" = main ] || { echo "!! hub:log writes main from the main checkout (now: ${br:-detached HEAD} in $dir)"; exit 1; }
  # fetch + merge, not git pull --ff-only (2026-10-28: "Cannot fast-forward to multiple branches"); same as ship.sh
  synced=0
  for i in 1 2; do
    if git fetch -q origin +refs/heads/main:refs/remotes/origin/main && git merge --ff-only -q refs/remotes/origin/main; then
      synced=1; break
    fi
    [ "$i" = 1 ] && { echo "sync of main failed, fetching again in 3 s"; sleep 3; }
  done
  [ "$synced" = 1 ] || { echo "!! fast-forward of main to origin/main failed (local commits diverged, or a dirty file is in the way): git status"; exit 1; }
  [ -z "$(tail -c1 "$f" 2>/dev/null)" ] || echo >> "$f"
  printf '%s\n' "$LINE" >> "$f"
  git commit -q -m "${FILE:-journal}: $(printf '%s' "$LINE" | cut -c1-60)" -- "$f" || { echo "!! commit of $f failed"; exit 1; }
  for i in 1 2; do
    if git push -q origin HEAD:main; then echo "pushed $f ($(git rev-parse --short HEAD))"; exit 0; fi
    [ "$i" = 2 ] && break
    echo "push rejected (main moved): fetch + rebase once"
    { git fetch -q origin +refs/heads/main:refs/remotes/origin/main && git rebase -q refs/remotes/origin/main; } || { git rebase --abort 2>/dev/null; break; }
  done
  echo "!! push of $f failed (conflict? the commit is kept locally): git status / git log origin/main..HEAD"; exit 1
fi

if [ "$mode" = submit ]; then
  [ -n "$br" ] && [ "$br" != main ] || { echo "!! pr:submit needs a branch checked out in $dir (now: ${br:-detached HEAD})"; exit 1; }
  [ -n "${TITLE:-}" ] || { echo "!! TITLE=\"…\" required"; exit 1; }
  if [ -n "$bf" ]; then [ -f "$bf" ] || { echo "!! BODY_FILE=$BODY_FILE not found ($bf)"; exit 1; }
  elif [ -z "${BODY:-}" ]; then echo "!! BODY=\"…\" or BODY_FILE=<file> required (the journal line goes in the body)"; exit 1; fi
  if [ -n "$(git status --porcelain)" ]; then
    [ -n "${MSG:-}" ] || { echo "!! uncommitted changes: MSG=\"<English commit message>\" required (or commit first)"; exit 1; }
    git add -A
    git commit -q -m "$MSG" || { echo "!! commit failed"; exit 1; }
    echo "committed $(git rev-parse --short HEAD): $MSG"
  fi
  git fetch -q origin +refs/heads/main:refs/remotes/origin/main 2>/dev/null || true
  [ -n "$(git rev-list -n1 refs/remotes/origin/main..HEAD 2>/dev/null)" ] || { echo "!! $br has no commit beyond origin/main: nothing to submit"; exit 1; }
  DIR="$dir" bash "$0" push || exit 1
  if url=$(gh pr view "$br" --json url,state -q 'select(.state=="OPEN") | .url' 2>/dev/null) && [ -n "$url" ]; then
    echo "PR #${url##*/} $url (already open; updated by the push)"; exit 0
  fi
  # docs-only / new index files only = fast lane (CLAUDE.md rule 4): the [fast] prefix also keeps the cloud review from starting
  files=$(git diff --name-only --diff-filter=ACMRD refs/remotes/origin/main...HEAD)
  added=$(git diff --name-only --diff-filter=A refs/remotes/origin/main...HEAD)
  case "$TITLE" in "[fast]"*) ;; *)
    fast=
    if [ -n "$files" ] && ! printf '%s\n' "$files" | grep -qv '^docs/'; then fast=docs; fi
    if [ -n "$files" ] && [ "$files" = "$added" ] && ! printf '%s\n' "$files" | grep -qv '^webapp/sql/indexes/'; then fast=index; fi
    if [ -n "$fast" ]; then
      TITLE="[fast] $TITLE"; echo "title: docs / index files only → $TITLE"
    fi ;;
  esac
  args=(pr create --base "${BASE:-main}" --head "$br" --title "$TITLE")
  if [ -n "$bf" ]; then args+=(--body-file "$bf"); else args+=(--body "$BODY"); fi
  url=$(gh "${args[@]}") || { echo "!! gh pr create failed: $url"; exit 1; }
  url=$(printf '%s\n' "$url" | grep -Eo 'https://[^ ]+/pull/[0-9]+' | tail -1)
  echo "PR #${url##*/} $url"; exit 0
fi

if [ "$mode" = pr ]; then
  [ -n "$br" ] && [ "$br" != main ] || { echo "!! pr:create needs a branch checked out in $dir (now: ${br:-detached HEAD})"; exit 1; }
  if ! git ls-remote --exit-code --heads origin "$br" >/dev/null 2>&1; then
    echo "!! origin has no branch $br yet: task push DIR=${DIR:-.} first"; exit 1
  fi
  args=(pr create --base "${BASE:-main}" --head "$br")
  if [ -n "${TITLE:-}" ]; then args+=(--title "$TITLE"); fi
  if [ -n "$bf" ]; then
    [ -f "$bf" ] || { echo "!! BODY_FILE=$BODY_FILE not found ($bf)"; exit 1; }
    args+=(--body-file "$bf")
  fi
  if [ -z "${TITLE:-}" ] || [ -z "${BODY_FILE:-}" ]; then args+=(--fill); fi
  exec gh "${args[@]}"
fi

if [ -z "$br" ] || [ "$br" = main ]; then
  git fetch -q origin +refs/heads/main:refs/remotes/origin/main || { echo "!! git fetch origin main failed"; exit 1; }
  files=$(git diff --name-only refs/remotes/origin/main...HEAD --)
  if [ -z "$files" ]; then echo "nothing to push: HEAD has no change against origin/main"; exit 0; fi
  other=$(printf '%s\n' "$files" | grep -vxE 'docs/(claims|journal)\.md' || true)
  if [ -n "$other" ]; then
    echo "!! refusing to push to main: only docs/claims.md / docs/journal.md go to main directly (CLAUDE.md rule 4)."
    echo "   also changed: $(echo $other)"
    echo "   → commit on a branch (git switch -c <name>) and task push DIR=… ; then task pr:create"
    exit 1
  fi
  for i in 1 2; do
    if git push -q origin HEAD:main; then echo "pushed to main: $(echo $files) ($(git rev-parse --short HEAD))"; exit 0; fi
    [ "$i" = 2 ] && break
    echo "push rejected (main moved): rebasing onto origin/main once"
    git fetch -q origin +refs/heads/main:refs/remotes/origin/main && git rebase -q refs/remotes/origin/main || { git rebase --abort 2>/dev/null; break; }
  done
  echo "!! push to main failed (conflict in $(echo $files)?): git -C $dir status"; exit 1
fi

up=""
# -u when asked, or when the upstream is not origin/<branch> (worktree add -b x origin/main tracks origin/main)
if [ "${SET_UPSTREAM:-0}" = 1 ] || [ "$(git rev-parse --abbrev-ref --symbolic-full-name '@{u}' 2>/dev/null)" != "origin/$br" ]; then up="-u"; fi
if git push -q $up origin "$br"; then
  echo "pushed $br ($(git rev-parse --short HEAD))${up:+, upstream set}"
else
  echo "!! push of $br was rejected (non-fast-forward?): git -C $dir fetch && git -C $dir rebase origin/$br — never --force"
  exit 1
fi
