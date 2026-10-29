#!/usr/bin/env python3
"""Practice log: one machine-readable record per practice (practice-log/SCHEMA.md) and an HTML report of the trend.

  practice.py record [slug=…] [notes_changed=…] [notes_hypothesis=…] [notes_obs=…] [reboot=pass|fail|not_run]
                     [out=<kit repo dir>] [problem=…] [bench_machine=…] [url_at=YYYY-mm-ddTHH:MM] [models="1=Opus 5.5+fast,2=…"]
                     [dq_risks=<n>] [date=YYYY-mm-dd] [journal=<path>] [claims=<path>] [sessions=<path>] [runs_txt=<file>]
      Run from a contest repo (task practice:record). Reads the board (<git common dir>/lanes or LANES_DIR): events.jsonl,
      cc-events.jsonl, otel/logs.jsonl; run scores from the ops panel (runs.py, when OPS_HOST / SSH_KEY are set),
      runs_txt (a saved `task runs` output) and `run <ts> score=<n>` in docs/journal.md; lanes per PR from the [PR]
      messages, the journal and docs/claims.md. Writes <out>/practice-log/<date>-<slug>.json (out = KIT_DIR by default);
      without either it prints the JSON and where to put it. Anything it cannot derive is null.
  practice.py report [dir=practice-log] [out=practice-log/report.html]
      One self-contained HTML (inline SVG, no external fetch) from every <dir>/*.json.

The numbers reuse timeline.py (lane:timeline): same URL time, bench cycles, prize hit, ships, idle split.
"""
import glob
import json
import os
import re
import statistics
import subprocess
import sys
from datetime import datetime, timezone

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import timeline  # noqa: E402

SCHEMA_VERSION = 1
LANES = ["1", "2", "3", "c"]
IDLE_MAP = {"human": "human", "lane": "other_lane", "none": "nothing", "agents": "subagent", "forgot": "unmarked",
            "unknown": "unmarked"}


# ---------- inputs ----------

def git_top(cwd=None):
    try:
        return subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True, text=True, cwd=cwd,
                              check=True).stdout.strip()
    except (subprocess.CalledProcessError, FileNotFoundError):
        return ""


def read_text(path):
    try:
        with open(path) as f:
            return f.read()
    except (OSError, TypeError):
        return ""


def parse_runs_list(text):
    """`task runs` / runs.py list output → [{ts, score, req, err, git}] (scored rows only)."""
    out = []
    for line in (text or "").splitlines():
        m = re.match(r"\s*(\d{8}-\d{6})\s+score=([0-9.]+)\s+req=(\d+)\s+err=(\d+)", line)
        if m:
            g = re.search(r"git=([0-9a-f]{7,})", line)
            out.append({"ts": m.group(1), "score": m.group(2), "req": int(m.group(3)), "err": int(m.group(4)),
                        "git": {"commit": g.group(1)} if g else {}})
    return out


def journal_lines(text):
    """[[time, branch / PR, what, why, result]] from docs/journal.md (`a | b | c | d | e` lines)."""
    out = []
    for line in (text or "").splitlines():
        parts = [p.strip() for p in line.split(" | ")]
        if len(parts) >= 5 and re.match(r"\d{1,2}:\d{2}$", parts[0]):
            out.append(parts[:4] + [" | ".join(parts[4:])])
    return out


def journal_runs(text):
    """Scored runs written in the journal: `run <ts> score=<n>` → [{ts, score}] (first mention of each ts). A short
    `run 161953 score=…` takes the date of the last full ts above it."""
    out, seen, day = [], set(), None
    for m in re.finditer(r"run (\d{8}-)?(\d{6}) score=([0-9][0-9,]*)", text or ""):
        day = m.group(1) or day
        if not day:
            continue
        ts = day + m.group(2)
        if ts not in seen:
            seen.add(ts)
            out.append({"ts": ts, "score": m.group(3).replace(",", "")})
    return out


def lane_of_text(s):
    m = re.search(r"(?:lane|レーン)\s*([123c])\b", s or "", re.I)
    return m.group(1).lower() if m else None


