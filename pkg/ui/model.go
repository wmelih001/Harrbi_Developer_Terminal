package ui

import (
	"devterminal/pkg/config"
	"devterminal/pkg/domain"
	"devterminal/pkg/service"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SessionState defines the current view
type SessionState int

const (
	StateDashboard SessionState = iota
	StateProjectSelect
	StateProjectActions // Submenu for launch options
	StateScanning
	StateFirstRun
	StateContextGen
	StateDependencyDoctor
	StateNgrok
	StatePortCheckWarning
	StateHealthScore
	StateTaskRunner
	StateSplash
	StateUpdateFeedback
)

type NgrokStep int

const (
	NgrokMainMenu NgrokStep = iota // New Start Screen
	NgrokCheckInstall
	NgrokModeSelect // Ask user if installed or not
	NgrokManualPath // New: Input path
	NgrokCheckAuth
	NgrokAuth
	NgrokAskPort
	NgrokRunning
)

type MainModel struct {
	Config *domain.Config
	// Services
	Scanner  *service.Scanner
	TreeGen  *service.TreeGenerator
	Launcher *service.Launcher
	Doctor   *service.Doctor
	State    SessionState

	// Components
	List           list.Model
	TaskRunnerList list.Model
	Table          table.Model
	Spinner        spinner.Model
	FirstRunInput  textinput.Model

	// Update Feedback Components
	UpdateProgress   progress.Model
	UpdateViewport   viewport.Model
	UpdateLogs       []string
	UpdateCommands   []*exec.Cmd
	UpdateCurrentCmd int
	UpdatePkgs       []string
	UpdateDone       bool
	UpdateHasError   bool
	UpdateVersions   map[string]UpdateVersionInfo

	// Ngrok
	NgrokService    *service.NgrokService
	HealthService   *service.HealthService
	NgrokStep       NgrokStep
	NgrokPathInput  textinput.Model
	NgrokPortInput  textinput.Model
	NgrokTokenInput textinput.Model
	NgrokCmd        string // final command to run

	// Data
	Projects []domain.Project
	Selected *domain.Project

	// Error handling
	Err    error
	Width  int
	Height int

	// Project actions viewport
	ActionScroll int
	HealthScroll int

	// Feedback Flags
	CopiedSuccess       bool
	AllPackagesUpToDate bool

	// Port Check
	PortWarnings      []service.PortInfo
	PendingLaunchMode string

	// Health Score
	HealthReport *service.HealthReport

	// Doctor State
	IsUpdatingDependencies bool // Güncelleme işlemi sürüyor mu? (Legacy flag, maybe unused in new state)
	DependencyTargets      []service.DependencyTarget
	DependencyTargetIndex  int

	// Splash
	SplashProgress float64
}

type UpdateVersionInfo struct {
	From string
	To   string
}

type dbToolAction int

const (
	dbToolNone dbToolAction = iota
	dbToolPrisma
	dbToolDrizzle
	dbToolHasura
	dbToolSupabase
	dbToolStorybook
)

// Custom Messages
type splashTickMsg time.Time
type packageUpdateCompleteMsg struct{}

type copiedResetMsg struct{}
type updateLogMsg string
type updateCmdFinishedMsg struct {
	output string
	err    error
}

// Async Update Preparation
type updatePrepMsg struct {
	cmds     []*exec.Cmd
	pkgs     []string
	versions map[string]UpdateVersionInfo
	err      error
}

// Simulated Progress Ticker
type progressTickMsg time.Time

// NOTE: Since full streaming is complex to implement inline without `Program` ref,
// I will simulate the "Downloading..." phases visually in the View using the Spinner
// and show the final logs.
// However, I CAN output logs if I use a channel.
// Let's stick to the simpler `CombinedOutput` for now but enable the UI *view* to show the pending packages.
// The user will see "Updating X, Y, Z..." and a spinner.
// When finished, they get the result.
// "Gelişmiş" ui request implies visual richness. The Spinner + Checklist is rich.
// Real-time progress bar for `pub add` is overkill/impossible without pipe.
// I will stick to: List packages. Show "Pending" -> "Done".
// It's effectively batch, so all go "Done" at once.
// I'll assume this is acceptable if the UI is pretty.
//
// WAIT. The user said "alt alta sıralansın".
// I'll list them.

func NewMainModel() *MainModel {
	cfg, err := config.LoadConfig()
	if err != nil {
		// handle fatal error or use empty config
		cfg = &domain.Config{}
	}

	s := spinner.New()
	s.Spinner = spinner.Dot
	// s.Style removed to prevent alignment issues

	tiPort := textinput.New()
	tiPort.Placeholder = "3000"
	tiPort.SetValue("3000")
	tiPort.CharLimit = 5
	tiPort.Width = 10

	tiToken := textinput.New()
	tiToken.Placeholder = "Authtoken"
	tiToken.CharLimit = 100
	tiToken.Width = 40
	tiToken.EchoMode = textinput.EchoPassword

	tiPath := textinput.New()
	tiPath.Placeholder = "C:\\path\\to\\ngrok.exe"
	tiPath.Width = 60

	// First Run Input
	tiFirstRun := textinput.New()
	tiFirstRun.Placeholder = "C:\\Projelerim (Tırnak işaretleri temizlenir)"
	tiFirstRun.Width = 60
	tiFirstRun.Focus()

	// Initial State: Splash Screen (if configured) or Scanning
	initialState := StateSplash

	// Ancak Config yüklü değilse/ilk çalışmaysa FirstRun
	if len(cfg.ProjectsPaths) == 0 {
		initialState = StateFirstRun
	}

	return &MainModel{
		Config:          cfg,
		Scanner:         service.NewScanner(cfg),
		TreeGen:         service.NewTreeGenerator(cfg),
		Launcher:        service.NewLauncher(cfg),
		Doctor:          service.NewDoctor(cfg),
		NgrokService:    service.NewNgrokService(cfg),
		HealthService:   service.NewHealthService(),
		NgrokPathInput:  tiPath,
		NgrokPortInput:  tiPort,
		NgrokTokenInput: tiToken,
		FirstRunInput:   tiFirstRun,
		State:           initialState,
		Spinner:         s,
		List:            list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0),
		TaskRunnerList:  list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0),
		Table:           newTable(),
		UpdateViewport:  viewport.New(100, 10), // Default size, key to resize later
	}
}

func newTable() table.Model {
	columns := []table.Column{
		{Title: "Paket", Width: 20},
		{Title: "Mevcut", Width: 10},
		{Title: "İstenen", Width: 10},
		{Title: "Son", Width: 10},
	}
	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(7),
	)
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)
	return t
}

func (m *MainModel) Init() tea.Cmd {
	var cmds []tea.Cmd

	if m.State == StateScanning {
		cmds = append(cmds, m.startScanningCmd())
	}
	if m.State == StateSplash {
		cmds = append(cmds, tea.Tick(time.Millisecond*50, func(t time.Time) tea.Msg {
			return splashTickMsg(t)
		}))
	}
	return tea.Batch(cmds...)
}

