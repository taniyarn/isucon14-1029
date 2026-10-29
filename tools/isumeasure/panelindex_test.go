package main

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 2026-10-28 practice: the Score timeline table repeated the Runs table, Runs was below a long timeline, and the
// hosts column made every Runs row several lines tall. Runs is now the main list (with Δ), after the one-line app card
// and Fleet (CPU is watched during a bench),
// a nav in the sticky bar jumps between sections, the timeline keeps only what Runs lacks inside a <details>
// (collapsed beyond 8 runs), and hosts are one line of badges.
func TestPanelIndexLayout(t *testing.T) {
	p := &panel{contest: "c", hosts: []panelHost{{Name: "svc1", Addr: "10.0.0.1"}}}
	rec := httptest.NewRecorder()
	p.handleIndex(rec, httptest.NewRequest("GET", "/", nil))
	body := rec.Body.String()
	if strings.Contains(body, "%!") {
		t.Fatalf("format verb leaked into the page: %q", body[strings.Index(body, "%!"):][:40])
	}
	for _, want := range []string{
		`<a href="#sec-runs">Runs</a>`, `<a href="#sec-timeline">Timeline</a>`, `<a href="#sec-fleet">Fleet</a>`,
		`function hostsCell(`, `function runDeltas(`, `rows.length <= 8`, `<details class="tl"`,
		// actions that must survive the layout change
		`onclick="benchDone()"`, `function collect(`, `function report(`, `function saveScore(`, `function toggleFail(`,
		`function delRun(`, `function pruneSelected(`, `function benchLog(`, `function secondaryActions(`, `onclick="retro()"`, `onclick="pause(30)"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	app, runs, tl, fleet := strings.Index(body, `id="sec-app"`), strings.Index(body, `id="sec-runs"`), strings.Index(body, `id="sec-timeline"`), strings.Index(body, `id="sec-fleet"`)
	if app < 0 || runs < 0 || tl < 0 || fleet < 0 || !(app < fleet && fleet < runs && runs < tl) {
		t.Errorf("section order: app=%d fleet=%d runs=%d timeline=%d (want app < fleet < runs < timeline)", app, fleet, runs, tl)
	}
	// the timeline table no longer repeats the Runs columns
	if i := strings.Index(body, "let tbl='<table>"); i < 0 || strings.Contains(body[i:i+400], "<th>commit</th>") || strings.Contains(body[i:i+400], "<th>note</th>") {
		t.Errorf("timeline table still has commit / note columns")
	}
	// the hosts cell no longer puts one <br> line per host
	if strings.Contains(body, `join("<br>") || '<span class="muted">(collected; raw data pruned)`) {
		t.Errorf("hosts column is still one line per host")
	}

	node, err := exec.LookPath("node")
	if err != nil {
		t.Log("node not found: script syntax not checked")
		return
	}
	a, b := strings.Index(body, "<script>"), strings.LastIndex(body, "</script>")
	f := filepath.Join(t.TempDir(), "panel.js")
	if err := os.WriteFile(f, []byte(body[a+len("<script>"):b]), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, "--check", f).CombinedOutput(); err != nil {
		t.Fatalf("panel script does not parse: %v\n%s", err, out)
	}
}
