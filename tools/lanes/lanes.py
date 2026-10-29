#!/usr/bin/env python3
"""Lanes board: a local blackboard for one human running four Claude Code sessions
(lanes 1 = hub/ハブ, 2 = infra/インフラ, 3 = app/アプリ, c = scout/スカウト).

Stored in <git common dir>/lanes/board.json, so every worktree of the repo shares it, it is never committed, and
another contest's repo has its own board. Writes take an flock on lanes/lock and replace the file atomically.

  lanes.py status  lane=1 state=working step="..." [note="..."] [agents=3]
  lanes.py items   id=A3 items="indexes|livecomment|stats"              → the step's own checklist (exact progress n/m)
  lanes.py item    id=A3 n=2 state=todo|doing|done|skip                 → tick one item (1-based)
  lanes.py alert   lane=1 level=warn|crit msg="..." [impact="..."]     → prints the id (a3); the lane goes to alert
  lanes.py resolve id=a3                                               → last open alert of the lane: previous state back
  lanes.py todo    lane=c msg="..." [cmd="..."] [why="..."] [after="..."] [pr=5 | pr="7 8 9"] [kind=bench]
                                                                       → prints the id (t4); cmd = the line to paste;
                                                                       pr = a review todo (one or several PRs): lane:web
                                                                       links each PR (title from gh at creation), a toggle
                                                                       per PR, 「✅ まとめて merge OK」「✋ 要修正」 instead of ✅
                                                                       kind = the todo's kind for lane:timeline (default:
                                                                       review / question / input / other)
                                                       [choices="OK|やめる|0 のまま"] → a question: lane:web shows one button
                                                                       per choice instead of ✅ (not with pr); a choice may be
                                                                       "label::description" (a card: label bold, description
                                                                       below, the first marked 推奨); the decision is the label
                                                       [picks="U1 a::why|U2 b::why"] → a multi-select: one card + toggle per
                                                                       item (all ON), 「✅ 選んだものを OK」「✋ 全部やめる」;
                                                                       an item's key is the label's first word; lane:wait
                                                                       prints picked=U1 U3 / dropped=U2 / decision=ok|none
                                                       [input="label1|label2"] → lane:web shows one text field per label and
                                                                       a submit button (not with pr / choices)
                                                       [envkey=HOSTS]  → with input: lane:web writes the answer into that
                                                                       .env line (HOSTS|HOST_LOGIN|BENCH_CMD|GH_PROJECT|CONTEST)
  lanes.py done    id=t4                                               → decision=done (only lane:web's buttons write ok / changes / a choice)
  lanes.py wait    id=t4 [timeout=3600]                                → blocks until done; prints "decision=<ok|changes|<choice>|done>"
                                                                       (input todos first print input.<n>=<value> [env.<KEY>=…];
                                                                       a decided PR todo first prints ok_prs=7 9 / held_prs=8)
  lanes.py starts  hub=<name>                                          → the start todos of lanes 2 / 3 / c (CMD = claude --model …
                                                                       "<start line>", models from sessions.json)
  lanes.py msg     from=1 to=3 msg="[TASK] 3 fix TS=…"                  → a MESSAGES.md line on the board (prints m4); the
                                                                       transport to / from Codex lanes (no SendMessage)
  lanes.py baseline sha=<short sha>                                    → hosts-setup.sh after 0 NG: band ready + the baseline
                                                                       bench todo + [DONE] 2 baseline-ready to 1, once per
                                                                       board (phase setup, band never moved); else "skip (…)"
  lanes.py inbox   lane=3 [all=1]                                      → unread messages for the lane, marked read (all = history)
  lanes.py wait-msg lane=1 [timeout=3600]                              → blocks until the lane has an unread message, prints it
  lanes.py agent   [lane=3]                                            → claude | codex (sessions.json "agent"); no lane = all
  lanes.py handoff lane=3 [out=docs/handoff-lane3.md]                  → one page of the lane's state for a successor session
  lanes.py note    lane=3 kind=app|bottleneck|scan|fix|decision|score|incident msg="<one line>" [ref="PR #7"]
                   [body="line 1\\nline 2"]                                (≤ 5 lines: the conclusion; lane:web ▸ expands it)
                                                                       → one line of the story (event "note"); lane:web's
                                                                       「📜 経緯」 lists it with the human's decisions, run
                                                                       scores and alerts (story())
  lanes.py event   kind=ship.step [k=v …]                              → append one event to events.jsonl (ship.sh, deploy)
  lanes.py timeline [from=hh:mm]                                       → Markdown: timeline, milestones, ships, human latency,
                                                                       lane time shares (timeline.py)
  lanes.py phase   phase=setup|baseline|loop|freeze|final              (baseline also marks the hub's H3 doing)
  lanes.py step    id=I4 state=todo|doing|done|skip [note="PR #5"]   → roadmap progress (steps in plan.json)
  lanes.py advance ids="H4 H5" [note="PR #7"]                          → the first of ids not done/skip becomes done (hub:ship)
  lanes.py autostep pr=7 branch=setup/app-map [files="a b"]            → every step whose plan.json "auto" globs match the
                                                                       merged PR's head / files and is not done/skip → done
                                                                       NOTE "PR #7" (hub:ship, per merged PR); prints the ids
  lanes.py msgstep lane=c msg="[DONE] c manual …"                      → steps of that lane whose plan.json "done_on" regexes
                                                                       match the sent message → done (cchook.py, op msg)
  lanes.py benchstep ts=<run ts> score=<n>                              → the first bench step B<n> not done/skip → done
                                                                       (runs.py wait per scored run; 「終わった」 does it too)
  lanes.py bench   state=none|ready|running|collect [sha=] [prs="7 8"] [next="9"]  → the bench band (empty = keep, "-" = clear);
                                                                       ready never moves a running / collect band back
                                                                       unless sha is new; without state only the fields
  lanes.py bench                                                       → prints the bench state (for ship.sh)
  lanes.py copy    id=t4                                               → todo's cmd (else msg) to the clipboard
  lanes.py board   [--color]                                           → ≤ 25 lines (for the sessions)
  lanes.py watch   [seconds]                                           → full-screen board for the human (keys: 1-9 copy, d+1-9 done, q)
  lanes.py web     [port]                                              → the same board in a browser (lanes_web.py)

LANES_DIR overrides the directory, LANES_PLAN the roadmap file (default: plan.json next to this script),
LANES_ENV_FILE the .env that lane:web writes for envkey todos (default: ./.env, else ../../.env as the Taskfile's dotenv),
LANES_SESSIONS the per-lane model / start line table (default: sessions.json next to this script), LANES_NO_GH=1 skips
the gh call that fetches PR titles for a review todo.
Every change of the board also appends timestamped events to <dir>/events.jsonl (append-only; lane:timeline reads it).
"""
import fcntl
import fnmatch
import ipaddress
import json
import os
import re
import stat
import subprocess
import sys
import time
from datetime import datetime

SEED_TODOS = [  # ids t1 t2 t3 on a new board; lane 2 waits on t2 (HOSTS), the hub on t3 (manual URLs + bench)
    {"start": "1",  # msg / cmd come from sessions.json (model + start line), see start_todo()
     "why": "4 つのセッションの起点。2 / 3 / c の起動の 1 行にはハブの名前が要る（ハブが task lane:starts HUB=<名前> で作る）",
     "after": "✅ を押す（2 / 3 / c の [HELLO] がそろったらハブが登録を確かめる）"},
    {"msg": "3 台の IP を入力して送信する（svc1 svc2 svc3 の順）→ 盤が .env の HOSTS に書く",
     "cmd": "",
     "why": "host:peek / hosts:setup / deploy / smoke が HOSTS 無しでは動かない。Claude は .env を書けない（書くのは盤の web）",
     "after": "送信するだけ（レーン 2 が起きて host:peek に進む）。練習の HOST_LOGIN=ubuntu は .env に手で",
     "inputs": ["svc1 IP", "svc2 IP", "svc3 IP"], "envkey": "HOSTS"},
    {"msg": "競技が始まったら、マニュアルの URL 2 つとベンチの回し方を入力して送信する（開始前は送らない）",
     "cmd": "",
     "why": "kickoff ① はマニュアルの URL で始まる。練習のベンチは事前にセットアップ済みのものを書く（レーンはベンチのために DNS・SG・env.sh を変えない）",
     "after": "送信するだけ（ハブが起きて kickoff ① を始め、ベンチの行をレーン 2 に渡す）",
     "inputs": ["当日マニュアル URL", "アプリマニュアル URL", "ベンチ（本番: ポータル / 練習: セットアップ済み、実行するマシンと --webapp の IP）"], "kind": "url"},
]

PHASES = ["setup", "baseline", "loop", "freeze", "final"]
STATES = ["working", "waiting-human", "waiting", "alert", "idle"]
# op hook (cchook.py, sessions started with LANE=<n>): target state → the states it may replace. The STEP text stays.
HOOK_FROM = {"working": ("idle", "waiting", "waiting-human"), "waiting": ("working",), "idle": ("working",)}
STEP_STATES = ["todo", "doing", "done", "skip"]
BENCH_STATES = ["none", "ready", "running", "collect"]
DECISIONS = ["ok", "changes"]  # written by lane:web's POST /decide only; lane:done writes "done"
MAX_PRS = 20  # one review todo per bench: PR="7 8 9"
KIND_RE = re.compile(r"^[a-z][a-z0-9_-]{0,15}$")
MAX_BENCH_OUTPUT = 65536  # lane:web 「🏁 終わった」 (bench todo): the pasted tail of the bench output
MAX_CHOICES, MAX_CHOICE = 6, 40  # todo choices="a|b|c": one lane:web button each; the clicked text is the decision
MAX_DESC = 200  # choices / picks "label::description": the description under the label on lane:web
MAX_PICKS, MAX_PICK = 12, 80  # todo picks="U1 a::why|U2 b::why": one card + toggle each; picked / dropped by key
NUDGE_SEC = 180  # lane:board / lane:web: a lane 2 / 3 / c waiting or idle this long (setup / baseline / loop) → hand it a [TASK]
NUDGE_PHASES = ("setup", "baseline", "loop")
MAX_INPUTS, MAX_INPUT_LABEL, MAX_INPUT = 6, 60, 500  # todo input="a|b": one lane:web text field each (POST /input only)
ENV_KEYS = ["HOSTS", "HOST_LOGIN", "BENCH_CMD", "GH_PROJECT", "CONTEST"]  # the .env keys an input todo may write
ENV_SHOWN = {"HOSTS"}  # lane:wait prints these values (task env:show shows them anyway); the others print env.<KEY>=set
PHASE_STEPS = {"baseline": ("H3", "doing")}  # lane:phase moves the hub's roadmap step too (H3 done = actions:plan)
ROW_ORDER = ["1", "2", "3", "c", "bench"]
LEVELS = ["warn", "crit"]
# lane:note: one line of the story (lane:web 「📜 経緯」); KIND → icon
NOTE_KINDS = {"app": "🧭", "bottleneck": "🔥", "scan": "🔎", "fix": "🔧", "decision": "🙋", "score": "📈", "incident": "🚨"}
MAX_NOTE, MAX_REF, STORY_MAX = 120, 40, 400  # a contest day: 200–300 rows
MAX_BODY_LINES, MAX_BODY_LINE = 5, 160  # lane:note body: the conclusion behind the headline (lane:web ▸ expands it)
BENCH_MERGE_SEC = 1200  # a run_scored this soon after an unscored 「終わった」 is the same bench (one header)
LANE_RE = re.compile(r"^[a-z0-9][a-z0-9_-]{0,7}$")
MAX_SHORT, MAX_TEXT, MAX_CMD, MAX_ITEMS, MAX_LANES, BOARD_ROWS = 120, 400, 1000, 200, 8, 25
LANE_NAMES = {"1": "ハブ", "2": "インフラ", "3": "アプリ", "c": "スカウト"}
LANE_FILES = {"1": "docs/lanes/1-hub.md", "2": "docs/lanes/2-infra.md", "3": "docs/lanes/3-app.md", "c": "docs/lanes/c-scout.md"}
AGENT_TYPES = ("claude", "codex")  # sessions.json "agent" per lane; codex lanes talk through lane:msg (no SendMessage)
MSG_TAG_RE = re.compile(r"^\[([A-Z][A-Z-]{1,15})\]")  # board messages keep the docs/MESSAGES.md one-line form
MAX_MSGS, MSG_HISTORY, CARD_MSGS = 300, 30, 4  # kept on the board / shown by lane:inbox ALL=1 / per lane card on lane:web
STATE_ICONS = {"working": "🟢", "waiting-human": "🙋", "waiting": "⏳", "alert": "🚨", "idle": "💤"}

COLORS = {"alert": "\033[1;37;41m", "waiting-human": "\033[1;33m", "working": "\033[32m", "crit": "\033[1;31m", "warn": "\033[33m"}
RESET = "\033[0m"


class LaneError(Exception):
    pass


def lanes_dir():
    d = os.environ.get("LANES_DIR")
    if not d:
        try:
            common = subprocess.run(["git", "rev-parse", "--path-format=absolute", "--git-common-dir"],
                                    capture_output=True, text=True, check=True).stdout.strip()
        except (subprocess.CalledProcessError, FileNotFoundError):
            raise LaneError("not inside a git repository (the board lives in <git common dir>/lanes)")
        d = os.path.join(common, "lanes")
    os.makedirs(d, exist_ok=True)
    return d


def now():
    return int(time.time())


def clean(v, n):
    v = "".join(" " if c in "\n\r\t" else c for c in (v or "") if c in "\n\r\t" or (ord(c) >= 0x20 and ord(c) != 0x7f))
    v = v.strip()
    if len(v) > n:
        v = v[: n - 1].rstrip() + "…"
    return v


# ---------- events: append-only log for lane:timeline (timeline.py) ----------
_PENDING = []  # events of the current locked() call; written after the board is saved


def emit(kind, t, **kw):
    """Queue one event (written to events.jsonl by locked()); empty fields are dropped."""
    _PENDING.append({"t": t, "kind": kind, **{k: v for k, v in kw.items() if v not in (None, "", [])}})


def events_path(d=None):
    return os.path.join(d or lanes_dir(), "events.jsonl")


def write_events(d, evs):
    if not evs:
        return
    try:
        with open(events_path(d), "a") as f:
            for e in evs:
                f.write(json.dumps(e, ensure_ascii=False) + "\n")
    except OSError:
        pass  # the board matters more than its log


def record_event(kind, **kw):
    """One event from outside the board (ship.sh steps, deploy hosts, runs:wait), under the board's lock."""
    d = lanes_dir()
    t = kw.pop("t", None) or now()
    ev = {"t": t, "kind": clean(kind, 40), **{clean(k, 40): (v if isinstance(v, (int, float)) else clean(str(v), MAX_TEXT))
                                             for k, v in kw.items() if v not in (None, "")}}
    for k in ("sec", "ok", "n"):  # numbers stay numbers (timeline sums them)
        if isinstance(ev.get(k), str) and re.fullmatch(r"-?[0-9]+", ev[k]):
            ev[k] = int(ev[k])
    with open(os.path.join(d, "lock"), "w") as lk:
        fcntl.flock(lk, fcntl.LOCK_EX)
        write_events(d, [ev])
    return ev