func (m *MainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.MouseMsg:
		if m.State == StateProjectActions {
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				m.scrollProjectActions(-3)
				return m, nil
			case tea.MouseButtonWheelDown:
				m.scrollProjectActions(3)
				return m, nil
			}

			if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
				if action := m.dbToolActionAtY(msg.Y); action != dbToolNone {
					return m, m.launchDbToolCmd(action)
				}
			}
		}
		if m.State == StateHealthScore {
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				m.scrollHealthScore(-3)
				return m, nil
			case tea.MouseButtonWheelDown:
				m.scrollHealthScore(3)
				return m, nil
			}
		}

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		// Context Global Back (Esc)
		if msg.String() == "esc" {
			if m.State == StateProjectActions {
				m.State = StateProjectSelect
				// Listeyi son açılanlara göre yeniden sırala
				return m, func() tea.Msg { return projectMsg(m.Projects) }
			}
		}

		// State Specific Handling
		switch m.State {
		case StateSplash:
			// Allow skipping splash
			if msg.String() == "enter" || msg.String() == "esc" || msg.String() == " " {
				m.State = StateScanning
				return m, m.startScanningCmd()
			}

		case StateFirstRun:
			var cmd tea.Cmd
			m.FirstRunInput, cmd = m.FirstRunInput.Update(msg)
			if msg.String() == "enter" {
				path := strings.Trim(m.FirstRunInput.Value(), "\"")
				if path != "" {
					m.Config.ProjectsPaths = []string{path}
					// Proje yolunu config'e kaydet
					if err := config.SaveConfig(m.Config); err != nil {
						m.Err = err
					} else {
						m.State = StateScanning
						cmds = append(cmds, m.startScanningCmd())
					}
					return m, tea.Batch(cmds...)
				}
			}
			return m, cmd

		case StateDashboard:
			switch msg.String() {
			case "1":
				m.State = StateProjectSelect
				m.List.Title = "Proje Seçiniz"
				cmds = append(cmds, m.List.StartSpinner())
			case "2":
				m.State = StateProjectSelect
				m.List.Title = "Bağımlılık Kontrolü İçin Proje Seç"
				// Seçimden sonra StateDependencyDoctor'a geçiş yukarıdaki logic'te
			case "3":
				// Start Ngrok Flow
				m.State = StateNgrok
				m.NgrokStep = NgrokMainMenu
				return m, nil
			case "q":
				return m, tea.Quit
			}

		case StateDependencyDoctor:
			// Eğer güncelleme sürüyorsa sadece Q ve Spinner'a izin ver
			if m.IsUpdatingDependencies {
				if msg.String() == "q" {
					return m, tea.Quit
				}
				// Spinner update already handled in global switch below (or above)?
				// Actually we need to explicitly handle spinner update here or ensure it falls through.
				// In the current structure, spinner is updated in the "Alt bileşenleri güncelle" section.
				// So we just return here to block other inputs.
				return m, nil
			}

			// Tablo navigasyonu için event'i tabloya ilet
			m.Table, cmd = m.Table.Update(msg)
			cmds = append(cmds, cmd)

			switch msg.String() {
			case "esc":
				m.State = StateProjectActions
				m.Err = nil
				return m, nil
			case "q":
				return m, tea.Quit
			case "tab":
				m.ensureDependencyTargets()
				if len(m.DependencyTargets) > 1 {
					m.DependencyTargetIndex = (m.DependencyTargetIndex + 1) % len(m.DependencyTargets)
					m.Table.SetRows([]table.Row{})
					m.AllPackagesUpToDate = false
					m.Err = nil
					m.IsUpdatingDependencies = true
					return m, tea.Batch(m.Spinner.Tick, m.checkDependenciesCmd())
				}
			case "f":
				// Update ALL packages (only those not up-to-date)
				var pkgsToUpdate []string
				var versionsToUpdate = make(map[string]UpdateVersionInfo)

				rows := m.Table.Rows()
				for _, row := range rows {
					if len(row) > 3 {
						// row[1] = Current, row[3] = Latest (display)
						// If row[3] is "latest", it means up to date.
						// Also check if they are equal string wise just in case.
						if row[3] != "latest" && row[1] != row[3] && row[1] != "?" {
							pkgsToUpdate = append(pkgsToUpdate, row[0])
							versionsToUpdate[row[0]] = UpdateVersionInfo{From: row[1], To: row[3]}
						}
					}
				}

				if len(pkgsToUpdate) == 0 {
					return m, nil // Nothing to update
				}

				// Switch to Spinner immediately
				m.IsUpdatingDependencies = true
				// Async preparation
				return m, tea.Batch(m.Spinner.Tick, m.prepareUpdateCmd(pkgsToUpdate, versionsToUpdate))

			case "enter":
				// Update SELECTED package
				if len(m.Table.Rows()) > 0 {
					row := m.Table.SelectedRow()
					if len(row) > 3 {
						// Check if already latest
						if row[3] == "latest" || row[1] == row[3] {
							return m, nil // Already up to date
						}

						pkgName := row[0]
						versionsToUpdate := make(map[string]UpdateVersionInfo)
						versionsToUpdate[pkgName] = UpdateVersionInfo{From: row[1], To: row[3]}

						m.IsUpdatingDependencies = true
						return m, tea.Batch(m.Spinner.Tick, m.prepareUpdateCmd([]string{pkgName}, versionsToUpdate))
					}
				}
			}

		case StateUpdateFeedback:
			if m.UpdateDone {
				switch msg.String() {
				case "esc", "enter", "q":
					// Return to Doctor and Refresh
					m.State = StateDependencyDoctor
					m.IsUpdatingDependencies = true // Show spinner while re-checking
					m.UpdateLogs = nil
					m.UpdateCommands = nil
					m.UpdatePkgs = nil
					m.UpdateVersions = nil
					m.Table.SetRows([]table.Row{})
					m.AllPackagesUpToDate = false
					return m, tea.Batch(m.Spinner.Tick, m.checkDependenciesCmd())
				}
			}

		case StateHealthScore:
			if m.handleHealthScoreScrollKey(msg.String()) {
				return m, nil
			}
			if msg.String() == "esc" {
				m.State = StateProjectActions
				return m, nil
			}
			if msg.String() == "q" {
				return m, tea.Quit
			}

		case StateTaskRunner:
			if msg.String() == "esc" {
				m.State = StateProjectActions
				return m, nil
			}
			if msg.String() == "q" {
				return m, tea.Quit
			}

			if msg.String() == "enter" {
				// Run selected script
				i, ok := m.TaskRunnerList.SelectedItem().(scriptItem)
				if ok {
					return m, func() tea.Msg {
						_ = m.Launcher.LaunchScript(*m.Selected, i.name, i.cmd)
						return nil
					}
				}
			}

			var cmd tea.Cmd
			m.TaskRunnerList, cmd = m.TaskRunnerList.Update(msg)
			return m, cmd

		case StateNgrok:
			switch m.NgrokStep {
			case NgrokMainMenu:
				switch msg.String() {
				case "1":
					m.NgrokStep = NgrokManualPath
					m.NgrokPathInput.Focus()
				case "2":
					// Try everything until something works
					url := "https://ngrok.com/download/windows"
					go func() {
						// Strategy 1: Explorer
						if err := exec.Command("explorer", url).Start(); err == nil {
							return
						}
						// Strategy 2: Rundll32
						if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start(); err == nil {
							return
						}
						// Strategy 3: Cmd Start (Standard)
						if err := exec.Command("cmd", "/c", "start", "", url).Start(); err == nil {
							return
						}
						if err := exec.Command("powershell", "-c", "Start-Process", fmt.Sprintf("'%s'", url)).Start(); err == nil {
							return
						}
						// Strategy 4: PowerShell
						_ = exec.Command("pwsh", "-c", "Start-Process", fmt.Sprintf("'%s'", url)).Start()
					}()

					m.NgrokCmd = "downloading"
					m.NgrokStep = NgrokManualPath
					m.NgrokPathInput.Focus()
				case "esc":
					m.State = StateProjectActions // Return to Project Menu
				}

			case NgrokCheckInstall:
				// Handled by Cmd
			case NgrokModeSelect:
				// Removed/Unused but keeping safe
				if msg.String() == "esc" {
					m.State = StateProjectActions
				}
			case NgrokManualPath:
				var cmd tea.Cmd
				m.NgrokPathInput, cmd = m.NgrokPathInput.Update(msg)
				if msg.String() == "enter" {
					path := strings.Trim(m.NgrokPathInput.Value(), "\"")
					if m.NgrokService.ValidatePath(path) {
						m.NgrokService.SavePath(path)
						m.NgrokStep = NgrokAuth   // Skip check, go straight to Auth
						m.NgrokTokenInput.Focus() // FIX: Focus the input!
						return m, nil
					}
					// Invalid? Shake or show error (for now just stay)
				}
				if msg.String() == "esc" {
					m.NgrokStep = NgrokMainMenu
					m.NgrokPathInput.Blur()
				}
				return m, cmd

			case NgrokCheckAuth:
				// Removed/Skipped
				m.NgrokStep = NgrokAuth
				return m, nil

			case NgrokAuth:
				var cmd tea.Cmd
				m.NgrokTokenInput, cmd = m.NgrokTokenInput.Update(msg)

				switch msg.String() {
				case "enter":
					token := m.NgrokTokenInput.Value()
					if token != "" {
						_ = m.NgrokService.SetAuthToken(token)
						m.NgrokStep = NgrokAskPort
						m.NgrokPortInput.Focus()
						return m, nil
					}
				case "ctrl+o":
					// Open Dashboard
					url := "https://dashboard.ngrok.com/get-started/your-authtoken"
					go func() {
						_ = exec.Command("cmd", "/c", "start", "", url).Start()
					}()
					return m, nil
				case "esc":
					m.NgrokStep = NgrokMainMenu
					m.NgrokTokenInput.Blur()
				}
				return m, cmd
			case NgrokAskPort:
				var cmd tea.Cmd
				m.NgrokPortInput, cmd = m.NgrokPortInput.Update(msg)
				if msg.String() == "enter" {
					m.NgrokStep = NgrokRunning
					port := m.NgrokPortInput.Value()
					exe := m.NgrokService.GetExecutable() // Use resolved path
					return m, func() tea.Msg {
						// We need to quote the exe path in case of spaces
						// wt command: wt new-tab ... cmd /k "path/to/ngrok" http 3000
						c := exec.Command("wt.exe", "new-tab", "-d", ".", "--title", "Ngrok "+port, "cmd", "/k", "\""+exe+"\"", "http", port)
						c.Start()
						return nil
					}
				}
				if msg.String() == "ctrl+r" {
					// Force Reset: Clear path and go to main menu
					m.NgrokService.SavePath("") // Clear config
					m.NgrokStep = NgrokMainMenu
					return m, nil
				}
				if msg.String() == "esc" {
					// Smart Back: If we are fully configured, go back to Project Menu.
					// If we are setting up manually, go back to Main Menu.
					if m.NgrokService.GetExecutable() != "" {
						m.State = StateProjectActions
					} else {
						m.NgrokStep = NgrokMainMenu
					}
					m.NgrokPortInput.Blur()
				}
				return m, cmd
			case NgrokRunning:
				if msg.String() == "esc" {
					m.State = StateProjectActions
				}
			}

			// Global Ngrok Quit
			if msg.String() == "q" {
				return m, tea.Quit
			}

		case StatePortCheckWarning:
			switch msg.String() {
			case "y", "Y", "enter":
				// Devam et
				// Launch
				mode := m.PendingLaunchMode
				m.State = StateProjectActions
				// Son açılanları HEMEN güncelle (sync)
				m.updateLastOpened(m.Selected.Path)
				return m, func() tea.Msg { m.Launcher.LaunchProject(m.Selected, mode); return nil }
			case "n", "N", "esc":
				// [Esc] Geri Dön
				m.State = StateProjectActions
				m.PortWarnings = nil
				return m, nil
			case "1":
				// [1] Açık olan sunucuyu kapat ve bu sunucuyu aç
				// Tüm çakışan portları öldür
				for _, w := range m.PortWarnings {
					_ = service.KillPort(w.Port)
				}
				// Biraz bekle (işletim sistemi portu serbest bıraksın)
				time.Sleep(1 * time.Second)
				// Sonra başlat
				mode := m.PendingLaunchMode
				m.State = StateProjectActions
				// Son açılanları HEMEN güncelle (sync)
				m.updateLastOpened(m.Selected.Path)
				return m, func() tea.Msg { m.Launcher.LaunchProject(m.Selected, mode); return nil }
			case "2":
				// [2] Açık olan portu kapat (Sadece öldür, başlatma)
				for _, w := range m.PortWarnings {
					_ = service.KillPort(w.Port)
				}
				m.State = StateProjectActions
				m.PortWarnings = nil
				return m, nil // tea.Quit yerine menüye dönmek daha mantıklı, kullanıcı belki başka işlem yapar
			case "q":
				return m, tea.Quit
			}

		case StateProjectActions:
			if action := dbToolActionForKey(msg.String(), m.Selected); action != dbToolNone {
				return m, m.launchDbToolCmd(action)
			}
			if m.handleProjectActionsScrollKey(msg.String()) {
				return m, nil
			}

			switch msg.String() {
			case "1", "f":
				// Port Check: Frontend
				warnings := service.CheckProjectPortsForProject(*m.Selected, "frontend")
				if len(warnings) > 0 {
					m.PortWarnings = warnings
					m.PendingLaunchMode = "frontend"
					m.State = StatePortCheckWarning
					return m, nil
				}
				// Son açılanları HEMEN güncelle (sync)
				m.updateLastOpened(m.Selected.Path)
				return m, func() tea.Msg { m.Launcher.LaunchProject(m.Selected, "frontend"); return nil }
			case "2", "b":
				// Port Check: Backend
				warnings := service.CheckProjectPortsForProject(*m.Selected, "backend")
				if len(warnings) > 0 {
					m.PortWarnings = warnings
					m.PendingLaunchMode = "backend"
					m.State = StatePortCheckWarning
					return m, nil
				}
				// Son açılanları HEMEN güncelle (sync)
				m.updateLastOpened(m.Selected.Path)
				return m, func() tea.Msg { m.Launcher.LaunchProject(m.Selected, "backend"); return nil }
			case "3", "l":
				// Port Check: Full
				warnings := service.CheckProjectPortsForProject(*m.Selected, "full")
				if len(warnings) > 0 {
					m.PortWarnings = warnings
					m.PendingLaunchMode = "full"
					m.State = StatePortCheckWarning
					return m, nil
				}
				// Son açılanları HEMEN güncelle (sync)
				m.updateLastOpened(m.Selected.Path)
				return m, func() tea.Msg { m.Launcher.LaunchProject(m.Selected, "full"); return nil }
			case "4":
				// Ngrok Flow - Smart Skip
				m.State = StateNgrok
				if m.NgrokService.GetExecutable() != "" {
					// Path is known, assume detailed setup is done -> Jump to Port
					m.NgrokStep = NgrokAskPort
					m.NgrokPortInput.Focus()
				} else {
					// First time? Show Setup Menu
					m.NgrokStep = NgrokMainMenu
				}
				return m, nil
			case "5", "c":
				// AI Context
				return m, func() tea.Msg {
					tree, err := m.TreeGen.GenerateTree(m.Selected.Path)
					if err != nil {
						return errMsg(err)
					}
					return contextMsg(tree)
				}
			case "6":
				// Doctor
				m.State = StateDependencyDoctor
				m.resetDependencyDoctor()
				return m, tea.Batch(m.Spinner.Tick, m.checkDependenciesCmd())

			case "h", "H": // Hidden shortcut for health? No, let's stick to requested "
				// Health Score Trigger
				report := m.HealthService.CheckProjectHealth(*m.Selected)
				m.HealthReport = &report
				m.HealthScroll = 0
				m.State = StateHealthScore
				return m, nil
			case "7", "t":
				// Task Runner
				if len(m.Selected.Scripts) == 0 {
					return m, nil // Script yoksa işlem yapma
				}

				var items []list.Item
				// Scriptleri listeye ekle
				for name, cmd := range m.Selected.Scripts {
					items = append(items, scriptItem{name: name, cmd: cmd})
				}
				// Sort by name
				sort.Slice(items, func(i, j int) bool {
					return items[i].(scriptItem).name < items[j].(scriptItem).name
				})

				m.TaskRunnerList.SetItems(items)
				m.TaskRunnerList.Title = "📜 " + m.Selected.Name + " Scriptleri"
				m.TaskRunnerList.SetStatusBarItemName("Script", "Script")
				m.TaskRunnerList.FilterInput.Prompt = "🔍 Ara: "
				m.TaskRunnerList.DisableQuitKeybindings()

				// Customize Keybindings and Help for Task Runner
				m.TaskRunnerList.KeyMap.CursorUp.SetHelp("↑", "Yukarı")
				m.TaskRunnerList.KeyMap.CursorDown.SetHelp("↓", "Aşağı")
				m.TaskRunnerList.KeyMap.Filter.SetHelp("tab", "Ara")
				m.TaskRunnerList.KeyMap.ClearFilter.SetHelp("tab/esc", "Vazgeç")
				m.TaskRunnerList.KeyMap.CancelWhileFiltering.SetHelp("esc", "İptal")
				m.TaskRunnerList.KeyMap.AcceptWhileFiltering.SetHelp("enter", "Seç")
				m.TaskRunnerList.KeyMap.ShowFullHelp.SetHelp(",", "Daha Fazla")
				m.TaskRunnerList.KeyMap.CloseFullHelp.SetHelp(",", "Kapat")
				m.TaskRunnerList.KeyMap.Quit.SetHelp("q", "Çıkış")

				// Apply same custom keys as main list
				m.TaskRunnerList.KeyMap.CursorUp.SetKeys("up")
				m.TaskRunnerList.KeyMap.CursorDown.SetKeys("down")
				m.TaskRunnerList.KeyMap.Filter.SetKeys("tab")
				m.TaskRunnerList.KeyMap.ClearFilter.SetKeys("tab", "esc")
				m.TaskRunnerList.KeyMap.CancelWhileFiltering.SetKeys("esc", "tab")
				m.TaskRunnerList.KeyMap.ShowFullHelp.SetKeys(",")
				m.TaskRunnerList.KeyMap.CloseFullHelp.SetKeys(",")

				m.TaskRunnerList.AdditionalShortHelpKeys = func() []key.Binding {
					return []key.Binding{
						key.NewBinding(
							key.WithKeys("q"),
							key.WithHelp(
								lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5555")).Render("q"),
								lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5555")).Render("Çıkış"),
							),
						),
					}
				}

				// Reset List State (Always start clean)
				m.TaskRunnerList.ResetFilter()
				m.TaskRunnerList.Select(0)

				m.State = StateTaskRunner
				return m, nil
			case "e", "E":
				// Quick Open: Explorer - tam yol ile
				path := m.Selected.Path
				go func() {
					// Windows system32'den explorer.exe çağır
					exec.Command("C:\\Windows\\explorer.exe", path).Run()
				}()
				return m, nil
			case "q":
				return m, tea.Quit
			}
		}

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.List.SetWidth(msg.Width)
		m.List.SetHeight(msg.Height - 10)
		m.TaskRunnerList.SetWidth(msg.Width)
		m.TaskRunnerList.SetHeight(msg.Height - 5) // Use more space for task runner
		m.clampProjectActionsScroll()
		m.clampHealthScoreScroll()

	case projectMsg:
		m.Projects = msg
		m.State = StateProjectSelect // Direkt listeye git

		// ========================================================
		// SIRALAMA: Son Açılanlar Üstte, Geri Kalanlar Alfabetik
		// ========================================================
		sort.SliceStable(m.Projects, func(i, j int) bool {
			timeI, hasI := m.Config.LastOpened[strings.ToLower(m.Projects[i].Path)]
			timeJ, hasJ := m.Config.LastOpened[strings.ToLower(m.Projects[j].Path)]

			// Her ikisi de son açılanlar listesinde -> En son açılan üste
			if hasI && hasJ {
				return timeI.After(timeJ)
			}
			// Sadece i son açılanlar listesinde -> i üste
			if hasI && !hasJ {
				return true
			}
			// Sadece j son açılanlar listesinde -> j üste
			if !hasI && hasJ {
				return false
			}
			// Hiçbiri son açılanlar listesinde değil -> Alfabetik
			return strings.ToLower(m.Projects[i].Name) < strings.ToLower(m.Projects[j].Name)
		})

		// Listeyi burada başlat
		items := make([]list.Item, len(m.Projects))
		for i, p := range m.Projects {
			// Technology icon helper
			getTechIcon := func(techType domain.ProjectType) string {
				icons := map[domain.ProjectType]string{
					domain.TypeNext:        "⚡",
					domain.TypeReact:       "⚛️",
					domain.TypeVue:         "💚",
					domain.TypeVite:        "⚡",
					domain.TypeReactNative: "📱",
					domain.TypeMobile:      "📱",
					domain.TypeHTML:        "🌐",
					domain.TypeTypeScript:  "🔷",
					domain.TypeAngular:     "🅰️",
					domain.TypeSvelte:      "🔥",
					domain.TypeSolidJS:     "💎",
					domain.TypeAstro:       "🚀",
					domain.TypeRemix:       "💿",
					domain.TypeNuxt:        "💚",
					domain.TypeNest:        "🐱",
					domain.TypeExpress:     "🚂",
					domain.TypeGo:          "🐹",
					domain.TypeDjango:      "🐍",
					domain.TypeFlask:       "🧪",
					domain.TypeLaravel:     "🐘",
					domain.TypeSpring:      "☕",
					domain.TypePHP:         "🐘",
					domain.TypeFastAPI:     "⚡",
					domain.TypeFiber:       "🔷",
					domain.TypeHono:        "🔥",
					domain.TypeKoa:         "🥝",
					domain.TypeFlutter:     "🦋",
					domain.TypeExpo:        "📱",
					domain.TypeDocker:      "🐳",
				}
				if icon, ok := icons[techType]; ok {
					return icon
				}
				return ""
			}

			// Build combined icon (Frontend + Backend)
			var iconParts []string
			if p.FrontendType != "" && p.FrontendType != domain.TypeUnknown {
				if ic := getTechIcon(p.FrontendType); ic != "" {
					iconParts = append(iconParts, ic)
				}
			}
			if p.BackendType != "" && p.BackendType != domain.TypeUnknown {
				if ic := getTechIcon(p.BackendType); ic != "" {
					iconParts = append(iconParts, ic)
				}
			}
			// Docker indicator
			if p.HasDocker {
				iconParts = append(iconParts, "🐳")
			}

			icon := "📁 "
			if len(iconParts) > 0 {
				icon = strings.Join(iconParts, "") + " "
			}

			// Build technology description (Frontend + Backend names)
			var techParts []string
			if p.FrontendType != "" && p.FrontendType != domain.TypeUnknown {
				techParts = append(techParts, string(p.FrontendType))
			}
			if p.BackendType != "" && p.BackendType != domain.TypeUnknown {
				techParts = append(techParts, string(p.BackendType))
			}

			techDesc := "Bilinmeyen"
			if len(techParts) > 0 {
				techDesc = strings.Join(techParts, " + ")
			}

			// Title: Icon + Name
			items[i] = item{title: icon + p.Name, desc: techDesc + " | " + p.Path, project: &m.Projects[i]}
		}

		// List Configuration
		delegate := list.NewDefaultDelegate()
		delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.BorderLeftForeground(lipgloss.Color("#bd93f9")).Foreground(lipgloss.Color("#bd93f9"))
		delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.BorderLeftForeground(lipgloss.Color("#bd93f9")).Foreground(lipgloss.Color("#6272a4"))

		// Setup Help Styles to match standard
		// delegate.Styles.HelpStyle ... (Usually internal, but we can verify)

		m.List = list.New(items, delegate, m.Width, m.Height)
		m.List.Title = "🚀 PROJELER"
		m.List.SetShowTitle(true)
		m.List.SetStatusBarItemName("Proje", "Proje")
		m.List.FilterInput.Prompt = "🔍 Ara: "
		m.List.DisableQuitKeybindings()

		// Translate KeyMap (Help)
		m.List.AdditionalShortHelpKeys = func() []key.Binding {
			return []key.Binding{
				key.NewBinding(
					key.WithKeys("r"),
					key.WithHelp(
						lipgloss.NewStyle().Foreground(lipgloss.Color("#bd93f9")).Render("r"),
						lipgloss.NewStyle().Foreground(lipgloss.Color("#bd93f9")).Render("Yenile"),
					),
				),
				key.NewBinding(
					key.WithKeys("q"),
					key.WithHelp(
						lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5555")).Render("q"),
						lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5555")).Render("Çıkış"),
					),
				),
			}
		}

		// Custom Key Bindings
		m.List.KeyMap.CursorUp.SetKeys("up")
		m.List.KeyMap.CursorDown.SetKeys("down")
		m.List.KeyMap.Filter.SetKeys("tab")
		m.List.KeyMap.ClearFilter.SetKeys("tab", "esc") // Tab toggle and Esc fallback
		m.List.KeyMap.ShowFullHelp.SetKeys(",")
		m.List.KeyMap.CloseFullHelp.SetKeys(",")

		m.List.KeyMap.CursorUp.SetHelp("↑", "Yukarı")
		m.List.KeyMap.CursorDown.SetHelp("↓", "Aşağı")
		m.List.KeyMap.Filter.SetHelp("tab", "Ara")
		m.List.KeyMap.ClearFilter.SetHelp("tab/esc", "Vazgeç")
		m.List.KeyMap.AcceptWhileFiltering.SetHelp("enter", "Seç")
		m.List.KeyMap.ShowFullHelp.SetHelp(",", "Daha Fazla")
		m.List.KeyMap.CloseFullHelp.SetHelp(",", "Kapat")
		m.List.KeyMap.Quit.SetHelp("q", "Çıkış") // Standart quit

		// Bizim custom q implementasyonumuzu menüde kırmızı göstermek için ekledik.
		// Ancak standart Help de aktif. Onu da yönetelim.

	case contextMsg:
		clipboard.WriteAll(string(msg))
		m.CopiedSuccess = true
		// Reset flag after 4 seconds
		cmd = tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
			return copiedResetMsg{}
		})
		cmds = append(cmds, cmd)
		m.State = StateProjectActions // Stay here

	case copiedResetMsg:
		m.CopiedSuccess = false

	case errMsg:
		m.Err = error(msg)
		m.IsUpdatingDependencies = false

	case packageUpdateCompleteMsg:
		m.IsUpdatingDependencies = false
		return m, m.checkDependenciesCmd()

	case updateCmdFinishedMsg:
		// Append logs
		cmdName := "Command"
		if m.UpdateCurrentCmd < len(m.UpdateCommands) {
			cmdName = fmt.Sprintf("%v", m.UpdateCommands[m.UpdateCurrentCmd].Args)
		}

		logEntry := fmt.Sprintf("▶ %s\n%s", cmdName, msg.output)
		if msg.err != nil {
			logEntry += fmt.Sprintf("\n❌ Error: %v\n", msg.err)
			m.UpdateHasError = true
		}
		m.UpdateLogs = append(m.UpdateLogs, logEntry)

		content := strings.Join(m.UpdateLogs, "\n----------------\n")
		m.UpdateViewport.SetContent(content)
		m.UpdateViewport.GotoBottom()

		m.UpdateCurrentCmd++
		if m.UpdateCurrentCmd < len(m.UpdateCommands) {
			// Run next
			return m, m.runNextUpdateCmd
		} else {
			// All done
			m.UpdateDone = true
			m.IsUpdatingDependencies = false
			// Do NOT force 1.0 here, let ticker finish it
			return m, nil
		}

	case updatePrepMsg:
		m.IsUpdatingDependencies = false // Spinner handled by Feedback logic now

		if msg.err != nil {
			m.Err = msg.err
			return m, nil
		}

		// Init StateUpdateFeedback
		m.UpdateCommands = msg.cmds
		m.UpdatePkgs = msg.pkgs
		m.UpdateVersions = msg.versions
		m.UpdateCurrentCmd = 0
		m.UpdateLogs = []string{}
		m.UpdateDone = false
		m.UpdateHasError = false
		m.UpdateViewport.SetContent("")
		// Progress removed

		m.State = StateUpdateFeedback
		m.Width, m.Height = m.List.Width(), m.List.Height()
		m.UpdateViewport.Width = m.Width - 10
		m.UpdateViewport.Height = 10

		// Start command AND spinner
		return m, tea.Batch(
			spinner.Tick,
			m.runNextUpdateCmd,
		)

	case progressTickMsg:
		if m.State == StateUpdateFeedback {
			// If Logic is Done but Animation not:
			if m.UpdateDone {
				if m.UpdateProgress.Percent() < 1.0 {
					// Fast finish (slowed down significantly for visibility)
					cmds = append(cmds, m.UpdateProgress.SetPercent(m.UpdateProgress.Percent()+0.005))
					cmds = append(cmds, tea.Tick(time.Millisecond*50, func(t time.Time) tea.Msg { return progressTickMsg(t) }))
				}
				return m, tea.Batch(cmds...)
			}

			// Logic still running
			// Simulate progress for current step
			// If we have N commands, each is 1/N.
			// Wihtin a step, we go from 0 to 0.9 (keep 0.1 for completion snap)

			stepSize := 1.0 / float64(len(m.UpdateCommands))
			baseProgress := float64(m.UpdateCurrentCmd) * stepSize

			// Increment loosely based on time or just random small increments?
			// Let's just increment current percent by small amount up to limit
			currentPct := m.UpdateProgress.Percent()
			targetLimit := baseProgress + (stepSize * 0.9)

			if currentPct < targetLimit {
				newPct := currentPct + 0.01 // Slow increment
				if newPct > targetLimit {
					newPct = targetLimit
				}
				cmds = append(cmds, m.UpdateProgress.SetPercent(newPct))
			}

			// Continue ticking
			cmds = append(cmds, tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg { return progressTickMsg(t) }))
		}
	case doctorMsg:
		m.IsUpdatingDependencies = false
		result := msg.result
		currentTarget := m.currentDependencyTarget()
		if msg.target.Path != "" && currentTarget.Path != "" && msg.target.Path != currentTarget.Path {
			return m, tea.Batch(cmds...)
		}
		if len(result) == 0 {
			// No dependencies found at all
			m.Table.SetRows([]table.Row{})
			m.AllPackagesUpToDate = true
		} else {
			// Populate table with ALL packages (sorted)
			keys := make([]string, 0, len(result))
			for k := range result {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			rows := []table.Row{}
			allUpToDate := true
			for _, k := range keys {
				info := result[k]
				latestDisplay := info.Latest
				if info.Current == info.Latest {
					latestDisplay = "latest"
				} else {
					allUpToDate = false
				}
				rows = append(rows, table.Row{k, info.Current, info.Wanted, latestDisplay})
			}
			m.Table.SetRows(rows)
			m.AllPackagesUpToDate = allUpToDate
		}
		m.Err = nil // Clear any previous errors
		// Tablo güncellendi

	case splashTickMsg:
		if m.State == StateSplash {
			m.SplashProgress += 0.00333 // %0.33 arttır (300 frame x 15ms = 4.5 saniye)
			if m.SplashProgress >= 1.0 {
				m.SplashProgress = 1.0
				m.State = StateScanning // Animasyon bitti, taramaya başla
				cmds = append(cmds, m.startScanningCmd())
			} else {
				cmds = append(cmds, tea.Tick(time.Millisecond*15, func(t time.Time) tea.Msg {
					return splashTickMsg(t)
				}))
			}
		}
	}

	// Alt bileşenleri güncelle
	// Alt bileşenleri güncelle
	switch m.State {
	case StateScanning, StateNgrok, StateDependencyDoctor, StateUpdateFeedback:
		// Spinner Update
		var sCmd tea.Cmd
		m.Spinner, sCmd = m.Spinner.Update(msg)
		cmds = append(cmds, sCmd)

		// Progress Bar Update
		// Handled via TickMsg mostly, but SetPercent cmd returns a model update too.
		// No manual calculation here anymore to allow simulation.

	case StateSplash:
		// No component update needed
	case StateTaskRunner:
		// "Tab" manuel kontrolüne gerek yok artık, KeyMap ile çözüldü.

		var cmd tea.Cmd
		m.TaskRunnerList, cmd = m.TaskRunnerList.Update(msg)
		cmds = append(cmds, cmd)
	case StateProjectSelect:
		// "r" ile yenile (Filtreleme modunda değilse)
		if key, ok := msg.(tea.KeyMsg); ok && key.String() == "r" && m.List.FilterState() != list.Filtering {
			m.Scanner.ClearCache()
			m.State = StateScanning
			cmds = append(cmds, m.startScanningCmd())
			return m, tea.Batch(cmds...)
		}

		// "Tab" ile filtreleme modu kapatma (Toggle)
		if key, ok := msg.(tea.KeyMsg); ok && key.String() == "tab" && m.List.FilterState() == list.Filtering {
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		}

		// "q" ile çıkış (eğer filtreleme modunda değilse)
		if m.List.FilterState() == list.Filtering {
			// Filtreleme modundaysa listeye bırak
		} else if key, ok := msg.(tea.KeyMsg); ok && key.String() == "q" {
			return m, tea.Quit
		}

		// List'i sadece proje seçim ekranında güncelle
		if m.State == StateProjectSelect {
			m.List, cmd = m.List.Update(msg)
			cmds = append(cmds, cmd)
		}

		// Liste seçimini yönet
		if m.State == StateProjectSelect {
			if val, ok := msg.(tea.KeyMsg); ok && val.String() == "enter" {
				// Proje seçildi
				i, ok := m.List.SelectedItem().(item)
				if ok {
					m.Selected = i.project
					if m.List.Title == "Bağımlılık Kontrolü İçin Proje Seç" {
						// Doktoru çalıştır
						m.State = StateDependencyDoctor
						m.resetDependencyDoctor()
						// Spinner başlat ve komutu tetikle
						cmds = append(cmds, m.Spinner.Tick, m.checkDependenciesCmd())
					} else {
						m.State = StateProjectActions // Alt menüye git
						m.ActionScroll = 0
					}
				}
			}
		}

	}

	return m, tea.Batch(cmds...)
}

