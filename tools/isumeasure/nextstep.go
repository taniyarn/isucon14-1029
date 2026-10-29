package main

import (
	"encoding/json"
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

// Next step: which procedure (skill) to run next, from fixed rules over what the report already knows plus the
// run history (panel's runs.json scores, previous runs' commits) and the contest repo (technique-scan / deep-dive
// outputs, app-map). Deterministic and cheap; when the top two are close, the report says to ask Claude (next-step skill).

type nextInput struct {
	ts           string
	t0           time.Time
	hosts        []nextHost
	p1, p2       int
	ciEmpty      bool // -code given but nothing parsed (ops clone not mounted, wrong dir)
	noCode       bool
	noGroups     bool // no alp.yml
	splitEps     int  // max endpoint kinds on a host (> 200 = paths not grouped)
	fixedBench   bool
	benchKnown   bool
	score        float64
	scoreOK      bool
	prevTS       string
	prevScore    float64
	prevScoreOK  bool
	recentScores []float64 // oldest → newest, including this run when known
	commit       string
	prevCommit   string
	repoRoot     string
	topEps       []string // busiest host's top endpoints (param names as :id)
	prizeTarget  float64  // alp.yml prize_target (special prize: first team to reach it)
}

type nextHost struct {
	name      string
	cpuUsed   float64
	verdict   string
	req       int
	errRate   float64
	pdnsShare float64 // PowerDNS share of the host's SQL time (0..1)
	pdnsCPU   float64 // pdns_server % of a core
}

type nextCand struct {
	name, skill, cmd string
	score            int
	reasons          []string
}

func jst(t time.Time) time.Time {
	if loc, err := time.LoadLocation("Asia/Tokyo"); err == nil {
		return t.In(loc)
	}
	return t.Add(9 * time.Hour)
}

func hhmm(t time.Time) int { t = jst(t); return t.Hour()*100 + t.Minute() }

func computeNextStep(in nextInput) []nextCand {
	var cs []nextCand
	add := func(name, skill, cmd string, score int, reasons ...string) {
		cs = append(cs, nextCand{name: name, skill: skill, cmd: cmd, score: score, reasons: reasons})
	}
	now := hhmm(in.t0)
	exists := func(rel string) bool {
		if in.repoRoot == "" {
			return false
		}
		m, _ := filepath.Glob(filepath.Join(in.repoRoot, rel))
		return len(m) > 0
	}
	// 1. measurement is broken: every other judgement is built on it
	var mr []string
	if in.ciEmpty {
		mr = append(mr, "code index is empty: SQL → code and router grouping are off")
	}
	if in.noGroups && in.splitEps > 200 {
		mr = append(mr, fmt.Sprintf("no alp.yml and %d endpoint kinds on one host: the endpoint tables under-count", in.splitEps))
	} else if in.splitEps > 200 {
		mr = append(mr, fmt.Sprintf("%d endpoint kinds on one host: add the Unmatched paths rules to alp.yml", in.splitEps))
	}
	if len(mr) > 0 {
		add("計測を直す", "-", "task groups → commit alp.yml → task ops:repo && task ops:restart → panel の re-report", 100, mr...)
	}
	// 2. errors
	for _, h := range in.hosts {
		if h.errRate >= 1 || h.verdict == "errors" {
			add("deep-dive（5xx の原因）", "deep-dive", "task deepdive TS="+in.ts, 95, fmt.Sprintf("%s: %.1f%% of requests are 5xx (report: Errors section)", h.name, h.errRate))
			break
		}
	}
	// 3. regression after a change
	if in.scoreOK && in.prevScoreOK && in.prevScore > 0 && in.score < in.prevScore*0.9 {
		if in.commit != "" && in.prevCommit != "" && in.commit != in.prevCommit {
			add("revert して切り分け", "-", fmt.Sprintf("git log --merges %s..%s で入った merge を確認 → git revert -m 1 <merge> → deploy → bench", in.prevCommit, in.commit), 90,
				fmt.Sprintf("score %.0f → %.0f (%.0f%%) and the code changed (%s → %s)", in.prevScore, in.score, 100*(in.score/in.prevScore-1), in.prevCommit, in.commit))
		} else {
			add("再計測（変更なしでもう 1 回）", "-", "同じ commit のままベンチをもう 1 回", 85,
				fmt.Sprintf("score %.0f → %.0f with the same commit %s: bench noise or state left from the previous run", in.prevScore, in.score, in.commit))
		}
	}
	// 4. final phase
	if now >= 1630 && now < 1800 {
		add("失格回避フェーズ（再起動試験・計測 OFF）", "-", "RUNBOOK §8: task hosts:measure-off → task db:snapshot LABEL=before → task reboot-test → bench → task db:snapshot LABEL=after", 98, fmt.Sprintf("run started %s JST (after 16:30): stop improving, rehearse the re-test", jst(in.t0).Format("15:04")))
	}
	// 4b. special prize within reach: 3 hosts (app ×2 + DB) and measurement off for one bench (score-sprint skill)
	if in.prizeTarget > 0 && in.scoreOK && in.score >= 0.75*in.prizeTarget && in.score < in.prizeTarget {
		add("特別賞を取りに行く（app 2 台 + 計測 OFF で 1 ベンチ）", "score-sprint", "score-sprint skill で。対象 run は "+in.ts, 92,
			fmt.Sprintf("score %.0f = %.0f%% of the prize target %.0f (alp.yml prize_target): one bench with app ×2 + DB ×1 and logging/measurement off may cross it", in.score, 100*in.score/in.prizeTarget, in.prizeTarget))
	}
	// 5. structure: aux services eating the app host, or one host saturated while another idles
	var busy, idle []string
	for _, h := range in.hosts {
		if h.pdnsShare >= 0.5 || h.pdnsCPU >= 20 {
			add("構成変更: DNS を app の host から外す / 置き換える", "technique-scan", "TECHNIQUES T27（別 host へ）→ 足りなければ T41（最小 DNS を自前実装）", 80,
				fmt.Sprintf("%s: PowerDNS is %.0f%% of the host's SQL time, pdns_server %.0f%% of a core", h.name, 100*h.pdnsShare, h.pdnsCPU))
		}
		if h.cpuUsed >= 90 {
			busy = append(busy, fmt.Sprintf("%s %.0f%%", h.name, h.cpuUsed))
		} else if h.cpuUsed <= 50 {
			idle = append(idle, fmt.Sprintf("%s %.0f%%", h.name, h.cpuUsed))
		}
	}
	if len(busy) > 0 && len(idle) > 0 {
		add("構成変更: 空いている host に処理を移す", "technique-scan", "TECHNIQUES T26（app 2 台 + host 固定の endpoint は nginx で寄せる）/ T27（付随サービス）", 75,
			"CPU used: busy "+strings.Join(busy, ", ")+" / idle "+strings.Join(idle, ", "))
	}
	// 6. keep fixing what the report lists
	if in.p1+in.p2 >= 3 {
		sc := 70
		r := []string{fmt.Sprintf("%d P1 + %d P2 actions in this report", in.p1, in.p2)}
		if in.prevScoreOK && in.scoreOK && in.score > in.prevScore {
			sc += 5
			r = append(r, fmt.Sprintf("the last change helped (%.0f → %.0f)", in.prevScore, in.score))
		}
		add("parallel-fix を続ける", "parallel-fix", "task actions:plan TS="+in.ts+" → parallel-fix skill", sc, r...)
	}
	// 7. technique-scan: the big moves (caches, files out of the DB, host roles, replacing middleware)
	{
		sc, r := 55, []string{}
		done := exists("docs/technique-scan-*.md")
		if in.p1 == 0 {
			sc += 15
			r = append(r, "no P1 action left: index / N+1 fixes are running dry")
		}
		if n := len(in.recentScores); n >= 3 {
			a, b := in.recentScores[n-3], in.recentScores[n-1]
			if a > 0 && b/a < 1.05 && b/a > 0.95 {
				sc += 15
				r = append(r, fmt.Sprintf("score flat over the last 3 runs (%.0f → %.0f)", a, b))
			}
		}
		if now >= 1230 && now < 1630 && !done {
			sc += 15
			r = append(r, "after 12:30 and no docs/technique-scan-*.md yet")
		}
		if done {
			sc -= 20
			r = append(r, "already run once (docs/technique-scan-*.md): re-run only for new evidence")
		}
		if len(r) > 0 {
			add("technique-scan", "technique-scan", "technique-scan skill で。対象 run は "+in.ts, sc, r...)
		}
	}
	// 8. deep-dive: waiting / locks / shape the tables cannot explain
	if len(in.hosts) > 0 {
		b := in.hosts[0]
		sc, r := 50, []string{}
		if b.verdict == "wait" || b.verdict == "lock" || b.verdict == "io" {
			sc += 20
			r = append(r, fmt.Sprintf("%s verdict is %q: the endpoint tables do not show what it waits for", b.name, b.verdict))
		}
		if in.benchKnown && !in.fixedBench {
			sc += 10
			r = append(r, "the bench raises load over the run: find when and why it stops growing")
		}
		if exists("docs/deepdive/" + in.ts + ".md") {
			sc -= 30
			r = append(r, "docs/deepdive/"+in.ts+".md exists")
		}
		if len(r) > 0 {
			add("deep-dive", "deep-dive", "task deepdive TS="+in.ts+" → deep-dive skill", sc, r...)
		}
	}
	// 9. app-analysis: the map is missing or the bench now hits endpoints it does not describe
	if in.repoRoot != "" {
		b, err := os.ReadFile(filepath.Join(in.repoRoot, "docs/app-map.md"))
		if err != nil {
			add("app-analysis", "app-analysis", "app-analysis skill（task app:refresh TS="+in.ts+" で数字だけ更新）", 45, "docs/app-map.md is missing")
		} else {
			mp := routeParamRe.ReplaceAllString(string(b), ":id")
			var miss []string
			for _, ep := range in.topEps {
				path := ep
				if i := strings.IndexByte(ep, ' '); i >= 0 {
					path = ep[i+1:]
				}
				if !strings.Contains(mp, path) {
					miss = append(miss, ep)
				}
			}
			if len(miss) > 0 {
				add("app-analysis を更新", "app-analysis", "task app:refresh TS="+in.ts, 40, "top endpoints not in docs/app-map.md: "+strings.Join(miss, ", "))
			}
		}
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].score > cs[j].score })
	return cs
}

