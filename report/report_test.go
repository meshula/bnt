package report

import (
	"strings"
	"testing"
)

func TestRender_HeadAndTailAlwaysIncluded(t *testing.T) {
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, "line")
	}
	out := Render(strings.Join(lines, "\n"))
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	got := strings.Count(out, "\nline")
	// 25 head + 25 tail. There are no errors so no other windows.
	if !strings.Contains(out, "...") {
		t.Errorf("expected ellipsis between head and tail; got:\n%s", out)
	}
	if got < 40 {
		t.Errorf("expected at least 40 line tokens; got %d", got)
	}
}

func TestRender_WindowAroundError(t *testing.T) {
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, "noise")
	}
	lines[100] = "make: *** Error processing"
	out := Render(strings.Join(lines, "\n"))
	if !strings.Contains(out, "Error processing") {
		t.Fatalf("error line not in output:\n%s", out)
	}
	if !strings.Contains(out, "...") {
		t.Errorf("expected ellipsis between sections; got:\n%s", out)
	}
}

func TestRender_AdjacentWindowsMerged(t *testing.T) {
	var lines []string
	for i := 0; i < 500; i++ {
		lines = append(lines, "x")
	}
	// Two error lines whose ±20 windows overlap should merge into one span.
	lines[200] = "error: A"
	lines[230] = "error: B"
	out := Render(strings.Join(lines, "\n"))
	// We expect: head ... merged-error-block ... tail = 2 ellipses.
	if got := strings.Count(out, "..."); got != 2 {
		t.Errorf("expected 2 ellipses; got %d:\n%s", got, out)
	}
}

func TestRender_AllOverlapsToSingleSpan(t *testing.T) {
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, "x")
	}
	lines[10] = "fatal: bad"
	out := Render(strings.Join(lines, "\n"))
	if strings.Contains(out, "...") {
		t.Errorf("expected no ellipsis when all spans overlap; got:\n%s", out)
	}
}