func (m *MainModel) checkDependenciesCmd() tea.Cmd {
	target := m.currentDependencyTarget()
	selected := m.Selected
	return func() tea.Msg {
		if selected == nil {
			return errMsg(fmt.Errorf("proje seçilmedi"))
		}
		if m.Doctor == nil {
			return errMsg(fmt.Errorf("dependency doctor hazır değil"))
		}

		var (
			res service.NpmOutdatedResult
			err error
		)
		if target.Path == "" {
			res, err = m.Doctor.CheckDependencies(selected)
		} else {
			res, err = m.Doctor.CheckTargetDependencies(target)
		}
		if err != nil {
			return errMsg(err)
		}
		return doctorMsg{target: target, result: res}
	}
}

func (m *MainModel) resetDependencyDoctor() {
	m.DependencyTargets = nil
	m.DependencyTargetIndex = 0
	m.ensureDependencyTargets()
	m.Table.SetRows([]table.Row{})
	m.AllPackagesUpToDate = false
	m.Err = nil
}

func (m *MainModel) ensureDependencyTargets() {
	if len(m.DependencyTargets) == 0 && m.Doctor != nil && m.Selected != nil {
		m.DependencyTargets = m.Doctor.DependencyTargets(m.Selected)
	}
	if m.DependencyTargetIndex < 0 || m.DependencyTargetIndex >= len(m.DependencyTargets) {
		m.DependencyTargetIndex = 0
	}
}

