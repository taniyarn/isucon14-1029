package main

import (
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"
)

// toc.go: a grouped table of contents at the top of report.md / summary.md / report.html, the sections of the body
// reordered to follow the groups (a small group heading before each group; raw data sub-headed per host), and a
// "↑ 目次" link under every section heading. Markdown uses explicit <a id> anchors (GitHub keeps them as
// user-content-* and still resolves #sec-N; the panel serves report.html, which carries the same ids), so the links
// do not depend on how a renderer slugs headings with "·", parentheses or Japanese. Section names and their "## "
// headings are unchanged (skills grep for them).

const tocBack = "[↑ 目次](#toc)"

type tocGroup struct {
	title, purpose string
	sections       []string // label prefixes, in body order
}

// tocGroups: the reading order of a report. A section not listed goes to the last group (raw data, per host).
var tocGroups = []tocGroup{
	{"Verdict", "verdict, next step, errors, difference from the previous run", []string{"Summary", "Verdict", "Next step", "Errors", "Bench output", "vs previous run"}},
	{"What to fix", "heavy work and suggested fixes", []string{"Where the time goes", "Periodic internal endpoints", "Next actions", "Flows"}},
	{"Tuning and data", "settings, data growth, trends", []string{"Tuning", "Data growth", "Per 10 s", "Unmatched paths"}},
	{"Per host", "timeline, endpoints, SQL, pprof", nil},
}

const rawGroup = 3 // index of "Raw data" in tocGroups

// flowsLink replaces the Flows section in report.html (the app-analysis report on the panel draws the same data,
// with roles, under its "bench flows" tab; report-app.js routes tabs by location.hash). md keeps Flows for skills.
const flowsLinkHTML = `<div class="flowslink"><a href="/app/#flows" target="_blank">Bench flows: app-analysis →</a></div>`

// htmlSecMeta: short section titles of report.html and the one-line description under them (Japanese in i18n.go).
// md headings are not touched. Keys are the section label without the host prefix.
var htmlSecMeta = map[string][2]string{
	"Verdict":                 {"Verdict", ""},
	"Next step":               {"Next step", ""},
	"Errors":                  {"Errors", "5xx and their causes"},
	"Bench output":            {"Bench output", ""},
	"vs previous run":         {"vs previous run", ""},
	"Where the time goes":     {"Where the time goes", "endpoints and SQL that take the time"},
	"Next actions":            {"Next actions", ""},
	"Tuning":                  {"Tuning", "settings likely to help"},
	"Data growth":             {"Data growth", "rows per table"},
	"Per 10 s":                {"Requests per 10 s", "requests and avg response time per 10 s for the main endpoints"},
	"Unmatched paths":         {"Unmatched paths", "candidates for alp.yml"},
	"Timeline":                {"Requests and Resources", "CPU, requests and MySQL per 10 s"},
	"Endpoints":               {"Endpoints", ""},
	"Slow queries":            {"Slow queries", ""},
	"Statements seen running": {"Running queries", "processlist sampled every 5 s"},
}

// htmlDrop: sections not rendered in report.html (md keeps them for skills / deep-dive). Flows and the periodic
// internal endpoints are shown by the app-analysis report's flows tab.
var htmlDrop = map[string]bool{"Flows": true, "Periodic internal endpoints": true, "Bench behaviour": true}

// htmlNoteDrop: sections whose old parenthesised explanation only repeats the new description.
var htmlNoteDrop = map[string]bool{"Unmatched paths": true, "Timeline": true, "Statements seen running": true}

var mutedSpanRe = regexp.MustCompile(`<span class="muted">(.*)</span>`)

// shortHTMLHead: the h2 inner html with a short title, and the lines under it (description + the old explanation).
func shortHTMLHead(s *tocSec) (string, string) {
	key := s.label
	if s.host != "" {
		key = strings.TrimPrefix(key, s.host+" · ")
	}
	meta, ok := htmlSecMeta[key]
	if !ok {
		return s.head, ""
	}
	title := html.EscapeString(meta[0])
	if s.host != "" {
		title = html.EscapeString(s.host) + " · " + title
	}
	var sub string
	if meta[1] != "" {
		sub += `<div class="desc">` + html.EscapeString(meta[1]) + `</div>`
	}
	if m := mutedSpanRe.FindStringSubmatch(s.head); m != nil && !htmlNoteDrop[key] {
		sub += `<div class="muted secnote">` + m[1] + `</div>`
	}
	return title, sub
}

// tocLabel: the heading without its parenthesised explanation and markdown punctuation.
func tocLabel(h string) string {
	if i := strings.Index(h, " ("); i > 0 {
		h = h[:i]
	}
	h = strings.NewReplacer("`", "", "*", "", "[", "", "]", "").Replace(h)
	return tocShort(strings.TrimSpace(h))
}

// tocShort drops per-run values from a TOC entry (the TOC lists sections, not numbers).
func tocShort(l string) string {
	if strings.HasPrefix(l, "vs previous run") {
		return "vs previous run"
	}
	return l
}

