#!/usr/bin/env python3
"""lane:web — the lanes board in a browser, with copy / done / resolve buttons.

Listens on 127.0.0.1 only. GET / (the page), GET /board.json, POST /done?id=t4, POST /resolve?id=a3,
POST /decide?id=t5&decision=ok|changes[&held=8,9] (a review todo, one or several PRs: 「✅ まとめて merge OK」 with the PRs
toggled off in held, 「✋ 要修正」) or decision=<choice text> (a todo made with choices="a|b|c": one button per choice);
/decide needs the X-Lanes-Token header.
POST /bench-done?id=t9 with a form body score=<n>&output=<pasted tail of the bench output> (both optional; 「🏁 終わった」
on the bench todo — KIND=bench, made by hub:ship; same token): the todo closes (decision=done, lane:wait also prints
score=<n>), the band (kept internally for hub:ship / runs:wait / lane:board, no longer drawn here) goes to collect, a
bench_done event is logged, the output is saved next to the board and `task bench:done [SCORE=<n>] [OUTPUT_FILE=<file>]`
runs in the background (LANES_TASK overrides the task binary, for tests); its log goes next to the board, a failure
into 「📜 経緯」.
board.json also carries 「📜 経緯」 as cards (lanes.story_cards(): one per bench, newest first, the latest 2 open — PRs on
it, 律速, 効果 ([DONE] … effect), the human's decisions, crit alerts; 「準備」 before the first bench) and as rows for
「すべて表示」 (lanes.story(): lane:note lines (with an optional ≤ 5-line body, ▸ expands it) + the human's decisions,
alerts, grouped per bench by the run_scored / 「終わった」 events, filter per KIND), and 「📌 いまの要点」
(lanes.highlights(): 6 fixed slots, a placeholder when empty — 🧭 kickoff app note + app-map headline, 🧮 score
formula, ⛔ 禁止 (kickoff note BODY, else docs/kickoff.md), 🔥 latest bottleneck (old after a newer bench), 📈 score
with Δ, 🙋 open decisions + latest decision note / human decision).
POST /input?id=t3 with a form body v=<answer 1>&v=<answer 2>… (a todo made with input="label1|label2": one text field per
label) stores the answers and closes the todo; an envkey todo (e.g. HOSTS) also rewrites that line of .env (this process
does it, cwd = repo root; LANES_ENV_FILE overrides). /input needs the same token as /decide.
board.json also carries the roadmap (plan.json + lane:step + the ops panel's scored runs, refreshed every 60 s in the
background through runs.py; RUNS_JSON=<file> for tests; no ops env = roadmap without bench auto-ticks).
POSTs need the header X-Lanes: 1 (the page sets it; a form on another site cannot), so nothing else can tick todos off.

The review decision (ok / changes / a choice) is the human's merge OK or answer, so /decide also needs a token made at startup
(LANES_TOKEN_FILE, default ~/.cache/isucon-lanes/web-token, mode 600, outside the board) that only the page carries.
This is NOT a real defence: a process running as the same user can read the token file or GET / and lift it from the
page. It keeps a Claude session from writing a decision through the normal commands (lane:done writes decision=done);
the sessions' settings deny curl to this port, and the hub notes "OK: 盤 hh:mm" in the journal.
"""
import hmac
import secrets
import json
import os
import re
import subprocess
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

import lanes

CARD_W, CARD_H, COL_W, ROW_PAD, GAP, LEFT = 176, 52, 204, 14, 10, 118
ROW_LABELS = {"1": "1 ハブ", "2": "2 インフラ", "3": "3 アプリ", "c": "c スカウト", "bench": "🏁 ベンチ"}


class RunsCache:
    """Scored runs from the ops panel, refreshed in the background so a slow ssh never blocks the page."""

    def __init__(self, every=60):
        self.every, self.runs, self.err, self.at, self.lock = every, None, "", 0, threading.Lock()

    def enabled(self):
        return bool(os.environ.get("RUNS_JSON") or (os.environ.get("OPS_HOST") and os.environ.get("SSH_KEY")))

    def refresh(self):
        if not self.enabled():
            self.err = "ops 未設定（.env の OPS_HOST / SSH_KEY）: ベンチの step は手で lane:step"
            return
        try:
            import runs
            rows = runs.fetch()
            with self.lock:
                self.runs, self.err, self.at = rows, "", time.time()
        except Exception as e:  # ssh / panel down: keep the last good list
            self.err = f"ops の run を読めない: {e}"[:200]

    def loop(self):
        while True:
            self.refresh()
            time.sleep(self.every)

    def start(self):
        threading.Thread(target=self.loop, daemon=True).start()


RUNS = RunsCache()
MAX_BODY = 16384  # POST /input: a few fields of ≤ 500 chars
MAX_BENCH_BODY = 1 << 20  # POST /bench-done: ≤ 64 KiB of output, URL-encoded (Japanese is 9 bytes a character)
TOKEN = ""


def token_file():
    return os.environ.get("LANES_TOKEN_FILE") or os.path.join(
        os.environ.get("XDG_CACHE_HOME") or os.path.expanduser("~/.cache"), "isucon-lanes", "web-token")


