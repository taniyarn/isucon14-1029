package main

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// deadlock.go: SHOW ENGINE INNODB STATUS keeps the LATEST DETECTED DEADLOCK — the two statements, the locks they
// held and waited for, and which one was rolled back. With sqltag the statements carry "/* METHOD /path @ file:line */",
// so the Errors row ("coordinate 500 × 46, Deadlock found") can say which code collided with which.

type deadlockTx struct {
	n          string // "1" / "2"
	tag, loc   string
	sql, norm  string
	waiting    string // "PRIMARY of `db`.`t` lock_mode X locks gap before rec insert intention waiting"
	rolledBack bool
}

type deadlock struct {
	host, at string
	txs      []deadlockTx
}

var dlTxRe = regexp.MustCompile(`^\*\*\* \((\d+)\) TRANSACTION:`)
var dlWaitRe = regexp.MustCompile(`^\*\*\* \((\d+)\) WAITING FOR THIS LOCK`)
var dlRollRe = regexp.MustCompile(`^\*\*\* WE ROLL BACK TRANSACTION \((\d+)\)`)
var dlLockRe = regexp.MustCompile(`index (\S+) of table (\S+) trx id \d+ (.*)$`)

func parseDeadlock(txt string) *deadlock {
	i := strings.Index(txt, "LATEST DETECTED DEADLOCK")
	if i < 0 {
		return nil
	}
	sec := txt[i:]
	if j := strings.Index(sec, "\nTRANSACTIONS\n"); j > 0 {
		sec = sec[:j]
	}
	lines := strings.Split(sec, "\n")
	dl := &deadlock{}
	if len(lines) > 2 {
		f := strings.Fields(lines[2])
		if len(f) >= 2 {
			dl.at = f[0] + " " + f[1]
		}
	}
	var cur *deadlockTx
	waitFor := ""
	for k := 0; k < len(lines); k++ {
		ln := lines[k]
		if m := dlTxRe.FindStringSubmatch(ln); m != nil {
			dl.txs = append(dl.txs, deadlockTx{n: m[1]})
			cur = &dl.txs[len(dl.txs)-1]
			continue
		}
		if cur != nil && cur.sql == "" && strings.HasPrefix(ln, "MySQL thread id") {
			var q []string
			for k+1 < len(lines) && strings.TrimSpace(lines[k+1]) != "" && !strings.HasPrefix(lines[k+1], "***") {
				k++
				q = append(q, strings.TrimSpace(lines[k]))
			}
			cur.tag, cur.loc, cur.sql = splitTag(strings.Join(q, " "))
			cur.norm = normSQL(cur.sql)
			continue
		}
		if m := dlWaitRe.FindStringSubmatch(ln); m != nil {
			waitFor = m[1]
			continue
		}
		if waitFor != "" {
			if m := dlLockRe.FindStringSubmatch(ln); m != nil {
				for t := range dl.txs {
					if dl.txs[t].n == waitFor {
						dl.txs[t].waiting = m[1] + " of " + m[2] + " " + m[3]
					}
				}
				waitFor = ""
			}
		}
		if m := dlRollRe.FindStringSubmatch(ln); m != nil {
			for t := range dl.txs {
				if dl.txs[t].n == m[1] {
					dl.txs[t].rolledBack = true
				}
			}
		}
	}
	if len(dl.txs) == 0 {
		return nil
	}
	return dl
}

// hostDeadlocks: the deadlocks a host's status snapshots show (mid-run and end; the same one twice counts once).
func hostDeadlocks(host, dir string) []deadlock {
	var out []deadlock
	seen := map[string]bool{}
	for _, f := range []string{"innodb-status-mid.txt", "innodb-status.txt"} {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			continue
		}
		if dl := parseDeadlock(string(b)); dl != nil {
			k := dl.at + dl.txs[0].sql
			if seen[k] {
				continue
			}
			seen[k] = true
			dl.host = host
			out = append(out, *dl)
		}
	}
	return out
}

func (t deadlockTx) where() string {
	w := t.tag
	if t.loc != "" {
		w += ternary(w != "", " @ ", "") + t.loc
	}
	return ternary(w != "", w, "(untagged)")
}

// summary: "`stmt1` (POST /x @ a.go:1) ⟷ `stmt2` (…)" for the Errors row.
func (d deadlock) summary() string {
	var parts []string
	for _, t := range d.txs {
		parts = append(parts, fmt.Sprintf("`%s` (%s%s)", trunc(t.norm, 90), t.where(), ternary(t.rolledBack, ", rolled back", "")))
	}
	return strings.Join(parts, " ⟷ ")
}

// matches: the deadlock involves a statement issued by endpoint key (tag is the route without :id distinction).
func (d deadlock) matches(key string) bool {
	for _, t := range d.txs {
		if t.tag != "" && paramKey(routeParamRe.ReplaceAllString(t.tag, ":id")) == paramKey(key) {
			return true
		}
	}
	return false
}

func renderDeadlocks(md, hb *strings.Builder, dls []deadlock) {
	if len(dls) == 0 {
		return
	}
	md.WriteString("Deadlocks (LATEST DETECTED DEADLOCK of SHOW ENGINE INNODB STATUS; the newest one only — MySQL keeps one):\n\n| host | at | tx | statement | issued by | waiting for | rolled back |\n|---|---|---|---|---|---|---|\n")
	hb.WriteString(`<div class="muted">Deadlocks (LATEST DETECTED DEADLOCK of SHOW ENGINE INNODB STATUS — MySQL keeps only the newest)</div><div class="tw"><table><tr><th>host</th><th>at</th><th>tx</th><th>statement</th><th>issued by</th><th>waiting for</th><th>rolled back</th></tr>`)
	for _, d := range dls {
		for _, t := range d.txs {
			fmt.Fprintf(md, "| %s | %s | (%s) | `%s` | %s | %s | %s |\n", d.host, d.at, t.n, strings.ReplaceAll(trunc(t.norm, 160), "|", "\\|"), t.where(), strings.ReplaceAll(t.waiting, "|", "/"), ternary(t.rolledBack, "yes", ""))
			fmt.Fprintf(hb, `<tr><td>%s</td><td>%s</td><td>(%s)</td><td><code>%s</code></td><td>%s</td><td class="muted">%s</td><td class="%s">%s</td></tr>`, html.EscapeString(d.host), html.EscapeString(d.at), t.n, html.EscapeString(trunc(t.norm, 200)), html.EscapeString(t.where()), html.EscapeString(t.waiting), ternary(t.rolledBack, "bad", ""), ternary(t.rolledBack, "yes", ""))
		}
	}
	md.WriteString("\nTwo of the same statement (e.g. INSERT … into a gap, SELECT … FOR UPDATE on a non-unique key) = gap locks: lock by primary key, make the row exist first (INSERT … ON DUPLICATE KEY UPDATE), or drop FOR UPDATE where a unique key already guards it.\n\n")
	hb.WriteString(`</table></div><div class="muted">two of the same statement (INSERT into a gap, FOR UPDATE on a non-unique key) = gap locks: lock by primary key, make the row exist first (INSERT … ON DUPLICATE KEY UPDATE), or drop FOR UPDATE where a unique key already guards it</div>`)
}
