package tui

import (
	"os"

	"github.com/charmbracelet/bubbles/filepicker"
	tea "github.com/charmbracelet/bubbletea"
)

// openPicker initializes the embedded filepicker rooted at the invoking input's
// current directory (or cwd) and switches to stateFilePicker. The state that
// invoked it is remembered in pickerReturn so select/cancel returns there.
func (m *Model) openPicker() (tea.Model, tea.Cmd) {
	m.pickerReturn = m.state
	m.completions = nil
	m.errMsg = ""

	start := m.cwd
	if in := m.activeInput(); in != nil {
		if v := expand(in.Value()); v != "" {
			if st, err := os.Stat(v); err == nil && st.IsDir() {
				start = v
			}
		}
	}

	fp := filepicker.New()
	fp.DirAllowed = true
	fp.FileAllowed = false
	fp.AutoHeight = false
	fp.CurrentDirectory = start
	if m.height > 6 {
		fp.Height = m.height - 6
	} else {
		fp.Height = 10
	}
	m.fp = fp
	m.state = stateFilePicker
	return m, m.fp.Init()
}

// updatePicker drives the embedded filepicker. On DidSelectFile it writes the
// chosen directory into the invoking input and returns to pickerReturn; q /
// ctrl+c cancel back to pickerReturn unchanged.
func (m *Model) updatePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "ctrl+c", "q":
			m.state = m.pickerReturn
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.fp, cmd = m.fp.Update(msg)

	if ok, path := m.fp.DidSelectFile(msg); ok {
		switch m.pickerReturn {
		case stateInputSource:
			m.sourceInput.SetValue(path)
			m.sourceInput.SetCursor(len(path))
		case stateInputBuild:
			m.buildInput.SetValue(path)
			m.buildInput.SetCursor(len(path))
		}
		m.state = m.pickerReturn
		return m, nil
	}
	return m, cmd
}

func (m *Model) viewPicker() string {
	return "\n  Select a directory (enter: choose • ←/h: up • q: cancel)\n\n" + m.fp.View() + "\n"
}
