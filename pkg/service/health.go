package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"devterminal/pkg/domain"
)

type HealthIssue struct {
	Description string
	Points      int // How many points were lost
}

type HealthReport struct {
	Score            int
	MaxScore         int
	Issues           []HealthIssue
	PassedItems      []string
	ComponentReports []ComponentHealthReport
}

type ComponentHealthReport struct {
	Name        string
	Role        domain.ComponentRole
	Path        string
	Score       int
	MaxScore    int
	Issues      []HealthIssue
	PassedItems []string
}

type HealthService struct{}

func NewHealthService() *HealthService {
	return &HealthService{}
}

func (s *HealthService) CheckHealth(projectPath string) HealthReport {
	return s.CheckProjectHealth(domain.Project{
		Name: filepath.Base(projectPath),
		Path: projectPath,
	})
}

func (s *HealthService) CheckProjectHealth(project domain.Project) HealthReport {
	projectPath := project.Path
	score := 0
	maxScore := 100
	var issues []HealthIssue
	var passed []string

	// Flags for detection
	hasGit := false
	hasDep := false
	hasReadme := false
	hasDocker := false
	hasEnv := false
	hasCICD := false
	hasLinter := false
	hasLicense := false

	// Tek seferlik recursive tarama
	_ = filepath.WalkDir(projectPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		name := d.Name()
		lowerName := strings.ToLower(name)

		// Gereksiz klasörleri atla
		if d.IsDir() && path != projectPath {
			// node_modules, .git (içeriği), vendor, dist, build atla
			if name == "node_modules" || name == ".git" || name == "vendor" || name == "dist" || name == "build" || name == ".next" {
				return filepath.SkipDir
			}
			// Gizli klasörleri atla (.config ve .github, .circleci hariç - bunlar aranıyor)
			if strings.HasPrefix(name, ".") {
				if name != ".config" && name != ".github" && name != ".circleci" {
					return filepath.SkipDir
				}
			}
		}

		// Derinlik kontrolü (Kökten en fazla 3 seviye aşağı in)
		rel, _ := filepath.Rel(projectPath, path)
		depth := strings.Count(rel, string(os.PathSeparator))
		if depth > 3 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// --- 1. Git ---
		if d.IsDir() && name == ".git" {
			hasGit = true
		}

		// --- 2. Dependencies ---
		if !d.IsDir() {
			switch name {
			case "package.json", "go.mod", "requirements.txt", "pom.xml", "build.gradle", "Gemfile", "composer.json", "mix.exs", "Cargo.toml":
				hasDep = true
			}
		}

		// --- 3. Readme ---
		if !d.IsDir() && (strings.HasPrefix(lowerName, "readme")) {
			hasReadme = true
		}

		// --- 4. Docker ---
		if !d.IsDir() && (strings.HasPrefix(lowerName, "dockerfile") || strings.HasPrefix(lowerName, "docker-compose") || name == "Containerfile") {
			hasDocker = true
		}

		// --- 5. Env / Config ---
		if !d.IsDir() {
			if strings.HasPrefix(name, ".env") ||
				name == "config.yaml" ||
				name == "config.json" ||
				name == "config.js" {
				hasEnv = true
			}
		}

		// --- 6. CI/CD ---
		if d.IsDir() && (name == ".github" || name == ".circleci") {
			hasCICD = true
		}
		if !d.IsDir() && (name == ".gitlab-ci.yml" || name == "azure-pipelines.yml" || name == "Jenkinsfile") {
			hasCICD = true
		}

		// --- 7. Linter ---
		if !d.IsDir() {
			if strings.HasPrefix(name, ".eslintrc") ||
				strings.HasPrefix(name, ".prettierrc") ||
				name == "golangci.yml" ||
				name == ".pylintrc" ||
				name == "checkstyle.xml" ||
				name == "rubocop.yml" {
				hasLinter = true
			}
		}

		// --- 8. License ---
		if !d.IsDir() && (strings.HasPrefix(lowerName, "license") || strings.HasPrefix(lowerName, "copying")) {
			hasLicense = true
		}

		return nil
	})

	// Puanlama ve Raporlama
	if hasGit {
		score += 20
		passed = append(passed, "Git Repository (20p)")
	} else {
		issues = append(issues, HealthIssue{"Git başlatılmamış (.git yok)", 20})
	}

	if hasDep {
		score += 20
		passed = append(passed, "Bağımlılık Dosyası (20p)")
	} else {
		issues = append(issues, HealthIssue{"Bağımlılık dosyası bulunamadı", 20})
	}

	if hasReadme {
		score += 10
		passed = append(passed, "README (10p)")
	} else {
		issues = append(issues, HealthIssue{"README dosyası eksik", 10})
	}

	if hasDocker {
		score += 10
		passed = append(passed, "Konteyner Yapılandırması (10p)")
	} else {
		issues = append(issues, HealthIssue{"Docker/Konteyner yapılandırması yok", 10})
	}

	if hasEnv {
		score += 10
		passed = append(passed, "Ortam Değişkenleri (.env)/Config (10p)")
	} else {
		issues = append(issues, HealthIssue{"Konfigürasyon/Env dosyası yok", 10})
	}

	if hasCICD {
		score += 10
		passed = append(passed, "CI/CD Yapılandırması (10p)")
	} else {
		issues = append(issues, HealthIssue{"CI/CD yapılandırması bulunamadı", 10})
	}

	if hasLinter {
		score += 10
		passed = append(passed, "Linter/Formatter Ayarları (10p)")
	} else {
		issues = append(issues, HealthIssue{"Linter/Formatter ayarları eksik", 10})
	}

	if hasLicense {
		score += 10
		passed = append(passed, "Lisans Dosyası (10p)")
	} else {
		issues = append(issues, HealthIssue{"Lisans dosyası eksik", 10})
	}

	if project.IsMonorepo || len(project.Components) > 1 {
		monorepoScore, monorepoIssues, monorepoPassed := s.checkMonorepoHealth(project)
		score = (score * 7 / 10) + monorepoScore
		issues = append(issues, monorepoIssues...)
		passed = append(passed, monorepoPassed...)
	}

	componentReports := s.checkComponentHealth(project)
	if len(componentReports) > 0 {
		avg := averageComponentHealthScore(componentReports)
		score = (score + avg) / 2
		for _, component := range componentReports {
			for _, issue := range component.Issues {
				issues = append(issues, HealthIssue{
					Description: component.Name + ": " + issue.Description,
					Points:      issue.Points,
				})
			}
		}
	}
	if score > 100 {
		score = 100
	}

	return HealthReport{
		Score:            score,
		MaxScore:         maxScore,
		Issues:           issues,
		PassedItems:      passed,
		ComponentReports: componentReports,
	}
}

