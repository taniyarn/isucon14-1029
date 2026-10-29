#!/usr/bin/env bash
# task hub:ship PRS="5 4" [INIT=1|0] [NEXT="9"] — the hub's one command after the human's OK (run it with run_in_background):
# merge the PRs in order → sync the main checkout → task deploy → task smoke [INIT=1] → bench todo on the board.
#   - A PR that cannot be merged (conflict, checks, rights) is skipped with a lane:alert warn; the rest still ship.
#   - Docs-only ship (the merged diff touches none of webapp/ etc/ deploy.sh env*.sh tools/): task ops:repo only —
#     no deploy, no smoke, no bench todo.
#   - Otherwise deploy / smoke failures stop with a lane:alert crit. On success: bench todo (CMD = BENCH_CMD from .env,
#     MSG names the PRs on it and main's sha), the board's bench band → ready, roadmap H4 → H5 (first not done) → done.
#   - After the merges (docs-only ships too) it records them on main itself: the PR body's journal line (time, #n,
#     merge <sha>, OK: 盤 hh:mm / タブ filled in; tools/lanes/shiprec.py) → docs/journal.md, the claims rows of the
#     merged branches → merged, and the roadmap's lane steps whose plan.json "auto" globs match a merged PR's head
#     branch / files → done "PR #n" (lanes.py autostep). One commit per file (journal-only, claims-only), pushed to main inside this script's
#     main sync, so the hub never commits them by hand (2026-10-27: a hand commit on main broke a ship).
#   - Every step is timed into the board's events.jsonl (task lane:timeline).
#   - A PR that adds / changes files under webapp/sql/indexes/ is checked first with tools/indexcheck (task index:check
#     on the PR head): a deadlock risk (locking read / UPDATE / DELETE by the indexed column in a tx + inserts into
#     the table; IMPLEMENTATION-NOTES §5) → warn alert with the findings and that PR is held (not merged), the rest
#     ship — unless the PR body has a line `index-check: reviewed` (the author fixed it / judged it and says so).
#   - Docs-only PRs given together with code PRs ride along in the same ship (all merged, one deploy). While a bench
#     runs, a mixed list ships its docs-only PRs now and queues the code PRs in the band's NEXT.
#   - No tools/app-analyzer/analyzer.config.json yet (kickoff phase 2 writes it): smoke.mjs falls back to a minimal
#     probe (GET / answers, no 5xx) and says so; 2026-10-28 the full smoke died on ENOENT and held the bench todo.
#   - A PR whose head changed after the human's merge OK on the board (a commit dated after the OK; 2026-10-28: lane 3
#     pushed a fix to #15 right before the ship) is held with a warn alert: re-ask the OK. REOK="15" when the OK was
#     taken again in the tab. The checked head goes to gh pr merge --match-head-commit (a push in between fails it).
# Refuses while the board says a bench is running (FORCE=1 overrides): a deploy mid-bench ruins the run. Docs-only
# ships (every PR's file list from gh pr diff and origin/main's new files outside the deploy paths) go any time and
# never touch the band.
# Refuses on unpushed commits in the main checkout, on uncommitted docs/journal.md / docs/claims.md, and on other
# uncommitted files only when origin/main or the PRs (gh pr diff --name-only) change them; others are left alone.
# Tests: tools/lanes/test_ship.py (temp bare repo, fake gh / task on PATH).
set -u
HERE=$(cd "$(dirname "$0")" && pwd)
cd "$(git rev-parse --show-toplevel)" || exit 1
PRS="${PRS:-}"
INIT="${INIT:-1}"
NEXT="${NEXT:-}"
nx=""; for x in $NEXT; do nx="${nx} #${x#\#}"; done; NEXT="${nx# }"
merged="" skipped="" held="" moved="" queued="" deploy="-" smoke="-" todo="-" mode="deploy"

lane() { python3 "$HERE/lanes.py" "$@"; }
ev() { python3 "$HERE/lanes.py" event "$@" >/dev/null 2>&1 || true; }  # timing for lane:timeline; never fails the ship
T0=$(date +%s); TS=$T0
step() {  # name ok: the time since the previous step
  local now; now=$(date +%s)
  ev kind=ship.step name="$1" sec=$(( now - TS )) ok="${2:-1}"
  TS=$now
}