def todo_kind(td):
    return td.get("kind") or ("review" if td.get("pr") else "question" if td.get("choices") else
                              "pick" if td.get("picks") else "input" if td.get("inputs") else "other")


def split_desc(x, n_label):
    """"label::description" → (label, description); no "::" → (text, "")."""
    lab, _, desc = (x or "").partition("::")
    return clean(lab, n_label), clean(desc, MAX_DESC)


def pick_key(label):
    return label.split()[0] if label.split() else label


def todo_prs(td):
    """The PRs of a review todo (old boards stored only pr=<n>)."""
    return [int(x) for x in (td.get("prs") or ([td["pr"]] if td.get("pr") else []))]


def parse_prs(v):
    """"7 8 9" / "#7,#8" / 5 → [7, 8, 9]; LaneError on anything that is not a PR number."""
    out = []
    for x in re.split(r"[\s,]+", str(v or "").strip()):
        x = x.strip().lstrip("#")
        if not x:
            continue
        if not x.isdigit():
            raise LaneError('pr must be PR numbers (PR=5 or PR="7 8 9")')
        if int(x) not in out:
            out.append(int(x))
    if len(out) > MAX_PRS:
        raise LaneError(f"at most {MAX_PRS} PRs in one review todo")
    return out


# ---------- sessions.json: per-lane model and start line (the start todos) ----------

def sessions_path():
    return os.environ.get("LANES_SESSIONS") or os.path.join(os.path.dirname(os.path.abspath(__file__)), "sessions.json")


def load_sessions(full=False):
    try:
        with open(sessions_path()) as f:
            d = json.load(f)
        v = d if full else (d.get("lanes") or {})
        return v if isinstance(v, dict) else {}
    except (OSError, ValueError, AttributeError):
        return {}


OTEL_ENV = ("CLAUDE_CODE_ENABLE_TELEMETRY=1 OTEL_METRICS_EXPORTER=otlp OTEL_LOGS_EXPORTER=otlp "
            "OTEL_EXPORTER_OTLP_PROTOCOL=grpc OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317 "
            "OTEL_METRIC_EXPORT_INTERVAL=1000 OTEL_LOGS_EXPORT_INTERVAL=1000 OTEL_RESOURCE_ATTRIBUTES=lane={lane}")
OTEL_TRACES_ENV = "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1 OTEL_TRACES_EXPORTER=otlp"


def env_setting(key):
    """KEY from the .env file as it is now (the human may have edited it after lane:web started), else the process env."""
    try:
        with open(env_path()) as f:
            for line in f:
                m = re.match(r"^\s*(?:export\s+)?" + re.escape(key) + r"\s*=\s*(.*?)\s*$", line)
                if m:
                    v = m.group(1)
                    if len(v) >= 2 and v[0] == v[-1] and v[0] in "'\"":
                        v = v[1:-1]
                    return v.split(" #")[0].strip()
    except OSError:
        pass
    return os.environ.get(key, "").strip()


def otel_on():
    """OTel env in the start commands: LANES_OTEL=1|0 in .env (read when the todo is shown / copied) or the environment
    wins, else sessions.json "otel"."""
    v = env_setting("LANES_OTEL")
    if v:
        return v == "1"
    return bool(load_sessions(full=True).get("otel"))


def is_practice():
    """Same rule as hosts-setup.sh: PRACTICE set and not 0, or HOST_LOGIN=ubuntu."""
    p = env_setting("PRACTICE")
    return (bool(p) and p != "0") or env_setting("HOST_LOGIN") == "ubuntu"


OTEL_TODO = {"id": "t0", "msg": "（任意・練習）Claude Code の計測（OTel）: Docker を起動して task otel:up、.env に LANES_OTEL=1 を足してから t1 へ",
             "cmd": "task otel:up",
             "why": "トークン数・費用・API の応答時間をレーンごとに取る（hooks の計測は無くても動く）",
             "after": "✅ を押す（やらないなら ✅ だけ）。起動の 1 行は表示・コピーのたびに .env の LANES_OTEL を読み直す（lane:web の再起動は要らない）",
             "kind": "otel"}


# Start command per agent type ({model} = " --model '<m>'" or "", {start} = the quoted start line). Codex flags were
# checked against `codex --help` 0.165.0: --approve-for-me = workspace-write sandbox + approvals routed to automatic
# review (no human prompt); network on for gh / git push / task deploy (ssh); --add-dir = the parent dir, so the
# sibling worktrees ../wt-* are writable. sessions.json "templates" overrides these.
DEFAULT_TEMPLATES = {
    "claude": "claude{model} {start}",
    "codex": 'codex{model} --approve-for-me -c sandbox_workspace_write.network_access=true --add-dir "$PWD/.." {start}',
}
CODEX_START = ("（Codex: AGENTS.md の「Codex で動くとき」に従う。SendMessage は無い: 合図は task lane:msg FROM={lane} TO=<宛先> "
               "MSG=\"[…] …\"、受け取りは手順の始めと終わりに task lane:inbox LANE={lane}）")
HUB_CODEX_NOTE = ("（ハブは Codex: ハブへの合図は SendMessage ではなく task lane:msg FROM={lane} TO=1 MSG=\"[…] …\" で送る。"
                  "ハブからの合図は task lane:wait-msg LANE={lane} を run_in_background で受ける（届いたら流し直す）。README §12）")


def lane_agent(lane):
    """"claude" or "codex" for the lane (sessions.json "agent"; anything else / missing = claude)."""
    v = str((load_sessions().get(lane) or {}).get("agent") or "claude").strip().lower()
    return v if v in AGENT_TYPES else "claude"


def codex_lanes():
    return [k for k in ("1", "2", "3", "c") if lane_agent(k) == "codex"]


def agents_text():
    """`lanes.py agent` without a lane: one line the hub reads at startup (which lanes need lane:msg)."""
    return " ".join(f"{k}={lane_agent(k)}" for k in ("1", "2", "3", "c")) + \
        (f"\ncodex lanes: {' '.join(codex_lanes())} → [TASK] などは task lane:msg、ハブは task lane:wait-msg LANE=1 を背景で"
         if codex_lanes() else "")


def lane_model(lane):
    full = load_sessions(full=True)
    cfg = (full.get("lanes") or {}).get(lane) or {}
    cx = full.get("codex") if isinstance(full.get("codex"), dict) else {}
    return str((cfg.get("codex_model") or cx.get("model") or "") if lane_agent(lane) == "codex"
               else (cfg.get("model") or "")).strip()


def session_cmd(lane, line):
    """The one pastable line that starts a session of this lane's agent with `line` as its first prompt:
    LANE=<lane> [OTel env] <template of the agent with model and the quoted line>."""
    import shlex
    full = load_sessions(full=True)
    agent, model = lane_agent(lane), lane_model(lane)
    env = f"LANE={shlex.quote(lane)} "  # the cchook.py hook files every Claude Code event under this lane
    if agent == "claude" and otel_on():  # the OTel env is Claude Code's own telemetry
        env += OTEL_ENV.format(lane=shlex.quote(lane)) + " "
        if full.get("otel_traces"):
            env += OTEL_TRACES_ENV + " "
    tmpl = ((full.get("templates") or {}) if isinstance(full.get("templates"), dict) else {}).get(agent) \
        or DEFAULT_TEMPLATES[agent]
    return env + tmpl.replace("{model}", f" --model {shlex.quote(model)}" if model else "").replace("{start}", shlex.quote(line))


def start_todo(lane, hub=""):
    """msg / cmd of the start todo of one lane: `LANE=<lane> [OTel env] claude --model <model> '<start line>'` (one
    pastable line; Claude Code takes the first prompt as an argument; LANE tells the cchook.py hook which lane it is),
    plus /fast in the message when the lane runs in fast mode. A lane with "agent": "codex" in sessions.json gets the
    codex template instead (Codex also takes the first prompt as an argument; `codex --help`: [PROMPT]) and a start line
    that points at AGENTS.md and lane:msg / lane:inbox."""
    full = load_sessions(full=True)
    cfg = (full.get("lanes") or {}).get(lane) or {}
    agent = lane_agent(lane)
    line = (cfg.get("start_codex") if agent == "codex" else "") or cfg.get("start") or ""
    line = line.replace("{hub}", hub or "<ハブの名前>")
    if not line:
        raise LaneError(f"sessions.json has no start line for lane {lane}")
    if agent == "codex" and not cfg.get("start_codex"):
        line += CODEX_START.format(lane=lane)
    elif agent == "claude" and lane != "1" and lane_agent("1") == "codex":
        line += HUB_CODEX_NOTE.format(lane=lane)
    cmd = session_cmd(lane, line)
    cx = full.get("codex") if isinstance(full.get("codex"), dict) else {}
    model = lane_model(lane)
    if agent == "codex":
        label = "Codex " + (cfg.get("codex_model_label") or cx.get("model_label") or model or "既定のモデル")
        fast = "。承認は自動レビュー（--approve-for-me）。初回に repo を trust するか聞かれたら trust"
    else:
        label = cfg.get("model_label") or model or "既定のモデル"
        fast = "。起動したら /fast を打つ（fast mode）" if cfg.get("fast") else ""
    msg = (f"レーン {lane}（{LANE_NAMES.get(lane, lane)}）を起動する: 新しいタブで repo のルートに cd → CMD を貼る（{label}）{fast}"
           + ("。ハブが名前を出したら、ハブが task lane:starts で 2 / 3 / c の起動の todo を作る" if lane == "1" else ""))
    return {"msg": clean(msg, MAX_TEXT), "cmd": clean(cmd, MAX_CMD)}


def empty_board():
    return {"phase": "setup", "since": 0, "lanes": {}, "alerts": [], "todos": [], "next_id": 0, "steps": {},
            "bench": {"state": "none"}, "msgs": [], "next_msg": 0}


def load(path):
    try:
        with open(path) as f:
            b = json.load(f)
    except (OSError, ValueError):
        return empty_board()
    base = empty_board()
    base.update(b if isinstance(b, dict) else {})
    if not isinstance(base.get("steps"), dict):
        base["steps"] = {}
    if not isinstance(base.get("bench"), dict):
        base["bench"] = {"state": "none"}
    if not isinstance(base.get("msgs"), list):
        base["msgs"] = []
    return base


def save(path, b):
    tmp = path + ".tmp"
    with open(tmp, "w") as f:
        json.dump(b, f, ensure_ascii=False, indent=1)
    os.replace(tmp, path)


