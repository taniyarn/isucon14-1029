package main

import (
	"fmt"
	"html"
	"sort"
	"strings"
	"time"
)

// Flows: what the bench's clients do, from the access log (session cookie = one client). Runs of the same
// endpoint (an icon list, polling) are collapsed to one step, so the flow reads as "screen actions".
//   - transitions: A → B with the share of A's next steps and the think time
//   - score loops: the path between two consecutive score_post requests of one client (alp.yml `score_post:`),
//     with the cycle time and the response time of each step — shorten the longest step to earn more
//   - drop-off: the last step of clients that stopped well before the end of the load (status class)
// Everything here is an inference about the bench (推測), like Bench shape.

type flowStep struct {
	ep     string
	status int
	start  time.Time
	end    time.Time
	rt     float64 // response time of the (first) request of the step
	lastRT float64 // response time of the last request collapsed into this step
	n      int     // requests collapsed into this step
}

type flowTrans struct {
	from, to string
	n        int
	share    float64 // n / all steps that left `from`
	gaps     []float64
}

type flowLoop struct {
	steps   []string
	n       int
	cycles  []float64
	stepRT  [][]float64 // per step position: response time
	stepGap [][]float64 // per step position: wait after this step's response until the next step starts (think time, other work)
}

type flowDrop struct {
	ep, status string
	reason     string    // error | slow | end | unknown (dropReason)
	terminal   float64   // share of the clients reaching ep for whom it was the last step
	medMs      float64   // ep's median response time over the run
	lastMs     []float64 // the dropped clients' last response times
	n          int
}

type flowsResult struct {
	clients, ended int
	trans          []*flowTrans
	loops          []*flowLoop
	drops          []flowDrop
	scorePost      string
	roles          []flowRole // per inferred role (cookie name, or what the client writes most)
}

func statusClass(s int) string {
	switch {
	case s >= 500:
		return "5xx"
	case s == 499:
		return "499"
	case s >= 400:
		return "4xx"
	}
	return "ok"
}

// flowClient: one client (session cookie) as a list of collapsed steps, with its inferred role.
type flowClient struct {
	role  string
	steps []flowStep
}