def init_token(write=True):
    """A fresh token per lane:web start; written 600 next to nothing else of the board (see the module doc)."""
    global TOKEN
    TOKEN = secrets.token_urlsafe(24)
    if write:
        f = token_file()
        os.makedirs(os.path.dirname(f), mode=0o700, exist_ok=True)
        fd = os.open(f, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        with os.fdopen(fd, "w") as fh:
            fh.write(TOKEN + "\n")
        os.chmod(f, 0o600)
    return TOKEN


def app_url():
    """The ops panel's app-analysis page (/app/): PANEL_URL, else https://<OPS_HOST dashed>.sslip.io (as the Taskfile)."""
    base = os.environ.get("PANEL_URL", "").strip().rstrip("/")
    if not base and os.environ.get("OPS_HOST", "").strip():
        base = "https://" + os.environ["OPS_HOST"].strip().replace(".", "-") + ".sslip.io"
    return base + "/app/" if base else ""


def task_bin():
    return os.environ.get("LANES_TASK") or "task"


def has_bench_done():
    """Whether this repo's Taskfile has bench:done (the kit of an older contest repo may not)."""
    try:
        out = subprocess.run([task_bin(), "--list"], capture_output=True, text=True, timeout=30).stdout
    except (OSError, subprocess.TimeoutExpired):
        return False
    return bool(re.search(r"^\*\s+bench:done:", out, re.M))


def run_bench_done(score, outfile, done_at):
    """Background: task bench:done [SCORE=] [OUTPUT_FILE=], log next to the board, outcome onto the band (if the band is
    still on that 「終わった」)."""
    def note(text):
        try:
            if (lanes.read_board().get("bench") or {}).get("done_at") == done_at:
                lanes.bench_note(text)
        except Exception:
            pass
    try:
        d = lanes.lanes_dir()
        log = os.path.join(d, time.strftime("bench-done-%Y%m%d-%H%M%S.log", time.localtime(done_at)))
        if not has_bench_done():
            note("!! task bench:done が無い（この repo の kit が古い）: panel で collect → report → score を手で")
            lanes.record_event("bench_done_task", ok=0, err="no bench:done")
            return
        args = [task_bin(), "bench:done"] + ([f"SCORE={score}"] if score else []) + ([f"OUTPUT_FILE={outfile}"] if outfile else [])
        t0 = time.time()
        with open(log, "w") as lf:
            p = subprocess.Popen(args, stdout=lf, stderr=subprocess.STDOUT, stdin=subprocess.DEVNULL, start_new_session=True)
            note(f"task bench:done を実行中（log: {log}）")
            rc = p.wait()
        note(f"task bench:done {'ok' if rc == 0 else '失敗 (exit %d)' % rc}（log: {log}）")
        lanes.record_event("bench_done_task", ok=int(rc == 0), sec=int(time.time() - t0))
    except Exception as e:  # never take lane:web down
        note(f"!! task bench:done を起動できない: {e}"[:300])


def bench_done(score, output, tid=""):
    """POST /bench-done?id=<bench todo>: todo closed, band → collect (+ event), output saved next to the board,
    bench:done in the background."""
    bn = lanes.bench_done(score, output, tid=tid)
    outfile = ""
    if (output or "").strip():
        outfile = os.path.join(lanes.lanes_dir(), time.strftime("bench-output-%Y%m%d-%H%M%S.txt", time.localtime(bn["done_at"])))
        with open(outfile, "w") as f:
            f.write(output if output.endswith("\n") else output + "\n")
    th = threading.Thread(target=run_bench_done, args=(bn.get("done_score", ""), outfile, bn["done_at"]), daemon=True)
    th.start()
    return th


def layout(rm):
    """Positions for the swimlane SVG: rows = lanes (ROW_ORDER, then any other), columns = stage."""
    steps = rm["steps"]
    rows = [r for r in lanes.ROW_ORDER if any(x["lane"] == r for x in steps)]
    rows += sorted({x["lane"] for x in steps} - set(rows))
    ncol = max((x["stage"] for x in steps), default=0) + 1
    pos, row_boxes, y = {}, [], 0
    for r in rows:
        cells = {}
        for x in steps:
            if x["lane"] == r:
                cells.setdefault(x["stage"], []).append(x["id"])
        n = max((len(v) for v in cells.values()), default=1)
        h = n * (CARD_H + GAP) - GAP + 2 * ROW_PAD
        for stage, ids in cells.items():
            for i, sid in enumerate(ids):
                pos[sid] = {"x": LEFT + stage * COL_W, "y": y + ROW_PAD + i * (CARD_H + GAP)}
        row_boxes.append({"lane": r, "label": ROW_LABELS.get(r, r), "y": y, "h": h})
        y += h
    by = {x["id"]: x for x in steps}
    crit = set(rm["critical"])
    edges = []
    for x in steps:
        for d in x["deps"]:
            if d in pos and x["id"] in pos:
                edges.append({"from": d, "to": x["id"], "done": by[d]["state"] in ("done", "skip"),
                              "critical": d in crit and x["id"] in crit})
        for d in x.get("soft") or []:
            if d in pos and x["id"] in pos:
                edges.append({"from": d, "to": x["id"], "done": by[d]["state"] in ("done", "skip"),
                              "critical": False, "soft": True})
    return {"pos": pos, "rows": row_boxes, "edges": edges, "w": LEFT + ncol * COL_W, "h": y,
            "card_w": CARD_W, "card_h": CARD_H, "stages": ncol}


LONGPOLL_S = float(os.environ.get("LANES_LONGPOLL_S") or 25)


def board_version():
    """Changes whenever the page's data can change: board.json rewritten (every lane:* mutate), the runs cache refreshed."""
    try:
        st = os.stat(os.path.join(lanes.lanes_dir(), "board.json"))
        v = f"{st.st_mtime_ns}-{st.st_size}"
    except OSError:
        v = "none"
    try:  # runs:wait / ship.sh / bench:done log events without touching board.json: 「📜 経緯」 must still refresh
        st = os.stat(lanes.events_path())
        v += f"-{st.st_size}"
    except OSError:
        pass
    return f"{v}-{int(RUNS.at or 0)}"


def payload():
    b = lanes.read_board()
    d = lanes.board_json(b, lanes.now())
    with RUNS.lock:
        runs_rows = RUNS.runs
    target = lanes.prize_target()
    rm = lanes.roadmap(b, runs=runs_rows, target=target)
    rm["layout"] = layout(rm)
    rm["runs_err"] = RUNS.err
    rm["runs_at"] = time.strftime("%H:%M:%S", time.localtime(RUNS.at)) if RUNS.at else ""
    d["roadmap"] = rm
    d["story"] = lanes.story()  # groups per bench, newest first
    d["highlights"] = lanes.highlights(b, d["story"], runs=runs_rows, target=target)
    d["cards"] = lanes.story_cards(d["story"])  # one card per bench, newest first (「すべて表示」 = the rows above)
    d["note_kinds"] = lanes.NOTE_KINDS
    return d

PAGE = r"""<!doctype html>
<html lang="ja"><head><meta charset="utf-8"><title>🏁 ISUCON lanes</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
:root{--bg:#0d1020;--card:#171b33;--line:#2a3058;--text:#e9ecff;--dim:#8a91c0;--l1:#ff6bcb;--l2:#4fd1ff;--l3:#7dff9b;--lc:#ffd166}
*{box-sizing:border-box}body{margin:0;font:15px/1.5 -apple-system,"Hiragino Sans",system-ui,sans-serif;color:var(--text);
background:radial-gradient(1200px 600px at 10% -10%,#3a1d6e55,transparent),radial-gradient(900px 500px at 110% 10%,#0e5a7a55,transparent),var(--bg);min-height:100vh}
header{display:flex;align-items:center;gap:14px;padding:18px 26px}
h1{margin:0;font-size:22px;letter-spacing:.5px}
.phase{padding:4px 12px;border-radius:999px;font-weight:700;background:linear-gradient(90deg,#ff6bcb,#7b61ff);color:#fff;text-transform:uppercase}
.clock{margin-left:auto;color:var(--dim);font-variant-numeric:tabular-nums}
main{padding:0 26px 30px;display:grid;gap:22px}
.lanes{display:grid;grid-template-columns:repeat(auto-fit,minmax(230px,1fr));gap:14px}
.lane{background:var(--card);border:1px solid var(--line);border-radius:16px;padding:14px 16px;position:relative;overflow:hidden}
.lane:before{content:"";position:absolute;inset:0 0 auto 0;height:4px;background:var(--c)}
.lane .top{display:flex;align-items:center;gap:8px}.lane .name{font-weight:800;font-size:17px}
.lane .state{margin-left:auto;font-size:12px;padding:2px 9px;border-radius:999px;background:#ffffff14}
.lane .step{margin-top:8px;font-size:14px}.lane .note{color:var(--dim);font-size:13px}
.lane.alert{box-shadow:0 0 0 2px #ff4d6d,0 0 30px #ff4d6d55}.lane.stuck{box-shadow:0 0 0 2px #ffb703,0 0 24px #ffb70355}
.lane.working .icon{animation:pulse 1.6s infinite}@keyframes pulse{50%{opacity:.35}}
h2{margin:0 0 10px;font-size:17px}.empty{color:var(--dim)}
.item{background:var(--card);border:1px solid var(--line);border-radius:14px;padding:12px 14px;margin-bottom:10px;display:grid;gap:6px}
.item .meta{color:var(--dim);font-size:12px}.item .msg{font-weight:600}
pre{margin:0;padding:9px 11px;border-radius:10px;background:#0a0d1c;border:1px solid var(--line);white-space:pre-wrap;word-break:break-all;font:13px/1.45 ui-monospace,Menlo,monospace;color:#b8f7ff}
.row{display:flex;gap:8px;flex-wrap:wrap;align-items:center}
button{border:0;border-radius:10px;padding:7px 13px;font-weight:700;cursor:pointer;color:#0d1020;background:#7dff9b;transition:transform .08s}
button:active{transform:scale(.96)}button.copy{background:#4fd1ff}button.res{background:#ffd166}
.crit{border-color:#ff4d6d}.crit .lv{background:#ff4d6d;color:#fff}.warn .lv{background:#ffb703;color:#111}
.lv{font-size:11px;font-weight:800;padding:1px 8px;border-radius:999px}
.hint{color:var(--dim);font-size:13px}.toast{position:fixed;right:22px;bottom:22px;background:#7dff9b;color:#0d1020;font-weight:800;padding:10px 16px;border-radius:12px;opacity:0;transition:opacity .2s}
.toast.on{opacity:1}.cols{display:grid;grid-template-columns:repeat(auto-fit,minmax(380px,1fr));gap:22px}
.rm{background:var(--card);border:1px solid var(--line);border-radius:18px;padding:16px 18px;display:grid;gap:14px}
.rmhead{display:flex;flex-wrap:wrap;gap:18px;align-items:center}
.scorebox{display:flex;gap:18px;align-items:center;flex-wrap:wrap}
.big{font-size:26px;font-weight:900;font-variant-numeric:tabular-nums}.lbl{color:var(--dim);font-size:12px}
.bar{position:relative;height:16px;width:340px;border-radius:999px;background:#ffffff12;overflow:hidden}
.bar>i{position:absolute;inset:0 auto 0 0;background:linear-gradient(90deg,#7b61ff,#ff6bcb,#ffd166);border-radius:999px}
.bar>b{position:absolute;right:8px;top:-1px;font-size:12px}
.prog{margin-left:auto;color:var(--dim)}
.swim{overflow-x:auto;border-radius:12px;background:#0a0d1c;border:1px solid var(--line)}
.swim svg{display:block;width:100%;min-width:1100px;height:auto}
.card rect{stroke-width:1.5}.card text{font-size:12px;fill:#e9ecff}.card .sid{font-weight:800;font-size:11px}
.st-done rect{fill:#1f2130;stroke:#3c4058}.st-done text{fill:#7d829f}.st-done{opacity:.75}.st-doing rect{fill:#10294a;stroke:#4fd1ff}.st-ready rect{fill:#2e2810;stroke:#ffd166;stroke-dasharray:5 3}
.st-blocked rect{fill:#1e1433;stroke:#8b6fd6;stroke-dasharray:6 4}.st-blocked text{fill:#b7a6ea}.st-skip rect{fill:#141625;stroke:#30344f}.st-skip text{fill:#5d628a;text-decoration:line-through}
.st-doing rect{animation:glow 1.4s ease-in-out infinite}@keyframes glow{50%{stroke-width:4;filter:drop-shadow(0 0 6px #4fd1ff)}}
.card .trophy{font-size:12px}.card.sel rect{stroke:#fff;stroke-width:3}
.card{cursor:pointer}
.edge{fill:none;stroke:#8a91c0;stroke-width:1.6;opacity:.9}.edge.done{opacity:.22}.edge.crit{stroke:#ffcc33;stroke-width:3;opacity:1}.edge.crit.done{opacity:.45}.edge.soft{stroke:#5eead4;stroke-dasharray:5 5;stroke-width:1.6;opacity:.85}.edge.soft.done{opacity:.25}
.detail{min-height:46px;color:var(--text);font-size:14px}.detail .k{color:var(--dim)}
.lists{display:grid;grid-template-columns:repeat(auto-fit,minmax(230px,1fr));gap:12px}
.tk{margin-top:8px;padding-top:6px;border-top:1px dashed #ffffff1a}.tkh{font-size:13px;display:flex;gap:6px;align-items:center}.tkn{margin-left:auto;font-weight:800;color:var(--c)}
.tkb{height:5px;border-radius:999px;background:#ffffff12;margin:5px 0;overflow:hidden}.tkb>i{display:block;height:100%;background:var(--c);transition:width .4s}
.tk ul{list-style:none;margin:0;padding:0;font-size:12.5px}.tk li{padding:1px 0}.tk li.done,.tk li.skip{color:#7c82ad}.tk li.doing{color:#fff;font-weight:700}
.agt{font-size:11px;font-weight:700;padding:1px 7px;border-radius:999px;margin-left:6px;background:#ffffff10;border:1px solid #8a91c0;color:#c9cdf0}.agt.codex{border-color:#7dff9b;color:#7dff9b}
.unread{font-size:12px;font-weight:800;margin-left:6px;color:#ffd166}.msgs{margin-top:6px;border-top:1px dashed #ffffff22;padding-top:4px;font-size:12px;color:#c9cdf0}.msgs .m{white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.msgs .m.ur{color:#ffd166;font-weight:700}
.agents{font-size:12px;font-weight:700;padding:2px 8px;border-radius:999px;background:#ffffff14;border:1px solid var(--c);margin-left:6px;animation:pulse 1.6s ease-in-out infinite}
.ll{background:#0f1328;border:1px solid var(--line);border-radius:14px;padding:10px 12px}
.ll h3{margin:0 0 6px;font-size:14px;display:flex;align-items:center;gap:8px}.ll h3 .pc{margin-left:auto;color:var(--dim);font-weight:600}
.ll .pb{height:6px;border-radius:999px;background:#ffffff12;margin-bottom:8px;overflow:hidden}.ll .pb>i{display:block;height:100%;background:var(--c)}
.ll .s{font-size:13px;padding:2px 0;display:flex;gap:6px}.ll .s .w{color:var(--dim);font-size:11px;margin-left:auto}
.ll .s.done{color:#7c82ad}.warnline{color:#ffb703;font-size:12px}
.applink{color:#b8f7ff;font-weight:700;text-decoration:none;padding:4px 12px;border:1px solid #4fd1ff;border-radius:999px}.applink:hover{background:#4fd1ff22}
.prlink{color:#b8f7ff;font-weight:800}button.ok{background:#7dff9b}button.chg{background:#ff8fa3}
.dec{font-size:13px;color:var(--dim);margin-top:4px}.dec b.ok{color:#7dff9b}.dec b.changes{color:#ff8fa3}.dec b.ch{color:#ffd166}
.tid{display:inline-block;font:900 20px/1 ui-monospace,Menlo,monospace;padding:5px 11px;border-radius:10px;background:#ffd166;color:#0d1020;margin-right:8px;vertical-align:middle;letter-spacing:.5px}
button.choice{background:#ffd166}button.send{background:#b69bff}
.inp{display:grid;gap:6px}.inp label{display:grid;gap:2px;font-size:12px;color:var(--dim)}
.inp input{font:14px ui-monospace,Menlo,monospace;padding:7px 10px;border-radius:9px;border:1px solid var(--line);background:#0a0d1c;color:var(--text);width:100%}
.prl{display:grid;gap:4px;margin:2px 0}.prl label{display:flex;gap:8px;align-items:center;font-size:14px}.prl label.off{opacity:.45;text-decoration:line-through}
.prl input{width:18px;height:18px}.prl .pt{color:var(--dim)}
button.bdone{background:#ffd166}.bform{display:none;gap:8px;padding:10px 12px;border-radius:12px;border:1px solid #ffd166;background:#2e281055}
.bform.on{display:grid}.bform input,.bform textarea{font:14px ui-monospace,Menlo,monospace;padding:7px 10px;border-radius:9px;border:1px solid var(--line);background:#0a0d1c;color:var(--text);width:100%}
.bform textarea{min-height:90px}
.story{background:var(--card);border:1px solid var(--line);border-radius:18px;padding:14px 16px}
.story .chips{display:flex;gap:6px;flex-wrap:wrap;margin-bottom:8px}.chip{background:#ffffff14;color:var(--text);font-weight:600;font-size:12.5px;padding:3px 10px;border-radius:999px}
.chip.on{background:#b69bff;color:#0d1020}.sg{font-size:12px;color:var(--dim);font-weight:800;margin:8px 0 2px;border-bottom:1px dashed #ffffff1a}
.sr{display:flex;gap:8px;align-items:baseline;font-size:13.5px;padding:1px 0;white-space:nowrap;overflow:hidden}.sr .tm{color:var(--dim);font-variant-numeric:tabular-nums;font-size:12px}
.sr .lb{font-size:11px;font-weight:900;padding:0 7px;border-radius:999px;background:var(--c);color:#0d1020}.sr .tx{overflow:hidden;text-overflow:ellipsis}
.sr .rf{color:var(--dim);font-size:12px}.sr.auto .tx{color:#c9cdf0}.sr.hasb{cursor:pointer}.sr .mk{width:10px;color:#ffd166;font-size:11px}
.sr.new{box-shadow:inset 3px 0 0 #ff6bcb;background:#ff6bcb14;padding-left:6px}.sr .nd{font-size:10px;font-weight:900;color:#ff6bcb}
.ub{font-size:11px;font-weight:900;padding:1px 8px;border-radius:999px;background:#ff6bcb;color:#0d1020;vertical-align:middle}
.sb{margin:0 0 4px 64px;padding:4px 10px;border-left:2px solid #ffd166;font-size:13px;color:#d9dcf5;white-space:pre-wrap}
.bg{margin-top:8px}.bgh{cursor:pointer;font-size:13px;font-weight:800;color:#ffd166;padding:3px 0;border-bottom:1px dashed #ffffff22}.bgh .n{color:var(--dim);font-weight:600}
.hl{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:10px;margin-bottom:10px}@media(max-width:1100px){.hl{grid-template-columns:repeat(2,minmax(0,1fr))}}
.hc .ph{color:var(--dim);font-size:12.5px;font-style:italic}.hc .stale{color:#ffb703;font-size:11.5px;font-weight:700}.hc .up{color:#7dff9b;font-weight:800}.hc .dn{color:#ff6b6b;font-weight:800}
.hc ul{margin:2px 0 0;padding-left:16px}.hc li{font-size:12.5px;color:#d9dcf5}
.bc{margin-top:8px;background:#0f1328;border:1px solid var(--line);border-radius:12px;padding:4px 10px}.bc .bgh{border-bottom:0}
.bc .cl{display:flex;gap:6px;font-size:13px;padding:1px 0 1px 14px;overflow:hidden;white-space:nowrap}.bc .cl .ck{color:var(--dim);font-size:11.5px;min-width:52px}.bc .cl .ct{overflow:hidden;text-overflow:ellipsis}
.bc .up{color:#7dff9b}.bc .dn{color:#ff6b6b}
.hc{background:#0f1328;border:1px solid var(--line);border-radius:12px;padding:8px 10px;font-size:13px;min-width:0}.hc h3{margin:0 0 4px;font-size:13px;color:var(--dim)}
.hc .hx{font-weight:700}.hc .hb{white-space:pre-wrap;color:#d9dcf5;font-size:12.5px;margin-top:3px}.hc .hm{color:var(--dim);font-size:11.5px}.hc .big{font-size:22px}
.chs{display:grid;gap:8px}.chc{display:block;text-align:left;width:100%;background:#2e2810;color:var(--text);border:1px solid #ffd166;border-radius:12px;padding:10px 13px;font-weight:400}
.chc b{font-size:16px;color:#ffd166}.chc .cd{margin-top:3px;font-size:13.5px;color:#d9dcf5;white-space:pre-wrap}.chc.rec{box-shadow:0 0 0 2px #7dff9b;background:#123d22}
.rec-tag{font-size:11px;font-weight:900;margin-left:8px;padding:1px 8px;border-radius:999px;background:#7dff9b;color:#0d1020}
.pk{display:flex;gap:10px;align-items:flex-start;padding:9px 12px;border-radius:12px;border:1px solid #b69bff;background:#1e1640;cursor:pointer}
.pk input{width:20px;height:20px;margin-top:2px}.pk b{color:#d6c8ff}.pk .cd{font-size:13.5px;color:#d9dcf5;white-space:pre-wrap}.pk.off{opacity:.45}.pk.off b{text-decoration:line-through}
.whyl{color:#d9dcf5;font-size:13.5px}.nudge{color:#ffb703;font-weight:800;font-size:14px;padding:8px 12px;border:1px solid #ffb703;border-radius:12px;background:#3d321055;margin-bottom:6px}
button.notif{background:#b69bff;font-size:13px}button.notif.off{background:#4a4f78;color:#c9cdf0}.item.flash{box-shadow:0 0 0 3px #ffd166}
</style></head><body>
<header><h1>🏁 ISUCON lanes</h1><span class="phase" id="phase">-</span><a class="applink" id="applink" target="_blank" rel="noopener" style="display:none">🧭 app-analysis</a><button class="notif" id="notif" style="display:none">🔔 通知を有効にする</button><button class="notif" id="sound" style="display:none" title="新しい todo（ベンチの todo は別の音）で音を鳴らす（WebAudio）">🔊 音 ON</button><span class="clock" id="clock"></span></header>
<main><section class="rm"><div class="rmhead"><h2 style="margin:0">🗺 ロードマップ</h2><div class="scorebox" id="score"></div><span class="prog" id="prog"></span></div>
<div class="swim" id="swim"></div><div class="detail" id="detail">カードをクリックすると、完了の条件と依存が出る。金色の線 = 特別賞までのクリティカルパス。水色の点線 = 間に合えば同じ ship に載せる（待たない）</div>
<div class="lists" id="lists" style="display:none"></div><div class="warnline" id="rmwarn"></div></section>
<section class="lanes" id="lanes"></section><div id="nudges"></div>
<div class="cols"><section><h2>🙋 あなたへの todo <span id="nt"></span></h2><div id="todos"></div><div id="decs"></div></section>
<section><h2>🚨 alert <span id="na"></span></h2><div id="alerts"></div></section></div>
<section class="story" id="story-sec"><h2>📜 経緯 <span class="ub" id="sunread" style="display:none"></span> <button class="copy" id="readall" style="font-size:12px;padding:3px 10px">✓ 既読にする</button> <button class="copy" id="sall" style="font-size:12px;padding:3px 10px">すべて表示</button> <span class="hint">（ベンチごとに 1 枚: 載った PR・律速・効果・決定。「すべて表示」で note・todo・alert の全行）</span></h2><div class="lbl" style="margin-bottom:4px">📌 いまの要点</div><div class="hl" id="hl"></div><div class="chips" id="chips"></div><div id="story"></div></section></main>
<div class="toast" id="toast"></div>
<script>
const LANES_CFG=__LANES_CFG__;
const COLORS={"1":"var(--l1)","2":"var(--l2)","3":"var(--l3)","c":"var(--lc)"};
const esc=s=>String(s??"").replace(/[&<>"']/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));
let last="",sel="",RM=null;const PRSEL={},PKSEL={};
const LCOL={"1":"#ff6bcb","2":"#4fd1ff","3":"#7dff9b","c":"#ffd166","bench":"#b69bff"};
const ICON={done:"✅",doing:"🔵",ready:"🟡",blocked:"⏳",skip:"➖"};
const fmt=n=>n==null?"-":Number(n).toLocaleString("ja-JP");
function spark(runs,target){if(!runs.length)return "";const W=220,H=46,mx=Math.max(target||0,...runs.map(r=>r.score))||1;
 const X=i=>runs.length<2?W/2:6+i*(W-12)/(runs.length-1),Y=v=>H-5-(H-12)*v/mx;
 let s=`<svg width="${W}" height="${H}">`;if(target)s+=`<line x1="0" x2="${W}" y1="${Y(target)}" y2="${Y(target)}" stroke="#ffcc33" stroke-dasharray="4 3"/>`;
 s+=`<polyline fill="none" stroke="#ff6bcb" stroke-width="2.2" points="${runs.map((r,i)=>X(i)+","+Y(r.score)).join(" ")}"/>`;
 runs.forEach((r,i)=>s+=`<circle cx="${X(i)}" cy="${Y(r.score)}" r="3.2" fill="#ffd166"><title>${esc(r.ts)} ${fmt(r.score)}</title></circle>`);return s+"</svg>"}
function trunc(t,n){t=String(t);let w=0,o="";for(const c of t){w+=c.charCodeAt(0)>255?2:1;if(w>n)return o+"…";o+=c}return o}
function renderRM(rm){RM=rm;const sc=rm.score,L=rm.layout;
 document.getElementById("score").innerHTML=`<div><div class="lbl">最新</div><div class="big">${fmt(sc.latest)}</div></div>`+
  (sc.target?`<div><div class="lbl">🏆 目標 ${fmt(sc.target)}</div><div class="bar"><i style="width:${Math.min(100,sc.pct||0)}%"></i><b>${sc.pct??0}%</b></div></div>`:`<div class="lbl">目標: alp.yml の prize_target が無い</div>`)+spark(sc.runs,sc.target);
 document.getElementById("prog").textContent=`done ${rm.done}/${rm.total}`;
 const P=L.pos,cw=L.card_w,ch=L.card_h,by=Object.fromEntries(rm.steps.map(s=>[s.id,s]));
 let g=`<svg viewBox="0 0 ${L.w+20} ${L.h}" preserveAspectRatio="xMinYMin meet" xmlns="http://www.w3.org/2000/svg"><defs><marker id="ar" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0,0L10,5L0,10z" fill="#8a91c0"/></marker><marker id="arc" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0,0L10,5L0,10z" fill="#ffcc33"/></marker></defs>`;
 L.rows.forEach((r,i)=>{g+=`<rect x="0" y="${r.y}" width="${L.w+20}" height="${r.h}" fill="${i%2?"#0c1024":"#0f1430"}"/><rect x="0" y="${r.y}" width="5" height="${r.h}" fill="${LCOL[r.lane]||"#888"}"/><text x="14" y="${r.y+r.h/2+5}" fill="${LCOL[r.lane]||"#ccc"}" font-weight="800" font-size="14">${esc(r.label)}</text>`});
 for(let s=0;s<L.stages;s++)g+=`<text x="${118+s*204+cw/2}" y="12" fill="#3a3f66" font-size="10" text-anchor="middle">stage ${s}</text>`;
 L.edges.forEach(e=>{const a=P[e.from],b=P[e.to];let d;if(b.x>a.x){const x1=a.x+cw,y1=a.y+ch/2,x2=b.x-2,y2=b.y+ch/2,dx=Math.max(30,(x2-x1)/2);d=`M${x1},${y1} C${x1+dx},${y1} ${x2-dx},${y2} ${x2},${y2}`}
  else{const x1=a.x+cw/2,y1=a.y+(b.y>a.y?ch:0),x2=b.x+cw/2+18,y2=b.y+(b.y>a.y?-2:ch+2),dy=(y2-y1)/2;d=`M${x1},${y1} C${x1},${y1+dy} ${x2},${y2-dy} ${x2},${y2}`}
  g+=`<path class="edge ${e.done?"done":""} ${e.critical?"crit":""} ${e.soft?"soft":""}" d="${d}" marker-end="url(#${e.critical?"arc":"ar"})"/>`});
 rm.steps.forEach(s=>{const p=P[s.id];if(!p)return;g+=`<g class="card st-${s.state} ${s.critical?"crit":""} ${sel===s.id?"sel":""}" data-step="${esc(s.id)}"><rect x="${p.x}" y="${p.y}" width="${cw}" height="${ch}" rx="11"/>
  <text x="${p.x+10}" y="${p.y+19}" class="sid">${ICON[s.state]||""} ${esc(s.id)}${s.auto?" · auto":""}${s.items&&s.items.length?` · ${s.items.filter(i=>i.state==="done"||i.state==="skip").length}/${s.items.length}`:""}</text><text x="${p.x+10}" y="${p.y+39}">${esc(trunc(s.title,24))}</text>${s.critical&&s.state!=="done"&&s.state!=="skip"?`<text class="trophy" x="${p.x+cw-20}" y="${p.y+19}">🏆</text>`:""}<title>${esc(s.title)}</title></g>`});
 document.getElementById("swim").innerHTML=g+"</svg>";
 const lanesOrder=["1","2","3","c","bench"];document.getElementById("lists").innerHTML=lanesOrder.filter(l=>rm.per_lane[l]).map(l=>{const pl=rm.per_lane[l],pc=Math.round(100*pl.done/pl.total);
  return `<div class="ll" style="--c:${LCOL[l]}"><h3 style="color:${LCOL[l]}">${esc({"1":"1 ハブ","2":"2 インフラ","3":"3 アプリ","c":"c スカウト","bench":"🏁 ベンチ"}[l]||l)}<span class="pc">${pl.done}/${pl.total}</span></h3><div class="pb"><i style="width:${pc}%"></i></div>`+
  rm.steps.filter(s=>s.lane===l).sort((a,b)=>a.stage-b.stage).map(s=>`<div class="s ${s.state}" data-step="${esc(s.id)}" style="cursor:pointer">${ICON[s.state]||""} <b>${esc(s.id)}</b> ${esc(s.title)}${s.state==="blocked"?`<span class="w">← ${esc((s.waiting_for||[]).join(" "))}</span>`:""}</div>`).join("")+"</div>"}).join("");
 document.getElementById("rmwarn").textContent=rm.runs_err||"";showDetail()}
function showDetail(){if(!RM||!sel)return;const s=RM.steps.find(x=>x.id===sel);if(!s)return;
 const at=s.at?new Date(s.at*1000).toLocaleTimeString("ja-JP",{hour:"2-digit",minute:"2-digit"}):"";
 document.getElementById("detail").innerHTML=`${ICON[s.state]||""} <b>${esc(s.id)} ${esc(s.title)}</b> <span class="k">· ${esc(s.state)}${at?" · "+at:""}${s.critical?" · 🏆 クリティカルパス":""}</span><br><span class="k">完了の条件:</span> ${esc(s.how)}<br><span class="k">依存:</span> ${s.deps.length?esc(s.deps.join(", ")):"なし"}${s.soft&&s.soft.length?` <span class="k">· 間に合えば載せる:</span> ${esc(s.soft.join(", "))}`:""}${s.waiting_for&&s.waiting_for.length?` <span class="k">（未完了: ${esc(s.waiting_for.join(", "))}）</span>`:""}${s.note?`<br><span class="k">メモ:</span> ${esc(s.note)}`:""}<br><span class="k">更新:</span> <code>task lane:step ID=${esc(s.id)} STATE=doing|done|skip</code>`}
function toast(t){const e=document.getElementById("toast");e.textContent=t;e.classList.add("on");setTimeout(()=>e.classList.remove("on"),1800)}
async function copy(text,id){try{await navigator.clipboard.writeText(text)}catch(e){const ta=document.createElement("textarea");ta.value=text;document.body.appendChild(ta);ta.select();document.execCommand("copy");ta.remove()}toast("📋 copied "+id)}
async function post(path,id,extra,body){const h={"X-Lanes":"1","X-Lanes-Token":LANES_CFG.token};if(body)h["Content-Type"]="application/x-www-form-urlencoded";
 const r=await fetch(path+"?id="+encodeURIComponent(id)+(extra||""),{method:"POST",headers:h,body:body||undefined});toast(r.ok?"✅ "+id:"!! "+(await r.text()));load(true);return r.ok}
function sendInput(id){const b=new URLSearchParams();document.querySelectorAll(`input[data-inp="${CSS.escape(id)}"]`).forEach(e=>b.append("v",e.value));post("/input",id,"",b.toString())}
// 「🏁 終わった」 on a bench todo: the form's open state and values survive the re-render of the todo list
const BF={};let SKIND="",SALL=false;
const ROPEN={},GOPEN={};
// unread: the last-read event time + rows read one by one (body opened), in localStorage per board. The first visit
// marks everything there as read (no wall of NEW).
const RKEY="lanes.read."+(LANES_CFG.board||"");let RD=null;
function rdLoad(){if(RD)return RD;try{RD=JSON.parse(localStorage.getItem(RKEY)||"null")}catch(e){}if(!RD||typeof RD.t!=="number")RD={t:-1,k:{}};return RD}
function rdSave(){try{const ks=Object.keys(RD.k);if(ks.length>500)ks.slice(0,ks.length-500).forEach(k=>delete RD.k[k]);localStorage.setItem(RKEY,JSON.stringify(RD))}catch(e){}}
const isNew=x=>{const r=rdLoad();return !!x&&x.t>r.t&&!r.k[x.key]};
function storyRows(d){return (d.story||[]).flatMap(g=>g.items)}
function readAll(d){const r=rdLoad();r.t=Math.max(r.t,...storyRows(d).map(x=>x.t),(Date.now()/1000|0));r.k={};rdSave()}
function unread(d){const r=rdLoad();if(r.t<0){readAll(d)}return storyRows(d).filter(isNew).length}
const ub=n=>n?` <span class="ub">${n}</span>`:"";
function srow(x){const hb=x.body&&x.body.length,o=ROPEN[x.key],nw=isNew(x);
 return `<div class="sr ${x.auto?"auto":""} ${hb?"hasb":""} ${nw?"new":""}" ${hb?`data-rkey="${esc(x.key)}"`:""} title="${esc(x.text)}"><span class="mk">${hb?(o?"▾":"▸"):""}</span><span class="tm">${esc(x.hm)}</span><span class="lb" style="--c:${COLORS[x.lane]||"#aaa"}">${esc(x.lane)}</span><span>${esc(x.icon)}</span><span class="tx">${esc(x.text)}</span>${x.ref?`<span class="rf">${esc(x.ref)}</span>`:""}${nw?`<span class="nd">● NEW</span>`:""}</div>`+(hb&&o?`<div class="sb">${esc(x.body.join("\n"))}</div>`:"")}
function hcard(t,inner,n,ph){return `<div class="hc"><h3>${t}${ub(n)}</h3>${inner||`<span class="ph">${esc(ph||"まだ無い")}</span>`}</div>`}
function note1(x){return x?`<div class="hx">${esc(x.text)}</div>${x.body&&x.body.length?`<div class="hb">${esc(x.body.join("\n"))}</div>`:""}<div class="hm">${esc(x.hm)} · ${esc(x.lane)}${x.ref?" · "+esc(x.ref):""}</div>`:""}
const pctS=p=>p==null?"":`<span class="${p>=0?"up":"dn"}">（${p>=0?"+":""}${p}%）</span>`;
const lis=a=>a&&a.length?`<ul>${a.map(x=>`<li>${esc(x)}</li>`).join("")}</ul>`:"";
// 「📌 いまの要点」: six fixed slots, a placeholder when one is empty (lanes.highlights())
function renderHL(h,d){if(!h)return;const sc=h.score||{},ca=h.calc||{},fb=h.forbid||{},ap=h.app,bn=h.bottleneck;
 const lg=(d.story||[]).find(g=>!g.current&&g.score),sn=lg&&rdLoad().t<lg.t?1:0;
 const appI=ap?note1(ap)+(ap.map?`<div class="hb">🗺 ${esc(ap.map)}</div>`:""):"";
 const calcI=(ca.lines||[]).length||ca.target?lis(ca.lines)+(ca.target?`<div class="hm">目標（特別賞） ${fmt(ca.target)}</div>`:"")+(ca.src?`<div class="hm">${esc(ca.src)}</div>`:""):"";
 const fbI=(fb.lines||[]).length?lis(fb.lines)+(fb.src?`<div class="hm">${esc(fb.src)}</div>`:""):"";
 const bnI=bn?`<div class="hx">${esc(bn.text)}</div>${bn.line?`<div class="hb">${esc(bn.line)}</div>`:""}${bn.stale?`<div class="stale">（古い: ${esc(bn.stale)}）</div>`:""}<div class="hm">${esc(bn.hm)} · ${esc(bn.lane)}${bn.ref?" · "+esc(bn.ref):""}</div>`+(bn.stale&&h.verdict?`<div class="hm">最新 run: ${esc(h.verdict)}</div>`:"")
  :(h.verdict?`<div class="hx">${esc(h.verdict)}</div><div class="hm">最新 run の判定（律速の note はまだ無い）</div>`:"");
 const scI=sc.latest||sc.best!=null?`<div class="big">${fmt(sc.latest)} ${pctS(sc.delta_pct)}</div><div class="hm">${sc.bench?`ベンチ ${esc(sc.bench)}`:""}${sc.hm?" · "+esc(sc.hm):""}${sc.prev?` · 前回 ${fmt(sc.prev)}`:""}</div><div class="hm">ベスト ${fmt(sc.best)}${sc.target?` · 目標 ${fmt(sc.target)} の ${sc.pct??0}%`:""}</div>`:"";
 const dcI=(h.decisions||[]).map(x=>`<div><span class="tid" style="font-size:12px;padding:2px 6px">${esc(x.id)}</span>${esc(x.msg)}</div>`).join("")+
  (h.decision_note?`<div class="hb">方針: ${esc(h.decision_note.text)} <span class="hm">${esc(h.decision_note.hm)} · ${esc(h.decision_note.lane)}</span></div>`:"")+
  (h.human?`<div class="hb">決定: ${esc(h.human.id)} ${esc(h.human.text)} <span class="hm">${esc(h.human.hm)}</span></div>`:"");
 document.getElementById("hl").innerHTML=hcard("🧭 アプリ",appI,ap&&isNew(ap)?1:0,"kickoff の後にハブが書く（lane:note KIND=app REF=docs/kickoff.md）")+
  hcard("🧮 スコア計算",calcI,0,"kickoff の後にハブが書く（docs/kickoff.md の スコア）")+
  hcard("⛔ 禁止・失格",fbI,0,"kickoff の後にハブが書く（docs/kickoff.md の 禁止・必須）")+
  hcard("🔥 今の律速",bnI,(bn&&isNew(bn)?1:0)+sn,"ベンチの後に律速の note（KIND=bottleneck）が出る")+
  hcard("📈 スコア",scI,sn,"最初のベンチの後に出る")+
  hcard("🙋 決定待ち・方針",dcI,0,"いま決めることは無い")}
const CLBL={pr:"載った PR",bottleneck:"律速",effect:"効果",fix:"中身",decision:"決定",incident:"crit",next:"次",app:"アプリ"};
function cardHtml(c,i){const open=c.key in GOPEN?GOPEN[c.key]:i<2;
 const head=c.prep?"準備（最初のベンチまで）":`ベンチ ${esc(c.bench)}${c.score?" · score "+fmt(c.score):""}${pctS(c.delta_pct)}${c.hm?" · "+esc(c.hm):""}`;
 const body=(c.lines||[]).map(l=>`<div class="cl" title="${esc(l.text)}"><span>${esc(l.icon)}</span><span class="ck">${esc(CLBL[l.k]||l.k)}</span><span class="ct">${esc(l.text)}</span></div>`).join("")||`<div class="cl"><span class="ck">（記録なし）</span></div>`;
 return `<div class="bc"><div class="bgh" data-gkey="${esc(c.key)}">${open?"▾":"▸"} ${head}</div>${open?body:""}</div>`}
function renderStory(d){const ks=d.note_kinds||{},gs=d.story||[],nu=unread(d);renderHL(d.highlights,d);
 const su=document.getElementById("sunread");su.textContent=nu+" 新着";su.style.display=nu?"":"none";
 document.getElementById("sall").textContent=SALL?"ベンチごとの要約に戻す":"すべて表示";
 const ch=document.getElementById("chips");ch.style.display=SALL?"":"none";
 if(!SALL){ch.innerHTML="";const cs=d.cards||[];document.getElementById("story").innerHTML=cs.map(cardHtml).join("")||`<div class="empty">まだ無い</div>`;return}
 ch.innerHTML=[["","すべて"]].concat(Object.keys(ks).map(k=>[k,ks[k]+" "+k])).map(([k,l])=>`<button class="chip ${SKIND===k?"on":""}" data-skind="${esc(k)}">${esc(l)}</button>`).join("");
 let h="";gs.forEach((g,i)=>{const rows=g.items.filter(x=>!SKIND||x.kind===SKIND),open=g.key in GOPEN?GOPEN[g.key]:i<2;
  const head=g.current?`いま（ベンチ ${g.bench} の前）`:`ベンチ ${g.bench}${g.score?" · score "+fmt(g.score):""} · ${esc(g.hm)}`;
  h+=`<div class="bg"><div class="bgh" data-gkey="${esc(g.key)}">${open?"▾":"▸"} ${head} <span class="n">${rows.length} 件</span>${ub(rows.filter(isNew).length)}</div>${open?rows.map(srow).join(""):""}</div>`});
 document.getElementById("story").innerHTML=h||`<div class="empty">まだ無い（task lane:note LANE=… KIND=… MSG="…"）</div>`}
function benchForm(id){const f=BF[id]||{};return `<div class="bform ${f.open?"on":""}" id="bf-${esc(id)}"><div class="hint">ベンチが終わったら送る: この todo が閉じ、裏で task bench:done（全ホストの run を終了 → collect → report → score）。どちらも空でよい</div>
  <label class="hint">スコア（数字だけ）<input data-bf="${esc(id)}" data-f="score" inputmode="numeric" autocomplete="off" placeholder="例 95591" value="${esc(f.score||"")}"></label>
  <label class="hint">ベンチの出力の最後の数行（練習。貼らなくてよい）<textarea data-bf="${esc(id)}" data-f="out" spellcheck="false">${esc(f.out||"")}</textarea></label>
  <div class="row"><button class="send" data-bsend="${esc(id)}">📨 終わった を送る</button><button class="copy" data-bopen="${esc(id)}">やめる</button></div></div>`}
function render(d){
 document.getElementById("phase").textContent=d.phase+" · "+d.phase_min+"m";
 document.getElementById("decs").innerHTML=(d.decisions||[]).length?`<div class="dec">最近の決定: ${d.decisions.map(x=>x.pr?`${(x.prs||[x.pr]).map(n=>`<a class="prlink" href="${esc(x.pr_url?x.pr_url.replace(/\/pull\/\d+$/,"/pull/"+n):"")}" target="_blank" rel="noopener">#${n}</a>`).join(" ")} <b class="${esc(x.decision.split("（")[0])}">${esc({ok:"merge OK",changes:"要修正",done:"done"}[x.decision]||x.decision.replace(/^ok/,"merge OK"))}</b> ${esc(x.at)}`:`<b>${esc(x.id)}</b> → <b class="ch">${esc(x.decision)}</b> ${esc(x.at)}`).join(" · ")}</div>`:"";
 document.getElementById("lanes").innerHTML=d.lanes.length?d.lanes.map(l=>`<div class="lane ${esc(l.state)} ${l.stuck?"stuck":""}" style="--c:${COLORS[l.id]||"#aaa"}">
  <div class="top"><span class="icon">${l.icon}</span><span class="name">${esc(l.label)}</span><span class="agt ${esc(l.agent)}" title="sessions.json の agent（codex のレーンへの合図は task lane:msg）">${l.agent==="codex"?"Codex":"Claude"}</span>${l.unread?`<span class="unread" title="盤の未読メッセージ（task lane:inbox LANE=${esc(l.id)}）">✉${l.unread}</span>`:""}${l.agents?`<span class="agents" title="このレーンが今走らせているサブエージェント">🤖×${l.agents}</span>`:""}<span class="state">${esc(l.state)} · ${l.age_min}m</span></div>
  <div class="step">${esc(l.step)}</div>${(l.tasks||[]).map(tk=>`<div class="tk"><div class="tkh"><b>${esc(tk.id)}</b> ${esc(tk.title)}${tk.total?`<span class="tkn">${tk.done_n}/${tk.total}</span>`:""}</div>${tk.total?`<div class="tkb"><i style="width:${Math.round(100*tk.done_n/tk.total)}%"></i></div><ul>${tk.items.map(it=>`<li class="${esc(it.state)}">${{"done":"✅","doing":"🔵","skip":"➖"}[it.state]||"⬜"} ${esc(it.t)}</li>`).join("")}</ul>`:`<div class="note">チェックリスト未登録（task lane:items）</div>`}</div>`).join("")}${l.note?`<div class="note">${esc(l.note)}</div>`:""}${(l.msgs||[]).length?`<div class="msgs">${l.msgs.map(m=>`<div class="m ${m.read?"rd":"ur"}" title="${m.read?"既読 "+esc(m.read_at):"未読"}">${esc(m.at)} ${esc(m.from)}→${esc(m.to)} ${m.read?"":"● "}${esc(m.msg)}</div>`).join("")}</div>`:""}${l.stuck?`<div class="note">⚠ 5 分以上 人待ち: そのタブを見る</div>`:""}</div>`).join(""):`<div class="empty">まだどのレーンも status を出していない</div>`;
 document.getElementById("nudges").innerHTML=(d.nudges||[]).map(n=>`<div class="nudge">${esc(n.text)}</div>`).join("");
 {const nu=unread(d),p=[];if(d.todos.length)p.push(d.todos.length+" todo");if(nu)p.push(nu+" 新着");document.title=(p.length?"("+p.join(" · ")+") ":"")+"lanes"}notify(d);noteNotify(d);
 document.getElementById("nt").textContent="("+d.todos.length+")";document.getElementById("na").textContent="("+d.alerts.length+")";
 const keep={},fe=document.activeElement,fk=fe&&fe.dataset&&fe.dataset.inp?fe.dataset.inp+"/"+fe.dataset.n:"",fb=fe&&fe.dataset&&fe.dataset.bf?fe.dataset.bf+"/"+fe.dataset.f:"";
 document.querySelectorAll("input[data-inp]").forEach(e=>keep[e.dataset.inp+"/"+e.dataset.n]=e.value);
 document.querySelectorAll("input[data-prsel]").forEach(e=>{PRSEL[e.dataset.prsel+"/"+e.value]=e.checked});
 document.querySelectorAll("input[data-pick]").forEach(e=>{PKSEL[e.dataset.pick+"/"+e.value]=e.checked});
 document.getElementById("todos").innerHTML=d.todos.length?d.todos.map(x=>`<div class="item" id="todo-${esc(x.id)}"><div class="meta"><span class="tid" title="この todo の番号（タブで「${esc(x.id)} は?」と聞ける）">${esc(x.id)}</span>${esc(x.label)} · ${x.age_min}m 前</div>
  <div class="msg">${esc(x.msg)}</div>${x.why?`<div class="whyl">なぜ: ${esc(x.why)}</div>`:""}${x.cmd?`<pre>${esc(x.cmd)}</pre>`:""}${x.after?`<div class="hint">終わったら: ${esc(x.after)}</div>`:""}
  ${x.inputs&&x.inputs.length?`<div class="inp">${x.inputs.map((l,i)=>`<label>${esc(l)}<input data-inp="${esc(x.id)}" data-n="${i}" maxlength="500" autocomplete="off" spellcheck="false"></label>`).join("")}${x.envkey?`<div class="hint">送信すると .env の ${esc(x.envkey)} をこの盤が書き換える</div>`:""}</div>`:""}
  ${x.pr?`<div class="prl">${x.pr_list.map(p=>{const on=PRSEL[x.id+"/"+p.n]!==false;return `<label class="${on?"":"off"}" title="外すとこの PR は merge しない（held_prs）"><input type="checkbox" data-prsel="${esc(x.id)}" value="${p.n}" ${on?"checked":""}>${p.url?`<a class="prlink" href="${esc(p.url)}" target="_blank" rel="noopener">🔗 #${p.n}</a>`:`#${p.n}`} <span class="pt">${esc(p.title||"")}</span></label>`}).join("")}${x.pr_url?"":`<span class="hint">（repo の URL が分からない: GIT_REMOTE）</span>`}</div>`:""}
  ${x.choices&&x.choices.length?`<div class="chs">${x.choices.map((c,i)=>`<button class="chc ${i===0&&x.choices.length>1?"rec":""}" data-decide="${esc(c)}" data-id="${esc(x.id)}"><b>${esc(c)}</b>${i===0&&x.choices.length>1?`<span class="rec-tag">推奨</span>`:""}${(x.choice_desc||{})[c]?`<div class="cd">${esc(x.choice_desc[c])}</div>`:""}</button>`).join("")}</div>`:""}
  ${x.picks&&x.picks.length?`<div class="chs">${x.picks.map(p=>{const on=PKSEL[x.id+"/"+p.key]!==false;return `<label class="pk ${on?"":"off"}"><input type="checkbox" data-pick="${esc(x.id)}" value="${esc(p.key)}" ${on?"checked":""}><div><b>${esc(p.label)}</b>${p.desc?`<div class="cd">${esc(p.desc)}</div>`:""}</div></label>`}).join("")}</div>`:""}
  <div class="row"><button class="copy" data-copy="${esc(x.id)}">📋 コピー</button>${x.picks&&x.picks.length?`<button class="ok" data-pickok="${esc(x.id)}">✅ 選んだものを OK</button><button class="chg" data-picknone="${esc(x.id)}">✋ 全部やめる</button>`:x.pr?`<button class="ok" data-decide="ok" data-id="${esc(x.id)}">✅ ${x.pr_list.length>1?"まとめて merge OK":"merge OK"}</button><button class="chg" data-decide="changes" data-id="${esc(x.id)}">✋ 要修正（タブで番号）</button>`:(x.choices&&x.choices.length?"":(x.inputs&&x.inputs.length?`<button class="send" data-send="${esc(x.id)}">📨 送信</button>`:x.kind==="bench"?`<button class="bdone" data-bopen="${esc(x.id)}">🏁 終わった</button>`:`<button data-done="${esc(x.id)}">✅ 完了</button>`))}</div>${x.kind==="bench"?benchForm(x.id):""}</div>`).join(""):`<div class="empty">なし 🎉</div>`;
 document.querySelectorAll("input[data-inp]").forEach(e=>{const k=e.dataset.inp+"/"+e.dataset.n;if(k in keep)e.value=keep[k];if(k===fk)e.focus()});
 if(fb)document.querySelectorAll("[data-bf]").forEach(e=>{if(e.dataset.bf+"/"+e.dataset.f===fb)e.focus()});
 renderStory(d);
 document.getElementById("alerts").innerHTML=d.alerts.length?d.alerts.map(x=>`<div class="item ${esc(x.level)}"><div class="meta"><span class="lv">${esc(x.level.toUpperCase())}</span> ${esc(x.id)} · ${esc(x.label)} · ${x.age_min}m 前</div>
  <div class="msg">${esc(x.msg)}</div>${x.impact?`<div class="hint">影響: ${esc(x.impact)}</div>`:""}<div class="hint">${esc(x.hint)}</div>
  <div class="row"><button class="res" data-res="${esc(x.id)}">解消</button></div></div>`).join(""):`<div class="empty">なし ✨</div>`;
 window._todos=Object.fromEntries(d.todos.map(x=>[x.id,x.copy]));
 if(d.roadmap)renderRM(d.roadmap);
}
let VER="",DOWN=false;
function tick(){if(!DOWN)document.getElementById("clock").textContent="🕒 "+new Date().toLocaleTimeString("ja-JP")}
function show(t){DOWN=false;tick();if(t!==last){last=t;render(JSON.parse(t))}}
async function load(force){try{const r=await fetch("/board.json",{cache:"no-store"});const t=await r.text();if(force)last="";show(t)}catch(e){DOWN=true;document.getElementById("clock").textContent="⚠ 盤に繋がらない"}}
// long-poll: the server answers as soon as board.json changes (or after ~25 s), so a todo shows within ~0.1 s even in a
// background tab (timers there are throttled to 1/min by Chrome; a pending fetch is not). Old server without the
// X-Board-Version header: plain 1 s polling.
async function loop(){for(;;){try{const r=await fetch("/board.json"+(VER?"?wait="+encodeURIComponent(VER):""),{cache:"no-store"});
  const v=r.headers.get("X-Board-Version")||"";const t=await r.text();show(t);if(!v){VER="";await new Promise(z=>setTimeout(z,1000))}else VER=v}
 catch(e){DOWN=true;VER="";document.getElementById("clock").textContent="⚠ 盤に繋がらない";await new Promise(z=>setTimeout(z,2000))}}}
document.addEventListener("click",e=>{const st=e.target.closest("[data-step]");if(st){sel=st.dataset.step;if(RM)renderRM(RM);return}
 const rk=e.target.closest("[data-rkey]");if(rk){const k=rk.dataset.rkey;ROPEN[k]=!ROPEN[k];if(ROPEN[k]){rdLoad().k[k]=1;rdSave()}if(last)render(JSON.parse(last));return}
 if(e.target.closest("#sall")){SALL=!SALL;if(last)renderStory(JSON.parse(last));return}
 if(e.target.closest("#readall")){if(last){readAll(JSON.parse(last));render(JSON.parse(last))}return}
 const gk=e.target.closest("[data-gkey]");if(gk){const k=gk.dataset.gkey,g=[...document.querySelectorAll("[data-gkey]")].indexOf(gk);GOPEN[k]=!(k in GOPEN?GOPEN[k]:g<2);if(last)renderStory(JSON.parse(last));return}
 const b=e.target.closest("button");if(!b)return;
 if(b.dataset.copy)copy(window._todos[b.dataset.copy],b.dataset.copy);if(b.dataset.done)post("/done",b.dataset.done);if(b.dataset.res)post("/resolve",b.dataset.res);if(b.dataset.send)sendInput(b.dataset.send);
 if(b.dataset.pickok){const id=b.dataset.pickok,dr=[...document.querySelectorAll(`input[data-pick="${CSS.escape(id)}"]`)].filter(e=>!e.checked).map(e=>e.value);
  post("/decide",id,"&decision=ok"+(dr.length?"&dropped="+encodeURIComponent(dr.join(",")):""))}
 if(b.dataset.picknone)post("/decide",b.dataset.picknone,"&decision=none");
 if(b.id==="notif"&&"Notification" in window){audio();Notification.requestPermission().then(()=>notifBtn())}
 if(b.id==="sound"){SOUND=!SOUND;try{localStorage.setItem("lanes.sound",SOUND?"1":"0")}catch(e){}notifBtn();if(SOUND)beep("todo")}
 if(b.dataset.decide){let extra="&decision="+encodeURIComponent(b.dataset.decide);
  if(b.dataset.decide==="ok"){const held=[...document.querySelectorAll(`input[data-prsel="${CSS.escape(b.dataset.id)}"]`)].filter(e=>!e.checked).map(e=>e.value);if(held.length)extra+="&held="+held.join(",")}
  post("/decide",b.dataset.id,extra)}
 if(b.dataset.skind!==undefined){SKIND=b.dataset.skind;if(last)renderStory(JSON.parse(last))}
 if(b.dataset.bopen){const id=b.dataset.bopen,f=BF[id]=BF[id]||{};f.open=!f.open;const el=document.getElementById("bf-"+id);if(el)el.classList.toggle("on",f.open)}
 if(b.dataset.bsend){const id=b.dataset.bsend,f=BF[id]||{},q=new URLSearchParams();q.append("score",f.score||"");q.append("output",f.out||"");
  post("/bench-done",id,"",q.toString()).then(ok=>{if(ok)delete BF[id]})}});
document.addEventListener("input",e=>{const k=e.target.closest("[data-bf]");if(k){const f=BF[k.dataset.bf]=BF[k.dataset.bf]||{};f[k.dataset.f]=k.value}});
document.addEventListener("change",e=>{const k=e.target.closest("input[data-pick]");if(k){PKSEL[k.dataset.pick+"/"+k.value]=k.checked;k.closest("label").className="pk "+(k.checked?"":"off")}
 const c=e.target.closest("input[data-prsel]");if(c){PRSEL[c.dataset.prsel+"/"+c.value]=c.checked;c.closest("label").className=c.checked?"":"off"}});
// W1: browser notifications for a new todo (a bench todo with the 3-note beep) and a new alert. The first load only
// remembers what is there; a click focuses this tab and scrolls to the todo.
let SEEN=null;
function notifBtn(){const b=document.getElementById("notif"),ok=("Notification" in window)&&Notification.permission==="granted";b.style.display=("Notification" in window)&&!ok?"":"none";
 const s=document.getElementById("sound");s.style.display=ok?"":"none";s.textContent=SOUND?"🔊 音 ON":"🔇 音 OFF";s.classList.toggle("off",!SOUND)}
// W2: a short WebAudio beep with the notification (🔊 toggle, default ON, remembered in localStorage; no audio files).
// The AudioContext is made / resumed on a click on the page (browsers start audio only after a user gesture).
let SOUND=true,ACTX=null;try{SOUND=localStorage.getItem("lanes.sound")!=="0"}catch(e){}
function audio(){try{if(!ACTX)ACTX=new (window.AudioContext||window.webkitAudioContext)();if(ACTX.state==="suspended")ACTX.resume()}catch(e){}return ACTX}
function beep(kind){if(!SOUND)return;const c=audio();if(!c)return;const notes=kind==="bench"?[660,880,1320]:[880,1320];
 notes.forEach((f,i)=>{const o=c.createOscillator(),g=c.createGain(),t=c.currentTime+i*0.16;o.type="sine";o.frequency.value=f;
  g.gain.setValueAtTime(0.0001,t);g.gain.exponentialRampToValueAtTime(0.25,t+0.02);g.gain.exponentialRampToValueAtTime(0.0001,t+0.14);
  o.connect(g).connect(c.destination);o.start(t);o.stop(t+0.15)})}
document.addEventListener("pointerdown",()=>{if(SOUND)audio()},{capture:true});
function pop(title,body,anchor){if(!("Notification" in window)||Notification.permission!=="granted")return;
 try{const n=new Notification(title,{body:String(body||"").slice(0,100),tag:title,silent:true});
  n.onclick=()=>{window.focus();const el=anchor&&document.getElementById(anchor);if(el){el.scrollIntoView({behavior:"smooth",block:"center"});el.classList.add("flash");setTimeout(()=>el.classList.remove("flash"),2500)}n.close()}}catch(e){}}
function notify(d){const cur={t:new Set(d.todos.map(x=>x.id)),a:new Set(d.alerts.map(x=>x.id))};
 if(SEEN){const nt=d.todos.filter(x=>!SEEN.t.has(x.id));nt.forEach(x=>pop(`${x.id} ${x.kind||(x.pr?"review":x.choices?"question":x.picks?"pick":x.inputs?"input":"todo")}`,x.msg,"todo-"+x.id));
  d.alerts.filter(x=>!SEEN.a.has(x.id)).forEach(x=>pop(`🚨 ${x.level} ${x.id} ${x.label}`,x.msg,""));
  if(nt.some(x=>x.kind==="bench"))beep("bench");else if(nt.length)beep("todo")}
 SEEN=cur}
// a new note with a BODY of kind bottleneck / scan / incident: one notification per burst (auto rows such as run scores
// and alerts notify on their own). The first load only remembers what is there.
let NSEEN=null,NQ=[],NT=0;
function noteNotify(d){const rows=storyRows(d).filter(x=>!x.auto&&x.body&&x.body.length&&["bottleneck","scan","incident"].includes(x.kind));
 if(NSEEN===null){NSEEN=new Set(rows.map(x=>x.key));return}
 rows.filter(x=>!NSEEN.has(x.key)).forEach(x=>{NSEEN.add(x.key);NQ.push(x)});
 if(NQ.length&&!NT)NT=setTimeout(()=>{const q=NQ;NQ=[];NT=0;pop(`📜 経緯 ${q.length} 件: ${q[0].icon} ${q[0].text}`,q.length>1?q.slice(1).map(x=>x.icon+" "+x.text).join(" / "):q[0].body.join(" / "),"story")},4000)}
notifBtn();
if(LANES_CFG.app){const a=document.getElementById("applink");a.href=LANES_CFG.app;a.style.display=""}
loop();setInterval(tick,1000);
document.addEventListener("visibilitychange",()=>{if(document.visibilityState==="visible")load(true)});
</script></body></html>
"""


class Handler(BaseHTTPRequestHandler):
    def _send(self, code, body, ctype="text/plain; charset=utf-8", headers=None):
        data = body.encode() if isinstance(body, str) else body
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        for k, v in (headers or {}).items():
            self.send_header(k, v)
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        path = urlparse(self.path).path
        if path == "/":
            cfg = json.dumps({"token": TOKEN, "app": app_url(), "board": lanes.lanes_dir()}).replace("<", "\\u003c")
            self._send(200, PAGE.replace("__LANES_CFG__", cfg), "text/html; charset=utf-8")
        elif path == "/board.json":
            wait = parse_qs(urlparse(self.path).query).get("wait", [""])[0]
            if wait:  # long-poll: hold until the board (or the runs cache) changes, at most LONGPOLL_S
                end = time.time() + LONGPOLL_S
                while board_version() == wait and time.time() < end:
                    time.sleep(0.05)
            ver = board_version()
            self._send(200, json.dumps(payload(), ensure_ascii=False), "application/json; charset=utf-8",
                       headers={"X-Board-Version": ver})
        else:
            self._send(404, "not found")

    def do_POST(self):
        u = urlparse(self.path)
        if u.path not in ("/done", "/resolve", "/decide", "/input", "/bench-done"):
            return self._send(404, "not found")
        if self.headers.get("X-Lanes") != "1":
            return self._send(403, "missing X-Lanes header")
        q = parse_qs(u.query)
        tid = q.get("id", [""])[0]
        try:
            if u.path in ("/decide", "/input", "/bench-done"):
                if not TOKEN or not hmac.compare_digest(self.headers.get("X-Lanes-Token", ""), TOKEN):
                    return self._send(403, "bad or missing X-Lanes-Token (reload the page after restarting lane:web)")
            if u.path == "/decide":
                lanes.decide(tid, q.get("decision", [""])[0], by="web", held=q.get("held", [""])[0],
                             dropped=q.get("dropped", [""])[0])
            elif u.path in ("/input", "/bench-done"):
                limit = MAX_BODY if u.path == "/input" else MAX_BENCH_BODY
                n = self.headers.get("Content-Length") or "0"
                n = int(n) if n.isdigit() else limit + 1
                if n > limit:
                    return self._send(413, "too large")
                try:
                    form = parse_qs(self.rfile.read(n).decode("utf-8"), keep_blank_values=True, strict_parsing=False)
                except UnicodeDecodeError:
                    return self._send(400, "body must be UTF-8")
                if u.path == "/input":
                    lanes.submit_input(tid, form.get("v", []), by="web")
                else:
                    if not tid:
                        return self._send(400, "id=<the bench todo> required (「🏁 終わった」 is on the bench todo)")
                    bench_done(form.get("score", [""])[0], form.get("output", [""])[0], tid)
            elif u.path == "/done":
                lanes.web_done(tid)
            else:
                lanes.mutate("resolve", {"id": tid})
        except lanes.LaneError as e:
            return self._send(400, str(e))
        self._send(200, "ok")

    def log_message(self, *args):
        pass


def make_server(port, token=None):
    """token=None: an in-memory token (tests); serve() writes it to token_file()."""
    global TOKEN
    if token is not None:
        TOKEN = token
    elif not TOKEN:
        init_token(write=False)
    return ThreadingHTTPServer(("127.0.0.1", port), Handler)


def serve(port=8765):
    try:
        lanes.mutate("init", {})  # first todos of the day on a new board (no-op once seeded)
    except Exception:
        pass
    init_token()
    srv = make_server(port)
    RUNS.start()
    url = f"http://127.0.0.1:{srv.server_address[1]}/"
    print(f"🏁 lanes board: {url}   (Ctrl-C to stop)", flush=True)
    if sys.platform == "darwin":
        subprocess.run(["open", url], capture_output=True)
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        srv.server_close()


if __name__ == "__main__":
    serve(int(sys.argv[1]) if len(sys.argv) > 1 else 8765)
