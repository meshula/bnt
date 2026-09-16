package tui

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/nporcino/bnt/cmake"
)

type state int

const (
	stateInputSource state = iota
	stateInputBuild
	stateFilePicker
	stateDiscovering
	stateSelect
	stateRunning
	stateResults
)

// phase labels the streamed step currently running, so the running view can
// title itself and the done-handlers know what just finished.
type phase int

const (
	phaseNone phase = iota
	phaseBuild
	phaseInstall
	phaseTest
)

type Config struct {
	SourceDir string
	BuildDir  string
	Jobs      int
	FixRPath  bool
	Verbose   bool

	// SavedTargets/SavedTests pre-check the checklist from a remembered run
	// (the .bnt.json replay). Empty means nothing is pre-checked.
	SavedTargets []string
	SavedTests   []string

	// ApplyRPathFix runs the macOS rpath swap over the selected test binaries.
	ApplyRPathFix func(buildDir string, tests []string)
	// OnCommit persists a committed run configuration (the .bnt.json write).
	// Called once when a build or test-only run is launched.
	OnCommit func(buildDir, installDir string, targets, tests []string)
}

type Model struct {
	cfg Config

	state        state
	phase        phase
	pickerReturn state // input state that invoked the file picker

	cwd       string
	sourceDir string
	buildDir  string

	sourceInput textinput.Model
	buildInput  textinput.Model
	filterInput textinput.Model
	completions []string // path-completion candidates from the last Tab press
	filterMode  bool
	visible     []int // indexes into items; rebuilt when filter changes

	fp filepicker.Model

	spinner spinner.Model

	targets  []string
	tests    []string
	testDirs map[string]string // test name → subdir (rel to buildDir) that installs it

	items  []checkItem
	cursor int // index into visible
	scroll int

	viewport viewport.Model
	logBuf   bytes.Buffer

	width  int
	height int

	errMsg      string
	buildFailed bool
	testFailed  bool
	committed   bool // OnCommit already fired for this run
	exitCode    int

	prog *tea.Program // set by main after construction; used to stream output
}

// SetProgram lets main hand the running program back to the model so the
// build/test goroutines can stream output via prog.Send.
func (m *Model) SetProgram(p *tea.Program) { m.prog = p }

// ExitCode is the process exit code after the program returns: 0 for a clean
// session or the most recent successful run, 1 if the most recent build/test
// run failed.
func (m *Model) ExitCode() int { return m.exitCode }

func New(cfg Config) *Model {
	cwd, _ := os.Getwd()

	src := cfg.SourceDir
	if src == "" {
		src = cwd
	}
	si := newTextInput("(source dir, . = cwd, ? = browse)", src)
	si.Focus()
	bi := newTextInput("/path/to/build   (. = cwd, ? = browse)", cfg.BuildDir)
	fi := newTextInput("filter (substring, case-insensitive)", "")
	fi.Prompt = "/ "

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	m := &Model{
		cfg:         cfg,
		state:       stateInputSource,
		cwd:         cwd,
		sourceDir:   src,
		buildDir:    cfg.BuildDir,
		sourceInput: si,
		buildInput:  bi,
		filterInput: fi,
		spinner:     sp,
	}
	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.spinner.Tick)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height
		m.viewport.Width = v.Width
		if v.Height > 6 {
			m.viewport.Height = v.Height - 6
		}
		if m.state == stateSelect {
			m.ensureCursorVisible()
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case discoverDoneMsg:
		m.targets = v.targets
		m.tests = v.tests
		m.testDirs = v.testDirs
		if v.err != nil {
			m.errMsg = v.err.Error()
		}
		m.buildChecklist()
		m.state = stateSelect
		return m, nil
	case buildChunkMsg:
		m.logBuf.Write(v)
		m.viewport.SetContent(m.logBuf.String())
		m.viewport.GotoBottom()
		return m, nil
	case buildDoneMsg:
		return m.onBuildDone(v.err)
	case installDoneMsg:
		return m.onInstallDone(v.err)
	case testDoneMsg:
		return m.onTestDone(v.err)
	}

	switch m.state {
	case stateInputSource, stateInputBuild:
		return m.updateInput(msg)
	case stateFilePicker:
		return m.updatePicker(msg)
	case stateDiscovering:
		if k, ok := msg.(tea.KeyMsg); ok {
			if k.Type == tea.KeyCtrlC {
				m.exitCode = 0
				return m, tea.Quit
			}
		}
		return m, nil
	case stateSelect:
		return m.updateChecklist(msg)
	case stateRunning:
		if k, ok := msg.(tea.KeyMsg); ok {
			switch k.String() {
			case "ctrl+c":
				return m, tea.Quit
			}
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	case stateResults:
		return m.updateResults(msg)
	}
	return m, nil
}

