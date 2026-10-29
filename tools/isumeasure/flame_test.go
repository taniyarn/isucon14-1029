package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const rawFixture = `PeriodType: cpu nanoseconds
Period: 10000000
Samples:
samples/count cpu/nanoseconds
          1   10000000: 1 2 4
          2   20000000: 1 3 4
          1   10000000: 1 2 4
Locations
     1: 0x1 M=1 crypto/sha256.block /x.go:1:0 s=0
     2: 0x2 M=1 main.handlerA /x.go:2:0 s=0
     3: 0x3 M=1 main.handlerB /x.go:3:0 s=0
     4: 0x4 M=1 net/http.(*conn).serve /x.go:4:0 s=0
Mappings
`

func TestParsePprofStacksMergesAndRootFirst(t *testing.T) {
	st := parsePprofStacks(rawFixture)
	if len(st) != 2 {
		t.Fatalf("want 2 unique stacks, got %d: %+v", len(st), st)
	}
	if got := strings.Join(st[0].Frames, ">"); got != "net/http.(*conn).serve>main.handlerA>crypto/sha256.block" || st[0].Value != 20000000 {
		t.Fatalf("stack 0 = %s %d", got, st[0].Value)
	}
	cum := flameCum(st)
	if c := cum["crypto/sha256.block"]; c != 1 { // the leaf of every stack: spread over two handlers, 100% in total
		t.Fatalf("cum sha256 = %v", c)
	}
	if c := cum["main.handlerB"]; c != 0.5 {
		t.Fatalf("cum handlerB = %v", c)
	}
	root := parsePprofRaw(rawFixture)
	if root.Value != 40000000 || len(root.Children) != 1 {
		t.Fatalf("tree root %d children %d", root.Value, len(root.Children))
	}
}

func TestBuildFlamePayloadStringTableAndTail(t *testing.T) {
	st := []flameStack{{Frames: []string{"a", "b"}, Value: 100000}, {Frames: []string{"a", "c"}, Value: 1}}
	p := buildFlamePayload(st, "ns")
	if p.Total != 100001 || len(p.Stacks) != 2 || p.Names[p.Stacks[1][0]] != "(other, < 0.01% each)" {
		t.Fatalf("payload %+v", p)
	}
}

