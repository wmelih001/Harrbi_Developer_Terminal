package main

import (
	"fmt"
	"os"

	"devterminal/pkg/ui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	opts, cleanup := programOptions()
	defer cleanup()

	p := tea.NewProgram(ui.NewMainModel(), opts...)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}