func (m *MainModel) currentDependencyTarget() service.DependencyTarget {
	m.ensureDependencyTargets()
	if len(m.DependencyTargets) == 0 {
		return service.DependencyTarget{}
	}
	return m.DependencyTargets[m.DependencyTargetIndex]
}

func (m *MainModel) dependencyDoctorTitle() string {
	if m.Selected == nil {
		return "Doktor Raporu"
	}
	targetLabel := m.dependencyTargetLabel()
	if targetLabel == "" {
		return fmt.Sprintf("%s İçin Doktor Raporu", m.Selected.Name)
	}
	return fmt.Sprintf("%s / %s İçin Doktor Raporu", m.Selected.Name, targetLabel)
}

func (m *MainModel) dependencyTargetLabel() string {
	target := m.currentDependencyTarget()
	if target.Label != "" {
		return target.Label
	}
	return target.Name
}

func (m *MainModel) handleProjectActionsScrollKey(key string) bool {
	switch strings.ToLower(key) {
	case "up", "k":
		m.scrollProjectActions(-1)
	case "down", "j":
		m.scrollProjectActions(1)
	case "pgup":
		m.scrollProjectActions(-m.projectActionsVisibleHeight())
	case "pgdown":
		m.scrollProjectActions(m.projectActionsVisibleHeight())
	case "home":
		m.ActionScroll = 0
	case "end":
		m.ActionScroll = m.projectActionsMaxScroll()
	default:
		return false
	}
	return true
}

