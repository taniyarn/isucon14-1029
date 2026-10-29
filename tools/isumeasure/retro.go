package main

import (
	"encoding/json"
	"flag"
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

// retro: build the retrospective material for one contest from the ops data dir:
//   data/<contest>/runs.json              (score / note / bench output per run, entered in the panel)
//   data/<contest>/measurements/<ts>/meta.json   (git commit, requests, duration per host — written at collect time)
//   data/<contest>/measurements/<ts>/summary.md  (verdict + top endpoints at that time)
// plus, optionally, the contest git repo (-repo) to list the commits between consecutive runs.
// Output: retro.md (for Claude / humans) and retro.html (score timeline chart + table).

type retroRun struct {
	TS       string
	At       time.Time
	Score    float64
	HasScore bool
	Note     string
	Bench    string
	Commit   string
	Branch   string
	Subject  string
	Req      int
	Err      int
	Dur      float64
	Verdict  string
	Reasons  []string
	Eps      map[string][4]float64 // endpoint -> share, count, avg ms, p99 ms (from summary.md)
	Changes  []string              // commits since the previous run
	Hosts    int
}

var verdictRe = regexp.MustCompile(`(?m)^### [^:]+: (.+)$`)
var reasonRe = regexp.MustCompile(`(?m)^- (load window .+|CPU .+|MySQL .+)$`)
var epRowRe = regexp.MustCompile("(?m)^\\| `([^`]+)` \\| (\\d+)% \\| (\\d+) \\| (\\d+) \\| (\\d+) \\|")

func runRetro(args []string) error {
	fs := flag.NewFlagSet("retro", flag.ContinueOnError)
	repo := fs.String("repo", "", "contest git repo (to list commits between runs)")
	out := fs.String("out", "", "output dir (default: the contest dir)")
	issues := fs.String("issues", "", "optional JSON from `gh issue list --state all --json number,title,state,closedAt,labels` to include")
	if err := fs.Parse(flagsFirst(args)); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: isumeasure retro <data/<contest> dir> [-repo PATH] [-issues FILE] [-out DIR]")
	}
	dir := fs.Arg(0)
	if *out == "" {
		*out = dir
	}
	notes := map[string]map[string]any{}
	if b, err := os.ReadFile(filepath.Join(dir, "runs.json")); err == nil {
		json.Unmarshal(b, &notes)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "measurements"))
	var runs []*retroRun
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		r := &retroRun{TS: e.Name(), Eps: map[string][4]float64{}}
		r.At, _ = time.Parse("20060102-150405", e.Name())
		base := filepath.Join(dir, "measurements", e.Name())
		if b, err := os.ReadFile(filepath.Join(base, "meta.json")); err == nil {
			var meta map[string]any
			json.Unmarshal(b, &meta)
			if hs, ok := meta["hosts"].([]any); ok {
				r.Hosts = len(hs)
				for _, x := range hs {
					hm, _ := x.(map[string]any)
					r.Req += int(num(hm["req_total"]))
					r.Err += int(num(hm["err_total"]))
					if d := num(hm["duration_sec"]); d > r.Dur {
						r.Dur = d
					}
					if g, ok := hm["git"].(map[string]any); ok && r.Commit == "" {
						r.Commit, r.Branch, r.Subject = str(g["commit"]), str(g["branch"]), str(g["subject"])
					}
				}
			}
		} else {
			// fall back to per-host run.json if raw data still exists
			hosts, _ := os.ReadDir(base)
			for _, h := range hosts {
				if b, err := os.ReadFile(filepath.Join(base, h.Name(), "run.json")); err == nil {
					var ri map[string]any
					json.Unmarshal(b, &ri)
					r.Hosts++
					r.Req += int(num(ri["req_total"]))
					r.Err += int(num(ri["err_total"]))
					if d := num(ri["duration_sec"]); d > r.Dur {
						r.Dur = d
					}
					if g, ok := ri["git"].(map[string]any); ok && r.Commit == "" {
						r.Commit, r.Branch, r.Subject = str(g["commit"]), str(g["branch"]), str(g["subject"])
					}
				}
			}
		}
		if b, err := os.ReadFile(filepath.Join(base, "summary.md")); err == nil {
			s := string(b)
			if m := verdictRe.FindStringSubmatch(s); m != nil {
				r.Verdict = m[1]
			}
			for _, m := range reasonRe.FindAllStringSubmatch(s, -1) {
				r.Reasons = append(r.Reasons, m[1])
			}
			for _, m := range epRowRe.FindAllStringSubmatch(s, -1) {
				if _, ok := r.Eps[m[1]]; !ok {
					sh, _ := strconv.ParseFloat(m[2], 64)
					c, _ := strconv.ParseFloat(m[3], 64)
					avg, _ := strconv.ParseFloat(m[4], 64)
					p99, _ := strconv.ParseFloat(m[5], 64)
					r.Eps[m[1]] = [4]float64{sh, c, avg, p99}
				}
			}
		}
		if n := notes[e.Name()]; n != nil {
			if v, err := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(n["score"])), 64); err == nil {
				r.Score, r.HasScore = v, true
			}
			r.Note = strings.TrimSpace(fmt.Sprint(n["note"]))
			r.Bench = strings.TrimSpace(fmt.Sprint(n["bench"]))
			if r.Note == "<nil>" {
				r.Note = ""
			}
			if r.Bench == "<nil>" {
				r.Bench = ""
			}
		}
		runs = append(runs, r)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].TS < runs[j].TS })
	// commits between consecutive runs
	if *repo != "" {
		for i := 1; i < len(runs); i++ {
			a, b := runs[i-1].Commit, runs[i].Commit
			if a == "" || b == "" || a == b {
				continue
			}
			outb, err := exec.Command("git", "-C", *repo, "log", "--format=%h %ci %an: %s", a+".."+b).Output()
			if err == nil {
				for _, l := range strings.Split(strings.TrimSpace(string(outb)), "\n") {
					if l != "" {
						runs[i].Changes = append(runs[i].Changes, l)
					}
				}
			}
		}
	}
	var issueRows []map[string]any
	if *issues != "" {
		if b, err := os.ReadFile(*issues); err == nil {
			json.Unmarshal(b, &issueRows)
		}
	}
	// ---- render ----
	e := html.EscapeString
	var md, hb strings.Builder
	contest := filepath.Base(dir)
	fmt.Fprintf(&md, "# Retrospective material: %s\n\n", contest)
	fmt.Fprintf(&md, "%d runs", len(runs))
	if len(runs) > 0 {
		fmt.Fprintf(&md, " from %s to %s", runs[0].At.Format("2006-01-02 15:04"), runs[len(runs)-1].At.Format("15:04"))
	}
	best, bestTS := 0.0, ""
	for _, r := range runs {
		if r.HasScore && r.Score > best {
			best, bestTS = r.Score, r.TS
		}
	}
	if bestTS != "" {
		fmt.Fprintf(&md, " · best score %.0f at %s", best, bestTS)
	}
	md.WriteString("\n\n## Timeline\n\n| time | score | Δ | commit | changes since previous run | verdict | req | 5xx | note |\n|---|---|---|---|---|---|---|---|---|\n")
	prev := 0.0
	hasPrev := false
	for _, r := range runs {
		delta := ""
		if r.HasScore && hasPrev {
			delta = fmt.Sprintf("%+.0f", r.Score-prev)
		}
		if r.HasScore {
			prev, hasPrev = r.Score, true
		}
		sc := ""
		if r.HasScore {
			sc = fmt.Sprintf("%.0f", r.Score)
		}
		ch := strings.Join(r.Changes, "<br>")
		if ch == "" && r.Subject != "" {
			ch = "(HEAD) " + r.Subject
		}
		fmt.Fprintf(&md, "| %s | %s | %s | %s@%s | %s | %s | %d | %d | %s |\n", r.At.Format("15:04"), sc, delta, r.Branch, r.Commit, strings.ReplaceAll(ch, "|", "\\|"), strings.ReplaceAll(r.Verdict, "|", "\\|"), r.Req, r.Err, strings.ReplaceAll(r.Note, "|", "\\|"))
	}
	// endpoint latency trends for the endpoints that were ever in the top 5
	top := map[string]bool{}
	for _, r := range runs {
		type kv struct {
			k string
			v float64
		}
		var l []kv
		for k, v := range r.Eps {
			l = append(l, kv{k, v[0]})
		}
		sort.Slice(l, func(i, j int) bool { return l[i].v > l[j].v })
		for i, x := range l {
			if i >= 5 {
				break
			}
			top[x.k] = true
		}
	}
	var topList []string
	for k := range top {
		topList = append(topList, k)
	}
	sort.Strings(topList)
	if len(topList) > 0 {
		md.WriteString("\n## Endpoint avg latency (ms) per run — the ones that were ever in the top 5 by total time\n\n| endpoint |")
		for _, r := range runs {
			md.WriteString(" " + r.At.Format("15:04") + " |")
		}
		md.WriteString("\n|---|" + strings.Repeat("---|", len(runs)) + "\n")
		for _, ep := range topList {
			fmt.Fprintf(&md, "| `%s` |", ep)
			for _, r := range runs {
				if v, ok := r.Eps[ep]; ok {
					fmt.Fprintf(&md, " %.0f (%.0f%%) |", v[2], v[0])
				} else {
					md.WriteString(" - |")
				}
			}
			md.WriteString("\n")
		}
	}
	md.WriteString("\n## Bench output per run\n\n")
	for _, r := range runs {
		if r.Bench != "" {
			fmt.Fprintf(&md, "### %s\n\n```\n%s\n```\n\n", r.TS, r.Bench)
		}
	}
	if len(issueRows) > 0 {
		md.WriteString("## Issues\n\n| # | state | closed | labels | title |\n|---|---|---|---|---|\n")
		for _, is := range issueRows {
			var labels []string
			if ls, ok := is["labels"].([]any); ok {
				for _, l := range ls {
					lm, _ := l.(map[string]any)
					labels = append(labels, str(lm["name"]))
				}
			}
			fmt.Fprintf(&md, "| %d | %s | %s | %s | %s |\n", int(num(is["number"])), str(is["state"]), strings.TrimSuffix(str(is["closedAt"]), "Z"), strings.Join(labels, ","), strings.ReplaceAll(str(is["title"]), "|", "\\|"))
		}
		md.WriteString("\n")
	}
	md.WriteString("## Verdict history\n\n")
	for _, r := range runs {
		if r.Verdict != "" {
			fmt.Fprintf(&md, "- %s: %s\n", r.At.Format("15:04"), r.Verdict)
		}
	}
	// html: score chart + table
	var pts []float64
	for _, r := range runs {
		if r.HasScore {
			pts = append(pts, r.Score)
		} else {
			pts = append(pts, 0)
		}
	}
	hb.WriteString(`<section><h2>Score timeline</h2>` + scoreChart(runs, 960, 260) + `</section>`)
	hb.WriteString(`<section><h2>Runs</h2><div class="tw"><table><tr><th>time</th><th>score</th><th>Δ</th><th>commit</th><th>changes since previous run</th><th>verdict</th><th>req</th><th>5xx</th><th>note</th><th>bench output</th></tr>`)
	prev, hasPrev = 0, false
	for _, r := range runs {
		delta := ""
		if r.HasScore && hasPrev {
			delta = fmt.Sprintf("%+.0f", r.Score-prev)
		}
		if r.HasScore {
			prev, hasPrev = r.Score, true
		}
		sc := ""
		if r.HasScore {
			sc = fmt.Sprintf("%.0f", r.Score)
		}
		var ch []string
		for _, c := range r.Changes {
			ch = append(ch, e(c))
		}
		chs := strings.Join(ch, "<br>")
		if chs == "" && r.Subject != "" {
			chs = `<span class="muted">(HEAD) ` + e(r.Subject) + `</span>`
		}
		fmt.Fprintf(&hb, `<tr><td class="nw">%s</td><td class="num"><b>%s</b></td><td class="num %s">%s</td><td class="nw"><code>%s@%s</code></td><td>%s</td><td>%s</td><td class="num">%d</td><td class="num">%d</td><td>%s</td><td>%s</td></tr>`, r.At.Format("15:04"), sc, ternary(strings.HasPrefix(delta, "-"), "bad", ""), delta, e(r.Branch), e(r.Commit), chs, e(r.Verdict), r.Req, r.Err, e(r.Note), ternary(r.Bench != "", `<details><summary>bench</summary><pre>`+e(r.Bench)+`</pre></details>`, ""))
	}
	hb.WriteString(`</table></div></section>`)
	if len(topList) > 0 {
		hb.WriteString(`<section><h2>Endpoint avg latency per run (ms, share of total time)</h2><div class="tw"><table><tr><th>endpoint</th>`)
		for _, r := range runs {
			fmt.Fprintf(&hb, `<th>%s</th>`, r.At.Format("15:04"))
		}
		hb.WriteString(`</tr>`)
		for _, ep := range topList {
			fmt.Fprintf(&hb, `<tr><td><code>%s</code></td>`, e(ep))
			for _, r := range runs {
				if v, ok := r.Eps[ep]; ok {
					fmt.Fprintf(&hb, `<td class="num">%.0f <span class="muted">%.0f%%</span></td>`, v[2], v[0])
				} else {
					hb.WriteString(`<td class="muted">-</td>`)
				}
			}
			hb.WriteString(`</tr>`)
		}
		hb.WriteString(`</table></div></section>`)
	}
	htm := `<!DOCTYPE html><html lang="ja"><head><meta charset="utf-8"><title>Retrospective ` + e(contest) + `</title><style>` + reportCSS + `</style></head><body><header><h1>Retrospective: ` + e(contest) + `</h1><div class="sub">` + fmt.Sprintf("%d runs · generated %s", len(runs), time.Now().Format(time.RFC3339)) + `</div></header><main>` + hb.String() + `</main><script>` + sortJS + `</script></body></html>`
	os.MkdirAll(*out, 0755)
	if err := os.WriteFile(filepath.Join(*out, "retro.md"), []byte(md.String()), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*out, "retro.html"), []byte(htm), 0644); err != nil {
		return err
	}
	fmt.Printf("%s/retro.md\n%s/retro.html\n", *out, *out)
	return nil
}

