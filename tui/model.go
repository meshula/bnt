package tui

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/nporcino/bnt/cmake"
)

type state int

const (
	stateInputBuildDir state = iota
	stateInputInstallDir
	stateDiscovering
	stateSelect
	stateRunning
)

type runMode int

const (
	runNone runMode = iota
	runBuildAndMaybeTest
	runTestsOnly
)

// RunPlan is what the TUI hands back to main after the program exits.
// Tests run after the program exits so ctest gets the real terminal.
type RunPlan struct {
	BuildDir    string
	InstallDir  string
	Tests       []string // empty means don't run ctest
	FixRPath    bool
	BuildFailed bool
	Cancelled   bool

	// SelTargets/SelTests record the full checklist selection at exit,
	// independent of run semantics: a failed build or a test-only run still
	// records exactly what was checked. main persists these to the dot file.
	SelTargets []string
	SelTests   []string
}

type Config struct {
	SourceDir  string
	BuildDir   string
	InstallDir string
	Jobs       int
	FixRPath   bool

	// Preselected skips the TUI's own build-dir/install-dir input states:
	// main has already resolved BuildDir/InstallDir (via stdin + filepicker)
	// so the model goes straight to discovery.
	Preselected bool
}

type Model struct {
	cfg Config

	state   state
	runMode runMode

	sourceDir    string
	buildDir     string
	installDir   string
	buildInput   textinput.Model
	installInput textinput.Model
	filterInput  textinput.Model
	completions  []string // path-completion candidates from the last Tab press
	filterMode   bool
	visible      []int // indexes into items; rebuilt when filter changes

	spinner spinner.Model

	targets []string
	tests   []string

	items  []checkItem
	cursor int // index into visible
	scroll int

	viewport viewport.Model
	logBuf   bytes.Buffer

	width  int
	height int

	errMsg string
	plan   RunPlan

	prog *tea.Program // set by main after construction; used to stream build output
}

// SetProgram lets main hand the running program back to the model so the
// build goroutine can stream output via prog.Send.
func (m *Model) SetProgram(p *tea.Program) { m.prog = p }

// FailedBuildOutput returns whatever was captured from the last build, so
// main can print it after the TUI exits when the build failed.
func (m *Model) FailedBuildOutput() string { return m.logBuf.String() }

func New(cfg Config) *Model {
	bi := newTextInput("/path/to/build", cfg.BuildDir)
	bi.Focus()
	ii := newTextInput("(optional) /path/to/install", cfg.InstallDir)
	fi := newTextInput("filter (substring, case-insensitive)", "")
	fi.Prompt = "/ "

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	m := &Model{
		cfg:          cfg,
		state:        stateInputBuildDir,
		sourceDir:    cfg.SourceDir,
		buildDir:     cfg.BuildDir,
		installDir:   cfg.InstallDir,
		buildInput:   bi,
		installInput: ii,
		filterInput:  fi,
		spinner:      sp,
	}
	switch {
	case cfg.Preselected:
		// main already resolved the build/install dirs; skip the input
		// states entirely and start discovery on Init.
		m.buildInput.Blur()
		m.installInput.Blur()
		m.state = stateDiscovering
	case cmake.IsBuildDir(cfg.BuildDir):
		m.buildInput.Blur()
		m.installInput.Focus()
		m.state = stateInputInstallDir
	}
	return m
}

func (m *Model) Plan() RunPlan { return m.plan }

func (m *Model) Init() tea.Cmd {
	if m.state == stateDiscovering {
		return tea.Batch(m.spinner.Tick, m.discoverCmd())
	}
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
		m.viewport.SetContent(m.logBuf.String())
		m.viewport.GotoBottom()
		m.plan.BuildDir = m.buildDir
		m.plan.InstallDir = m.installDir
		m.plan.FixRPath = m.cfg.FixRPath
		// Record the full selection before Tests may be cleared on failure.
		m.plan.SelTargets = m.checkedTargets()
		m.plan.SelTests = m.checkedTests()
		if v.err != nil {
			m.plan.BuildFailed = true
			m.plan.Tests = nil
			m.errMsg = "build failed: " + v.err.Error()
		} else {
			m.plan.Tests = m.checkedTests()
		}
		return m, tea.Quit
	}

	switch m.state {
	case stateInputBuildDir, stateInputInstallDir:
		return m.updateInput(msg)
	case stateDiscovering:
		if k, ok := msg.(tea.KeyMsg); ok {
			if k.Type == tea.KeyCtrlC || k.Type == tea.KeyEsc {
				m.plan.Cancelled = true
				return m, tea.Quit
			}
		}
		return m, nil
	case stateSelect:
		next, cmd := m.updateChecklist(msg)
		// If checklist requested test-only run, build the plan now.
		if m.runMode == runTestsOnly && len(m.plan.Tests) == 0 {
			m.plan.BuildDir = m.buildDir
			m.plan.InstallDir = m.installDir
			m.plan.FixRPath = m.cfg.FixRPath
			m.plan.Tests = m.checkedTests()
			m.plan.SelTargets = m.checkedTargets()
			m.plan.SelTests = m.checkedTests()
		}
		return next, cmd
	case stateRunning:
		if k, ok := msg.(tea.KeyMsg); ok {
			switch k.String() {
			case "ctrl+c", "q", "esc":
				m.plan.Cancelled = true
				return m, tea.Quit
			}
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) View() string {
	switch m.state {
	case stateInputBuildDir, stateInputInstallDir:
		return m.viewInput()
	case stateDiscovering:
		return fmt.Sprintf("\n%s discovering targets and tests in %s …\n",
			m.spinner.View(), m.buildDir)
	case stateSelect:
		return m.viewChecklist()
	case stateRunning:
		var b strings.Builder
		b.WriteString(TitleStyle.Render(fmt.Sprintf("Building %d target(s)", len(m.checkedTargets()))))
		b.WriteString("\n")
		b.WriteString(m.viewport.View())
		b.WriteString("\n")
		if m.errMsg != "" {
			b.WriteString(ErrorStyle.Render(m.errMsg))
			b.WriteString("\n")
		}
		b.WriteString(HelpStyle.Render("q: cancel"))
		return b.String()
	}
	return ""
}

// ---- discovery command ----

type discoverDoneMsg struct {
	targets []string
	tests   []string
	err     error
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
		go func() {
			t, err := cmake.DiscoverTargets(bd)
			tch <- result{t, err}
		}()
		go func() {
			n, err := cmake.DiscoverTests(bd)
			nch <- result{n, err}
		}()
		tr := <-tch
		nr := <-nch
		var err error
		switch {
		case tr.err != nil:
			err = tr.err
		case nr.err != nil:
			err = nr.err
		}
		return discoverDoneMsg{targets: tr.vals, tests: nr.vals, err: err}
	}
}

// ---- build command ----

type buildChunkMsg []byte
type buildDoneMsg struct{ err error }

// streamWriter forwards every Write into a buildChunkMsg via prog.Send.
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

func (m *Model) startBuild() (tea.Model, tea.Cmd) {
	m.state = stateRunning
	m.logBuf.Reset()
	m.viewport = viewport.New(maxInt(m.width, 80), maxInt(6, m.height-6))
	m.viewport.SetContent("starting build…\n")

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