func renderNextStep(md, hb *strings.Builder, cs []nextCand) {
	if len(cs) == 0 {
		return
	}
	e := html.EscapeString
	md.WriteString("## Next step (which procedure to run next — fixed rules; verify against the plan of the day)\n\n| # | next step | skill / command | why |\n|---|---|---|---|\n")
	hb.WriteString(`<section><h2>Next step <span class="muted">· which procedure to run next (fixed rules over this report, the run history and the repo)</span></h2><div class="tw"><table><tr><th>#</th><th>next step</th><th>skill / command</th><th>why</th></tr>`)
	for i, c := range cs {
		if i >= 3 {
			break
		}
		fmt.Fprintf(md, "| %d | **%s** | %s: `%s` | %s |\n", i+1, c.name, c.skill, c.cmd, strings.Join(c.reasons, "; "))
		fmt.Fprintf(hb, `<tr><td class="num">%d</td><td><b>%s</b></td><td><span class="muted">%s</span><br><code>%s</code></td><td>%s</td></tr>`, i+1, e(c.name), e(c.skill), e(c.cmd), e(strings.Join(c.reasons, "; ")))
	}
	hb.WriteString(`</table></div>`)
	if len(cs) >= 2 && cs[0].score-cs[1].score <= 10 {
		md.WriteString("\nThe top two are close: ask Claude (`next-step skill で。対象 run は …`) to pick one with the run history and the journal.\n")
		hb.WriteString(`<div class="muted">The top two are close: ask Claude (<code>next-step skill で。対象 run は …</code>) to pick one with the run history and the journal.</div>`)
	}
	md.WriteString("\n")
	hb.WriteString(`</section>`)
}

