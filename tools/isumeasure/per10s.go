package main

import (
	"fmt"
	"html"
	"math"
	"strings"
	"time"
)

// per10s.go: count and avg ms per 10-second bucket of the load window for the endpoints that matter (top by share,
// every internal job, the score POST). ISUCON14: coordinate fell 9,049 → 3,685 per 10 s while matching fell from 38
// to 3–4 calls per 10 s — invisible in the whole-window averages.

func per10sRows(eps []*epStat, scorePost string) []*epStat {
	var rows []*epStat
	seen := map[string]bool{}
	add := func(e *epStat) {
		if !seen[e.key] {
			seen[e.key] = true
			rows = append(rows, e)
		}
	}
	n := 0
	for _, e := range eps { // eps are sorted by total time = share
		if n >= 6 {
			break
		}
		if strings.Contains(e.key, "static assets") || isInternalEp(e.key) {
			continue
		}
		add(e)
		n++
	}
	for _, e := range eps {
		if scorePost != "" && paramKey(e.key) == paramKey(scorePost) {
			add(e)
		}
	}
	for _, e := range eps {
		if isInternalEp(e.key) {
			add(e)
		}
	}
	return rows
}

func renderPer10s(md, hb *strings.Builder, host string, eps []*epStat, w loadWindow, t0 time.Time, bucket float64, scorePost string) {
	rows := per10sRows(eps, scorePost)
	if len(rows) == 0 || t0.IsZero() || w.fromT.IsZero() || !w.toT.After(w.fromT) {
		return
	}
	from := int(math.Floor(w.fromT.Sub(t0).Seconds() / bucket))
	to := int(math.Floor(w.toT.Add(-time.Millisecond).Sub(t0).Seconds() / bucket))
	if to < from {
		return
	}
	if to-from > 30 { // a 5-minute bench at most; keep the table readable
		to = from + 30
	}
	e := html.EscapeString
	fmt.Fprintf(md, "## Per 10 s (%s, load window; count · avg ms per bucket)\n\n| endpoint |", host)
	fmt.Fprintf(hb, `<section><h2>Per 10 s <span class="muted">(%s · load window · count / avg ms per 10-second bucket; whole-run averages hide a mid-run collapse)</span></h2>`, e(host))
	hb.WriteString(per10sCharts(rows, from, to, int(bucket)))
	hb.WriteString(`<div class="tw"><table class="tl"><tr><th>endpoint</th>`)
	for b := from; b <= to; b++ {
		fmt.Fprintf(md, " %ds |", b*int(bucket))
		fmt.Fprintf(hb, `<th>%ds</th>`, b*int(bucket))
	}
	md.WriteString(" first → last |\n|---|" + strings.Repeat("---|", to-from+2) + "\n")
	hb.WriteString(`<th>first → last</th></tr>`)
	for _, r := range rows {
		fmt.Fprintf(md, "| `%s` |", r.key)
		fmt.Fprintf(hb, `<tr><td><code>%s</code></td>`, e(r.key))
		for b := from; b <= to; b++ {
			c := r.perBucket[b]
			if c == 0 {
				md.WriteString(" 0 |")
				hb.WriteString(`<td class="num muted">0</td>`)
				continue
			}
			avg := 1000 * r.perBucketSum[b] / float64(c)
			fmt.Fprintf(md, " %d · %.0f |", c, avg)
			fmt.Fprintf(hb, `<td class="num">%d <span class="muted">· %.0f</span></td>`, c, avg)
		}
		// trend between the first and last full buckets (the edge buckets are partial)
		f, l := from, to
		if to-from >= 3 {
			f, l = from+1, to-1
		}
		trend, cls := "-", ""
		if c0 := r.perBucket[f]; c0 > 0 {
			ch := 100 * float64(r.perBucket[l]-c0) / float64(c0)
			trend = fmt.Sprintf("%d → %d (%+.0f%%)", c0, r.perBucket[l], ch)
			cls = ternary(ch <= -40, "bad", ternary(ch >= 40, "hi", ""))
			if ch <= -40 {
				trend += " ⚠"
			}
		}
		fmt.Fprintf(md, " %s |\n", trend)
		fmt.Fprintf(hb, `<td class="num %s">%s</td></tr>`, cls, e(trend))
	}
	md.WriteString("\n⚠ = the count fell by 40 % or more from the first to the last full bucket: something stopped feeding this endpoint (a job, a queue, an error), find out when and why.\n\n")
	hb.WriteString(`</table></div><div class="muted">⚠ = the count fell by ≥ 40 % from the first to the last full bucket: something stopped feeding this endpoint (a job, a queue, an error)</div></section>`)
}

