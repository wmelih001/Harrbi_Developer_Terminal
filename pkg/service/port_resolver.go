package service

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"devterminal/pkg/domain"
)

type PortSource string

const (
	PortSourceScript   PortSource = "script"
	PortSourceEnv      PortSource = "env"
	PortSourceCode     PortSource = "code"
	PortSourceDocker   PortSource = "docker"
	PortSourceFallback PortSource = "fallback"
)

type ResolvedPort struct {
	Port       int
	Role       domain.ComponentRole
	Source     PortSource
	Path       string
	Detail     string
	Confidence int
}

type PortResolveRequest struct {
	RootPath      string
	ComponentPath string
	Role          domain.ComponentRole
	StartCmd      string
}

type PortReport struct {
	Ports []ResolvedPort
}

type PortResolver struct{}

func NewPortResolver() *PortResolver {
	return &PortResolver{}
}

func (r *PortResolver) Resolve(req PortResolveRequest) PortReport {
	if req.ComponentPath == "" {
		req.ComponentPath = req.RootPath
	}

	collector := newPortCollector()
	for _, port := range portsFromCommand(req.StartCmd, req.Role, PortSourceScript, "start command", 90) {
		collector.add(port)
	}

	for _, script := range resolveStartScripts(req.RootPath, req.ComponentPath, req.StartCmd, req.Role) {
		for _, port := range portsFromCommand(script.Command, req.Role, PortSourceScript, script.Path, 95) {
			collector.add(port)
		}
	}

	for _, port := range portsFromEnv(req.RootPath, req.ComponentPath, req.Role) {
		collector.add(port)
	}
	for _, port := range portsFromCode(req.RootPath, req.ComponentPath, req.Role) {
		collector.add(port)
	}
	for _, port := range portsFromDocker(req.RootPath, req.ComponentPath, req.Role) {
		collector.add(port)
	}

	return PortReport{Ports: collector.list()}
}

func ResolveProjectPorts(p domain.Project, mode string) []ResolvedPort {
	var ports []ResolvedPort
	resolver := NewPortResolver()
	for _, component := range componentsForMode(p, mode) {
		report := resolver.Resolve(PortResolveRequest{
			RootPath:      p.Path,
			ComponentPath: component.Path,
			Role:          component.Role,
			StartCmd:      component.StartCmd,
		})
		ports = append(ports, report.Ports...)
	}

	ports = dedupeResolvedPorts(ports)
	if len(ports) > 0 {
		return ports
	}
	return fallbackResolvedPorts(p, mode)
}

func CheckProjectPortsForProject(p domain.Project, mode string) []PortInfo {
	var results []PortInfo
	checked := make(map[int]bool)
	for _, resolved := range ResolveProjectPorts(p, mode) {
		if checked[resolved.Port] {
			continue
		}
		checked[resolved.Port] = true
		info := GetPortInfo(resolved.Port)
		if info.InUse {
			results = append(results, info)
		}
	}
	return results
}

type resolvedScript struct {
	Path    string
	Command string
}

func resolveStartScripts(rootPath, componentPath, startCmd string, role domain.ComponentRole) []resolvedScript {
	scriptName := packageManagerScriptName(startCmd)
	var scripts []resolvedScript
	for _, path := range uniquePaths(componentPath, rootPath) {
		pkg, ok := readPackageJSON(path)
		if !ok {
			continue
		}
		if scriptName != "" {
			if cmd, ok := pkg.Scripts[scriptName]; ok {
				scripts = append(scripts, resolvedScript{Path: filepath.Join(path, "package.json") + " scripts." + scriptName, Command: cmd})
				continue
			}
		}
		for name, cmd := range pkg.Scripts {
			if shouldInspectScriptForPort(name, cmd, role) {
				scripts = append(scripts, resolvedScript{Path: filepath.Join(path, "package.json") + " scripts." + name, Command: cmd})
			}
		}
	}
	return scripts
}