def apply(b, op, a, t):
    """Mutates board b with one op; returns the created id or ''."""
    lane = (a.get("lane") or "").strip().lower()

    def need_lane():
        if not LANE_RE.match(lane):
            raise LaneError("lane required (1, 2, 3, c; [a-z0-9_-], <= 8 chars)")
        if lane not in b["lanes"] and len(b["lanes"]) >= MAX_LANES:
            raise LaneError(f"too many lanes (max {MAX_LANES})")

    def ensure():
        return b["lanes"].setdefault(lane, {"state": "idle", "step": "", "note": "", "since": t, "updated": t})

    def set_state(l, st, lid=None):
        if l["state"] != st:
            emit("lane", t, lane=lid or lane, frm=l["state"], to=st, step=l.get("step", ""), note=l.get("note", "")[:60],
                 agents=l.get("agents", 0))
            l["state"], l["since"] = st, t
        l["updated"] = t

    new_id = ""
    if op == "status":
        need_lane()
        st = (a.get("state") or "").strip()
        if st not in STATES:
            raise LaneError("state must be one of " + "|".join(STATES))
        l = ensure()
        old = (l["state"], l.get("step", ""), l.get("note", ""), l.get("agents", 0))
        if "step" in a:
            l["step"] = clean(a["step"], MAX_SHORT)
        if "note" in a:
            l["note"] = clean(a["note"], MAX_SHORT)
        ag = (a.get("agents") or "").strip()
        if ag.isdigit():  # subagents this lane is running now (AGENTS=3 when it launches them, AGENTS=0 when they are back)
            l["agents"] = min(int(ag), 99)
        if st == "idle":
            l["agents"] = 0
        if old[0] == st and old[1:] != (l.get("step", ""), l.get("note", ""), l.get("agents", 0)):
            emit("lane_step", t, lane=lane, state=st, step=l.get("step", ""), note=l.get("note", "")[:60],
                 agents=l.get("agents", 0))  # lane:timeline (agents: its サブエージェント待ち)
        set_state(l, st)
        l.pop("prev_state", None)
    elif op == "hook":  # cchook.py: the state follows the session (prompt → working, [PR]/[DONE] → waiting, Stop → idle)
        need_lane()
        to = (a.get("to") or "").strip()
        if to not in HOOK_FROM:
            raise LaneError("to must be one of " + "|".join(HOOK_FROM))
        l = ensure()
        # never over alert, nor over a hand-set waiting / waiting-human; Stop keeps working while subagents run (AGENTS=n)
        if l["state"] in HOOK_FROM[to] and not (to == "idle" and l.get("agents")):
            set_state(l, to)
    elif op == "alert":
        need_lane()
        lv = (a.get("level") or "warn").strip()
        if lv not in LEVELS:
            raise LaneError("level must be warn|crit")
        msg = clean(a.get("msg"), MAX_TEXT)
        if not msg:
            raise LaneError("msg required")
        b["next_id"] += 1
        new_id = f"a{b['next_id']}"
        b["alerts"].append({"id": new_id, "lane": lane, "level": lv, "msg": msg, "impact": clean(a.get("impact"), MAX_TEXT),
                            "created": t, "resolved": False, "resolved_at": None})
        emit("alert", t, id=new_id, lane=lane, level=lv, msg=msg[:120])
        l = ensure()
        if l["state"] != "alert":
            l["prev_state"] = l["state"]
        set_state(l, "alert")
    elif op == "resolve":
        al = next((x for x in b["alerts"] if x["id"] == (a.get("id") or "").strip()), None)
        if al is None:
            raise LaneError("no such alert id")
        if not al["resolved"]:
            al["resolved"], al["resolved_at"] = True, t
            emit("resolve", t, id=al["id"], lane=al["lane"], sec=t - (al.get("created") or t))
            l = b["lanes"].get(al["lane"])
            if l and l["state"] == "alert" and not any(x["lane"] == al["lane"] and not x["resolved"] for x in b["alerts"]):
                prev = l.pop("prev_state", "") or "working"
                set_state(l, "working" if prev == "alert" else prev, al["lane"])
    elif op == "init":
        # first todos of the day, once per board (a new contest repo = a new board): what the human does before any lane runs
        if b.get("seeded"):
            return "already"
        b["seeded"] = t
        if b["lanes"] or b["todos"]:  # a board already in use (e.g. made before this feature): do not add the morning todos
            return "already"
        if is_practice():  # t0 keeps t1 t2 t3 as they are (the lane files name them)
            td = {**OTEL_TODO, "lane": "1", "created": t, "done": False, "done_at": None}
            b["todos"].append(td)
            emit("todo", t, id="t0", lane="1", tkind="otel", msg=td["msg"][:80])
        for sd in SEED_TODOS:
            b["next_id"] += 1
            sd = dict(sd)
            if sd.get("start"):
                try:
                    sd.update(start_todo(sd["start"]))
                except LaneError:  # no sessions.json: the plain start line
                    sd.update({"msg": "レーン 1（ハブ）を起動する（repo のルートで claude → この 1 行を貼る）",
                               "cmd": "あなたはレーン 1（ハブ）。docs/lanes/README.md と docs/lanes/1-hub.md を読んで従って。最初に ListAgents で自分の名前を出して。"})
                sd.update({"kind": "start", "start_lane": sd["start"]})
            td = {"id": f"t{b['next_id']}", "lane": "1", "msg": sd["msg"], "cmd": sd["cmd"], "why": sd["why"],
                  "after": sd["after"], "created": t, "done": False, "done_at": None}
            for k in ("inputs", "envkey", "kind", "start_lane"):
                if sd.get(k):
                    td[k] = sd[k]
            b["todos"].append(td)
            emit("todo", t, id=td["id"], lane="1", tkind=todo_kind(td), msg=td["msg"][:80])
    elif op == "starts":
        # the hub, right after it knows its name: the start todos of lanes 2 / 3 / c (model + start line from sessions.json)
        hub = clean(a.get("hub"), 60)
        if not hub:
            raise LaneError("hub=<the hub's name from ListAgents> required")
        made = []
        for sl in ("2", "3", "c"):
            if any(x.get("start_lane") == sl and not x["done"] for x in b["todos"]):
                continue  # already open: do not stack a second one
            st = start_todo(sl, hub)
            b["next_id"] += 1
            td = {"id": f"t{b['next_id']}", "lane": "1", "msg": st["msg"], "cmd": st["cmd"],
                  "why": f"レーン {sl}（{LANE_NAMES.get(sl, sl)}）のセッション（{lane_agent(sl)}）。起動するとハブ {hub} に [HELLO] を送る"
                         + ("（task lane:msg）" if "codex" in (lane_agent(sl), lane_agent("1")) else ""),
                  "after": "✅ を押す（[HELLO] が来たらハブが確かめる）", "created": t, "done": False, "done_at": None,
                  "kind": "start", "start_lane": sl, "hub": hub}
            b["todos"].append(td)
            emit("todo", t, id=td["id"], lane="1", tkind="start", msg=td["msg"][:80])
            made.append(td["id"])
        new_id = " ".join(made) or "already"
    elif op == "todo":
        need_lane()
        msg = clean(a.get("msg"), MAX_TEXT)
        if not msg:
            raise LaneError("msg required")
        prs = parse_prs(a.get("pr"))
        pr = prs[0] if prs else None
        kind = (a.get("kind") or "").strip().lower()
        if kind and not KIND_RE.match(kind):
            raise LaneError("kind must be [a-z][a-z0-9_-], <= 16 chars (bench, review, start …)")
        choices, choice_desc = [], {}
        for x in (a.get("choices") or "").split("|"):
            x, desc = split_desc(x, MAX_CHOICE)
            if x and x not in choices:
                choices.append(x)
                if desc:
                    choice_desc[x] = desc
        picks = []
        for x in (a.get("picks") or "").split("|"):
            lab, desc = split_desc(x, MAX_PICK)
            if not lab:
                continue
            if any(p["key"] == pick_key(lab) for p in picks):
                raise LaneError(f"picks: key {pick_key(lab)!r} twice (the first word of each item must differ)")
            picks.append({"key": pick_key(lab), "label": lab, "desc": desc})
        if len(picks) > MAX_PICKS:
            raise LaneError(f"at most {MAX_PICKS} picks")
        if picks and (pr or choices or (a.get("input") or "").strip()):
            raise LaneError("picks does not mix with pr / choices / input (one kind of answer per todo)")
        if choices and pr:
            raise LaneError("choices and pr do not mix (a PR todo already has merge OK / 要修正)")
        if "done" in choices:
            raise LaneError('"done" is what lane:done writes; pick another choice text')
        if len(choices) > MAX_CHOICES:
            raise LaneError(f"at most {MAX_CHOICES} choices")
        inputs = [clean(x, MAX_INPUT_LABEL) for x in (a.get("input") or "").split("|") if x.strip()]
        envkey = (a.get("envkey") or "").strip().upper()
        if inputs and (pr or choices):
            raise LaneError("input does not mix with pr / choices (one kind of answer per todo)")
        if len(inputs) > MAX_INPUTS:
            raise LaneError(f"at most {MAX_INPUTS} input fields")
        if envkey:
            if not inputs:
                raise LaneError('envkey needs input="<label>" (the field whose answer goes into .env)')
            if envkey not in ENV_KEYS:
                raise LaneError("envkey must be one of " + "|".join(ENV_KEYS))
            if envkey != "HOSTS" and len(inputs) != 1:
                raise LaneError(f"envkey={envkey} takes exactly one input field (only HOSTS joins several)")
        b["next_id"] += 1
        new_id = f"t{b['next_id']}"
        td = {"id": new_id, "lane": lane, "msg": msg, "cmd": clean(a.get("cmd"), MAX_CMD),
              "why": clean(a.get("why"), MAX_TEXT),
              "after": clean(a.get("after"), MAX_TEXT), "created": t, "done": False, "done_at": None}
        if pr:
            td["pr"], td["prs"] = pr, prs
            titles = a.get("titles") if isinstance(a.get("titles"), dict) else {}
            tt = {str(n): clean(titles.get(str(n)) or titles.get(n) or "", MAX_SHORT) for n in prs}
            if any(tt.values()):
                td["pr_titles"] = {k: v for k, v in tt.items() if v}
        if kind:
            td["kind"] = kind
        if choices:
            td["choices"] = choices
            if choice_desc:
                td["choice_desc"] = choice_desc
        if picks:
            td["picks"] = picks
        if inputs:
            td["inputs"] = inputs
        if envkey:
            td["envkey"] = envkey
        b["todos"].append(td)
        ensure()
        emit("todo", t, id=new_id, lane=lane, tkind=todo_kind(td), prs=" ".join(map(str, prs)), msg=msg[:80])
    elif op == "done":
        td = next((x for x in b["todos"] if x["id"] == (a.get("id") or "").strip()), None)
        if td is None:
            raise LaneError("no such todo id")
        if not td["done"]:
            td["done"], td["done_at"] = True, t
            td["decision"], td["decided_by"] = "done", "cli"
            emit_done(td, t)
    elif op == "note":  # one line of the story: an event only (lane:web reads events.jsonl), the board is not changed
        need_lane()
        nk = (a.get("kind") or "").strip().lower()
        if nk not in NOTE_KINDS:
            raise LaneError("kind must be one of " + "|".join(NOTE_KINDS))
        msg = clean(a.get("msg"), MAX_NOTE)
        if not msg:
            raise LaneError("msg required (one line, <= 120 chars)")
        emit("note", t, lane=lane, nkind=nk, msg=msg, ref=clean(a.get("ref"), MAX_REF), body=note_body(a.get("body")))
        b["notes_n"] = int(b.get("notes_n") or 0) + 1  # board.json changes → lane:web's long-poll wakes up
    elif op == "advance":  # hub:ship: the first of ids (in order) that is not done/skip becomes done
        plan_ids = {x["id"] for x in load_plan()}
        for sid in (a.get("ids") or "").upper().split():
            if (plan_ids and sid not in plan_ids) or (b["steps"].get(sid) or {}).get("state") in ("done", "skip"):
                continue
            cur = b["steps"].get(sid) or {}
            cur.update({"state": "done", "at": t})
            if (a.get("note") or "").strip():
                cur["note"] = clean(a["note"], MAX_SHORT)
            b["steps"][sid] = cur
            new_id = sid
            break
    elif op == "autostep":  # hub:ship, per merged PR: steps whose "auto" globs match its head branch / files → done
        pr = (a.get("pr") or "").strip().lstrip("#")
        hit = []
        for sid in auto_steps(load_plan(), a.get("branch") or "", (a.get("files") or "").split()):
            cur = b["steps"].get(sid) or {}
            if cur.get("state") in ("done", "skip"):
                continue
            cur.update({"state": "done", "at": t, "note": clean(f"PR #{pr}" if pr else "merged", MAX_SHORT), "by": "ship"})
            b["steps"][sid] = cur
            emit("step", t, id=sid, state="done")
            hit.append(sid)
        new_id = " ".join(hit)
    elif op == "msgstep":  # cchook.py ([PR]/[DONE]/[PICK] SendMessage) and op msg: plan steps whose "done_on" matches → done
        need_lane()
        new_id = " ".join(msg_steps(b, t, lane, a.get("msg") or ""))
    elif op == "benchstep":  # runs.py wait, per scored run: the next bench step B<n> → done (bench_step)
        new_id = bench_step(b, t, ts=(a.get("ts") or "").strip(), score=(a.get("score") or "").strip())
    elif op == "bench":
        cur = b["bench"]
        st = (a.get("state") or "").strip() or cur.get("state") or "none"  # no state: only sha / prs / next change
        if st not in BENCH_STATES:
            raise LaneError("state must be one of " + "|".join(BENCH_STATES))
        sha = (a.get("sha") or "").strip().strip("-")
        if st == "ready" and cur.get("state") in ("running", "collect") and not (sha and sha != cur.get("sha")):
            # 2026-10-27 20:54:42: the hub re-sent STATE=ready (to add NEXT) right after runs:wait saw the bench start
            # → the band said ready during the run. Only a new sha (a new ship) starts a new bench.
            new_id = f"kept {cur['state']} (ready only with a new SHA; NEXT / PRS updated)"
            st = cur["state"]
        if cur.get("state") != st:
            emit("bench", t, frm=cur.get("state") or "none", to=st, sha=(a.get("sha") or "").strip().strip("-"),
                 prs=prs_text((a.get("prs") or "").strip().strip("-")), src=clean(a.get("src"), 20))
            cur["since"] = t
            if st == "ready":  # a new bench: the previous 「終わった」 no longer applies
                for k in ("done_at", "done_score", "done_note"):
                    cur.pop(k, None)
        cur.update({"state": st, "at": t})
        for k in ("sha", "prs", "next"):  # empty keeps (the Taskfile always passes them), "-" clears
            v = (a.get(k) or "").strip()
            if v == "-":
                cur.pop(k, None)
            elif v:
                cur[k] = clean(prs_text(v) if k in ("prs", "next") else v, MAX_SHORT)
    elif op == "step":
        sid = (a.get("id") or "").strip().upper()
        st = (a.get("state") or "").strip()
        if st not in STEP_STATES:
            raise LaneError("state must be one of " + "|".join(STEP_STATES))
        ids = {x["id"] for x in load_plan()}
        if ids and sid not in ids:
            raise LaneError(f"no such step {sid!r} in plan.json ({' '.join(sorted(ids))})")
        cur = b["steps"].get(sid) or {}
        if st == "todo" and not a.get("note"):
            b["steps"].pop(sid, None)  # back to automatic (bench steps) / plain todo
        else:
            cur.update({"state": st, "at": t})
            if (a.get("note") or "").strip():  # empty NOTE (the Taskfile always passes one) keeps the old note
                cur["note"] = clean(a["note"], MAX_SHORT)
            b["steps"][sid] = cur
        emit("step", t, id=sid, state=st)
    elif op in ("items", "item"):
        sid = (a.get("id") or "").strip().upper()
        ids = {x["id"] for x in load_plan()}
        if ids and sid not in ids:
            raise LaneError(f"no such step {sid!r} in plan.json")
        cur = b["steps"].setdefault(sid, {"state": "doing", "at": t})
        if op == "items":  # the lane's own checklist for this step: progress = items done / items, nothing estimated
            names = [clean(x, MAX_SHORT) for x in (a.get("items") or "").split("|") if x.strip()]
            if not names:
                raise LaneError('items="a|b|c" required')
            cur["items"] = [{"t": n, "state": "todo"} for n in names][:20]
            if cur.get("state") in (None, "todo"):
                cur.update({"state": "doing", "at": t})
        else:
            st = (a.get("state") or "done").strip()
            if st not in STEP_STATES:
                raise LaneError("state must be one of " + "|".join(STEP_STATES))
            its = cur.get("items") or []
            n = (a.get("n") or "").strip()
            k = int(n) - 1 if n.isdigit() else next((i for i, x in enumerate(its) if x["t"] == (a.get("text") or "")), -1)
            if not 0 <= k < len(its):
                raise LaneError(f"no item {n or a.get('text')!r} in {sid} ({len(its)} items)")
            its[k]["state"], its[k]["at"] = st, t
    elif op == "phase":
        ph = (a.get("phase") or "").strip()
        if ph not in PHASES:
            raise LaneError("phase must be one of " + "|".join(PHASES))
        if b["phase"] != ph or not b["since"]:
            emit("phase", t, phase=ph, frm=b["phase"])
            b["phase"], b["since"] = ph, t
        sid, sst = PHASE_STEPS.get(ph, ("", ""))
        if sid and sid in {x["id"] for x in load_plan()} and (b["steps"].get(sid) or {}).get("state") not in ("doing", "done", "skip"):
            b["steps"][sid] = {**(b["steps"].get(sid) or {}), "state": sst, "at": t}
    elif op == "msg":  # a MESSAGES.md line on the board: the transport for Codex lanes (no SendMessage), any lane may use it
        frm, to = (a.get("from") or "").strip().lower(), (a.get("to") or "").strip().lower()
        if not (LANE_RE.match(frm) and LANE_RE.match(to)):
            raise LaneError("from= and to= required (1, 2, 3, c)")
        if frm == to or "1" not in (frm, to):
            raise LaneError("hub and spoke: 1 → 2 / 3 / c or 2 / 3 / c → 1 (docs/MESSAGES.md)")
        text = clean(a.get("msg"), MAX_TEXT)
        m = MSG_TAG_RE.match(text)
        if not m:
            raise LaneError('msg must be one docs/MESSAGES.md line starting with its tag, e.g. "[TASK] 3 fix TS=…"')
        b["next_msg"] = int(b.get("next_msg") or 0) + 1
        new_id = f"m{b['next_msg']}"
        b.setdefault("msgs", []).append({"id": new_id, "t": t, "from": frm, "to": to, "msg": text, "read_at": None})
        emit("msg", t, id=new_id, frm=frm, to=to, tag=m.group(1), msg=text[:80])
        msg_steps(b, t, frm, text)  # Codex lanes: the same done_on marking as cchook.py for SendMessage
        msgs = b["msgs"]
        while len(msgs) > MAX_MSGS:  # drop the oldest read message first
            del msgs[next((j for j, x in enumerate(msgs) if x.get("read_at")), 0)]
    elif op == "baseline":  # hosts-setup.sh after 0 NG: band ready + the baseline bench todo, without the hub
        # 2026-10-28: ready waited 2–3.5 min on deploy:init + the hub's [DONE] servers turn, which the baseline (the
        # undeployed initial state) does not need. Once per board: a second hosts:setup, or any earlier bench, skips.
        sha = (a.get("sha") or "").strip()
        bn = b["bench"]
        why = ("already offered" if b.get("baseline_todo") else f"phase is {b['phase']}" if b["phase"] != "setup"
               else "the band has moved before" if bn.get("since") or bn.get("state", "none") != "none"
               else "sha= required" if not sha else "")
        if why:
            return f"skip ({why})"
        url = next((x for x in reversed(b["todos"]) if x.get("kind") == "url"), None)
        vals = (url or {}).get("values") or []
        bench = (vals[2] if len(vals) > 2 else "").strip() or "本番: ポータルでベンチを実行 / 練習: t3 に書いたベンチ"
        new_id = apply(b, "todo", {"lane": "1", "kind": "bench", "msg": (
            f"baseline ベンチ（deploy しない初期状態 main {sha}。載っている PR: なし / 次に回す PR: setup・early-fix・c）。"
            f"{bench}"), "why": "hosts:setup 0 NG（計測の準備だけで足りる。deploy:init と [DONE] servers を待たない）",
            "after": "終わったら lane:web のこの todo の「🏁 終わった」（score とベンチの出力の最後を貼れる。裏で task bench:done）。"
                     "panel に score が入ればハブが task runs:wait で拾う"}, t)
        apply(b, "bench", {"state": "ready", "sha": sha, "prs": "baseline", "next": "setup early-fix c",
                           "src": "hosts:setup"}, t)
        b["baseline_todo"] = new_id
        apply(b, "msg", {"from": "2", "to": "1", "msg": f"[DONE] 2 baseline-ready {new_id} main {sha}"}, t)
    elif op == "hubpr":  # hubprs.py: a PR no lane announced (a teammate's) → one review todo for the hub, once per PR
        prs = parse_prs(a.get("pr"))
        if len(prs) != 1:
            raise LaneError("pr= one PR number")
        seen = b.setdefault("hub_prs", [])
        if prs[0] in seen or any(prs[0] in todo_prs(x) for x in b["todos"]):
            return "already"
        seen.append(prs[0])
        new_id = apply(b, "todo", {"lane": "1", "kind": "review", "pr": str(prs[0]), "msg": a.get("msg"),
                                   "why": a.get("why"), "after": a.get("after"),
                                   "titles": {str(prs[0]): a.get("title") or ""}}, t)
    elif op == "inbox":  # unread messages for the lane (then read); all=1 = the lane's history too
        need_lane()
        unread = [x for x in b.get("msgs", []) if x["to"] == lane and not x.get("read_at")]
        if str(a.get("all") or "").strip() in ("1", "true", "yes"):
            shown = [x for x in b.get("msgs", []) if lane in (x["to"], x["from"])][-MSG_HISTORY:]
        else:
            shown = unread
        lines = [msg_line(x, t) for x in shown]
        for x in unread:
            x["read_at"] = t
            emit("msg_read", t, id=x["id"], lane=lane, frm=x["from"], sec=t - (x.get("t") or t),
                 tag=(MSG_TAG_RE.match(x["msg"]) or [None, ""])[1])
        new_id = "\n".join(lines) or f"(lane {lane}: 未読のメッセージは無い)"
    else:
        raise LaneError("op must be status|alert|resolve|todo|done|note|phase|step|advance|autostep|msgstep|benchstep|bench|items|item|init|starts"
                        "|msg|inbox|baseline")
    for key, closed in (("alerts", "resolved"), ("todos", "done")):  # cap: drop the oldest closed first
        items = b[key]
        while len(items) > MAX_ITEMS:
            i = next((j for j, x in enumerate(items) if x[closed]), 0)
            del items[i]
    return new_id


