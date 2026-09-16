package tui

import (
	"github.com/charmbracelet/bubbles/filepicker"
	tea "github.com/charmbracelet/bubbletea"
)

// pickModel wraps the filepicker bubble for a single directory selection. It
// quits when the user selects a directory (storing the path) or cancels with
// ctrl+c / q (leaving path empty). esc / h / ← remain the filepicker's "back"
// navigation.
type pickModel struct {
	fp       filepicker.Model
	path     string
	quitting bool
}

func (m pickModel) Init() tea.Cmd { return m.fp.Init() }

func (m pickModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.fp, cmd = m.fp.Update(msg)

	if ok, path := m.fp.DidSelectFile(msg); ok {
		m.path = path
		m.quitting = true
		return m, tea.Quit
	}
	return m, cmd
}

func (m pickModel) View() string {
	if m.quitting {
		return ""
	}
	return "\n  Select a directory (enter: choose • ←/h: up • q: cancel)\n\n" + m.fp.View() + "\n"
}

// PickDirectory runs a short filepicker program rooted at start and returns the
// selected directory, or "" if the user cancelled.
func PickDirectory(start string) (string, error) {
	fp := filepicker.New()
	fp.DirAllowed = true
	fp.FileAllowed = false
	fp.AutoHeight = true
	fp.CurrentDirectory = start

	m := pickModel{fp: fp}
	res, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return "", err
	}
	return res.(pickModel).path, nil
}