// per10sCharts (report.html): request count and avg ms per bucket for the table's endpoints, one checkbox per
// endpoint shared by both charts (per10sJS toggles the series with the same data-s).
func per10sCharts(rows []*epStat, from, to, bucket int) string {
	var cnt, avg [][]float64
	for _, r := range rows {
		var c, a []float64
		for b := from; b <= to; b++ {
			n := r.perBucket[b]
			c = append(c, float64(n))
			if n > 0 {
				a = append(a, 1000*r.perBucketSum[b]/float64(n))
			} else {
				a = append(a, math.NaN())
			}
		}
		cnt, avg = append(cnt, c), append(avg, a)
	}
	var b strings.Builder
	b.WriteString(`<div class="p10">`)
	b.WriteString(`<div class="p10ctl"><button type="button" data-p10="all">show all</button> <button type="button" data-p10="none">hide all</button>`)
	for i, r := range rows {
		fmt.Fprintf(&b, ` <label><input type="checkbox" data-s="%d" checked><span class="sw" style="background:%s"></span><code>%s</code></label>`, i, palette[i%len(palette)], html.EscapeString(r.key))
	}
	b.WriteString(`</div><div class="charts"><div><div class="ct">requests per 10 s</div>`)
	b.WriteString(seriesSVG(cnt, from, bucket, 520, 200))
	b.WriteString(`</div><div><div class="ct">avg ms per 10 s</div>`)
	b.WriteString(seriesSVG(avg, from, bucket, 520, 200))
	b.WriteString(`</div></div></div>`)
	return b.String()
}

