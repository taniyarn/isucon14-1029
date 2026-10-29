#!/usr/bin/env python3
"""practice-log/*.json → one self-contained HTML (inline SVG + a few lines of JS for tooltips; no external fetch).

Charts: (a) URL → special prize per practice, annotated with notes.changed; (b) URL → baseline / baseline → prize
(stacked) and the human wait before the prize; (c) cost to the prize vs minutes to the prize, by model set;
(d) per lane over practices: cost, idle before the prize, merged PRs; (e) a table of every record.
Works with 1..N records; a null value is a gap (no mark), never a zero.
"""
import html
import json

W = 560  # chart width in a half card (SVG viewBox units ≈ CSS px, so text keeps its size)
WW = 1180  # in a wide card
SLOTS = 3  # categorical slots used for model sets in the scatter (the palette validates 3 for all-pairs forms)
LANE_SLOT = {"1": 1, "2": 2, "3": 3, "c": 4}
LANE_NAME = {"1": "1 ハブ", "2": "2 インフラ", "3": "3 アプリ", "c": "c スカウト"}

CSS = """
.viz-root{color-scheme:light;--surface:#fcfcfb;--page:#f9f9f7;--ink:#0b0b0b;--ink2:#52514e;--muted:#898781;
--grid:#e1e0d9;--axis:#c3c2b7;--border:rgba(11,11,11,.10);--other:#898781;
--s1:#2a78d6;--s2:#eb6834;--s3:#1baf7a;--s4:#eda100}
@media (prefers-color-scheme:dark){:root:where(:not([data-theme="light"])) .viz-root{color-scheme:dark;--surface:#1a1a19;
--page:#0d0d0d;--ink:#fff;--ink2:#c3c2b7;--muted:#898781;--grid:#2c2c2a;--axis:#383835;--border:rgba(255,255,255,.10);
--s1:#3987e5;--s2:#d95926;--s3:#199e70;--s4:#c98500}}
:root[data-theme="dark"] .viz-root{color-scheme:dark;--surface:#1a1a19;--page:#0d0d0d;--ink:#fff;--ink2:#c3c2b7;
--grid:#2c2c2a;--axis:#383835;--border:rgba(255,255,255,.10);--s1:#3987e5;--s2:#d95926;--s3:#199e70;--s4:#c98500}
body{margin:0}
.viz-root{background:var(--page);color:var(--ink);font:14px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif;padding:24px;min-height:100vh}
h1{font-size:20px;margin:0 0 4px} h2{font-size:15px;margin:0 0 2px} .sub{color:var(--ink2);font-size:12.5px;margin:0 0 8px}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(520px,1fr));gap:16px;margin-top:16px}
.card{background:var(--surface);border:1px solid var(--border);border-radius:8px;padding:14px 16px}
.wide{grid-column:1/-1}
svg{display:block;width:100%;height:auto;overflow:visible}
svg text{pointer-events:none;fill:var(--muted);font-size:11px;font-variant-numeric:tabular-nums}
svg text.lab{fill:var(--ink2)} svg text.val{fill:var(--ink);font-weight:600}
.gl{stroke:var(--grid);stroke-width:1} .ax{stroke:var(--axis);stroke-width:1}
.f1{fill:var(--s1)} .f2{fill:var(--s2)} .f3{fill:var(--s3)} .f4{fill:var(--s4)} .f0{fill:var(--other)}
.l1{stroke:var(--s1)} .l2{stroke:var(--s2)} .l3{stroke:var(--s3)} .l4{stroke:var(--s4)} .l0{stroke:var(--other)}
.line{fill:none;stroke-width:2;stroke-linejoin:round;stroke-linecap:round}
.dot{stroke:var(--surface);stroke-width:2;pointer-events:none}
.hollow{fill:var(--surface);stroke:var(--muted);stroke-width:2}
.hit{fill:transparent;cursor:default} .hit:focus{outline:none} .hit:focus+.dot,.hit:hover+.dot{stroke:var(--ink)}
.legend{display:flex;flex-wrap:wrap;gap:4px 14px;color:var(--ink2);font-size:12px;margin:2px 0 6px}
.legend i{display:inline-block;width:10px;height:10px;border-radius:2px;margin-right:5px;vertical-align:-1px}
.legend i.f1{background:var(--s1)} .legend i.f2{background:var(--s2)} .legend i.f3{background:var(--s3)} .legend i.f4{background:var(--s4)} .legend i.f0{background:var(--other)}
.empty{color:var(--muted);font-size:12.5px;padding:24px 0;text-align:center}
.note{color:var(--muted);font-size:12px;margin-top:6px}
table{border-collapse:collapse;width:100%;font-size:12.5px;font-variant-numeric:tabular-nums}
th,td{border-bottom:1px solid var(--grid);padding:5px 8px;text-align:right;white-space:nowrap}
th{color:var(--ink2);font-weight:600;position:sticky;top:0;background:var(--surface)}
td.t,th.t{text-align:left;white-space:normal;min-width:160px}
.tablewrap{overflow-x:auto}
#tip{position:fixed;pointer-events:none;background:var(--surface);color:var(--ink);border:1px solid var(--border);
border-radius:6px;padding:6px 9px;font-size:12px;box-shadow:0 2px 10px rgba(0,0,0,.12);display:none;max-width:320px;z-index:9}
#tip b{font-weight:600}
.sm{display:grid;grid-template-columns:110px repeat(4,1fr);gap:4px 10px;align-items:center}
.sm .h{color:var(--ink2);font-size:12px;font-weight:600}
.sm .rl{color:var(--ink2);font-size:12px}
"""

