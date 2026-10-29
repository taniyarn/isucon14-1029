package main

import (
	"strings"
	"testing"
)

func TestAddTOCMarkdown(t *testing.T) {
	md := "# Bench report x\n\nintro\n\n## Verdict\n\ntext\n\n```\n## not a heading\n```\n\n## Errors (5xx by endpoint)\n"
	got := addTOCMarkdown(md)
	for _, want := range []string{`<a id="toc"></a>`, "[Verdict](#sec-1)", "[Errors](#sec-2)", `## <a id="sec-2"></a>Errors (5xx by endpoint)`, tocBack, "## not a heading"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "sec-3") {
		t.Error("heading inside a code fence was indexed")
	}
	if !strings.Contains(got, `<a id="grp-1"></a>**1. Verdict**`) {
		t.Error("missing group heading")
	}
	if strings.Index(got, "**目次**") < strings.Index(got, "# Bench report") {
		t.Error("TOC must follow the title")
	}
}

func TestAddTOCHTML(t *testing.T) {
	body, nav := addTOCHTML(`<section><h2>Errors <span class="muted">(x)</span></h2></section><section><h2>Verdict — where?</h2></section>`)
	if !strings.Contains(body, `<section id="sec-1">`) || !strings.Contains(body, `href="#toc"`) {
		t.Errorf("body: %s", body)
	}
	// reordered: Verdict comes before Errors
	if !strings.Contains(nav, `<a href="#sec-1">Verdict</a>`) || !strings.Contains(nav, `<a href="#sec-2">Errors</a>`) || !strings.Contains(nav, `href="#grp-1"`) {
		t.Errorf("nav: %s", nav)
	}
}

func TestTOCOrder(t *testing.T) {
	md := "# t\n\n## Timeline (x)\n\nT\n\n## Next actions (y)\n\nN\n\n## Verdict\n\nV\n\n## Flows (svc1)\n\nF\n\n## vs previous run 2026 (a)\n\nP\n"
	got := addTOCMarkdown(md)
	idx := func(s string) int { return strings.Index(got, s) }
	if !(idx("\nV\n") < idx("\nP\n") && idx("\nP\n") < idx("\nN\n") && idx("\nN\n") < idx("\nF\n") && idx("\nF\n") < idx("\nT\n")) {
		t.Errorf("order wrong:\n%s", got)
	}
	if !strings.Contains(got, "[vs previous run](#") {
		t.Errorf("TOC must not carry the run id:\n%s", got)
	}
}