func (m *Model) View() string {
	switch m.state {
	case stateInputSource, stateInputBuild:
		return m.viewInput()
	case stateFilePicker:
		return m.viewPicker()
	case stateDiscovering:
		return fmt.Sprintf("\n%s discovering targets and tests in %s …\n",
			m.spinner.View(), m.buildDir)
	case stateSelect:
		return m.viewChecklist()
	case stateRunning:
		return m.viewRunning()
	case stateResults:
		return m.viewResults()
	}
	return ""
}

func (m *Model) viewRunning() string {
	var b strings.Builder
	var title string
	switch m.phase {
	case phaseBuild:
		title = fmt.Sprintf("Building %d target(s)", len(m.checkedTargets()))
	case phaseInstall:
		title = "Installing"
	case phaseTest:
		title = "Running tests"
	default:
		title = "Working"
	}
	b.WriteString(TitleStyle.Render(title))
	b.WriteString("\n")
	b.WriteString(m.viewport.View())
	b.WriteString("\n")
	if m.errMsg != "" {
		b.WriteString(ErrorStyle.Render(m.errMsg))
		b.WriteString("\n")
	}
	b.WriteString(HelpStyle.Render("ctrl+c: cancel"))
	return b.String()
}

// ---- discovery command ----

type discoverDoneMsg struct {
	targets  []string
	tests    []string
	testDirs map[string]string
	err      error
}

func (m *Model) discoverCmd() tea.Cmd {
	bd := m.buildDir
	return func() tea.Msg {
		if !cmake.IsBuildDir(bd) {
			return discoverDoneMsg{err: fmt.Errorf("no CMakeCache.txt in %s", bd)}
		}
		type result struct {
			vals []string
			err  error
		}
		tch := make(chan result, 1)
		nch := make(chan result, 1)
		type dirResult struct {
			dirs map[string]string
			err  error
		}
		dch := make(chan dirResult, 1)
		go func() {
			t, err := cmake.DiscoverTargets(bd)
			tch <- result{t, err}
		}()
		go func() {
			n, err := cmake.DiscoverTests(bd)
			nch <- result{n, err}
		}()
		go func() {
			d, err := cmake.TestDirs(bd)
			dch <- dirResult{d, err}
		}()
		tr := <-tch
		nr := <-nch
		dr := <-dch
		var err error
		switch {
		case tr.err != nil:
			err = tr.err
		case nr.err != nil:
			err = nr.err
		case dr.err != nil:
			err = dr.err
		}
		return discoverDoneMsg{targets: tr.vals, tests: nr.vals, testDirs: dr.dirs, err: err}
	}
}

// ---- streamed run commands ----

type buildChunkMsg []byte
type buildDoneMsg struct{ err error }
type installDoneMsg struct{ err error }
type testDoneMsg struct{ err error }

// streamWriter forwards every Write into a buildChunkMsg via prog.Send. It is
// shared by build, install, and test streaming — all three just append to the
// viewport buffer.
type streamWriter struct{ prog *tea.Program }

func (w *streamWriter) Write(p []byte) (int, error) {
	if w.prog != nil {
		// Copy because bubbletea may consume it asynchronously.
		buf := make([]byte, len(p))
		copy(buf, p)
		w.prog.Send(buildChunkMsg(buf))
	}
	return len(p), nil
}

