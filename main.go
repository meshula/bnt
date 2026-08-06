package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/pflag"

	"github.com/nporcino/bnt/cmake"
	"github.com/nporcino/bnt/report"
	"github.com/nporcino/bnt/tui"
)

// Defaults from bnt.sh — kept here so --fix-rpath is a no-arg flag that does
// the same thing the original script did.
const (
	defaultOldRPath = "/Library/Frameworks/Python.framework"
	defaultNewRPath = "/Library/ManagedFrameworks/Python/Python3.framework"
)

func main() {
	var (
		sourceDir   string
		buildDir    string
		installDir  string
		buildFlags  []string
		testFlags   []string
		jobs        int
		fixRPath    bool
		verbose     bool
		oldRPath    string
		newRPath    string
		testBinDir  string
	)

	pwd, _ := os.Getwd()

	pflag.StringVar(&sourceDir, "source", pwd, "Source directory (CMakeLists.txt)")
	pflag.StringVar(&buildDir, "build-dir", "", "CMake build directory")
	pflag.StringVar(&installDir, "install-dir", "", "cmake --install destination subdir relative to build dir")
	pflag.StringSliceVar(&buildFlags, "build", nil, "Build target (repeatable). When set, runs headless.")
	pflag.StringSliceVar(&testFlags, "test", nil, "ctest -R regex (repeatable). When set, runs headless.")
	pflag.IntVar(&jobs, "jobs", runtime.NumCPU(), "Parallel build jobs")
	pflag.BoolVar(&fixRPath, "fix-rpath", false, "macOS: swap Python.framework rpath on test binaries")
	pflag.BoolVar(&verbose, "verbose", false, "ctest -V")
	pflag.StringVar(&oldRPath, "rpath-old", defaultOldRPath, "rpath to remove with --fix-rpath")
	pflag.StringVar(&newRPath, "rpath-new", defaultNewRPath, "rpath to add with --fix-rpath")
	pflag.StringVar(&testBinDir, "test-bin-dir", "", "Directory containing test binaries to fix rpaths on (defaults to <build-dir>/../tests, mirroring bnt.sh)")
	pflag.Parse()

	sourceDir = abs(sourceDir)
	buildDir = abs(buildDir)
	installDir = abs(installDir)

	headless := len(buildFlags) > 0 || len(testFlags) > 0
	if headless {
		os.Exit(runHeadless(headlessOpts{
			BuildDir:   buildDir,
			InstallDir: installDir,
			Targets:    buildFlags,
			TestRegex:  joinRegex(testFlags),
			Tests:      testFlags,
			Jobs:       jobs,
			FixRPath:   fixRPath,
			Verbose:    verbose,
			OldRPath:   oldRPath,
			NewRPath:   newRPath,
			TestBinDir: testBinDir,
		}))
	}

	cfg := tui.Config{
		SourceDir:  sourceDir,
		BuildDir:   buildDir,
		InstallDir: installDir,
		Jobs:       jobs,
		FixRPath:   fixRPath,
	}
	model := tui.New(cfg)
	prog := tea.NewProgram(model, tea.WithAltScreen())
	model.SetProgram(prog)
	if _, err := prog.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "tui error:", err)
		os.Exit(1)
	}

	plan := model.Plan()
	if plan.Cancelled {
		os.Exit(0)
	}
	if plan.BuildFailed {
		emitReport(os.Stderr, "build", model.FailedBuildOutput())
		fmt.Fprintln(os.Stderr, "build failed")
		os.Exit(1)
	}

	// Optional install step (only if user typed a non-empty install path).
	if plan.InstallDir != "" {
		dir, prefix := resolveInstall(plan.BuildDir, plan.InstallDir)
		var buf bytes.Buffer
		w := io.MultiWriter(os.Stdout, &buf)
		if err := cmake.Install(dir, prefix, w); err != nil {
			emitReport(os.Stderr, "install", buf.String())
			fmt.Fprintln(os.Stderr, "install failed:", err)
			os.Exit(1)
		}
	}

	if plan.FixRPath {
		applyRPathFix(plan.BuildDir, testBinDir, plan.Tests, oldRPath, newRPath)
	}

	if len(plan.Tests) == 0 {
		return
	}

	regex := joinRegex(plan.Tests)
	if err := cmake.RunTests(plan.BuildDir, regex, verbose); err != nil {
		// ctest exit code propagates via ExitError.
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		fmt.Fprintln(os.Stderr, "ctest:", err)
		os.Exit(1)
	}
}