def msg_line(x, t=None):
    """One board message as lane:inbox prints it: m4 10:42 1→3 [TASK] 3 fix TS=… (未読 / 既読 hh:mm)."""
    st = f"既読 {hm(x['read_at'])}" if x.get("read_at") else "未読"
    return f"{x['id']} {hm(x.get('t'))} {x['from']}→{x['to']} {x['msg']}  ({st})"


def unread_msgs(b, lane):
    return [x for x in b.get("msgs", []) if x["to"] == lane and not x.get("read_at")]


def wait_msg(a, poll=2.0):
    """Block until the lane has an unread board message, then print them (marked read) and exit 0; 2 on timeout.
    Claude sessions run it with run_in_background (the exit wakes them); Codex runs it in the foreground while waiting."""
    lane = (a.get("lane") or "").strip().lower()
    if not LANE_RE.match(lane):
        raise LaneError("lane required (1, 2, 3, c)")
    timeout = float(a.get("timeout") or 3600)
    deadline = time.time() + timeout
    while True:
        if unread_msgs(read_board(), lane):
            print(mutate("inbox", {"lane": lane}))
            return 0
        if time.time() > deadline:
            print(f"⏳ lane {lane}: no message in {int(timeout)} s")
            return 2
        time.sleep(poll)


def emit_done(td, t):
    emit("todo_done", t, id=td["id"], lane=td.get("lane"), tkind=todo_kind(td), decision=td.get("decision"),
         by=td.get("decided_by"), sec=t - (td.get("created") or t), ok_prs=" ".join(map(str, td.get("ok_prs") or [])),
         held_prs=" ".join(map(str, td.get("held_prs") or [])), picked=" ".join(td.get("picked") or []),
         dropped=" ".join(td.get("dropped") or []))


def locked(fn):
    """fn(board, t) under the board's flock; the board is saved after it returns, then its events are appended."""
    d = lanes_dir()
    with open(os.path.join(d, "lock"), "w") as lk:
        fcntl.flock(lk, fcntl.LOCK_EX)
        del _PENDING[:]
        path = os.path.join(d, "board.json")
        b = load(path)
        out = fn(b, now())
        save(path, b)
        write_events(d, _PENDING)
        del _PENDING[:]
    return out


def mutate(op, a):
    return locked(lambda b, t: apply(b, op, a, t))


def msg_steps(b, t, lane, text):
    """Steps of <lane> whose plan.json "done_on" regexes match a sent message (one docs/MESSAGES.md line) → done,
    note = the message's first 40 chars, by=msg. Done / skip steps are kept. Returns the ids marked."""
    text = " ".join(str(text or "").split())
    hit = []
    if not text:
        return hit
    for x in load_plan():
        if x["lane"] != lane or not x.get("done_on"):
            continue
        if not any(re.search(rx, text) for rx in x["done_on"]):
            continue
        cur = b["steps"].get(x["id"]) or {}
        if cur.get("state") in ("done", "skip"):
            continue
        cur.update({"state": "done", "at": t, "note": clean(text[:40], MAX_SHORT), "by": "msg"})
        b["steps"][x["id"]] = cur
        emit("step", t, id=x["id"], state="done")
        hit.append(x["id"])
    return hit


def bench_step(b, t, ts="", score="", button=False, advance=True):
    """A bench ended: the first bench step B<n> (plan order, never ts order) that is not done/skip → done, note
    "<ts> score <n>". One bench may end twice — the human's 「終わった」 (button, no ts) and runs:wait's run_scored (ts):
    a run within BENCH_MERGE_SEC of a button-only mark (or a button after a run-only mark) fills that step instead of
    ticking the next one; a ts already on a step is ignored (two waits log the same run). Returns the id or ''."""
    ids = [x["id"] for x in load_plan() if x["lane"] == "bench" and re.fullmatch(r"B\d+", x["id"])]
    marked = [(sid, b["steps"][sid]) for sid in ids if (b["steps"].get(sid) or {}).get("by") == "bench"]
    if ts and any(s.get("ts") == ts for _, s in marked):
        return ""
    sc = f" score {score}" if score else ""
    last = max(marked, key=lambda kv: kv[1].get("bench_at") or 0, default=None)
    if last and t - (last[1].get("bench_at") or 0) <= BENCH_MERGE_SEC:
        sid, cur = last
        if ts and not cur.get("ts"):  # the run of the bench the human already marked 「終わった」
            cur.update({"ts": ts, "note": clean(f"{ts}{sc}", MAX_SHORT)})
            return sid
        if button and cur.get("ts") and not cur.get("button"):  # 「終わった」 pressed after runs:wait saw the score
            cur["button"] = True
            return ""
    if not advance:
        return ""
    sid = next((x for x in ids if (b["steps"].get(x) or {}).get("state") not in ("done", "skip")), "")
    if not sid:
        return ""
    cur = b["steps"].get(sid) or {}
    cur.update({"state": "done", "at": t, "bench_at": t, "by": "bench",
                "note": clean(f"{ts}{sc}" if ts else f"{hm(t)} 終わった{sc}", MAX_SHORT)})
    if ts:
        cur["ts"] = ts
    if button:
        cur["button"] = True
    b["steps"][sid] = cur
    emit("step", t, id=sid, state="done")
    return sid


def decide(tid, decision, by="web", held=None, dropped=None):
    """The human's decision on a PR todo (ok|changes) or on a todo with choices (one of its choice texts): closes it
    with that decision. A review todo with several PRs takes held = the PRs the human toggled off: ok_prs / held_prs
    are stored (changes holds them all). Only lane:web's POST /decide calls this (it is not a CLI op, so `task lane:*`
    cannot write a decision). Not a real defence: anything running as the user can edit board.json or read the web
    token — it only keeps a session from doing it by accident."""
    decision = (decision or "").strip()
    held = parse_prs(held) if not isinstance(held, list) else held

    def fn(b, t):
        td = next((x for x in b["todos"] if x["id"] == (tid or "").strip()), None)
        if td is None:
            raise LaneError("no such todo id")
        if td.get("picks"):
            keys = [p["key"] for p in td["picks"]]
            drop = [x for x in re.split(r"[\s,]+", str(dropped or "")) if x]
            bad = [x for x in drop if x not in keys]
            if bad:
                raise LaneError(f"dropped: {bad[0]!r} is not one of {' '.join(keys)}")
            if decision not in ("ok", "none"):
                raise LaneError("decision must be ok|none for a picks todo")
            if td["done"]:
                raise LaneError(f"{td['id']} is already closed (decision={td.get('decision', 'done')})")
            picked = [k for k in keys if decision == "ok" and k not in drop]
            td.update({"done": True, "done_at": t, "decision": "ok" if picked else "none", "decided_by": by,
                       "decided_at": t, "picked": picked, "dropped": [k for k in keys if k not in picked]})
            emit_done(td, t)
            return td["id"]
        if td.get("choices"):
            if decision not in td["choices"]:
                raise LaneError("decision must be one of: " + " | ".join(td["choices"]))
        elif not td.get("pr"):
            raise LaneError("not a PR todo nor a todo with choices / picks (use ✅)")
        elif decision not in DECISIONS:
            raise LaneError("decision must be ok|changes")
        if td["done"]:
            raise LaneError(f"{td['id']} is already closed (decision={td.get('decision', 'done')})")
        if held and not td.get("pr"):
            raise LaneError("held= only for a review todo")
        upd = {"done": True, "done_at": t, "decision": decision, "decided_by": by, "decided_at": t}
        if td.get("pr"):
            prs = todo_prs(td)
            bad = [p for p in held if p not in prs]
            if bad:
                raise LaneError(f"held: #{bad[0]} is not in this todo ({' '.join('#%d' % p for p in prs)})")
            if decision == "ok" and len(set(held)) == len(prs):
                raise LaneError("全部の PR を外すなら「✋ 要修正」")
            upd["ok_prs"] = [p for p in prs if p not in held] if decision == "ok" else []
            upd["held_prs"] = [p for p in prs if p in held] if decision == "ok" else prs
        td.update(upd)
        emit_done(td, t)
        return td["id"]
    return locked(fn)


def board_ok(pr):
    """"盤 hh:mm" when the human pressed merge OK on lane:web for a todo that names PR <pr> (and did not hold it); ''."""
    best = None
    for x in read_board()["todos"]:
        if x.get("decision") != "ok" or x.get("decided_by") != "web" or pr not in todo_prs(x):
            continue
        if "ok_prs" in x and pr not in x["ok_prs"]:
            continue
        if best is None or (x.get("decided_at") or 0) > (best.get("decided_at") or 0):
            best = x
    return f"盤 {hm(best.get('decided_at') or best.get('done_at'))}" if best else ""


def bench_done(score="", output="", by="web", tid=""):
    """「🏁 終わった」 on lane:web's bench todo (KIND=bench, made by hub:ship; token-protected POST, like decisions): the
    todo closes with decision=done (lane:wait prints score=<n> when one was typed), and the band — still kept for
    hub:ship / runs:wait / lane:board — goes to collect when it is ready / running; the time and the optional score are
    kept on the band and logged (bench_done event). Without tid (old callers) the band must be ready / running.
    Returns the board's bench dict. The pasted output is written by the caller (lane:web) next to the board."""
    score = (score or "").strip().replace(",", "").replace("_", "")
    if score and not re.fullmatch(r"[0-9]{1,9}(\.[0-9]+)?", score):
        raise LaneError("score は数字だけ（空でもよい）")
    if len(output or "") > MAX_BENCH_OUTPUT:
        raise LaneError(f"ベンチの出力は {MAX_BENCH_OUTPUT} 文字まで（最後の数行で十分）")
    tid = (tid or "").strip()

    def fn(b, t):
        cur = b["bench"]
        st = cur.get("state") or "none"
        td = None
        if tid:
            td = next((x for x in b["todos"] if x["id"] == tid), None)
            if td is None:
                raise LaneError("no such todo id")
            if td.get("kind") != "bench":
                raise LaneError(f"{tid} はベンチの todo ではない（KIND=bench だけ「終わった」）")
            if td["done"]:
                raise LaneError(f"{tid} is already closed (decision={td.get('decision', 'done')})")
        elif st not in ("ready", "running"):
            raise LaneError(f"帯が {st}: id の無い「終わった」は ready / running のときだけ（ボタンはベンチの todo にある）")
        emit("bench_done", t, frm=st, score=score, by=by, sha=cur.get("sha"), prs=cur.get("prs"), id=tid)
        if st in ("ready", "running"):
            emit("bench", t, frm=st, to="collect", sha=cur.get("sha"), prs=cur.get("prs"), src="bench_done")
            cur.update({"state": "collect", "since": t, "at": t})
        cur["done_at"] = t
        cur.pop("done_score", None)
        if score:
            cur["done_score"] = score
        cur.pop("done_note", None)
        # a band already none = runs:wait saw this bench's score first: the press only joins that step
        bench_step(b, t, score=score, button=True, advance=st in ("ready", "running", "collect"))
        if td is not None:
            td.update({"done": True, "done_at": t, "decision": "done", "decided_by": by, "decided_at": t})
            if score:
                td["done_score"] = score
            emit_done(td, t)
        return dict(cur)
    return locked(fn)


def bench_note(note):
    """What became of the bench:done run (lane:board shows it on the band; a failure also goes into 「📜 経緯」)."""
    def fn(b, t):
        b["bench"]["done_note"] = clean(note, MAX_TEXT)
    locked(fn)


HOST_RE = re.compile(r"^(?=.{1,253}$)[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$")
ENV_RE = {"HOST_LOGIN": re.compile(r"^[a-z_][a-z0-9_-]{0,31}$"), "CONTEST": re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$"),
          "GH_PROJECT": re.compile(r"^(?:[A-Za-z0-9][A-Za-z0-9-]{0,38}/)?[0-9]+$")}


