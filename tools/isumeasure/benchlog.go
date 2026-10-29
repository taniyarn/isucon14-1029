package main

// Bench output: the organisers' benchmarker prints a short result (score, pass/fail) plus messages — warnings,
// errors, and lines that describe the load model ("負荷レベルが上昇しました", "N% のライドは…に不満"). The portal
// shows them (ISUCON13 de-duplicates them, ISUCON14 prints periodic progress lines); in practice the bench writes
// them to stdout / contestant.log. A human pastes the portal text into the panel (or `task bench:log` uploads a
// file) as <run>/bench.log, and the report condenses it to ≤ 20 lines so nobody has to read the raw log.

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type benchPattern struct {
	re      *regexp.Regexp
	meaning string
}

// builtinBenchPatterns: lines that carry information about the load model or the scoring, seen in 9–14.
// alp.yml `bench_patterns:` adds the contest's own (kickoff phase 1 copies the manual's wording).
var builtinBenchPatterns = []benchPattern{
	{regexp.MustCompile(`(?i)(score|スコア)\s*[:=：]?\s*[\d,]+|"pass"\s*:|^(PASS|FAIL|passed|failed)\b|合格しました|失格`), "結果行"},
	{regexp.MustCompile(`DNS水責め|水責め`), "DNS 攻撃の負荷段階（13。スコアには無関係）"},
	{regexp.MustCompile(`負荷レベルが上昇|負荷が上昇|load level`), "負荷が上がった（アプリが捌けている合図）"},
	{regexp.MustCompile(`レスポンスが遅いため.*上げられません|負荷レベルを上げられません`), "負荷が頭打ち（遅延で増やしてもらえない）"},
	{regexp.MustCompile(`ユーザーが増えません|ユーザが増えません`), "ユーザー増加が止まった（11q 型: trend が変わらない等）"},
	{regexp.MustCompile(`%のライドは`), "不満の内訳（14: マッチ待ち / 迎車 / 実移動）"},
	{regexp.MustCompile(`新規登録しました|利用をやめました`), "ユーザー数の推移（14）"},
	{regexp.MustCompile(`タイムアウト|timeout|Timeout`), "タイムアウト（減点。どの endpoint か）"},
	{regexp.MustCompile(`クリティカル|critical|仕様違反`), "クリティカル（即 FAIL の原因）"},
	{regexp.MustCompile(`整合性.*(失敗|エラー|不正|異な)|validation (failed|error)|検証に失敗|一致しません|期待より古い|不整合`), "整合性チェック NG（キャッシュ・遅延書き込みの副作用）"},
	{regexp.MustCompile(`離脱|退出|諦め|gave up|abandon`), "ユーザーの離脱（失敗したステップの直後。終了時刻と同時なら打ち切りの副作用）"},
	{regexp.MustCompile(`減点対象ではありません|減点されません`), "減点なし（無視してよい）"},
}

type benchLine struct {
	text    string
	count   int
	meaning string
}

type benchSummary struct {
	lines   []benchLine // aggregated, most frequent first
	total   int
	kinds   int
	result  []string // lines that look like the final result (score / pass)
	flagged int      // lines that matched a pattern
	score   int64    // from a {"score": N} / score: N line, -1 when absent
	pass    string   // "true" / "false" / ""
	dnsOK   int64    // ISUCON13 名前解決成功数 (-1 when absent)
	dnsFail int64    // ISUCON13 名前解決失敗数 (-1 when absent)
}

var benchScoreRe = regexp.MustCompile(`(?i)(?:"?score"?|スコア)\s*[:=：]\s*"?(\d[\d,]*)`)
var benchDNSOKRe = regexp.MustCompile(`名前解決成功数\s*[:=：]\s*(\d[\d,]*)|"resolved_count"\s*:\s*(\d+)`)
var benchDNSFailRe = regexp.MustCompile(`名前解決失敗数\s*[:=：]\s*(\d[\d,]*)`)
var benchPassRe = regexp.MustCompile(`(?i)"pass"\s*:\s*(true|false)|\b(PASS|FAIL)\b`)

