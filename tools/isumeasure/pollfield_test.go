package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPollFields(t *testing.T) {
	dir := t.TempDir()
	src := "package main\n\ntype resp struct {\n\tData any `json:\"data\"`\n\tRetryAfterMs int `json:\"retry_after_ms\"`\n}\n\nfunc setup(mux *http.ServeMux) {\n\tmux.HandleFunc(\"GET /api/chair/notification\", chairGetNotification)\n}\n\nfunc chairGetNotification(w http.ResponseWriter, r *http.Request) {\n\twriteJSON(w, build())\n}\n\nfunc build() *resp {\n\treturn &resp{RetryAfterMs: 30}\n}\n"
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0644)
	ci := loadCodeIndex(dir)
	if len(ci.polls) != 1 || ci.polls[0].value != 30 || ci.polls[0].json != "retry_after_ms" {
		t.Fatalf("polls: %+v", ci.polls)
	}
	acts := []action{{priority: "P2", title: "Polling: GET /api/chair/notification at 700 req/s (median gap 40ms)", kind: "polling"}}
	applyPollFields(acts, ci, "")
	if acts[0].priority != "P1" || !strings.Contains(acts[0].title, "retry_after_ms = 30") || !strings.Contains(acts[0].detail, "build()") {
		t.Errorf("action: %+v", acts[0])
	}
}