def claims_owners(text):
    """{branch: lane} from docs/claims.md rows (owner column: lane2 / laneC / lane 3 …)."""
    out = {}
    for line in (text or "").splitlines():
        cells = [c.strip() for c in line.strip().strip("|").split("|")]
        if len(cells) >= 5 and cells[0] and not cells[0].startswith("---") and cells[0] != "branch":
            br = re.sub(r"\s*\(.*?\)|（.*?）", "", cells[0]).strip("` ")
            ln = lane_of_text(cells[3])
            if br and ln:
                out[br] = ln
    return out


def pr_heads(cc):
    """{pr: lane} from `[PR] #n ready …` messages (SendMessage head; `/ #m ready` names more PRs)."""
    out = {}
    for e in cc:
        h = str(e.get("head") or "")
        if e.get("event") != "PreToolUse" or e.get("tool") != "SendMessage" or not h.startswith("[PR]"):
            continue
        ln = e.get("lane")
        if ln not in LANES:
            continue
        seg = re.split(r"\s/\s*\[", h)[0]
        for m in re.finditer(r"(?:\[PR\]|/)\s*#(\d+)\b", seg):
            out.setdefault(int(m.group(1)), ln)
    return out


def review_catches(cc):
    """`[DONE] c review #n ng …` or `… <n> 件` with n > 0, once per message; None when no review result was sent."""
    seen, n, any_ = set(), 0, False
    for e in cc:
        h = str(e.get("head") or "")
        if e.get("event") != "PreToolUse" or e.get("tool") != "SendMessage" or not re.match(r"\[DONE\] c review", h):
            continue
        key = (e.get("sid"), h)
        if key in seen:
            continue
        seen.add(key)
        any_ = True
        m = re.search(r"(\d+)\s*件", h)
        if re.search(r"#\d+\s+ng\b", h) or (m and int(m.group(1)) > 0):
            n += 1
    return n if any_ else None


def merged_prs(evs, jl):
    """PR numbers merged: ship.end `merged` (the board), else journal lines whose result says merge."""
    out = []
    for e in evs:
        if e["kind"] == "ship.end" and e.get("ok"):
            out += [int(x) for x in re.findall(r"#?(\d+)", str(e.get("merged") or ""))]
    if not out:
        for row in jl:
            if re.search(r"\bmerge\b", row[4]) and not row[1].startswith("main "):
                out += [int(x) for x in re.findall(r"#(\d+)", row[1])]
    return list(dict.fromkeys(out))


def pr_lanes(prs, cc, jl, claims):
    """{pr: lane or "?"}: 1. the lane that sent `[PR] #n`, 2. `lane X` / `レーン X` in the journal line of that PR,
    3. the claims owner of the journal line's branch (same name, or one ending with /<branch>; integ/<ts> → the
    owner of every fix/<ts>/* row when they agree)."""
    heads = pr_heads(cc)
    by_pr = {}
    for row in jl:
        for x in re.findall(r"([\w./-]+)\s*/\s*#(\d+)", row[1]):
            by_pr.setdefault(int(x[1]), (x[0], row))
        for n in re.findall(r"#(\d+)", row[1]):
            by_pr.setdefault(int(n), ("", row))
    out = {}
    for n in prs:
        if n in heads:
            out[n] = heads[n]
            continue
        br, row = by_pr.get(n, ("", None))
        ln = lane_of_text(row[2]) if row else None
        if not ln and br:
            ln = claims_lane(br, claims)
        out[n] = ln or "?"
    return out


def claims_lane(br, claims):
    """The claims owner of a journal branch: same name ("/" and "-" alike), or a claims branch ending with it
    (ops-pdns → fix/<ts>/ops-pdns); integ/<x> → the one owner of every <x>/* or fix/<x>/* row."""
    def nm(x):
        return x.replace("/", "-")
    b = nm(br)
    for k, v in claims.items():
        if nm(k) == b:
            return v
    ends = {v for k, v in claims.items() if nm(k).endswith("-" + b)}
    if len(ends) == 1:
        return ends.pop()
    m = re.match(r"integ/(.+)$", br) or re.match(r"(fix/\d{8}-\d{6})$", br)
    if m:
        x = m.group(1)
        owners = {v for k, v in claims.items() if k.startswith(x + "/") or k.startswith("fix/" + x + "/")}
        if len(owners) == 1:
            return owners.pop()
    return None