def check_input(v):
    """One answer typed on lane:web: a single line, stripped, at most MAX_INPUT chars, no control characters."""
    v = v if isinstance(v, str) else ""
    if any(ord(c) < 0x20 or ord(c) == 0x7f or c in "\u2028\u2029" for c in v):
        raise LaneError("改行や制御文字は入れられない（1 行で）")
    v = v.strip()
    if len(v) > MAX_INPUT:
        raise LaneError(f"{MAX_INPUT} 文字まで")
    return v


def env_value(key, values):
    """The .env value for an envkey todo's answers (validated); LaneError when it does not fit the key."""
    if key not in ENV_KEYS:
        raise LaneError("envkey must be one of " + "|".join(ENV_KEYS))
    if key == "HOSTS":
        hosts = [h for v in values for h in re.split(r"[\s,]+", v) if h]
        if not hosts:
            raise LaneError("HOSTS: IP を 1 つ以上")
        for h in hosts:
            if re.fullmatch(r"[0-9.]+", h):
                try:
                    ipaddress.IPv4Address(h)
                except ValueError:
                    raise LaneError(f"HOSTS: {h!r} は IPv4 ではない")
            elif not HOST_RE.match(h):
                raise LaneError(f"HOSTS: {h!r} は IPv4 でもホスト名でもない")
        return " ".join(hosts)
    vals = [v for v in values if v]
    if len(vals) != 1:
        raise LaneError(f"{key}: 値を 1 つ")
    if key in ENV_RE and not ENV_RE[key].match(vals[0]):
        raise LaneError(f"{key}: 形が違う（{ENV_RE[key].pattern}）")
    return vals[0]


def env_line(key, value):
    """KEY=value as the Taskfile's dotenv (godotenv) reads it back: bare when safe (the way .env.example and
    hosts:setup's BENCH_CMD hint write it), else single quotes (no $ expansion, no # comment)."""
    if value and value[0] not in "'\"`" and "$" not in value and "#" not in value:
        return f"{key}={value}"
    if "'" not in value:
        return f"{key}='{value}'"
    raise LaneError(f"{key}: ' と $ / # / 先頭の引用符を同時に含む値は書けない（.env に手で書く）")


def env_path():
    """The .env lane:web writes: LANES_ENV_FILE, else ./.env (lane:web runs in the repo root), else ../../.env (a
    .claude/worktrees/<x> checkout, as the Taskfile's dotenv), else ./.env (created)."""
    p = os.environ.get("LANES_ENV_FILE", "").strip()
    if p:
        return p
    for c in (".env", os.path.join("..", "..", ".env")):
        if os.path.exists(c):
            return os.path.abspath(c)
    return os.path.abspath(".env")


def write_env(path, key, value):
    """Rewrite only the KEY= line(s) of path (append when absent); every other line stays byte-identical. Written to a
    temp file in the same directory and renamed over the old one; the file mode is kept (600 for a new file)."""
    if key not in ENV_KEYS:
        raise LaneError("envkey must be one of " + "|".join(ENV_KEYS))
    path = os.path.realpath(path)
    body = env_line(key, value).encode()
    try:
        with open(path, "rb") as f:
            data = f.read()
        mode = stat.S_IMODE(os.stat(path).st_mode)
    except FileNotFoundError:
        data, mode = b"", 0o600
    pat = re.compile(rb"^([ \t]*(?:export[ \t]+)?)" + re.escape(key.encode()) + rb"[ \t]*=")
    out, hit = [], False
    for line in data.splitlines(keepends=True):
        m = pat.match(line)
        if m:
            nl = line[len(line.rstrip(b"\r\n")):]
            out.append(m.group(1) + body + nl)
            hit = True
        else:
            out.append(line)
    if not hit:
        if out and not out[-1].endswith(b"\n"):
            out.append(b"\n")
        out.append(body + b"\n")
    d = os.path.dirname(path)
    tmp = os.path.join(d, f".{os.path.basename(path)}.lanes-{os.getpid()}.tmp")
    fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    try:
        with os.fdopen(fd, "wb") as f:
            f.write(b"".join(out))
            f.flush()
            os.fsync(f.fileno())
        os.chmod(tmp, mode)
        os.replace(tmp, path)
    except BaseException:
        try:
            os.unlink(tmp)
        except OSError:
            pass
        raise
    return path


def submit_input(tid, values, by="web"):
    """The human's answers to an input todo (one per label, in order): validated, written to .env when the todo has an
    envkey, stored on the todo (except non-HOSTS env values), and the todo closes with decision=done. Only lane:web's
    POST /input calls this (not a CLI op, so `task lane:*` cannot write answers; same caveat as decide())."""
    def fn(b, t):
        td = next((x for x in b["todos"] if x["id"] == (tid or "").strip()), None)
        if td is None:
            raise LaneError("no such todo id")
        labels = td.get("inputs") or []
        if not labels:
            raise LaneError("not an input todo")
        if td["done"]:
            raise LaneError(f"{td['id']} is already closed (decision={td.get('decision', 'done')})")
        if len(values) > len(labels):
            raise LaneError(f"{len(labels)} fields, got {len(values)}")
        vals = [check_input(v) for v in values] + [""] * (len(labels) - len(values))
        if not any(vals):
            raise LaneError("どれか 1 つは入力する")
        key = td.get("envkey") or ""
        upd = {"done": True, "done_at": t, "decision": "done", "decided_by": by, "decided_at": t, "answered": True}
        if key:
            value = env_value(key, vals)
            try:
                write_env(env_path(), key, value)
            except OSError as e:
                raise LaneError(f".env を書けない: {e}")
            if key in ENV_SHOWN:
                upd.update({"values": vals, "env_value": value})
        else:
            upd["values"] = vals
        td.update(upd)
        emit_done(td, t)
        return td["id"]
    return locked(fn)


def web_done(tid):
    """✅ on lane:web for a todo without a PR or choices (those need merge OK / 要修正 or a choice button)."""
    def fn(b, t):
        td = next((x for x in b["todos"] if x["id"] == (tid or "").strip()), None)
        if td is None:
            raise LaneError("no such todo id")
        if td.get("pr") and not td["done"]:
            raise LaneError("PR todo: 「merge OK」か「要修正」を押す")
        if td.get("choices") and not td["done"]:
            raise LaneError("選択肢の todo: どれかの選択肢のボタンを押す")
        if td.get("picks") and not td["done"]:
            raise LaneError("選ぶ todo: 「選んだものを OK」か「全部やめる」を押す")
        if td.get("inputs") and not td["done"]:
            raise LaneError("入力の todo: 入力して「送信」を押す")
        if not td["done"]:
            td.update({"done": True, "done_at": t, "decision": "done", "decided_by": "web"})
            emit_done(td, t)
        return td["id"]
    return locked(fn)


def prs_text(v):
    """"7 8" / "#7,#8" → "#7 #8"; anything else is kept as typed."""
    toks = [x.lstrip("#") for x in re.split(r"[\s,]+", v.strip()) if x.strip()]
    return " ".join("#" + x if x.isdigit() else x for x in toks)


_REPO_URL = None


def repo_url():
    """https://github.com/<owner>/<repo> from GIT_REMOTE (.env) or `git remote get-url origin`; '' when unknown."""
    global _REPO_URL
    if _REPO_URL is not None:
        return _REPO_URL
    r = os.environ.get("GIT_REMOTE", "").strip()
    if not r:
        try:
            r = subprocess.run(["git", "remote", "get-url", "origin"], capture_output=True, text=True).stdout.strip()
        except FileNotFoundError:
            r = ""
    m = re.match(r"^(?:[a-z+]+://)?(?:[^@/]+@)?([^:/]+)(?::\d+)?[:/](.+?)(?:\.git)?/?$", r)
    host = m.group(1) if m else ""
    _REPO_URL = f"https://{host if '.' in host else 'github.com'}/{m.group(2)}" if m else ""
    return _REPO_URL


def pr_url(n):
    base = repo_url()
    return f"{base}/pull/{n}" if base and n else ""


def bench_text(bn):
    """One line for the bench band: what the human may do with the bench right now."""
    st = bn.get("state") or "none"
    on = f"main {bn['sha']}" if bn.get("sha") else "main"
    prs = f" ({bn['prs']})" if bn.get("prs") else ""
    nxt = f"  次に回す: {bn['next']}" if bn.get("next") else ""
    return {"ready": f"いま回してよい: {on}{prs}",
            "running": f"実行中: {on}{prs}（終わるまで ship しない）",
            "collect": f"ended を確かめて collect → report → score（{on}{prs}）",
            }.get(st, "ベンチの予定なし（ship 待ち）") + nxt


def recent_decisions(b, n=5):
    """Closed PR / choice / input todos, newest first: what the human decided on the board."""
    xs = [x for x in b["todos"] if (x.get("pr") or x.get("choices") or x.get("inputs") or x.get("picks")) and x["done"]]
    return sorted(xs, key=lambda x: x.get("done_at") or 0, reverse=True)[:n]


def decision_text(x):
    """The decision as the board shows it; an input todo's answers are never echoed (only that it was answered)."""
    if x.get("inputs"):
        return ("入力済み" + (f"（.env {x['envkey']}）" if x.get("envkey") else "")) if x.get("answered") else "done"
    if x.get("picks"):
        return ("ok " + " ".join(x.get("picked") or [])) if x.get("picked") else x.get("decision", "done")
    if x.get("decision") == "ok" and x.get("held_prs"):
        return "ok（保留 " + " ".join(f"#{p}" for p in x["held_prs"]) + "）"
    return x.get("decision", "done")


def prs_label(x):
    return " ".join(f"#{p}" for p in todo_prs(x))


def inputs_text(x):
    return " / ".join(x["inputs"]) + (f" → .env の {x['envkey']}" if x.get("envkey") else "")


def age(t0, t):
    return "-" if not t0 else f"{max(0, (t - t0) // 60)} min"


def hm(t):
    return datetime.fromtimestamp(t).strftime("%H:%M") if t else "--:--"


def lane_order(lanes):
    rank = {"1": 0, "2": 1, "3": 2, "c": 3}
    return sorted(lanes, key=lambda k: (rank.get(k, 9), k))


def lane_label(k):
    return f"{k} {LANE_NAMES[k]}" if k in LANE_NAMES else k


def todo_cmd(td):
    """A start todo's command is rebuilt from sessions.json and .env (LANES_OTEL) each time it is shown or copied."""
    if td.get("start_lane"):
        try:
            return start_todo(td["start_lane"], td.get("hub", ""))["cmd"]
        except LaneError:
            pass
    return td.get("cmd", "")


def copy_text(td):
    """What the human pastes for a todo: its cmd, else its msg."""
    return todo_cmd(td) or td["msg"]


def to_clipboard(text):
    """Returns the tool used, or '' when no clipboard tool is available."""
    for tool in (["pbcopy"], ["wl-copy"], ["xclip", "-selection", "clipboard"]):
        try:
            subprocess.run(tool, input=text, text=True, check=True, capture_output=True, timeout=5)
            return tool[0]
        except (FileNotFoundError, subprocess.CalledProcessError, subprocess.TimeoutExpired):
            continue
    return ""


def open_todos(b):
    return [x for x in b["todos"] if not x["done"]]


def open_alerts(b):
    return [x for x in b["alerts"] if not x["resolved"]]


# ---------- roadmap: plan.json + board steps + scored runs ----------

def plan_path():
    return os.environ.get("LANES_PLAN") or os.path.join(os.path.dirname(os.path.abspath(__file__)), "plan.json")


def load_plan():
    try:
        with open(plan_path()) as f:
            steps = json.load(f).get("steps") or []
    except (OSError, ValueError, AttributeError):
        return []
    out = []
    for x in steps:
        if isinstance(x, dict) and x.get("id"):
            out.append({"id": str(x["id"]).upper(), "lane": str(x.get("lane", "1")), "title": x.get("title", ""),
                        "deps": [str(d).upper() for d in x.get("deps") or []], "stage": int(x.get("stage", 0)),
                        # soft = "if it is ready in time, it rides along" (drawn dotted; never blocks, not on the critical path)
                        "soft": [str(d).upper() for d in x.get("soft") or []],
                        "how": x.get("how", ""), "auto": auto_rule(x.get("auto")),
                        "done_on": done_on_rule(x.get("done_on"))})
    return out


def done_on_rule(v):
    """plan.json "done_on": [regex] matched (re.search) against the messages the step's lane sends; bad regexes are
    dropped (empty = never marked by a message)."""
    out = []
    for rx in v if isinstance(v, list) else ([v] if isinstance(v, str) else []):
        rx = str(rx)
        if not rx.strip():
            continue
        try:
            re.compile(rx)
        except re.error:
            continue
        out.append(rx)
    return out


def auto_rule(v):
    """plan.json "auto": {"branches": [glob], "not": [glob], "files": [glob]} → lists of str (empty = never auto)."""
    v = v if isinstance(v, dict) else {}
    return {k: [str(g) for g in (v.get(k) or []) if str(g).strip()] for k in ("branches", "not", "files")}


def auto_match(rule, branch, files):
    """A merged PR matches a step: its head branch matches a "branches" glob (and no "not" glob), or one of its changed
    files matches a "files" glob. fnmatch, case-sensitive; * also crosses "/"."""
    rule = rule or {}
    br = (branch or "").strip()
    if br and any(fnmatch.fnmatchcase(br, g) for g in rule.get("branches") or []) \
            and not any(fnmatch.fnmatchcase(br, g) for g in rule.get("not") or []):
        return True
    return any(fnmatch.fnmatchcase(f, g) for f in files or [] for g in rule.get("files") or [])


def auto_steps(plan, branch, files):
    """Ids (plan order) of the steps a merged PR completes."""
    return [x["id"] for x in plan if auto_match(x.get("auto"), branch, files)]


_TOPLEVEL = {}


def repo_toplevel():
    """git rev-parse --show-toplevel, once per cwd: lane:web's payload() called it on every poll (a fork + exec,
    ~15 of the ~17 ms of each /board.json)."""
    cwd = os.getcwd()
    if cwd not in _TOPLEVEL:
        _TOPLEVEL[cwd] = subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True, text=True).stdout.strip()
    return _TOPLEVEL[cwd]


def prize_target():
    v = os.environ.get("PRIZE_TARGET", "")
    try:
        top = repo_toplevel()
        with open(os.path.join(top or ".", "alp.yml")) as f:
            m = re.search(r"^prize_target:\s*([0-9][0-9_,.]*)", f.read(), re.M)
            if m:
                v = m.group(1)
    except OSError:
        pass
    try:
        return int(float(v.replace(",", "").replace("_", ""))) if v else None
    except ValueError:
        return None


def bench_runs(runs):
    """Scored runs that are benches (not mis-detections), oldest first: [{ts, score}]."""
    out = []
    for r in runs or []:
        try:
            sc = float(str(r.get("score")))
        except (TypeError, ValueError):
            continue
        out.append({"ts": r.get("ts", ""), "score": sc, "req": int(r.get("req") or 0)})
    return sorted(out, key=lambda r: r["ts"])


def critical_path(plan, goal="P1"):
    by = {x["id"]: x for x in plan}
    seen, stack = set(), [goal] if goal in by else []
    while stack:
        k = stack.pop()
        if k in seen:
            continue
        seen.add(k)
        stack += [d for d in by[k]["deps"] if d in by]
    return seen


