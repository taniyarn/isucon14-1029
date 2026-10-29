package main

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// score.go: Next actions ranked by expected effect instead of rule order.
//
//	score = share × bottleneck weight × score-path weight
//	  share       the time the action is about: an endpoint's share of request time; a query's share of SQL time × how
//	              busy the DB box is; a CPU hot spot's share of the app's CPU profile × how busy the app box is.
//	              Actions without a measured share keep a base from their rule (P1 12, P2 5, P3 1).
//	  bottleneck  1.5 when the action works on the bottleneck the Verdict found (SQL on a DB-CPU-bound host, CPU hot
//	              spots / endpoints on an app-CPU-bound host, endpoints / polling when waiting, lock work when
//	              lock-bound), 0.5 when it works on the other side, else 1.
//	  score path  1.5 when it touches the loop that earns points (alp.yml score_post + the Flows score loops: the
//	              endpoint itself, or a query tagged with one of those endpoints, or a hot spot called from one).
//	5xx always come first. Priority bands: P1 ≥ 10, P2 ≥ 3, else P3.

// scoreCtx: what one host's Verdict and the run's Flows say, for scoreActions.
type scoreCtx struct {
	kind     string          // verdict kind of the host the actions came from
	dbBusy   float64         // mysqld avg ÷ ceiling on that host (0..1; 0 = unknown / no DB here)
	appBusy  float64         // app processes avg ÷ ceiling on that host
	pathEps  map[string]bool // paramKey(endpoint) on the score path
	pathSQLs map[string]bool // normalized queries tagged with a score-path endpoint (sqltag)
	pathFns  map[string]bool // handler funcs of score-path endpoints (code index)
}

var sqlKinds = map[string]bool{"index": true, "n1": true, "aggregate": true, "slowidx": true}

func bottleneckWeight(kind, actKind string) float64 {
	switch kind {
	case "db-cpu":
		switch {
		case sqlKinds[actKind]:
			return 1.5
		case actKind == "hotspot":
			return 0.5
		}
	case "app-cpu":
		switch {
		case actKind == "hotspot" || actKind == "endpoint":
			return 1.5
		case sqlKinds[actKind]:
			return 0.5
		}
	case "wait":
		if actKind == "endpoint" || actKind == "polling" {
			return 1.5
		}
	case "lock":
		if actKind == "lock" || actKind == "aggregate" {
			return 1.5
		}
	case "io":
		if actKind == "io" {
			return 1.5
		}
	}
	return 1
}

func clamp01(x float64) float64 {
	switch {
	case x < 0:
		return 0
	case x > 1:
		return 1
	}
	return x
}

// scoreActions fills score / why / priority and sorts (5xx first, then score). Polling-interval fields marked P1 by
// applyPollFields keep at least the P1 band.
func scoreActions(acts []action, c scoreCtx) {
	for i := range acts {
		a := &acts[i]
		if a.kind == "errors" {
			a.score, a.why, a.priority = 1000, "5xx first", "P1"
			continue
		}
		share, src := a.share, ""
		switch {
		case a.share > 0 && sqlKinds[a.kind]:
			busy := clamp01(c.dbBusy)
			if busy < 0.25 { // unknown or idle DB box: do not erase the query's own share
				busy = 0.25
			}
			share, src = a.share*busy, fmt.Sprintf("%.0f%% of SQL × DB busy %.2f", a.share, busy)
		case a.share > 0 && a.kind == "hotspot":
			busy := clamp01(c.appBusy)
			if busy < 0.25 {
				busy = 0.25
			}
			share, src = a.share*busy, fmt.Sprintf("%.0f%% of app CPU × app busy %.2f", a.share, busy)
		case a.share > 0:
			src = fmt.Sprintf("%.0f%% of request time", a.share)
		default:
			share, src = map[string]float64{"P1": 12, "P2": 5, "P3": 1}[a.priority], "rule "+a.priority
		}
		bw := bottleneckWeight(c.kind, a.kind)
		pw := 1.0
		if onScorePath(*a, c) {
			pw = 1.5
		}
		a.score = share * bw * pw
		parts := []string{src}
		if bw != 1 {
			parts = append(parts, fmt.Sprintf("bottleneck ×%.1f", bw))
		}
		if pw != 1 {
			parts = append(parts, "score path ×1.5")
		}
		a.why = strings.Join(parts, " · ")
		band := "P3"
		switch {
		case a.score >= 10:
			band = "P1"
		case a.score >= 3:
			band = "P2"
		}
		if a.pinned && a.priority < band { // a rule that knows better (tiny retry_after …) keeps its band
			band = a.priority
		}
		a.priority = band
	}
	sort.SliceStable(acts, func(i, j int) bool { return acts[i].score > acts[j].score })
}