// resetPane clears the output buffer and rebuilds the viewport for a new run.
func (m *Model) resetPane(banner string) {
	m.logBuf.Reset()
	m.viewport = viewport.New(maxInt(m.width, 80), maxInt(6, m.height-6))
	m.viewport.SetContent(banner)
}

// commitRun records the checklist selection once per launched run (build or
// test-only) via the OnCommit callback.
func (m *Model) commitRun() {
	if m.committed || m.cfg.OnCommit == nil {
		return
	}
	m.cfg.OnCommit(m.buildDir, "", m.checkedTargets(), m.checkedTests())
	m.committed = true
}

func (m *Model) startBuild() (tea.Model, tea.Cmd) {
	m.commitRun()
	m.state = stateRunning
	m.phase = phaseBuild
	m.buildFailed = false
	m.errMsg = ""
	m.resetPane("starting build…\n")

	targets := m.checkedTargets()
	bd := m.buildDir
	jobs := m.cfg.Jobs
	prog := m.prog

	go func() {
		w := &streamWriter{prog: prog}
		err := cmake.Build(bd, targets, jobs, w)
		if prog != nil {
			prog.Send(buildDoneMsg{err: err})
		}
	}()
	return m, nil
}

// startInstall installs each dir in sequence (stopping at the first error),
// streaming all output into the shared pane. dirs are absolute paths passed
// straight to `cmake --install <dir>` (configure-time prefix, no override).
func (m *Model) startInstall(dirs []string) (tea.Model, tea.Cmd) {
	m.state = stateRunning
	m.phase = phaseInstall
	m.resetPane("starting install…\n")

	prog := m.prog
	go func() {
		w := &streamWriter{prog: prog}
		var err error
		for _, dir := range dirs {
			if err = cmake.Install(dir, "", w); err != nil {
				break
			}
		}
		if prog != nil {
			prog.Send(installDoneMsg{err: err})
		}
	}()
	return m, nil
}

// installScope returns the unique build subdirs (absolute paths) that install
// the checked tests, for the fast test-only loop. If any checked test is
// unmappable, or its subdir has no cmake_install.cmake, it falls back to a
// root install ([]string{buildDir}) and notes the reason in errMsg so the
// running pane banner explains the wider install.
func (m *Model) installScope() []string {
	tests := m.checkedTests()
	seen := map[string]bool{}
	var dirs []string
	for _, t := range tests {
		rel, ok := m.testDirs[t]
		if !ok {
			m.errMsg = fmt.Sprintf("test %s has no scoped install dir → root install", t)
			return []string{m.buildDir}
		}
		dir := filepath.Join(m.buildDir, rel)
		if _, err := os.Stat(filepath.Join(dir, "cmake_install.cmake")); err != nil {
			m.errMsg = fmt.Sprintf("test %s (%s) has no cmake_install.cmake → root install", t, rel)
			return []string{m.buildDir}
		}
		if !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}
	if len(dirs) == 0 {
		return []string{m.buildDir}
	}
	return dirs
}

// startTests launches ctest with opts, streaming into the pane. It is used for
// the first interactive run and for the results-screen reruns.
func (m *Model) startTests(opts cmake.TestOptions) (tea.Model, tea.Cmd) {
	m.state = stateRunning
	m.phase = phaseTest
	m.testFailed = false
	m.errMsg = ""
	m.resetPane("starting tests…\n")

	bd := m.buildDir
	prog := m.prog
	go func() {
		w := &streamWriter{prog: prog}
		err := cmake.RunTestsStream(bd, opts, w)
		if prog != nil {
			prog.Send(testDoneMsg{err: err})
		}
	}()
	return m, nil
}

