package service

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	packageManagerNpm  = "npm"
	packageManagerPnpm = "pnpm"
	packageManagerYarn = "yarn"
	packageManagerBun  = "bun"
)

var lookPath = exec.LookPath

func detectPackageManagerName(path string) string {
	if pm := packageManagerFromPackageJSON(path); pm != "" {
		return pm
	}
	if fileExists(filepath.Join(path, "bun.lockb")) || fileExists(filepath.Join(path, "bun.lock")) {
		return packageManagerBun
	}
	if fileExists(filepath.Join(path, "pnpm-lock.yaml")) || fileExists(filepath.Join(path, "pnpm-workspace.yaml")) {
		return packageManagerPnpm
	}
	if fileExists(filepath.Join(path, "yarn.lock")) {
		return packageManagerYarn
	}
	if fileExists(filepath.Join(path, "package-lock.json")) || fileExists(filepath.Join(path, "npm-shrinkwrap.json")) {
		return packageManagerNpm
	}
	return packageManagerNpm
}

func packageManagerFromPackageJSON(path string) string {
	data, err := os.ReadFile(filepath.Join(path, "package.json"))
	if err != nil {
		return ""
	}

	var pkg struct {
		PackageManager string `json:"packageManager"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return ""
	}

	value := strings.ToLower(strings.TrimSpace(pkg.PackageManager))
	switch {
	case strings.HasPrefix(value, packageManagerPnpm+"@"):
		return packageManagerPnpm
	case strings.HasPrefix(value, packageManagerYarn+"@"):
		return packageManagerYarn
	case strings.HasPrefix(value, packageManagerBun+"@"):
		return packageManagerBun
	case strings.HasPrefix(value, packageManagerNpm+"@"):
		return packageManagerNpm
	default:
		return ""
	}
}

func packageManagerCommand(manager string) string {
	manager = normalizePackageManagerName(manager)
	if manager == "" {
		manager = packageManagerNpm
	}
	if _, err := lookPath(manager); err == nil {
		return manager
	}
	if supportsCorepack(manager) {
		if _, err := lookPath("corepack"); err == nil {
			return "corepack " + manager
		}
	}
	return manager
}

func packageManagerRunScriptCommand(path, scriptName string) string {
	manager := detectPackageManagerName(path)
	command := packageManagerCommand(manager)
	if manager == packageManagerNpm && scriptName == "start" {
		return command + " start"
	}
	return command + " run " + scriptName
}

func normalizePackageManagerCommand(cmd string) string {
	leading, firstToken, rest := splitFirstCommandToken(cmd)
	if firstToken == "" {
		return cmd
	}

	first := normalizePackageManagerName(firstToken)
	if first == "" || first == packageManagerNpm {
		return cmd
	}

	replacement := packageManagerCommand(first)
	if replacement == firstToken {
		return cmd
	}

	return leading + replacement + rest
}

func packageManagerScriptName(cmd string) string {
	args := parseArgs(cmd)
	if len(args) == 0 {
		return ""
	}

	offset := 0
	if isCorepackCommand(args[0]) {
		offset = 1
	}
	if len(args) <= offset {
		return ""
	}

	manager := normalizePackageManagerName(args[offset])
	if manager == "" {
		return ""
	}
	if manager == packageManagerNpm && len(args) > offset+1 && args[offset+1] == "start" {
		return "start"
	}
	if len(args) > offset+2 && args[offset+1] == "run" {
		return args[offset+2]
	}
	return ""
}

func isCorepackCommand(name string) bool {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(name)))
	base = strings.TrimSuffix(base, ".cmd")
	base = strings.TrimSuffix(base, ".exe")
	return base == "corepack"
}

func normalizePackageManagerName(name string) string {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(name)))
	base = strings.TrimSuffix(base, ".cmd")
	base = strings.TrimSuffix(base, ".exe")
	switch base {
	case packageManagerNpm, packageManagerPnpm, packageManagerYarn, packageManagerBun:
		return base
	default:
		return ""
	}
}

func splitFirstCommandToken(cmd string) (string, string, string) {
	i := 0
	for i < len(cmd) && (cmd[i] == ' ' || cmd[i] == '\t') {
		i++
	}
	if i >= len(cmd) {
		return cmd, "", ""
	}

	leading := cmd[:i]
	if cmd[i] == '"' || cmd[i] == '\'' {
		quote := cmd[i]
		i++
		start := i
		for i < len(cmd) && cmd[i] != quote {
			i++
		}
		token := cmd[start:i]
		if i < len(cmd) {
			i++
		}
		return leading, token, cmd[i:]
	}

	start := i
	for i < len(cmd) && cmd[i] != ' ' && cmd[i] != '\t' {
		i++
	}
	return leading, cmd[start:i], cmd[i:]
}

func supportsCorepack(manager string) bool {
	return manager == packageManagerPnpm || manager == packageManagerYarn
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