JS = """
(function(){var tip=document.getElementById('tip');
function show(e){var t=e.target.closest('[data-tip]');if(!t){tip.style.display='none';return;}
tip.textContent='';t.getAttribute('data-tip').split('\\n').forEach(function(l,i){var d=document.createElement('div');
if(i===0){var b=document.createElement('b');b.textContent=l;d.appendChild(b);}else{d.textContent=l;}tip.appendChild(d);});
tip.style.display='block';var r=t.getBoundingClientRect();var x=(e.clientX||r.right)+12,y=(e.clientY||r.top)+12;
if(x+tip.offsetWidth>innerWidth-8)x=innerWidth-tip.offsetWidth-8;if(y+tip.offsetHeight>innerHeight-8)y=y-tip.offsetHeight-24;
tip.style.left=x+'px';tip.style.top=y+'px';}
document.addEventListener('pointermove',show);document.addEventListener('focusin',show);
document.addEventListener('focusout',function(){tip.style.display='none';});})();
"""


def esc(s):
    return html.escape("" if s is None else str(s), quote=True)


def get(r, path):
    cur = r
    for k in path.split("."):
        if not isinstance(cur, dict):
            return None
        cur = cur.get(k)
    return cur


def label(r):
    m = r.get("meta") or {}
    return f"{m.get('date') or '?'} {m.get('problem') or m.get('slug') or ''}".strip()


def fmt(v, unit=""):
    if v is None:
        return "—"
    if isinstance(v, float):
        v = f"{v:,.1f}".rstrip("0").rstrip(".") if abs(v) < 1000 else f"{v:,.0f}"
    elif isinstance(v, int):
        v = f"{v:,}"
    return f"{v}{unit}"


def nice_step(v, n=4):
    """A clean tick step (1 / 2 / 2.5 / 5 × 10^k) so that about n steps cover v."""
    import math
    raw = (v if v and v > 0 else 1) / n
    e = 10 ** math.floor(math.log10(raw))
    for m in (1, 2, 2.5, 5, 10):
        if m * e >= raw:
            return m * e
    return 10 * e


def nice_max(v):
    """The axis top: v rounded up to a whole number of clean steps."""
    import math
    st = nice_step(v)
    return st * max(1, math.ceil((v if v and v > 0 else 1) / st - 1e-9))


def ticks(vmax):
    st = nice_step(vmax)
    out, v = [], 0.0
    while v <= vmax + 1e-9:
        out.append(v)
        v += st
    return out


def short(s, n):
    s = (s or "").strip()
    return s if len(s) <= n else s[: n - 1] + "…"


def yaxis(x0, x1, y0, y1, vmax, unit=""):
    out = []
    for v in ticks(vmax):
        y = y1 - (y1 - y0) * v / vmax
        out.append(f'<line class="{"ax" if v == 0 else "gl"}" x1="{x0}" x2="{x1}" y1="{y:.1f}" y2="{y:.1f}"/>')
        out.append(f'<text x="{x0 - 6}" y="{y + 4:.1f}" text-anchor="end">{fmt(int(v) if v == int(v) else round(v, 1))}{unit}</text>')
    return out


