package main

import (
	"bytes"
	"encoding/json"
	"html"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// panelapp.go: /app/ serves the app-analysis report the contest repo publishes (REPO_DIR/docs/reports/latest.html
// and anything next to it), read-only from the clone mounted into the panel container. The cron pull keeps the
// clone fresh, so a pushed refresh is visible here within a minute.
//
// /app/?ref=<branch> serves the same file from origin/<branch> of the clone (git cat-file, read-only), so the report
// can be seen while its PR waits for review (2026-10-25: the app-map PR sat 18 min unmerged and /app/ was a 404).
// /api/app says what exists: main's report (crawl time, commit) and branches carrying a newer one.

const appReportPath = "docs/reports/latest.html"

func appRepoDir() string { return envOr("REPO_DIR", "/opt/isucon-ops/repo") }

func appReportsDir() string {
	return filepath.Join(appRepoDir(), "docs", "reports")
}

// safeJoin resolves rel under root and refuses anything that leaves it (.., absolute paths, symlinks pointing out,
// dotfiles). It returns the real path of an existing file or directory.
func safeJoin(root, rel string) (string, bool) {
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "/")
	if !safeRel(rel) {
		return "", false
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false
	}
	p, err := filepath.EvalSymlinks(filepath.Join(realRoot, filepath.FromSlash(rel)))
	if err != nil {
		return "", false
	}
	if p != realRoot && !strings.HasPrefix(p, realRoot+string(filepath.Separator)) {
		return "", false
	}
	return p, true
}

// safeRel: no "..", no dotfile segments.
func safeRel(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".." || strings.HasPrefix(seg, ".") && seg != "" {
			return false
		}
	}
	return true
}

var refRe = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// validRef: a branch name we are willing to put into a git argument.
func validRef(ref string) bool {
	return len(ref) <= 200 && refRe.MatchString(ref) && !strings.Contains(ref, "..") && !strings.HasPrefix(ref, "-") &&
		!strings.HasPrefix(ref, "/") && !strings.HasSuffix(ref, "/") && !strings.Contains(ref, "//")
}

func gitCmd(args ...string) *exec.Cmd {
	return exec.Command("git", append([]string{"-c", "safe.directory=*", "-C", appRepoDir()}, args...)...)
}

// refOf: ?ref=, or the ref of the page that asked for this file (a relative image/json of latest.html?ref=x
// arrives without the query; the Referer still has it).
func refOf(r *http.Request) string {
	if ref := r.URL.Query().Get("ref"); ref != "" {
		return ref
	}
	if rf, err := url.Parse(r.Referer()); err == nil && strings.HasPrefix(rf.Path, "/app/") && rf.Host == r.Host {
		return rf.Query().Get("ref")
	}
	return ""
}

func handleAppReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "read-only", http.StatusMethodNotAllowed)
		return
	}
	root := appReportsDir()
	rel := strings.TrimPrefix(r.URL.Path, "/app/")
	if rel == "" {
		rel = "latest.html"
	}
	if ref := refOf(r); ref != "" {
		serveAppFromRef(w, r, ref, rel)
		return
	}
	p, ok := safeJoin(root, rel)
	if !ok {
		if rel == "latest.html" {
			msg := "no app-analysis report in " + root + " yet (main): run the app-analysis skill (refresh), push docs/reports/ and merge it; the ops clone pulls main every minute."
			if bs := appStatusCached().Branches; len(bs) > 0 {
				msg += "\n\nNot merged yet, but on a branch:\n"
				for _, b := range bs {
					msg += "  " + b.URL + "   (" + b.Sha + " " + b.Subject + ")\n"
				}
			}
			http.Error(w, msg, http.StatusNotFound)
			return
		}
		http.NotFound(w, r)
		return
	}
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, p)
}

