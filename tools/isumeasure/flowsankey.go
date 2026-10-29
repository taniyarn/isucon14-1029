package main

import (
	"flag"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// runFlows writes <dir>/flows.html (a Sankey diagram of what the bench's clients do) and <dir>/flows.md (the same
// links as a table, for Claude). A Sankey cannot draw loops, so each client's first -depth steps are unrolled into
// columns ("1 GET /api/x" → "2 POST /api/y" …); a client that stops earlier flows into "(end: ok|4xx|5xx)".
// -from starts every client at its first request to that endpoint (to see the steady loop instead of the login).
// Runs of the same endpoint are collapsed into one step, as in the report's Flows section.
// The HTML loads Mermaid from a CDN: open it on your machine (not served by the ops panel).
func runFlows(args []string) error {
	fs := flag.NewFlagSet("flows", flag.ContinueOnError)
	code := fs.String("code", envOr("CODE_DIR", ""), "app source dir: same router-based grouping as the report")
	groups := fs.String("groups", "", "alp.yml (default: next to / above -code, or ./alp.yml)")
	host := fs.String("host", "", "host whose access log to use (default: the one with the most requests)")
	depth := fs.Int("depth", 8, "steps per client to draw")
	from := fs.String("from", "", "start each client at its first request to this endpoint (e.g. 'GET /api/livestream/:livestream_id/livecomment')")
	role := fs.String("role", "", "only sessions whose cookie name is this (e.g. the viewer's cookie)")
	minPct := fs.Float64("min", 1.5, "hide links carrying less than this % of clients")
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			flags = append(flags, args[i])
			if !strings.Contains(args[i], "=") && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
		} else {
			pos = append(pos, args[i])
		}
	}
	if err := fs.Parse(append(flags, pos...)); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: isumeasure flows <measurements/<ts> dir> [-depth 8] [-from 'METHOD /path'] [-role cookie] [-host svc1]")
	}
	dir := fs.Arg(0)
	var ci *codeIndex
	if *code != "" {
		ci = loadCodeIndex(*code)
	}
	gcfg, err := loadGroups(findGroupsFile(*groups, *code))
	if err != nil {
		return err
	}
	nz := newNormalizer(gcfg.groups, gcfg.src, ci)
	var hosts []*hostRun
	entries, _ := os.ReadDir(dir)
	for _, en := range entries {
		if en.IsDir() && en.Name() != "export" {
			if h, err := loadHostRun(filepath.Join(dir, en.Name()), en.Name()); err == nil {
				hosts = append(hosts, h)
			}
		}
	}
	var h *hostRun
	for _, x := range hosts {
		if (*host != "" && x.Name == *host) || (*host == "" && (h == nil || len(x.Access) > len(h.Access))) {
			h = x
		}
	}
	if h == nil || len(h.Access) == 0 {
		return fmt.Errorf("no access log under %s", dir)
	}
	bySess := map[string][]accessEvent{}
	for _, ev := range h.Access {
		if ev.sess == "" || isStatic(ev.uri) || strings.Contains(ev.uri, "/initialize") {
			continue
		}
		if *role != "" && !strings.HasPrefix(ev.sess, *role+"=") {
			continue
		}
		bySess[ev.sess] = append(bySess[ev.sess], ev)
	}
	start := func(ev accessEvent) time.Time { return ev.at.Add(-time.Duration(ev.reqtime * float64(time.Second))) }
	type link struct {
		src, dst string
		n        int
	}
	links := map[string]*link{}
	add := func(a, b string) {
		k := a + "\x00" + b
		if links[k] == nil {
			links[k] = &link{src: a, dst: b}
		}
		links[k].n++
	}
	// labels: drop /api, parameter names -> :id (the report keeps the full names)
	short := func(ep string) string {
		return routeParamRe.ReplaceAllString(strings.Replace(ep, " /api/", " /", 1), ":id")
	}
	clients := 0
	fromKey := paramKey(*from)
	for _, list := range bySess {
		sort.Slice(list, func(i, j int) bool { return start(list[i]).Before(start(list[j])) })
		var steps []string
		var last accessEvent
		for _, ev := range list {
			ep := ev.method + " " + nz.normQuiet(ev.uri)
			if len(steps) == 0 && *from != "" && paramKey(ep) != fromKey {
				continue
			}
			if n := len(steps); n > 0 && steps[n-1] == ep {
				last = ev
				continue
			}
			if len(steps) == *depth {
				break
			}
			steps = append(steps, ep)
			last = ev
		}
		if len(steps) == 0 {
			continue
		}
		clients++
		for i := 1; i < len(steps); i++ {
			add(fmt.Sprintf("%d %s", i, short(steps[i-1])), fmt.Sprintf("%d %s", i+1, short(steps[i])))
		}
		if len(steps) < *depth {
			add(fmt.Sprintf("%d %s", len(steps), short(steps[len(steps)-1])), "(end: "+statusClass(last.status)+")")
		}
	}
	var ls []*link
	hidden := 0
	for _, l := range links {
		if float64(l.n) < *minPct/100*float64(clients) {
			hidden += l.n
			continue
		}
		ls = append(ls, l)
	}
	sort.Slice(ls, func(i, j int) bool { return ls[i].n > ls[j].n })
	// height: enough room for the busiest column (≈ 40 px per node)
	perCol := map[string]map[string]bool{}
	for _, l := range ls {
		for _, nd := range []string{l.src, l.dst} {
			c := strings.SplitN(nd, " ", 2)[0]
			if perCol[c] == nil {
				perCol[c] = map[string]bool{}
			}
			perCol[c][nd] = true
		}
	}
	height := 600
	for _, m := range perCol {
		if hh := 40 * len(m); hh > height {
			height = hh
		}
	}
	title := fmt.Sprintf("Flows %s · %s · %d clients · first %d steps", filepath.Base(dir), h.Name, clients, *depth)
	if *from != "" {
		title += " from " + *from
	}
	if *role != "" {
		title += " · role " + *role
	}
	// flows.md: the same links as a table (Claude reads this, not the picture)
	var md strings.Builder
	fmt.Fprintf(&md, "# %s\n\n推測 — clients are session cookies; runs of the same endpoint are one step. %d client-steps in links under %.1f%% of clients are hidden.\n\n| from (step) | to (step) | clients |\n|---|---|---|\n", title, hidden, *minPct)
	for _, l := range ls {
		fmt.Fprintf(&md, "| %s | %s | %d |\n", l.src, l.dst, l.n)
	}
	// flows.html: Mermaid sankey-beta (CSV rows; quote names)
	var csvb strings.Builder
	q := func(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
	for _, l := range ls {
		fmt.Fprintf(&csvb, "%s,%s,%d\n", q(l.src), q(l.dst), l.n)
	}
	page := `<!DOCTYPE html><html lang="ja"><head><meta charset="utf-8"><title>` + html.EscapeString(title) + `</title>
<style>body{font:13px system-ui,sans-serif;margin:16px;color:#111}h1{font-size:16px}.muted{color:#666}</style></head><body>
<h1>` + html.EscapeString(title) + `</h1>
<div class="muted">推測 — one client = one session cookie; runs of the same endpoint (icon lists, polling) are one step; "N" = step number; links under ` + fmt.Sprintf("%.1f", *minPct) + `% of clients hidden (` + fmt.Sprint(hidden) + ` client-steps). Same data as a table: flows.md. Options: task flows TS=… DEPTH=8 FROM='METHOD /path' ROLE=cookie</div>
<pre class="mermaid">
---
config:
  sankey:
    showValues: true
    width: 1800
    height: ` + fmt.Sprint(height) + `
    linkColor: source
---
sankey-beta
` + html.EscapeString(csvb.String()) + `</pre>
<script type="module">import mermaid from "https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.esm.min.mjs"; mermaid.initialize({ startOnLoad: true });</script>
</body></html>`
	if err := os.WriteFile(filepath.Join(dir, "flows.html"), []byte(page), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "flows.md"), []byte(md.String()), 0644); err != nil {
		return err
	}
	fmt.Printf("%s/flows.html (open in a browser)\n%s/flows.md (%d links, %d clients)\n", dir, dir, len(ls), clients)
	return nil
}