def roadmap(b, plan=None, runs=None, target=None):
    """Effective state per step: manual (lane:step) done / skip wins; an automatic done (bench steps B<n> / P1 from the
    scored runs) wins over a manual todo / doing; a todo step is ready when every dep is done/skip, else blocked."""
    plan = load_plan() if plan is None else plan
    benches = bench_runs(runs)
    best = max((r["score"] for r in benches), default=None)
    auto = {}
    for x in plan:
        m = re.fullmatch(r"B(\d+)", x["id"])
        if x["lane"] == "bench" and m and len(benches) >= int(m.group(1)):
            r = benches[int(m.group(1)) - 1]
            auto[x["id"]] = {"state": "done", "note": f"{r['ts']} score={r['score']:g}", "auto": True}
        if x["id"] == "P1" and target and best is not None and best >= target:
            auto[x["id"]] = {"state": "done", "note": f"best {best:g} ≥ {target}", "auto": True}
    eff = {}
    for x in plan:
        man = b.get("steps", {}).get(x["id"])
        au = auto.get(x["id"])
        if man and (man.get("state") in ("done", "skip") or not au):
            eff[x["id"]] = dict(man)
        else:
            eff[x["id"]] = dict(au or {"state": "todo"})
    for x in plan:
        e = eff[x["id"]]
        if e["state"] == "todo":
            open_deps = [d for d in x["deps"] if eff.get(d, {"state": "done"})["state"] not in ("done", "skip")]
            e["state"] = "blocked" if open_deps else "ready"
            e["waiting_for"] = open_deps
    crit = critical_path(plan)
    steps = [{**x, **eff[x["id"]], "critical": x["id"] in crit} for x in plan]
    total = len(steps)
    done = sum(1 for x in steps if x["state"] in ("done", "skip"))
    per_lane = {}
    for x in steps:
        pl = per_lane.setdefault(x["lane"], {"total": 0, "done": 0})
        pl["total"] += 1
        pl["done"] += x["state"] in ("done", "skip")
    latest = benches[-1]["score"] if benches else None
    return {"steps": steps, "done": done, "total": total, "per_lane": per_lane,
            "score": {"latest": latest, "best": best, "target": target, "runs": benches,
                      "pct": round(100 * best / target) if target and best is not None else None},
            "critical": sorted(crit)}


def roadmap_line(rm):
    if not rm["total"]:
        return ""
    by = lambda st: " ".join(x["id"] for x in rm["steps"] if x["state"] == st)
    ready = by("ready").split()
    s = f"roadmap: done {rm['done']}/{rm['total']}, doing: {by('doing') or '-'}, ready: {' '.join(ready[:8]) or '-'}"
    return s + (f" +{len(ready) - 8}" if len(ready) > 8 else "")


def nudges(b, t):
    """Lanes 2 / 3 / c waiting or idle ≥ NUDGE_SEC (no subagent running) in setup / baseline / loop: the hub should hand
    them a [TASK] from their lane file's 「手が空いたら」 list. [(lane, text)]."""
    if b.get("phase") not in NUDGE_PHASES:
        return []
    out = []
    for k in lane_order(b["lanes"]):
        l = b["lanes"][k]
        if k not in ("2", "3", "c") or l.get("state") not in ("waiting", "idle") or l.get("agents"):
            continue
        if not l.get("since") or t - l["since"] < NUDGE_SEC:
            continue
        what = l.get("note") or l.get("step") or ""
        out.append((k, f"!! レーン {k}: {l['state']} {(t - l['since']) // 60}m" + (f"（{what}）" if what else " ")
                    + "→ 手が空いたら から [TASK]"))
    return out


