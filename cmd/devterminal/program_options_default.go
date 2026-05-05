//go:build !windows

package main

import tea "github.com/charmbracelet/bubbletea"

func platformProgramOptions() ([]tea.ProgramOption, func()) {
	return nil, func() {}
}
