package main

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Flame graph without the pprof web UI: `go tool pprof -raw` gives every sample with its stack; we fold them into
// unique stacks and write a single self-contained HTML. The page rebuilds the tree in the browser so it can filter
// frames out (runtime, syscall, …), fold the framework frames above the first main.* frame, list functions by
// self / cum wherever they are called from (a cost spread over many handlers shows up as one row), show the callers
// and callees of a function (sandwich), and colour by the change against the previous run's profile.
// Works for cpu and fgprof profiles.

const flameVersion = "flame-v5" // a cached -flame.html without this marker is rebuilt

var locHeadRe = regexp.MustCompile(`^(\d+): 0x[0-9a-fA-F]+ (?:M=\d+ )?(\S+)`)

type flameNode struct {
	Name     string       `json:"n"`
	Value    int64        `json:"v"`
	Children []*flameNode `json:"c,omitempty"`
	kids     map[string]*flameNode
}

func (n *flameNode) child(name string) *flameNode {
	if n.kids == nil {
		n.kids = map[string]*flameNode{}
	}
	c := n.kids[name]
	if c == nil {
		c = &flameNode{Name: name}
		n.kids[name] = c
		n.Children = append(n.Children, c)
	}
	return c
}

// flameStack is one unique call stack, root first, with its total value (ns for cpu, count for fgprof).
type flameStack struct {
	Frames []string
	Value  int64
}

// parsePprofStacks reads `go tool pprof -raw` output into unique root-first stacks. Stacks in -raw are leaf-first;
// each location may carry inlined callers on continuation lines (leaf first as well).
func parsePprofStacks(raw string) []flameStack {
	lines := strings.Split(raw, "\n")
	locs := map[string][]string{} // id -> frames leaf-first
	inLoc := false
	cur := ""
	for _, ln := range lines {
		if strings.HasPrefix(ln, "Locations") {
			inLoc = true
			continue
		}
		if strings.HasPrefix(ln, "Mappings") {
			inLoc = false
			continue
		}
		if !inLoc || strings.TrimSpace(ln) == "" {
			continue
		}
		t := strings.TrimSpace(ln)
		if m := locHeadRe.FindStringSubmatch(t); m != nil {
			// "1: 0x1048a9de4 M=1 main.leafB /tmp/x.go:10:0 s=10"
			cur = m[1]
			locs[cur] = []string{m[2]}
			continue
		}
		// continuation (inlined caller): "             main.midA /tmp/x.go:11:0 s=11"
		if cur != "" {
			if f := strings.Fields(t); len(f) >= 1 {
				locs[cur] = append(locs[cur], f[0])
			}
		}
	}
	byKey := map[string]int{}
	var out []flameStack
	inSamples := false
	for _, ln := range lines {
		if strings.HasPrefix(ln, "Samples:") {
			inSamples = true
			continue
		}
		if strings.HasPrefix(ln, "Locations") {
			break
		}
		if !inSamples {
			continue
		}
		i := strings.Index(ln, ": ")
		if i < 0 || strings.Contains(ln, "/") { // header line "samples/count cpu/nanoseconds"
			continue
		}
		vals := strings.Fields(ln[:i])
		ids := strings.Fields(ln[i+2:])
		if len(vals) == 0 || len(ids) == 0 {
			continue
		}
		v, err := strconv.ParseInt(vals[len(vals)-1], 10, 64) // last column: ns (cpu) or count (fgprof)
		if err != nil || v <= 0 {
			continue
		}
		var leafFirst []string
		for _, id := range ids {
			leafFirst = append(leafFirst, locs[id]...)
		}
		frames := make([]string, len(leafFirst))
		for j := range leafFirst {
			frames[j] = leafFirst[len(leafFirst)-1-j]
		}
		key := strings.Join(frames, "\x00")
		if k, ok := byKey[key]; ok {
			out[k].Value += v
			continue
		}
		byKey[key] = len(out)
		out = append(out, flameStack{Frames: frames, Value: v})
	}
	return out
}

// parsePprofRaw folds `go tool pprof -raw` output into a root node (kept for callers that want the tree).
func parsePprofRaw(raw string) *flameNode {
	root := &flameNode{Name: "all"}
	for _, s := range parsePprofStacks(raw) {
		n := root
		root.Value += s.Value
		for _, f := range s.Frames {
			n = n.child(f)
			n.Value += s.Value
		}
	}
	return root
}

// flameCum: per function, the share of all time spent in stacks that contain it (each stack counted once).
func flameCum(stacks []flameStack) map[string]float64 {
	var total int64
	cum := map[string]int64{}
	for _, s := range stacks {
		total += s.Value
		seen := map[string]bool{}
		for _, f := range s.Frames {
			if !seen[f] {
				seen[f] = true
				cum[f] += s.Value
			}
		}
	}
	out := map[string]float64{}
	if total == 0 {
		return out
	}
	for f, v := range cum {
		out[f] = float64(v) / float64(total)
	}
	return out
}

// flamePayload is what the page gets: a string table and stacks as index lists (small enough for a few thousand
// unique stacks), plus the previous run's cum share per function (empty when there is none).
type flamePayload struct {
	Names  []string           `json:"names"`
	Stacks [][]int            `json:"stacks"`
	Values []int64            `json:"values"`
	Total  int64              `json:"total"`
	Unit   string             `json:"unit"`
	Prev   map[string]float64 `json:"prev,omitempty"`
	PrevTS string             `json:"prev_ts,omitempty"`
	// per handler (the first main.* frame that is a registered route / ends in Handler, middleware excluded)
	Handlers     []flameHandler `json:"handlers,omitempty"`
	WindowNote   string         `json:"window_note,omitempty"`
	WinFrom      string         `json:"win_from,omitempty"` // CPU profile window, UTC "15:04:05" (same clock as the access log)
	WinTo        string         `json:"win_to,omitempty"`
	WinSec       float64        `json:"win_sec,omitempty"`
	CountsApprox bool           `json:"counts_approx,omitempty"`
}

