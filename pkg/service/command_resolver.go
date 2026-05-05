package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"devterminal/pkg/domain"
)

type CommandSource string

const (
	CommandSourceNone            CommandSource = ""
	CommandSourceRootScript      CommandSource = "root-script"
	CommandSourceComponentScript CommandSource = "component-script"
	CommandSourceFallback        CommandSource = "fallback"
)

type CommandResolveRequest struct {
	RootPath      string
	ComponentName string
	ComponentPath string
	Role          domain.ComponentRole
}

type CommandPlan struct {
	Command    string
	Cwd        string
	ScriptName string
	Source     CommandSource
	Role       domain.ComponentRole
	Confidence int
	Reason     string
}

type CommandResolver struct{}

func NewCommandResolver() *CommandResolver {
	return &CommandResolver{}
}

func (r *CommandResolver) ResolveStartCommand(req CommandResolveRequest) CommandPlan {
	if req.ComponentPath == "" {
		req.ComponentPath = req.RootPath
	}
	if req.ComponentName == "" {
		req.ComponentName = filepath.Base(req.ComponentPath)
	}

	rootPlan := r.resolvePackageScript(req.RootPath, req.RootPath, req.ComponentName, req.Role, CommandSourceRootScript)
	componentPlan := r.resolvePackageScript(req.RootPath, req.ComponentPath, req.ComponentName, req.Role, CommandSourceComponentScript)

	if rootPlan.Command != "" && rootPlan.Confidence >= componentPlan.Confidence {
		return rootPlan
	}
	if componentPlan.Command != "" {
		return componentPlan
	}

	return CommandPlan{
		Command:    "",
		Cwd:        req.ComponentPath,
		Source:     CommandSourceNone,
		Role:       req.Role,
		Confidence: 0,
		Reason:     "package.json script bulunamadı",
	}
}

func (r *CommandResolver) resolvePackageScript(rootPath, scriptPath, componentName string, role domain.ComponentRole, source CommandSource) CommandPlan {
	pkg, ok := readPackageJSON(scriptPath)
	if !ok || len(pkg.Scripts) == 0 {
		return CommandPlan{}
	}

	bestScript := ""
	bestScore := -9999
	bestReason := ""
	for scriptName, scriptCmd := range pkg.Scripts {
		score, reason := scoreCommandScript(scriptName, scriptCmd, scriptPath, componentName, role, source)
		if score > bestScore {
			bestScore = score
			bestScript = scriptName
			bestReason = reason
		}
	}

	if bestScript == "" || bestScore <= 0 {
		return CommandPlan{}
	}

	manager := commandPackageManager(rootPath, scriptPath)
	return CommandPlan{
		Command:    packageManagerRunScriptCommandWithManager(manager, bestScript),
		Cwd:        scriptPath,
		ScriptName: bestScript,
		Source:     source,
		Role:       role,
		Confidence: commandConfidence(bestScore),
		Reason:     bestReason,
	}
}

func readPackageJSON(path string) (packageJSON, bool) {
	data, err := os.ReadFile(filepath.Join(path, "package.json"))
	if err != nil {
		return packageJSON{}, false
	}

	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return packageJSON{}, false
	}
	return pkg, true
}

func scoreCommandScript(scriptName, scriptCmd, scriptPath, componentName string, role domain.ComponentRole, source CommandSource) (int, string) {
	name := strings.ToLower(scriptName)
	cmd := strings.ToLower(scriptCmd)
	component := strings.ToLower(componentName)
	roleNames := commandRoleNames(component, role)
	score := 0
	var reasons []string

	if isNonStartScriptName(name) {
		return -500, "başlatma dışı script elendi"
	}

	if source == CommandSourceComponentScript {
		score += 30
		reasons = append(reasons, "component script")
	}

	if name == "dev" || name == "start" || name == "start:dev" || name == "serve" {
		score += 60
		reasons = append(reasons, fmt.Sprintf("%s script", name))
	}
	if strings.Contains(name, "dev") || strings.Contains(name, "start") || strings.Contains(name, "serve") {
		score += 35
		reasons = append(reasons, "başlatma adı")
	}

	for _, roleName := range roleNames {
		if name == "dev:"+roleName || name == roleName+":dev" || name == "start:"+roleName || name == roleName+":start" {
			score += 260
			reasons = append(reasons, roleName+" exact script")
		}
		if strings.Contains(name, roleName) && (strings.Contains(name, "dev") || strings.Contains(name, "start")) {
			score += 140
			reasons = append(reasons, roleName+" role script")
		}
		if commandMentionsComponentPath(cmd, roleName) {
			score += 170
			reasons = append(reasons, roleName+" path")
		}
	}

	if matchesRoleCommandContent(cmd, role) {
		score += 70
		reasons = append(reasons, "role content")
	}
	if mismatchesRoleCommandContent(cmd, role) {
		score -= 220
		reasons = append(reasons, "role mismatch")
	}

	if source == CommandSourceRootScript && name == "dev" && !commandMentionsComponentPath(cmd, component) {
		score -= 90
		reasons = append(reasons, "root dev component belirtmiyor")
	}

	if len(reasons) == 0 {
		reasons = append(reasons, "skor bazlı seçim")
	}
	return score, strings.Join(reasons, ", ")
}