func (s *HealthService) checkMonorepoHealth(project domain.Project) (int, []HealthIssue, []string) {
	score := 0
	var issues []HealthIssue
	var passed []string

	if hasWorkspaceConfig(project.Path) {
		score += 15
		passed = append(passed, "Workspace Yapılandırması (15p)")
	} else {
		issues = append(issues, HealthIssue{"Workspace yapılandırması eksik", 15})
	}

	if hasRootComponentScripts(project) {
		score += 15
		passed = append(passed, "Root frontend/backend scriptleri (15p)")
	} else {
		issues = append(issues, HealthIssue{"Root frontend/backend scriptleri eksik", 15})
	}

	return score, issues, passed
}

func (s *HealthService) checkComponentHealth(project domain.Project) []ComponentHealthReport {
	components := project.Components
	if len(components) == 0 {
		if project.HasFrontend && project.FrontendPath != "" {
			components = append(components, domain.ProjectComponent{
				Name:     componentName(project.Name, project.FrontendPath, "frontend"),
				Path:     project.FrontendPath,
				Role:     domain.ComponentRoleFrontend,
				Type:     project.FrontendType,
				StartCmd: project.FrontendCmd,
			})
		}
		if project.HasBackend && project.BackendPath != "" {
			components = append(components, domain.ProjectComponent{
				Name:     componentName(project.Name, project.BackendPath, "backend"),
				Path:     project.BackendPath,
				Role:     domain.ComponentRoleBackend,
				Type:     project.BackendType,
				StartCmd: project.BackendCmd,
			})
		}
	}

	var reports []ComponentHealthReport
	for _, component := range components {
		if component.Path == "" {
			continue
		}
		reports = append(reports, s.checkSingleComponentHealth(project.Path, component))
	}
	return reports
}

