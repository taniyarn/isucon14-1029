#!/usr/bin/env python3
"""Runs as the ops panel knows them (score / note typed in the panel or read from bench.log, verdict, next step), so the
hub reads the score itself instead of asking the human to paste it.

  runs.py list [N]                         → last N runs (default 8), one line each, newest last
  runs.py wait [after=<ts>] [timeout=1800]  → only runs with a report AND a score count (mis-detected runs — a smoke
                                             initialize, a crawl — never get a score, so they do not wake the hub). Poll every 15 s until a run newer than <ts> (default: the newest scored run) has a
                                             report AND a score; print it and exit 0 (exit 2 on timeout). The hub runs it
                                             with run_in_background right after the bench todo: the exit wakes the hub.
                                             It also moves the board's bench band (task lane:bench): running while a
                                             new run is going (any host without end_reason in /api/runs; without that
                                             data, a new run with no report), collect once it has ended (req ≥ 1000),
                                             none when the scored report is in. After the human's 「🏁 終わった」 (bench todo)
                                             on lane:web the band is collect and stays there (a host still
                                             without end_reason does not move it back to running) until the
                                             scored report is in. Events for lane:timeline: run_seen (a new run
                                             detected), run_scored (ts, score).

Data: GET /api/timeline of the panel, fetched over ssh + docker exec (same path as task run:report; no Basic auth).
Env: OPS_HOST, SSH_KEY, SSH_OPTS (Taskfile passes them). RUNS_JSON=<file> reads a saved response instead (tests),
RUNS_LIVE_JSON=<file> a saved /api/runs (tests; absent = no live data).
"""
import json
import os
import shlex
import subprocess
import sys
import time

SUSPECT_REQ = 1000  # a real bench is thousands of requests; smoke / a hand-made initialize / app-analysis crawl is a few hundred at most


SEP = "--8<-- api/runs --8<--"


def fetch(live=False):
    """/api/timeline rows; with live=True a tuple (rows, /api/runs groups or None) from the same ssh call."""
    f = os.environ.get("RUNS_JSON")
    if f:
        with open(f) as fh:
            rows = json.load(fh) or []
        if not live:
            return rows
        lf = os.environ.get("RUNS_LIVE_JSON")
        if not lf:
            return rows, None
        with open(lf) as fh:
            return rows, json.load(fh) or []
    get = "docker compose exec -T panel wget -q -O- http://127.0.0.1:8080/api/"
    remote = "cd /opt/isucon-ops/src/ops/compose && %stimeline" % get
    if live:
        remote += " && echo && echo '%s' && { %sruns || true; }" % (SEP, get)
    cmd = ["ssh", "-i", os.path.expanduser(os.environ["SSH_KEY"])] + shlex.split(os.environ.get("SSH_OPTS", "")) + [
        "ubuntu@" + os.environ["OPS_HOST"], remote]
    out = subprocess.run(cmd, capture_output=True, text=True, timeout=60)
    if out.returncode != 0:
        raise RuntimeError("panel /api/timeline failed: " + (out.stderr.strip() or "exit %d" % out.returncode))
    head, _, tail = out.stdout.partition(SEP)
    rows = json.loads(head.strip() or "[]") or []
    if not live:
        return rows
    try:
        groups = json.loads(tail.strip()) if tail.strip() else None
    except ValueError:
        groups = None
    return rows, groups


def live_state(groups, after):
    """running / collect from the panel's /api/runs for runs newer than <after>; '' when nothing new."""
    st = ""
    for g in groups or []:
        if str(g.get("ts", "")) <= after or g.get("ops_only"):
            continue
        hosts = list((g.get("hosts") or {}).values())
        if any(isinstance(h, dict) and not h.get("end_reason") for h in hosts):
            return "running"
        req = sum(int(float(h.get("req_total") or 0)) for h in hosts if isinstance(h, dict))
        if hosts and req >= SUSPECT_REQ:  # ended; a smoke / crawl (few requests) is not a bench
            st = "collect"
    return st


def set_bench(state, **kw):
    """Move the lanes board's bench band (same board file as task lane:bench). Never fails the wait."""
    try:
        import lanes
        lanes.mutate("bench", dict(state=state, **kw))
        return True
    except Exception as e:  # not in a repo / board locked oddly: the wait itself matters more
        print("(lanes board not updated: %s)" % e, flush=True)
        return False


def board_bench():
    """The board's bench band (dict), {} when there is no board."""
    try:
        import lanes
        return lanes.read_board().get("bench") or {}
    except Exception:
        return {}


def bench_step(r):
    """The roadmap's next bench step B<n> → done for a scored run (lanes.py bench_step; a ts already marked is kept)."""
    try:
        import lanes
        lanes.mutate("benchstep", {"ts": r.get("ts", ""), "score": str(r.get("score"))})
    except Exception:
        pass