func scoreChart(runs []*retroRun, w, h int) string {
	if len(runs) == 0 {
		return `<div class="muted">no runs</div>`
	}
	maxv := 1.0
	for _, r := range runs {
		if r.Score > maxv {
			maxv = r.Score
		}
	}
	t0, t1 := runs[0].At, runs[len(runs)-1].At
	span := t1.Sub(t0).Seconds()
	if span <= 0 {
		span = 1
	}
	left, top, right, bottom := 56, 16, 16, 28
	pw, ph := float64(w-left-right), float64(h-top-bottom)
	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="chart" width="%d" height="%d">`, w, h)
	for i := 0; i <= 4; i++ {
		y := float64(top) + ph*float64(i)/4
		fmt.Fprintf(&b, `<line x1="%d" y1="%.1f" x2="%d" y2="%.1f" stroke="#e5e7eb"/><text x="%d" y="%.1f" font-size="10" text-anchor="end" fill="#6b7280">%s</text>`, left, y, w-right, y, left-4, y+3, fmtNum(maxv*float64(4-i)/4))
	}
	var pts []string
	for _, r := range runs {
		x := float64(left) + pw*r.At.Sub(t0).Seconds()/span
		if r.HasScore {
			y := float64(top) + ph - ph*r.Score/maxv
			pts = append(pts, fmt.Sprintf("%.1f,%.1f", x, y))
			fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="4" fill="%s"><title>%s score %.0f%s</title></circle>`, x, y, ternary(r.Err > 0, "#dc2626", "#0f766e"), r.At.Format("15:04"), r.Score, html.EscapeString(ternary(r.Note != "", " · "+r.Note, "")+ternary(r.Subject != "", " · "+r.Commit+" "+r.Subject, "")))
		} else {
			fmt.Fprintf(&b, `<line x1="%.1f" y1="%d" x2="%.1f" y2="%d" stroke="#d1d5db" stroke-dasharray="2 3"><title>%s (no score)</title></line>`, x, top, x, h-bottom, r.At.Format("15:04"))
		}
		fmt.Fprintf(&b, `<text x="%.1f" y="%d" font-size="9" text-anchor="middle" fill="#6b7280">%s</text>`, x, h-10, r.At.Format("15:04"))
	}
	if len(pts) > 1 {
		fmt.Fprintf(&b, `<polyline fill="none" stroke="#0f766e" stroke-width="1.5" points="%s"/>`, strings.Join(pts, " "))
	}
	b.WriteString(`</svg><div class="muted">green = pass, red = run with 5xx, dashed = no score entered. Hover a point for commit and note.</div>`)
	return b.String()
}

// flagsFirst lets "-flag value" appear after the positional argument (Go's flag package stops at the first non-flag).
// Only string flags are assumed (every "-x" without "=" consumes the next arg).
func flagsFirst(args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") && a != "-" {
			flags = append(flags, a)
			if !strings.Contains(a, "=") && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		pos = append(pos, a)
	}
	return append(flags, pos...)
}