func shouldInspectScriptForPort(scriptName, scriptCmd string, role domain.ComponentRole) bool {
	name := strings.ToLower(scriptName)
	cmd := strings.ToLower(scriptCmd)
	if isNonStartScriptName(name) {
		return false
	}
	if name == "dev" || name == "start" || strings.Contains(name, "dev") || strings.Contains(name, "start") {
		if role == domain.ComponentRoleUnknown {
			return true
		}
		return matchesRoleCommandContent(cmd, role) || strings.Contains(name, string(role))
	}
	return false
}

func portsFromCommand(command string, role domain.ComponentRole, source PortSource, path string, confidence int) []ResolvedPort {
	var ports []ResolvedPort
	for _, port := range parseCommandPorts(command) {
		ports = append(ports, ResolvedPort{
			Port:       port,
			Role:       role,
			Source:     source,
			Path:       path,
			Detail:     strings.TrimSpace(command),
			Confidence: confidence,
		})
	}
	return ports
}

func parseCommandPorts(command string) []int {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?:^|\s)--port(?:=|\s+)(\d{2,5})(?:\s|$)`),
		regexp.MustCompile(`(?:^|\s)-p(?:=|\s+)(\d{2,5})(?:\s|$)`),
		regexp.MustCompile(`(?:^|\s)PORT=(\d{2,5})(?:\s|$)`),
	}
	return portsFromPatterns(command, patterns)
}

func portsFromEnv(rootPath, componentPath string, role domain.ComponentRole) []ResolvedPort {
	var ports []ResolvedPort
	for _, file := range scanEnvFilesForPorts(rootPath, componentPath) {
		for key, value := range file.Variables {
			if !isPortEnvKey(key) {
				continue
			}
			port, ok := parsePort(value)
			if !ok {
				continue
			}
			ports = append(ports, ResolvedPort{
				Port:       port,
				Role:       role,
				Source:     PortSourceEnv,
				Path:       file.Path,
				Detail:     key,
				Confidence: 90,
			})
		}
	}
	return ports
}

func scanEnvFilesForPorts(rootPath, componentPath string) []EnvFileInfo {
	var files []EnvFileInfo
	for _, path := range uniquePaths(componentPath, rootPath) {
		for _, file := range scanEnvFiles(path, EnvScopeComponent) {
			if !file.IsExample {
				files = append(files, file)
			}
		}
	}
	return files
}

func isPortEnvKey(key string) bool {
	key = strings.ToUpper(strings.TrimSpace(key))
	return key == "PORT" ||
		key == "API_PORT" ||
		key == "SERVER_PORT" ||
		key == "APP_PORT" ||
		key == "NEXT_PORT" ||
		key == "VITE_PORT"
}

func portsFromCode(rootPath, componentPath string, role domain.ComponentRole) []ResolvedPort {
	var ports []ResolvedPort
	candidates := []string{
		"src/main.ts",
		"src/main.js",
		"main.ts",
		"main.js",
		"app/main.py",
	}
	for _, base := range uniquePaths(componentPath, rootPath) {
		for _, candidate := range candidates {
			fullPath := filepath.Join(base, candidate)
			data, err := os.ReadFile(fullPath)
			if err != nil {
				continue
			}
			for _, port := range parseCodePorts(string(data)) {
				ports = append(ports, ResolvedPort{
					Port:       port,
					Role:       role,
					Source:     PortSourceCode,
					Path:       fullPath,
					Detail:     "listen",
					Confidence: 86,
				})
			}
		}
	}
	return ports
}

func parseCodePorts(content string) []int {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`listen\(\s*(\d{2,5})`),
		regexp.MustCompile(`app\.run\([^)]*port\s*=\s*(\d{2,5})`),
	}
	return portsFromPatterns(content, patterns)
}

func portsFromDocker(rootPath, componentPath string, role domain.ComponentRole) []ResolvedPort {
	var ports []ResolvedPort
	for _, base := range uniquePaths(componentPath, rootPath) {
		for _, name := range []string{"docker-compose.yml", "docker-compose.yaml"} {
			fullPath := filepath.Join(base, name)
			data, err := os.ReadFile(fullPath)
			if err != nil {
				continue
			}
			for _, port := range parseDockerComposePorts(string(data)) {
				ports = append(ports, ResolvedPort{
					Port:       port,
					Role:       role,
					Source:     PortSourceDocker,
					Path:       fullPath,
					Detail:     "ports mapping",
					Confidence: 80,
				})
			}
		}
	}
	return ports
}

func parseDockerComposePorts(content string) []int {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`["']?(\d{2,5})\s*:\s*\d{2,5}["']?`),
	}
	return portsFromPatterns(content, patterns)
}

