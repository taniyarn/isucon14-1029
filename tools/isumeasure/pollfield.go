package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// pollfield.go: a server-directed polling interval (ISUCON14: retry_after_ms = 30 in the notification responses)
// decides how hard the bench polls. Raising it from 30 to 100/200 ms was the biggest single win of 2026-10-18
// (+36 %), while the report kept the polling action at P2. The code index finds such fields (json tag looks like
// an interval) and the literal values assigned to them; the report then raises the polling action and shows the
// allowed staleness from docs/app-annotations.json.

type pollField struct {
	json, field string
	value       int
	file        string
	line        int
	fn          string // enclosing func of the assignment
}

var pollTagRe = regexp.MustCompile("(\\w+)\\s+[\\w.*\\[\\]]+\\s+`[^`]*json:\"((?:\\w*(?:retry|poll|interval|wait|refresh)\\w*))[\",]")
var goFuncLineRe = regexp.MustCompile(`^func (?:\([^)]*\) )?(\w+)\(`)

func scanPollFields(dir string, files []string) []pollField {
	srcs := map[string]string{}
	fields := map[string]string{} // Go field -> json name
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		srcs[f] = string(b)
		for _, m := range pollTagRe.FindAllStringSubmatch(string(b), -1) {
			fields[m[1]] = m[2]
		}
	}
	if len(fields) == 0 {
		return nil
	}
	var out []pollField
	for f, src := range srcs {
		fn := ""
		for i, ln := range strings.Split(src, "\n") {
			if m := goFuncLineRe.FindStringSubmatch(ln); m != nil {
				fn = m[1]
			}
			for field, js := range fields {
				re := regexp.MustCompile(`\b` + regexp.QuoteMeta(field) + `\s*[:=]\s*(\d+)\b`)
				if m := re.FindStringSubmatch(ln); m != nil {
					v, _ := strconv.Atoi(m[1])
					rel, _ := filepath.Rel(dir, f)
					out = append(out, pollField{json: js, field: field, value: v, file: rel, line: i + 1, fn: fn})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].file+fmt.Sprint(out[i].line) < out[j].file+fmt.Sprint(out[j].line) })
	return out
}

// pollFieldsFor: the interval fields set by the handler of endpoint key (directly, or in a helper it calls).
func (ci *codeIndex) pollFieldsFor(key string) []pollField {
	if ci == nil || len(ci.polls) == 0 {
		return nil
	}
	handler := ""
	for fn, route := range ci.routeOf {
		if paramKey(route) == paramKey(key) {
			handler = fn
			break
		}
	}
	if handler == "" {
		return nil
	}
	var out []pollField
	for _, p := range ci.polls {
		if p.fn == handler || ci.calledBy(p.fn, handler, 3) {
			out = append(out, p)
		}
	}
	return out
}

func (ci *codeIndex) calledBy(fn, handler string, depth int) bool {
	if depth == 0 {
		return false
	}
	for _, c := range ci.callers[fn] {
		if c == handler || ci.calledBy(c, handler, depth-1) {
			return true
		}
	}
	return false
}

// annotationRules: docs/app-annotations.json "rules" whose endpoints include key and that state a time limit.
func annotationRules(repoRoot, key string) []string {
	if repoRoot == "" {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(repoRoot, "docs", "app-annotations.json"))
	if err != nil {
		return nil
	}
	var doc struct {
		Rules []struct {
			Text      string   `json:"text"`
			Endpoints []string `json:"endpoints"`
		} `json:"rules"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return nil
	}
	timeRe := regexp.MustCompile(`(?i)\d+\s*(?:秒|ms|ミリ秒|s\b|sec|seconds?)|以内|within`)
	var out []string
	for _, r := range doc.Rules {
		for _, ep := range r.Endpoints {
			if paramKey(ep) == paramKey(key) && timeRe.MatchString(r.Text) {
				out = append(out, r.Text)
				break
			}
		}
	}
	return out
}

// applyPollFields rewrites the polling actions with what the code says about the interval.
func applyPollFields(acts []action, ci *codeIndex, repoRoot string) {
	for i := range acts {
		a := &acts[i]
		if a.kind != "polling" {
			continue
		}
		key := strings.TrimPrefix(strings.SplitN(a.title, " at ", 2)[0], "Polling: ")
		pfs := ci.pollFieldsFor(key)
		if len(pfs) == 0 {
			continue
		}
		minV := pfs[0].value
		var locs []string
		for _, p := range pfs {
			if p.value < minV {
				minV = p.value
			}
			locs = append(locs, fmt.Sprintf("%s:%d %s()=%d", p.file, p.line, p.fn, p.value))
		}
		js := pfs[0].json
		info := fmt.Sprintf("The response sets `%s` = %d (%s): the client waits that long before the next poll.", js, minV, strings.Join(locs, ", "))
		if rules := annotationRules(repoRoot, key); len(rules) > 0 {
			info += " Allowed staleness (docs/app-annotations.json): " + strings.Join(rules, " / ") + "."
		} else {
			info += " Check the manual's allowed delay for this endpoint before raising it."
		}
		if minV <= 100 {
			a.priority, a.pinned = "P1", true
			a.title += fmt.Sprintf(" — %s = %d ms", js, minV)
			info += fmt.Sprintf(" %d ms is very short: raise it toward the allowed delay (ISUCON14: 30 → 100/200 ms gave +36 %%), one step per bench, and watch the score.", minV)
		}
		a.detail = info + " " + a.detail
		if a.where == "" {
			a.where = pfs[0].fn + "()"
		}
	}
}

// appPurposes: docs/app-annotations.json "endpoints" (written by app-analysis) → purpose, keyed by paramKey.
func appPurposes(repoRoot string) map[string]string {
	out := map[string]string{}
	if repoRoot == "" {
		return out
	}
	b, err := os.ReadFile(filepath.Join(repoRoot, "docs", "app-annotations.json"))
	if err != nil {
		return out
	}
	var doc struct {
		Endpoints map[string]struct {
			Purpose string `json:"purpose"`
		} `json:"endpoints"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return out
	}
	for k, v := range doc.Endpoints {
		if v.Purpose != "" {
			out[paramKey(k)] = v.Purpose
		}
	}
	return out
}