summary() {
  echo "== hub:ship"
  echo "merged:${merged:- none}${skipped:+  skipped:${skipped} (conflict)}${held:+  held:${held} (index:check)}${moved:+  held:${moved} (head changed after the OK)}${queued:+  queued for after the bench:${queued}}  mode: ${mode}  deploy: ${deploy}  smoke: ${smoke}"
  echo "bench todo: ${todo}  (main $(git rev-parse --short HEAD))"
}
fail() {  # level msg impact
  lane alert lane=1 level="$1" msg="$2" impact="$3"
  ev kind=ship.end ok=0 sec=$(( $(date +%s) - T0 )) merged="${merged# }" mode="$mode" why="$2"
  summary
  exit 1
}

# main checkout = origin/main, fast-forward only. Fetch and merge are separate (git pull --ff-only once died with
# "Cannot fast-forward to multiple branches" in the hub's checkout); one retry after a fresh fetch.
sync_main() {
  local i
  for i in 1 2; do
    if git fetch -q origin +refs/heads/main:refs/remotes/origin/main && git merge --ff-only -q refs/remotes/origin/main; then
      return 0
    fi
    [ "$i" = 1 ] && { echo "sync of main failed, fetching again in 3 s"; sleep 3; }
  done
  return 1
}

br=$(git branch --show-current)
[ "$br" = main ] || fail warn "hub:ship: この checkout は ${br}（main ではない）" "merge も deploy もしていない。main の checkout（ハブ）で実行する"

DEPLOY_RE='^(webapp/|etc/|tools/|deploy\.sh$)|(^|/)env[^/]*\.sh$'  # a diff touching these needs deploy + smoke + bench

# Local commits on the hub's main would make the post-merge fast-forward fail AFTER the PRs are merged on GitHub
# (2026-10-27: merged but not deployed). Refuse before merging anything.
git fetch -q origin +refs/heads/main:refs/remotes/origin/main || fail crit "hub:ship: git fetch origin main に失敗" "merge も deploy もしていない。ネットワーク / 認証を見る"
ahead=$(git rev-list --count refs/remotes/origin/main..HEAD 2>/dev/null || echo "?")
if [ "$ahead" != 0 ]; then
  echo "!! main has ${ahead} commit(s) not on origin/main:"
  git log --oneline refs/remotes/origin/main..HEAD
  fail warn "ハブの main に未 push の commit がある: push するか退けてから hub:ship（未 push ${ahead} 件）" \
    "merge も deploy もしていない。git push origin main（journal / claims だけなら）か別ブランチへ退けて再実行"
fi

# What the merge will bring: origin/main since HEAD + each PR's files (gh pr diff --name-only; unknown = assume code).
main_incoming=$(git diff --no-renames --name-only HEAD refs/remotes/origin/main --)
incoming="$main_incoming"
unknown="" docs_prs="" code_prs="" idx_prs=""
for n in $PRS; do
  n="${n#\#}"
  if f=$(gh pr diff "$n" --name-only 2>/dev/null); then
    incoming="${incoming}
${f}"
    if printf '%s\n' "$f" | grep -qE "$DEPLOY_RE"; then code_prs="${code_prs} ${n}"; else docs_prs="${docs_prs} ${n}"; fi
    printf '%s\n' "$f" | grep -qE '^webapp/sql/indexes/.*\.sql$' && idx_prs="${idx_prs} ${n}"
  else
    unknown="${unknown} #${n}"; code_prs="${code_prs} ${n}"
  fi
done
incoming=$(printf '%s\n' "$incoming" | grep -v '^$' | sort -u || true)
docs_only_expected=0
if [ -z "$unknown" ] && [ -n "$incoming" ] && ! printf '%s\n' "$incoming" | grep -qE "$DEPLOY_RE"; then
  docs_only_expected=1
fi