func (m *MainModel) scrollProjectActions(delta int) {
	m.ActionScroll += delta
	m.clampProjectActionsScroll()
}

func (m *MainModel) clampProjectActionsScroll() {
	maxScroll := m.projectActionsMaxScroll()
	if m.ActionScroll < 0 {
		m.ActionScroll = 0
	}
	if m.ActionScroll > maxScroll {
		m.ActionScroll = maxScroll
	}
}

func (m *MainModel) projectActionsMaxScroll() int {
	bodyHeight := m.projectActionsVisibleHeight()
	if bodyHeight <= 0 || m.Selected == nil {
		return 0
	}

	lineCount := len(strings.Split(m.actionsBodyView(), "\n"))
	if lineCount <= bodyHeight {
		return 0
	}
	return lineCount - bodyHeight
}

func (m *MainModel) projectActionsVisibleHeight() int {
	if m.Height <= 0 {
		return 0
	}
	height := m.Height - 1
	if height < 1 {
		return 1
	}
	return height
}

func (m *MainModel) handleHealthScoreScrollKey(key string) bool {
	switch strings.ToLower(key) {
	case "up", "k":
		m.scrollHealthScore(-1)
	case "down", "j":
		m.scrollHealthScore(1)
	case "pgup":
		m.scrollHealthScore(-m.healthScoreVisibleHeight())
	case "pgdown":
		m.scrollHealthScore(m.healthScoreVisibleHeight())
	case "home":
		m.HealthScroll = 0
	case "end":
		m.HealthScroll = m.healthScoreMaxScroll()
	default:
		return false
	}
	return true
}

func (m *MainModel) scrollHealthScore(delta int) {
	m.HealthScroll += delta
	m.clampHealthScoreScroll()
}

func (m *MainModel) clampHealthScoreScroll() {
	maxScroll := m.healthScoreMaxScroll()
	if m.HealthScroll < 0 {
		m.HealthScroll = 0
	}
	if m.HealthScroll > maxScroll {
		m.HealthScroll = maxScroll
	}
}

