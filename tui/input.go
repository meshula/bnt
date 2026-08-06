package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func newTextInput(placeholder, initial string) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.SetValue(initial)
	ti.CharLimit = 4096
	ti.Width = 64
	ti.Prompt = "› "
	return ti
}

// activeInput returns the currently-focused path input pointer for the input
// states; nil otherwise.
func (m *Model) activeInput() *textinput.Model {
	switch m.state {
	case stateInputBuildDir:
		return &m.buildInput
	case stateInputInstallDir:
		return &m.installInput
	}
	return nil
}

func (m *Model) updateInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.Type {
		case tea.KeyEsc, tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyTab:
			m.handleTabComplete()
			return m, nil
		case tea.KeyEnter:
			switch m.state {
			case stateInputBuildDir:
				v := strings.TrimSpace(m.buildInput.Value())
				if v == "" {
					m.errMsg = "build dir is required"
					return m, nil
				}
				m.errMsg = ""
				m.completions = nil
				m.buildDir = expand(v)
				m.buildInput.Blur()
				m.installInput.Focus()
				m.state = stateInputInstallDir
				return m, nil
			case stateInputInstallDir:
				m.installDir = expand(strings.TrimSpace(m.installInput.Value()))
				m.installInput.Blur()
				m.completions = nil
				m.state = stateDiscovering
				return m, m.discoverCmd()
			}
		default:
			// Any other key invalidates the completion candidates list.
			m.completions = nil
		}
	}

	var cmd tea.Cmd
	switch m.state {
	case stateInputBuildDir:
		m.buildInput, cmd = m.buildInput.Update(msg)
	case stateInputInstallDir:
		m.installInput, cmd = m.installInput.Update(msg)
	}
	return m, cmd
}

// handleTabComplete looks at the active input's current value and either
// extends it to the longest common prefix of matching filesystem entries or,
// if there's a unique match, completes to that entry (adding a trailing /
// when it's a directory). If multiple entries match, they are stashed on the
// model so viewInput can list them under the field.
func (m *Model) handleTabComplete() {
	in := m.activeInput()
	if in == nil {
		return
	}
	cur := in.Value()
	replacement, matches := pathComplete(cur)
	m.completions = nil
	if len(matches) == 0 {
		return
	}
	if replacement != cur {
		in.SetValue(replacement)
		in.SetCursor(len(replacement))
	}
	if len(matches) > 1 {
		m.completions = matches
	}
}

// pathComplete computes a Tab-completion replacement for input. It returns
// the new input string (which may equal input if no extension is possible)
// and the list of matching directory entries. ~/ is expanded for filesystem
// lookup but the replacement preserves the user's original ~/ prefix.
func pathComplete(input string) (string, []string) {
	if input == "" {
		input = "./"
	}

	// Resolve ~/ to a real path for filesystem lookup; remember the literal
	// prefix the user typed so we can rebuild the replacement preserving it.
	// Use string concatenation rather than filepath.Join to preserve any
	// trailing slash — Join calls Clean which would strip it, and we rely
	// on the trailing / to know we should ReadDir the path as-is rather
	// than treat its last component as a partial name.
	expanded := input
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		switch {
		case input == "~":
			expanded = home
		case strings.HasPrefix(input, "~/"):
			expanded = home + input[1:]
		}
	}

	// Split expanded into a directory we can ReadDir and a leading partial
	// name that the user is in the middle of typing.
	var dir, partial string
	if strings.HasSuffix(expanded, "/") {
		dir = expanded
		partial = ""
	} else {
		dir = filepath.Dir(expanded)
		partial = filepath.Base(expanded)
	}
	if dir == "" {
		dir = "."
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return input, nil
	}

	var matches []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, partial) {
			matches = append(matches, name)
		}
	}
	if len(matches) == 0 {
		return input, nil
	}

	lcp := longestCommonPrefix(matches)

	// Reattach lcp to whatever path-prefix the user typed, preserving the
	// original ~/ if it was there. We do this by trimming the partial off
	// the raw input string rather than rewriting the absolute path.
	rawPartial := ""
	if i := strings.LastIndex(input, "/"); i >= 0 {
		rawPartial = input[i+1:]
	} else if !strings.HasPrefix(input, "~") {
		rawPartial = input
	}
	rawPrefix := input[:len(input)-len(rawPartial)]
	replacement := rawPrefix + lcp

	// If the unique match is a directory, append a trailing slash so the
	// next Tab descends into it.
	if len(matches) == 1 {
		full := filepath.Join(dir, lcp)
		if st, err := os.Stat(full); err == nil && st.IsDir() {
			replacement += "/"
		}
	}
	return replacement, matches
}