func (s *HealthService) checkSingleComponentHealth(rootPath string, component domain.ProjectComponent) ComponentHealthReport {
	pkg := readHealthPackageJSON(component.Path)
	score := 0
	maxScore := 100
	var issues []HealthIssue
	var passed []string

	add := func(ok bool, points int, passText, issueText string) {
		if ok {
			score += points
			passed = append(passed, passText)
			return
		}
		issues = append(issues, HealthIssue{issueText, points})
	}

	add(fileExists(filepath.Join(component.Path, "package.json")) ||
		fileExists(filepath.Join(component.Path, "go.mod")) ||
		fileExists(filepath.Join(component.Path, "requirements.txt")) ||
		fileExists(filepath.Join(component.Path, "pubspec.yaml")),
		15, "Bileşen bağımlılık dosyası (15p)", "Bileşen bağımlılık dosyası eksik")
	add(component.StartCmd != "" || hasScript(pkg, "dev", "start", "serve"),
		15, "Bileşen başlatma scripti (15p)", "Bileşen başlatma scripti eksik")

	switch component.Role {
	case domain.ComponentRoleFrontend:
		add(hasFrontendDependency(component, pkg),
			15, "Frontend framework sinyali (15p)", "Frontend framework sinyali eksik")
		add(hasScript(pkg, "build"),
			20, "Frontend build scripti (20p)", "Frontend build scripti eksik")
		add(hasScriptContaining(pkg, "lint") || hasLinterConfig(component.Path),
			15, "Frontend lint kontrolü (15p)", "Frontend lint scripti eksik")
		add(hasScriptContaining(pkg, "type") || fileExists(filepath.Join(component.Path, "tsconfig.json")),
			15, "Frontend typecheck kontrolü (15p)", "Frontend typecheck scripti eksik")
		add(hasEnvExample(component.Path),
			5, "Frontend env örneği (5p)", "Frontend env example eksik")
		add(hasScriptContaining(pkg, "test"),
			15, "Frontend test scripti (15p)", "Frontend test scripti eksik")
	case domain.ComponentRoleBackend:
		add(hasBackendDependency(component, pkg),
			15, "Backend runtime/framework sinyali (15p)", "Backend framework sinyali eksik")
		add(hasScript(pkg, "build"),
			15, "Backend build scripti (15p)", "Backend build scripti eksik")
		add(hasScriptContaining(pkg, "lint") || hasLinterConfig(component.Path),
			15, "Backend lint kontrolü (15p)", "Backend lint scripti eksik")
		add(hasScriptContaining(pkg, "test"),
			15, "Backend test scripti (15p)", "Backend test scripti eksik")
		add(hasBackendEnvExample(rootPath, component.Path, pkg),
			20, "Backend env örneği (20p)", "Backend env example eksik")
		add(hasBackendSchemaOrConfig(component.Path, pkg),
			5, "Backend schema/config sinyali (5p)", "Backend schema/config sinyali eksik")
	default:
		add(hasScript(pkg, "build"), 20, "Build scripti (20p)", "Build scripti eksik")
		add(hasScriptContaining(pkg, "lint") || hasLinterConfig(component.Path), 15, "Lint kontrolü (15p)", "Lint scripti eksik")
		add(hasScriptContaining(pkg, "test"), 15, "Test scripti (15p)", "Test scripti eksik")
		add(hasEnvExample(component.Path), 20, "Env örneği (20p)", "Env example eksik")
	}

	return ComponentHealthReport{
		Name:        firstNonEmptyString(component.Name, filepath.Base(component.Path)),
		Role:        component.Role,
		Path:        component.Path,
		Score:       normalizeScore(score, maxScore),
		MaxScore:    maxScore,
		Issues:      issues,
		PassedItems: passed,
	}
}

type healthPackageJSON struct {
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	Workspaces      interface{}       `json:"workspaces"`
}

func readHealthPackageJSON(path string) healthPackageJSON {
	data, err := os.ReadFile(filepath.Join(path, "package.json"))
	if err != nil {
		return healthPackageJSON{}
	}
	var pkg healthPackageJSON
	_ = json.Unmarshal(data, &pkg)
	return pkg
}

func hasWorkspaceConfig(path string) bool {
	if fileExists(filepath.Join(path, "pnpm-workspace.yaml")) ||
		fileExists(filepath.Join(path, "lerna.json")) ||
		fileExists(filepath.Join(path, "turbo.json")) ||
		fileExists(filepath.Join(path, "nx.json")) {
		return true
	}
	pkg := readHealthPackageJSON(path)
	return pkg.Workspaces != nil
}

func hasRootComponentScripts(project domain.Project) bool {
	pkg := readHealthPackageJSON(project.Path)
	if len(pkg.Scripts) == 0 {
		return false
	}
	needFrontend := project.HasFrontend
	needBackend := project.HasBackend
	for _, component := range project.Components {
		needFrontend = needFrontend || component.Role == domain.ComponentRoleFrontend
		needBackend = needBackend || component.Role == domain.ComponentRoleBackend
	}

	seenFrontend := !needFrontend
	seenBackend := !needBackend
	for _, component := range project.Components {
		if component.Role == domain.ComponentRoleFrontend {
			seenFrontend = seenFrontend || hasScriptForComponent(pkg, component, "frontend", "web", "client", "ui")
		}
		if component.Role == domain.ComponentRoleBackend {
			seenBackend = seenBackend || hasScriptForComponent(pkg, component, "backend", "api", "server", "service")
		}
	}
	return seenFrontend && seenBackend
}