func (m *MainModel) healthScoreMaxScroll() int {
	bodyHeight := m.healthScoreVisibleHeight()
	if bodyHeight <= 0 || m.HealthReport == nil {
		return 0
	}

	lineCount := len(strings.Split(m.healthScoreBodyView(), "\n"))
	if lineCount <= bodyHeight {
		return 0
	}
	return lineCount - bodyHeight
}

func (m *MainModel) healthScoreVisibleHeight() int {
	if m.Height <= 0 {
		return 0
	}
	height := m.Height - 1
	if height < 1 {
		return 1
	}
	return height
}

func dbToolActionForKey(key string, project *domain.Project) dbToolAction {
	switch strings.ToLower(key) {
	case "f1":
		if project != nil && project.HasPrisma {
			return dbToolPrisma
		}
	case "f2":
		if project != nil && project.HasDrizzle {
			return dbToolDrizzle
		}
	case "f3":
		if project != nil && project.HasHasura {
			return dbToolHasura
		}
	case "f4":
		if project != nil && project.HasSupabase {
			return dbToolSupabase
		}
	case "f5":
		if project != nil && project.HasStorybook {
			return dbToolStorybook
		}
	}
	return dbToolNone
}

func dbToolActionForLine(line string, project *domain.Project) dbToolAction {
	if strings.Contains(line, "[F1]") && project != nil && project.HasPrisma {
		return dbToolPrisma
	}
	if strings.Contains(line, "[F2]") && project != nil && project.HasDrizzle {
		return dbToolDrizzle
	}
	if strings.Contains(line, "[F3]") && project != nil && project.HasHasura {
		return dbToolHasura
	}
	if strings.Contains(line, "[F4]") && project != nil && project.HasSupabase {
		return dbToolSupabase
	}
	if strings.Contains(line, "[F5]") && project != nil && project.HasStorybook {
		return dbToolStorybook
	}
	return dbToolNone
}

func (m *MainModel) dbToolActionAtY(y int) dbToolAction {
	lines := strings.Split(m.actionsView(), "\n")
	for _, index := range []int{y, y - 1} {
		if index < 0 || index >= len(lines) {
			continue
		}
		if action := dbToolActionForLine(lines[index], m.Selected); action != dbToolNone {
			return action
		}
	}
	return dbToolNone
}

func (m *MainModel) launchDbToolCmd(action dbToolAction) tea.Cmd {
	return func() tea.Msg {
		if m.Selected == nil {
			return errMsg(fmt.Errorf("proje seçilmedi"))
		}
		var err error
		switch action {
		case dbToolPrisma:
			err = m.Launcher.LaunchPrisma(m.Selected)
		case dbToolDrizzle:
			err = m.Launcher.LaunchDrizzle(m.Selected)
		case dbToolHasura:
			err = m.Launcher.LaunchHasura(m.Selected)
		case dbToolSupabase:
			err = m.Launcher.LaunchSupabase(m.Selected)
		case dbToolStorybook:
			err = m.Launcher.LaunchStorybook(m.Selected)
		default:
			return nil
		}
		if err != nil {
			return errMsg(err)
		}
		return nil
	}
}

// updateLastOpened proje açılma zamanını kaydeder (sync)
func (m *MainModel) updateLastOpened(path string) {
	if m.Config.LastOpened == nil {
		m.Config.LastOpened = make(map[string]time.Time)
	}
	m.Config.LastOpened[strings.ToLower(path)] = time.Now()
	_ = config.SaveConfig(m.Config)
}

func (m *MainModel) View() string {
	if m.Err != nil {
		return "Hata: " + m.Err.Error()
	}

	switch m.State {
	case StateScanning:
		return fmt.Sprintf("\n\n   %s Taranıyor... %d yol bulundu.\n\n", m.Spinner.View(), len(m.Config.ProjectsPaths))
	case StateFirstRun:
		return fmt.Sprintf("\n\n  👋 Hoşgeldiniz! \n\n  Lütfen projelerinizin bulunduğu ana klasör yolunu giriniz:\n  (Tırnak işareti ile yapıştırabilirsiniz)\n\n  %s\n\n  [Enter] Kaydet\n", m.FirstRunInput.View())
	case StateDashboard:
		return m.dashboardView()
	case StateProjectSelect:
		v := m.List.View()
		v = strings.ReplaceAll(v, "No Proje found", "Proje bulunamadı")
		v = strings.ReplaceAll(v, "No Proje", "Proje yok") // Fallback
		return v
	case StateProjectActions:
		return m.actionsView()
	case StatePortCheckWarning:
		// Format warnings
		var warnText string
		for _, w := range m.PortWarnings {
			procInfo := ""
			if w.Process != "" {
				procInfo = fmt.Sprintf(" (%s, PID: %d)", w.Process, w.ProcessID)
			}
			warnText += fmt.Sprintf("• Port %d dolu%s\n", w.Port, procInfo)
		}

		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#ff5555")).
			Padding(1, 2).
			Align(lipgloss.Center).
			Render(
				lipgloss.JoinVertical(lipgloss.Center,
					lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5555")).Bold(true).Render("⚠️  PORT ÇAKIŞMASI TESPİT EDİLDİ"),
					"",
					lipgloss.NewStyle().Foreground(lipgloss.Color("#f8f8f2")).Align(lipgloss.Center).Render(warnText),
					"",
					"Bu portlar şu an kullanımda. Ne yapmak istersiniz?",
					"",
					lipgloss.NewStyle().Foreground(lipgloss.Color("#50fa7b")).Render("[1] 🔄 Kapat ve Başlat (Kill & Start)"),
					lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5555")).Render("[2] 🛑 Sadece Portu Kapat (Kill Only)"),
					"",
					lipgloss.NewStyle().Foreground(lipgloss.Color("#6272a4")).Render("[Esc] Geri Dön"),
				),
			)
		return lipgloss.Place(m.Width, m.Height, lipgloss.Center, lipgloss.Center, box)

	case StateDependencyDoctor:
		title := m.dependencyDoctorTitle()
		// Show error if exists (Higher priority)
		if m.Err != nil {
			// Stop loading if error occurred
			// But wait, m.Err might be from update failure.
			// We should probably clear IsUpdatingDependencies if error occurs.
			// For now, let's assume update handler sets Err and clears IsUpdating.
			// Wait, in my Update logic above, I returned errMsg directly.
			// I need to handle errMsg to clear the flag!

			footer := m.renderFooter("Esc", "Geri Dön")
			errorMsg := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#ff5555")).
				Render(fmt.Sprintf("\n  ⚠️  Hata: %s", m.Err.Error()))
			return fmt.Sprintf("\n  %s\n%s\n\n  %s", title, errorMsg, footer)
		}

		// Update in progress?
		if m.IsUpdatingDependencies {
			targetLabel := m.dependencyTargetLabel()
			if targetLabel == "" && m.Selected != nil {
				targetLabel = m.Selected.Name
			}
			return fmt.Sprintf("\n\n   %s Paketler güncelleniyor...\n\n   %s için işlem yapılıyor.\n   (Bu işlem internet hızınıza göre zaman alabilir)", m.Spinner.View(), targetLabel)
		}

		footerPairs := []string{"Enter", "Seçileni Güncelle", "f", "Tümünü Güncelle", "Esc", "Geri Dön"}
		m.ensureDependencyTargets()
		if len(m.DependencyTargets) > 1 {
			footerPairs = append([]string{"Tab", "Bileşen"}, footerPairs...)
		}
		footer := m.renderFooter(footerPairs...)

		// Show loading or results
		var tableView string
		if len(m.Table.Rows()) == 0 {
			// Empty table - either loading or all up to date
			if m.AllPackagesUpToDate {
				// All packages are up to date
				tableView = "\n  " + lipgloss.NewStyle().
					Foreground(lipgloss.Color("#50fa7b")).
					Render("✅ Tüm paketler güncel!")
			} else {
				// Still loading initial check
				tableView = "\n  " + m.Spinner.View() + " Paketler kontrol ediliyor..."
			}
		} else {
			// Results arrived - show table
			tableView = "\n" + m.Table.View()
		}

		return fmt.Sprintf("\n  %s\n%s\n\n  %s", title, tableView, footer)
	case StateNgrok:
		return m.ngrokView()
	case StateHealthScore:
		return m.healthScoreView()
	case StateTaskRunner:
		return m.taskRunnerView()
	case StateSplash:
		return m.splashView()
	case StateUpdateFeedback:
		return m.viewUpdateFeedback()
	}

	return "Bilinmeyen Durum"
}

// Msg types
type errMsg error
type contextMsg string
type doctorMsg struct {
	target service.DependencyTarget
	result service.NpmOutdatedResult
}
type ngrokInstalledMsg string // changed to string (path)
type ngrokAuthMsg bool

func (m *MainModel) checkNgrokInstallCmd() tea.Cmd {
	return func() tea.Msg {
		// Temporary Bypass to verify UI flow
		c := make(chan string, 1)
		go func() {
			// m.NgrokService.CheckCommonPaths()
			time.Sleep(500 * time.Millisecond) // Simulate work
			c <- ""                            // Return empty (Not found)
		}()

		select {
		case res := <-c:
			return ngrokInstalledMsg(res)
		case <-time.After(1000 * time.Millisecond):
			return ngrokInstalledMsg("")
		}
	}
}