def load_sessions_models(path):
    """{lane: {"model": label, "fast": bool}} from tools/lanes/sessions.json; {} when absent."""
    try:
        with open(path) as f:
            d = json.load(f)
    except (OSError, ValueError, TypeError):
        return {}
    out = {}
    for ln, v in (d.get("lanes") or {}).items():
        if isinstance(v, dict):
            out[ln] = {"model": v.get("model_label") or v.get("model"), "fast": bool(v.get("fast"))}
    return out


def parse_models(s):
    """"1=Opus 5.5+fast,2=Opus（既定）" → {lane: {model, fast}}."""
    out = {}
    for part in (s or "").split(","):
        if "=" in part:
            ln, v = part.split("=", 1)
            fast = v.strip().endswith("+fast")
            out[ln.strip()] = {"model": v.strip()[:-5].strip() if fast else v.strip(), "fast": fast}
    return out


def model_config(models):
    """A short label of the model set, e.g. "1 Opus 5.5+fast / 2 3 Opus 5.5 / c Fable 5.1"."""
    groups = {}
    for ln in LANES:
        m = models.get(ln)
        if m and m.get("model"):
            groups.setdefault(m["model"] + ("+fast" if m.get("fast") else ""), []).append(ln)
    return " / ".join(" ".join(v) + " " + k for k, v in groups.items()) or None


# ---------- the record ----------

def r1(x, n=1):
    if x is None:
        return None
    return int(round(x)) if n == 0 else round(x, n)


def med(xs):
    xs = [x for x in xs if x is not None]
    return statistics.median(xs) if xs else None


def union_seconds(spans, a, b):
    """Total length of the union of [s, e] spans clipped to [a, b]."""
    xs = sorted((max(s, a), min(e, b)) for s, e in spans if min(e, b) > max(s, a))
    tot, cur = 0, None
    for s, e in xs:
        if cur is None or s > cur[1]:
            if cur:
                tot += cur[1] - cur[0]
            cur = [s, e]
        else:
            cur[1] = max(cur[1], e)
    if cur:
        tot += cur[1] - cur[0]
    return tot