type tocSec struct {
	label     string
	host      string // raw data: "svc1" of "svc1 · endpoints"
	g, s, ord int
	body      []string // markdown: lines after the heading; html: the chunk after <h2>…</h2>
	head      string   // heading text (md) / h2 inner html
}

// rankSecs sets group / position of every section; raw sections are ordered by host (first appearance), the host's
// timeline first, then the original order.
func rankSecs(secs []*tocSec) {
	hostIdx := map[string]int{}
	for _, s := range secs {
		s.g, s.s = len(tocGroups)-1, -1
		found := false
		for gi, g := range tocGroups {
			for si, p := range g.sections {
				if s.label == p || strings.HasPrefix(s.label, p+" ") {
					s.g, s.s, found = gi, si, true
				}
			}
			if found {
				break
			}
		}
		if found {
			continue
		}
		if i := strings.Index(s.label, " · "); i > 0 {
			s.host = s.label[:i]
			if _, ok := hostIdx[s.host]; !ok {
				hostIdx[s.host] = len(hostIdx)
			}
			s.s = 1000 * (1 + hostIdx[s.host])
			if !strings.HasPrefix(strings.ToLower(s.label[i+len(" · "):]), "timeline") {
				s.s++
			}
		}
	}
	sort.SliceStable(secs, func(i, j int) bool {
		if secs[i].g != secs[j].g {
			return secs[i].g < secs[j].g
		}
		if secs[i].s != secs[j].s {
			return secs[i].s < secs[j].s
		}
		return secs[i].ord < secs[j].ord
	})
}

// addTOCMarkdown reorders the "## " sections (outside code fences) by group, inserts anchors, back links, group and
// host headings, and puts the TOC after the first "# " title line (or at the top).
func addTOCMarkdown(md string) string {
	lines := strings.Split(md, "\n")
	var pre []string
	var secs []*tocSec
	fence, titleAt := false, -1
	for _, ln := range lines {
		if strings.HasPrefix(ln, "```") {
			fence = !fence
		}
		if !fence && len(secs) == 0 && titleAt < 0 && strings.HasPrefix(ln, "# ") {
			pre = append(pre, ln)
			titleAt = len(pre)
			continue
		}
		if !fence && strings.HasPrefix(ln, "## ") {
			text := strings.TrimPrefix(ln, "## ")
			secs = append(secs, &tocSec{label: tocLabel(text), head: text, ord: len(secs)})
			continue
		}
		if len(secs) == 0 {
			pre = append(pre, ln)
		} else {
			secs[len(secs)-1].body = append(secs[len(secs)-1].body, ln)
		}
	}
	if len(secs) == 0 {
		return md
	}
	rankSecs(secs)
	var out []string
	toc := []string{"", `<a id="toc"></a>`, "", "**目次**", ""}
	lastG, lastHost := -1, ""
	var items []string
	flushHost := func() {
		if lastHost != "" && len(items) > 0 {
			toc = append(toc, fmt.Sprintf("   - **%s**: %s", lastHost, strings.Join(items, " · ")))
			items = nil
		}
	}
	flush := func() {
		if lastG < 0 {
			return
		}
		if lastHost != "" {
			flushHost()
			return
		}
		g := tocGroups[lastG]
		toc = append(toc, fmt.Sprintf("%d. **%s** — %s: %s", lastG+1, g.title, g.purpose, strings.Join(items, " · ")))
		items = nil
	}
	for n, s := range secs {
		id := fmt.Sprintf("sec-%d", n+1)
		if s.g != lastG {
			flush()
			lastG, lastHost = s.g, ""
			g := tocGroups[s.g]
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			out = append(out, "---", "", fmt.Sprintf(`<a id="grp-%d"></a>**%d. %s** — %s`, s.g+1, s.g+1, g.title, g.purpose), "")
			if s.g == rawGroup && s.host != "" {
				toc = append(toc, fmt.Sprintf("%d. **%s** — %s", s.g+1, g.title, g.purpose))
			}
		}
		if s.host != "" && s.host != lastHost {
			if lastHost == "" && len(items) > 0 { // raw sections without a host come first
				g := tocGroups[s.g]
				toc = append(toc, fmt.Sprintf("%d. **%s** — %s: %s", s.g+1, g.title, g.purpose, strings.Join(items, " · ")))
				items = nil
			}
			flushHost()
			lastHost = s.host
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			out = append(out, fmt.Sprintf(`<a id="host-%s"></a>**%d. %s › %s**`, s.host, s.g+1, tocGroups[s.g].title, s.host), "")
		}
		label := s.label
		if s.host != "" {
			label = strings.TrimPrefix(label, s.host+" · ")
		}
		items = append(items, fmt.Sprintf("[%s](#%s)", label, id))
		out = append(out, fmt.Sprintf(`## <a id="%s"></a>%s`, id, s.head), "", tocBack)
		out = append(out, s.body...)
	}
	flush()
	toc = append(toc, "")
	if titleAt < 0 {
		titleAt = 0
		toc = toc[1:]
	}
	res := append(append(append(append([]string{}, pre[:titleAt]...), toc...), pre[titleAt:]...), out...)
	return strings.Join(res, "\n")
}