type headlessOpts struct {
	BuildDir   string
	InstallDir string
	Targets    []string
	TestRegex  string
	Tests      []string
	Jobs       int
	FixRPath   bool
	Verbose    bool
	OldRPath   string
	NewRPath   string
	TestBinDir string
}

func runHeadless(o headlessOpts) int {
	if o.BuildDir == "" {
		fmt.Fprintln(os.Stderr, "error: --build-dir is required in headless mode")
		return 2
	}
	if !cmake.IsBuildDir(o.BuildDir) {
		fmt.Fprintf(os.Stderr, "error: %s is not a CMake build directory (no CMakeCache.txt)\n", o.BuildDir)
		return 2
	}
	if len(o.Targets) > 0 {
		var buf bytes.Buffer
		w := io.MultiWriter(os.Stdout, &buf)
		if err := cmake.Build(o.BuildDir, o.Targets, o.Jobs, w); err != nil {
			emitReport(os.Stderr, "build", buf.String())
			fmt.Fprintln(os.Stderr, "build failed:", err)
			return 1
		}
	}
	if o.InstallDir != "" {
		dir, prefix := resolveInstall(o.BuildDir, o.InstallDir)
		var buf bytes.Buffer
		w := io.MultiWriter(os.Stdout, &buf)
		if err := cmake.Install(dir, prefix, w); err != nil {
			emitReport(os.Stderr, "install", buf.String())
			fmt.Fprintln(os.Stderr, "install failed:", err)
			return 1
		}
	}
	if o.FixRPath {
		applyRPathFix(o.BuildDir, o.TestBinDir, o.Tests, o.OldRPath, o.NewRPath)
	}
	if o.TestRegex != "" {
		if err := cmake.RunTests(o.BuildDir, o.TestRegex, o.Verbose); err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				return ee.ExitCode()
			}
			fmt.Fprintln(os.Stderr, "ctest:", err)
			return 1
		}
	}
	return 0
}

// applyRPathFix mirrors bnt.sh: for each test name, edit the binary at
// <testBinDir>/<name>. testBinDir defaults to <build-dir>/../tests, which is
// where bnt.sh's local-usd layout drops them.
func applyRPathFix(buildDir, testBinDir string, tests []string, oldRP, newRP string) {
	if testBinDir == "" {
		testBinDir = filepath.Join(filepath.Dir(buildDir), "tests")
	}
	for _, t := range tests {
		bin := filepath.Join(testBinDir, t)
		if _, err := os.Stat(bin); err != nil {
			continue
		}
		_ = cmake.FixRPath(bin, oldRP, newRP)
	}
}

func joinRegex(items []string) string {
	if len(items) == 0 {
		return ""
	}
	if len(items) == 1 {
		return items[0]
	}
	return "(" + strings.Join(items, "|") + ")"
}

func abs(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[2:])
		}
	}
	a, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return a
}

// emitReport writes a focused excerpt of buffered command output to w. label
// is the phase name (e.g. "build", "install") for the section banner. If
// out is empty, nothing is written.
func emitReport(w io.Writer, label, out string) {
	if strings.TrimSpace(out) == "" {
		return
	}
	excerpt := report.Render(out)
	if excerpt == "" {
		excerpt = out
	}
	fmt.Fprintf(w, "\n──── %s output (focused) ────\n", label)
	fmt.Fprint(w, excerpt)
	if !strings.HasSuffix(excerpt, "\n") {
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "──────────────────────────────")
}

// resolveInstall picks how to invoke `cmake --install` for the user's input.
//
//	relative or absolute path UNDER buildDir → subdir-style install
//	    (matches bnt.sh: `cmake --install <buildDir>/<subdir>`).
//	absolute path NOT under buildDir → `--prefix` override
//	    (cmake --install <buildDir> --prefix <userPath>).
//
// Returns (dir, prefix) suitable for passing to cmake.Install.
func resolveInstall(buildDir, userPath string) (string, string) {
	if userPath == "" {
		return buildDir, ""
	}
	if !filepath.IsAbs(userPath) {
		return filepath.Join(buildDir, userPath), ""
	}
	rel, err := filepath.Rel(buildDir, userPath)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
		return filepath.Join(buildDir, rel), ""
	}
	return buildDir, userPath
}
