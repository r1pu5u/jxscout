package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/francisconeves97/jxscout/internal/core/errutil"
)

func (t *TUI) Run() error {
	p := tea.NewProgram(
		t,
		tea.WithAltScreen(),
	)
	if _, err := p.Run(); err != nil {
		return errutil.Wrap(err, "error running program")
	}

	return nil
}