def xlabels(xs, names, y):
    return [f'<text class="lab" x="{x:.1f}" y="{y}" text-anchor="middle">{esc(short(n, 22))}</text>' for x, n in zip(xs, names)]


def xpos(n, x0, x1):
    if n == 1:
        return [(x0 + x1) / 2]
    return [x0 + (x1 - x0) * i / (n - 1) for i in range(n)]


def svg(h, body, label_, w=W):
    return f'<svg viewBox="0 0 {w} {h}" role="img" aria-label="{esc(label_)}">' + "".join(body) + "</svg>"


def dot(x, y, cls, tip, r=4.5):
    """A marker with a hit target bigger than the mark (r 12) carrying the tooltip."""
    return (f'<circle class="hit" cx="{x:.1f}" cy="{y:.1f}" r="12" tabindex="0" data-tip="{esc(tip)}"/>'
            f'<circle class="dot {cls}" cx="{x:.1f}" cy="{y:.1f}" r="{r}"/>')


def line_path(pts):
    """Polyline through consecutive non-null points; a null breaks the line."""
    segs, cur = [], []
    for p in pts:
        if p is None:
            if len(cur) > 1:
                segs.append(cur)
            cur = []
        else:
            cur.append(p)
    if len(cur) > 1:
        segs.append(cur)
    return ["M" + " L".join(f"{x:.1f},{y:.1f}" for x, y in s) for s in segs]


# ---------- (a) north star ----------

def chart_north(recs):
    vals = [r.get("north_star") for r in recs]
    if not any(v is not None for v in vals):
        return '<div class="empty">特別賞を越えた記録が無い</div>'
    h, x0, x1, y0, y1 = 300, 52, WW - 20, 64, 250
    vmax = nice_max(max(v for v in vals if v is not None) * 1.1)
    xs = xpos(len(recs), x0 + 150, x1 - 150)  # room for the notes.changed label on both sides
    body = yaxis(x0, x1, y0, y1, vmax, " 分")
    pts = [None if v is None else (x, y1 - (y1 - y0) * v / vmax) for x, v in zip(xs, vals)]
    body += [f'<path class="line l1" d="{d}"/>' for d in line_path(pts)]
    for i, (r, x, p, v) in enumerate(zip(recs, xs, pts, vals)):
        ch = get(r, "meta.notes.changed") or ""
        tip = f"{label(r)}\nURL → 特別賞 {fmt(v, ' 分')}\nベンチ {fmt(get(r, 'speed.benches_to_prize'))} 本目で越えた\n変えたこと: {ch or '—'}"
        if p is None:
            body.append(f'<circle class="hollow" cx="{x:.1f}" cy="{y1:.1f}" r="4.5"/>')
            body.append(f'<rect class="hit" x="{x - 14:.1f}" y="{y1 - 14:.1f}" width="28" height="28" tabindex="0" data-tip="{esc(tip)}"/>')
            body.append(f'<text class="lab" x="{x:.1f}" y="{y1 - 12:.1f}" text-anchor="middle">未達</text>')
            continue
        body.append(dot(p[0], p[1], "f1", tip))
        body.append(f'<text class="val" x="{x:.1f}" y="{p[1] - 12:.1f}" text-anchor="middle">{fmt(v)} 分</text>')
        if ch:
            body.append(f'<text class="lab" x="{x:.1f}" y="{max(14, p[1] - (32 if i % 2 == 0 else 48)):.1f}" text-anchor="middle">{esc(short(ch, 34))}</text>')
    body += xlabels(xs, [label(r) for r in recs], h - 22)
    return svg(h, body, "URL から特別賞の目標を越えるまでの分（練習ごと）", WW)


# ---------- (b) stacked: url → baseline / baseline → prize, + human wait ----------

