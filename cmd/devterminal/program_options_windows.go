//go:build windows

package main

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
	"golang.org/x/sys/windows"
)

type ansiStdinReader struct {
	file *os.File
}

func (r ansiStdinReader) Read(p []byte) (int, error) {
	return r.file.Read(p)
}

func platformProgramOptions() ([]tea.ProgramOption, func()) {
	state, err := term.MakeRaw(os.Stdin.Fd())
	if err != nil {
		return nil, func() {}
	}

	handle := windows.Handle(os.Stdin.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err == nil {
		_ = windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_INPUT)
	}

	cleanup := func() {
		_ = term.Restore(os.Stdin.Fd(), state)
	}

	return []tea.ProgramOption{
		tea.WithInput(ansiStdinReader{file: os.Stdin}),
	}, cleanup
}
