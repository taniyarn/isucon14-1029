package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"html"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

// codeLinker turns "file.go:123" into a link to the contest repo at the commit the bench ran
// (https://github.com/<o>/<r>/blob/<commit>/<prefix>/file.go#L123). Everything is derived from the code dir's
// own git checkout (remote, path inside the repo) and the run's git commit, so no extra flags are needed.
type codeLinker struct {
	web, prefix string
	commit      string // the run's commit: sqltag caller lines were recorded there
	head        string // the code dir's HEAD: code-index (literal) line numbers were read there
}

var sshRemoteRe = regexp.MustCompile(`^(?:ssh://)?git@([^:/]+)[:/](.+?)(\.git)?$`)

func newCodeLinker(codeDir, commit string) *codeLinker {
	if codeDir == "" || commit == "" {
		return nil
	}
	out := func(args ...string) string {
		b, err := exec.Command("git", append([]string{"-c", "safe.directory=*", "-C", codeDir}, args...)...).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	remote := out("remote", "get-url", "origin")
	web := ""
	if m := sshRemoteRe.FindStringSubmatch(remote); m != nil {
		web = "https://" + m[1] + "/" + m[2]
	} else if strings.HasPrefix(remote, "https://") {
		web = strings.TrimSuffix(remote, ".git")
	}
	if web == "" {
		return nil
	}
	head := out("rev-parse", "--short", "HEAD")
	if head == "" {
		head = commit
	}
	return &codeLinker{web: web, prefix: out("rev-parse", "--show-prefix"), commit: commit, head: head}
}

// url for "file.go:123" (the file name may carry a dir relative to the code dir). exact = a sqltag caller line
// (recorded at the run's commit); otherwise a code-index line (read from the code dir's HEAD).
func (cl *codeLinker) url(loc string, exact bool) string {
	if cl == nil {
		return ""
	}
	file, line := loc, ""
	if i := strings.LastIndex(loc, ":"); i > 0 {
		file, line = loc[:i], loc[i+1:]
	}
	rev := cl.head
	if exact {
		rev = cl.commit
	}
	u := cl.web + "/blob/" + rev + "/" + cl.prefix + file
	if line != "" {
		u += "#L" + line
	}
	return u
}

// sqlCode is where a normalized statement lives in the code: exact call sites from sqltag's " @ file:line"
// (with counts), else the line of the matching SQL literal from the code index, else nothing.
type sqlCode struct {
	locs  []string // "file.go:123"
	count []int    // per loc (exact only)
	exact bool
}

func codeOf(q *sqlStat, ci *codeIndex) sqlCode {
	if len(q.locs) > 0 {
		var c sqlCode
		c.exact = true
		for k := range q.locs {
			c.locs = append(c.locs, k)
		}
		sort.Slice(c.locs, func(i, j int) bool { return q.locs[c.locs[i]] > q.locs[c.locs[j]] })
		for _, k := range c.locs {
			c.count = append(c.count, q.locs[k])
		}
		return c
	}
	if ci != nil {
		seen := map[string]bool{}
		var c sqlCode
		for _, l := range ci.sqlLoc[strings.TrimSuffix(strings.TrimSpace(q.norm), ";")] {
			if !seen[l] {
				seen[l] = true
				c.locs = append(c.locs, l)
			}
		}
		return c
	}
	return sqlCode{}
}

var grepFragRe = regexp.MustCompile(`(?i)\b(FROM|INTO|UPDATE|JOIN)\s+[A-Za-z_\x60][^\s(]*(\s+[A-Za-z_=<>][^\s?(),]*){0,3}`)

// grepFrag returns a run of words from the statement that also appears verbatim in the source
// (no placeholders, no IN lists): `FROM icons WHERE user_id`. Used when the code location is unknown.
func grepFrag(norm string) string {
	w := strings.Fields(strings.TrimRight(grepFragRe.FindString(norm), "=<> "))
	// a trailing operator keyword is often written differently in the source (IN(?) vs IN (...))
	for len(w) > 2 && regexp.MustCompile(`(?i)^(IN|LIKE|IS|NOT|BETWEEN|AND|OR)$`).MatchString(w[len(w)-1]) {
		w = w[:len(w)-1]
	}
	if len(w) < 2 {
		return ""
	}
	return strings.Join(w, " ")
}

// markdown cell: `file.go:12` ×n, … or grep: `git grep -n "FROM t WHERE c"`
func (c sqlCode) md(norm string) string {
	if len(c.locs) == 0 {
		if f := grepFrag(norm); f != "" {
			return fmt.Sprintf("grep: `git grep -n %q`", f)
		}
		return ""
	}
	var parts []string
	for i, l := range c.locs {
		if i >= 3 {
			parts = append(parts, fmt.Sprintf("+%d", len(c.locs)-3))
			break
		}
		if c.exact {
			parts = append(parts, fmt.Sprintf("`%s` ×%d", l, c.count[i]))
		} else {
			parts = append(parts, fmt.Sprintf("`%s`", l))
		}
	}
	s := strings.Join(parts, ", ")
	if !c.exact {
		s += " (literal)"
	}
	return s
}

func (c sqlCode) html(norm string, cl *codeLinker) string {
	if len(c.locs) == 0 {
		if f := grepFrag(norm); f != "" {
			return fmt.Sprintf(`<span class="muted">grep:</span> <code>git grep -n %s</code>`, html.EscapeString(fmt.Sprintf("%q", f)))
		}
		return `<span class="muted">-</span>`
	}
	var parts []string
	for i, l := range c.locs {
		if i >= 3 {
			parts = append(parts, fmt.Sprintf(`<span class="muted">+%d</span>`, len(c.locs)-3))
			break
		}
		t := html.EscapeString(l)
		if u := cl.url(l, c.exact); u != "" {
			t = fmt.Sprintf(`<a href="%s" target="_blank">%s</a>`, html.EscapeString(u), t)
		}
		if c.exact {
			t += fmt.Sprintf(` <span class="muted">×%d</span>`, c.count[i])
		}
		parts = append(parts, t)
	}
	s := strings.Join(parts, "<br>")
	if !c.exact {
		s += ` <span class="muted" title="matched the SQL literal in the code; sqltag's caller location is exact">(literal)</span>`
	}
	return s
}

// anchor ids shared by the endpoint and SQL tables so the two link to each other
func sqlAnchor(host, norm string) string {
	h := sha1.Sum([]byte(norm))
	return "sql-" + host + "-" + hex.EncodeToString(h[:4])
}
func epAnchor(host, key string) string { return "ep-" + host + "-" + slugOf(paramKey(key)) }

// calledFromHTML is calledFrom with each endpoint linked to its row in the endpoint table.
func calledFromHTML(q *sqlStat, epAt map[string]string) string {
	if len(q.tags) == 0 {
		return html.EscapeString(calledFrom(q))
	}
	type kv struct {
		k string
		n int
	}
	var l []kv
	for k, n := range q.tags {
		l = append(l, kv{k, n})
	}
	sort.Slice(l, func(i, j int) bool { return l[i].n > l[j].n })
	var parts []string
	for i, x := range l {
		if i >= 3 {
			parts = append(parts, fmt.Sprintf("+%d", len(l)-3))
			break
		}
		t := fmt.Sprintf("%s ×%d", html.EscapeString(x.k), x.n)
		if a, ok := epAt[paramKey(x.k)]; ok {
			t = `<a href="#` + a + `">` + t + `</a>`
		}
		parts = append(parts, t)
	}
	return strings.Join(parts, ", ")
}
