package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// codeIndex maps normalized SQL literals found in Go source to the HTTP routes whose handlers run them
// (directly or through one level of helper functions). Used when queries are not tagged at runtime.
type codeIndex struct {
	routeOf   map[string]string   // handler func -> "METHOD /path" with every param as :id (matching)
	routeRaw  map[string]string   // handler func -> "METHOD /path/:name" as registered (display, alp.yml)
	sqlOf     map[string][]string // func -> normalized SQL literals
	callers   map[string][]string // func -> funcs that call it
	fileOf    map[string]string   // func -> file (relative to the code dir)
	routeSegs [][]string          // lazily built from routeOf for normByRoutes
	bodyOf    map[string]string   // func -> body with whitespace collapsed (fragment search fallback)
	execs     map[string]bool     // funcs that run external commands (exec.Command) — directly or via callees
	sqlLoc    map[string][]string // normalized SQL (no trailing ;) -> "file.go:line" of each literal
	nSQL      int                 // SQL literals found (0 with a non-empty dir = the code index is not usable)
	polls     []pollField         // response fields that set a polling interval (retry_after_ms …) and their literal values
}

var sqlLitRe = regexp.MustCompile(`(?i)^\s*\(?\s*(SELECT|INSERT|UPDATE|DELETE)\b`)

func itoa(n int) string { return strconv.Itoa(n) }

var goFuncRe = regexp.MustCompile(`(?m)^func (?:\([^)]*\) )?(\w+)\(`)
var goRouteRe1 = regexp.MustCompile(`(\w+)\.HandleFunc\("(\w+) ([^"]+)",\s*(\w+)\)`)              // net/http 1.22 mux
var goRouteRe2 = regexp.MustCompile(`(\w+)\.(Get|Post|Put|Delete|Patch)\("([^"]+)",\s*(\w+)\)`)   // chi
var goRouteRe3 = regexp.MustCompile(`(\w+)\.(GET|POST|PUT|DELETE|PATCH)\("([^"]+)",\s*(\w+)[,)]`) // echo / gin
var goGroupVarRe = regexp.MustCompile(`(\w+)\s*:?=\s*(\w+)\.Group\("([^"]*)"`)                    // g := e.Group("/api")
var goRouteClosureRe = regexp.MustCompile(`\.(Route|Group)\("([^"]*)",\s*func\(`)                 // r.Route("/api", func(r chi.Router) {
var braceParamNameRe = regexp.MustCompile(`\{(\w+)[^}]*\}`)

type routeReg struct{ method, path, fn string }

// braceToColon: chi/net-http {id} and {id:[0-9]+} -> :id (echo's :id stays)
func braceToColon(p string) string { return braceParamNameRe.ReplaceAllString(p, ":$1") }

// extractRoutes reads router registrations with their group prefixes: `g := e.Group("/api")` + `g.GET("/x", h)`
// and chi's `r.Route("/api", func(r chi.Router) { r.Get("/x", h) })` (brace depth tracks the closure).
func extractRoutes(src string) []routeReg {
	var out []routeReg
	groupOf := map[string]string{} // var -> full prefix
	type frame struct {
		prefix string
		depth  int
	}
	var stack []frame
	depth := 0
	prefixOfStack := func() string {
		s := ""
		for _, f := range stack {
			s += f.prefix
		}
		return s
	}
	resolve := func(v string) string {
		p, seen := "", 0
		for v != "" && seen < 6 {
			if g, ok := groupOf[v]; ok {
				return g + p
			}
			break
		}
		return p
	}
	for _, line := range strings.Split(src, "\n") {
		code := line
		if i := strings.Index(code, "//"); i >= 0 {
			code = code[:i]
		}
		if m := goGroupVarRe.FindStringSubmatch(code); m != nil {
			groupOf[m[1]] = resolve(m[2]) + m[3] // relative to the enclosing Route closure, if any
		}
		if m := goRouteClosureRe.FindStringSubmatch(code); m != nil {
			stack = append(stack, frame{prefix: m[2], depth: depth + 1})
		}
		for _, m := range goRouteRe1.FindAllStringSubmatch(code, -1) {
			out = append(out, routeReg{m[2], prefixOfStack() + resolve(m[1]) + m[3], m[4]})
		}
		for _, m := range goRouteRe2.FindAllStringSubmatch(code, -1) {
			out = append(out, routeReg{strings.ToUpper(m[2]), prefixOfStack() + resolve(m[1]) + m[3], m[4]})
		}
		for _, m := range goRouteRe3.FindAllStringSubmatch(code, -1) {
			out = append(out, routeReg{m[2], prefixOfStack() + resolve(m[1]) + m[3], m[4]})
		}
		depth += strings.Count(code, "{") - strings.Count(code, "}")
		for len(stack) > 0 && depth < stack[len(stack)-1].depth {
			stack = stack[:len(stack)-1]
		}
	}
	return out
}

