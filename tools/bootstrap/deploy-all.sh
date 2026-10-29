#!/usr/bin/env bash
# task deploy BRANCH=x — deploy BRANCH to every host in HOSTS:
#   1. the DEPLOY_FIRST hosts (space-separated: IP, 1|2|3, svcN or db; default svc3 = 3rd of HOSTS) one by one, in that
#      order (the DB / DNS-DB host must be reconfigured before the app hosts start against it); a failure stops here;
#   2. every other host of HOSTS in parallel (bash background jobs: go-task's own shell mishandles background
#      subshells, HANDOFF §5), each output line prefixed with [host]; waits for all, fails if any failed.
# Then: the old-commit / webapp/sql hint (svc1's previous HEAD vs the new one → task smoke INIT=1), and the duration
# (printed, and as deploy.host / deploy.end events on the lanes board for task lane:timeline).
# Env (the Taskfile passes them inline): HOSTS DB_HOST DEPLOY_FIRST BRANCH SSH_KEY SSH_OPTS HOST_LOGIN HOST_USER.
# Tests / dry runs: DEPLOY_HOST_CMD="<cmd>" replaces `task host:deploy` (called as <cmd> <host> <branch>),
# DEPLOY_SSH replaces ssh for the svc1 commit reads.
set -u
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
cd "$ROOT" || exit 1
BRANCH="${BRANCH:-main}"
SSH_BIN="${DEPLOY_SSH:-ssh}"
HU="${HOST_USER:-isucon}"
R=/home/$HU

ev() { python3 "$ROOT/tools/lanes/lanes.py" event "$@" >/dev/null 2>&1 || true; }
prefix() { local l; while IFS= read -r l || [ -n "$l" ]; do printf '[%s] %s\n' "$1" "$l"; done; }
deploy_one() {  # host
  if [ -n "${DEPLOY_HOST_CMD:-}" ]; then $DEPLOY_HOST_CMD "$1" "$BRANCH"; else task host:deploy HOST="$1" BRANCH="$BRANCH"; fi
}
svc_git() {  # git args on svc1 as HOST_USER
  # shellcheck disable=SC2086
  "$SSH_BIN" -i "${SSH_KEY:-}" ${SSH_OPTS:-} "${HOST_LOGIN:-ubuntu}@$svc1" "sudo -u $HU git -C $R $*" 2>/dev/null
}

set -- ${HOSTS:-}
[ $# -ge 1 ] || { echo "HOSTS is empty in .env"; exit 1; }
svc1=$1
first="${DEPLOY_FIRST:-}"; first="${first:-${3:-}}"
order=""
for f in $first; do
  ip=$(HOSTS="${HOSTS:-}" DB_HOST="${DB_HOST:-}" bash "$ROOT/tools/bootstrap/host-ip.sh" "$f") || exit 1
  case " $order " in *" $ip "*) ;; *) order="$order $ip" ;; esac
done
order="${order# }"
rest=""
for h in "$@"; do case " $order $rest " in *" $h "*) ;; *) rest="$rest $h" ;; esac; done
rest="${rest# }"

# the commit svc1 runs now: after the deploy, svc1 diffs it against the new HEAD (it has both commits)
old=$(svc_git rev-parse HEAD || true)
T0=$(date +%s)
timing=""
for h in $order; do
  echo "=== deploy $BRANCH -> $h (first, sequential)"
  s=$(date +%s)
  deploy_one "$h" 2>&1 | prefix "$h"
  rc=${PIPESTATUS[0]}
  sec=$(( $(date +%s) - s )); timing="$timing $h=${sec}s"
  ev kind=deploy.host host="$h" sec="$sec" ok=$(( rc == 0 )) mode=first branch="$BRANCH"
  if [ "$rc" != 0 ]; then
    echo "!! deploy failed on $h (exit $rc): stopped before the other hosts"
    ev kind=deploy.end ok=0 sec=$(( $(date +%s) - T0 )) branch="$BRANCH" failed="$h"
    exit 1
  fi
done

failed=""
if [ -n "$rest" ]; then
  echo "=== deploy $BRANCH -> $rest (in parallel)"
  tmp=$(mktemp -d)
  pids=()
  for h in $rest; do
    (
      s=$(date +%s)
      deploy_one "$h" </dev/null 2>&1 | prefix "$h"
      rc=${PIPESTATUS[0]}
      echo "$rc $(( $(date +%s) - s ))" > "$tmp/$h"
      exit "$rc"
    ) &
    pids+=($!)
  done
  for p in "${pids[@]}"; do wait "$p"; done
  for h in $rest; do
    read -r rc sec < "$tmp/$h" 2>/dev/null || { rc=1; sec=0; }
    timing="$timing $h=${sec}s"
    ev kind=deploy.host host="$h" sec="$sec" ok=$(( rc == 0 )) mode=parallel branch="$BRANCH"
    [ "$rc" = 0 ] || failed="$failed $h"
  done
  rm -rf "$tmp"
fi
total=$(( $(date +%s) - T0 ))
ev kind=deploy.end ok=$([ -z "$failed" ] && echo 1 || echo 0) sec="$total" branch="$BRANCH" failed="${failed# }"
echo "deploy took ${total}s:${timing}"
if [ -n "$failed" ]; then
  echo "!! deploy failed on:${failed}  (the other hosts are on $BRANCH; task host:diag HOST=<ip>)"
  exit 1
fi
echo "deployed $BRANCH to: $order${order:+ }$rest  (now bench from the portal; the agent records the run)"

# schema / init changes only take effect after /initialize: until then new tables are missing (500s)
if [ -n "$old" ]; then
  sql=$(svc_git diff --name-only "$old" HEAD -- webapp/sql | grep -E '^webapp/sql/([^/]+\.sql|init\.sh)$' || true)
  if [ -n "$sql" ]; then
    printf '\033[1;33m!! this deploy changed %s\n!! run: task smoke INIT=1   (the new schema / init.sh only applies after /initialize; not run automatically)\033[0m\n' "$(echo $sql | tr ' ' ',')"
  fi
else echo "(could not read the previous commit on svc1: check yourself whether webapp/sql/*.sql or init.sh changed -> task smoke INIT=1)"; fi
