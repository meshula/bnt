package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type itemKind int

const (
	itemTarget itemKind = iota
	itemTest
)

type checkItem struct {
	name    string
	kind    itemKind
	checked bool
}

func (m *Model) buildChecklist() {
	savedTargets := toSet(m.cfg.SavedTargets)
	savedTests := toSet(m.cfg.SavedTests)

	m.items = m.items[:0]
	for _, t := range m.targets {
		m.items = append(m.items, checkItem{name: t, kind: itemTarget, checked: savedTargets[t]})
	}
	for _, t := range m.tests {
		m.items = append(m.items, checkItem{name: t, kind: itemTest, checked: savedTests[t]})
	}
	m.recomputeVisible()
	m.cursor = 0
	m.scroll = 0
}

func toSet(ss []string) map[string]bool {
	if len(ss) == 0 {
		return nil
	}
	s := make(map[string]bool, len(ss))
	for _, v := range ss {
		s[v] = true
	}
	return s
}

// recomputeVisible rebuilds m.visible from the current filter text. The
// filter is a case-insensitive substring match against the item name. If the
// filter is empty, every item is visible.
func (m *Model) recomputeVisible() {
	q := strings.ToLower(strings.TrimSpace(m.filterInput.Value()))
	m.visible = m.visible[:0]
	for i, it := range m.items {
		if q == "" || strings.Contains(strings.ToLower(it.name), q) {
			m.visible = append(m.visible, i)
		}
	}
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// pageSize is the number of items we can show given the current terminal
// height, leaving room for title, filter line, section headers, footer.
func (m *Model) pageSize() int {
	const chrome = 9
	if m.height <= chrome+3 {
		return 10
	}
	return m.height - chrome
}

// ensureCursorVisible scrolls the visible window to keep the cursor on screen.
func (m *Model) ensureCursorVisible() {
	page := m.pageSize()
	if m.cursor < m.scroll {
		m.scroll = m.cursor
	}
	if m.cursor >= m.scroll+page {
		m.scroll = m.cursor - page + 1
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
}

// itemAtCursor returns a pointer to the actual item at the current cursor,
// resolving through m.visible. Returns nil if the visible list is empty.
func (m *Model) itemAtCursor() *checkItem {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return nil
	}
	return &m.items[m.visible[m.cursor]]
}

func (m *Model) updateChecklist(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Filter mode swallows most keys and routes them into the textinput.
	if m.filterMode {
		return m.updateFilterMode(msg)
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	page := m.pageSize()
	switch k.String() {
	case "ctrl+c", "q":
		m.exitCode = 0
		return m, tea.Quit
	case "/":
		m.filterMode = true
		m.filterInput.Focus()
		m.errMsg = ""
		return m, nil
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.visible)-1 {
			m.cursor++
		}
	case "pgup", "ctrl+u":
		m.cursor -= page
		if m.cursor < 0 {
			m.cursor = 0
		}
	case "pgdown", "ctrl+d":
		m.cursor += page
		if m.cursor > len(m.visible)-1 {
			m.cursor = len(m.visible) - 1
		}
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = len(m.visible) - 1
	case " ":
		if it := m.itemAtCursor(); it != nil {
			it.checked = !it.checked
		}
	case "a":
		// Toggle every currently visible item of the same kind as the cursor.
		it := m.itemAtCursor()
		if it == nil {
			return m, nil
		}
		kind := it.kind
		anyOff := false
		for _, idx := range m.visible {
			if m.items[idx].kind == kind && !m.items[idx].checked {
				anyOff = true
				break
			}
		}
		for _, idx := range m.visible {
			if m.items[idx].kind == kind {
				m.items[idx].checked = anyOff
			}
		}
	case "c":
		// Clear all selections (everywhere, not just visible).
		for i := range m.items {
			m.items[i].checked = false
		}
	case "b":
		if !m.hasCheckedTargets() {
			m.errMsg = "no build targets selected"
			return m, nil
		}
		m.errMsg = ""
		return m.startBuild()
	case "t":
		if !m.hasCheckedTests() {
			m.errMsg = "no tests selected"
			return m, nil
		}
		m.errMsg = ""
		// Test-only run: libraries are already installed and current, so scope
		// the install to just the subdirs owning the selected tests, then
		// afterInstall runs the rpath fix and ctest.
		m.commitRun()
		return m.startInstall(m.installScope())
	}
	m.ensureCursorVisible()
	return m, nil
}

func (m *Model) updateFilterMode(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.Type {
		case tea.KeyEsc:
			m.filterInput.SetValue("")
			m.filterInput.Blur()
			m.filterMode = false
			m.recomputeVisible()
			m.ensureCursorVisible()
			return m, nil
		case tea.KeyEnter:
			m.filterInput.Blur()
			m.filterMode = false
			m.ensureCursorVisible()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	m.recomputeVisible()
	m.ensureCursorVisible()
	return m, cmd
}

func (m *Model) hasCheckedTargets() bool {
	for _, it := range m.items {
		if it.kind == itemTarget && it.checked {
			return true
		}
	}
	return false
}

func (m *Model) hasCheckedTests() bool {
	for _, it := range m.items {
		if it.kind == itemTest && it.checked {
			return true
		}
	}
	return false
}

func (m *Model) checkedTargets() []string {
	var out []string
	for _, it := range m.items {
		if it.kind == itemTarget && it.checked {
			out = append(out, it.name)
		}
	}
	return out
}

func (m *Model) checkedTests() []string {
	var out []string
	for _, it := range m.items {
		if it.kind == itemTest && it.checked {
			out = append(out, it.name)
		}
	}
	return out
}

func (m *Model) viewChecklist() string {
	var b strings.Builder

	nTargets, nTests := 0, 0
	cTargets, cTests := 0, 0
	for _, it := range m.items {
		switch it.kind {
		case itemTarget:
			nTargets++
			if it.checked {
				cTargets++
			}
		case itemTest:
			nTests++
			if it.checked {
				cTests++
			}
		}
	}

	b.WriteString(TitleStyle.Render(fmt.Sprintf(
		"Select targets and tests  —  targets %d/%d  tests %d/%d",
		cTargets, nTargets, cTests, nTests)))
	b.WriteString("\n")

	// Filter line, always present so the user knows it's there.
	if m.filterMode {
		b.WriteString(SelectedStyle.Render("filter: "))
		b.WriteString(m.filterInput.View())
	} else if v := m.filterInput.Value(); v != "" {
		b.WriteString(SubtitleStyle.Render(fmt.Sprintf("filter: /%s   (press / to edit, esc to clear)", v)))
	} else {
		b.WriteString(SubtitleStyle.Render("press / to filter"))
	}
	b.WriteString("\n")
	b.WriteString(SubtitleStyle.Render(fmt.Sprintf("build dir: %s", m.buildDir)))
	b.WriteString("\n")

	if len(m.visible) == 0 {
		b.WriteString("\n")
		if m.filterInput.Value() != "" {
			b.WriteString(SubtitleStyle.Render("(no items match the filter)"))
		} else {
			b.WriteString(SubtitleStyle.Render("(no targets or tests discovered)"))
		}
		b.WriteString("\n")
		b.WriteString(HelpStyle.Render("/ to filter • esc to clear • q to quit"))
		return b.String()
	}

	page := m.pageSize()
	start := m.scroll
	end := start + page
	if end > len(m.visible) {
		end = len(m.visible)
	}

	var lastKind itemKind = -1
	if start > 0 {
		lastKind = m.items[m.visible[start-1]].kind
	}
	for vi := start; vi < end; vi++ {
		it := m.items[m.visible[vi]]
		if it.kind != lastKind {
			label := "Build targets"
			if it.kind == itemTest {
				label = "Tests"
			}
			b.WriteString(SectionStyle.Render(label))
			b.WriteString("\n")
			lastKind = it.kind
		}

		cursor := "  "
		if vi == m.cursor {
			cursor = CursorStyle.Render("› ")
		}
		box := "[ ]"
		if it.checked {
			box = CheckedStyle.Render("[x]")
		}
		line := fmt.Sprintf("%s%s %s", cursor, box, it.name)
		if vi == m.cursor {
			line = SelectedStyle.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	b.WriteString(SubtitleStyle.Render(fmt.Sprintf("[%d-%d / %d]", start+1, end, len(m.visible))))
	b.WriteString("\n")
	if m.errMsg != "" {
		b.WriteString(ErrorStyle.Render(m.errMsg))
		b.WriteString("\n")
	}
	if m.filterMode {
		b.WriteString(HelpStyle.Render("type to filter • enter: apply • esc: clear"))
	} else {
		b.WriteString(HelpStyle.Render("/: filter • ↑/↓: nav • space: toggle • a: section • c: clear all • b: build • t: tests only • q: quit"))
	}
	return b.String()
}