var (
	benchTSRe   = regexp.MustCompile(`^\s*(\[?\d{4}[-/]\d{2}[-/]\d{2}[ T]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?\]?\s*|\d{2}:\d{2}:\d{2}(?:\.\d+)?\s*)`)
	benchANSIRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	benchLvlRe  = regexp.MustCompile(`^\s*(?:(?:level=)?(?:INFO|WARN|WARNING|ERROR|DEBUG|info|warn|error)\b[:\]]?\s*|\[(?:INFO|WARN|ERROR)\]\s*)(?:[\w.-]*bench[\w.-]*\s+)?`) // level, then a logger name like isupipe-benchmarker
	benchNumRe  = regexp.MustCompile(`\d+(?:[.,]\d+)?`)
	benchIDRe   = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|[0-9A-HJKMNP-TV-Z]{26}|\b[0-9a-f]{16,}\b`)
	benchJSONRe = regexp.MustCompile(`("[^"]+"\s*:\s*)"(?:[^"\\]|\\.)*"`) // JSON string values (user names, ids) → "*" for the grouping key
)

// readBenchLog aggregates <dir>/bench.log: identical messages (after stripping timestamps, log levels, ANSI, ids
// and digits) collapse into one line with a count. The visible text is the first occurrence.
func readBenchLog(path string, extra []benchPattern) *benchSummary {
	return readLogLines(path, nil, extra)
}

// readLogLines is the generic condenser: filter (nil = every line) selects lines, then identical shapes collapse.
func readLogLines(path string, filter *regexp.Regexp, extra []benchPattern) *benchSummary {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	type agg struct {
		first   string
		count   int
		meaning string
	}
	m := map[string]*agg{}
	var order []string
	s := &benchSummary{score: -1, dnsOK: -1, dnsFail: -1}
	pats := append(append([]benchPattern{}, extra...), builtinBenchPatterns...)
	for sc.Scan() {
		raw := strings.TrimSpace(benchANSIRe.ReplaceAllString(sc.Text(), ""))
		if raw == "" || strings.HasPrefix(raw, "{") && strings.HasSuffix(raw, "}") && len(raw) > 2000 {
			continue
		}
		if filter != nil && !filter.MatchString(raw) {
			continue
		}
		if m := benchScoreRe.FindStringSubmatch(raw); m != nil {
			if v, err := strconv.ParseInt(strings.ReplaceAll(m[1], ",", ""), 10, 64); err == nil {
				s.score = v
			}
		}
		if m := benchDNSOKRe.FindStringSubmatch(raw); m != nil {
			s.dnsOK = atoiComma(m[1] + m[2])
		}
		if m := benchDNSFailRe.FindStringSubmatch(raw); m != nil {
			s.dnsFail = atoiComma(m[1])
		}
		if m := benchPassRe.FindStringSubmatch(raw); m != nil {
			if m[1] != "" {
				s.pass = m[1]
			} else {
				s.pass = ternary(strings.ToLower(m[2]) == "pass", "true", "false")
			}
		}
		line := benchLvlRe.ReplaceAllString(benchTSRe.ReplaceAllString(raw, ""), "")
		if line == "" || strings.Trim(line, ".-=_ ") == "" { // progress bars / separators
			continue
		}
		s.total++
		key := benchNumRe.ReplaceAllString(benchIDRe.ReplaceAllString(benchJSONRe.ReplaceAllString(line, `$1"*"`), "<id>"), "N")
		if len(key) > 160 {
			key = key[:160]
		}
		a := m[key]
		if a == nil {
			a = &agg{first: line}
			for _, p := range pats {
				if p.re.MatchString(line) {
					a.meaning = p.meaning
					break
				}
			}
			m[key] = a
			order = append(order, key)
		}
		a.count++
	}
	s.kinds = len(m)
	for _, k := range order {
		a := m[k]
		if a.meaning == "結果行" {
			s.result = append(s.result, a.first)
			continue
		}
		if a.meaning != "" {
			s.flagged += a.count
		}
		if len(a.first) > 200 {
			a.first = a.first[:200] + "…"
		}
		s.lines = append(s.lines, benchLine{text: a.first, count: a.count, meaning: a.meaning})
	}
	// flagged lines first (load model, errors), then by frequency; keep the order of first appearance for ties
	sort.SliceStable(s.lines, func(i, j int) bool {
		fi, fj := s.lines[i].meaning != "", s.lines[j].meaning != ""
		if fi != fj {
			return fi
		}
		return s.lines[i].count > s.lines[j].count
	})
	if len(s.result) > 5 {
		s.result = s.result[:5]
	}
	return s
}

func atoiComma(v string) int64 {
	n, err := strconv.ParseInt(strings.ReplaceAll(v, ",", ""), 10, 64)
	if err != nil {
		return -1
	}
	return n
}

// parseBenchPatterns turns alp.yml `bench_patterns:` ("regex": "meaning") into patterns; bad regexes are skipped.
func parseBenchPatterns(m map[string]string) []benchPattern {
	var out []benchPattern
	for k, v := range m {
		if re, err := regexp.Compile(k); err == nil {
			out = append(out, benchPattern{re, v})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].re.String() < out[j].re.String() })
	return out
}

func (b *benchSummary) markdown() string {
	var sb strings.Builder
	sb.WriteString("## Bench output (bench.log: portal messages / contestant log, de-duplicated)\n\n")
	if b.score >= 0 || b.pass != "" {
		fmt.Fprintf(&sb, "- score %s · pass %s (from the bench's result line)\n", ternary(b.score >= 0, strconv.FormatInt(b.score, 10), "?"), ternary(b.pass != "", b.pass, "?"))
	}
	if b.dnsOK >= 0 || b.dnsFail >= 0 {
		fmt.Fprintf(&sb, "- 名前解決 成功 %d · 失敗 %d (DNS; ISUCON13)\n", b.dnsOK, b.dnsFail)
	}
	for _, r := range b.result {
		fmt.Fprintf(&sb, "- **%s**\n", r)
	}
	fmt.Fprintf(&sb, "- %d lines → %d kinds (%d lines matched a known pattern)\n\n| count | message | meaning |\n|---|---|---|\n", b.total, b.kinds, b.flagged)
	for i, l := range b.lines {
		if i >= 20 {
			fmt.Fprintf(&sb, "| … | %d more kinds | |\n", len(b.lines)-20)
			break
		}
		fmt.Fprintf(&sb, "| %d | %s | %s |\n", l.count, strings.ReplaceAll(l.text, "|", "\\|"), l.meaning)
	}
	sb.WriteString("\n")
	return sb.String()
}
