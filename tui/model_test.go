package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// writeBuildDir creates a minimal directory that IsBuildDir accepts (it only
// checks for CMakeCache.txt).
func writeBuildDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CMakeCache.txt"), []byte("# fake\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// TestInputFlowDotAndPath walks source → build using "." for source and a real
// build dir path for build, and confirms committing the build dir goes straight
// to discovery — there is no interactive install prompt.
func TestInputFlowDotAndPath(t *testing.T) {
	bd := writeBuildDir(t)
	m := New(Config{})
	cwd, _ := os.Getwd()

	// Source: "." → cwd.
	m.sourceInput.SetValue(".")
	mi, _ := m.Update(key("enter"))
	m = mi.(*Model)
	if m.state != stateInputBuild {
		t.Fatalf("after source enter, state = %v, want stateInputBuild", m.state)
	}
	if m.sourceDir != cwd {
		t.Errorf("sourceDir = %q, want cwd %q", m.sourceDir, cwd)
	}

	// Build: a real path → straight to discovery.
	m.buildInput.SetValue(bd)
	mi, cmd := m.Update(key("enter"))
	m = mi.(*Model)
	if m.state != stateDiscovering {
		t.Fatalf("after build enter, state = %v, want stateDiscovering", m.state)
	}
	if m.buildDir != bd {
		t.Errorf("buildDir = %q, want %q", m.buildDir, bd)
	}
	if cmd == nil {
		t.Error("expected a discover command after build enter")
	}
}

// TestInstallScopeScoped maps checked tests to their owning subdirs when those
// subdirs carry a cmake_install.cmake.
func TestInstallScopeScoped(t *testing.T) {
	bd := writeBuildDir(t)
	// Two tests owned by one subdir (pxr/base/tf) with a cmake_install.cmake.
	sub := filepath.Join(bd, "pxr", "base", "tf")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "cmake_install.cmake"), []byte("# install\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := New(Config{})
	m.buildDir = bd
	m.testDirs = map[string]string{"TfDl": "pxr/base/tf", "TfType": "pxr/base/tf"}
	m.items = []checkItem{
		{name: "TfDl", kind: itemTest, checked: true},
		{name: "TfType", kind: itemTest, checked: true},
	}

	got := m.installScope()
	want := []string{sub}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("installScope() = %v, want %v", got, want)
	}
	if m.errMsg != "" {
		t.Errorf("errMsg = %q, want empty for scoped install", m.errMsg)
	}
}

// TestInstallScopeFallback falls back to a root install (and sets a banner note)
// when a checked test is unmappable or its subdir lacks a cmake_install.cmake.
func TestInstallScopeFallback(t *testing.T) {
	bd := writeBuildDir(t)

	t.Run("unmapped test", func(t *testing.T) {
		m := New(Config{})
		m.buildDir = bd
		m.testDirs = map[string]string{} // nothing mapped
		m.items = []checkItem{{name: "Ghost", kind: itemTest, checked: true}}
		got := m.installScope()
		if len(got) != 1 || got[0] != bd {
			t.Fatalf("installScope() = %v, want root [%s]", got, bd)
		}
		if m.errMsg == "" {
			t.Error("expected a banner note for the unmapped test")
		}
	})

	t.Run("subdir without cmake_install.cmake", func(t *testing.T) {
		m := New(Config{})
		m.buildDir = bd
		m.testDirs = map[string]string{"TfDl": "pxr/base/tf"} // dir has no install file
		m.items = []checkItem{{name: "TfDl", kind: itemTest, checked: true}}
		got := m.installScope()
		if len(got) != 1 || got[0] != bd {
			t.Fatalf("installScope() = %v, want root [%s]", got, bd)
		}
		if m.errMsg == "" {
			t.Error("expected a banner note for the missing cmake_install.cmake")
		}
	})
}

