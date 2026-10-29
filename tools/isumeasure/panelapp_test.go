package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHandleAppReport(t *testing.T) {
	repo := t.TempDir()
	dir := filepath.Join(repo, "docs", "reports")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "latest.html"), []byte("<html>app</html>"), 0644)
	os.WriteFile(filepath.Join(repo, "secret.txt"), []byte("no"), 0644)
	os.Symlink(filepath.Join(repo, "secret.txt"), filepath.Join(dir, "link.txt"))
	t.Setenv("REPO_DIR", repo)
	for path, want := range map[string]int{"/app/": 200, "/app/latest.html": 200, "/app/../secret.txt": 404, "/app/link.txt": 404, "/app/.git/config": 404, "/app/%2e%2e/secret.txt": 404} {
		req := httptest.NewRequest("GET", "http://x"+path, nil)
		req.URL.Path = path // keep the raw form (httptest would clean some of them)
		rr := httptest.NewRecorder()
		handleAppReport(rr, req)
		if rr.Code != want {
			t.Errorf("%s: got %d want %d", path, rr.Code, want)
		}
	}
	req := httptest.NewRequest("POST", "http://x/app/latest.html", nil)
	rr := httptest.NewRecorder()
	handleAppReport(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", rr.Code)
	}
}

func TestValidRef(t *testing.T) {
	for ref, want := range map[string]bool{"main": true, "setup/app-map": true, "fix/20261025-143032/indexes": true, "a.b_c-d": true,
		"": false, "../x": false, "a..b": false, "-x": false, "/a": false, "a/": false, "a//b": false, "a b": false, "a;rm": false, "a:b": false, "a~1": false, "a^": false} {
		if got := validRef(ref); got != want {
			t.Errorf("validRef(%q) = %v, want %v", ref, got, want)
		}
	}
}

// gitT runs git in dir for a test (fails the test on error).
func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// appRepoFixture: a bare origin with main (no report) and branch setup/app-map (latest.html + a png), and the ops-like
// clone (init + remote add + fetch + checkout main) in REPO_DIR.
func appRepoFixture(t *testing.T, withMainReport bool) (clone string) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	base := t.TempDir()
	origin, work := filepath.Join(base, "origin.git"), filepath.Join(base, "work")
	clone = filepath.Join(base, "clone")
	gitT(t, base, "init", "-q", "--bare", "-b", "main", origin)
	gitT(t, base, "init", "-q", "-b", "main", work)
	os.WriteFile(filepath.Join(work, "README"), []byte("x"), 0644)
	gitT(t, work, "add", "README")
	gitT(t, work, "commit", "-qm", "init")
	if withMainReport {
		os.MkdirAll(filepath.Join(work, "docs/reports"), 0755)
		os.WriteFile(filepath.Join(work, "docs/reports/latest.html"), []byte(`<html>main {"generatedAt":"2026-10-25T13:00:00.000Z"}</html>`), 0644)
		gitT(t, work, "add", "docs")
		gitT(t, work, "commit", "-qm", "app-analysis on main")
	}
	gitT(t, work, "remote", "add", "origin", origin)
	gitT(t, work, "push", "-q", "origin", "main")
	gitT(t, work, "checkout", "-q", "-b", "setup/app-map")
	os.MkdirAll(filepath.Join(work, "docs/reports"), 0755)
	os.WriteFile(filepath.Join(work, "docs/reports/latest.html"), []byte(`<html><body class="x">branch <img src="shot.png"> {"generatedAt":"2026-10-25T14:16:07.020Z","refreshedAt":"2026-10-25T14:17:58.893Z"}</html>`), 0644)
	os.WriteFile(filepath.Join(work, "docs/reports/shot.png"), []byte("\x89PNG\r\n\x1a\nfake"), 0644)
	os.WriteFile(filepath.Join(work, "secret.txt"), []byte("no"), 0644)
	time.Sleep(1100 * time.Millisecond) // committer dates have 1 s resolution: the branch must be newer than main
	gitT(t, work, "add", "docs", "secret.txt")
	gitT(t, work, "commit", "-qm", "docs: app-map and app-analysis report")
	gitT(t, work, "push", "-q", "origin", "setup/app-map")
	os.MkdirAll(clone, 0755)
	gitT(t, clone, "init", "-q")
	gitT(t, clone, "remote", "add", "origin", origin)
	gitT(t, clone, "fetch", "-q", "origin")
	gitT(t, clone, "checkout", "-q", "-f", "-B", "main", "origin/main")
	t.Setenv("REPO_DIR", clone)
	appStMu.Lock()
	appStAt = time.Time{}
	appStMu.Unlock()
	return clone
}