func computeFlows(evs []accessEvent, nz *normalizer, scorePost string) flowsResult {
	bySess := map[string][]accessEvent{}
	var tEnd time.Time
	cookies := map[string]bool{}
	for _, ev := range evs {
		if ev.at.After(tEnd) {
			tEnd = ev.at
		}
		if ev.sess == "" || isStatic(ev.uri) || strings.Contains(ev.uri, "/initialize") {
			continue
		}
		bySess[ev.sess] = append(bySess[ev.sess], ev)
		if i := strings.IndexByte(ev.sess, '='); i > 0 && ev.sess[:i] != "uid" { // uid = the X-Isu-Uid measurement header, not a role
			cookies[ev.sess[:i]] = true
		}
	}
	start := func(ev accessEvent) time.Time { return ev.at.Add(-time.Duration(ev.reqtime * float64(time.Second))) }
	var clients []flowClient
	for sess, list := range bySess {
		sort.Slice(list, func(i, j int) bool { return start(list[i]).Before(start(list[j])) })
		var steps []flowStep
		writes := map[string]int{}
		for _, ev := range list {
			ep := ev.method + " " + nz.normQuiet(ev.uri)
			if ev.method != "GET" && ev.method != "HEAD" {
				writes[ep]++
			}
			if n := len(steps); n > 0 && steps[n-1].ep == ep {
				steps[n-1].n++
				steps[n-1].end = ev.at
				steps[n-1].status = ev.status
				steps[n-1].lastRT = ev.reqtime
				continue
			}
			steps = append(steps, flowStep{ep: ep, status: ev.status, start: start(ev), end: ev.at, rt: ev.reqtime, lastRT: ev.reqtime, n: 1})
		}
		if len(steps) == 0 {
			continue
		}
		// role: the cookie name when the app uses several (app_session / owner_session …); otherwise what the
		// client writes most (its two most frequent non-GET endpoints) — viewers, streamers, sign-ups … (推測)
		role := ""
		if len(cookies) > 1 {
			role = sess[:strings.IndexByte(sess, '=')]
		} else {
			type kv struct {
				k string
				n int
			}
			var w []kv
			for k, n := range writes {
				w = append(w, kv{k, n})
			}
			sort.Slice(w, func(a, b int) bool { return w[a].n > w[b].n || (w[a].n == w[b].n && w[a].k < w[b].k) })
			var names []string
			for x, v := range w {
				if x >= 2 {
					break
				}
				names = append(names, v.k)
			}
			sort.Strings(names)
			role = strings.Join(names, " + ")
			if role == "" {
				role = "read only"
			}
		}
		clients = append(clients, flowClient{role: role, steps: steps})
	}
	// per endpoint over all clients: median response time, and how often it is a client's last step ("terminal")
	rts, visits, lasts := map[string][]float64{}, map[string]int{}, map[string]int{}
	for _, c := range clients {
		seen := map[string]bool{}
		for _, s := range c.steps {
			rts[s.ep] = append(rts[s.ep], s.rt)
			if !seen[s.ep] {
				seen[s.ep] = true
				visits[s.ep]++
			}
		}
		lasts[c.steps[len(c.steps)-1].ep]++
	}
	medRT := map[string]float64{}
	for ep, l := range rts {
		sort.Float64s(l)
		medRT[ep] = pct(l, .5)
	}
	ctx := flowCtx{tEnd: tEnd, scorePost: scorePost, medRT: medRT, terminal: map[string]float64{}}
	for ep, n := range lasts {
		ctx.terminal[ep] = float64(n) / float64(visits[ep])
	}
	fr := aggregateFlows(clients, ctx)
	// roles: the biggest 6, the rest as "other"
	byRole := map[string][]flowClient{}
	for _, c := range clients {
		byRole[c.role] = append(byRole[c.role], c)
	}
	var names []string
	for r := range byRole {
		names = append(names, r)
	}
	sort.Slice(names, func(a, b int) bool { return len(byRole[names[a]]) > len(byRole[names[b]]) })
	var other []flowClient
	for i, r := range names {
		if i >= 6 || len(byRole[r]) < 3 {
			other = append(other, byRole[r]...)
			continue
		}
		rr := aggregateFlows(byRole[r], ctx)
		fr.roles = append(fr.roles, flowRole{name: r, res: rr})
	}
	if len(other) > 0 {
		fr.roles = append(fr.roles, flowRole{name: "other", res: aggregateFlows(other, ctx)})
	}
	return fr
}

type flowCtx struct {
	tEnd      time.Time
	scorePost string
	medRT     map[string]float64
	terminal  map[string]float64
}

type flowRole struct {
	name string
	res  flowsResult
}

// dropReason: why a client stopped well before the end (推測). error = its last response was 4xx/5xx;
// slow = the last response took > 2× the endpoint's median (and ≥ 200 ms) — the bench may have given up;
// end = the step is where most clients that reach it stop (≥ 50%: the scenario's natural end, e.g. exit);
// unknown = none of these (the client may have hit a time limit, or the scenario has a fixed length).
func dropReason(ep string, status int, lastRT float64, ctx flowCtx) string {
	switch {
	case status >= 400 || status == 0:
		return "error"
	case lastRT >= 0.2 && lastRT > 2*ctx.medRT[ep]:
		return "slow"
	case ctx.terminal[ep] >= 0.5:
		return "end"
	}
	return "unknown"
}