# A bench running: code ships wait (a deploy mid-bench ruins the run; FORCE=1 overrides). Docs-only ships go any time
# (lanes README §7): they never deploy and never touch the band. A mixed list: the docs-only PRs go now, the code PRs
# are queued in the band's NEXT (the hub ships them after the run).
bstate=$(lane bench 2>/dev/null || echo none)
if [ "$bstate" = running ] && [ "${FORCE:-0}" != 1 ] && [ "$docs_only_expected" != 1 ]; then
  if [ -n "$docs_prs" ] && ! printf '%s\n' "$main_incoming" | grep -qE "$DEPLOY_RE"; then
    for n in $code_prs; do queued="${queued} #${n}"; done
    nxt=$(python3 -c 'import sys; sys.path.insert(0, sys.argv[1]); import lanes; print(lanes.read_board()["bench"].get("next") or "")' "$HERE" 2>/dev/null || true)
    for x in $queued; do case " $nxt " in *" $x "*) ;; *) nxt="${nxt:+$nxt }$x";; esac; done
    lane bench next="$nxt" src=ship >/dev/null 2>&1 || true
    echo "!! a bench is running: the code PRs${queued} wait (queued in the band's NEXT: ${nxt}); shipping the docs-only PRs now:$(for n in $docs_prs; do printf ' #%s' "$n"; done)"
    PRS="$docs_prs"
    docs_only_expected=1
  else
    echo "!! the board says a bench is running (task lane:board). Code ships wait for it (docs-only ships go any time), or: task lane:bench STATE=none / FORCE=1"
    summary
    exit 1
  fi
fi

# index:check on the PR head of every PR that adds index files (tools/indexcheck; IMPLEMENTATION-NOTES §5). Risk and no
# `index-check: reviewed` line in the PR body → warn alert with the findings, the PR is held, the others go on.
index_gate() {  # n → 0 ship it, 1 hold it
  local n="$1" js head body out rc labels
  case " $idx_prs " in *" $n "*) ;; *) return 0;; esac
  js=$(gh pr view "$n" --json headRefName,body 2>/dev/null || echo '{}')
  head=$(printf '%s' "$js" | python3 -c 'import json,sys; print((json.load(sys.stdin).get("headRefName") or ""))' 2>/dev/null || true)
  body=$(printf '%s' "$js" | python3 -c 'import json,sys; print((json.load(sys.stdin).get("body") or ""))' 2>/dev/null || true)
  if [ -z "$head" ] || ! git fetch -q origin "+refs/heads/${head}:refs/remotes/origin/${head}" 2>/dev/null; then
    echo "!! index:check: could not fetch the head of #${n} (${head:-?}); not checked"
    lane alert lane=1 level=warn msg="hub:ship: #${n} の index:check を実行できなかった（head ${head:-?} を fetch できない）。ship は続ける" \
      impact="手で task index:check BRANCH=${head:-<branch>} を見る" >/dev/null
    return 0
  fi
  out=$(python3 "$HERE/../indexcheck/indexcheck.py" --ref "refs/remotes/origin/${head}" --base refs/remotes/origin/main 2>&1)
  rc=$?
  printf '%s\n' "$out" | sed "s/^/  [index:check #${n}] /"
  [ "$rc" = 0 ] && return 0
  if [ "$rc" != 2 ]; then
    lane alert lane=1 level=warn msg="hub:ship: #${n} の index:check が失敗（exit ${rc}）。ship は続ける" impact="$(printf '%s' "$out" | tail -3 | tr '\n' ' ' | cut -c1-300)" >/dev/null
    return 0
  fi
  if printf '%s\n' "$body" | grep -qiE '^[[:space:]>*-]*`?index-check:[[:space:]]*reviewed`?[[:space:]]*$'; then
    echo "  [index:check #${n}] risk found, but the PR body says index-check: reviewed — shipping"
    return 0
  fi
  labels=$(printf '%s\n' "$out" | sed -n 's/^!! \([^ ]*\) — deadlock risk.*/\1/p' | tr '\n' ' ')
  lane alert lane=1 level=warn msg="hub:ship: #${n} の新しい index ${labels}が deadlock を起こしうる（index:check）。#${n} は merge していない（他は続ける）" \
    impact="持ち主に [TASK] <lane> fix #${n}: IMPLEMENTATION-NOTES §5 の直し（主キーで DELETE・UPDATE / 親の行を FOR UPDATE / 1213 で retry / index を外す）→ PR 本文に index-check: reviewed の行 → 再 ship。全文: task index:check BRANCH=${head}。箇所: $(printf '%s\n' "$out" | grep -E '^   \[[a-d]\]' | head -3 | sed -E 's/^ *//; s/  .*//' | tr '\n' ' ')" >/dev/null
  return 1
}