def compute(evs, cc=None, otel_rows=None, runs_rows=None, journal="", claims="", target=None, url_at=None, meta=None):
    """The record (dict, SCHEMA.md) from the board's events, Claude Code hooks, OTel rows, runs and the journal."""
    cc = cc or []
    meta = dict(meta or {})
    jl = journal_lines(journal)
    rows = list(runs_rows or [])
    known = {r.get("ts") for r in rows}
    rows += [r for r in journal_runs(journal) if r["ts"] not in known]
    scores = timeline.collect_scores(evs, rows)
    url_t = timeline.url_time(evs) or url_at
    times = [e["t"] for e in evs] + [e["t"] for e in cc] + [t for t, _ in scores]
    first = min(times) if times else None
    start = url_t or first
    # the window ends with the board's last event (cc-events go on with later sessions, e.g. the retro); no board:
    # the last run or hook event
    end = max(e["t"] for e in evs) if evs else (max(times) if times else None)
    hit = next(((t, sc) for t, sc in scores if target and sc >= target and (start is None or t >= start)), None)
    prize_t = hit[0] if hit else None

    # speed
    cycles = timeline.bench_cycles(evs, scores)
    readies = [c["ready"] for c in cycles if start is None or c["ready"] >= start]
    if not readies:  # no board: each scored run's start stands for its bench
        readies = sorted(t for t in (timeline.run_epoch(r.get("ts")) for r in rows) if t and (start is None or t >= start))
    base_ready = readies[0] if readies else None
    if not prize_t:
        benches_to_prize = None
    elif cycles:  # the board's benches (band → ready) up to the one whose score crossed
        benches_to_prize = sum(1 for c in cycles if (start is None or c["ready"] >= start) and c["ready"] <= prize_t)
    else:  # no board: scored runs up to the crossing one
        benches_to_prize = sum(1 for t, sc in scores if (start is None or t >= start) and t <= prize_t)
    loops = [(b - a) / 60 for a, b in zip(readies, readies[1:])]
    ships = [e.get("sec") for e in evs if e["kind"] == "ship.end" and e.get("sec") is not None]
    deps = [e.get("sec") for e in evs if e["kind"] == "deploy.end" and e.get("sec") is not None]
    speed = {
        "url_to_baseline_ready_min": r1((base_ready - url_t) / 60) if url_t and base_ready else None,
        "benches_to_prize": benches_to_prize,
        "loop_median_min": r1(med(loops)),
        "ship_median_s": r1(med(ships), 0),
        "ship_max_s": max(ships) if ships else None,
        "deploy_median_s": r1(med(deps), 0),
    }

    # outcome
    in_win = [(t, sc) for t, sc in scores if start is None or t >= start]
    best = max((sc for _, sc in in_win), default=None)
    at60 = max((sc for t, sc in in_win if url_t and t <= url_t + 3600), default=None)
    alerts = [e for e in evs if e["kind"] == "alert"]
    fails = sum(1 for r in rows if str(r.get("score")) in ("0", "0.0")) + \
        sum(1 for row in jl if re.search(r"(ベンチ|bench)[^、。,;]*(失敗|FAIL|fail)", row[2] + " " + row[4]))
    reverts = sum(1 for row in jl if re.search(r"revert", " ".join(row[1:4]) + " " + row[4], re.I)
                  and not re.search(r"revert ?(なし|しない)|no revert", " ".join(row), re.I))
    outcome = {
        "score_at_60min_ratio": r1(at60 / target, 3) if at60 is not None and target and url_t else None,
        "best_ratio": r1(best / target, 3) if best is not None and target else None,
        "best_score": int(best) if best is not None else None,
        "runs": len(in_win) if scores else None,
        "fails": fails if (rows or jl) else None,
        "new_5xx_alerts": sum(1 for e in alerts if re.search(r"5xx|\b50\d\b|deadlock", str(e.get("msg")), re.I)) if evs else None,
        "reverts": reverts if jl else None,
        "reboot_test": meta.pop("reboot_test", None) or "not_run",
    }

    # agents
    prs = merged_prs(evs, jl)
    lane_pr = pr_lanes(prs, cc, jl, claims_owners(claims))
    st, _ = timeline.cc_stats(cc, start, end) if cc else ({}, [])
    if cc:
        _, per, before, _, _ = timeline.idle_split(evs, cc, start, prize_t or end, prize_t)
    else:
        per, before = {}, {}
    api = timeline.otel_api_requests(otel_rows or [], start or 0, end or 0) if otel_rows else {}
    api_prize = timeline.otel_api_requests(otel_rows or [], start or 0, prize_t) if otel_rows and prize_t else {}
    models = meta.get("models") or {}
    agents = {}
    for ln in LANES:
        L = st.get(ln)
        P = per.get(ln)
        idle = None
        if P is not None:
            idle = {k: 0.0 for k in ("human", "other_lane", "nothing", "subagent", "unmarked")}
            for k, v in P.items():
                idle[IDLE_MAP.get(k, "unmarked")] += v / 60
            idle = {k: r1(v) for k, v in idle.items()}
        agents[ln] = {
            "model": (models.get(ln) or {}).get("model"),
            "cost_usd": r1(api[ln]["cost"], 2) if ln in api else None,
            "turns": len(L["turns"]) if L else None,
            "turn_median_s": r1(med(L["turns"]), 0) if L and L["turns"] else None,
            "busy_min": r1(L["busy"] / 60) if L else None,
            "idle_min_before_prize": r1(before.get(ln, 0) / 60) if cc and prize_t and ln in per else None,
            "idle_breakdown": idle,
            "merged_prs": sum(1 for v in lane_pr.values() if v == ln) if prs else None,
            "alerts_raised": sum(1 for e in alerts if e.get("lane") == ln) if evs else None,
            "permission_wait_s": r1(sum(L["perm"]), 0) if L else None,
        }
    cost = sum(v["cost"] for v in api.values()) if api else None
    totals = {
        "cost_usd": r1(cost, 2),
        "cost_to_prize_usd": r1(sum(v["cost"] for v in api_prize.values()), 2) if api_prize else None,
        "merged_prs": len(prs) if prs else None,
        "merged_unattributed": sum(1 for v in lane_pr.values() if v == "?") if prs else None,
        "merged_per_usd": r1(len(prs) / cost, 2) if prs and cost else None,
    }

    # human
    todos = [e for e in evs if e["kind"] == "todo"]
    done = [e for e in evs if e["kind"] == "todo_done"]
    kind_of = {e.get("id"): e.get("tkind", "other") for e in todos}
    by_kind = {}
    for e in done:
        by_kind.setdefault(e.get("tkind") or kind_of.get(e.get("id"), "other"), []).append(e.get("sec") or 0)
    created = {e.get("id"): e["t"] for e in todos}
    closed = {e.get("id"): e["t"] for e in done}
    spans = [(t0, closed.get(i, end)) for i, t0 in created.items()]
    human = {
        "todos": len(todos) if evs else None,
        "response_median_s": {k: r1(med(v), 0) for k, v in sorted(by_kind.items())} if done else None,
        "total_wait_min": r1(union_seconds(spans, start, end) / 60) if todos and start else None,
        "wait_before_prize_min": r1(union_seconds(spans, start, prize_t) / 60) if todos and start and prize_t else None,
        "decisions": {"choices": sum(1 for e in done if (e.get("tkind") or kind_of.get(e.get("id"))) == "question"),
                      "picks": sum(1 for e in done if (e.get("tkind") or kind_of.get(e.get("id"))) == "pick"),
                      "pr_reviews": sum(1 for e in done if (e.get("tkind") or kind_of.get(e.get("id"))) == "review")}
        if done else None,
    }
    safety = {
        "crit_alerts": sum(1 for e in alerts if e.get("level") == "crit") if evs else None,
        "review_catches": review_catches(cc) if cc else None,
        "dq_risks_found": meta.pop("dq_risks_found", None),
    }
    north = r1((prize_t - url_t) / 60) if prize_t and url_t else None
    m = {
        "date": meta.get("date") or (datetime.fromtimestamp(start).strftime("%Y-%m-%d") if start else None),
        "slug": meta.get("slug"),
        "contest_repo": meta.get("contest_repo"),
        "problem": meta.get("problem"),
        "prize_target": target,
        "kit_commit": meta.get("kit_commit"),
        "kit_head": meta.get("kit_head"),
        "models": models or None,
        "model_config": model_config(models),
        "bench_machine": meta.get("bench_machine"),
        "hosts": meta.get("hosts"),
        "url_at": datetime.fromtimestamp(url_t).strftime("%Y-%m-%dT%H:%M:%S") if url_t else None,
        "notes": {"changed": meta.get("notes_changed") or "", "hypothesis": meta.get("notes_hypothesis") or "",
                  "observations": meta.get("notes_obs") or ""},
    }
    return {"schema_version": SCHEMA_VERSION, "meta": m, "north_star": north, "speed": speed, "outcome": outcome,
            "agents": agents, "agents_total": totals, "human": human, "safety": safety,
            "pr_lanes": {str(k): v for k, v in sorted(lane_pr.items())} or None}