func (m *MainModel) checkNgrokAuthCmd() tea.Cmd {
	return func() tea.Msg {
		return ngrokAuthMsg(m.NgrokService.HasAuthToken())
	}
}

func (m *MainModel) ngrokView() string {
	var content, footer string

	s := "\n  🌐 Ngrok Bağlantı Sihirbazı\n\n"

	switch m.NgrokStep {
	case NgrokMainMenu:
		content = s + "  Ne yapmak istersiniz?\n\n" +
			"  [1] Manuel Yol Gir\n" +
			"  [2] Ngrok İndir\n"
		footer = m.renderFooter("Esc", "İptal")

	case NgrokCheckInstall:
		content = s + "  " + m.Spinner.View() + " Ngrok kontrol ediliyor..."
		// No footer here

	case NgrokModeSelect:
		content = s + "  Ngrok kurulumu nasıl yapılsın?\n\n" +
			"  [1] Otomatik İndir (Önerilen)\n" +
			"  [2] Manuel Yol Göster\n"
		footer = m.renderFooter("Esc", "İptal")

	case NgrokManualPath:
		content = s + "  Lütfen 'ngrok.exe' dosyasının tam yolunu girin:\n"
		if m.NgrokCmd == "downloading" {
			content += lipgloss.NewStyle().Foreground(lipgloss.Color("#50fa7b")).Render("  (Tarayıcı açıldı. İndirdikten sonra kurulum yolunu girin)") + "\n"
		} else {
			content += "  (Örn: C:\\ProgramData\\chocolatey\\bin\\ngrok.exe)\n"
		}
		content += "\n" + fmt.Sprintf("  Yol: %s\n", m.NgrokPathInput.View())

		footer = m.renderFooter("Enter", "Kaydet", "Esc", "Geri")

	case NgrokCheckAuth:
		content = s + "  Authtoken ekranına geçiliyor..."

	case NgrokAuth:
		content = s + "  🔐 Ngrok Authtoken Gerekiyor\n\n" +
			"  1. Tarayıcıda şu adrese gidin:\n" +
			lipgloss.NewStyle().Foreground(lipgloss.Color("#bd93f9")).Render("     https://dashboard.ngrok.com/get-started/your-authtoken") + "\n\n" +
			"  2. 'Your Authtoken' yazısının altındaki " + lipgloss.NewStyle().Bold(true).Render("Copy") + " butonuna basın.\n" +
			"  3. Çıkan kodu kopyalayıp aşağıya yapıştırın:\n" +
			"\n" + fmt.Sprintf("  Token: %s\n", m.NgrokTokenInput.View())

		footer = m.renderFooter("Enter", "Kaydet", "Ctrl+O", "Siteye Git", "Esc", "Geri")

	case NgrokAskPort:
		content = s + "  Bağlantı Portu:\n" +
			fmt.Sprintf("  %s\n", m.NgrokPortInput.View())

		footer = m.renderFooter("Enter", "Başlat", "Ctrl+R", "Yeniden Kur", "Esc", "Geri")

	case NgrokRunning:
		content = s + "  🚀 Ngrok Çalışıyor!\n" +
			"  Yeni sekmede tünel açıldı."

		footer = m.renderFooter("Esc", "Projeye Dön")
	}

	// Calculate vertical space needed to push footer to bottom
	if footer != "" {
		hContent := lipgloss.Height(content)
		hFooter := lipgloss.Height(footer)
		gap := m.Height - hContent - hFooter - 1 // -1 for safety margin
		if gap > 0 {
			content += strings.Repeat("\n", gap)
		} else {
			content += "\n\n" // Fallback minimum spacing
		}
		content += "  " + footer // Add some left padding
	}

	return content
}

// -- Helper Types & Cmds --

type projectMsg []domain.Project

func (m *MainModel) scanProjectsCmd() tea.Cmd {
	return func() tea.Msg {
		projs := m.Scanner.ScanProjects()
		return projectMsg(projs)
	}
}

func (m *MainModel) startScanningCmd() tea.Cmd {
	return tea.Batch(m.Spinner.Tick, m.scanProjectsCmd())
}

// List Item Adapter
type item struct {
	title, desc string
	project     *domain.Project
}

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.project.Name }

// Script Item Adapter
type scriptItem struct {
	name, cmd string
}

func (i scriptItem) Title() string       { return i.name }
func (i scriptItem) Description() string { return i.cmd }
func (i scriptItem) FilterValue() string { return i.name }

func (m *MainModel) healthScoreView() string {
	if m.HealthReport == nil {
		return "Sağlık raporu oluşturulamadı."
	}

	body := m.healthScoreBodyView()
	footer := m.renderFooter("↑/↓", "Kaydır", "Esc", "Geri Dön")
	return m.renderHealthScoreViewport(body, footer)
}

func (m *MainModel) healthScoreBodyView() string {
	scoreColor := "#ff5555" // Red
	if m.HealthReport.Score >= 80 {
		scoreColor = "#50fa7b" // Green
	} else if m.HealthReport.Score >= 50 {
		scoreColor = "#f1fa8c" // Yellow
	}

	scoreTitle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(scoreColor)).
		Bold(true).
		Render(fmt.Sprintf("%d/%d", m.HealthReport.Score, healthMaxScore(m.HealthReport.MaxScore)))

	header := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(1, 2).
		MarginBottom(1).
		Render(fmt.Sprintf("Proje Sağlık Skoru: %s", scoreTitle))

	var rows []string
	generalIssues := m.generalHealthIssues()

	rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color("#bd93f9")).Bold(true).Render("Genel Kontroller"))
	if len(generalIssues) == 0 && len(m.HealthReport.PassedItems) == 0 {
		rows = append(rows, "  Kayıtlı genel kontrol yok.")
	}
	if len(generalIssues) > 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5555")).Bold(true).Render("Eksikler:"))
		for _, issue := range generalIssues {
			rows = append(rows, healthIssueLine(issue))
		}
	}
	if len(m.HealthReport.PassedItems) > 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color("#50fa7b")).Bold(true).Render("Tamamlananlar:"))
		for _, item := range m.HealthReport.PassedItems {
			rows = append(rows, healthPassedLine(item))
		}
	}

	if len(m.HealthReport.ComponentReports) > 0 {
		rows = append(rows, "", lipgloss.NewStyle().Foreground(lipgloss.Color("#8be9fd")).Bold(true).Render("Bileşen Kontrolleri"))
		for _, component := range m.HealthReport.ComponentReports {
			rows = append(rows, healthComponentTitle(component))
			if len(component.Issues) == 0 && len(component.PassedItems) == 0 {
				rows = append(rows, "  Kayıtlı bileşen kontrolü yok.")
				continue
			}
			for _, item := range component.PassedItems {
				rows = append(rows, healthPassedLine(item))
			}
			for _, issue := range component.Issues {
				rows = append(rows, healthIssueLine(issue))
			}
		}
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		header,
		m.healthScoreDistributionTable(),
		"",
		strings.Join(rows, "\n"),
	)

	return lipgloss.NewStyle().PaddingLeft(2).Render("\n" + content + "\n")
}

func (m *MainModel) renderHealthScoreViewport(content, footer string) string {
	if m.Height <= 0 {
		if footer == "" {
			return content
		}
		return content + "\n\n  " + footer
	}

	m.clampHealthScoreScroll()

	bodyHeight := m.healthScoreVisibleHeight()
	lines := strings.Split(content, "\n")
	start := m.HealthScroll
	if start > len(lines) {
		start = len(lines)
	}
	end := start + bodyHeight
	if end > len(lines) {
		end = len(lines)
	}

	visible := strings.Join(lines[start:end], "\n")
	if h := lipgloss.Height(visible); h < bodyHeight {
		visible += strings.Repeat("\n", bodyHeight-h)
	}

	if footer == "" {
		return visible
	}
	return visible + "\n  " + footer
}

func (m *MainModel) generalHealthIssues() []service.HealthIssue {
	if m.HealthReport == nil {
		return nil
	}
	if len(m.HealthReport.ComponentReports) == 0 {
		return m.HealthReport.Issues
	}

	prefixes := make([]string, 0, len(m.HealthReport.ComponentReports))
	for _, component := range m.HealthReport.ComponentReports {
		if component.Name != "" {
			prefixes = append(prefixes, component.Name+": ")
		}
	}

	var issues []service.HealthIssue
	for _, issue := range m.HealthReport.Issues {
		if hasAnyPrefix(issue.Description, prefixes) {
			continue
		}
		issues = append(issues, issue)
	}
	return issues
}

type healthScoreDistributionRow struct {
	Name  string
	Score int
	Max   int
}