var colonParamRe = regexp.MustCompile(`:\w+`)
var goStrRe = regexp.MustCompile("`([^`]*)`|\"((?:[^\"\\\\]|\\\\.)*)\"")
var goCallRe = regexp.MustCompile(`\b([a-zA-Z_]\w*)\(`)

func loadCodeIndex(dir string) *codeIndex {
	ci := &codeIndex{routeOf: map[string]string{}, routeRaw: map[string]string{}, sqlOf: map[string][]string{}, sqlLoc: map[string][]string{}, callers: map[string][]string{}, fileOf: map[string]string{}, bodyOf: map[string]string{}, execs: map[string]bool{}}
	var files []string
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() && (info.Name() == "vendor" || info.Name() == "node_modules" || strings.HasPrefix(info.Name(), ".")) && p != dir {
			return filepath.SkipDir
		}
		if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			files = append(files, p)
		}
		return nil
	})
	ci.polls = scanPollFields(dir, files)
	type fn struct {
		name, body, file string
		line             int // line of the func declaration (1-based)
	}
	var fns []fn
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		src := string(b)
		for _, r := range extractRoutes(src) {
			raw := braceToColon(r.path)
			ci.routeRaw[r.fn] = r.method + " " + raw
			ci.routeOf[r.fn] = r.method + " " + colonParamRe.ReplaceAllString(raw, ":id")
		}
		locs := goFuncRe.FindAllStringSubmatchIndex(src, -1)
		for i, l := range locs {
			end := len(src)
			if i+1 < len(locs) {
				end = locs[i+1][0]
			}
			rel, _ := filepath.Rel(dir, f)
			fns = append(fns, fn{src[l[2]:l[3]], src[l[0]:end], rel, strings.Count(src[:l[0]], "\n") + 1})
			ci.fileOf[src[l[2]:l[3]]] = rel
		}
	}
	names := map[string]bool{}
	for _, f := range fns {
		names[f.name] = true
	}
	for _, f := range fns {
		ci.bodyOf[f.name] = strings.Join(strings.Fields(f.body), " ")
		if strings.Contains(f.body, "exec.Command") {
			ci.execs[f.name] = true
		}
		for _, mi := range goStrRe.FindAllStringSubmatchIndex(f.body, -1) {
			lit := ""
			if mi[2] >= 0 {
				lit = f.body[mi[2]:mi[3]]
			}
			if lit == "" && mi[4] >= 0 {
				lit = f.body[mi[4]:mi[5]]
			}
			if !sqlLitRe.MatchString(lit) {
				continue
			}
			n := normSQL(lit)
			ci.sqlOf[f.name] = append(ci.sqlOf[f.name], n)
			ci.nSQL++
			loc := filepath.Base(f.file) + ":" + itoa(f.line+strings.Count(f.body[:mi[0]], "\n"))
			k := strings.TrimSuffix(n, ";")
			ci.sqlLoc[k] = append(ci.sqlLoc[k], loc)
		}
		for _, m := range goCallRe.FindAllStringSubmatch(f.body, -1) {
			if m[1] != f.name && names[m[1]] {
				ci.callers[m[1]] = append(ci.callers[m[1]], f.name)
			}
		}
	}
	// propagate "shells out" to callers (handler -> helper -> exec.Command)
	for changed := true; changed; {
		changed = false
		for fn := range ci.execs {
			for _, c := range ci.callers[fn] {
				if !ci.execs[c] {
					ci.execs[c] = true
					changed = true
				}
			}
		}
	}
	return ci
}

