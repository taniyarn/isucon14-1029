#!/usr/bin/env bash
# Local preview of the ops panel with dummy data: no ops host, no .env, no real agents.
#   tools/isumeasure/dev/preview.sh            -> http://127.0.0.1:18080 (Ctrl-C stops everything)
#   MEAS=<dir> PORT=<n> tools/isumeasure/dev/preview.sh
# Runs: the practice runs under $MEAS (top-level files only: meta/note/report/summary) plus synthetic older runs.
# Fleet: 3 fake agents (python3 http.server on 127.0.0.1:19101-19103) answering /status, /runs, /tail with random cpu/mem.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
SRC="$(cd "$HERE/.." && pwd)"
PORT="${PORT:-18080}"
MEAS="${MEAS:-$(cd "$SRC/../.." && pwd)/../isucon11-1028/measurements}"
CONTEST=preview
TMP="$(mktemp -d "${TMPDIR:-/tmp}/isupanel-preview.XXXXXX")"
PIDS=()
cleanup() { for p in "${PIDS[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null || true; done; rm -rf "$TMP"; }
trap cleanup EXIT INT TERM

echo "building isumeasure…"
(cd "$SRC" && go build -o "$TMP/isumeasure" .)

D="$TMP/data/$CONTEST/measurements"
mkdir -p "$D"
# real practice runs (if present): small top-level files only, the raw per-host dirs are skipped
if [ -d "$MEAS" ]; then
  for r in "$MEAS"/2*; do
    [ -f "$r/meta.json" ] || continue
    ts="$(basename "$r")"; mkdir -p "$D/$ts"
    for f in meta.json note.json report.html summary.md report.json flows.html; do [ -f "$r/$f" ] && cp "$r/$f" "$D/$ts/"; done
  done
else
  echo "(no $MEAS: synthetic runs only)"
fi

# synthetic older runs + runs.json (the panel's notes) built from the note.json files
python3 - "$TMP/data/$CONTEST" <<'PY'
import json, os, random, sys, datetime as dt
root = sys.argv[1]; meas = os.path.join(root, "measurements")
random.seed(7)
syn = [("20261028-130512", "1820", "baseline"), ("20261028-132930", "4410", "index: isu_condition(jia_isu_uuid, timestamp)"),
       ("20261028-135547", "0", "fail · N+1 rewrite returned 500"), ("20261028-141803", "18250", "N+1 fix in GET /api/trend + pool 100"),
       ("20261028-144120", "41030", "DB moved to svc3; nginx keepalive")]
for ts, score, note in syn:
    t0 = dt.datetime.strptime(ts, "%Y%m%d-%H%M%S").replace(tzinfo=dt.timezone.utc)
    hosts = []
    for i, h in enumerate(["svc1", "svc2", "svc3"]):
        req = 0 if h == "svc3" else random.randint(20000, 160000)
        hosts.append({"host": h, "req_total": req, "err_total": random.choice([0, 0, 3, 41]) if req else 0,
                      "duration_sec": round(random.uniform(60, 75), 1), "end_reason": "bench-done", "follow": h != "svc1",
                      "start": t0.isoformat(), "end": (t0 + dt.timedelta(seconds=70)).isoformat(),
                      "git": {"branch": "main", "commit": "%07x" % random.randrange(16**7), "subject": note}})
    os.makedirs(os.path.join(meas, ts), exist_ok=True)
    json.dump({"ts": ts, "contest": "preview", "hosts": hosts}, open(os.path.join(meas, ts, "meta.json"), "w"))
    n = {"bench_done_at": (t0 + dt.timedelta(seconds=70)).isoformat(), "note": note}
    if note.startswith("fail"):
        n.update({"pass": "false", "score": 0, "score_src": "fail"})
    else:
        n.update({"score": score, "score_src": "typed"})
    json.dump(n, open(os.path.join(meas, ts, "note.json"), "w"))
notes = {}
texts = ["condition: bulk insert", "trend: cache per character", "revert: svc2 split", "isu list LIMIT 1", "pool 200 + interpolateParams"]
for i, ts in enumerate(sorted(os.listdir(meas))):
    p = os.path.join(meas, ts, "note.json")
    if not os.path.exists(p): continue
    n = json.load(open(p))
    n.setdefault("note", texts[i % len(texts)])
    if i == 2: n["bench"] = "score: 4410 (加点 5120 / 減点 710)\n[ERROR] GET /api/isu/:jia_isu_uuid: timeout ×12"
    notes[ts] = n
json.dump(notes, open(os.path.join(root, "runs.json"), "w"), indent=1)
print("runs:", len(notes))
PY

# fake agents: /status, /runs (empty: every run is ops-only), /tail (rps + 120 s of sys samples), POSTs answer {}
cat > "$TMP/agent.py" <<'PY'
import json, math, random, sys, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
port, name = int(sys.argv[1]), sys.argv[2]
rnd = random.Random(port)
base_cpu, base_mem = {"svc1": 55, "svc2": 35, "svc3": 70}[name], {"svc1": 48, "svc2": 30, "svc3": 72}[name]
started = time.time()
def sample(k):
    cpu = max(0, min(97, base_cpu + 30 * math.sin(k / 9) + rnd.uniform(-8, 8)))
    user = cpu * 0.8
    io = max(0, rnd.uniform(-2, 6) + (8 if name == "svc3" and k % 40 < 6 else 0))
    mem = max(0, min(99, base_mem + 4 * math.sin(k / 30) + rnd.uniform(-1, 1)))
    return {"t": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time() - (119 - k % 120))), "cpu_user_pct": round(user, 1),
            "cpu_sys_pct": round(cpu - user, 1), "cpu_iowait_pct": round(io, 1), "cpu_idle_pct": round(max(0, 100 - cpu - io), 1),
            "load1": round(cpu / 25, 2), "mem_avail_mb": int((100 - mem) * 40), "mem_used_pct": round(mem, 1), "disk_free_gb": 21.4}