func (m *MainModel) healthScoreDistributionTable() string {
	if m.HealthReport == nil {
		return ""
	}

	rows := []healthScoreDistributionRow{
		{Name: "Proje", Score: m.HealthReport.Score, Max: healthMaxScore(m.HealthReport.MaxScore)},
	}

	generalPassed := sumHealthPassedPoints(m.HealthReport.PassedItems)
	generalLost := sumHealthIssuePoints(m.generalHealthIssues())
	if generalPassed+generalLost > 0 {
		rows = append(rows, healthScoreDistributionRow{
			Name:  "Genel",
			Score: generalPassed,
			Max:   generalPassed + generalLost,
		})
	}

	for _, component := range m.HealthReport.ComponentReports {
		name := component.Name
		if name == "" {
			name = "Bileşen"
		}
		name = fmt.Sprintf("%s (%s)", name, healthRoleLabel(component.Role))
		rows = append(rows, healthScoreDistributionRow{
			Name:  name,
			Score: component.Score,
			Max:   healthMaxScore(component.MaxScore),
		})
	}

	const (
		innerW = 48
		nameW  = 24
		scoreW = 10
		lostW  = 11
	)

	border := lipgloss.NewStyle().Foreground(ColorGrey)
	header := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)

	top := border.Render("┌" + strings.Repeat("─", innerW) + "┐")
	sep := border.Render("├" + strings.Repeat("─", innerW) + "┤")
	bottom := border.Render("└" + strings.Repeat("─", innerW) + "┘")
	title := healthTableCell(header.Render("Puan Dağılımı"), innerW-2)

	tableRows := []string{
		top,
		healthTableRow(border, " "+title+" ", innerW),
		sep,
		healthTableRow(border, " "+
			healthTableCell(header.Render("Alan"), nameW)+
			healthTableCell(header.Render("Skor"), scoreW)+
			healthTableCell(header.Render("Kayıp"), lostW), innerW),
		sep,
	}

	for _, row := range rows {
		maxScore := healthMaxScore(row.Max)
		scoreText := fmt.Sprintf("%d/%d", row.Score, maxScore)
		lost := maxScore - row.Score
		if lost < 0 {
			lost = 0
		}
		lostText := fmt.Sprintf("%dp", lost)
		if lost == 0 {
			lostText = "Yok"
		}
		tableRows = append(tableRows,
			healthTableRow(border, " "+
				healthTableCell(fitCell(row.Name, nameW), nameW)+
				healthTableCell(healthScoreStyle(row.Score, maxScore).Render(scoreText), scoreW)+
				healthTableCell(healthLostStyle(lost).Render(lostText), lostW), innerW))
	}

	tableRows = append(tableRows, bottom)
	return lipgloss.JoinVertical(lipgloss.Left, tableRows...)
}

func healthTableRow(border lipgloss.Style, content string, width int) string {
	return border.Render("│") + healthTableCell(content, width) + border.Render("│")
}

func healthTableCell(content string, width int) string {
	contentWidth := lipgloss.Width(content)
	if contentWidth >= width {
		return content
	}
	return content + strings.Repeat(" ", width-contentWidth)
}

func healthScoreStyle(score, maxScore int) lipgloss.Style {
	ratio := 0
	if maxScore > 0 {
		ratio = score * 100 / maxScore
	}
	if ratio >= 80 {
		return lipgloss.NewStyle().Foreground(ColorGreen)
	}
	if ratio >= 50 {
		return lipgloss.NewStyle().Foreground(ColorYellow)
	}
	return lipgloss.NewStyle().Foreground(ColorRed)
}

func healthLostStyle(lost int) lipgloss.Style {
	if lost == 0 {
		return lipgloss.NewStyle().Foreground(ColorGreen)
	}
	return lipgloss.NewStyle().Foreground(ColorRed)
}

func sumHealthIssuePoints(issues []service.HealthIssue) int {
	total := 0
	for _, issue := range issues {
		total += issue.Points
	}
	return total
}

func sumHealthPassedPoints(items []string) int {
	total := 0
	for _, item := range items {
		total += healthPassedItemPoints(item)
	}
	return total
}

func healthPassedItemPoints(item string) int {
	start := strings.LastIndex(item, "(")
	end := strings.LastIndex(item, "p)")
	if start == -1 || end == -1 || end <= start+1 {
		return 0
	}
	points, err := strconv.Atoi(strings.TrimSpace(item[start+1 : end]))
	if err != nil {
		return 0
	}
	return points
}

func hasAnyPrefix(text string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

func healthComponentTitle(component service.ComponentHealthReport) string {
	role := healthRoleLabel(component.Role)
	name := component.Name
	if name == "" {
		name = "Bileşen"
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#f8f8f2")).
		Bold(true).
		Render(fmt.Sprintf("%s (%s) - %d/%d", name, role, component.Score, healthMaxScore(component.MaxScore)))
}

func healthMaxScore(maxScore int) int {
	if maxScore <= 0 {
		return 100
	}
	return maxScore
}

func healthRoleLabel(role domain.ComponentRole) string {
	switch role {
	case domain.ComponentRoleFrontend:
		return "Frontend"
	case domain.ComponentRoleBackend:
		return "Backend"
	case domain.ComponentRoleTool:
		return "Araç"
	case domain.ComponentRoleLibrary:
		return "Kütüphane"
	default:
		return "Bileşen"
	}
}

func healthIssueLine(issue service.HealthIssue) string {
	icon := lipgloss.NewStyle().Foreground(ColorRed).Bold(true).Render("×")
	text := lipgloss.NewStyle().Foreground(ColorRed).Render(fmt.Sprintf("%s (-%d puan)", issue.Description, issue.Points))
	return fmt.Sprintf("  %s %s", icon, text)
}

func healthPassedLine(item string) string {
	icon := lipgloss.NewStyle().Foreground(ColorGreen).Bold(true).Render("✓")
	text := lipgloss.NewStyle().Foreground(ColorGreen).Render(item)
	return fmt.Sprintf("  %s %s", icon, text)
}

func (m *MainModel) viewUpdateFeedback() string {
	// 1. Result Screen (Auto-forwarded)
	if m.UpdateDone || m.UpdateHasError {
		borderColor := lipgloss.Color("#50fa7b") // Green
		title := "✅ GÜNCELLEME TAMAMLANDI"
		desc := "Tüm paketler başarıyla güncellendi."

		if m.UpdateHasError || m.Err != nil {
			borderColor = lipgloss.Color("#ff5555") // Red
			title = "❌ GÜNCELLEME TAMAMLANAMADI"

			// Show specific error if available
			desc = "Bazı paketler güncellenirken hata oluştu."
			if m.Err != nil {
				desc = fmt.Sprintf("Hata: %v\n\nLütfen aşağıdaki çıktıları kontrol edin.", m.Err)
			} else {
				desc = "Bilinmeyen bir hata oluştu. Lütfen günlükleri kontrol edin."
			}
		}

		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Padding(1, 2).
			Align(lipgloss.Center).
			Render(
				lipgloss.JoinVertical(lipgloss.Center,
					lipgloss.NewStyle().Foreground(borderColor).Bold(true).Render(title),
					"",
					lipgloss.NewStyle().Foreground(lipgloss.Color("#f8f8f2")).Align(lipgloss.Center).Render(desc),
					"",
					lipgloss.NewStyle().Foreground(lipgloss.Color("#6272a4")).Render("[ESC] / [Enter] ile Geri Dön"),
				),
			)
		return lipgloss.Place(m.Width, m.Height, lipgloss.Center, lipgloss.Center, box)
	}

	// 2. Processing Screen (Spinner List)
	var s string
	s += "\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true).Render("📦 Paketler Güncelleniyor...") + "\n\n"

	if len(m.UpdatePkgs) > 0 {
		// Define styles
		pkgStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Bold(true).Width(30)
		spinnerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("208")) // Orange
		bracketStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // Grey

		for _, pkg := range m.UpdatePkgs {
			spinnerView := m.Spinner.View()

			// Use JoinHorizontal to guarantee single line
			row := lipgloss.JoinHorizontal(lipgloss.Left,
				"   ",
				pkgStyle.Render(pkg),
				bracketStyle.Render("[ "),
				spinnerStyle.Render(spinnerView),
				bracketStyle.Render(" ]"),
			)
			s += row + "\n\n"
		}
	} else {
		// Fallback
		s += fmt.Sprintf("   %s [ %s ]\n", "Tüm paketler...", m.Spinner.View())
	}

	s += "\n\n"

	return s
}

func (m *MainModel) runNextUpdateCmd() tea.Msg {
	if m.UpdateCurrentCmd >= len(m.UpdateCommands) {
		return nil
	}
	cmd := m.UpdateCommands[m.UpdateCurrentCmd]
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Enhance error debugging
		err = fmt.Errorf("Cmd: %s\nDir: %s\nErr: %v", cmd.String(), cmd.Dir, err)
	}
	return updateCmdFinishedMsg{output: string(output), err: err}
}

func (m *MainModel) prepareUpdateCmd(pkgs []string, versions map[string]UpdateVersionInfo) tea.Cmd {
	target := m.currentDependencyTarget()
	selected := m.Selected
	return func() tea.Msg {
		var (
			cmds      []*exec.Cmd
			finalPkgs []string
			err       error
		)
		if m.Doctor == nil {
			err = fmt.Errorf("dependency doctor hazır değil")
		} else if target.Path == "" {
			if selected == nil {
				err = fmt.Errorf("proje seçilmedi")
			} else {
				cmds, finalPkgs, err = m.Doctor.GetUpdateCommands(selected, pkgs)
			}
		} else {
			cmds, finalPkgs, err = m.Doctor.GetUpdateCommandsForTarget(target, pkgs)
		}
		return updatePrepMsg{
			cmds:     cmds,
			pkgs:     finalPkgs,
			versions: versions,
			err:      err,
		}
	}
}