// runHistory reads the panel's runs.json (data/<contest>/runs.json next to measurements/) for scores.
func runHistory(dir string) map[string]float64 {
	out := map[string]float64{}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(dir)), "runs.json"))
	if err != nil {
		return out
	}
	var m map[string]map[string]any
	if json.Unmarshal(b, &m) != nil {
		return out
	}
	for ts, n := range m {
		if f, ok := scoreOf(n["score"]); ok {
			out[ts] = f
		}
	}
	return out
}

var scoreNumRe = regexp.MustCompile(`-?[\d,]+(\.\d+)?`)

func scoreOf(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case string:
		s := strings.ReplaceAll(scoreNumRe.FindString(x), ",", "")
		f, err := strconv.ParseFloat(s, 64)
		return f, err == nil && s != ""
	}
	return 0, false
}

// runCommit: the first host's git commit recorded for a run dir.
func runCommit(dir string) string {
	entries, _ := os.ReadDir(dir)
	for _, en := range entries {
		b, err := os.ReadFile(filepath.Join(dir, en.Name(), "run.json"))
		if err != nil {
			continue
		}
		var m map[string]any
		if json.Unmarshal(b, &m) == nil {
			if g, ok := m["git"].(map[string]any); ok && str(g["commit"]) != "" {
				return str(g["commit"])
			}
		}
	}
	return ""
}

func repoRootOf(codeDir string) string {
	if codeDir == "" {
		return ""
	}
	b, err := exec.Command("git", "-c", "safe.directory=*", "-C", codeDir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