func commandRoleNames(componentName string, role domain.ComponentRole) []string {
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		name = strings.ToLower(strings.TrimSpace(name))
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}

	add(componentName)
	switch role {
	case domain.ComponentRoleFrontend:
		add("web")
		add("client")
		add("frontend")
		add("ui")
	case domain.ComponentRoleBackend:
		add("api")
		add("server")
		add("backend")
		add("service")
	}
	return names
}

func isNonStartScriptName(name string) bool {
	return strings.Contains(name, "test") ||
		strings.Contains(name, "lint") ||
		strings.Contains(name, "build") ||
		strings.Contains(name, "typecheck") ||
		strings.Contains(name, "type-check") ||
		strings.Contains(name, "verify") ||
		strings.Contains(name, "e2e")
}

func commandMentionsComponentPath(cmd, name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return false
	}
	return strings.Contains(cmd, "apps/"+name) ||
		strings.Contains(cmd, "apps\\"+name) ||
		strings.Contains(cmd, "packages/"+name) ||
		strings.Contains(cmd, "packages\\"+name) ||
		strings.Contains(cmd, "services/"+name) ||
		strings.Contains(cmd, "services\\"+name)
}

func matchesRoleCommandContent(cmd string, role domain.ComponentRole) bool {
	switch role {
	case domain.ComponentRoleFrontend:
		return strings.Contains(cmd, "next dev") ||
			strings.Contains(cmd, "vite") ||
			strings.Contains(cmd, "nuxt dev") ||
			strings.Contains(cmd, "react-scripts start")
	case domain.ComponentRoleBackend:
		return strings.Contains(cmd, "tsx") ||
			strings.Contains(cmd, "ts-node") ||
			strings.Contains(cmd, "nest start") ||
			strings.Contains(cmd, "nodemon") ||
			strings.Contains(cmd, "go run") ||
			strings.Contains(cmd, "uvicorn")
	default:
		return false
	}
}

func mismatchesRoleCommandContent(cmd string, role domain.ComponentRole) bool {
	switch role {
	case domain.ComponentRoleFrontend:
		return strings.Contains(cmd, "nest start") ||
			strings.Contains(cmd, "apps/api") ||
			strings.Contains(cmd, "apps\\api") ||
			strings.Contains(cmd, "src/main.ts")
	case domain.ComponentRoleBackend:
		return strings.Contains(cmd, "next dev") ||
			strings.Contains(cmd, "vite") ||
			strings.Contains(cmd, "apps/web") ||
			strings.Contains(cmd, "apps\\web")
	default:
		return false
	}
}

func commandConfidence(score int) int {
	switch {
	case score >= 350:
		return 98
	case score >= 250:
		return 94
	case score >= 160:
		return 86
	case score >= 90:
		return 72
	default:
		return 55
	}
}

func commandPackageManager(rootPath, scriptPath string) string {
	rootManager := detectPackageManagerName(rootPath)
	scriptManager := detectPackageManagerName(scriptPath)
	if scriptManager == packageManagerNpm && rootManager != "" && rootManager != packageManagerNpm {
		return rootManager
	}
	if scriptManager != "" {
		return scriptManager
	}
	return rootManager
}

func packageManagerRunScriptCommandWithManager(manager, scriptName string) string {
	command := packageManagerCommand(manager)
	if manager == packageManagerNpm && scriptName == "start" {
		return command + " start"
	}
	return command + " run " + scriptName
}