type flameHandler struct {
	Fn       string  `json:"fn"`
	Endpoint string  `json:"ep,omitempty"`
	NS       int64   `json:"ns"`
	Count    int     `json:"count"`
	Approx   bool    `json:"approx,omitempty"`
	MsPerReq float64 `json:"ms_per_req,omitempty"`
}

// flameCtx: what the report knows besides the profile (nil = flame page without the handler table).
type flameCtx struct {
	access []accessEvent
	nz     *normalizer
	ci     *codeIndex
}

var middlewareRe = regexp.MustCompile(`(?i)middleware|measure|sqltag`)

func funcBase(frame string) string { // "main.getUserHandler.func1" -> "getUserHandler"
	f := strings.TrimPrefix(frame, "main.")
	if i := strings.IndexByte(f, '.'); i >= 0 {
		f = f[:i]
	}
	return f
}

// handlerIndex: index of the handler frame in a root-first stack, -1 when the stack has no main.* frame.
// Preference: a function registered as a route (code index) > a name ending in Handler > the first main.* frame,
// middleware and the measurement patch excluded.
func handlerIndex(frames []string, routes map[string]string) int {
	first, named := -1, -1
	for i, f := range frames {
		if !strings.HasPrefix(f, "main.") || middlewareRe.MatchString(f) {
			continue
		}
		b := funcBase(f)
		if _, ok := routes[b]; ok {
			return i
		}
		if named < 0 && strings.HasSuffix(b, "Handler") {
			named = i
		}
		if first < 0 {
			first = i
		}
	}
	if named >= 0 {
		return named
	}
	return first
}

var rawTimeRe = regexp.MustCompile(`(?m)^Time: (.+)$`)
var rawDurRe = regexp.MustCompile(`(?m)^Duration: ([\d.]+)`)

// profileWindow: when the profile was taken, from the -raw header ("Time: 2026-10-20 06:18:12.66 -1000 HST",
// "Duration: 30.1"). ok=false when the header is missing.
func profileWindow(raw string) (time.Time, time.Duration, bool) {
	m, d := rawTimeRe.FindStringSubmatch(raw), rawDurRe.FindStringSubmatch(raw)
	if m == nil || d == nil {
		return time.Time{}, 0, false
	}
	f := strings.Fields(strings.TrimSpace(m[1]))
	if len(f) < 3 {
		return time.Time{}, 0, false
	}
	t, err := time.Parse("2006-01-02 15:04:05.999999999 -0700", f[0]+" "+f[1]+" "+f[2])
	if err != nil {
		return time.Time{}, 0, false
	}
	sec, _ := strconv.ParseFloat(d[1], 64)
	return t, time.Duration(sec * float64(time.Second)), sec > 0
}

// flameHandlers sums the profile per handler and joins request counts from the access log in the profile window
// (or the whole run scaled to the window length when the window is unknown).
func flameHandlers(stacks []flameStack, raw string, ctx *flameCtx) ([]flameHandler, string) {
	routes := map[string]string{}
	if ctx != nil && ctx.ci != nil {
		routes = ctx.ci.routeRaw
	}
	ns := map[string]int64{}
	for _, s := range stacks {
		name := "(main 以外: runtime / GC / net など)"
		if i := handlerIndex(s.Frames, routes); i >= 0 {
			name = s.Frames[i]
		}
		ns[name] += s.Value
	}
	var out []flameHandler
	for fn, v := range ns {
		h := flameHandler{Fn: fn, NS: v}
		if strings.HasPrefix(fn, "main.") {
			h.Endpoint = routes[funcBase(fn)]
		}
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NS > out[j].NS || out[i].NS == out[j].NS && out[i].Fn < out[j].Fn })
	if ctx == nil || ctx.nz == nil || len(ctx.access) == 0 {
		return out, ""
	}
	from, dur, ok := profileWindow(raw)
	to := from.Add(dur)
	scale, note := 1.0, ""
	if ok {
		note = fmt.Sprintf("件数 = CPU プロファイルの窓 %s〜%s UTC（%.0f 秒）に入った access log のリクエスト（このホスト）。ms/件 = その窓の CPU 秒 ÷ 件数", from.UTC().Format("15:04:05"), to.UTC().Format("15:04:05"), dur.Seconds())
	} else {
		first, last := ctx.access[0].at, ctx.access[len(ctx.access)-1].at
		if span := last.Sub(first).Seconds(); span > 0 {
			scale = 30 / span
		}
		note = "件数は近似: ≈ run 全体の件数 × 30 / 負荷区間の秒（CPU プロファイルの時刻が読めなかった）。ms/件 = CPU 秒 ÷ その件数"
	}
	count := map[string]int{}
	for _, ev := range ctx.access {
		if ok && (ev.at.Before(from) || ev.at.After(to)) {
			continue
		}
		count[ev.method+" "+ctx.nz.normQuiet(ev.uri)]++
	}
	for i := range out {
		if out[i].Endpoint == "" {
			continue
		}
		n := float64(count[out[i].Endpoint]) * scale
		out[i].Count, out[i].Approx = int(n+0.5), !ok
		if n >= 1 {
			out[i].MsPerReq = float64(out[i].NS) / 1e6 / n
		}
	}
	return out, note
}

