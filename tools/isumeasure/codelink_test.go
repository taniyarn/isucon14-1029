package main

import "testing"

func TestSplitTag(t *testing.T) {
	cases := []struct{ in, tag, loc, sql string }{
		{"/* GET /api/user/{id} @ user_handler.go:101 */ SELECT 1", "GET /api/user/:id", "user_handler.go:101", "SELECT 1"},
		{"/* @ main.go:9 */ DELETE FROM t", "", "main.go:9", "DELETE FROM t"},
		{"/* GET /api/x */ SELECT 1", "GET /api/x", "", "SELECT 1"},
		{"SELECT 1", "", "", "SELECT 1"},
		{"/* GET /api/a@b */ SELECT 1", "GET /api/a@b", "", "SELECT 1"},
	}
	for _, c := range cases {
		tag, loc, sql := splitTag(c.in)
		if tag != c.tag || loc != c.loc || sql != c.sql {
			t.Errorf("%q: got (%q, %q, %q)", c.in, tag, loc, sql)
		}
	}
}

func TestGrepFrag(t *testing.T) {
	cases := map[string]string{
		"SELECT user_id, hash AS h FROM icons WHERE user_id IN (...) ORDER BY id;": "FROM icons WHERE user_id",
		"INSERT INTO icons (user_id, image, hash) VALUES (...);":                   "INTO icons",
		"UPDATE reservation_slots SET slot = slot - ? WHERE start_at >= ?":         "UPDATE reservation_slots SET slot",
		"COMMIT;": "",
	}
	for in, want := range cases {
		if got := grepFrag(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestCodeOfExact(t *testing.T) {
	q := &sqlStat{norm: "SELECT 1", locs: map[string]int{"a.go:1": 2, "b.go:5": 9}}
	c := codeOf(q, nil)
	if !c.exact || c.locs[0] != "b.go:5" || c.count[0] != 9 {
		t.Fatalf("got %+v", c)
	}
	if got := c.md(q.norm); got != "`b.go:5` ×9, `a.go:1` ×2" {
		t.Fatalf("md: %q", got)
	}
}