var htmlH2Re = regexp.MustCompile(`<section><h2>(.*?)</h2>`)
var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

func htmlLabel(inner string) string {
	label := inner
	if i := strings.Index(label, `<span class="muted">`); i > 0 {
		label = label[:i]
	}
	label = strings.TrimSpace(html.UnescapeString(htmlTagRe.ReplaceAllString(label, "")))
	return tocShort(strings.TrimSuffix(strings.TrimSpace(strings.SplitN(label, " — ", 2)[0]), " ·"))
}

// addTOCHTML reorders the <section><h2> blocks by group, gives every section an id, a back link under the heading,
// group headings and per-host sub-headings, replaces Flows with a link to the app-analysis report, and returns the
// TOC nav to put at the top of <main>.
func addTOCHTML(body string) (string, string) {
	locs := htmlH2Re.FindAllStringSubmatchIndex(body, -1)
	if len(locs) == 0 {
		return body, ""
	}
	pre := body[:locs[0][0]]
	var secs []*tocSec
	for i, l := range locs {
		end := len(body)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		inner := body[l[2]:l[3]]
		s := &tocSec{label: htmlLabel(inner), head: inner, body: []string{body[l[1]:end]}, ord: i}
		secs = append(secs, s)
	}
	rankSecs(secs)
	kept := secs[:0]
	for _, s := range secs {
		key := s.label
		if s.host != "" {
			key = strings.TrimPrefix(key, s.host+" · ")
		}
		if !htmlDrop[key] && !(s.host != "" && strings.HasPrefix(strings.ToLower(key), "timeline")) { // per-host timeline tables: md only (the charts cover them)
			kept = append(kept, s)
		}
	}
	secs = kept
	var b, nav strings.Builder
	b.WriteString(pre)
	nav.WriteString(`<nav id="toc" class="toc"><b>目次</b>`)
	lastG, lastHost := -1, ""
	flowsDone := false
	endGroup := func() {
		if lastG == 1 && !flowsDone {
			b.WriteString(flowsLinkHTML)
			flowsDone = true
		}
		if lastG >= 0 {
			nav.WriteString(`</div>`)
		}
	}
	for n, s := range secs {
		id := fmt.Sprintf("sec-%d", n+1)
		first := false
		if s.g != lastG {
			if lastG < 1 && s.g > 1 && !flowsDone { // no group 2 at all
				b.WriteString(flowsLinkHTML)
				flowsDone = true
			}
			endGroup()
			lastG, lastHost, first = s.g, "", true
			g := tocGroups[s.g]
			fmt.Fprintf(&b, `<div class="grp" id="grp-%d"><span class="gn">%d.</span> <span>%s</span> <span class="muted">%s</span></div>`, s.g+1, s.g+1, html.EscapeString(g.title), html.EscapeString(g.purpose))
			fmt.Fprintf(&nav, `<div class="tg"><span class="gn">%d.</span> <a class="gt" href="#grp-%d">%s</a> <span class="muted">%s</span><br>`, s.g+1, s.g+1, html.EscapeString(g.title), html.EscapeString(g.purpose))
		}
		label := s.label
		if s.host != "" {
			label = strings.TrimPrefix(label, s.host+" · ")
			if s.host != lastHost {
				lastHost, first = s.host, true
				hid := "host-" + html.EscapeString(s.host)
				fmt.Fprintf(&b, `<div class="hostgrp" id="%s">%d. <span>%s</span> › %s</div>`, hid, s.g+1, html.EscapeString(tocGroups[s.g].title), html.EscapeString(s.host))
				fmt.Fprintf(&nav, `<div class="th"><a class="hn" href="#%s">%s</a> `, hid, html.EscapeString(s.host))
			}
		}
		if !first {
			nav.WriteString(` <span class="sep">·</span> `)
		}
		if m, ok := htmlSecMeta[label]; ok {
			label = m[0]
		}
		fmt.Fprintf(&nav, `<a href="#%s">%s</a>`, id, html.EscapeString(label))
		if s.host != "" && (n+1 == len(secs) || secs[n+1].host != s.host) {
			nav.WriteString(`</div>`)
		}
		head, sub := shortHTMLHead(s)
		fmt.Fprintf(&b, `<section id="%s"><h2>%s</h2><div class="totop"><a href="#toc">↑ 目次</a></div>%s%s`, id, head, sub, s.body[0])
	}
	if lastG <= 1 && !flowsDone {
		b.WriteString(flowsLinkHTML)
		flowsDone = true
	}
	endGroup()
	nav.WriteString(`</nav>`)
	return b.String(), nav.String()
}