func onScorePath(a action, c scoreCtx) bool {
	if a.sql != "" && c.pathSQLs[a.sql] {
		return true
	}
	if a.kind == "endpoint" || a.kind == "polling" {
		for ep := range c.pathEps {
			if strings.Contains(paramKey(a.title), ep) {
				return true
			}
		}
	}
	if fn := strings.SplitN(a.where, "(", 2)[0]; fn != "" && c.pathFns[strings.TrimSpace(fn)] {
		return true
	}
	for _, fn := range a.callers {
		if c.pathFns[fn] {
			return true
		}
	}
	return false
}

// ---------- CPU hot spots from the profile ----------

type hotFunc struct {
	name      string
	self, cum float64 // % of the profile
	callers   []string
	callerPct []float64
}

// hotspotSkip: frames that are plumbing, not work you can remove (the runtime, the HTTP server, the framework, the
// measurement patch). Handlers are left out too: the endpoint actions already name them.
func hotspotSkip(fn string) bool {
	for _, p := range []string{"runtime.", "runtime/", "net/http.", "github.com/labstack/", "syscall.", "internal/", "main.main", "main.measure", "github.com/go-chi/", "net.", "bufio.", "sync.", "context."} {
		if strings.HasPrefix(fn, p) {
			return true
		}
	}
	short := fn[strings.LastIndex(fn, ".")+1:]
	return strings.HasSuffix(short, "Handler") || strings.HasSuffix(short, "Middleware") || strings.Contains(fn, ".func")
}

// hotFuncs folds a profile tree into per-function self / cum (each stack counted once per function) and, for every
// function, the nearest main.* caller weighted by samples.
func hotFuncs(root *flameNode) []hotFunc {
	if root == nil || root.Value == 0 {
		return nil
	}
	total := float64(root.Value)
	self, cum := map[string]float64{}, map[string]float64{}
	callers := map[string]map[string]float64{}
	var walk func(n *flameNode, onPath map[string]int, mains []string)
	walk = func(n *flameNode, onPath map[string]int, mains []string) {
		kids := 0.0
		for _, c := range n.Children {
			kids += float64(c.Value)
		}
		self[n.Name] += float64(n.Value) - kids
		if onPath[n.Name] == 0 { // topmost occurrence on this path: count its subtree once
			cum[n.Name] += float64(n.Value)
			if len(mains) > 0 {
				m := callers[n.Name]
				if m == nil {
					m = map[string]float64{}
					callers[n.Name] = m
				}
				m[mains[len(mains)-1]] += float64(n.Value)
			}
		}
		onPath[n.Name]++
		nm := mains
		if strings.HasPrefix(n.Name, "main.") && !strings.HasPrefix(n.Name, "main.main") && !strings.HasPrefix(n.Name, "main.measure") && !strings.Contains(n.Name, ".func") {
			nm = append(append([]string{}, mains...), strings.TrimPrefix(n.Name, "main."))
		}
		for _, c := range n.Children {
			walk(c, onPath, nm)
		}
		onPath[n.Name]--
	}
	for _, c := range root.Children {
		walk(c, map[string]int{}, nil)
	}
	var out []hotFunc
	for fn := range cum {
		hf := hotFunc{name: fn, self: 100 * self[fn] / total, cum: 100 * cum[fn] / total}
		type kv struct {
			k string
			v float64
		}
		var cs []kv
		for k, v := range callers[fn] {
			if "main."+k != fn {
				cs = append(cs, kv{k, v})
			}
		}
		sort.Slice(cs, func(i, j int) bool { return cs[i].v > cs[j].v })
		for i, x := range cs {
			if i >= 3 {
				break
			}
			hf.callers = append(hf.callers, x.k)
			hf.callerPct = append(hf.callerPct, 100*x.v/total)
		}
		out = append(out, hf)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].self+out[i].cum > out[j].self+out[j].cum })
	return out
}