def chart_stack(recs):
    rows = []
    for r in recs:
        a = get(r, "speed.url_to_baseline_ready_min")
        ns = r.get("north_star")
        b = ns - a if ns is not None and a is not None else None
        rows.append((a, b, get(r, "human.wait_before_prize_min"), ns))
    if not any(any(v is not None for v in row[:3]) for row in rows):
        return '<div class="empty">データなし</div>'
    h, x0, x1, y0, y1 = 280, 48, W - 20, 20, 236
    tops = [max((row[0] or 0) + (row[1] or 0), row[2] or 0) for row in rows]
    vmax = nice_max(max(tops) * 1.1)
    xs = xpos(len(recs), x0 + 50, x1 - 50)
    bw = 24

    def yv(v):
        return y1 - (y1 - y0) * v / vmax

    body = yaxis(x0, x1, y0, y1, vmax, " 分")
    for r, x, (a, b, hw, ns) in zip(recs, xs, rows):
        base = 0
        segs = [(a, "f1", "URL → baseline の ready"), (b, "f2", "baseline → 特別賞")]
        drawn = [s for s in segs if s[0]]
        for i, (v, cls, name) in enumerate(drawn):
            top = i == len(drawn) - 1
            ya, yb = yv(base), yv(base + v)
            hgt = max(0.0, ya - yb - (2 if base > 0 else 0))  # 2px surface gap between stacked segments
            tip = f"{label(r)}\n{name} {fmt(v, ' 分')}"
            body.append(bar(x - bw - 2, yb, bw, hgt, cls, tip, rounded=top))
            base += v
        if ns is not None:
            body.append(f'<text class="val" x="{x - bw / 2 - 2:.1f}" y="{yv(base) - 6:.1f}" text-anchor="middle">{fmt(ns)}</text>')
        if hw is not None:
            tip = f"{label(r)}\n人を待っていた（URL → 特別賞の間に開いていた todo）{fmt(hw, ' 分')}"
            body.append(bar(x + 2, yv(hw), bw, y1 - yv(hw), "f3", tip, rounded=True))
            body.append(f'<text class="lab" x="{x + 2 + bw / 2:.1f}" y="{yv(hw) - 6:.1f}" text-anchor="middle">{fmt(hw)}</text>')
        if a is None and b is None and hw is None:
            body.append(f'<text class="lab" x="{x:.1f}" y="{y1 - 8}" text-anchor="middle">データなし</text>')
    body += xlabels(xs, [label(r) for r in recs], h - 22)
    return svg(h, body, "URL→baseline、baseline→特別賞、人待ち")


def bar(x, y, w, h, cls, tip, rounded=True):
    """Column with a 4px rounded data end and a square baseline."""
    if h <= 0:
        return ""
    r = min(4, h, w / 2) if rounded else 0
    d = (f"M{x:.1f},{y + h:.1f} L{x:.1f},{y + r:.1f} Q{x:.1f},{y:.1f} {x + r:.1f},{y:.1f} L{x + w - r:.1f},{y:.1f} "
         f"Q{x + w:.1f},{y:.1f} {x + w:.1f},{y + r:.1f} L{x + w:.1f},{y + h:.1f} Z")
    return f'<path class="{cls}" d="{d}" tabindex="0" data-tip="{esc(tip)}"/>'


# ---------- (c) scatter ----------

def config_slots(recs):
    order = []
    for r in recs:
        c = get(r, "meta.model_config") or "（記録なし）"
        if c not in order:
            order.append(c)
    return {c: (i + 1 if i < SLOTS else 0) for i, c in enumerate(order)}


def chart_scatter(recs):
    slots = config_slots(recs)
    pts = [(r, get(r, "agents_total.cost_to_prize_usd"), r.get("north_star")) for r in recs]
    ok = [p for p in pts if p[1] is not None and p[2] is not None]
    missing = [label(r) for r, c, n in pts if c is None or n is None]
    legend = '<div class="legend">' + "".join(
        f'<span><i class="f{s}"></i>{esc(c)}</span>' for c, s in slots.items() if s) + \
        ('<span><i class="f0"></i>その他のモデル構成</span>' if 0 in slots.values() else "") + "</div>"
    if not ok:
        return legend + '<div class="empty">費用（OTel）と特別賞の両方がある練習が無い</div>' + \
            (f'<div class="note">データなし: {esc("、".join(missing))}</div>' if missing else "")
    h, x0, x1, y0, y1 = 280, 48, W - 24, 16, 236
    xmax = nice_max(max(p[1] for p in ok) * 1.15)
    ymax = nice_max(max(p[2] for p in ok) * 1.15)
    body = yaxis(x0, x1, y0, y1, ymax, " 分")
    for v in ticks(xmax):
        x = x0 + (x1 - x0) * v / xmax
        body.append(f'<text x="{x:.1f}" y="{y1 + 16}" text-anchor="middle">${fmt(int(v) if v == int(v) else round(v, 1))}</text>')
    body.append(f'<text x="{x1}" y="{y1 + 32}" text-anchor="end">特別賞までの費用（USD、OTel）</text>')
    for r, c, n in ok:
        x, y = x0 + (x1 - x0) * c / xmax, y1 - (y1 - y0) * n / ymax
        s = slots.get(get(r, "meta.model_config") or "（記録なし）", 0)
        tip = f"{label(r)}\n{fmt(n, ' 分')} / ${fmt(c)}\n{get(r, 'meta.model_config') or 'モデル記録なし'}"
        body.append(dot(x, y, f"f{s}", tip))
        right = x > x1 - 150
        body.append(f'<text class="lab" x="{x - 9 if right else x + 9:.1f}" y="{y - 8:.1f}" text-anchor="{"end" if right else "start"}">{esc(label(r))}</text>')
    out = legend + svg(h + 20, body, "特別賞までの費用と分")
    if missing:
        out += f'<div class="note">点が無い練習（費用か特別賞が null）: {esc("、".join(missing))}</div>'
    return out


