package main

import tea "github.com/charmbracelet/bubbletea"

func programOptions() ([]tea.ProgramOption, func()) {
	opts := []tea.ProgramOption{
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	}

	platformOpts, cleanup := platformProgramOptions()
	opts = append(opts, platformOpts...)
	return opts, cleanup
}