// hotspotActions: at most 3 functions that are real work (not hotspotSkip): first the ones with self ≥ 10% of the
// app's CPU profile (named with their main.* callers: sha256.block ← fillUserResponse is one action), then main.*
// functions with cum ≥ 10% that are not already named as such a caller.
func hotspotActions(profile string) []action {
	goBin := ""
	for _, c := range []string{"go", "/usr/local/go/bin/go"} {
		if p, err := exec.LookPath(c); err == nil {
			goBin = p
			break
		}
	}
	if goBin == "" || profile == "" {
		return nil
	}
	raw, err := exec.Command(goBin, "tool", "pprof", "-raw", profile).Output()
	if err != nil {
		return nil
	}
	return hotspotActionsFrom(hotFuncs(parsePprofRaw(string(raw))))
}

func hotspotActionsFrom(fs []hotFunc) []action {
	var acts []action
	named := map[string]bool{} // main.* funcs already named as the caller of a picked hot spot
	pick := func(f hotFunc, share float64) {
		var cs []string
		for i, c := range f.callers {
			cs = append(cs, fmt.Sprintf("%s() %.0f%%", c, f.callerPct[i]))
			named[c] = true
		}
		where := ""
		if len(f.callers) > 0 {
			where = f.callers[0] + "()"
		}
		if strings.HasPrefix(f.name, "main.") {
			where = strings.TrimPrefix(f.name, "main.") + "()"
		}
		a := mk("P2", fmt.Sprintf("CPU hot spot: %s (%.0f%% of the app's CPU)", f.name, share),
			fmt.Sprintf("self %.0f%%, cum %.0f%% of the CPU profile. Called from: %s. Do the work less often (compute once on write and store it, cache it, skip it when the result is not used) or with a cheaper call.", f.self, f.cum, ternary(len(cs) > 0, strings.Join(cs, ", "), "-")), "", where)
		a.share, a.callers, a.kind = share, f.callers, "hotspot"
		acts = append(acts, a)
	}
	// 1) leaves that burn the CPU themselves (self ≥ 10%): the real cost, named with their main.* callers
	bySelf := append([]hotFunc{}, fs...)
	sort.Slice(bySelf, func(i, j int) bool { return bySelf[i].self > bySelf[j].self })
	for _, f := range bySelf {
		if len(acts) >= 3 || f.self < 10 {
			break
		}
		if !hotspotSkip(f.name) {
			pick(f, f.self)
		}
	}
	// 2) app functions whose subtree is heavy (cum ≥ 10%) and that no picked hot spot already names as its caller
	byCum := append([]hotFunc{}, fs...)
	sort.Slice(byCum, func(i, j int) bool { return byCum[i].cum > byCum[j].cum })
	for _, f := range byCum {
		if len(acts) >= 3 || f.cum < 10 {
			break
		}
		if !strings.HasPrefix(f.name, "main.") || hotspotSkip(f.name) || named[strings.TrimPrefix(f.name, "main.")] || f.self >= 10 {
			continue
		}
		pick(f, f.cum)
	}
	return acts
}