// serveAppFromRef: docs/reports/<rel> as committed on origin/<ref> (read-only; nothing is checked out).
func serveAppFromRef(w http.ResponseWriter, r *http.Request, ref, rel string) {
	rel = strings.TrimPrefix(rel, "/")
	if !validRef(ref) || !safeRel(rel) || path.Clean("docs/reports/"+rel) != "docs/reports/"+rel {
		http.Error(w, "bad ref or path", http.StatusBadRequest)
		return
	}
	spec := "refs/remotes/origin/" + ref + ":docs/reports/" + rel
	if t, err := gitCmd("cat-file", "-t", spec).Output(); err != nil || strings.TrimSpace(string(t)) != "blob" {
		http.Error(w, "no docs/reports/"+rel+" on origin/"+ref+" in the ops clone (pushed? the clone fetches every minute)", http.StatusNotFound)
		return
	}
	b, err := gitCmd("cat-file", "blob", spec).Output()
	if err != nil {
		http.Error(w, "git cat-file failed", http.StatusInternalServerError)
		return
	}
	ct := mime.TypeByExtension(path.Ext(rel))
	if ct == "" {
		ct = http.DetectContentType(b)
	}
	if strings.HasPrefix(ct, "text/html") { // say on the page that this is not main
		sha, at, subj := lastTouch("refs/remotes/origin/" + ref)
		banner := `<div style="position:sticky;top:0;z-index:9999;background:#7c3aed;color:#fff;font:13px -apple-system,sans-serif;padding:5px 14px">branch <b>` +
			html.EscapeString(ref) + `</b> (not merged into main yet) · commit ` + html.EscapeString(sha+" "+subj) + ` ` + html.EscapeString(at) +
			` · <a href="/app/" style="color:#fff">main's report</a></div>`
		if i := bytes.Index(b, []byte("<body")); i >= 0 {
			if j := bytes.IndexByte(b[i:], '>'); j >= 0 {
				b = append(append(append([]byte{}, b[:i+j+1]...), banner...), b[i+j+1:]...)
			}
		}
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		return
	}
	w.Write(b)
}

// ---- /api/app: what the panel's "App analysis" card shows ----

type appRepInfo struct {
	Ref         string `json:"ref,omitempty"` // branch (empty = main, i.e. the clone's working tree)
	Sha         string `json:"sha,omitempty"` // last commit that touched latest.html
	Subject     string `json:"subject,omitempty"`
	CommitAt    string `json:"commit_at,omitempty"`
	GeneratedAt string `json:"generated_at,omitempty"` // crawl time written by app-analysis into the html
	RefreshedAt string `json:"refreshed_at,omitempty"` // last task app:refresh (numbers only)
	URL         string `json:"url"`
}

type appRepStatus struct {
	RepoDir  string       `json:"repo_dir"`
	Main     *appRepInfo  `json:"main"`     // nil = no latest.html on main yet
	Branches []appRepInfo `json:"branches"` // origin/* whose latest.html is newer than main's, newest first
	Error    string       `json:"error,omitempty"`
}

var (
	appStMu    sync.Mutex
	appSt      appRepStatus
	appStAt    time.Time
	appGenMu   sync.Mutex
	appGenMemo = map[string][2]string{} // blob id → generatedAt, refreshedAt
)

func handleAppStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(appStatusCached())
}

// appStatusCached: the status is a dozen git calls; 20 s is well under the clone's 1 min fetch.
func appStatusCached() appRepStatus {
	appStMu.Lock()
	defer appStMu.Unlock()
	if appStAt.IsZero() || time.Since(appStAt) > 20*time.Second {
		appSt, appStAt = appStatus(), time.Now()
	}
	return appSt
}

var (
	genAtRe = regexp.MustCompile(`"generatedAt":"([0-9TZ:.+-]+)"`)
	refAtRe = regexp.MustCompile(`"refreshedAt":"([0-9TZ:.+-]+)"`)
)