func TestPrevProfileFindsNewestEarlierRun(t *testing.T) {
	base := t.TempDir()
	for _, ts := range []string{"20261020-100000", "20261020-110000", "20261020-120000"} {
		os.MkdirAll(filepath.Join(base, ts, "svc1"), 0755)
	}
	os.WriteFile(filepath.Join(base, "20261020-100000", "svc1", "cpu.pb.gz"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(base, "20261020-100000", "meta.json"), []byte(`{"hosts":[{"req_total":3000},{"req_total":500}]}`), 0644)
	cur := filepath.Join(base, "20261020-120000", "svc1", "cpu.pb.gz")
	p, ts := prevProfile(cur)
	if ts != "20261020-100000" || !strings.HasSuffix(p, filepath.Join("20261020-100000", "svc1", "cpu.pb.gz")) {
		t.Fatalf("prev = %s %s", p, ts)
	}
	if p, _ := prevProfile(filepath.Join(base, "20261020-100000", "svc1", "cpu.pb.gz")); p != "" {
		t.Fatalf("oldest run has no prev, got %s", p)
	}
}

func TestPprofTopWithLinks(t *testing.T) {
	top := "Showing nodes accounting for 1s, 100% of 1s total\n      flat  flat%   sum%        cum   cum%\n     0.35s 34.70% 34.70%      0.35s 34.70%  crypto/sha256.block\n     0.10s 10.00% 44.70%      0.50s 50.00%  main.fillUserResponse"
	h := pprofTopWithLinks(top, "svc1/cpu-flame.html")
	if !strings.Contains(h, `<a href="svc1/cpu-flame.html#fn=crypto%2Fsha256.block"`) || !strings.Contains(h, ">main.fillUserResponse</a>") {
		t.Fatalf("links missing:\n%s", h)
	}
	if !strings.HasPrefix(h, "Showing nodes") || strings.Count(h, "<a ") != 2 {
		t.Fatalf("header lines must stay plain:\n%s", h)
	}
}

func TestPrevProfileSkipsMisdetectedRuns(t *testing.T) {
	base := t.TempDir()
	mk := func(ts string, req int) {
		os.MkdirAll(filepath.Join(base, ts, "svc1"), 0755)
		os.WriteFile(filepath.Join(base, ts, "svc1", "cpu.pb.gz"), []byte("x"), 0644)
		os.WriteFile(filepath.Join(base, ts, "meta.json"), []byte(`{"hosts":[{"req_total":`+strconv.Itoa(req)+`}]}`), 0644)
	}
	mk("20261020-160320", 3416) // bench
	mk("20261020-161000", 120)  // app-analysis crawl, not a bench
	mk("20261020-161752", 36433)
	os.MkdirAll(filepath.Join(base, "20261020-155000", "svc1"), 0755) // no meta.json: skipped too
	os.WriteFile(filepath.Join(base, "20261020-155000", "svc1", "cpu.pb.gz"), []byte("x"), 0644)
	if _, ts := prevProfile(filepath.Join(base, "20261020-161752", "svc1", "cpu.pb.gz")); ts != "20261020-160320" {
		t.Fatalf("prev ts = %q, want the bench 20261020-160320", ts)
	}
	if p, _ := prevProfile(filepath.Join(base, "20261020-160320", "svc1", "cpu.pb.gz")); p != "" {
		t.Fatalf("no earlier bench: want none, got %s", p)
	}
}

func TestHandlerIndexPrefersRoutesAndSkipsMiddleware(t *testing.T) {
	routes := map[string]string{"getReactionsHandler": "GET /api/livestream/:livestream_id/reaction"}
	fr := []string{"net/http.(*conn).serve", "main.main.Middleware.func1", "main.measureMiddleware.func1", "main.getReactionsHandler", "main.fillReactionResponse", "crypto/sha256.block"}
	if i := handlerIndex(fr, routes); i != 3 {
		t.Fatalf("handler index = %d", i)
	}
	if i := handlerIndex([]string{"main.measureMiddleware.func1", "main.helper", "main.postIconHandler.func1"}, nil); i != 2 {
		t.Fatalf("a name ending in Handler wins over the first main.* helper: %d", i)
	}
	if i := handlerIndex([]string{"runtime.gcBgMarkWorker"}, nil); i != -1 {
		t.Fatalf("no main.* frame: %d", i)
	}
}

func TestFlameHandlersCountsRequestsInTheProfileWindow(t *testing.T) {
	raw := "Time: 2026-10-20 06:18:12.5 -1000 HST\nDuration: 30\n" + rawFixture
	from, dur, ok := profileWindow(raw)
	if !ok || dur.Seconds() != 30 || from.UTC().Format("15:04:05") != "16:18:12" {
		t.Fatalf("window %v %v %v", from.UTC(), dur, ok)
	}
	ci := &codeIndex{routeRaw: map[string]string{"handlerA": "GET /a", "handlerB": "GET /b"}}
	nz := newNormalizer(nil, "test", ci)
	at := func(s string) time.Time { t, _ := time.Parse(time.RFC3339, s); return t }
	acc := []accessEvent{
		{at: at("2026-10-20T16:18:00Z"), method: "GET", uri: "/a"}, // before the window
		{at: at("2026-10-20T16:18:20Z"), method: "GET", uri: "/a"},
		{at: at("2026-10-20T16:18:21Z"), method: "GET", uri: "/a"},
		{at: at("2026-10-20T16:18:30Z"), method: "GET", uri: "/b"},
	}
	hs, note := flameHandlers(parsePprofStacks(raw), raw, &flameCtx{access: acc, nz: nz, ci: ci})
	if !strings.Contains(note, "16:18:12〜16:18:42 UTC（30 秒）") || !strings.Contains(note, "ms/件 = その窓の CPU 秒 ÷ 件数") || strings.Contains(note, "近似") {
		t.Fatalf("note %q", note)
	}
	by := map[string]flameHandler{}
	for _, h := range hs {
		by[h.Fn] = h
	}
	a, b := by["main.handlerA"], by["main.handlerB"]
	if a.Endpoint != "GET /a" || a.Count != 2 || a.NS != 20000000 || a.MsPerReq != 10 {
		t.Fatalf("handlerA %+v", a)
	}
	if b.Count != 1 || b.MsPerReq != 20 {
		t.Fatalf("handlerB %+v", b)
	}
	if l := flameHandlersLine(hs); !strings.HasPrefix(l, "CPU の多いハンドラ: handler") || !strings.Contains(l, "ms/req") {
		t.Fatalf("line %q", l)
	}
	_, note2 := flameHandlers(parsePprofStacks(rawFixture), rawFixture, &flameCtx{access: acc, nz: nz, ci: ci})
	if !strings.Contains(note2, "近似") {
		t.Fatalf("no window -> approximate, got %q", note2)
	}
}

func TestFlamePageHasChildrenBreakdownAndSplitter(t *testing.T) {
	out := filepath.Join(t.TempDir(), "cpu-flame.html")
	p := buildFlamePayload(parsePprofStacks(rawFixture), "ns")
	p.Handlers = []flameHandler{{Fn: "main.handlerA", Endpoint: "GET /a", NS: 20000000, Count: 2}}
	if writeFlamePage(out, "t", p) == "" {
		t.Fatal("page not written")
	}
	b, _ := os.ReadFile(out)
	for _, want := range []string{flameVersion, `id="htitle"`, "直下の内訳", `id="grip"`, "isumeasure.flame.side", "pointerdown", "dblclick", "data-c=", `id="bSide"`, "isumeasure.flame.sidehidden", `id="wnote"`, "件数<br><span class=legend>窓 "} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("page lacks %q", want)
		}
	}
	if strings.Contains(string(b), "ハンドラごとの CPU") {
		t.Fatal("the old handler table title is still there")
	}
}