// TestBuildDirValidation rejects a non-build-dir at the build prompt.
func TestBuildDirValidation(t *testing.T) {
	m := New(Config{})
	m.state = stateInputBuild
	m.buildInput.Focus()
	m.buildInput.SetValue(t.TempDir()) // no CMakeCache.txt
	mi, _ := m.Update(key("enter"))
	m = mi.(*Model)
	if m.state != stateInputBuild {
		t.Fatalf("state advanced past invalid build dir: %v", m.state)
	}
	if m.errMsg == "" {
		t.Error("expected an error message for non-build dir")
	}
}

// TestPickerQuestionMark enters the picker from the build prompt and confirms
// pickerReturn is remembered.
func TestPickerQuestionMark(t *testing.T) {
	m := New(Config{})
	m.state = stateInputBuild
	m.buildInput.Focus()
	m.buildInput.SetValue("?")
	mi, _ := m.Update(key("enter"))
	m = mi.(*Model)
	if m.state != stateFilePicker {
		t.Fatalf("state = %v, want stateFilePicker", m.state)
	}
	if m.pickerReturn != stateInputBuild {
		t.Errorf("pickerReturn = %v, want stateInputBuild", m.pickerReturn)
	}
	// Cancel returns to the invoking input.
	mi, _ = m.Update(key("q"))
	m = mi.(*Model)
	if m.state != stateInputBuild {
		t.Errorf("after cancel, state = %v, want stateInputBuild", m.state)
	}
}

// TestCtrlDQuits confirms Ctrl+D quits from an input state with exit 0.
func TestCtrlDQuits(t *testing.T) {
	m := New(Config{})
	_, cmd := m.Update(key("ctrl+d"))
	if cmd == nil {
		t.Fatal("expected a quit command from ctrl+d")
	}
	if m.ExitCode() != 0 {
		t.Errorf("exit code = %d, want 0", m.ExitCode())
	}
}

// TestResultsRerunKeys confirms the results screen transitions to a running
// test phase on r / v and back to select on b.
func TestResultsRerunKeys(t *testing.T) {
	bd := writeBuildDir(t)

	newAtResults := func() *Model {
		m := New(Config{})
		m.buildDir = bd
		// One checked test so v has a regex to build.
		m.items = []checkItem{{name: "t1", kind: itemTest, checked: true}}
		m.recomputeVisible()
		m.phase = phaseTest
		m.testFailed = true
		m.state = stateResults
		return m
	}

	t.Run("r reruns failed", func(t *testing.T) {
		m := newAtResults()
		mi, _ := m.Update(key("r"))
		m = mi.(*Model)
		if m.state != stateRunning || m.phase != phaseTest {
			t.Fatalf("state=%v phase=%v, want running/test", m.state, m.phase)
		}
	})

	t.Run("v reruns all", func(t *testing.T) {
		m := newAtResults()
		mi, _ := m.Update(key("v"))
		m = mi.(*Model)
		if m.state != stateRunning || m.phase != phaseTest {
			t.Fatalf("state=%v phase=%v, want running/test", m.state, m.phase)
		}
	})

	t.Run("b returns to checklist", func(t *testing.T) {
		m := newAtResults()
		mi, _ := m.Update(key("b"))
		m = mi.(*Model)
		if m.state != stateSelect {
			t.Fatalf("state = %v, want stateSelect", m.state)
		}
	})
}

// TestOnTestDoneTransitions confirms pass/fail recording and results landing.
func TestOnTestDoneTransitions(t *testing.T) {
	m := New(Config{})
	m.phase = phaseTest

	mi, _ := m.onTestDone(nil)
	m = mi.(*Model)
	if m.testFailed || m.exitCode != 0 || m.state != stateResults {
		t.Errorf("pass: failed=%v exit=%d state=%v", m.testFailed, m.exitCode, m.state)
	}

	m2 := New(Config{})
	m2.phase = phaseTest
	mi, _ = m2.onTestDone(&fakeExitErr{})
	m2 = mi.(*Model)
	if !m2.testFailed || m2.exitCode != 1 || m2.state != stateResults {
		t.Errorf("fail: failed=%v exit=%d state=%v", m2.testFailed, m2.exitCode, m2.state)
	}
}

type fakeExitErr struct{}

func (fakeExitErr) Error() string { return "exit status 1" }