// crawlTimes: generatedAt / refreshedAt of an app-analysis html (memoized per blob; the html is ~2 MB).
func crawlTimes(blob string, read func() []byte) (string, string) {
	appGenMu.Lock()
	defer appGenMu.Unlock()
	if v, ok := appGenMemo[blob]; ok && blob != "" {
		return v[0], v[1]
	}
	b := read()
	var g, f string
	if m := genAtRe.FindSubmatch(b); m != nil {
		g = string(m[1])
	}
	if m := refAtRe.FindSubmatch(b); m != nil {
		f = string(m[1])
	}
	if blob != "" {
		appGenMemo[blob] = [2]string{g, f}
	}
	return g, f
}

// lastTouch: the last commit on rev that changed latest.html (short sha, ISO time, subject).
func lastTouch(rev string) (sha, at, subject string) {
	out, err := gitCmd("log", "-1", "--format=%h%x09%cI%x09%s", rev, "--", appReportPath).Output()
	if err != nil {
		return
	}
	f := strings.SplitN(strings.TrimSpace(string(out)), "\t", 3)
	if len(f) == 3 {
		return f[0], f[1], f[2]
	}
	return
}

func blobOf(rev string) string {
	out, err := gitCmd("rev-parse", "-q", "--verify", rev+":"+appReportPath).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// notNewer: ISO time a is at or before b (false when either does not parse).
func notNewer(a, b string) bool {
	ta, e1 := time.Parse(time.RFC3339, a)
	tb, e2 := time.Parse(time.RFC3339, b)
	return e1 == nil && e2 == nil && !ta.After(tb)
}

func appStatus() appRepStatus {
	st := appRepStatus{RepoDir: appRepoDir(), Branches: []appRepInfo{}}
	local := filepath.Join(appRepoDir(), filepath.FromSlash(appReportPath))
	mainBlob, mainAt := "", ""
	if fi, err := os.Stat(local); err == nil && !fi.IsDir() {
		mi := &appRepInfo{URL: "/app/"}
		mi.Sha, mi.CommitAt, mi.Subject = lastTouch("HEAD")
		mainBlob = blobOf("HEAD")
		key := mainBlob
		if err := gitCmd("diff", "--quiet", "HEAD", "--", appReportPath).Run(); err != nil {
			key = "" // the working tree differs from HEAD (or not a git clone): do not memoize
		}
		mi.GeneratedAt, mi.RefreshedAt = crawlTimes(key, func() []byte { b, _ := os.ReadFile(local); return b })
		mainAt = mi.CommitAt
		st.Main = mi
	}
	out, err := gitCmd("for-each-ref", "--sort=-committerdate", "--count=40", "--format=%(refname)", "refs/remotes/origin").Output()
	if err != nil {
		if st.Main == nil {
			st.Error = "not a git clone: " + appRepoDir()
		}
		return st
	}
	head := ""
	if h, err := gitCmd("rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
		head = strings.TrimSpace(string(h))
	}
	for _, full := range strings.Fields(string(out)) {
		ref := strings.TrimPrefix(full, "refs/remotes/origin/")
		if ref == "HEAD" || ref == head || ref == "main" || !validRef(ref) {
			continue
		}
		blob := blobOf(full)
		if blob == "" || blob == mainBlob {
			continue
		}
		bi := appRepInfo{Ref: ref, URL: "/app/?ref=" + url.QueryEscape(ref)}
		bi.Sha, bi.CommitAt, bi.Subject = lastTouch(full)
		if mainAt != "" && notNewer(bi.CommitAt, mainAt) {
			continue // older than main's report (a merged or abandoned branch)
		}
		bi.GeneratedAt, bi.RefreshedAt = crawlTimes(blob, func() []byte {
			b, _ := gitCmd("cat-file", "blob", blob).Output()
			return b
		})
		st.Branches = append(st.Branches, bi)
		if len(st.Branches) >= 5 {
			break
		}
	}
	return st
}