func (m *Model) onBuildDone(err error) (tea.Model, tea.Cmd) {
	m.viewport.SetContent(m.logBuf.String())
	m.viewport.GotoBottom()
	if err != nil {
		m.buildFailed = true
		m.exitCode = 1
		m.errMsg = "build failed: " + err.Error()
		m.state = stateResults
		return m, nil
	}
	// Build succeeded → root install (a build may leave downstream installed
	// copies stale; CMake won't propagate, so always install at the root).
	return m.startInstall([]string{m.buildDir})
}

func (m *Model) onInstallDone(err error) (tea.Model, tea.Cmd) {
	m.viewport.SetContent(m.logBuf.String())
	m.viewport.GotoBottom()
	if err != nil {
		m.exitCode = 1
		m.errMsg = "install failed: " + err.Error()
		m.state = stateResults
		return m, nil
	}
	return m.afterInstall()
}

// afterInstall runs the optional rpath fix, then either kicks off the first
// test run or lands on results for a build-only run.
func (m *Model) afterInstall() (tea.Model, tea.Cmd) {
	if m.cfg.FixRPath && m.cfg.ApplyRPathFix != nil {
		m.cfg.ApplyRPathFix(m.buildDir, m.checkedTests())
	}
	if m.hasCheckedTests() {
		return m.startTests(cmake.TestOptions{
			Regex:        joinRegex(m.checkedTests()),
			Verbose:      m.cfg.Verbose,
			OutputOnFail: true,
			Jobs:         m.cfg.Jobs,
		})
	}
	// Build-only success.
	m.exitCode = 0
	m.state = stateResults
	return m, nil
}

func (m *Model) onTestDone(err error) (tea.Model, tea.Cmd) {
	m.viewport.SetContent(m.logBuf.String())
	m.viewport.GotoBottom()
	m.testFailed = err != nil
	if err != nil {
		m.exitCode = 1
	} else {
		m.exitCode = 0
	}
	m.state = stateResults
	return m, nil
}

// ---- results screen ----

// ctestSummaryRE matches ctest's tail summary, e.g.
// "80% tests passed, 1 tests failed out of 5". Display-only.
var ctestSummaryRE = regexp.MustCompile(`\d+% tests passed, \d+ tests failed out of \d+`)

func (m *Model) updateResults(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "r":
			return m.startTests(cmake.TestOptions{RerunFailed: true, Verbose: true, Jobs: m.cfg.Jobs})
		case "v":
			return m.startTests(cmake.TestOptions{
				Regex:   joinRegex(m.checkedTests()),
				Verbose: true,
				Jobs:    m.cfg.Jobs,
			})
		case "b":
			m.state = stateSelect
			m.errMsg = ""
			m.ensureCursorVisible()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *Model) viewResults() string {
	var b strings.Builder

	var header string
	switch {
	case m.buildFailed:
		header = ErrorStyle.Render("BUILD FAILED")
	case m.phase == phaseTest && m.testFailed:
		header = ErrorStyle.Render("TESTS FAILED")
	case m.phase == phaseTest:
		header = OKStyle.Render("TESTS PASSED")
	case m.errMsg != "" && m.exitCode != 0:
		header = ErrorStyle.Render("FAILED")
	default:
		header = OKStyle.Render("DONE")
	}
	if s := ctestSummaryRE.FindString(m.logBuf.String()); s != "" {
		header += "  " + SubtitleStyle.Render(s)
	}
	b.WriteString(TitleStyle.Render("Results"))
	b.WriteString("  ")
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(m.viewport.View())
	b.WriteString("\n")
	if m.errMsg != "" {
		b.WriteString(ErrorStyle.Render(m.errMsg))
		b.WriteString("\n")
	}
	b.WriteString(HelpStyle.Render("r: rerun failed -V • v: rerun all -V • b: back • q: quit"))
	return b.String()
}

// ---- small helpers ----

func joinRegex(items []string) string {
	if len(items) == 0 {
		return ""
	}
	if len(items) == 1 {
		return items[0]
	}
	return "(" + strings.Join(items, "|") + ")"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func expand(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[2:])
		}
	}
	abs, err := filepath.Abs(p)
	if err == nil {
		return abs
	}
	return p
}