# The head the human OK'd: the newest web "ok" decision on the board that names PR n (not toggled off) vs the newest
# committer date among the PR's commits (gh pr view --json headRefOid,commits). A commit after the OK → hold (1).
# No board OK (tab OK, fast lane) or no data → no check (0). MATCH = the head sha checked, for --match-head-commit.
head_gate() {  # n → 0 merge it, 1 hold it
  local n="$1" js res st sha okhm cd
  MATCH=""
  js=$(gh pr view "$n" --json headRefOid,commits 2>/dev/null) || return 0
  res=$(printf '%s' "$js" | python3 -c '
import datetime, json, sys
sys.path.insert(0, sys.argv[1])
n, reok = int(sys.argv[2]), {x.lstrip("#") for x in sys.argv[3].split()}
d = json.load(sys.stdin)
head = d.get("headRefOid") or "-"
try:
    import lanes
    ok = max([x.get("decided_at") or 0 for x in lanes.read_board()["todos"]
              if x.get("decision") == "ok" and x.get("decided_by") == "web" and n in lanes.todo_prs(x)
              and ("ok_prs" not in x or n in x["ok_prs"])] or [0])
except Exception:
    ok = 0
def ts(c):
    try:
        return datetime.datetime.fromisoformat((c.get("committedDate") or "").replace("Z", "+00:00")).timestamp()
    except ValueError:
        return 0
last = max([ts(c) for c in d.get("commits") or []] or [0])
moved = ok and last > ok and str(n) not in reok
hm = lambda t: datetime.datetime.fromtimestamp(t).strftime("%H:%M:%S") if t else "-"
print("moved" if moved else "ok", head, hm(ok), hm(last))
' "$HERE" "$n" "${REOK:-}" 2>/dev/null) || return 0
  read -r st sha okhm cd <<< "$res"
  [ -n "$sha" ] && [ "$sha" != - ] && MATCH="$sha"
  [ "$st" = moved ] || return 0
  echo "!! #${n}: head ${sha:0:7} has a commit at ${cd}, after the merge OK on the board at ${okhm}: not merged"
  lane alert lane=1 level=warn msg="hub:ship: #${n} の head が merge OK（盤 ${okhm}）の後に変わった（${sha:0:7}、commit ${cd}）。#${n} は merge していない（他は続ける）" \
    impact="差分を見て OK を取り直す（新しいレビューの todo PR=${n} → OK の後に再 ship。タブで取り直したら REOK=\"${n}\" を付けて hub:ship）" >/dev/null
  return 1
}

# Uncommitted changes to tracked files only stop the ship when a fast-forward would touch them (git merge --ff-only
# fails only then; 2026-10-27: an edited file the merge did not touch blocked a ship for 2.5 min). journal / claims are
# the ship's own records: dirty → stop (recording them would mix the hand edit into the ship's commit).
dirty=$(git diff --no-renames --name-only HEAD --)
if [ -n "$dirty" ]; then
  own=$(printf '%s\n' "$dirty" | grep -xE 'docs/(journal|claims)\.md' || true)
  if [ -n "$own" ]; then
    echo "!! uncommitted changes to the ship's own records: $(echo $own)"
    fail warn "ハブの main で $(echo $own) が未 commit: hub:ship が merge の記録をそこに書くので止めた" \
      "merge も deploy もしていない。その変更を commit して git push origin main するか、git stash してから再実行"
  fi
  hit=""
  [ -n "$incoming" ] && hit=$(printf '%s\n' "$dirty" | grep -xF -f <(printf '%s\n' "$incoming") || true)
  if [ -n "$hit" ] || [ -n "$unknown" ]; then
    echo "!! uncommitted changes in the main checkout:"; printf '  %s\n' $dirty
    [ -n "$hit" ] && { echo "   also changed by origin/main or the PRs:"; printf '  %s\n' $hit; }
    [ -n "$unknown" ] && echo "   could not list the files of${unknown} (gh pr diff)"
    fail warn "ハブの main に未 commit の変更があり、merge がそれを書き換える: ${hit:+$(echo $hit)}${unknown:+（PR のファイル一覧を取れない:${unknown}）}" \
      "merge も deploy もしていない。その変更を commit + push するか git stash / 別ブランチへ退けて再実行"
  fi
  echo "(main checkout: uncommitted changes left alone, not touched by this ship: $(echo $dirty))"
fi

ev kind=ship.start prs="$PRS" init="$INIT"
sync_main || fail crit "hub:ship: main の同期（fetch + merge --ff-only）に失敗（main の checkout に手元の commit か変更がある）" "merge も deploy もしていない。git status を見る"
before=$(git rev-parse HEAD)
step sync

for n in $PRS; do
  n="${n#\#}"
  if ! index_gate "$n"; then held="${held} #${n}"; continue; fi
  if ! head_gate "$n"; then moved="${moved} #${n}"; continue; fi
  if gh pr merge "$n" --merge ${MATCH:+--match-head-commit "$MATCH"}; then
    merged="${merged} #${n}"
  else
    skipped="${skipped} #${n}"
    lane alert lane=1 level=warn msg="hub:ship: PR #${n} を merge できない（衝突 / チェック / 権限）。飛ばして残りを続ける" \
      impact="#${n} は今回のベンチに載らない。持ち主に [TASK] <lane> rebase #${n}" >/dev/null
  fi
done
if [ -n "$PRS" ] && [ -z "$merged" ]; then
  fail warn "hub:ship: どの PR も merge できなかった（${skipped:+衝突:${skipped} }${held:+index:check で保留:${held} }${moved:+OK の後に head が変わった:${moved}}）" "deploy していない。衝突は持ち主に rebase、保留は直して index-check: reviewed、head が変わったものは OK を取り直す"
fi

sync_main || fail crit "hub:ship: merge 後の main の同期（fetch + merge --ff-only）に失敗" "deploy していない。git status / git log origin/main を見る"
step merge
changed=$(git diff --name-only "$before" HEAD)

# journal + claims for the merged PRs: one commit per file, pushed to main now (still inside the main sync).
# A failure here never stops the ship: a warn alert says what to add by hand.
record() {
  local n info jl=() marked="" br title msha bodyf i st
  bodyf=$(mktemp)
  for n in $merged; do
    n="${n#\#}"
    if ! gh pr view "$n" --json headRefName,title,body,mergeCommit,files > "$bodyf.json" 2>/dev/null; then
      echo "(journal: gh pr view $n failed; synthesizing the line)"; echo '{}' > "$bodyf.json"
    fi
    info=$(python3 - "$bodyf.json" "$bodyf" <<'PY'
import json, sys
try:
    d = json.load(open(sys.argv[1]))
except Exception:
    d = {}
open(sys.argv[2], "w").write(d.get("body") or "")
open(sys.argv[2] + ".files", "w").write(" ".join((f or {}).get("path", "") for f in d.get("files") or []))
print((d.get("headRefName") or "").replace("\t", " "), (d.get("title") or "").replace("\t", " "),
      ((d.get("mergeCommit") or {}).get("oid") or "")[:7], sep="\t")
PY
)
    IFS=$'\t' read -r br title msha <<< "$info"
    msha="${msha:-$(git rev-parse --short HEAD)}"
    jl+=("$(python3 "$HERE/shiprec.py" journal pr="$n" sha="$msha" branch="${br:-?}" title="$title" body_file="$bodyf")")
    if [ -n "$br" ]; then
      marked="$marked $(python3 "$HERE/shiprec.py" claims branch="$br" before="$before")"
    fi
    # roadmap: the lane steps this PR completes (plan.json "auto" globs on head branch / files) → done "PR #n"
    st=$(lane autostep pr="$n" branch="$br" files="$(cat "$bodyf.files" 2>/dev/null)" 2>/dev/null || true)
    [ -n "$st" ] && [ "$st" != ok ] && echo "roadmap: ${st} done (PR #${n})"
  done
  rm -f "$bodyf" "$bodyf.json" "$bodyf.files"
  marked=$(echo $marked)
  if [ -n "$(git status --porcelain -- docs/journal.md)" ]; then
    git add -- docs/journal.md && git commit -q -m "journal: merged${merged}" -- docs/journal.md || return 1
  fi
  if [ -n "$(git status --porcelain -- docs/claims.md)" ]; then
    git add -- docs/claims.md && git commit -q -m "claims: merged ${marked}" -- docs/claims.md || return 1
  fi
  [ "$(git rev-list --count refs/remotes/origin/main..HEAD)" = 0 ] && return 0
  # someone (a lane's claims row) may have pushed meanwhile: rebase our two commits on it and retry
  for i in 1 2 3; do
    git push -q origin HEAD:main && { echo "journal / claims: pushed (${marked:-no claims rows})"; return 0; }
    git fetch -q origin +refs/heads/main:refs/remotes/origin/main || continue
    git rebase -q --autostash refs/remotes/origin/main || { git rebase --abort; break; }
  done
  return 1
}
if [ -n "$merged" ]; then
  if ! record; then
    # drop only our 2 commits; --keep leaves the hand edits that the ship did not touch (checked above)
    git reset -q --keep refs/remotes/origin/main || echo "!! git reset --keep origin/main failed: git status in the main checkout"
    lane alert lane=1 level=warn msg="hub:ship: journal / claims を main に push できなかった（merge・deploy は続ける）" \
      impact="journal に merge の行（${merged# }）を、claims の行を merged に手で（ship の完了後）" >/dev/null
  fi
fi
step record
sha=$(git rev-parse --short HEAD)
if [ -n "$changed" ] && ! printf '%s\n' "$changed" | grep -qE "$DEPLOY_RE"; then
  mode="docs-only"
  if task ops:repo; then deploy="skipped (ops:repo ok)"; else deploy="skipped (ops:repo FAILED)"; fi
  smoke="skipped"
  todo="none (docs only)"
  step deploy
  ev kind=ship.end ok=1 sec=$(( $(date +%s) - T0 )) merged="${merged# }" mode="$mode"
  summary
  exit 0
fi

if [ "$bstate" = running ] && [ "${FORCE:-0}" != 1 ]; then  # let in as docs-only, but the merged diff has code
  fail warn "hub:ship: ベンチ実行中に docs だけのはずの ship が code を含んでいた（${merged# } merge 済み、deploy していない）" \
    "ベンチが終わったら task hub:ship PRS= （PR なしで main を deploy）"
fi
if task deploy BRANCH=main; then deploy="ok (${sha})"; step deploy; else
  deploy="FAILED"; step deploy 0; fail crit "hub:ship: task deploy main ${sha} が失敗" "途中のホストで止まっている。task host:diag で原因 → 直すか、直前の merge を git revert -m 1"
fi

# No analyzer config yet (kickoff phase 2 writes it, often after the first ships): smoke.mjs runs a minimal probe and
# says so (2026-10-28 00:56: `task smoke` died on ENOENT analyzer.config.json → crit alert, no bench todo).
smoke_note=""
[ -f tools/app-analyzer/analyzer.config.json ] || smoke_note=" (minimal probe: no tools/app-analyzer/analyzer.config.json yet)"
if [ "$INIT" = 1 ]; then task smoke INIT=1; else task smoke; fi
if [ $? -eq 0 ]; then smoke="ok${smoke_note}"; step smoke; else
  smoke="FAILED"; step smoke 0; fail crit "hub:ship: task smoke が失敗（main ${sha}）" "ベンチを回さない。出力の失敗行 → 直前の merge を git revert -m 1 して hub:ship をやり直す"
fi

on="${merged# }"
on="${on:-PR なし（main の再 deploy）}"
on="${on}${moved:+（OK の後に head が変わり保留:${moved}）}"
cmd="${BENCH_CMD:-ポータルでベンチ実行}"
todo=$(lane todo lane=1 msg="ベンチ: main ${sha} — 載っている PR: ${on}${skipped:+（載っていない:${skipped}）}${held:+（index:check で保留:${held}）}${NEXT:+ / 次に回す: ${NEXT}}。終わったら 30 秒待ち、Runs で ended を確かめてから collect → report → score / note" \
  cmd="$cmd" kind=bench why="merge・deploy・smoke 済み" after="終わったら lane:web のこの todo の「🏁 終わった」（score とベンチの出力の最後を貼れる。裏で task bench:done）。panel に score が入ればハブが task runs:wait で拾う")
lane bench state=ready sha="$sha" prs="${merged:--}" next="${NEXT:--}" src=ship >/dev/null
step=$(lane advance ids="H4 H5" note="ship${merged:- main} (${sha})")
[ "$step" = ok ] || echo "roadmap: ${step} done"
step todo
ev kind=ship.end ok=1 sec=$(( $(date +%s) - T0 )) merged="${merged# }" mode="$mode"
summary
