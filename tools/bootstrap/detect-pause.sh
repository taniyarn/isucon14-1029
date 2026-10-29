#!/usr/bin/env bash
# detect-pause.sh on|off — stop / resume run detection on every agent of HOSTS (POST 127.0.0.1:19100/pause), so our own
# POST /api/initialize (task smoke INIT=1, task diffcheck, the app-analysis crawl) does not start a run on the panel.
# Every host, not only the target: the initialize's MySQL load starts a db-triggered run on the DB host too, and the
# panel then starts follow runs on the rest. "on" pauses for PAUSE_MIN minutes (default 2) so a forgotten "off" heals
# itself; "off" resumes at once. Never fails the caller: a host that does not answer is reported and skipped.
# Env (the Taskfile passes them inline): HOSTS SSH_KEY SSH_OPTS HOST_LOGIN PAUSE_MIN.
set -u
case "${1:-}" in
  on) mins="${PAUSE_MIN:-2}" ;;
  off) mins=0 ;;
  *) echo "usage: detect-pause.sh on|off"; exit 2 ;;
esac
set -- ${HOSTS:-}
[ $# -ge 1 ] || { echo "detect-pause: HOSTS is empty in .env"; exit 0; }
for h in "$@"; do
  # shellcheck disable=SC2086
  ( out=$(ssh -i "${SSH_KEY:-}" ${SSH_OPTS:-} -o ConnectTimeout=5 "${HOST_LOGIN:-ubuntu}@$h" \
      "curl -s -m 3 -X POST '127.0.0.1:19100/pause?minutes=$mins'" 2>/dev/null) \
      || out="(no answer)"
    echo "detect-pause $h minutes=$mins $out" ) &
done
wait
exit 0