func longestCommonPrefix(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	prefix := ss[0]
	for _, s := range ss[1:] {
		// Walk byte-by-byte; we only deal with filesystem names so the
		// risk of breaking inside a multi-byte rune is low and the impact
		// is benign (one extra Tab press resolves it).
		i := 0
		for i < len(prefix) && i < len(s) && prefix[i] == s[i] {
			i++
		}
		prefix = prefix[:i]
		if prefix == "" {
			break
		}
	}
	return prefix
}

func (m *Model) viewInput() string {
	var b strings.Builder
	b.WriteString(TitleStyle.Render("bnt — build and test"))
	b.WriteString("\n")
	b.WriteString(SubtitleStyle.Render(fmt.Sprintf("source: %s", m.sourceDir)))
	b.WriteString("\n\n")

	switch m.state {
	case stateInputBuildDir:
		b.WriteString(SectionStyle.Render("CMake build directory"))
		b.WriteString("\n")
		b.WriteString(m.buildInput.View())
	case stateInputInstallDir:
		b.WriteString(SubtitleStyle.Render(fmt.Sprintf("build dir: %s", m.buildDir)))
		b.WriteString("\n\n")
		b.WriteString(SectionStyle.Render("Install (optional, press Enter to skip)"))
		b.WriteString("\n")
		b.WriteString(SubtitleStyle.Render("relative path → subdir of build to install (e.g. pxr/usd/usdProfiles)"))
		b.WriteString("\n")
		b.WriteString(SubtitleStyle.Render("absolute path outside build → --prefix override"))
		b.WriteString("\n")
		b.WriteString(m.installInput.View())
	}

	if len(m.completions) > 0 {
		b.WriteString("\n\n")
		b.WriteString(SubtitleStyle.Render(formatCompletions(m.completions, m.width)))
	}

	if m.errMsg != "" {
		b.WriteString("\n\n")
		b.WriteString(ErrorStyle.Render(m.errMsg))
	}
	b.WriteString("\n")
	b.WriteString(HelpStyle.Render("tab: complete • enter: continue • esc: quit"))
	return b.String()
}

// formatCompletions packs candidates into rows that fit termWidth columns.
// Caps the output at a couple of rows so the field doesn't get pushed off
// screen on a directory full of entries.
func formatCompletions(items []string, termWidth int) string {
	const maxRows = 6
	if termWidth <= 0 {
		termWidth = 80
	}
	maxName := 0
	for _, s := range items {
		if len(s) > maxName {
			maxName = len(s)
		}
	}
	colW := maxName + 2
	cols := termWidth / colW
	if cols < 1 {
		cols = 1
	}
	maxItems := cols * maxRows
	truncated := items
	suffix := ""
	if len(items) > maxItems {
		truncated = items[:maxItems]
		suffix = fmt.Sprintf("  (+%d more)", len(items)-maxItems)
	}

	var b strings.Builder
	for i, s := range truncated {
		b.WriteString(s)
		// Pad to column width unless this is the last column on a row.
		if (i+1)%cols == 0 || i == len(truncated)-1 {
			b.WriteString("\n")
		} else {
			b.WriteString(strings.Repeat(" ", colW-len(s)))
		}
	}
	if suffix != "" {
		b.WriteString(suffix)
	}
	return strings.TrimRight(b.String(), "\n")
}
