package main

// Endpoint grouping, shared by report, export and the ops panel — one normalizer, one answer everywhere:
//   (a) alp.yml in the contest repo  — alp-compatible `matching_groups` (first match wins) + isumeasure's `names`
//   (b) the app's router (code index) — /api/user/mtanaka1/icon -> /api/user/:username/icon
//   (c) generic id substitution       — numbers / UUID / ULID / long hex path segments -> :id
// `isumeasure groups -code webapp/go > alp.yml` writes (a) from (b) so the ops host groups the same way even
// without the code, and `alp ltsv --config alp.yml` groups the same way too.

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type epGroup struct {
	re   *regexp.Regexp
	name string
}

type normHit struct {
	path  string
	stage int // 1 = alp.yml, 2 = router, 3 = id substitution only
}

type normalizer struct {
	groups []epGroup
	ci     *codeIndex
	src    string // where the groups came from, for the report header
	cache  map[string]normHit
	loose  map[string]int // stage-3 results -> requests (feeds "Unmatched paths")
}

const normCacheMax = 200000

func newNormalizer(groups []epGroup, src string, ci *codeIndex) *normalizer {
	return &normalizer{groups: groups, ci: ci, src: src, cache: map[string]normHit{}, loose: map[string]int{}}
}

// norm returns the endpoint path for an observed URI (query stripped). Call it once per request (it also
// counts stage-3 results for "Unmatched paths"); normQuiet is the same lookup without the counting.
func (n *normalizer) norm(uri string) string {
	h, _ := n.normStage(uri, true)
	return h
}

func (n *normalizer) normQuiet(uri string) string {
	h, _ := n.normStage(uri, false)
	return h
}

func (n *normalizer) normStage(uri string, count bool) (string, int) {
	p := uri
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	if h, ok := n.cache[p]; ok {
		if h.stage == 3 && count {
			n.loose[h.path]++
		}
		return h.path, h.stage
	}
	h := normHit{stage: 3}
	for _, g := range n.groups {
		if g.re.MatchString(p) {
			h = normHit{path: g.name, stage: 1}
			break
		}
	}
	if h.stage == 3 && n.ci != nil {
		if r := n.ci.normByRoutes(p); r != "" {
			h = normHit{path: r, stage: 2}
		}
	}
	if h.stage == 3 {
		h.path = normID(p)
		if count && (len(n.loose) < normCacheMax/2 || n.loose[h.path] > 0) {
			n.loose[h.path]++
		}
	}
	if len(n.cache) < normCacheMax {
		n.cache[p] = h
	}
	return h.path, h.stage
}

var idSegRe = regexp.MustCompile(`^([0-9A-HJKMNP-TV-Z]{26}|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|[0-9a-f]{16,}|\d+)$`)

// normID is stage (c): id-looking path segments -> :id. Usernames and slugs stay as they are.
func normID(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		if idSegRe.MatchString(s) {
			segs[i] = ":id"
		} else if j := strings.LastIndexByte(s, '.'); j > 0 && idSegRe.MatchString(s[:j]) {
			segs[i] = ":id" + s[j:]
		}
	}
	return strings.Join(segs, "/")
}

// paramKey makes two endpoint spellings comparable (:username vs :id): the sqltag join and the handler lookup use it.
var routeParamRe = regexp.MustCompile(`:\w+`)

func paramKey(k string) string { return routeParamRe.ReplaceAllString(k, ":id") }

// describe: one line for the report header.
func (n *normalizer) describe() string {
	parts := []string{}
	if len(n.groups) > 0 {
		parts = append(parts, fmt.Sprintf("alp.yml %d rules (%s)", len(n.groups), n.src))
	} else {
		parts = append(parts, "no alp.yml")
	}
	if n.ci != nil {
		parts = append(parts, fmt.Sprintf("router %d routes", len(n.ci.routeOf)))
	} else {
		parts = append(parts, "no code index")
	}
	parts = append(parts, "id substitution")
	return strings.Join(parts, " → ")
}

// unmatched: families of stage-3 paths that look like one endpoint with a variable segment
// (/api/user/*/icon seen with 39 different second segments) — candidates for alp.yml.
type unmatchedFamily struct {
	pattern  string // /api/user/*/icon
	distinct int
	requests int
	examples []string
	regex    string // ^/api/user/[^/]+/icon$
	name     string // /api/user/:x/icon
}