// funcsByFragment finds functions whose body contains the leading part of a normalized SQL statement
// (fallback when literal normalization differs, e.g. SQL built with Sprintf). Returns at most 3 names.
func (ci *codeIndex) funcsByFragment(norm string) []string {
	frag := strings.ToLower(strings.Join(strings.Fields(norm), " "))
	if i := strings.Index(frag, " where "); i > 20 {
		frag = frag[:i]
	}
	if i := strings.Index(frag, " values "); i > 20 {
		frag = frag[:i]
	}
	if len(frag) > 70 {
		frag = frag[:70]
	}
	if len(frag) < 16 {
		return nil
	}
	var out []string
	for fn, body := range ci.bodyOf {
		if strings.Contains(strings.ToLower(body), frag) {
			out = append(out, fn)
		}
	}
	sort.Strings(out)
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

// handlersFor returns route labels (or function names) that run the given normalized SQL.
func (ci *codeIndex) handlersFor(norm string) []string {
	key := strings.TrimSuffix(strings.TrimSpace(norm), ";")
	set := map[string]bool{}
	for fn, list := range ci.sqlOf {
		hit := false
		for _, l := range list {
			if strings.TrimSuffix(l, ";") == key {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		if r, ok := ci.routeOf[fn]; ok {
			set[r] = true
			continue
		}
		// helper: attribute to its callers that are handlers (one level)
		found := false
		for _, c := range ci.callers[fn] {
			if r, ok := ci.routeOf[c]; ok {
				set[r+" (via "+fn+")"] = true
				found = true
			}
		}
		if !found {
			set[fn] = true
		}
	}
	var out []string
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	if len(out) > 4 {
		out = append(out[:4], "…")
	}
	return out
}

// funcsFor returns the functions whose body contains the given normalized SQL (the place to edit).
func (ci *codeIndex) funcsFor(norm string) []string {
	key := strings.TrimSuffix(strings.TrimSpace(norm), ";")
	var out []string
	for fn, list := range ci.sqlOf {
		for _, l := range list {
			if strings.TrimSuffix(l, ";") == key {
				out = append(out, fn)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// normByRoutes maps an observed path to the router's pattern, params keeping their names
// (/api/user/mtanaka1/icon -> /api/user/:username/icon). "" when no route matches.
func (ci *codeIndex) normByRoutes(path string) string {
	if ci == nil {
		return ""
	}
	if ci.routeSegs == nil {
		seen := map[string]bool{}
		for _, r := range ci.routeRaw {
			sp := strings.SplitN(r, " ", 2)
			if len(sp) != 2 || seen[sp[1]] {
				continue
			}
			seen[sp[1]] = true
			segs := strings.Split(sp[1], "/")
			for i, sg := range segs {
				if sg == "*" || strings.HasPrefix(sg, "*") {
					segs[i] = ":rest"
				}
			}
			ci.routeSegs = append(ci.routeSegs, segs)
		}
	}
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	segs := strings.Split(path, "/")
	best, bestLit := "", -1
	for _, rs := range ci.routeSegs {
		if len(rs) != len(segs) {
			continue
		}
		ok, lit := true, 0
		for i := range rs {
			if strings.HasPrefix(rs[i], ":") {
				continue
			}
			if rs[i] != segs[i] {
				ok = false
				break
			}
			lit++
		}
		if ok && lit > bestLit {
			best, bestLit = strings.Join(rs, "/"), lit
		}
	}
	return best
}