def event(kind, **kw):
    try:
        import lanes
        lanes.record_event(kind, **kw)
    except Exception:
        pass


def scored(r):
    s = r.get("score")
    try:
        float(str(s))
        return True
    except (TypeError, ValueError):
        return False


def line(r):
    g = r.get("git") or {}
    sha = (g.get("commit") or g.get("sha") or g.get("head") or "")[:7] if isinstance(g, dict) else ""
    score = r.get("score") if scored(r) else "-"
    state = "report" if r.get("verdict") or r.get("next") else ("no report" if r.get("req") else "not collected")
    req = int(r.get("req") or 0)
    parts = [r.get("ts", "?"), "score=%s" % score, "req=%d err=%d" % (req, int(r.get("err") or 0)), state]
    if req < SUSPECT_REQ and not scored(r):
        parts.append("(誤検知? req < %d、score 無し: bench ではない initialize や手の操作。使わない)" % SUSPECT_REQ)
    if sha:
        parts.append("git=" + sha)
    if r.get("verdict"):
        parts.append("verdict: " + str(r["verdict"])[:60])
    if r.get("next"):
        parts.append("next: " + str(r["next"]))
    if r.get("note"):
        parts.append('note: "%s"' % str(r["note"])[:60])
    return "  ".join(parts)


def main(argv):
    if not argv or argv[0] not in ("list", "wait"):
        print(__doc__.strip())
        return 2
    kv = dict(a.split("=", 1) for a in argv[1:] if "=" in a)
    if argv[0] == "wait":
        rows, groups = fetch(live=True)
    else:
        rows, groups = fetch(), None
    rows = sorted(rows, key=lambda r: r.get("ts", ""))
    if argv[0] == "list":
        n = int(next((a for a in argv[1:] if a.isdigit()), "8"))
        if not rows:
            print("no runs on the panel yet")
        for r in rows[-n:]:
            print(line(r))
        return 0
    # default AFTER = the newest SCORED run, not the newest run: a bench that started before this wait (its run already
    # on the panel, no score yet) still wakes it (2026-10-28: the baseline's end went unnoticed for 6 min)
    after = kv.get("after") or next((r["ts"] for r in reversed(rows) if scored(r)), "")
    if not kv.get("after"):
        print("after=%s (default: the newest scored run)" % (after or "-"), flush=True)
    deadline = time.time() + int(kv.get("timeout", "1800"))
    seen, bench, logged = None, "", set()
    while True:
        new = [r for r in rows if r.get("ts", "") > after]
        ready = [r for r in new if scored(r) and (r.get("verdict") or r.get("next"))]
        st = "none" if ready else (live_state(groups, after) or ("running" if new and groups is None else ""))
        if st == "running":
            cur = board_bench()
            if cur.get("state") == "collect" or bench == "collect":
                st = ""  # never back from collect (the human's 「終わった」 or a host flickering): wait for the score
        # compare with the board, not only with what this loop last wrote: a hand-made `task lane:bench` may have moved it
        if st and (st != bench or (board_bench().get("state") or "") != st):
            set_bench(st, src="runs")
            bench = st
        for r in new:
            if r.get("ts") not in logged:
                logged.add(r.get("ts"))
                event("run_seen", ts=r.get("ts", ""), req=int(r.get("req") or 0))
        if ready:
            for r in ready:
                event("run_scored", ts=r.get("ts", ""), score=str(r.get("score")))
                bench_step(r)
            if len(ready) > 1:  # more than one scored run since the bench: the human scored a mis-detection too, or two benches
                print("CHECK: %d scored runs after %s — confirm which one is the bench before using it" % (len(ready), after))
                for r in ready:
                    print("  " + line(r))
            print("NEW RUN  " + line(ready[-1]))
            # the hub's next step (lanes/1-hub.md): the human never runs it (it writes docs/ in the hub's main checkout)
            print("next (hub): task app:refresh TS=%s → commit docs/reports/ only → git push origin main → task ops:repo"
                  " (not while task hub:ship runs)" % ready[-1].get("ts", ""))
            return 0
        if new and new[-1]["ts"] != seen:  # progress for the log: detected / collected, score not in yet
            seen = new[-1]["ts"]
            print("waiting: " + line(new[-1]), flush=True)
        if time.time() > deadline:
            print("timeout: no scored run with a report after %s (collect → report → score in the panel?)" % (after or "start"))
            return 2
        time.sleep(15)
        try:
            rows, groups = fetch(live=True)
            rows = sorted(rows, key=lambda r: r.get("ts", ""))
        except Exception as e:  # ops hiccup: keep waiting
            print("fetch failed (retrying): %s" % e, flush=True)


if __name__ == "__main__":
    try:
        sys.exit(main(sys.argv[1:]))
    except KeyboardInterrupt:
        sys.exit(130)