func aggregateFlows(clients []flowClient, ctx flowCtx) flowsResult {
	var fr flowsResult
	fr.scorePost = ctx.scorePost
	tr := map[string]*flowTrans{}
	out := map[string]int{}
	loops := map[string]*flowLoop{}
	drops := map[string]*flowDrop{}
	sp := ""
	if ctx.scorePost != "" {
		sp = paramKey(ctx.scorePost)
	}
	for _, c := range clients {
		steps := c.steps
		fr.clients++
		for i := 1; i < len(steps); i++ {
			k := steps[i-1].ep + "\x00" + steps[i].ep
			t := tr[k]
			if t == nil {
				t = &flowTrans{from: steps[i-1].ep, to: steps[i].ep}
				tr[k] = t
			}
			t.n++
			out[steps[i-1].ep]++
			g := steps[i].start.Sub(steps[i-1].end).Seconds()
			if g < 0 {
				g = 0
			}
			t.gaps = append(t.gaps, g)
		}
		// score loops: steps from one score_post up to (not including) the next
		if sp != "" {
			prev := -1
			for i, s := range steps {
				if paramKey(s.ep) != sp {
					continue
				}
				if prev >= 0 && i-prev <= 12 {
					seg := steps[prev:i]
					var names []string
					for _, x := range seg {
						names = append(names, x.ep)
					}
					k := strings.Join(names, "\x00")
					l := loops[k]
					if l == nil {
						l = &flowLoop{steps: names, stepRT: make([][]float64, len(names)), stepGap: make([][]float64, len(names))}
						loops[k] = l
					}
					l.n++
					l.cycles = append(l.cycles, s.start.Sub(steps[prev].start).Seconds())
					for j, x := range seg {
						l.stepRT[j] = append(l.stepRT[j], x.rt)
						next := s.start // the next score POST closes the loop
						if j+1 < len(seg) {
							next = seg[j+1].start
						}
						g := next.Sub(x.end).Seconds()
						if g < 0 {
							g = 0
						}
						l.stepGap[j] = append(l.stepGap[j], g)
					}
				}
				prev = i
			}
		}
		// drop-off: the client's last step ended more than 10 s before the last request of the run
		last := steps[len(steps)-1]
		if ctx.tEnd.Sub(last.end) > 10*time.Second {
			fr.ended++
			reason := dropReason(last.ep, last.status, last.lastRT, ctx)
			k := last.ep + "\x00" + statusClass(last.status) + "\x00" + reason
			d := drops[k]
			if d == nil {
				d = &flowDrop{ep: last.ep, status: statusClass(last.status), reason: reason, terminal: ctx.terminal[last.ep], medMs: 1000 * ctx.medRT[last.ep]}
				drops[k] = d
			}
			d.n++
			d.lastMs = append(d.lastMs, 1000*last.lastRT)
		}
	}
	for _, t := range tr {
		if out[t.from] > 0 {
			t.share = float64(t.n) / float64(out[t.from])
		}
		sort.Float64s(t.gaps)
		fr.trans = append(fr.trans, t)
	}
	sort.Slice(fr.trans, func(i, j int) bool { return fr.trans[i].n > fr.trans[j].n })
	for _, l := range loops {
		sort.Float64s(l.cycles)
		fr.loops = append(fr.loops, l)
	}
	sort.Slice(fr.loops, func(i, j int) bool { return fr.loops[i].n > fr.loops[j].n })
	for _, d := range drops {
		sort.Float64s(d.lastMs)
		fr.drops = append(fr.drops, *d)
	}
	sort.Slice(fr.drops, func(i, j int) bool { return fr.drops[i].n > fr.drops[j].n })
	return fr
}

