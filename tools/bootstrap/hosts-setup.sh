#!/bin/bash
# Contest-day host setup, run by `task hosts:setup` (plain bash on purpose: Task's built-in shell mis-handled
# backgrounded subshells inside a for loop, so two hosts ended up sharing one log and one host was skipped).
#   All hosts start at once:
#     svc1:        bootstrap(init: ... + merge template + push) → agent → ops key
#     svc2, svc3:  bootstrap(join, PHASE=prep: apt off, tools, nginx ltsv, slow log) → agent → ops key
#                  → wait for svc1's push → bootstrap(join, PHASE=git: fetch + reset to origin/main)
#   Only the git step of the other hosts waits for svc1 (2026-10-25: svc1 init 125 s, then the joins another 55 s).
# Env: HOSTS (space separated), plus whatever `task host:*` needs from .env (task is called for each step).
#      PRACTICE / HOST_LOGIN / HOST_USER / SSH_KEY / BENCH_CMD: only for the practice bench-command hint at the end.
set -u
HOSTS=${HOSTS:-}
[ -n "$HOSTS" ] || { echo "HOSTS is empty (.env)"; exit 1; }
set -- $HOSTS
first=$1; shift
HERE=$(cd "$(dirname "$0")" && pwd)
t0=$(date +%s); say() { echo "[$(( $(date +%s) - t0 ))s] $*"; }
W=$(mktemp -d /tmp/isucon-setup.XXXXXX)   # init.ok / init.ng markers: the joins' git step waits on them
trap 'rm -rf "$W"' EXIT
INIT_WAIT=${INIT_WAIT:-600}
log() { echo "/tmp/isucon-setup-$1.log"; }
plain() { awk '{gsub(/\033\[[0-9;]*m/, ""); print}' "$1" 2>/dev/null; }   # host:bootstrap output is colourised
# stage <name> <cmd...>: run one step, record "@@ stage <name> <s> rc=<rc>" in the host log (read by the timing summary)
stage() { local n=$1 s rc; shift; s=$(date +%s); "$@"; rc=$?; echo "@@ stage $n $(( $(date +%s) - s ))s rc=$rc"; return $rc; }

say "=== all hosts at once: $first = init + agent + ops key; $* = join prep + agent + ops key, then git once $first has pushed (logs: /tmp/isucon-setup-<ip>.log)"
pids=""; names=""
(
  if stage bootstrap task host:bootstrap HOST="$first" ROLE=init; then touch "$W/init.ok"; else touch "$W/init.ng"; exit 1; fi
  stage agent task host:agent HOST="$first" && stage ops-key task host:authorize-ops HOST="$first"
) > "$(log "$first")" 2>&1 &
pids="$pids $!"; names="$names $first"
for h in "$@"; do
  (
    stage prep task host:bootstrap HOST="$h" ROLE=join PHASE=prep || echo "NG   join prep failed on $h (agent / git still run)"
    stage agent task host:agent HOST="$h" && stage ops-key task host:authorize-ops HOST="$h"
    rc=$?
    s=$(date +%s)
    while [ ! -e "$W/init.ok" ] && [ ! -e "$W/init.ng" ] && [ $(( $(date +%s) - s )) -lt "$INIT_WAIT" ]; do sleep 1; done
    echo "@@ stage wait-init $(( $(date +%s) - s ))s rc=0"
    if [ -e "$W/init.ok" ]; then stage git task host:bootstrap HOST="$h" ROLE=join PHASE=git || rc=1
    else echo "NG   $first init did not finish (failed or >${INIT_WAIT}s); git join skipped on $h - after fixing $first: task host:bootstrap HOST=$h ROLE=join"; rc=1; fi
    exit $rc
  ) > "$(log "$h")" 2>&1 &
  pids="$pids $!"; names="$names $h"
done
fail=0
n=0
for p in $pids; do
  n=$((n+1)); h=$(echo $names | cut -d' ' -f$n)
  if wait "$p"; then say "$h finished"; else say "$h FAILED (see below)"; fail=1; fi
done
if [ ! -e "$W/init.ok" ]; then
  plain "$(log "$first")" | grep -E "^(NG|##|task: Failed)" | grep -v '^## timing' | tail -25 | sh "$HERE/colorize.sh"
  echo "init bootstrap failed on $first; fix it (RUNBOOK §2) and re-run"; exit 1
fi

ngs=0
for h in $first "$@"; do
  echo "--- $h"
  plain "$(log "$h")" | grep -E "^(NG|##|task: Failed)|^authorized$|agent .*(installed|active)" | grep -v '^## timing' | tail -25 | sh "$HERE/colorize.sh"
  c=$(plain "$(log "$h")" | grep -c '^NG'); ngs=$((ngs + ${c:-0}))
done

# one line per host: wall-clock per stage, the bootstrap sections in brackets (the slow one shows at a glance)
echo "--- timing (per host)"
i=0
for h in $first "$@"; do
  i=$((i+1))
  plain "$(log "$h")" | awk -v name="svc$i ($h)" '
    /^## timing / { bs = $0; sub(/^.*: total [0-9]+s \(/, "", bs); sub(/\)$/, "", bs) }
    /^@@ stage / { st = st (st ? " | " : "") $3 " " $4 (($5 == "rc=0") ? "" : " FAILED") (bs != "" ? " (" bs ")" : ""); bs = ""; tot += $4 + 0 }
    END { printf "%s: total %ds — %s\n", name, tot, st }'