func buildFlamePayload(stacks []flameStack, unit string) flamePayload {
	var total int64
	for _, s := range stacks {
		total += s.Value
	}
	// drop the tail (< 0.01% each) into one "(other)" stack so the page stays small
	sort.SliceStable(stacks, func(i, j int) bool { return stacks[i].Value > stacks[j].Value })
	p := flamePayload{Total: total, Unit: unit}
	idx := map[string]int{}
	name := func(n string) int {
		if i, ok := idx[n]; ok {
			return i
		}
		idx[n] = len(p.Names)
		p.Names = append(p.Names, n)
		return idx[n]
	}
	var other int64
	for _, s := range stacks {
		if total > 0 && s.Value*10000 < total {
			other += s.Value
			continue
		}
		st := make([]int, len(s.Frames))
		for i, f := range s.Frames {
			st[i] = name(f)
		}
		p.Stacks = append(p.Stacks, st)
		p.Values = append(p.Values, s.Value)
	}
	if other > 0 {
		p.Stacks = append(p.Stacks, []int{name("(other, < 0.01% each)")})
		p.Values = append(p.Values, other)
	}
	return p
}

// prevProfile finds the same profile of the same host in the newest earlier run next to this one
// (<measurements>/<ts>/<host>/cpu.pb.gz → <measurements>/<earlier ts>/<host>/cpu.pb.gz). "" when there is none.
func prevProfile(profile string) (string, string) {
	hostDir := filepath.Dir(profile)
	runDir := filepath.Dir(hostDir)
	base := filepath.Dir(runDir)
	host, ts, file := filepath.Base(hostDir), filepath.Base(runDir), filepath.Base(profile)
	ents, err := os.ReadDir(base)
	if err != nil {
		return "", ""
	}
	var names []string
	for _, e := range ents {
		if e.IsDir() && e.Name() < ts {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	for _, n := range names {
		if runReqTotal(filepath.Join(base, n)) < minBenchReq { // mis-detected run (smoke, crawl, a hand-made initialize)
			continue
		}
		p := filepath.Join(base, n, host, file)
		if _, err := os.Stat(p); err == nil {
			return p, n
		}
	}
	return "", ""
}

// minBenchReq: a real bench is thousands of requests; fewer means a mis-detected run (same idea as tools/lanes/runs.py).
const minBenchReq = 1000

// runReqTotal sums hosts[].req_total of <run>/meta.json (0 when it is missing or unreadable).
func runReqTotal(runDir string) float64 {
	b, err := os.ReadFile(filepath.Join(runDir, "meta.json"))
	if err != nil {
		return 0
	}
	var meta struct {
		Hosts []struct {
			ReqTotal float64 `json:"req_total"`
		} `json:"hosts"`
	}
	if json.Unmarshal(b, &meta) != nil {
		return 0
	}
	var sum float64
	for _, h := range meta.Hosts {
		sum += h.ReqTotal
	}
	return sum
}

func goToolPprofRaw(profile string) (string, bool) {
	goBin := ""
	for _, c := range []string{"go", "/usr/local/go/bin/go"} {
		if p, err := exec.LookPath(c); err == nil {
			goBin = p
			break
		}
	}
	if goBin == "" {
		return "", false
	}
	raw, err := exec.Command(goBin, "tool", "pprof", "-raw", profile).Output()
	if err != nil {
		return "", false
	}
	return string(raw), true
}

// pprofFlameHTML writes <profile>-flame.html next to the profile and returns its path ("" when go is missing).
func pprofFlameHTML(profile, title string) string {
	out := strings.TrimSuffix(strings.TrimSuffix(profile, ".gz"), ".pb") + "-flame.html"
	if b, err := os.ReadFile(out); err == nil && strings.Contains(string(b), flameVersion) {
		return out
	}
	p, _ := pprofFlameHTMLCtx(profile, title, nil)
	return p
}

// pprofFlameHTMLCtx is pprofFlameHTML with the report's access log and code index: it adds the per-handler table
// and returns the handlers (largest first) for the report's one-line summary. Always rebuilds the page.
func pprofFlameHTMLCtx(profile, title string, ctx *flameCtx) (string, []flameHandler) {
	out := strings.TrimSuffix(strings.TrimSuffix(profile, ".gz"), ".pb") + "-flame.html"
	raw, ok := goToolPprofRaw(profile)
	if !ok {
		return "", nil
	}
	unit := "ns"
	if strings.Contains(filepath.Base(profile), "fgprof") {
		unit = "samples"
	}
	stacks := parsePprofStacks(raw)
	hs, note := flameHandlers(stacks, raw, ctx)
	p := buildFlamePayload(stacks, unit)
	if p.Total == 0 {
		return "", nil
	}
	p.Handlers, p.WindowNote = hs, note
	if from, dur, ok := profileWindow(raw); ok {
		p.WinFrom, p.WinTo, p.WinSec = from.UTC().Format("15:04:05"), from.Add(dur).UTC().Format("15:04:05"), dur.Seconds()
	} else {
		p.CountsApprox = true
	}
	if prev, prevTS := prevProfile(profile); prev != "" {
		if praw, ok := goToolPprofRaw(prev); ok {
			if m := flameCum(parsePprofStacks(praw)); len(m) > 0 {
				p.Prev, p.PrevTS = m, prevTS
			}
		}
	}
	return writeFlamePage(out, title, p), hs
}

// flameHandlersLine: "CPU の多いハンドラ: a 34% (12.3 ms/req) · b … · c …" for the report.
func flameHandlersLine(hs []flameHandler) string {
	var total int64
	for _, h := range hs {
		total += h.NS
	}
	var parts []string
	for _, h := range hs {
		if !strings.HasPrefix(h.Fn, "main.") || total == 0 {
			continue
		}
		s := fmt.Sprintf("%s %.1f%%", funcBase(h.Fn), 100*float64(h.NS)/float64(total))
		if h.Endpoint != "" {
			s += " (" + h.Endpoint
			if h.MsPerReq > 0 {
				s += fmt.Sprintf(", %.1f ms/req", h.MsPerReq)
			}
			s += ")"
		}
		parts = append(parts, s)
		if len(parts) == 3 {
			break
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "CPU の多いハンドラ: " + strings.Join(parts, " · ")
}

func writeFlamePage(out, title string, p flamePayload) string {
	data, _ := json.Marshal(p)
	page := strings.ReplaceAll(flameHTML, "__TITLE__", html.EscapeString(title))
	page = strings.ReplaceAll(page, "__VERSION__", flameVersion)
	page = strings.ReplaceAll(page, "__DATA__", strings.ReplaceAll(string(data), "</", `<\/`))
	if err := os.WriteFile(out, []byte(page), 0644); err != nil {
		return ""
	}
	return out
}

func flameTitle(host, kind, profile string) string {
	return fmt.Sprintf("%s · %s · %s", host, kind, filepath.Base(profile))
}

// flameFuncLink: link from the report's pprof top table to the flame page with that function selected.
func flameFuncLink(flameRel, fn string) string {
	var b strings.Builder
	for _, c := range []byte(fn) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("-_.~()*", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return flameRel + "#fn=" + b.String()
}

// pprofTopWithLinks: the `pprof -top` text with each function name linked to the flame page (HTML, escaped).
// Rows look like "  flat  flat%   sum%   cum   cum%  name"; header and summary lines are kept as they are.
func pprofTopWithLinks(top, flameRel string) string {
	var b strings.Builder
	for _, ln := range strings.Split(top, "\n") {
		f := strings.Fields(ln)
		if flameRel == "" || len(f) < 6 || !strings.HasSuffix(f[1], "%") || !strings.HasSuffix(f[4], "%") {
			b.WriteString(html.EscapeString(ln) + "\n")
			continue
		}
		i := strings.LastIndex(ln, f[5])
		fn := strings.TrimSpace(ln[i:])
		b.WriteString(html.EscapeString(ln[:i]) + `<a href="` + html.EscapeString(flameFuncLink(flameRel, fn)) +
			`" target="_blank" title="flame graph でこの関数をハイライト">` + html.EscapeString(fn) + "</a>\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

const flameHTML = `<!DOCTYPE html><html><head><meta charset="utf-8"><title>flame · __TITLE__</title><!-- __VERSION__ -->
<style>
body{font-family:-apple-system,"Hiragino Sans",sans-serif;margin:0;background:#fff;color:#111;font-size:13px}
header{padding:8px 14px;background:#0f766e;color:#fff;display:flex;flex-wrap:wrap;gap:10px;align-items:center}
header input{padding:3px 6px;border:0;border-radius:4px;min-width:200px}
header button{font-size:12px;padding:3px 8px;border:0;border-radius:4px;cursor:pointer;background:#ccfbf1}
header button.on{background:#fde047;font-weight:700}
.bar{display:flex;flex-wrap:wrap;gap:6px;align-items:center;padding:6px 14px;background:#f0fdfa;border-bottom:1px solid #99f6e4}
.bar .k{color:#0f766e;font-weight:700;margin-right:2px}
.bar button{padding:2px 8px;border:1px solid #5eead4;border-radius:999px;background:#fff;cursor:pointer;font-size:12px}
.bar button.on{background:#0f766e;color:#fff}
.wrap{display:grid;grid-template-columns:var(--side,470px) 8px minmax(0,1fr);gap:6px;padding:8px 14px 40px}
.grip{cursor:col-resize;border-radius:4px;background:linear-gradient(90deg,transparent 3px,#99f6e4 3px,#99f6e4 5px,transparent 5px);touch-action:none}.grip:hover,.grip.drag{background:#5eead4}
.side{min-width:0}
table{border-collapse:collapse;width:100%;font-size:12px}th,td{padding:2px 6px;border-bottom:1px solid #eee;text-align:right;white-space:nowrap}
th{background:#f8fafc;cursor:pointer}td.fn{text-align:left;max-width:260px;overflow:hidden;text-overflow:ellipsis}td.ep{text-align:left;max-width:120px;overflow:hidden;text-overflow:ellipsis;color:#555}#htbl td.fn{max-width:135px}#htbl th,#htbl td{padding:2px 4px}
tr.sel td{background:#fef9c3;font-weight:700}tbody tr{cursor:pointer}tbody tr:hover td{background:#f1f5f9}
.pct{display:inline-block;height:8px;background:#f97316;border-radius:2px;vertical-align:middle;margin-right:4px}
.up{color:#dc2626}.down{color:#2563eb}
h3{margin:8px 0 4px;font-size:13px}.side{max-height:calc(100vh - 120px);overflow:auto}
#sel{padding:6px 8px;background:#fef9c3;border-radius:6px;margin-bottom:6px;display:none}
#g{position:relative}
.f{position:absolute;height:17px;line-height:17px;font-size:11px;padding:0 3px;box-sizing:border-box;overflow:hidden;white-space:nowrap;border:1px solid #fff;border-radius:2px;cursor:pointer}
.f:hover{outline:2px solid #111;z-index:2}.f.dim{opacity:.22}.f.hit{outline:2px solid #dc2626;z-index:1}
#tip{position:fixed;bottom:0;left:0;right:0;background:#111;color:#fff;font-size:12px;padding:6px 14px;font-family:ui-monospace,monospace}
.legend{font-size:11px;color:#555;font-weight:400}
.wnote{font-size:11px;color:#0f766e;background:#f0fdfa;border-left:3px solid #5eead4;padding:3px 6px;margin:0 0 4px}
header .win{font-size:11px;opacity:.85}
.wrap.noside{grid-template-columns:minmax(0,1fr)}.wrap.noside .side,.wrap.noside .grip{display:none}
#crumbs{padding:4px 0 6px;font-size:13px}#crumbs a{color:#0f766e}#crumbs button{font-size:12px;padding:2px 8px;border:1px solid #0f766e;border-radius:4px;background:#fff;cursor:pointer;margin-right:6px}
</style></head><body>
<header><b>__TITLE__</b><span class="win" id="win"></span><button id="bSide" title="表を隠す / 出す（キー t）">◀ 表を隠す</button><input id="q" placeholder="検索（正規表現） 例: sha256|sql"><button onclick="resetAll()">reset</button>
<button id="bFold" onclick="toggle('fold')" title="ハンドラ（middleware 以外の最初の main.*）より上の net/http・echo・middleware を省く">枠組みをたたむ</button><button id="bDiff" onclick="toggle('diff')" style="display:none">前の run との差分</button>
<span id="info"></span></header>
<div class="bar"><span class="k">除外:</span><span id="presets"></span><input id="xq" placeholder="除外する関数（正規表現）" style="min-width:220px;padding:2px 6px">
<span class="legend">除外した関数の時間は呼び出し元に寄せる · 箱をクリック = ズーム（⌘/Alt+クリック = その関数を選ぶ、Esc / ← 戻る / ブラウザの戻る = 1 段戻る） · 表の行 = その関数をハイライト</span></div>
<div class="wrap"><div class="side"><div id="sel"></div>
<div id="hwrap"><h3><span id="htitle">直下の内訳</span> <span class="legend" id="hnote"></span></h3><div class="wnote" id="wnote"></div><table id="htbl"></table></div>
<h3><span id="scope">関数ごとの合計（呼び出し経路を問わない）</span> <span class="legend" id="tblnote"></span></h3><table id="tbl"></table>
<div id="sandwich"></div></div><div class="grip" id="grip" title="ドラッグで幅を変える（ダブルクリックで既定）"></div>
<div style="min-width:0"><div id="crumbs"></div><div id="g"></div></div></div><div id="tip">&nbsp;</div>
<script>
const D=__DATA__;
const PRESETS=[["runtime","^runtime\\."],["syscall","^(syscall|internal/runtime/syscall|internal/poll)\\."],["net/http","^net/http\\."],["database/sql","^database/sql\\."],["GC","^runtime\\.(gc|mark|scan|bgsweep|sweep|mallocgc)"]];
const HFN=new Set((D.handlers||[]).map(h=>h.fn));
const S={fold:false,diff:false,on:new Set(),sel:null,zoom:null,sort:"self"};
const ROW=17,SEP="\u001f";
const esc=s=>String(s).replace(/[&<>"']/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));
const short=s=>s.replace(/^.*\//,"");
const color=s=>{let h=0;for(const c of s)h=(h*31+c.charCodeAt(0))>>>0;return "hsl("+(20+h%40)+",80%,"+(60+h%20)+"%)";};
const pctOf=(v,t)=>(100*v/(t||1)).toFixed(v*1000<t?2:1)+"%";
const pct=v=>pctOf(v,D.total);
function exRe(){const parts=[...S.on].map(i=>PRESETS[i][1]);const x=document.getElementById("xq").value.trim();if(x)parts.push(x);try{return parts.length?new RegExp(parts.join("|")):null}catch(e){return null}}
// fold: start every stack at its handler (the frame the handler table uses), else at the first non-middleware main.*
function foldAt(fr){let j=fr.findIndex(f=>HFN.has(f));if(j<0)j=fr.findIndex(f=>/^main\./.test(f)&&!/middleware|measure|sqltag/i.test(f));return j;}
function stacks(){const ex=exRe();const out=[];
  for(let i=0;i<D.stacks.length;i++){let fr=D.stacks[i].map(k=>D.names[k]);
    if(S.fold){const j=foldAt(fr);if(j>0)fr=fr.slice(j);}
    if(ex){const k=fr.filter(f=>!ex.test(f));fr=k.length?k:["("+short(fr[fr.length-1])+" など除外分)"];}
    out.push([fr,D.values[i]]);}
  return out;}
// the stacks under the zoomed box, each cut to start at it (the range the tables are computed over)
function scoped(st){const z=S.zoom;if(!z||!z.length)return st;const out=[];
  for(const [fr,v] of st){if(fr.length<z.length)continue;let ok=true;for(let i=0;i<z.length;i++)if(fr[i]!==z[i]){ok=false;break;}if(ok)out.push([fr.slice(z.length-1),v]);}
  return out;}
function tree(st){const root={n:"all",v:0,c:new Map()};for(const [fr,v] of st){root.v+=v;let n=root;for(const f of fr){let c=n.c.get(f);if(!c){c={n:f,v:0,c:new Map()};n.c.set(f,c);}c.v+=v;n=c;}}return root;}
function funcs(st){const m=new Map();for(const [fr,v] of st){const seen=new Set();for(const f of fr){let r=m.get(f);if(!r){r={n:f,self:0,cum:0};m.set(f,r);}if(!seen.has(f)){seen.add(f);r.cum+=v;}}m.get(fr[fr.length-1]).self+=v;}return [...m.values()];}
const sum=st=>st.reduce((a,[,v])=>a+v,0);
function sandwich(st,fn,rt){const callers=new Map(),callees=new Map();let tot=0;
  for(const [fr,v] of st){const i=fr.indexOf(fn);if(i<0)continue;tot+=v;const a=i>0?fr[i-1]:"(範囲の先頭)";callers.set(a,(callers.get(a)||0)+v);
    const b=i<fr.length-1?fr[i+1]:"(self)";callees.set(b,(callees.get(b)||0)+v);}
  if(!tot)return "";
  const two=S.zoom&&S.zoom.length;
  const head=k=>"<thead><tr><th style='text-align:left'>"+k+"</th>"+(two?"<th>範囲の</th>":"")+"<th>全体の</th><th>この関数の</th></tr></thead>";
  const rows=m=>[...m.entries()].sort((a,b)=>b[1]-a[1]).slice(0,12).map(([k,v])=>"<tr data-fn=\""+esc(k)+"\"><td class=fn title=\""+esc(k)+"\">"+esc(short(k))+"</td>"+(two?"<td>"+pctOf(v,rt)+"</td>":"")+"<td>"+pct(v)+"</td><td>"+(100*v/tot).toFixed(1)+"%</td></tr>").join("");
  return "<h3>呼び出し元（"+esc(short(fn))+" を呼んでいる関数）</h3><table class=sw>"+head("caller")+"<tbody>"+rows(callers)+"</tbody></table>"+
         "<h3>呼び出し先（"+esc(short(fn))+" の中で時間を使っている先）</h3><table class=sw>"+head("callee")+"<tbody>"+rows(callees)+"</tbody></table>";}
let CUM=new Map();
function diffColor(n){const now=CUM.get(n)||0,was=(D.prev||{})[n]||0,d=now-was;if(Math.abs(d)<0.002)return "#e5e7eb";
  const a=Math.min(1,Math.abs(d)/0.1);return d>0?"rgba(220,38,38,"+(0.25+0.75*a)+")":"rgba(37,99,235,"+(0.25+0.75*a)+")";}
// --- history: every zoom is a hash entry (#zoom=a␟b&fold=1&fn=x) so the browser's back button steps out one level
function hashOf(){const p=[];if(S.zoom&&S.zoom.length)p.push("zoom="+encodeURIComponent(S.zoom.join(SEP)));if(S.fold)p.push("fold=1");if(S.sel)p.push("fn="+encodeURIComponent(S.sel));return p.length?"#"+p.join("&"):"#";}
function readHash(){const h=location.hash.replace(/^#/,"");const q={};for(const kv of h.split("&")){const i=kv.indexOf("=");if(i>0)q[kv.slice(0,i)]=decodeURIComponent(kv.slice(i+1));}
  S.zoom=q.zoom?q.zoom.split(SEP):null;S.fold=q.fold==="1";S.sel=q.fn||null;}
// zoomTo pushes a history entry (n = how many of ours are below it); back() pops ours with history.back() so the
// browser's back button and Esc / ← 戻る walk the same stack; with none of ours left (opened via a #zoom link) it replaces
function zoomTo(path){S.zoom=(path&&path.length)?path:null;const h=hashOf();if(location.hash!==h)history.pushState({n:((history.state&&history.state.n)||0)+1},"",h);render();}
function setView(path){S.zoom=(path&&path.length)?path:null;history.replaceState(history.state,"",hashOf());render();}
function back(){if(!(S.zoom&&S.zoom.length))return;if(history.state&&history.state.n>0){history.back();return;}setView(S.zoom.length>1?S.zoom.slice(0,-1):null);}
function render(){const all=stacks(),root=tree(all);const st=scoped(all);const rt=sum(st);const fs=funcs(st);
  CUM=new Map(funcs(all).map(r=>[r.n,r.cum/D.total]));
  const zoomed=!!(S.zoom&&S.zoom.length);
  // direct children of the selected (zoomed) box, largest first; endpoint / count / ms per request when a child is
  // a handler (D.handlers, counted in the profile window)
  let zn=root;if(zoomed){for(const p of S.zoom){const c=zn.c.get(p);if(!c){zn=root;break;}zn=c;}}
  const kids=[...zn.c.values()].sort((a,b)=>b.v-a.v),HB=new Map((D.handlers||[]).map(h=>[h.fn,h]));
  const withEp=kids.some(k=>HB.get(k.n)&&HB.get(k.n).ep);
  document.getElementById("hwrap").style.display=kids.length?"":"none";
  document.getElementById("htitle").innerHTML="直下の内訳: <b>"+esc(zoomed?short(S.zoom[S.zoom.length-1]):"all")+"</b>";
  if(kids.length){let h="<thead><tr><th style='text-align:left'>子の関数</th><th>全体の</th>"+(zoomed?"<th title='選択中のブロックの中の %'>選択中の</th>":"")+"<th title='CPU 秒'>秒</th>"+(withEp?"<th style='text-align:left'>endpoint</th><th title='"+esc(D.window_note||"")+"'>"+(D.win_sec?"件数<br><span class=legend>窓 "+Math.round(D.win_sec)+" 秒</span>":"件数<br><span class=legend>≈ 近似</span>")+"</th><th title='その窓の CPU 秒 ÷ 件数'>ms/件</th>":"")+"</tr></thead><tbody>";
    for(const k of kids.slice(0,20)){const hb=HB.get(k.n),ep=hb&&hb.ep,cnt=ep?hb.count:0;
      h+="<tr data-c=\""+esc(k.n)+"\"><td class=fn title=\""+esc(k.n)+"\"><span class=pct style='width:"+(40*k.v/(kids[0].v||1)).toFixed(0)+"px'></span>"+esc(short(k.n).replace(/^main\./,""))+"</td><td>"+pct(k.v)+"</td>"+(zoomed?"<td>"+pctOf(k.v,zn.v)+"</td>":"")+"<td>"+(D.unit==="ns"?(k.v/1e9).toFixed(2):"-")+"</td>"+
        (withEp?"<td class=ep title=\""+esc(ep||"")+"\">"+esc(ep||"")+"</td><td>"+(cnt?(hb.approx?"≈":"")+cnt:"")+"</td><td>"+(cnt&&D.unit==="ns"?(k.v/1e6/cnt).toFixed(1):"")+"</td>":"")+"</tr>";}
    document.getElementById("htbl").innerHTML=h+"</tbody>";
    document.getElementById("hnote").textContent="行 = その子にズーム";
    const wn=document.getElementById("wnote");wn.textContent=withEp&&D.window_note?D.window_note:"";wn.style.display=wn.textContent?"":"none";}
  // function table (range)
  fs.sort((a,b)=>S.sort==="self"?b.self-a.self:b.cum-a.cum);const top=fs.slice(0,20);const mx=Math.max(1,...top.map(r=>r[S.sort]));
  const hasPrev=!!(D.prev&&Object.keys(D.prev).length);
  document.getElementById("scope").innerHTML=zoomed?"範囲: <b>"+esc(short(S.zoom[S.zoom.length-1]))+"</b>（全体の "+pct(rt)+"）の中の関数":"関数ごとの合計（呼び出し経路を問わない）";
  let h="<thead><tr><th style='text-align:left'>関数</th><th onclick=\"S.sort='self';render()\">self"+(S.sort==="self"?" ▼":"")+"</th><th onclick=\"S.sort='cum';render()\">cum"+(S.sort==="cum"?" ▼":"")+"</th>"+(zoomed?"<th title='範囲の中の cum'>範囲の</th>":"")+(hasPrev&&!zoomed?"<th title='前の run "+esc(D.prev_ts||"")+" の cum との差（ポイント）'>Δcum</th>":"")+"</tr></thead><tbody>";
  for(const r of top){const d=hasPrev?(r.cum/D.total-(D.prev[r.n]||0)):0;
    h+="<tr class='"+(S.sel===r.n?"sel":"")+"' data-fn=\""+esc(r.n)+"\"><td class=fn title=\""+esc(r.n)+"\"><span class=pct style='width:"+(40*r[S.sort]/mx).toFixed(0)+"px'></span>"+esc(short(r.n))+"</td><td>"+pct(r.self)+"</td><td>"+pct(r.cum)+"</td>"+
       (zoomed?"<td>"+pctOf(r.cum,rt)+"</td>":"")+(hasPrev&&!zoomed?"<td class='"+(d>0.002?"up":d<-0.002?"down":"")+"'>"+(d>=0?"+":"")+(100*d).toFixed(1)+"</td>":"")+"</tr>";}
  document.getElementById("tbl").innerHTML=h+"</tbody>";
  document.getElementById("tblnote").textContent="上位 20 · self / cum は全体の %"+(zoomed?"、「範囲の」= ズームした箱の中の %":"")+"（見出しで並べ替え）"+(hasPrev&&!zoomed?" · Δ = 前の run "+(D.prev_ts||"")+" との差":"");
  const sel=document.getElementById("sel");
  if(S.sel){const r=fs.find(x=>x.n===S.sel);sel.style.display="block";
    sel.innerHTML="<b>"+esc(S.sel)+"</b><br>"+(zoomed?"範囲の <b>"+(r?pctOf(r.cum,rt):"0%")+"</b> · ":"")+"全体の <b>"+(r?pct(r.cum):"0%")+"</b>（cum: どこから呼ばれても合計）· self "+(r?pct(r.self):"0%")+" · <a href='#' id='unsel'>解除</a>";
    document.getElementById("unsel").onclick=e=>{e.preventDefault();select(null);};
    document.getElementById("sandwich").innerHTML=sandwich(st,S.sel,rt);}
  else{sel.style.display="none";document.getElementById("sandwich").innerHTML="";}
  document.querySelectorAll("#tbl tbody tr, table.sw tbody tr").forEach(tr=>tr.onclick=()=>select(tr.dataset.fn));
  document.querySelectorAll("#htbl tbody tr").forEach(tr=>tr.onclick=()=>zoomTo((zoomed?S.zoom:[]).concat([tr.dataset.c])));
  // breadcrumbs
  const cr=document.getElementById("crumbs");
  if(zoomed){let c="<button id='bBack' title='1 段戻る（Esc / ブラウザの戻る）'>← 戻る</button> <a href='#' data-i='-1'>all</a>";
    S.zoom.forEach((f,i)=>{c+=" › "+(i===S.zoom.length-1?"<b title=\""+esc(f)+"\">"+esc(short(f))+"</b>":"<a href='#' data-i='"+i+"' title=\""+esc(f)+"\">"+esc(short(f))+"</a>");});
    cr.innerHTML=c+" <span class=legend>（全体の "+pct(rt)+"）</span>";cr.style.display="";
    document.getElementById("bBack").onclick=back;
    cr.querySelectorAll("a[data-i]").forEach(a=>a.onclick=e=>{e.preventDefault();const i=+a.dataset.i;zoomTo(i<0?null:S.zoom.slice(0,i+1));});}
  else{cr.innerHTML="";cr.style.display="none";}
  // flame
  const g=document.getElementById("g");g.innerHTML="";
  let z=root;if(zoomed){for(const p of S.zoom){const c=z.c.get(p);if(!c){S.zoom=null;z=root;break;}z=c;}}
  const total=z.v||1,w=Math.max(500,g.clientWidth||900);
  const q=document.getElementById("q").value.trim();let re=null;try{re=q?new RegExp(q,"i"):null}catch(e){}
  const isHit=n=>(re&&re.test(n))||(S.sel&&n===S.sel);
  let hits=0,hitv=0,maxd=0;
  const draw=(n,x,d,path)=>{const pw=n.v/total*w;if(pw<1)return;maxd=Math.max(maxd,d);const el=document.createElement("div");el.className="f";
    const hit=isHit(n.n);if(hit){hits++;if(![...n.c.values()].some(c=>isHit(c.n)))hitv+=n.v;}
    if((re||S.sel)&&!hit)el.classList.add("dim");if(hit)el.classList.add("hit");
    el.style.left=x+"px";el.style.top=(d*ROW)+"px";el.style.width=Math.max(1,pw-1)+"px";el.style.background=S.diff&&D.prev?diffColor(n.n):color(n.n);
    el.textContent=pw>30?short(n.n):"";
    el.onmouseenter=()=>{let t=n.n+"  "+pct(n.v)+" of all · "+(100*n.v/total).toFixed(2)+"% of view";
      if(D.prev){t+=" · cum "+(100*(CUM.get(n.n)||0)).toFixed(1)+"%（前の run "+(100*((D.prev[n.n])||0)).toFixed(1)+"%）";}document.getElementById("tip").textContent=t;};
    el.onclick=e=>{e.stopPropagation();if(e.altKey||e.metaKey){select(n.n);return;}if(d===0){back();return;}zoomTo(path);};
    g.appendChild(el);let cx=x;for(const c of n.c.values()){draw(c,cx,d+1,path.concat([c.n]));cx+=c.v/total*w;}};
  draw(z,0,0,zoomed?S.zoom:[]);g.style.height=((maxd+1)*ROW+10)+"px";
  document.getElementById("info").textContent=(re||S.sel)?(hits+" 個 · 全体の "+(100*hitv/D.total).toFixed(1)+"%"):"";
  document.getElementById("bFold").className=S.fold?"on":"";document.getElementById("bDiff").className=S.diff?"on":"";
  document.getElementById("presets").innerHTML=PRESETS.map((p,i)=>"<button class='"+(S.on.has(i)?"on":"")+"' data-p='"+i+"'>"+p[0]+"</button>").join(" ");
  document.querySelectorAll("#presets button").forEach(b=>b.onclick=()=>preset(+b.dataset.p));}
function select(fn){S.sel=(fn&&S.sel!==fn)?fn:null;history.replaceState(null,"",hashOf());render();}
function preset(i){S.on.has(i)?S.on.delete(i):S.on.add(i);setView(null);}
function toggle(k){S[k]=!S[k];if(k==="fold"){setView(null);return;}render();}
function resetAll(){S.zoom=null;S.sel=null;S.on.clear();S.fold=false;S.diff=false;document.getElementById("q").value="";document.getElementById("xq").value="";history.replaceState(null,"",location.pathname);render();}
if(D.prev&&Object.keys(D.prev).length){const b=document.getElementById("bDiff");b.style.display="";b.title="赤 = 前の run（"+(D.prev_ts||"")+"）より cum が増えた関数、青 = 減った関数";}
window.addEventListener("popstate",()=>{readHash();render();});
document.addEventListener("keydown",e=>{if(e.key==="Escape"&&document.activeElement.tagName!=="INPUT"){back();}});
readHash();
document.getElementById("q").addEventListener("input",render);document.getElementById("xq").addEventListener("input",()=>setView(null));
// side / flame splitter: drag the grip (260 px … 70% of the window), double-click = default; the width is remembered
const SIDE_KEY="isumeasure.flame.side",SIDE_DEF=470;
const clampSide=w=>Math.round(Math.max(260,Math.min(window.innerWidth*0.7,w)));
function setSide(w,save){const wrap=document.querySelector(".wrap");wrap.style.setProperty("--side",clampSide(w)+"px");if(save){try{localStorage.setItem(SIDE_KEY,String(clampSide(w)))}catch(e){}}}
(function(){let saved=null;try{saved=+localStorage.getItem(SIDE_KEY)}catch(e){}setSide(saved||SIDE_DEF,false);
  const g=document.getElementById("grip"),side=document.querySelector(".side");let x0=0,w0=0,raf=0;
  g.addEventListener("pointerdown",e=>{x0=e.clientX;w0=side.getBoundingClientRect().width;g.setPointerCapture(e.pointerId);g.classList.add("drag");e.preventDefault();});
  g.addEventListener("pointermove",e=>{if(!g.classList.contains("drag"))return;setSide(w0+e.clientX-x0,false);if(!raf)raf=requestAnimationFrame(()=>{raf=0;render();});});
  const end=e=>{if(!g.classList.contains("drag"))return;g.classList.remove("drag");setSide(side.getBoundingClientRect().width,true);render();};
  g.addEventListener("pointerup",end);g.addEventListener("pointercancel",end);
  g.addEventListener("dblclick",()=>{setSide(SIDE_DEF,true);render();});})();
// hide / show the whole side column (button or key t); kept apart from the splitter width, which is restored on show
const HIDE_KEY="isumeasure.flame.sidehidden";
function setHidden(h,save){document.querySelector(".wrap").classList.toggle("noside",h);document.getElementById("bSide").textContent=h?"▶ 表を出す":"◀ 表を隠す";
  if(save){try{localStorage.setItem(HIDE_KEY,h?"1":"0")}catch(e){}}render();}
(function(){let h=false;try{h=localStorage.getItem(HIDE_KEY)==="1"}catch(e){}document.getElementById("bSide").onclick=()=>setHidden(!document.querySelector(".wrap").classList.contains("noside"),true);
  document.addEventListener("keydown",e=>{if(e.key==="t"&&!e.metaKey&&!e.ctrlKey&&!e.altKey&&document.activeElement.tagName!=="INPUT")setHidden(!document.querySelector(".wrap").classList.contains("noside"),true);});
  if(h)setHidden(true,false);
  if(D.win_from)document.getElementById("win").textContent="CPU 窓 "+D.win_from+"〜"+D.win_to+" UTC（"+Math.round(D.win_sec)+" 秒）";else if(D.counts_approx)document.getElementById("win").textContent="CPU 窓: 不明";})();
window.addEventListener("resize",()=>{const sd=document.querySelector(".side");if(!document.querySelector(".wrap").classList.contains("noside"))setSide(sd.getBoundingClientRect().width,false);render();});render();
</script></body></html>`