// seriesSVG: a plain multi-series line chart; series i is a <g data-s="i"> (NaN = gap in the line).
func seriesSVG(ss [][]float64, from, bucket, w, h int) string {
	maxv, n := 0.0, 0
	for _, s := range ss {
		for _, v := range s {
			if !math.IsNaN(v) && v > maxv {
				maxv = v
			}
		}
		n = max(n, len(s))
	}
	if maxv == 0 {
		maxv = 1
	}
	left, top, right, bottom := 48, 8, 10, 22
	pw, ph := float64(w-left-right), float64(h-top-bottom)
	var b strings.Builder
	// data-top / data-ph / data-max: geometry for per10sJS, which rescales y to the visible series
	fmt.Fprintf(&b, `<svg class="chart" width="%d" height="%d" viewBox="0 0 %d %d" data-top="%d" data-ph="%.1f" data-max="%g">`, w, h, w, h, top, ph, maxv)
	for i := 0; i <= 4; i++ {
		y := float64(top) + ph*float64(i)/4
		fmt.Fprintf(&b, `<line x1="%d" y1="%.1f" x2="%d" y2="%.1f" stroke="#e5e7eb"/><text class="yt" data-k="%d" x="%d" y="%.1f" font-size="10" text-anchor="end" fill="#6b7280">%s</text>`, left, y, w-right, y, 4-i, left-4, y+3, fmtNum(maxv*float64(4-i)/4))
	}
	xOf := func(i int) float64 { return float64(left) + pw*float64(i)/float64(max(1, n-1)) }
	yOf := func(v float64) float64 { return float64(top) + ph - ph*v/maxv }
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, `<text x="%.1f" y="%d" font-size="10" text-anchor="middle" fill="#6b7280">%ds</text>`, xOf(i), h-6, (from+i)*bucket)
	}
	for si, s := range ss {
		c := palette[si%len(palette)]
		fmt.Fprintf(&b, `<g data-s="%d">`, si)
		var cur, vals []string
		flush := func() {
			if len(cur) > 0 {
				fmt.Fprintf(&b, `<polyline fill="none" stroke="%s" stroke-width="1.8" points="%s" data-v="%s"/>`, c, strings.Join(cur, " "), strings.Join(vals, ","))
			}
			cur, vals = nil, nil
		}
		for i, v := range s {
			if math.IsNaN(v) {
				flush()
				continue
			}
			cur = append(cur, fmt.Sprintf("%.1f,%.1f", xOf(i), yOf(v)))
			vals = append(vals, fmt.Sprintf("%g", v))
		}
		flush()
		for i, v := range s {
			if !math.IsNaN(v) {
				fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="2" fill="%s" data-v="%g"><title>%s</title></circle>`, xOf(i), yOf(v), c, v, fmtNum(v))
			}
		}
		b.WriteString(`</g>`)
	}
	b.WriteString(`</svg>`)
	return b.String()
}

// per10sJS: the checkbox of an endpoint hides / shows its series in both charts; "show all" / "hide all".
const per10sJS = `
document.querySelectorAll('.p10').forEach(function(box){
  function fmt(v){ return v >= 1e6 ? (v/1e6).toFixed(1)+'M' : v >= 1e3 ? (v/1e3).toFixed(1)+'k' : v >= 100 ? v.toFixed(0) : v >= 10 ? v.toFixed(1) : v.toFixed(2); }
  // y axis = max of the visible series (none visible: keep the last scale)
  function rescale(svg){
    var top = +svg.getAttribute('data-top'), ph = +svg.getAttribute('data-ph'), mx = 0;
    svg.querySelectorAll('g[data-s]').forEach(function(g){ if (g.style.display !== 'none') g.querySelectorAll('[data-v]').forEach(function(el){ el.getAttribute('data-v').split(',').forEach(function(v){ mx = Math.max(mx, +v); }); }); });
    if (mx <= 0) return;
    svg.setAttribute('data-max', mx);
    var y = function(v){ return (top + ph - ph * v / mx).toFixed(1); };
    svg.querySelectorAll('polyline[data-v]').forEach(function(pl){ var vs = pl.getAttribute('data-v').split(','), xs = pl.getAttribute('points').split(' '); pl.setAttribute('points', xs.map(function(p, i){ return p.split(',')[0] + ',' + y(+vs[i]); }).join(' ')); });
    svg.querySelectorAll('circle[data-v]').forEach(function(c){ c.setAttribute('cy', y(+c.getAttribute('data-v'))); });
    svg.querySelectorAll('text.yt').forEach(function(t){ t.textContent = fmt(mx * (+t.getAttribute('data-k')) / 4); });
  }
  function sync(){ box.querySelectorAll('input[data-s]').forEach(function(cb){ box.querySelectorAll('svg g[data-s="'+cb.getAttribute('data-s')+'"]').forEach(function(g){ g.style.display = cb.checked ? '' : 'none'; }); }); box.querySelectorAll('svg[data-ph]').forEach(rescale); }
  box.querySelectorAll('input[data-s]').forEach(function(cb){ cb.addEventListener('change', sync); });
  box.querySelectorAll('button[data-p10]').forEach(function(bt){ bt.addEventListener('click', function(){ var on = bt.getAttribute('data-p10') === 'all'; box.querySelectorAll('input[data-s]').forEach(function(cb){ cb.checked = on; }); sync(); }); });
});
`
