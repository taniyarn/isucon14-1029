package main

import (
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"
)

// internal.go: periodically called internal endpoints (/api/internal/*: a matcher, a settlement job) are not
// request traffic. Their average latency says nothing; what bounds the score is how often they run and how much
// they do per run (ISUCON14: matching assigned one ride per 0.5 s call, so at most 120 rides per minute).

var internalEpRe = regexp.MustCompile(`^(?:[A-Z]+ )?/(?:api/)?internal/`)

func isInternalEp(key string) bool { return internalEpRe.MatchString(key) }

type internalWork struct {
	norm    string
	perCall float64
	total   int
}

type internalStat struct {
	host, key        string
	calls            int
	medGap, p90Gap   float64 // seconds between call starts
	avgMs, p99Ms     float64
	work             []internalWork // writes (and the heaviest reads) per call, from sqltag
	handler          string
	writesPerCallSum float64
}

// internalStats: one row per internal endpoint with ≥ 3 calls on this host.
func internalStats(host string, eps []*epStat, byTag map[string][]*sqlStat, epHandler map[string]string) []internalStat {
	var out []internalStat
	for _, e := range eps {
		if !isInternalEp(e.key) || e.count < 3 {
			continue
		}
		st := internalStat{host: host, key: e.key, calls: e.count, avgMs: 1000 * e.sum / float64(e.count), p99Ms: 1000 * pct(e.rts, .99), handler: epHandler[e.key]}
		starts := append(e.starts[:0:0], e.starts...)
		sort.Slice(starts, func(i, j int) bool { return starts[i].Before(starts[j]) })
		var gaps []float64
		for i := 1; i < len(starts); i++ {
			gaps = append(gaps, starts[i].Sub(starts[i-1]).Seconds())
		}
		sort.Float64s(gaps)
		st.medGap, st.p90Gap = pct(gaps, .5), pct(gaps, .9)
		for _, q := range byTag[paramKey(e.key)] {
			up := strings.ToUpper(strings.TrimSpace(q.norm))
			if strings.HasPrefix(up, "UPDATE") || strings.HasPrefix(up, "INSERT") || strings.HasPrefix(up, "DELETE") || strings.HasPrefix(up, "REPLACE") {
				st.work = append(st.work, internalWork{norm: q.norm, perCall: float64(q.count) / float64(e.count), total: q.count})
				st.writesPerCallSum += float64(q.count) / float64(e.count)
			}
		}
		sort.Slice(st.work, func(i, j int) bool { return st.work[i].total > st.work[j].total })
		out = append(out, st)
	}
	return out
}

func (st internalStat) workLine() string {
	if len(st.work) == 0 {
		return "no tagged writes (sqltag off, or the job only reads)"
	}
	var parts []string
	for i, w := range st.work {
		if i >= 3 {
			break
		}
		parts = append(parts, fmt.Sprintf("%.1f× %s", w.perCall, trunc(strings.TrimSuffix(w.norm, ";"), 70)))
	}
	return strings.Join(parts, "; ")
}

// action: the job's throughput bound, so it is weighed against the endpoint work rather than hidden in "Speed up".
func (st internalStat) action() action {
	rate := ""
	if st.medGap > 0 && len(st.work) > 0 {
		rate = fmt.Sprintf(" ⇒ ≈ %.1f %s per second at this interval", st.work[0].perCall/st.medGap, strings.Fields(st.work[0].norm)[0]+" "+firstTable(st.work[0].norm))
	}
	title := fmt.Sprintf("Periodic job %s: every %.0f ms, %s per call", st.key, 1000*st.medGap, ternary(len(st.work) > 0, fmt.Sprintf("≈ %.1f writes", st.writesPerCallSum), "work unknown"))
	detail := fmt.Sprintf("%d calls, median gap %.0f ms (p90 %.0f ms), %.0f ms per call (p99 %.0f). Work per call: %s%s. If what it produces (matches, settlements) is what the bench waits for, process more items per call (the whole queue, not one) or run it more often — not faster per call.", st.calls, 1000*st.medGap, 1000*st.p90Gap, st.avgMs, st.p99Ms, st.workLine(), rate)
	a := mk("P2", title, detail, "", st.handler)
	a.kind = "internal"
	return a
}

var firstTableRe = regexp.MustCompile("(?i)\\b(?:UPDATE|INTO|FROM)\\s+`?(\\w+)`?")

func firstTable(q string) string {
	if m := firstTableRe.FindStringSubmatch(q); m != nil {
		return m[1]
	}
	return ""
}

func renderInternal(md, hb *strings.Builder, sts []internalStat) {
	if len(sts) == 0 {
		return
	}
	md.WriteString("## Periodic internal endpoints (call interval × work per call; not request traffic)\n\n| host | endpoint | calls | median gap ms | p90 gap ms | avg ms | p99 ms | work per call (sqltag) |\n|---|---|---|---|---|---|---|---|\n")
	hb.WriteString(`<section><h2>Periodic internal endpoints <span class="muted">(call interval × work per call; excluded from "Speed up")</span></h2><div class="tw"><table><tr><th>host</th><th>endpoint</th><th>calls</th><th>median gap ms</th><th>p90 gap ms</th><th>avg ms</th><th>p99 ms</th><th>work per call (sqltag)</th></tr>`)
	for _, st := range sts {
		fmt.Fprintf(md, "| %s | `%s` | %d | %.0f | %.0f | %.0f | %.0f | %s |\n", st.host, st.key, st.calls, 1000*st.medGap, 1000*st.p90Gap, st.avgMs, st.p99Ms, strings.ReplaceAll(st.workLine(), "|", "\\|"))
		fmt.Fprintf(hb, `<tr><td>%s</td><td><code>%s</code></td><td class="num">%d</td><td class="num">%.0f</td><td class="num">%.0f</td><td class="num">%.0f</td><td class="num">%.0f</td><td>%s</td></tr>`, html.EscapeString(st.host), html.EscapeString(st.key), st.calls, 1000*st.medGap, 1000*st.p90Gap, st.avgMs, st.p99Ms, html.EscapeString(st.workLine()))
	}
	md.WriteString("\nA job that runs every N ms and handles K items per call caps the score at K/N items per second however fast each call is.\n\n")
	hb.WriteString(`</table></div><div class="muted">a job that runs every N ms and handles K items per call caps the score at K/N items per second however fast each call is</div></section>`)
}