func hasScriptForComponent(pkg healthPackageJSON, component domain.ProjectComponent, aliases ...string) bool {
	name := strings.ToLower(component.Name)
	for scriptName, scriptContent := range pkg.Scripts {
		text := strings.ToLower(scriptName + " " + scriptContent)
		if name != "" && strings.Contains(text, name) {
			return true
		}
		for _, alias := range aliases {
			if strings.Contains(text, alias) {
				return true
			}
		}
	}
	return false
}

func hasScript(pkg healthPackageJSON, names ...string) bool {
	for _, name := range names {
		if _, ok := pkg.Scripts[name]; ok {
			return true
		}
	}
	return false
}

func hasScriptContaining(pkg healthPackageJSON, token string) bool {
	token = strings.ToLower(token)
	for name, content := range pkg.Scripts {
		if strings.Contains(strings.ToLower(name), token) || strings.Contains(strings.ToLower(content), token) {
			return true
		}
	}
	return false
}

func hasDependency(pkg healthPackageJSON, names ...string) bool {
	for _, name := range names {
		if _, ok := pkg.Dependencies[name]; ok {
			return true
		}
		if _, ok := pkg.DevDependencies[name]; ok {
			return true
		}
	}
	return false
}

func hasFrontendDependency(component domain.ProjectComponent, pkg healthPackageJSON) bool {
	if component.Type == domain.TypeNext ||
		component.Type == domain.TypeReact ||
		component.Type == domain.TypeVue ||
		component.Type == domain.TypeAngular ||
		component.Type == domain.TypeSvelte ||
		component.Type == domain.TypeAstro ||
		component.Type == domain.TypeRemix ||
		component.Type == domain.TypeNuxt {
		return true
	}
	return hasDependency(pkg, "next", "react", "vue", "@angular/core", "svelte", "astro", "@remix-run/react", "nuxt", "vite")
}

func hasBackendDependency(component domain.ProjectComponent, pkg healthPackageJSON) bool {
	if component.Type == domain.TypeNest ||
		component.Type == domain.TypeExpress ||
		component.Type == domain.TypeGo ||
		component.Type == domain.TypeDjango ||
		component.Type == domain.TypeFlask ||
		component.Type == domain.TypeLaravel ||
		component.Type == domain.TypeSpring ||
		component.Type == domain.TypeFastAPI ||
		component.Type == domain.TypeHono ||
		component.Type == domain.TypeKoa {
		return true
	}
	return hasDependency(pkg, "@nestjs/core", "express", "fastify", "hono", "koa", "@prisma/client", "django", "flask", "fastapi")
}

func hasLinterConfig(path string) bool {
	configs := []string{
		".eslintrc", ".eslintrc.json", ".eslintrc.js", ".eslintrc.cjs",
		"eslint.config.js", "eslint.config.mjs", ".prettierrc", "golangci.yml",
	}
	for _, name := range configs {
		if fileExists(filepath.Join(path, name)) {
			return true
		}
	}
	return false
}

func hasEnvExample(path string) bool {
	return fileExists(filepath.Join(path, ".env.example")) ||
		fileExists(filepath.Join(path, ".env.sample")) ||
		fileExists(filepath.Join(path, ".env.template"))
}

func hasBackendEnvExample(rootPath, componentPath string, pkg healthPackageJSON) bool {
	if hasEnvExample(componentPath) || hasEnvExample(rootPath) {
		return true
	}
	if !backendNeedsEnv(componentPath, pkg) {
		return true
	}
	return false
}

func backendNeedsEnv(path string, pkg healthPackageJSON) bool {
	if hasDependency(pkg, "@prisma/client", "prisma", "typeorm", "sequelize", "mongoose", "pg", "mysql2", "mongodb", "dotenv") {
		return true
	}
	return fileExists(filepath.Join(path, "prisma", "schema.prisma")) ||
		fileExists(filepath.Join(path, "src", "prisma", "schema.prisma"))
}

func hasBackendSchemaOrConfig(path string, pkg healthPackageJSON) bool {
	return fileExists(filepath.Join(path, "prisma", "schema.prisma")) ||
		fileExists(filepath.Join(path, "src", "prisma", "schema.prisma")) ||
		fileExists(filepath.Join(path, "nest-cli.json")) ||
		fileExists(filepath.Join(path, "tsconfig.json")) ||
		hasDependency(pkg, "@nestjs/config", "dotenv")
}

func averageComponentHealthScore(reports []ComponentHealthReport) int {
	if len(reports) == 0 {
		return 0
	}
	total := 0
	for _, report := range reports {
		total += report.Score
	}
	return total / len(reports)
}

func normalizeScore(score, maxScore int) int {
	if maxScore <= 0 {
		return 0
	}
	normalized := score * 100 / maxScore
	if normalized > 100 {
		return 100
	}
	if normalized < 0 {
		return 0
	}
	return normalized
}
