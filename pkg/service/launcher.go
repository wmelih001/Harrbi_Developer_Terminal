package service

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"text/template"
	"time"

	"devterminal/pkg/config"
	"devterminal/pkg/domain"
)

type Launcher struct {
	Config *domain.Config
}

func NewLauncher(cfg *domain.Config) *Launcher {
	return &Launcher{Config: cfg}
}

// LaunchProject opens the project in Windows Terminal using the configured template
func (l *Launcher) LaunchProject(p *domain.Project, mode string) error {
	// Update LastOpened time
	if l.Config.LastOpened == nil {
		l.Config.LastOpened = make(map[string]time.Time)
	}
	l.Config.LastOpened[strings.ToLower(p.Path)] = time.Now()
	// Save config silently
	_ = config.SaveConfig(l.Config)

	var cmdTmpl string
	switch mode {
	case "frontend":
		if p.FrontendCmd == "" {
			return fmt.Errorf("frontend başlatma komutu bulunamadı")
		}
		cmdTmpl = l.Config.Commands.LaunchFrontend
	case "backend":
		if p.BackendCmd == "" {
			return fmt.Errorf("backend başlatma komutu bulunamadı")
		}
		cmdTmpl = l.Config.Commands.LaunchBackend
	case "full":
		// Eğer sadece frontend veya sadece backend varsa, o modu çalıştır
		if p.HasFrontend && !p.HasBackend {
			return l.LaunchProject(p, "frontend")
		}
		if !p.HasFrontend && p.HasBackend {
			return l.LaunchProject(p, "backend")
		}
		// İkisi de varsa Full şablonu kullan
		cmdTmpl = l.Config.Commands.LaunchFull
	default:
		return fmt.Errorf("unknown mode: %s", mode)
	}

	// Parse template
	tmpl, err := template.New("cmd").Parse(cmdTmpl)
	if err != nil {
		return err
	}

	var cmdStr bytes.Buffer
	if err := tmpl.Execute(&cmdStr, p); err != nil {
		return err
	}

	// Parse the command string into executable and args
	// We need to support quoted arguments.
	expandedCmd := cmdStr.String()
	args := parseArgs(expandedCmd)

	if len(args) == 0 {
		return fmt.Errorf("empty command")
	}

	// Execute directly, bypassing cmd /C
	// args[0] is executable (e.g. wt.exe), args[1:] are arguments
	c := exec.Command(args[0], args[1:]...)
	return c.Start()
}

// parseArgs splits a string into arguments, respecting quotes
func parseArgs(cmd string) []string {
	var args []string
	var current []rune
	inQuote := false
	quoteChar := rune(0)

	for _, r := range cmd {
		if inQuote {
			if r == quoteChar {
				inQuote = false
			} else {
				current = append(current, r)
			}
		} else {
			switch r {
			case '"', '\'':
				inQuote = true
				quoteChar = r
			case ' ', '\t':
				if len(current) > 0 {
					args = append(args, string(current))
					current = nil
				}
			default:
				current = append(current, r)
			}
		}
	}
	if len(current) > 0 {
		args = append(args, string(current))
	}
	return args
}

// LaunchPrisma opens Prisma Studio for the project
func (l *Launcher) LaunchPrisma(p *domain.Project) error {
	return l.runCmd(l.prismaCommand(p))
}

// LaunchDrizzle opens Drizzle Studio
func (l *Launcher) LaunchDrizzle(p *domain.Project) error {
	return l.runCmd(l.drizzleCommand(p))
}

// LaunchHasura opens Hasura Console
func (l *Launcher) LaunchHasura(p *domain.Project) error {
	return l.runCmd(l.hasuraCommand(p))
}

// LaunchSupabase opens Supabase Dashboard
func (l *Launcher) LaunchSupabase(p *domain.Project) error {
	return l.runCmd(l.supabaseCommand(p))
}

// LaunchStorybook opens Storybook
func (l *Launcher) LaunchStorybook(p *domain.Project) error {
	return l.runCmd(l.storybookCommand(p))
}

func (l *Launcher) prismaCommand(p *domain.Project) string {
	path := p.Path
	if p.PrismaPath != "" {
		path = p.PrismaPath
	}
	return toolTerminalCommand("Prisma Studio", path, packageManagerExecCommand(path, "prisma", "studio"))
}

func (l *Launcher) drizzleCommand(p *domain.Project) string {
	path := p.Path
	if p.DrizzlePath != "" {
		path = p.DrizzlePath
	}
	return toolTerminalCommand("Drizzle Studio", path, packageManagerExecCommand(path, "drizzle-kit", "studio"))
}

func (l *Launcher) hasuraCommand(p *domain.Project) string {
	path := p.Path
	if p.HasuraPath != "" {
		path = p.HasuraPath
	}
	return toolTerminalCommand("Hasura Console", path, "hasura console")
}

func (l *Launcher) supabaseCommand(p *domain.Project) string {
	path := p.Path
	if p.SupabasePath != "" {
		path = p.SupabasePath
	}
	return toolTerminalCommand("Supabase Status", path, packageManagerExecCommand(path, "supabase", "status"))
}

func (l *Launcher) storybookCommand(p *domain.Project) string {
	path := p.Path
	if p.StorybookPath != "" {
		path = p.StorybookPath
	}
	return toolTerminalCommand("Storybook", path, packageManagerRunScriptCommand(path, "storybook"))
}

func toolTerminalCommand(title, path, runCmd string) string {
	return fmt.Sprintf(`wt.exe -w 0 nt --title "%s" -d "%s" cmd /k "%s"`, title, path, runCmd)
}

func packageManagerExecCommand(path, binary string, args ...string) string {
	manager := detectPackageManagerName(path)
	command := packageManagerCommand(manager)
	parts := append([]string{binary}, args...)
	switch manager {
	case packageManagerPnpm, packageManagerYarn:
		return command + " exec " + strings.Join(parts, " ")
	case packageManagerBun:
		return command + " x " + strings.Join(parts, " ")
	default:
		return "npx " + strings.Join(parts, " ")
	}
}

// LaunchScript opens a new terminal tab to run the selected package.json script
// LaunchScript opens a new terminal tab to run the selected package.json script
func (l *Launcher) LaunchScript(p domain.Project, scriptName, scriptCmd string) error {
	workingDir := p.Path
	actualScriptName := scriptName

	// Determine correct working directory and clean script name
	if strings.HasPrefix(scriptName, "client:") {
		workingDir = p.FrontendPath
		actualScriptName = strings.TrimPrefix(scriptName, "client:")
	} else if strings.HasPrefix(scriptName, "server:") {
		workingDir = p.BackendPath
		actualScriptName = strings.TrimPrefix(scriptName, "server:")
	}

	pm := detectPackageManagerName(workingDir)
	runCmd := packageManagerRunScriptCommand(workingDir, actualScriptName)

	title := fmt.Sprintf("%s %s", pm, actualScriptName)

	// Use workingDir instead of p.Path
	cmdStr := fmt.Sprintf(`wt -w 0 nt --title "%s" -d "%s" cmd /k "%s"`, title, workingDir, runCmd)
	return l.runCmd(cmdStr)
}

// runCmd helper
func (l *Launcher) runCmd(cmdStr string) error {
	args := parseArgs(cmdStr)
	if len(args) == 0 {
		return fmt.Errorf("failed to create command")
	}
	c := exec.Command(args[0], args[1:]...)
	return c.Start()
}
