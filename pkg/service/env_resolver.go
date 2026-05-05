package service

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type EnvScope string

const (
	EnvScopeRoot      EnvScope = "root"
	EnvScopeComponent EnvScope = "component"
)

type EnvFileInfo struct {
	Path      string
	Name      string
	Scope     EnvScope
	IsExample bool
	Variables map[string]string
}

type EnvRequirement struct {
	Name     string
	Source   string
	Present  bool
	HasValue bool
}

type EnvReport struct {
	RootPath      string
	ComponentPath string
	Files         []EnvFileInfo
	Requirements  []EnvRequirement
	Missing       []EnvRequirement
}

func (r EnvReport) Requirement(name string) *EnvRequirement {
	for i := range r.Requirements {
		if r.Requirements[i].Name == name {
			return &r.Requirements[i]
		}
	}
	return nil
}

type EnvResolver struct{}

func NewEnvResolver() *EnvResolver {
	return &EnvResolver{}
}

func (r *EnvResolver) Resolve(rootPath, componentPath string) EnvReport {
	if componentPath == "" {
		componentPath = rootPath
	}

	report := EnvReport{
		RootPath:      rootPath,
		ComponentPath: componentPath,
	}

	report.Files = append(report.Files, scanEnvFiles(rootPath, EnvScopeRoot)...)
	if componentPath != "" && !samePath(rootPath, componentPath) {
		report.Files = append(report.Files, scanEnvFiles(componentPath, EnvScopeComponent)...)
	}

	requirements := collectEnvRequirements(report.Files)
	for _, name := range collectPrismaEnvRequirements(rootPath, componentPath) {
		if _, ok := requirements[name]; !ok {
			requirements[name] = EnvRequirement{Name: name, Source: "prisma/schema.prisma"}
		}
	}

	actualValues := collectActualEnvValues(report.Files)
	for name, req := range requirements {
		value, present := actualValues[name]
		req.Present = present
		req.HasValue = present && strings.TrimSpace(value) != ""
		report.Requirements = append(report.Requirements, req)
		if !req.Present || !req.HasValue {
			report.Missing = append(report.Missing, req)
		}
	}

	return report
}

func scanEnvFiles(path string, scope EnvScope) []EnvFileInfo {
	names := []string{".env", ".env.local", ".env.development", ".env.example"}
	var files []EnvFileInfo

	for _, name := range names {
		fullPath := filepath.Join(path, name)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}
		files = append(files, EnvFileInfo{
			Path:      fullPath,
			Name:      name,
			Scope:     scope,
			IsExample: strings.Contains(name, "example"),
			Variables: parseEnvVariables(string(data)),
		})
	}

	return files
}

func parseEnvVariables(content string) map[string]string {
	vars := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if key != "" {
			vars[key] = value
		}
	}
	return vars
}

func collectEnvRequirements(files []EnvFileInfo) map[string]EnvRequirement {
	requirements := make(map[string]EnvRequirement)
	for _, file := range files {
		if !file.IsExample {
			continue
		}
		for name := range file.Variables {
			requirements[name] = EnvRequirement{
				Name:   name,
				Source: file.Path,
			}
		}
	}
	return requirements
}

func collectActualEnvValues(files []EnvFileInfo) map[string]string {
	values := make(map[string]string)
	for _, file := range files {
		if file.IsExample {
			continue
		}
		for name, value := range file.Variables {
			values[name] = value
		}
	}
	return values
}

func collectPrismaEnvRequirements(rootPath, componentPath string) []string {
	seen := make(map[string]bool)
	var names []string
	for _, base := range uniquePaths(rootPath, componentPath) {
		schemaPath := filepath.Join(base, "prisma", "schema.prisma")
		data, err := os.ReadFile(schemaPath)
		if err != nil {
			continue
		}
		for _, name := range parsePrismaEnvNames(string(data)) {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	return names
}

func parsePrismaEnvNames(content string) []string {
	re := regexp.MustCompile(`env\(\s*"([^"]+)"\s*\)`)
	matches := re.FindAllStringSubmatch(content, -1)
	var names []string
	for _, match := range matches {
		if len(match) > 1 {
			names = append(names, match[1])
		}
	}
	return names
}

func uniquePaths(paths ...string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, path := range paths {
		if path == "" {
			continue
		}
		key := strings.ToLower(filepath.Clean(path))
		if !seen[key] {
			seen[key] = true
			result = append(result, path)
		}
	}
	return result
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
