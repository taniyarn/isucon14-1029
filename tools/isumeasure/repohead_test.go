package main

import (
	"strings"
	"testing"
)

func TestRepoHeadLine(t *testing.T) {
	root := repoRootOf(".")
	if root == "" {
		t.Skip("not in a git checkout")
	}
	rh := readRepoHead(root, "0000000")
	if rh == nil || rh.sha == "" {
		t.Fatal("no HEAD read")
	}
	if rh.hasBench != -1 || !strings.Contains(rh.line("alp.yml", ""), "stale") || !strings.Contains(rh.line("alp.yml", ""), "not set") {
		t.Errorf("unknown bench commit must be flagged: %d %s", rh.hasBench, rh.line("alp.yml", ""))
	}
	if rh := readRepoHead(root, rh.sha); rh.hasBench != 1 || strings.Contains(rh.line("a", "POST /x"), "⚠") {
		t.Errorf("HEAD itself must count as present: %+v", rh)
	}
	var nilRH *repoHead
	if !strings.Contains(nilRH.line("", "POST /x"), "not a git clone") {
		t.Error("nil line")
	}
}