func (n *normalizer) unmatched(minDistinct int) []unmatchedFamily {
	type fam struct {
		members  map[string]int
		requests int
	}
	fams := map[string]*fam{}
	for p, c := range n.loose {
		segs := strings.Split(p, "/")
		if len(segs) < 3 { // "/x" has nothing to vary
			continue
		}
		for i := 2; i < len(segs); i++ { // vary one segment at a time (never the first component, /api)
			if segs[i] == "" || strings.HasPrefix(segs[i], ":") {
				continue
			}
			k := strings.Join(append(append(append([]string{}, segs[:i]...), "*"), segs[i+1:]...), "/")
			f := fams[k]
			if f == nil {
				f = &fam{members: map[string]int{}}
				fams[k] = f
			}
			f.members[p] += c
			f.requests += c
		}
	}
	var out []unmatchedFamily
	for k, f := range fams {
		if len(f.members) < minDistinct {
			continue
		}
		var ex []string
		for m := range f.members {
			ex = append(ex, m)
		}
		sort.Slice(ex, func(i, j int) bool { return f.members[ex[i]] > f.members[ex[j]] })
		if len(ex) > 3 {
			ex = ex[:3]
		}
		segs := strings.Split(k, "/")
		var re, name []string
		for _, s := range segs {
			switch {
			case s == "*":
				re, name = append(re, "[^/]+"), append(name, ":x")
			case s == ":id":
				re, name = append(re, `[^/]+`), append(name, ":id")
			default:
				re, name = append(re, regexp.QuoteMeta(s)), append(name, s)
			}
		}
		out = append(out, unmatchedFamily{pattern: k, distinct: len(f.members), requests: f.requests, examples: ex, regex: "^" + strings.Join(re, "/") + "$", name: strings.Join(name, "/")})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].requests != out[j].requests {
			return out[i].requests > out[j].requests
		}
		return out[i].pattern < out[j].pattern
	})
	// a path can sit in two families (/a/*/c and /a/b/*); keep the one that collapses more members
	seen := map[string]bool{}
	var dedup []unmatchedFamily
	for _, f := range out {
		if seen[f.pattern] {
			continue
		}
		seen[f.pattern] = true
		dedup = append(dedup, f)
	}
	return dedup
}

// ---------- alp.yml ----------

// findGroupsFile: explicit path, $ALP_YML, then alp.yml next to / above the code dir, then the current dir.
func findGroupsFile(explicit, codeDir string) string {
	if explicit != "" {
		return explicit
	}
	if p := os.Getenv("ALP_YML"); p != "" {
		return p
	}
	var cands []string
	if codeDir != "" {
		cands = append(cands, filepath.Join(codeDir, "alp.yml"), filepath.Join(codeDir, "..", "alp.yml"), filepath.Join(codeDir, "..", "..", "alp.yml"))
	}
	cands = append(cands, "alp.yml")
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

// loadGroups reads alp.yml (matching_groups + names) or a legacy alp-groups.txt (one regex per line).
// The YAML reader covers exactly the subset the generator writes: two top-level keys, a list of scalars and a
// map of scalars, with "double", 'single' or plain quoting and # comments.
type groupsCfg struct {
	groups        []epGroup
	src           string
	scorePost     string            // alp.yml `score_post:` — the POST that earns score; the panel shows score ÷ its count per run
	prizeTarget   float64           // alp.yml `prize_target:` — the special prize's score (first team to reach it); 0 = unknown
	benchPatterns map[string]string // alp.yml `bench_patterns:` — "regex": "meaning" for this contest's bench messages
}

func loadGroups(path string) (groupsCfg, error) {
	var cfg groupsCfg
	if path == "" {
		return cfg, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return cfg, err
	}
	defer f.Close()
	var order []string
	names := map[string]string{}
	legacy := !strings.HasSuffix(path, ".yml") && !strings.HasSuffix(path, ".yaml")
	section := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := stripYAMLComment(sc.Text())
		if strings.TrimSpace(line) == "" {
			continue
		}
		if legacy {
			order = append(order, strings.TrimSpace(line))
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, "-") {
			section = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ":"))
			if i := strings.Index(line, ":"); i >= 0 {
				section = strings.TrimSpace(line[:i])
				if section == "score_post" {
					cfg.scorePost = yamlScalar(line[i+1:])
				}
				if section == "prize_target" {
					if v, err := strconv.ParseFloat(strings.ReplaceAll(yamlScalar(line[i+1:]), ",", ""), 64); err == nil {
						cfg.prizeTarget = v
					}
				}
			}
			continue
		}
		t := strings.TrimSpace(line)
		switch section {
		case "matching_groups":
			if strings.HasPrefix(t, "- ") || t == "-" {
				if v := yamlScalar(strings.TrimSpace(t[1:])); v != "" {
					order = append(order, v)
				}
			}
		case "names":
			k, v, ok := yamlPair(t)
			if ok {
				names[k] = v
			}
		case "bench_patterns":
			if k, v, ok := yamlPair(t); ok {
				if cfg.benchPatterns == nil {
					cfg.benchPatterns = map[string]string{}
				}
				cfg.benchPatterns[k] = v
			}
		}
	}
	for _, r := range order {
		re, err := regexp.Compile(r)
		if err != nil {
			return cfg, fmt.Errorf("%s: bad regex %q: %v", path, r, err)
		}
		name := names[r]
		if name == "" {
			name = autoName(r)
		}
		cfg.groups = append(cfg.groups, epGroup{re: re, name: name})
	}
	cfg.src = filepath.Base(path)
	if legacy {
		cfg.src += " (legacy)"
	}
	return cfg, nil
}