# ---------- (d) small multiples per lane ----------

SM_ROWS = [("cost_usd", "費用 USD", "$"), ("idle_min_before_prize", "特別賞までの idle 分", ""), ("merged_prs", "merge された PR", "")]


def chart_lanes(recs):
    names = [label(r) for r in recs]
    cells = ['<div class="sm"><div></div>'] + [f'<div class="h">{esc(LANE_NAME[ln])}</div>' for ln in LANE_SLOT]
    for key, title, pre in SM_ROWS:
        allv = [get(r, f"agents.{ln}.{key}") for r in recs for ln in LANE_SLOT]
        vmax = nice_max(max([v for v in allv if v is not None] or [0]) * 1.15)  # one y scale per row: lanes compare
        cells.append(f'<div class="rl">{esc(title)}<br><span class="sub">0〜{pre}{fmt(vmax)}</span></div>')
        for ln, slot in LANE_SLOT.items():
            vals = [get(r, f"agents.{ln}.{key}") for r in recs]
            if not any(v is not None for v in vals):
                cells.append('<div class="empty" style="padding:8px 0">—</div>')
                continue
            w, h, x0, x1, y0, y1 = 200, 70, 8, 192, 14, 60
            xs = [x0 + (x1 - x0) / 2] if len(recs) == 1 else [x0 + (x1 - x0) * i / (len(recs) - 1) for i in range(len(recs))]
            pts = [None if v is None else (x, y1 - (y1 - y0) * v / vmax) for x, v in zip(xs, vals)]
            body = [f'<line class="ax" x1="{x0}" x2="{x1}" y1="{y1}" y2="{y1}"/>']
            body += [f'<path class="line l{slot}" d="{d}"/>' for d in line_path(pts)]
            for n, p, v in zip(names, pts, vals):
                if p is not None:
                    body.append(dot(p[0], p[1], f"f{slot}", f"{LANE_NAME[ln]} · {n}\n{title} {pre}{fmt(v)}", r=4))
            last = next(((p, v) for p, v in zip(reversed(pts), reversed(vals)) if p is not None), None)
            if last:
                body.append(f'<text class="val" x="{min(last[0][0], x1 - 4):.1f}" y="{last[0][1] - 8:.1f}" text-anchor="end">{pre}{fmt(last[1])}</text>')
            cells.append(f'<svg viewBox="0 0 {w} {h}" role="img" aria-label="{esc(LANE_NAME[ln] + " " + title)}">' + "".join(body) + "</svg>")
    cells.append("</div>")
    return "".join(cells) + f'<div class="note">横軸 = 練習（左から {esc(" → ".join(names))}）。行ごとに同じ縦軸。null は点なし。</div>'


# ---------- (e) table ----------