func portsFromPatterns(content string, patterns []*regexp.Regexp) []int {
	seen := make(map[int]bool)
	var ports []int
	for _, pattern := range patterns {
		for _, match := range pattern.FindAllStringSubmatch(content, -1) {
			if len(match) < 2 {
				continue
			}
			port, ok := parsePort(match[1])
			if ok && !seen[port] {
				seen[port] = true
				ports = append(ports, port)
			}
		}
	}
	sort.Ints(ports)
	return ports
}

func parsePort(value string) (int, bool) {
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || port <= 0 || port > 65535 {
		return 0, false
	}
	return port, true
}

type portCollector struct {
	items map[int]ResolvedPort
}

func newPortCollector() *portCollector {
	return &portCollector{items: make(map[int]ResolvedPort)}
}

func (c *portCollector) add(port ResolvedPort) {
	if existing, ok := c.items[port.Port]; ok && existing.Confidence >= port.Confidence {
		return
	}
	c.items[port.Port] = port
}

func (c *portCollector) list() []ResolvedPort {
	var ports []ResolvedPort
	for _, port := range c.items {
		ports = append(ports, port)
	}
	sort.Slice(ports, func(i, j int) bool {
		return ports[i].Port < ports[j].Port
	})
	return ports
}

func componentsForMode(p domain.Project, mode string) []domain.ProjectComponent {
	if len(p.Components) == 0 {
		return fallbackComponentsForMode(p, mode)
	}

	var components []domain.ProjectComponent
	for _, component := range p.Components {
		if mode == "full" ||
			(mode == "frontend" && component.Role == domain.ComponentRoleFrontend) ||
			(mode == "backend" && component.Role == domain.ComponentRoleBackend) {
			components = append(components, component)
		}
	}
	return components
}

func fallbackComponentsForMode(p domain.Project, mode string) []domain.ProjectComponent {
	var components []domain.ProjectComponent
	if (mode == "full" || mode == "frontend") && p.HasFrontend {
		components = append(components, domain.ProjectComponent{
			Name:     "frontend",
			Path:     p.FrontendPath,
			Role:     domain.ComponentRoleFrontend,
			StartCmd: p.FrontendCmd,
		})
	}
	if (mode == "full" || mode == "backend") && p.HasBackend {
		components = append(components, domain.ProjectComponent{
			Name:     "backend",
			Path:     p.BackendPath,
			Role:     domain.ComponentRoleBackend,
			StartCmd: p.BackendCmd,
		})
	}
	return components
}

func dedupeResolvedPorts(ports []ResolvedPort) []ResolvedPort {
	collector := newPortCollector()
	for _, port := range ports {
		collector.add(port)
	}
	return collector.list()
}

func fallbackResolvedPorts(p domain.Project, mode string) []ResolvedPort {
	var ports []ResolvedPort
	add := func(role domain.ComponentRole, values []int) {
		for _, port := range values {
			ports = append(ports, ResolvedPort{
				Port:       port,
				Role:       role,
				Source:     PortSourceFallback,
				Path:       p.Path,
				Detail:     "common dev port",
				Confidence: 30,
			})
		}
	}
	if (mode == "full" || mode == "frontend") && p.HasFrontend {
		add(domain.ComponentRoleFrontend, CommonPorts["frontend"])
	}
	if (mode == "full" || mode == "backend") && p.HasBackend {
		add(domain.ComponentRoleBackend, CommonPorts["backend"])
	}
	return dedupeResolvedPorts(ports)
}