# ---------- CLI: record ----------

def parse_at(s):
    if not s:
        return None
    for fmt in ("%Y-%m-%dT%H:%M:%S", "%Y-%m-%dT%H:%M", "%Y-%m-%d %H:%M"):
        try:
            return datetime.strptime(s.strip(), fmt).timestamp()
        except ValueError:
            pass
    raise SystemExit("url_at: YYYY-mm-ddTHH:MM (local time)")


def guess_problem(top):
    m = re.search(r"ISUCON\s?1\d", read_text(os.path.join(top, "CLAUDE.md")) + read_text(os.path.join(top, "docs", "kickoff.md")))
    return m.group(0).replace(" ", "") if m else None


def hosts_count(top):
    m = re.search(r"^HOSTS=[\"']?([^\"'\n]*)", read_text(os.path.join(top, ".env")), re.M)
    return len(m.group(1).split()) if m and m.group(1).strip() else None


def template_merge(top):
    try:
        out = subprocess.run(["git", "log", "--grep", "merge template", "-1", "--format=%h"], capture_output=True,
                             text=True, cwd=top).stdout.strip()
        return out or None
    except FileNotFoundError:
        return None


def record_main(a):
    import lanes
    top = git_top() or "."
    d = lanes.lanes_dir()
    evs = timeline.load_events(os.path.join(d, "events.jsonl"))
    cc = timeline.load_cc(os.path.join(d, "cc-events.jsonl"))
    otel = timeline.load_jsonl(os.path.join(d, "otel", "logs.jsonl"))
    rows = timeline.fetch_runs()
    if a.get("runs_txt"):
        have = {r.get("ts") for r in rows}
        rows += [r for r in parse_runs_list(read_text(a["runs_txt"])) if r["ts"] not in have]
    models = load_sessions_models(a.get("sessions") or os.path.join(top, "tools", "lanes", "sessions.json"))
    models.update(parse_models(a.get("models")))
    kit = (a.get("out") or os.environ.get("KIT_DIR") or "").strip()
    meta = {
        "slug": (a.get("slug") or "").strip() or None, "date": (a.get("date") or "").strip() or None,
        "contest_repo": os.path.basename(top), "problem": (a.get("problem") or "").strip() or guess_problem(top),
        "kit_commit": template_merge(top), "kit_head": None, "models": models,  # kit_head: the kit's HEAD now (out / KIT_DIR)
        "bench_machine": (a.get("bench_machine") or "").strip() or None, "hosts": hosts_count(top),
        "notes_changed": a.get("notes_changed"), "notes_hypothesis": a.get("notes_hypothesis"), "notes_obs": a.get("notes_obs"),
        "reboot_test": (a.get("reboot") or "").strip() or None,
        "dq_risks_found": int(a["dq_risks"]) if (a.get("dq_risks") or "").strip().isdigit() else None,
    }
    if kit and os.path.isdir(os.path.join(kit, ".git")) or (kit and git_top(kit)):
        try:
            meta["kit_head"] = subprocess.run(["git", "rev-parse", "--short", "HEAD"], capture_output=True, text=True,
                                              cwd=kit).stdout.strip() or None
        except FileNotFoundError:
            pass
    if "kit_head" in a:  # backfill: kit_head=- (unknown) or the sha the practice used
        meta["kit_head"] = a["kit_head"].strip() if a["kit_head"].strip() not in ("", "-") else None
    rec = compute(evs, cc, otel, rows, read_text(a.get("journal") or os.path.join(top, "docs", "journal.md")),
                  read_text(a.get("claims") or os.path.join(top, "docs", "claims.md")), lanes.prize_target(),
                  parse_at(a.get("url_at")), meta)
    if not rec["meta"]["slug"]:
        rec["meta"]["slug"] = (rec["meta"]["problem"] or os.path.basename(top)).lower()
    name = f"{rec['meta']['date'] or 'undated'}-{rec['meta']['slug']}.json"
    text = json.dumps(rec, ensure_ascii=False, indent=2) + "\n"
    if kit:
        path = os.path.join(kit, "practice-log", name)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w") as f:
            f.write(text)
        return f"wrote {path}\nnext: task practice:report in the kit repo, then a PR to the kit"
    return text + f"\n# KIT_DIR / OUT not set: save the JSON above as <kit repo>/practice-log/{name}"


# ---------- CLI: report ----------

def report_main(a):
    import practice_report
    d = a.get("dir") or "practice-log"
    recs = []
    for p in sorted(glob.glob(os.path.join(d, "*.json"))):
        try:
            with open(p) as f:
                r = json.load(f)
            if isinstance(r, dict) and r.get("schema_version"):
                recs.append(r)
        except (OSError, ValueError):
            print(f"skip {p}: not JSON", file=sys.stderr)
    out = a.get("out") or os.path.join(d, "report.html")
    with open(out, "w") as f:
        f.write(practice_report.render(recs))
    return f"wrote {out} ({len(recs)} records)"


def main(argv):
    if not argv or argv[0] not in ("record", "report"):
        print(__doc__.strip())
        return 2
    a = {}
    for x in argv[1:]:
        if "=" in x:
            k, v = x.split("=", 1)
            a[k.strip().lower()] = v
    print(record_main(a) if argv[0] == "record" else report_main(a))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