COLS = [("練習", lambda r: label(r)), ("変えたこと", lambda r: get(r, "meta.notes.changed")),
        ("仮説", lambda r: get(r, "meta.notes.hypothesis")), ("見えたこと", lambda r: get(r, "meta.notes.observations")),
        ("目標", lambda r: get(r, "meta.prize_target")), ("URL→特別賞 分", lambda r: r.get("north_star")),
        ("URL→baseline 分", lambda r: get(r, "speed.url_to_baseline_ready_min")),
        ("特別賞までのベンチ", lambda r: get(r, "speed.benches_to_prize")), ("1 周 分", lambda r: get(r, "speed.loop_median_min")),
        ("ship 中央値 s", lambda r: get(r, "speed.ship_median_s")), ("deploy 中央値 s", lambda r: get(r, "speed.deploy_median_s")),
        ("60 分時点 / 目標", lambda r: get(r, "outcome.score_at_60min_ratio")), ("最高", lambda r: get(r, "outcome.best_score")),
        ("最高 / 目標", lambda r: get(r, "outcome.best_ratio")), ("run", lambda r: get(r, "outcome.runs")),
        ("fail", lambda r: get(r, "outcome.fails")), ("revert", lambda r: get(r, "outcome.reverts")),
        ("追試", lambda r: get(r, "outcome.reboot_test")), ("費用 $", lambda r: get(r, "agents_total.cost_usd")),
        ("特別賞まで $", lambda r: get(r, "agents_total.cost_to_prize_usd")), ("merge PR", lambda r: get(r, "agents_total.merged_prs")),
        ("PR / $", lambda r: get(r, "agents_total.merged_per_usd")), ("人待ち 分（特別賞まで）", lambda r: get(r, "human.wait_before_prize_min")),
        ("todo", lambda r: get(r, "human.todos")), ("crit", lambda r: get(r, "safety.crit_alerts")),
        ("レビューの指摘", lambda r: get(r, "safety.review_catches")), ("失格リスク", lambda r: get(r, "safety.dq_risks_found")),
        ("モデル", lambda r: get(r, "meta.model_config")), ("kit", lambda r: get(r, "meta.kit_commit"))]
TEXT_COLS = {"変えたこと", "仮説", "見えたこと", "モデル"}


def table(recs):
    head = "".join(f'<th class="{"t" if n in TEXT_COLS else ""}">{esc(n)}</th>' for n, _ in COLS)
    body = []
    for r in recs:
        body.append("<tr>" + "".join(f'<td class="{"t" if n in TEXT_COLS else ""}">{esc(fmt(f(r)) if not isinstance(f(r), str) else (f(r) or "—"))}</td>'
                                     for n, f in COLS) + "</tr>")
    return f'<div class="tablewrap"><table><thead><tr>{head}</tr></thead><tbody>{"".join(body)}</tbody></table></div>'


def render(recs):
    recs = sorted(recs, key=lambda r: (get(r, "meta.date") or "", get(r, "meta.slug") or ""))
    n = len(recs)
    if not recs:
        main = '<div class="card"><div class="empty">記録が無い: practice-log/*.json を足す（task practice:record）</div></div>'
    else:
        main = f"""
<div class="grid">
<div class="card wide"><h2>(a) URL → 特別賞（分、少ないほど良い）</h2><p class="sub">練習ごと。点の上はその練習で kit を変えたこと（notes.changed）。○ = 越えなかった</p>{chart_north(recs)}</div>
<div class="card"><h2>(b) 内訳: URL → baseline / baseline → 特別賞、人待ち</h2><p class="sub">左の積み上げ = URL → 特別賞。右の棒 = 同じ区間で人への todo が開いていた時間（重なりあり）</p>
<div class="legend"><span><i class="f1"></i>URL → baseline の ready</span><span><i class="f2"></i>baseline → 特別賞</span><span><i class="f3"></i>人を待っていた</span></div>{chart_stack(recs)}</div>
<div class="card"><h2>(c) 特別賞までの費用 × 分</h2><p class="sub">左下ほど良い。色 = モデルの組み合わせ（meta.model_config）</p>{chart_scatter(recs)}</div>
<div class="card wide"><h2>(d) レーンごとの推移</h2><p class="sub">費用（OTel）、特別賞までの idle、merge された PR の数</p>{chart_lanes(recs)}</div>
<div class="card wide"><h2>(e) 全記録</h2><p class="sub">— = null（記録から出せなかった）</p>{table(recs)}</div>
</div>"""
    data = json.dumps(recs, ensure_ascii=False).replace("</", "<\\/")
    return f"""<!doctype html>
<html lang="ja"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>practice-log — 練習ごとの推移</title><style>{CSS}</style></head>
<body><div class="viz-root">
<h1>practice-log — 練習ごとの推移（{n} 件）</h1>
<p class="sub">kit / エージェントの変更で、1 人 4 セッションの周回が速く・安くなったか。指標の定義は practice-log/SCHEMA.md</p>
{main}
<div id="tip" role="tooltip"></div>
<script type="application/json" id="records">{data}</script>
<script>{JS}</script>
</div></body></html>
"""