func stripYAMLComment(line string) string {
	inS, inD := false, false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\'':
			if !inD {
				inS = !inS
			}
		case '"':
			if !inS && (i == 0 || line[i-1] != '\\') {
				inD = !inD
			}
		case '#':
			if !inS && !inD && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t') {
				return line[:i]
			}
		}
	}
	return line
}

func yamlScalar(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		b := s[1 : len(s)-1]
		b = strings.ReplaceAll(b, `\"`, `"`)
		b = strings.ReplaceAll(b, `\\`, `\`)
		return b
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'")
	}
	return s
}

// yamlPair splits `key: value` where the key may be quoted (regexes contain ':' only inside quotes).
func yamlPair(t string) (string, string, bool) {
	if t == "" {
		return "", "", false
	}
	if t[0] == '"' || t[0] == '\'' {
		q := t[0]
		for i := 1; i < len(t); i++ {
			if t[i] == q && (q == '\'' || t[i-1] != '\\') {
				if q == '\'' && i+1 < len(t) && t[i+1] == '\'' {
					i++
					continue
				}
				rest := strings.TrimSpace(t[i+1:])
				if !strings.HasPrefix(rest, ":") {
					return "", "", false
				}
				return yamlScalar(t[:i+1]), yamlScalar(rest[1:]), true
			}
		}
		return "", "", false
	}
	i := strings.Index(t, ": ")
	if i < 0 {
		if strings.HasSuffix(t, ":") {
			return yamlScalar(t[:len(t)-1]), "", true
		}
		return "", "", false
	}
	return yamlScalar(t[:i]), yamlScalar(t[i+2:]), true
}

var autoNameRe = regexp.MustCompile(`\[\^/\]\+|\[\^/\]\*|\\d\+|\[0-9\]\+|\\w\+|\.\+|\.\*|\(\.\*\)|\(\[\^/\]\+\)`)

// autoName turns a regex into a readable endpoint name when alp.yml gives none: ^/api/user/[^/]+/icon$ -> /api/user/:id/icon
func autoName(re string) string {
	s := strings.TrimSuffix(strings.TrimPrefix(re, "^"), "$")
	s = autoNameRe.ReplaceAllString(s, ":id")
	s = regexp.MustCompile(`\\(.)`).ReplaceAllString(s, "$1")
	return s
}

// ---------- generator: isumeasure groups -code webapp/go ----------

func runGroups(args []string) error {
	fs := flag.NewFlagSet("groups", flag.ContinueOnError)
	code := fs.String("code", envOr("CODE_DIR", ""), "app source dir (Go): routes are read from the router registrations")
	out := fs.String("out", "", "write here instead of stdout (e.g. alp.yml at the repo root)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *code == "" {
		return fmt.Errorf("usage: isumeasure groups -code webapp/go [-out alp.yml]")
	}
	ci := loadCodeIndex(*code)
	if len(ci.routeRaw) == 0 {
		return fmt.Errorf("no routes found under %s (echo/chi/gin/HandleFunc registrations)", *code)
	}
	text := groupsYAML(ci)
	if *out == "" {
		fmt.Print(text)
		return nil
	}
	return os.WriteFile(*out, []byte(text), 0644)
}

type routeSpec struct {
	path    string
	methods []string
	params  int
}

func routeSpecs(ci *codeIndex) []routeSpec {
	byPath := map[string]*routeSpec{}
	for _, r := range ci.routeRaw {
		sp := strings.SplitN(r, " ", 2)
		if len(sp) != 2 {
			continue
		}
		s := byPath[sp[1]]
		if s == nil {
			s = &routeSpec{path: sp[1]}
			for _, sg := range strings.Split(sp[1], "/") {
				if strings.HasPrefix(sg, ":") || sg == "*" {
					s.params++
				}
			}
			byPath[sp[1]] = s
		}
		s.methods = append(s.methods, sp[0])
	}
	var out []routeSpec
	for _, s := range byPath {
		sort.Strings(s.methods)
		out = append(out, *s)
	}
	// first match wins: literal routes before the parameterized siblings that would swallow them,
	// then deeper paths first, then alphabetical (stable, diff-friendly)
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.params != b.params {
			return a.params < b.params
		}
		na, nb := strings.Count(a.path, "/"), strings.Count(b.path, "/")
		if na != nb {
			return na > nb
		}
		return a.path < b.path
	})
	return out
}

func routeRegex(path string) string {
	segs := strings.Split(path, "/")
	parts := make([]string, len(segs))
	for i, s := range segs {
		switch {
		case s == "*" || strings.HasPrefix(s, "*"):
			parts[i] = ".*"
		case strings.HasPrefix(s, ":"):
			parts[i] = "[^/]+"
		default:
			parts[i] = regexp.QuoteMeta(s)
		}
	}
	return "^" + strings.Join(parts, "/") + "$"
}

func groupsYAML(ci *codeIndex) string {
	var b strings.Builder
	b.WriteString("# Endpoint groups for alp and isumeasure. Generated by `isumeasure groups -code webapp/go`; edit freely.\n")
	b.WriteString("# Order matters: the first matching regex wins, so literal paths (/api/user/me) sit above the parameterized\n")
	b.WriteString("# siblings (/api/user/[^/]+) that would swallow them. Re-generate when routes are added, or append by hand.\n")
	b.WriteString("#   alp:        alp ltsv --config alp.yml -r --sort sum --file access.log\n")
	b.WriteString("#   isumeasure: report / export / the ops panel read this file from the repo root (alp ignores `names`).\n")
	b.WriteString("matching_groups:\n")
	specs := routeSpecs(ci)
	for _, s := range specs {
		fmt.Fprintf(&b, "  - %q   # %s\n", routeRegex(s.path), strings.Join(s.methods, ", "))
	}
	b.WriteString("names:   # isumeasure only: how the group is shown (default: the regex with [^/]+ as :id)\n")
	for _, s := range specs {
		if s.params > 0 {
			fmt.Fprintf(&b, "  %q: %q\n", routeRegex(s.path), s.path)
		}
	}
	b.WriteString("# isumeasure only: the POST that earns score (from the manual's score formula). The panel's Score timeline then\n")
	b.WriteString("# shows score ÷ its count per run, and report.md prints the count. Use the name as shown in the report.\n")
	guess := ""
	for _, s := range specs {
		for _, m := range s.methods {
			if m == "POST" && s.params > 0 && !strings.Contains(s.path, "login") && !strings.Contains(s.path, "register") && !strings.Contains(s.path, "initialize") {
				guess = "POST " + s.path
				break
			}
		}
		if guess != "" {
			break
		}
	}
	if guess == "" {
		guess = "POST /api/..."
	}
	fmt.Fprintf(&b, "# score_post: %q\n", guess)
	b.WriteString("# isumeasure only: the special prize's score from the manual (first team to reach it). The report shows how close each run is;\n")
	b.WriteString("# from 75% on, Next step suggests the score-sprint skill (app x2 + measurement off for one bench).\n# prize_target: 50000\n")
	b.WriteString("# isumeasure only: this contest's bench messages worth flagging in the report's \"Bench output\" (regex: meaning).\n")
	b.WriteString("# bench_patterns:\n#   \"ユーザーが増えません\": \"ユーザー増加が止まった\"\n")
	return b.String()
}