func getApp(t *testing.T, target string, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "http://x"+target, nil)
	rr := httptest.NewRecorder()
	h(rr, req)
	return rr
}

func TestAppReportFromBranch(t *testing.T) {
	appRepoFixture(t, false)
	// main has no report: 404 that names the branch
	rr := getApp(t, "/app/", handleAppReport)
	if rr.Code != 404 || !strings.Contains(rr.Body.String(), "/app/?ref=setup%2Fapp-map") {
		t.Errorf("/app/ without a report on main: %d %q", rr.Code, rr.Body.String())
	}
	// the branch's report, and a file next to it (by ?ref= and by Referer)
	rr = getApp(t, "/app/?ref=setup/app-map", handleAppReport)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `<body class="x"><div`) || !strings.Contains(rr.Body.String(), "not merged into main yet") || !strings.HasPrefix(rr.Header().Get("Content-Type"), "text/html") {
		t.Errorf("?ref=: %d %s %q", rr.Code, rr.Header().Get("Content-Type"), rr.Body.String())
	}
	rr = getApp(t, "/app/shot.png?ref=setup/app-map", handleAppReport)
	if rr.Code != 200 || rr.Header().Get("Content-Type") != "image/png" {
		t.Errorf("png by ref: %d %s", rr.Code, rr.Header().Get("Content-Type"))
	}
	req := httptest.NewRequest("GET", "http://x/app/shot.png", nil)
	req.Header.Set("Referer", "http://x/app/?ref=setup/app-map")
	rr = httptest.NewRecorder()
	handleAppReport(rr, req)
	if rr.Code != 200 {
		t.Errorf("png by Referer: %d", rr.Code)
	}
	for _, target := range []string{"/app/?ref=../x", "/app/?ref=-x", "/app/?ref=a%20b", "/app/..%2Fsecret.txt?ref=setup/app-map", "/app/.git/config?ref=setup/app-map"} {
		if rr := getApp(t, target, handleAppReport); rr.Code != 400 && rr.Code != 404 {
			t.Errorf("%s: %d (want 400/404)", target, rr.Code)
		}
	}
	if rr := getApp(t, "/app/?ref=nope", handleAppReport); rr.Code != 404 {
		t.Errorf("unknown branch: %d", rr.Code)
	}
	// status: no main, one branch with its crawl time and commit
	var st appRepStatus
	json.Unmarshal(getApp(t, "/api/app", handleAppStatus).Body.Bytes(), &st)
	if st.Main != nil || len(st.Branches) != 1 {
		t.Fatalf("status: %+v", st)
	}
	b := st.Branches[0]
	if b.Ref != "setup/app-map" || b.Sha == "" || b.Subject != "docs: app-map and app-analysis report" || b.GeneratedAt != "2026-10-25T14:16:07.020Z" || b.RefreshedAt == "" || b.URL != "/app/?ref=setup%2Fapp-map" {
		t.Errorf("branch: %+v", b)
	}
}

func TestAppStatusMain(t *testing.T) {
	appRepoFixture(t, true)
	var st appRepStatus
	json.Unmarshal(getApp(t, "/api/app", handleAppStatus).Body.Bytes(), &st)
	if st.Main == nil || st.Main.Sha == "" || st.Main.Subject != "app-analysis on main" || st.Main.GeneratedAt != "2026-10-25T13:00:00.000Z" {
		t.Fatalf("main: %+v", st.Main)
	}
	if len(st.Branches) != 1 { // the branch's report is newer than main's
		t.Errorf("branches: %+v", st.Branches)
	}
	if rr := getApp(t, "/app/", handleAppReport); rr.Code != 200 || !strings.Contains(rr.Body.String(), "main") {
		t.Errorf("/app/: %d", rr.Code)
	}
}

func TestAppStatusNoRepo(t *testing.T) {
	t.Setenv("REPO_DIR", t.TempDir())
	appStMu.Lock()
	appStAt = time.Time{}
	appStMu.Unlock()
	st := appStatusCached()
	if st.Main != nil || len(st.Branches) != 0 || st.Error == "" {
		t.Errorf("no repo: %+v", st)
	}
	if rr := getApp(t, "/app/", handleAppReport); rr.Code != 404 || !strings.Contains(rr.Body.String(), "no app-analysis report") {
		t.Errorf("/app/: %d %q", rr.Code, rr.Body.String())
	}
}