def series():
    k0 = int(time.time())
    return [sample(k) for k in range(k0 - 119, k0 + 1)]
class H(BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def send(self, v):
        b = json.dumps(v).encode(); self.send_response(200); self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(b))); self.end_headers(); self.wfile.write(b)
    def do_GET(self):
        path = self.path.split("?")[0]
        if path == "/tail":
            self.send({"rps": [rnd.randint(0, 900) for _ in range(60)], "sys": series()})
        elif path == "/runs":
            self.send([])
        elif path == "/status":
            s = series()[-1]
            st = {"version": "preview", "host": name, "contest": "preview", "time": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                  "uptime_sec": int(time.time() - started), "state": "idle", "req_per_s_10s": 0 if name == "svc3" else rnd.randint(0, 40),
                  "sys": s, "disk_free_gb": s["disk_free_gb"], "disk_warning": False, "last_error": "", "paused_until": ""}
            if "full=1" in self.path:
                units = ["isucondition.go", "nginx"] if name != "svc3" else ["mysql"]
                st.update({"services": [{"unit": u + ".service", "active": "active", "enabled": "enabled"} for u in units],
                           "git": {"branch": "main", "commit": "2845eb2", "subject": "journal: bench 7", "dirty_files": 0},
                           "env": {"SVC_NAME": name, "DB_HOST": "192.168.0.13", "DB_PORT": "3306"}, "runs": []})
            self.send(st)
        else:
            self.send_response(404); self.end_headers()
    def do_POST(self):
        self.send({})
    do_DELETE = do_POST
ThreadingHTTPServer(("127.0.0.1", port), H).serve_forever()
PY
for i in 1 2 3; do python3 "$TMP/agent.py" "1910$i" "svc$i" & PIDS+=($!); done

cat > "$TMP/isucon.env" <<EOF
CONTEST=$CONTEST
SVC1=192.168.0.11
SVC2=192.168.0.12
SVC3=192.168.0.13
EOF
mkdir -p "$TMP/repo"
REPO_DIR="$TMP/repo" CODE_DIR="" "$TMP/isumeasure" panel -env "$TMP/isucon.env" -data "$TMP/data" \
  -listen "127.0.0.1:$PORT" -agent-base "http://127.0.0.1:1910" & PIDS+=($!)
for _ in $(seq 1 50); do curl -fs "http://127.0.0.1:$PORT/api/runs" >/dev/null 2>&1 && break; sleep 0.2; done
echo
echo "panel preview: http://127.0.0.1:$PORT   (data: $TMP/data, removed on exit; Ctrl-C to stop)"
wait