done

say "=== ops:env"
task ops:env

# ---------- practice only: the bench command (svc2 runs the bench, RUNBOOK 付録). Printed, never written to .env ----------
bench_hint() {
  [ "$#" -ge 1 ] || { echo "   (one host only: no svc2 to run the bench from)"; return 0; }
  local svc2=$1 key=${SSH_KEY:-} login=${HOST_LOGIN:-ubuntu} user=${HOST_USER:-isucon}
  local o="-o StrictHostKeyChecking=accept-new -o ConnectTimeout=10" k info priv app dom target cmd
  [ -n "$key" ] || return 0
  k=${key/#\~/$HOME}
  # svc1: private IP | isupipe unit (= ISUCON13) | the practice domain from nginx (u.isucon.<tld>; the AMI changes .dev)
  info=$(ssh -i "$k" $o "$login@$first" 'printf "%s|%s|%s\n" "$(hostname -I | cut -d" " -f1)" "$(systemctl list-unit-files --no-legend "isupipe*" 2>/dev/null | head -1 | cut -d" " -f1)" "$(sudo -n grep -rhoE "u\.isucon\.[a-z]+" /etc/nginx/sites-enabled /etc/nginx/conf.d 2>/dev/null | sort | uniq -c | sort -rn | head -1 | awk "{print \$2}")"' 2>/dev/null)
  priv=$(echo "$info" | cut -d'|' -f1); app=$(echo "$info" | cut -d'|' -f2); dom=$(echo "$info" | cut -d'|' -f3)
  [ -n "$info" ] || { echo "   (could not ssh to $first; BENCH_CMD not suggested)"; return 0; }
  [ -n "$app" ] || { echo "   (not ISUCON13 - no isupipe unit on $first: write BENCH_CMD by hand from RUNBOOK 付録)"; return 0; }
  echo "$priv" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' || { echo "   (could not read the private IP of $first; BENCH_CMD not suggested)"; return 0; }
  [ "$(ssh -i "$k" $o "$login@$svc2" "test -x /home/$user/bench/bench && echo yes" 2>/dev/null)" = yes ] || { echo "   (no /home/$user/bench/bench on $svc2; BENCH_CMD not suggested)"; return 0; }
  if [ -n "$dom" ]; then target="https://pipe.$dom"; else target="<target>"; fi
  cmd="ssh -t -i $key $login@$svc2 'sudo -u $user -i bash -c \"cd ~/bench && ./bench run --target $target --enable-ssl --nameserver $priv --webapp $priv 2>&1 | tee /tmp/bench.out\"'"
  echo "   ISUCON13 practice, bench from svc2 against svc1 ($priv). Paste into .env (the hub's bench todo uses it):"
  [ -n "$dom" ] || echo "   !! no u.isucon.<tld> in svc1's nginx: replace <target> (e.g. https://pipe.u.isucon.local)"
  printf '\033[1mBENCH_CMD=%s\033[0m\n' "$cmd"
}
if { [ -n "${PRACTICE:-}" ] && [ "${PRACTICE:-}" != 0 ]; } || [ "${HOST_LOGIN:-}" = ubuntu ]; then
  echo "--- practice bench command"
  [ -z "${BENCH_CMD:-}" ] || echo "   BENCH_CMD already set in .env: $BENCH_CMD"
  bench_hint "$@"
fi

if [ $fail = 1 ] || [ "$ngs" -gt 0 ]; then
  printf "\033[1;31m!! %s NG line(s) / failed step(s) — see above and /tmp/isucon-setup-<ip>.log (RUNBOOK §2)\033[0m\n" "$ngs"
  [ $fail = 1 ] && exit 1
else
  printf "\033[1;32mdone in %ss, 0 NG on all hosts\033[0m\n" "$(( $(date +%s) - t0 ))"
  # roadmap: I2 (hosts:setup 0 NG) and I1 (peek, its prerequisite) → done unless already done / skip; never fails
  python3 "$HERE/../lanes/lanes.py" advance ids=I1 note="hosts:setup 0 NG" >/dev/null 2>&1 || true
  python3 "$HERE/../lanes/lanes.py" advance ids=I2 note="0 NG" >/dev/null 2>&1 || true
  # the baseline is the undeployed initial state: it needs only the measurement set up above, not deploy:init or the
  # hub's [DONE] servers turn (2026-10-28: those held ready 2–3.5 min). Once per board; never fails the setup.
  git fetch -q origin 2>/dev/null
  sha=$(git rev-parse --short origin/main 2>/dev/null)
  if out=$(python3 "$HERE/../lanes/lanes.py" baseline sha="$sha" 2>&1); then
    case $out in
      skip*) echo "baseline: $out" ;;
      *) printf "\033[1;32mbaseline: band ready, todo %s (main %s) — lane 2: send [DONE] 2 baseline-ready %s main %s to the hub now\033[0m\n" "$out" "$sha" "$out" "$sha" ;;
    esac
  else
    echo "!! baseline: board not updated ($out) — the hub creates the baseline todo on [DONE] servers"
  fi
fi
