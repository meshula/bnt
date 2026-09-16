package cmake

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTestDirs builds a temp CTest tree with two libraries, each with its own
// CTestTestfile.cmake, and confirms every test maps to its owning subdir and an
// unknown name is absent.
func TestTestDirs(t *testing.T) {
	build := t.TempDir()

	// pxr/base/tf owns TfDl + TfType.
	tf := filepath.Join(build, "pxr", "base", "tf")
	if err := os.MkdirAll(tf, 0o755); err != nil {
		t.Fatal(err)
	}
	tfBody := `add_test([=[TfDl]=] "/abs/tests/testTf" TfDl)
set_tests_properties([=[TfDl]=] PROPERTIES WORKING_DIRECTORY "/abs")
add_test([=[TfType]=] "/abs/tests/testTf" TfType)
`
	if err := os.WriteFile(filepath.Join(tf, "CTestTestfile.cmake"), []byte(tfBody), 0o644); err != nil {
		t.Fatal(err)
	}

	// pxr/base/gf owns GfVec.
	gf := filepath.Join(build, "pxr", "base", "gf")
	if err := os.MkdirAll(gf, 0o755); err != nil {
		t.Fatal(err)
	}
	gfBody := `add_test([=[GfVec]=] "/abs/tests/testGf" GfVec)
`
	if err := os.WriteFile(filepath.Join(gf, "CTestTestfile.cmake"), []byte(gfBody), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := TestDirs(build)
	if err != nil {
		t.Fatalf("TestDirs: %v", err)
	}

	want := map[string]string{
		"TfDl":   filepath.Join("pxr", "base", "tf"),
		"TfType": filepath.Join("pxr", "base", "tf"),
		"GfVec":  filepath.Join("pxr", "base", "gf"),
	}
	for name, dir := range want {
		if got[name] != dir {
			t.Errorf("TestDirs[%q] = %q, want %q", name, got[name], dir)
		}
	}
	if _, ok := got["Unknown"]; ok {
		t.Errorf("unknown test name present in map: %v", got)
	}
	if len(got) != len(want) {
		t.Errorf("got %d entries, want %d: %v", len(got), len(want), got)
	}
}