func meanOf(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

// renderFlows writes the Flows section (markdown + html). host = the app host whose access log was used.
func renderFlows(md, hb *strings.Builder, host string, fr flowsResult) {
	e := html.EscapeString
	if fr.clients == 0 {
		return
	}
	fmt.Fprintf(md, "## Flows (%s, 推測 — %d clients by session cookie; runs of the same endpoint collapsed into one step)\n\n", host, fr.clients)
	fmt.Fprintf(hb, `<section><h2>Flows <span class="muted">· %s · 推測 — %d clients by session cookie; runs of the same endpoint (icon lists, polling) collapsed into one step · Sankey: <code>task flows TS=…</code></span></h2>`, e(host), fr.clients)
	// score loops first: they say what earns points
	if fr.scorePost == "" {
		md.WriteString("Score loops: set `score_post:` in alp.yml to see the loop that earns points.\n\n")
		hb.WriteString(`<div class="muted">Score loops: set <code>score_post:</code> in alp.yml to see the loop that earns points.</div>`)
	} else if len(fr.loops) > 0 {
		md.WriteString("**Score loops** (one client's steps from a `" + fr.scorePost + "` to the next; shorten the slowest step to earn more):\n\n| loop | times | cycle median s | p90 s | step response times ms (mean) |\n|---|---|---|---|---|\n")
		hb.WriteString(`<h3>Score loops <span class="muted">· one client's steps from a <code>` + e(fr.scorePost) + `</code> to the next; shorten the slowest step to earn more</span></h3><div class="tw"><table><tr><th>loop</th><th>times</th><th>cycle median s</th><th>p90 s</th><th>step response times ms</th></tr>`)
		for i, l := range fr.loops {
			if i >= 3 {
				break
			}
			var rts []string
			slow, slowI := 0.0, 0
			for j := range l.steps {
				m := 1000 * meanOf(l.stepRT[j])
				rts = append(rts, fmt.Sprintf("%.0f", m))
				if m > slow {
					slow, slowI = m, j
				}
			}
			fmt.Fprintf(md, "| `%s` | %d | %.3f | %.3f | %s |\n", strings.Join(l.steps, " → "), l.n, pct(l.cycles, .5), pct(l.cycles, .9), strings.Join(rts, " / "))
			var cells []string
			for j, s := range l.steps {
				c := "<code>" + e(s) + "</code>"
				if j == slowI && len(l.steps) > 1 {
					c = `<b class="hi">` + c + `</b>`
				}
				cells = append(cells, c)
			}
			fmt.Fprintf(hb, `<tr><td>%s</td><td class="num">%d</td><td class="num">%.3f</td><td class="num">%.3f</td><td class="num">%s</td></tr>`, strings.Join(cells, " → "), l.n, pct(l.cycles, .5), pct(l.cycles, .9), e(strings.Join(rts, " / ")))
		}
		md.WriteString("\n")
		hb.WriteString(`</table></div>`)
	}
	md.WriteString("**Transitions** (share = of all steps that left `from`):\n\n| from | to | count | share | median gap s |\n|---|---|---|---|---|\n")
	hb.WriteString(`<h3>Transitions <span class="muted">· share = of all steps that left "from" · gap = think time after the previous response</span></h3><div class="tw"><table><tr><th>from</th><th>to</th><th>count</th><th>share</th><th>median gap s</th></tr>`)
	for i, t := range fr.trans {
		if i >= 15 {
			break
		}
		fmt.Fprintf(md, "| `%s` | `%s` | %d | %.0f%% | %.3f |\n", t.from, t.to, t.n, 100*t.share, pct(t.gaps, .5))
		fmt.Fprintf(hb, `<tr><td><code>%s</code></td><td><code>%s</code></td><td class="num">%d</td><td class="num">%.0f%%</td><td class="num">%.3f</td></tr>`, e(t.from), e(t.to), t.n, 100*t.share, pct(t.gaps, .5))
	}
	md.WriteString("\n")
	hb.WriteString(`</table></div>`)
	if fr.ended > 0 {
		const dropDef = "a client whose last request ended > 10 s before the run's last request (normal ends included). reason: error = last response 4xx/5xx; slow = last response > 2× the endpoint's median and ≥ 200 ms (the bench may have given up); end = most clients that reach this step stop there (the scenario's natural end); unknown = none of these"
		fmt.Fprintf(md, "**Drop-off** (%d of %d clients; %s):\n\n| last step | status | reason | clients | last ms (median) | endpoint median ms |\n|---|---|---|---|---|---|\n", fr.ended, fr.clients, dropDef)
		fmt.Fprintf(hb, `<h3>Drop-off <span class="muted">· %d of %d clients · %s</span></h3><div class="tw"><table><tr><th>last step</th><th>status</th><th>reason</th><th>clients</th><th>last ms (median)</th><th>endpoint median ms</th></tr>`, fr.ended, fr.clients, e(dropDef))
		for i, d := range fr.drops {
			if i >= 8 {
				break
			}
			fmt.Fprintf(md, "| `%s` | %s | %s | %d | %.0f | %.0f |\n", d.ep, d.status, d.reason, d.n, pct(d.lastMs, .5), d.medMs)
			fmt.Fprintf(hb, `<tr><td><code>%s</code></td><td class="%s">%s</td><td class="%s">%s</td><td class="num">%d</td><td class="num">%.0f</td><td class="num">%.0f</td></tr>`, e(d.ep), ternary(d.status != "ok", "bad", ""), e(d.status), ternary(d.reason == "error" || d.reason == "slow", "bad", ""), e(d.reason), d.n, pct(d.lastMs, .5), d.medMs)
		}
		md.WriteString("\n")
		hb.WriteString(`</table></div>`)
	}
	hb.WriteString(`</section>`)
}

// flowsMeta: the part of Flows that other tools (app-analysis refresh, next-step) read from report.json.
func flowsMeta(fr flowsResult) map[string]any {
	var loops []map[string]any
	for i, l := range fr.loops {
		if i >= 3 {
			break
		}
		// per step: response time and the wait until the next step, as p10 / median / p90 (ms)
		q3 := func(xs []float64) []float64 {
			x := append([]float64(nil), xs...)
			sort.Float64s(x)
			return []float64{1000 * pct(x, .1), 1000 * pct(x, .5), 1000 * pct(x, .9)}
		}
		var rts, gaps []float64
		var rtq, gapq [][]float64
		for j := range l.steps {
			r, g := q3(l.stepRT[j]), q3(l.stepGap[j])
			rts, gaps = append(rts, r[1]), append(gaps, g[1])
			rtq, gapq = append(rtq, r), append(gapq, g)
		}
		loops = append(loops, map[string]any{"steps": l.steps, "times": l.n, "cycle_med_s": pct(l.cycles, .5), "cycle_p10_s": pct(l.cycles, .1), "cycle_p90_s": pct(l.cycles, .9), "cycle_max_s": pct(l.cycles, 1), "step_ms": rts, "gap_ms": gaps, "step_q_ms": rtq, "gap_q_ms": gapq})
	}
	var trans []map[string]any
	for i, t := range fr.trans {
		if i >= 30 {
			break
		}
		trans = append(trans, map[string]any{"from": t.from, "to": t.to, "n": t.n, "share": t.share})
	}
	var drops []map[string]any
	for i, d := range fr.drops {
		if i >= 10 {
			break
		}
		drops = append(drops, map[string]any{"ep": d.ep, "status": d.status, "reason": d.reason, "n": d.n, "last_ms": pct(d.lastMs, .5), "ep_med_ms": d.medMs, "terminal": d.terminal})
	}
	m := map[string]any{"clients": fr.clients, "ended": fr.ended, "score_post": fr.scorePost, "loops": loops, "transitions": trans, "drops": drops}
	var roles []map[string]any
	for _, r := range fr.roles {
		rm := flowsMeta(r.res)
		rm["name"] = r.name
		roles = append(roles, rm)
	}
	if roles != nil {
		m["roles"] = roles
	}
	return m
}

// computeFlowsCached: the Next actions' score path and the Flows section use the same flows of the busiest host.
var flowsCache = map[*hostRun]flowsResult{}

func computeFlowsCached(h *hostRun, nz *normalizer, scorePost string) flowsResult {
	if fr, ok := flowsCache[h]; ok {
		return fr
	}
	fr := computeFlows(h.Access, nz, scorePost)
	flowsCache[h] = fr
	return fr
}
