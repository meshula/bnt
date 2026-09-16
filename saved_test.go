package main

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestSavedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Saved{
		BuildDir:   "/build/OpenUSD",
		InstallDir: "/install",
		Targets:    []string{"usdProfiles", "_usdProfiles"},
		Tests:      []string{"testUsdProfiles"},
		FixRPath:   true,
		Verbose:    true,
		Jobs:       8,
	}

	if _, ok := loadSaved(dir); ok {
		t.Fatal("loadSaved should report not-ok when no dot file exists")
	}
	if err := saveSaved(dir, want); err != nil {
		t.Fatalf("saveSaved: %v", err)
	}
	got, ok := loadSaved(dir)
	if !ok {
		t.Fatal("loadSaved reported not-ok after saveSaved")
	}
	if !reflect.DeepEqual(*got, want) {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", *got, want)
	}
}

func TestLoadSavedCorrupt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(savedPath(dir), []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadSaved(dir); ok {
		t.Error("loadSaved should report not-ok on corrupt json")
	}
}

func TestSavedSummaryCap(t *testing.T) {
	var s Saved
	s.BuildDir = "/b"
	// 20 targets → 20 combined lines, over the cap of 16.
	for i := 0; i < 20; i++ {
		s.Targets = append(s.Targets, "t"+string(rune('a'+i)))
	}
	out := s.summary()

	nTargetLines := strings.Count(out, "target: ")
	if nTargetLines != summaryCap {
		t.Errorf("expected %d target lines, got %d\n%s", summaryCap, nTargetLines, out)
	}
	if !strings.Contains(out, "…4 others") {
		t.Errorf("expected '…4 others' tail, got:\n%s", out)
	}
}

func TestSavedSummaryNoCap(t *testing.T) {
	s := Saved{BuildDir: "/b", Targets: []string{"a", "b"}, Tests: []string{"x"}}
	out := s.summary()
	if strings.Contains(out, "others") {
		t.Errorf("did not expect a truncation tail:\n%s", out)
	}
	if !strings.Contains(out, "install dir: (none)") {
		t.Errorf("expected '(none)' install line:\n%s", out)
	}
}