def render(b, t, color=False):
    def c(key, s):
        return COLORS[key] + s + RESET if color and key in COLORS else s

    bn = b.get("bench") or {}
    lines = [f"bench: {bn.get('state', 'none')} — {bench_text(bn)}" + (f" ({age(bn.get('since'), t)})" if bn.get("since") else ""),
             f"phase: {b['phase']} ({age(b['since'], t)})  now {hm(t)}"]
    for k in lane_order(b["lanes"]):
        l = b["lanes"][k]
        s = f"lane {lane_label(k)} {c(l['state'], l['state'].ljust(13))} {age(l['since'], t):>7}  {l['step']}"
        if l.get("agents"):
            s += f"  🤖{l['agents']}"
        if lane_agent(k) == "codex":
            s += "  [codex]"
        if unread_msgs(b, k):
            s += f"  ✉{len(unread_msgs(b, k))}"
        if l.get("note"):
            s += f"  ({l['note']})"
        lines.append(s.rstrip())
    lines += [c("warn", x) for _, x in nudges(b, t)]
    rl = roadmap_line(roadmap(b))
    if rl:
        lines.append(rl)
    alerts = [f"  {x['id']} {c(x['level'], '[' + x['level'] + ']')} lane {lane_label(x['lane'])} {hm(x['created'])}: {x['msg']}"
              + (f" — impact: {x['impact']}" if x["impact"] else "") for x in b["alerts"] if not x["resolved"]]
    dec = [f"{x['id']} {('PR ' + prs_label(x)) if x.get('pr') else '→'} {decision_text(x)} {hm(x.get('done_at'))}" for x in recent_decisions(b, 3)
           if t - (x.get("done_at") or 0) < 3600]
    if dec:
        lines.append("decided: " + ", ".join(dec))
    todos = [f"  {x['id']} lane {lane_label(x['lane'])} {hm(x['created'])}: " + (f"[PR {prs_label(x)}] " if x.get("pr") else "") + f"{x['msg']}"
             + (f" | 選択: {' / '.join(x['choices'])}（lane:web のボタン）" if x.get("choices") else "")
             + (f" | 選ぶ: {' / '.join(p['label'] for p in x['picks'])}（lane:web で選んで OK）" if x.get("picks") else "")
             + (f" | 入力: {inputs_text(x)}（lane:web で送信）" if x.get("inputs") else "")
             + (f" | cmd: {todo_cmd(x)}" if x.get("cmd") else "") + (f" | why: {x['why']}" if x["why"] else "")
             + (f" | after: {x['after']}" if x["after"] else "") for x in b["todos"] if not x["done"]]
    n_alerts, n_todos = len(alerts), len(todos)
    room = max(2, BOARD_ROWS - len(lines) - 2)
    nt = min(len(todos), room // 2)  # alerts get at least half the room, more when there are few todos

    def fit(items, n):
        if len(items) <= n:
            return items
        n = max(1, n)
        return items[: n - 1] + [f"  … +{len(items) - n + 1} more"]

    alerts = fit(alerts, room - nt)
    todos = fit(todos, room - len(alerts))
    lines.append(c("crit", f"alerts open: {n_alerts}") if n_alerts else "alerts open: 0")
    lines += alerts
    lines.append(f"human todos open: {n_todos}")
    lines += todos
    return "\n".join(lines[:BOARD_ROWS])


def read_board():
    return load(os.path.join(lanes_dir(), "board.json"))


def resolver_hint(al):
    return f"直ったら: task lane:resolve ID={al['id']}（{lane_label(al['lane'])} が resolve）"


def lane_tasks(b, lane):
    """The lane's doing steps with their checklists (lane:items), for the lane card: exact n/m, no guesses."""
    plan = {x["id"]: x for x in load_plan()}
    out = []
    for sid, cur in b.get("steps", {}).items():
        p = plan.get(sid)
        if not p or p["lane"] != lane or cur.get("state") != "doing":
            continue
        its = cur.get("items") or []
        out.append({"id": sid, "title": p["title"], "items": its, "total": len(its),
                    "done_n": sum(1 for x in its if x["state"] in ("done", "skip"))})
    return sorted(out, key=lambda x: x["id"])


def board_json(b, t):
    """What lane:web serves: the board plus display fields, so the page does no logic of its own."""
    lanes = [{"id": k, "label": lane_label(k), "icon": STATE_ICONS.get(l["state"], "•"), **l,
              "age_min": max(0, (t - l["since"]) // 60) if l.get("since") else 0,
              "tasks": lane_tasks(b, k), "agent": lane_agent(k), "unread": len(unread_msgs(b, k)),
              "msgs": [{"id": m["id"], "from": m["from"], "to": m["to"], "msg": m["msg"], "at": hm(m.get("t")),
                        "read": bool(m.get("read_at")), "read_at": hm(m.get("read_at")) if m.get("read_at") else ""}
                       for m in [x for x in b.get("msgs", []) if k in (x["to"], x["from"])][-CARD_MSGS:]],
              "stuck": bool(l["state"] == "waiting-human" and l.get("since") and t - l["since"] >= 300)}
             for k, l in ((k, b["lanes"][k]) for k in lane_order(b["lanes"]))]
    todos = [{**{k: v for k, v in x.items() if k not in ("values", "env_value")}, "cmd": todo_cmd(x), "label": lane_label(x["lane"]),
              "copy": copy_text(x), "age_min": max(0, (t - x["created"]) // 60), "pr_url": pr_url(x.get("pr")),
              "pr_list": [{"n": p, "url": pr_url(p), "title": (x.get("pr_titles") or {}).get(str(p), "")}
                          for p in todo_prs(x)]}
             for x in open_todos(b)]
    decisions = [{"id": x["id"], "pr": x.get("pr"), "pr_url": pr_url(x.get("pr")), "decision": decision_text(x),
                  "prs": todo_prs(x), "ok_prs": x.get("ok_prs"), "held_prs": x.get("held_prs"),
                  "by": x.get("decided_by", ""), "at": hm(x.get("done_at")), "msg": x["msg"]} for x in recent_decisions(b)]
    bn = dict(b.get("bench") or {"state": "none"})
    bn.update({"text": bench_text(bn), "age_min": max(0, (t - bn["since"]) // 60) if bn.get("since") else 0,
               "done_hm": hm(bn["done_at"]) if bn.get("done_at") else ""})
    alerts = [{**x, "label": lane_label(x["lane"]), "hint": resolver_hint(x), "age_min": max(0, (t - x["created"]) // 60)}
              for x in open_alerts(b)]
    return {"phase": b["phase"], "phase_min": max(0, (t - b["since"]) // 60) if b["since"] else 0, "now": hm(t),
            "lanes": lanes, "todos": todos, "alerts": alerts, "decisions": decisions, "bench": bn,
            "nudges": [{"lane": k, "text": x} for k, x in nudges(b, t)]}


# ---------- story: 「📜 経緯」 on lane:web ----------
_STORY_CACHE = {}


def read_events(path=None):
    """events.jsonl as a list (file order), re-read only when its size / mtime changed (lane:web polls)."""
    path = path or events_path()
    try:
        st = os.stat(path)
    except OSError:
        return []
    key = (st.st_size, st.st_mtime_ns)
    hit = _STORY_CACHE.get(path)
    if hit and hit[0] == key:
        return hit[1]
    out = []
    with open(path, errors="replace") as f:
        for line in f:
            try:
                e = json.loads(line)
            except ValueError:
                continue
            if isinstance(e, dict) and isinstance(e.get("t"), (int, float)) and e.get("kind"):
                out.append(e)
    _STORY_CACHE[path] = (key, out)
    return out


def decision_answer(e):
    """A todo_done event → the human's answer as text (「merge OK #7」, 「OK U1 U2」, the clicked choice), or None when
    it is not a decision: a plain ✅, an input answer (inputs are never echoed), start / otel / bench todos."""
    d = e.get("decision") or "done"
    if d == "done" or e.get("by") != "web" or e.get("tkind") in ("start", "otel", "bench"):
        return None
    if e.get("tkind") == "review" or e.get("ok_prs") or e.get("held_prs"):
        return ("merge OK " + " ".join("#" + x for x in e.get("ok_prs", "").split()) if d == "ok" else "要修正") + \
            (" / 保留 " + " ".join("#" + x for x in e["held_prs"].split()) if e.get("held_prs") and d == "ok" else "")
    if e.get("picked") or e.get("dropped"):
        return ("OK " + e["picked"]) if e.get("picked") else "全部やめる"
    return d


def story_item(e, msgs):
    """One event → a story row {t, lane, kind, text, ref, auto} or None. Notes come from lane:note; the rest is picked
    up without any session call: the human's decisions, the bench end, run scores, alerts, a failed bench:done.
    Board messages (event "msg", lane:msg / Codex) are never story rows: they are signals, a conclusion is a lane:note."""
    k = e["kind"]
    if k == "note":
        body = e.get("body") if isinstance(e.get("body"), list) else []
        return {"lane": e.get("lane", ""), "kind": e.get("nkind", ""), "text": e.get("msg", ""), "ref": e.get("ref", ""),
                "body": [str(x) for x in body][:MAX_BODY_LINES], "auto": False}
    if k == "todo_done":
        ans = decision_answer(e)
        if ans is None:
            return None
        what = msgs.get(e.get("id"), "")
        return {"lane": e.get("lane", ""), "kind": "decision", "text": clean(f"{ans} — {what}" if what else ans, MAX_NOTE),
                "ref": e.get("id", ""), "auto": True}
    if k == "bench_done":
        sc = f" score {e['score']}" if e.get("score") else ""
        return {"lane": "1", "kind": "score", "text": f"🏁 ベンチ終了{sc}" + (f"（main {e['sha']}）" if e.get("sha") else ""),
                "ref": e.get("id", ""), "auto": True}
    if k == "run_scored":
        return {"lane": "1", "kind": "score", "text": f"run score {e.get('score')}", "ref": e.get("ts", ""), "auto": True}
    if k == "alert":
        return {"lane": e.get("lane", ""), "kind": "incident", "text": f"[{e.get('level', 'warn')}] {e.get('msg', '')}",
                "ref": e.get("id", ""), "auto": True}
    if k == "bench_done_task" and not e.get("ok"):
        return {"lane": "1", "kind": "incident", "text": "task bench:done 失敗" + (f": {e['err']}" if e.get("err") else ""),
                "ref": "", "auto": True}
    return None


def note_body(v):
    """BODY="a\\nb": lines split on real newlines and on the two characters backslash-n (what a Taskfile var carries);
    at most MAX_BODY_LINES lines of MAX_BODY_LINE chars, empty lines dropped."""
    lines = [clean(x, MAX_BODY_LINE) for x in re.split(r"\\n|\r?\n", v or "")]
    lines = [x for x in lines if x]
    if len(lines) > MAX_BODY_LINES:
        raise LaneError(f"body: {MAX_BODY_LINES} 行まで（結論だけ。詳細は docs/ に書いて REF で指す）")
    return lines


def is_bench_mark(e):
    return e["kind"] in ("run_scored", "bench_done")


def story(evs=None, n=STORY_MAX):
    """「📜 経緯」 grouped by bench: newest group first; each group = the rows logged after the previous bench end up to
    its own end (run_scored / 「終わった」; a run_scored soon after an unscored 「終わった」 joins that header), newest row
    first; the last group (current=True) is what happened since the last bench. Rows carry key (stable across polls:
    the event's place in events.jsonl) for the page's expand state. At most n rows (the oldest go first)."""
    evs = read_events() if evs is None else evs
    msgs, seen, groups, cur = {}, set(), [], []
    for i, e in enumerate(evs):
        if e["kind"] == "todo" and e.get("id"):
            msgs[e["id"]] = e.get("msg", "")
        if e["kind"] == "run_scored":  # runs:wait may log the same run twice (two waits)
            if e.get("ts") in seen:
                continue
            seen.add(e.get("ts"))
        if is_bench_mark(e):
            last = groups[-1] if groups else None
            if (e["kind"] == "run_scored" and last and last["src"] == "bench_done" and not last.get("ts")
                    and e["t"] - last["t"] < BENCH_MERGE_SEC):
                last.update({"ts": e.get("ts", ""), "score": e.get("score") or last.get("score", "")})
                last["items"] += cur
            else:
                groups.append({"key": f"g{e['t']}-{i}", "t": e["t"], "hm": hm(e["t"]), "src": e["kind"],
                               "ts": e.get("ts", "") if e["kind"] == "run_scored" else "", "score": e.get("score", ""),
                               "prs": e.get("prs", "") if e["kind"] == "bench_done" else "", "items": cur})
            cur = []
            continue
        it = story_item(e, msgs)
        if it:
            it.update({"t": e["t"], "hm": hm(e["t"]), "icon": NOTE_KINDS.get(it["kind"], "•"), "key": f"r{e['t']}-{i}"})
            cur.append(it)
    for k, g in enumerate(groups, 1):
        g["bench"] = k
    groups.append({"key": "now", "t": None, "hm": "", "src": "", "ts": "", "score": "", "prs": "", "items": cur, "current": True,
                   "bench": len(groups) + 1})
    left = n  # keep the newest n rows
    for g in reversed(groups):
        g["items"] = g["items"][max(0, len(g["items"]) - left):][::-1] if left > 0 else []
        left -= len(g["items"])
    return groups[::-1]


def story_rows(groups):
    return [x for g in groups for x in g["items"]]


BRIEF_SCORE_RE = re.compile(r"^(スコア|減点|猶予|score)", re.I)
BRIEF_FORBID_RE = re.compile(r"^禁止")
_DOCS_CACHE = {}
_CC_CACHE = {}
_BRIEF_CACHE = {}


def brief_docs(top=None):
    """docs/kickoff.md (repo toplevel, as prize_target reads alp.yml) → {"score": [≤ 3 lines], "forbid": [≤ 2 lines]}
    for 「📌 いまの要点」 when the hub's kickoff note has no スコア / 禁止 body lines: the `| スコア |` / `| 禁止・必須 |`
    table row's cell, else the first bullets of the `## スコア` / `## 禁止…` section. Re-read only when the file changed."""
    path = os.path.join(top or repo_toplevel() or ".", "docs", "kickoff.md")
    try:
        st = os.stat(path)
    except OSError:
        return {"score": [], "forbid": []}
    key = (st.st_size, st.st_mtime_ns)
    hit = _DOCS_CACHE.get(path)
    if hit and hit[0] == key:
        return hit[1]
    with open(path, errors="replace") as f:
        text = f.read()

    def plain(v):
        return clean(re.sub(r"\*\*|`", "", v).strip(), MAX_BODY_LINE)

    def row(name):
        m = re.search(r"^\|\s*" + name + r"[^|]*\|([^|\n]+)\|", text, re.M)
        if not m or not m.group(1).strip() or set(m.group(1).strip()) <= set("-: "):
            return []
        return [plain(x) for x in re.split(r"<br\s*/?>|。(?=.)", m.group(1)) if plain(x)]

    def section(name, n):
        m = re.search(r"^##+\s*" + name + r"[^\n]*\n(.*?)(?=^##|\Z)", text, re.M | re.S)
        if not m:
            return []
        return [plain(x) for x in re.findall(r"^\s*[-*]\s+(.+)$", m.group(1), re.M) if plain(x)][:n]

    out = {"score": (row("スコア") or section("スコア", 3))[:3], "forbid": (row("禁止") or section("禁止", 2))[:2]}
    _DOCS_CACHE[path] = (key, out)
    return out


def cc_effects(path=None):
    """[DONE] … effect lines the Claude lanes sent with SendMessage (cc-events.jsonl next to events.jsonl: the hook keeps
    the first 60 chars) → [{t, lane, text}], re-read only when the file changed. Codex lanes' lane:msg are events."""
    path = path or os.path.join(os.path.dirname(events_path()), "cc-events.jsonl")
    try:
        st = os.stat(path)
    except OSError:
        return []
    key = (st.st_size, st.st_mtime_ns)
    hit = _CC_CACHE.get(path)
    if hit and hit[0] == key:
        return hit[1]
    out = []
    with open(path, errors="replace") as f:
        for line in f:
            if "SendMessage" not in line or "effect" not in line:
                continue
            try:
                h = json.loads(line)
            except ValueError:
                continue
            head = str(h.get("head") or "")
            if h.get("event") == "PreToolUse" and re.match(r"\[DONE\]\s+\S+\s+effect\b", head) and h.get("ts"):
                out.append({"t": int(h["ts"]) // 1000, "lane": str(h.get("lane") or ""), "text": head})
    _CC_CACHE[path] = (key, out)
    return out


def brief_scan(evs):
    """One pass over events.jsonl (not the story rows, which drop the morning on a long day) for the 📌 slots:
    the kickoff app note (REF has kickoff, else the oldest app note), the app-map note, the latest bottleneck /
    decision note, the latest human decision. Cached per events list (read_events returns the same list until the
    file changes)."""
    hit = _BRIEF_CACHE.get("scan")
    if hit and hit[0] is evs and hit[1] == len(evs):
        return hit[2]
    msgs, out = {}, {"kickoff": None, "app_first": None, "app_map": None, "bottleneck": None, "decision": None,
                     "human": None}
    for i, e in enumerate(evs):
        k = e["kind"]
        if k == "todo" and e.get("id"):
            msgs[e["id"]] = e.get("msg", "")
        elif k == "note":
            it = story_item(e, msgs)
            it.update({"t": e["t"], "hm": hm(e["t"]), "key": f"r{e['t']}-{i}"})
            nk, ref = it["kind"], it["ref"].lower()
            if nk == "app":
                if "kickoff" in ref and not out["kickoff"]:
                    out["kickoff"] = it
                if "app-map" in ref:
                    out["app_map"] = it
                out["app_first"] = out["app_first"] or it
            elif nk in ("bottleneck", "decision"):
                out[nk] = it
        elif k == "todo_done":
            ans = decision_answer(e)
            if ans is not None:
                out["human"] = {"t": e["t"], "hm": hm(e["t"]), "id": e.get("id", ""), "lane": e.get("lane", ""),
                                "text": clean(ans, MAX_NOTE), "what": clean(msgs.get(e.get("id"), ""), MAX_NOTE)}
    _BRIEF_CACHE["scan"] = (evs, len(evs), out)
    return out


def score_of(g):
    try:
        return float(g.get("score"))
    except (TypeError, ValueError):
        return None


def highlights(b, groups, runs=None, target=None, evs=None, docs=None):
    """「📌 いまの要点」: six fixed slots, each present even when empty (the page shows a placeholder).
    app: the hub's kickoff app note (never replaced by a later app note) + ≤ 2 body lines that are not score / 禁止,
    + the app-map note's headline. calc: the kickoff note's スコア / 減点 / 猶予 lines (≤ 3), else docs/kickoff.md,
    + target. forbid: its 禁止 lines (≤ 2), else docs/kickoff.md. bottleneck: the latest bottleneck note (stale when
    older than the latest scored bench), else the latest run's verdict. score: latest, best, Δ vs the previous bench,
    % of prize_target. decisions: open question / review / pick todos + the latest decision note + the latest human
    decision. fix (not drawn): the last 3 fix notes."""
    evs = read_events() if evs is None else evs
    sc_ = brief_scan(evs)
    docs = brief_docs() if docs is None else docs
    kick = sc_["kickoff"] or sc_["app_first"]
    app = None
    if kick:
        body = [x for x in kick["body"] if not BRIEF_SCORE_RE.match(x) and not BRIEF_FORBID_RE.match(x)][:2]
        mp = sc_["app_map"] if sc_["app_map"] and sc_["app_map"]["key"] != kick["key"] else None
        app = {**kick, "body": body, "map": mp and mp["text"], "map_ref": mp and mp["ref"]}
    kbody = kick["body"] if kick and "kickoff" in kick["ref"].lower() else []
    calc = [x for x in kbody if BRIEF_SCORE_RE.match(x)][:3]
    forbid = [x for x in kbody if BRIEF_FORBID_RE.match(x)][:2]
    calc_src = "kickoff note" if calc else ("docs/kickoff.md" if docs.get("score") else "")
    forbid_src = "kickoff note" if forbid else ("docs/kickoff.md" if docs.get("forbid") else "")
    calc, forbid = calc or docs.get("score") or [], forbid or docs.get("forbid") or []

    rows = story_rows(groups)  # newest first
    benches = [g for g in groups if not g.get("current") and score_of(g) is not None]
    scores = [score_of(g) for g in benches]
    for r in bench_runs(runs):  # the ops panel's scored runs (also those runs:wait never logged)
        scores.append(r["score"])
    latest = benches[0] if benches else None
    prev = benches[1] if len(benches) > 1 else None
    verdict = ""
    if latest and latest.get("ts"):
        verdict = next((str(r.get("verdict") or "") for r in runs or [] if r.get("ts") == latest["ts"]), "")
    best = max(scores) if scores else None
    delta = None
    if latest and prev and score_of(prev):
        delta = round(100 * (score_of(latest) - score_of(prev)) / score_of(prev), 1)
    bn = sc_["bottleneck"]
    if bn:
        bn = {**bn, "line": bn["body"][0] if bn["body"] else "",
              "stale": f"ベンチ {latest['bench']} より前" if latest and bn["t"] < latest["t"] else ""}
    open_dec = [{"id": x["id"], "kind": todo_kind(x), "msg": x["msg"], "lane": x["lane"],
                 "prs": todo_prs(x)} for x in open_todos(b) if todo_kind(x) in ("question", "review", "pick")]
    return {"app": app, "calc": {"lines": calc, "src": calc_src, "target": target},
            "forbid": {"lines": forbid, "src": forbid_src},
            "bottleneck": bn, "verdict": clean(verdict, 200),
            "fix": [x for x in rows if not x["auto"] and x["kind"] == "fix"][:3],
            "decisions": open_dec, "decision_note": sc_["decision"], "human": sc_["human"],
            "score": {"latest": latest and latest.get("score"), "bench": latest and latest.get("bench"),
                      "hm": latest and latest.get("hm"), "ts": latest and latest.get("ts"), "verdict": clean(verdict, 200),
                      "prev": prev and prev.get("score"), "delta_pct": delta, "best": best, "target": target,
                      "pct": round(100 * best / target) if target and best is not None else None}}


def pr_nums(v):
    return list(dict.fromkeys(re.findall(r"#?(\d+)", str(v or ""))))


def story_cards(groups=None, evs=None, cc=None):
    """「📜 経緯」 as one card per bench, newest first, built from events only: 載った PR (the bench's 「終わった」 prs,
    else the code ships before it), then what came after it up to the next bench — 律速 (the first bottleneck note),
    効果 ([DONE] … effect lines: lane:msg events and the Claude lanes' SendMessage in cc-events.jsonl; else the fix
    notes of its PRs), 決定 (the human's decisions, non-review first), crit alerts. Before the first bench: 「準備」
    with the kickoff app note and the decisions. ≤ 5 lines per card."""
    evs = read_events() if evs is None else evs
    groups = story(evs, n=0) if groups is None else groups
    cc = cc_effects() if cc is None else cc
    bs = sorted((g for g in groups if not g.get("current")), key=lambda g: g["t"])
    edges = [g["t"] for g in bs]

    def slot(t):  # 0 = before the first bench end (準備), k = after bench k's end
        return sum(1 for x in edges if t > x)
    n = len(bs) + 1
    ships, docs_ships, bnote, effects, decs, crit, fixes, kick = ([[] for _ in range(n)] for _ in range(8))
    msgs = {}
    for i, e in enumerate(evs):
        k, t = e["kind"], e["t"]
        if k == "todo" and e.get("id"):
            msgs[e["id"]] = e.get("msg", "")
        elif k == "ship.end" and e.get("ok"):
            # a ship is part of the next bench: slot(t) is the bench it lands in (index of the next bench end)
            (docs_ships if e.get("mode") == "docs-only" else ships)[min(slot(t), n - 1)] += pr_nums(e.get("merged"))
        elif k == "note":
            nk, ref = e.get("nkind"), str(e.get("ref") or "")
            if nk == "bottleneck":
                bnote[slot(t)].append(e)
            elif nk == "fix":
                fixes[0].append(e)
            elif nk == "app":
                kick[0].append(e)
        elif k == "msg" and e.get("tag") == "DONE" and re.match(r"\[DONE\]\s+\S+\s+effect\b", e.get("msg", "")):
            effects[slot(t)].append({"t": t, "text": e.get("msg", "")})
        elif k == "todo_done":
            ans = decision_answer(e)
            if ans is not None:
                decs[slot(t)].append({"t": t, "id": e.get("id", ""), "ans": ans, "review": e.get("tkind") == "review",
                                      "what": msgs.get(e.get("id"), "")})
        elif k == "alert" and e.get("level") == "crit":
            crit[slot(t)].append(e)
    for x in cc:
        effects[slot(x["t"])].append(x)
    for j, g in enumerate(bs):  # an effect line naming a run ts goes to that bench
        for s in range(n):
            for x in list(effects[s]):
                if g.get("ts") and g["ts"] in x["text"] and s != j + 1:
                    effects[s].remove(x)
                    effects[j + 1].append(x)

    def dec_line(ds):
        pick = [d for d in ds if not d["review"]] or ds
        return " / ".join(clean(f"{d['id']} {d['ans']}", 60) for d in pick[-2:])
    cards = []
    prev_score = None
    for j, g in enumerate(bs):
        s = j + 1
        prs = pr_nums(g.get("prs")) or ships[j]
        lines = []
        if prs or docs_ships[j]:
            lines.append({"k": "pr", "icon": "🚢", "text": " ".join("#" + x for x in prs) +
                          (f"（docs {' '.join('#' + x for x in docs_ships[j])}）" if docs_ships[j] else "")})
        if bnote[s]:
            lines.append({"k": "bottleneck", "icon": "🔥", "text": bnote[s][0].get("msg", "")})
        if effects[s]:
            lines.append({"k": "effect", "icon": "🎯", "text": " / ".join(clean(x["text"], 80) for x in effects[s][-2:])})
        else:
            fx = [f for f in fixes[0] if set(pr_nums(f.get("ref"))) & set(prs)]
            if fx:
                lines.append({"k": "fix", "icon": "🔧", "text": " / ".join(clean(f.get("msg", ""), 60) for f in fx[-2:])})
        if decs[s]:
            lines.append({"k": "decision", "icon": "🙋", "text": dec_line(decs[s])})
        if crit[s]:
            lines.append({"k": "incident", "icon": "🚨", "text": " / ".join(clean(a.get("msg", ""), 80) for a in crit[s][-2:])})
        sc = score_of(g)
        delta = round(100 * (sc - prev_score) / prev_score, 1) if sc is not None and prev_score else None
        if sc is not None:
            prev_score = sc
        cards.append({"key": "c" + g["key"], "bench": g["bench"], "t": g["t"], "hm": g["hm"], "score": g.get("score", ""),
                      "delta_pct": delta, "ts": g.get("ts", ""), "lines": lines[:5]})
    nxt = {"k": "next", "icon": "⏭", "text": " ".join("#" + x for x in ships[len(bs)]) + "（merge 済み、次のベンチに載る）"}
    if ships[len(bs)] and cards:  # merged after the last bench end: they ride on the next one
        cards[-1]["lines"] = (cards[-1]["lines"] + [nxt])[:5]
    lines = []
    if kick[0]:  # the kickoff app note, else the oldest app note
        app = next((x for x in kick[0] if "kickoff" in str(x.get("ref") or "").lower()), kick[0][0])
        lines.append({"k": "app", "icon": "🧭", "text": app.get("msg", "")})
    if decs[0]:
        lines.append({"k": "decision", "icon": "🙋", "text": dec_line(decs[0])})
    if crit[0]:
        lines.append({"k": "incident", "icon": "🚨", "text": " / ".join(clean(a.get("msg", ""), 80) for a in crit[0][-2:])})
    if ships[0] and not bs:
        lines.append(nxt)
    cards.insert(0, {"key": "cprep", "bench": 0, "t": None, "hm": "", "score": "", "delta_pct": None, "ts": "",
                     "prep": True, "lines": lines[:5]})
    return cards[::-1]


# ---------- watch: full-screen board for the human ----------

def cell_width(ch):
    import unicodedata
    if unicodedata.combining(ch) or ch in "\u200d\ufe0f":
        return 0
    if unicodedata.east_asian_width(ch) in ("W", "F") or ord(ch) >= 0x1F300:
        return 2
    return 1


def fit_width(text, width):
    if sum(cell_width(c) for c in text) <= width:
        return text
    out, w = [], 0
    for ch in text:
        cw = cell_width(ch)
        if w + cw > width - 1:
            break
        out.append(ch)
        w += cw
    return "".join(out) + "…"


STYLE = {"title": "\033[1;97;45m", "phase": "\033[1;30;46m", "dim": "\033[90m", "working": "\033[32m",
         "waiting-human": "\033[1;33m", "stuck": "\033[1;30;43m", "waiting": "\033[36m", "alert": "\033[1;97;41m",
         "idle": "\033[37m", "crit": "\033[1;31m", "warn": "\033[33m", "head": "\033[1;97m", "todo": "\033[1;93m",
         "ok": "\033[1;32m", "": ""}


def watch_frame(b, t, width=100, flash=""):
    """List of (style, text) lines. Plain text so it can be cut to the terminal width before colouring."""
    d = board_json(b, t)
    rows = [("title", f" 🏁 ISUCON lanes ".ljust(18)), ("phase", f" {d['phase'].upper()} {d['phase_min']}m "),
            ("dim", f"  🕒 {datetime.fromtimestamp(t).strftime('%H:%M:%S')}")]
    lines = [rows]
    bn = d["bench"]
    lines.append([({"ready": "ok", "running": "waiting", "collect": "todo"}.get(bn["state"], "dim"),
                   f" 🏁 BENCH {bn['state'].upper()}  {bn['text']}")])
    lines.append([("dim", "─" * min(width, 80))])
    if not d["lanes"]:
        lines.append([("dim", "  (まだどのレーンも status を出していない)")])
    for l in d["lanes"]:
        st = "stuck" if l["stuck"] else l["state"]
        name = l["label"]
        pad = 11 - sum(cell_width(c) for c in name)
        seg = [(st, f" {l['icon']} {name}{' ' * max(1, pad)}{l['state']:<13} {l['age_min']:>3}m  "), ("", l["step"])]
        if l.get("agents"):
            seg.append(("", f"  🤖×{l['agents']}"))
        for tk in l.get("tasks") or []:
            if tk["total"]:
                seg.append(("dim", f"  [{tk['id']} {tk['done_n']}/{tk['total']}]"))
        if l.get("note"):
            seg.append(("dim", f"  · {l['note']}"))
        if l["stuck"]:
            seg.append(("stuck", "  ⚠ 5 分超: そのタブで許可を"))
        lines.append(seg)
    for x in d.get("nudges") or []:
        lines.append([("warn", f" {x['text']}")])
    lines.append([("", "")])
    lines.append([("head", f" 🙋 あなたへの todo ({len(d['todos'])})")])
    if not d["todos"]:
        lines.append([("dim", "   なし 🎉")])
    for i, x in enumerate(d["todos"][:9], 1):
        lines.append([("todo", f"  [{i}] "), ("dim", f"{x['id']} {x['label']} {x['age_min']}m  ")]
                     + ([("head", f"PR {prs_label(x)} ")] if x.get("pr") else []) + [("", x["msg"])])
        for pl in x.get("pr_list") or []:
            if pl["url"] or pl["title"]:
                lines.append([("dim", f"       🔗 #{pl['n']} {pl['title']}  {pl['url']}")])
        if x.get("pr"):
            lines.append([("dim", "       （merge OK / PR ごとの除外 / 要修正 は lane:web のボタン）")])
        if x.get("choices"):
            lines.append([("dim", f"       選択: {' / '.join(x['choices'])}  （lane:web のボタンで答える）")])
        if x.get("picks"):
            lines.append([("dim", f"       選ぶ: {' / '.join(p['label'] for p in x['picks'])}  （lane:web で選んで OK）")])
        if x.get("inputs"):
            lines.append([("dim", f"       入力: {inputs_text(x)}  （lane:web で入力して送信）")])
        if x.get("cmd"):
            lines.append([("dim", f"       $ {x['cmd']}")])
        if x.get("after"):
            lines.append([("dim", f"       → 終わったら: {x['after']}")])
    if len(d["todos"]) > 9:
        lines.append([("dim", f"   … +{len(d['todos']) - 9} 件（task lane:board）")])
    if d["decisions"]:
        lines.append([("dim", "   決定: " + "  ".join(f"{' '.join('#%d' % p for p in x['prs']) if x.get('pr') else x['id']} {x['decision']} {x['at']}" for x in d["decisions"][:4]))])
    lines.append([("", "")])
    lines.append([("head", f" 🚨 alert ({len(d['alerts'])})")])
    if not d["alerts"]:
        lines.append([("dim", "   なし ✨")])
    for x in d["alerts"]:
        lines.append([(x["level"], f"  {x['level'].upper():<4} "), ("dim", f"{x['id']} {x['label']} {x['age_min']}m  "), ("", x["msg"])])
        lines.append([("dim", f"       {x['hint']}")])
    lines.append([("", "")])
    lines.append([("dim", " [1-9] CMD をコピー   d+[1-9] 完了   q 終了"), ("ok", f"   {flash}" if flash else "")])
    return lines


def paint(lines, width, color=True):
    out = []
    for segs in lines:
        used, buf = 0, []
        for style, text in segs:
            room = width - used
            if room <= 0:
                break
            txt = fit_width(text, room)
            used += sum(cell_width(c) for c in txt)
            buf.append(STYLE.get(style, "") + txt + RESET if color and STYLE.get(style) else txt)
        out.append("".join(buf))
    return out


def watch(secs):
    try:
        mutate("init", {})  # first todos of the day on a new board (no-op once seeded)
    except LaneError:
        pass
    import select
    import shutil
    tty = sys.stdin.isatty() and sys.stdout.isatty()
    if not tty:  # piped: print the plain board every N s
        try:
            while True:
                print(render(read_board(), now()) + "\n")
                sys.stdout.flush()
                time.sleep(secs)
        except (KeyboardInterrupt, BrokenPipeError):
            return
    import termios
    import tty as ttymod
    fd = sys.stdin.fileno()
    old = termios.tcgetattr(fd)
    flash, flash_until, pending_d, last = "", 0.0, False, 0.0
    sys.stdout.write("\033[?1049h\033[?25l")
    try:
        ttymod.setcbreak(fd)
        while True:
            t0 = time.time()
            if t0 - last >= secs or last == 0:
                cols, rows = shutil.get_terminal_size((100, 40))
                b = read_board()
                fl = flash if t0 < flash_until else ("d → 番号で完了" if pending_d else "")
                frame = paint(watch_frame(b, now(), cols, fl), cols)[: max(1, rows - 1)]
                sys.stdout.write("\033[H" + "".join(line + "\033[K\n" for line in frame) + "\033[J")
                sys.stdout.flush()
                last = t0
            r, _, _ = select.select([sys.stdin], [], [], 0.25)
            if not r:
                continue
            ch = os.read(fd, 1).decode(errors="ignore")
            if ch in ("q", "\x03"):
                break
            if ch == "d":
                pending_d, last = True, 0
                continue
            if ch.isdigit() and ch != "0":
                todos = open_todos(read_board())
                k = int(ch) - 1
                if k < len(todos):
                    td = todos[k]
                    if pending_d and td.get("pr"):
                        flash = f"PR {prs_label(td)} は lane:web の「merge OK」「要修正」で"
                    elif pending_d and (td.get("choices") or td.get("picks")):
                        flash = f"{td['id']} は lane:web の選択肢のボタンで"
                    elif pending_d and td.get("inputs"):
                        flash = f"{td['id']} は lane:web で入力して送信"
                    elif pending_d:
                        mutate("done", {"id": td["id"]})
                        flash = f"✅ done {td['id']}"
                    else:
                        tool = to_clipboard(copy_text(td))
                        flash = f"📋 copied {td['id']}" if tool else f"📋 クリップボード無し: {copy_text(td)}"
                    flash_until = time.time() + 4
            pending_d, last = False, 0
    except KeyboardInterrupt:
        pass
    finally:
        termios.tcsetattr(fd, termios.TCSADRAIN, old)
        sys.stdout.write("\033[?25h\033[?1049l")
        sys.stdout.flush()


def parse_kv(args):
    a = {}
    for x in args:
        if "=" not in x:
            raise LaneError(f"expected key=value, got {x!r}")
        k, v = x.split("=", 1)
        a[k.strip().lower()] = v
    return a


def wait_todo(a, poll=3.0):
    """Block until todo <id> is done (the human pressed ✅ on lane:web / d+n on lane:watch, or ran task lane:done).
    A session runs it with run_in_background right after creating a todo it waits on: the exit wakes the session."""
    tid = (a.get("id") or "").strip()
    timeout = float(a.get("timeout") or 3600)
    deadline = time.time() + timeout
    while True:
        td = next((x for x in read_board()["todos"] if x["id"] == tid), None)
        if td is None:
            raise LaneError("no such todo id")
        if td.get("done"):
            print(f"✅ {tid} done: {td.get('msg', '')}")
            if td.get("inputs"):
                if not td.get("answered"):
                    print("input=none")  # closed by lane:done / d+n without answers: ask again or read it elsewhere
                for i, v in enumerate(td.get("values") or [], 1):
                    print(f"input.{i}={v}")
                key = td.get("envkey")
                if key and td.get("answered"):
                    print(f"env.{key}=" + (td.get("env_value", "") if key in ENV_SHOWN else "set"))
            if td.get("picks") and td.get("decided_by") == "web":  # a picks todo decided on lane:web
                print("picked=" + " ".join(td.get("picked") or []))
                print("dropped=" + " ".join(td.get("dropped") or []))
            if td.get("pr") and td.get("decision") in DECISIONS:  # a review todo decided on lane:web
                print("ok_prs=" + " ".join(map(str, td.get("ok_prs", todo_prs(td) if td["decision"] == "ok" else []))))
                print("held_prs=" + " ".join(map(str, td.get("held_prs", [] if td["decision"] == "ok" else todo_prs(td)))))
            if td.get("done_score"):  # a bench todo ended with 「🏁 終わった」 and a typed score
                print(f"score={td['done_score']}")
            print(f"decision={td.get('decision') or 'done'}")
            return 0
        if time.time() > deadline:
            print(f"⏳ {tid} still open after {int(timeout)} s: {td.get('msg', '')}")
            return 2
        time.sleep(poll)


def gh_pr_titles(prs):
    """{"7": "title"} for the review todo's PRs from one `gh pr list` (open PRs); {} when gh is missing / slow / off."""
    if not prs or os.environ.get("LANES_NO_GH") == "1":
        return {}
    try:
        out = subprocess.run(["gh", "pr", "list", "--state", "open", "--limit", "200", "--json", "number,title"],
                             capture_output=True, text=True, timeout=15)
        rows = json.loads(out.stdout or "[]") if out.returncode == 0 else []
    except (FileNotFoundError, subprocess.TimeoutExpired, ValueError):
        return {}
    want = set(prs)
    return {str(r["number"]): str(r.get("title") or "") for r in rows if isinstance(r, dict) and r.get("number") in want}


def main(argv):
    if not argv:
        print(__doc__.strip(), file=sys.stderr)
        return 2
    op, rest = argv[0], argv[1:]
    try:
        if op == "board":
            print(render(read_board(), now(), color="--color" in rest))
        elif op == "watch":
            watch(float(rest[0]) if rest else 2)
        elif op == "web":
            import lanes_web
            lanes_web.serve(int(rest[0]) if rest else 8765)
        elif op == "wait":
            return wait_todo(parse_kv(rest))
        elif op == "wait-msg":
            return wait_msg(parse_kv(rest))
        elif op == "agent":  # which lanes run on Codex (sessions.json): the hub reads it at startup
            lane = (parse_kv(rest).get("lane") or "").strip().lower()
            print(lane_agent(lane) if lane else agents_text())
        elif op == "handoff":
            import lanehandoff
            a = parse_kv(rest)
            print(lanehandoff.main(a))
        elif op == "bench" and not (parse_kv(rest).get("state") or "").strip() and not any(
                (parse_kv(rest).get(k) or "").strip() for k in ("sha", "prs", "next")):
            print(read_board()["bench"].get("state") or "none")
        elif op == "timeline":
            import timeline
            print(timeline.main(parse_kv(rest)))
        elif op == "dir":  # the board directory (otel.sh writes the collector's files under it)
            print(lanes_dir())
        elif op == "event":
            a = parse_kv(rest)
            kind = a.pop("kind", "").strip()
            if not kind:
                raise LaneError("kind= required")
            record_event(kind, **a)
            print("ok")
        elif op == "prok":  # ship.sh: "盤 hh:mm" when PR <pr> got merge OK on lane:web, else empty
            ps = parse_prs(parse_kv(rest).get("pr"))
            print(board_ok(ps[0]) if ps else "")
        elif op == "todo" and (parse_kv(rest).get("pr") or "").strip():
            a = parse_kv(rest)
            a["titles"] = gh_pr_titles(parse_prs(a.get("pr")))  # before the lock: no network call while holding it
            print(mutate(op, a) or "ok")
        elif op == "copy":
            tid = parse_kv(rest).get("id", "").strip()
            td = next((x for x in read_board()["todos"] if x["id"] == tid), None)
            if td is None:
                raise LaneError("no such todo id")
            text = copy_text(td)
            tool = to_clipboard(text)
            print(f"📋 copied {tid} ({tool}): {text}" if tool else f"(no clipboard tool) {text}")
        else:
            new_id = mutate(op, parse_kv(rest))
            print(new_id or "ok")
    except LaneError as e:
        print(f"!! lane: {e}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
