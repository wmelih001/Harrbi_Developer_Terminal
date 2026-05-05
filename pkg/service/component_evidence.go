package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"devterminal/pkg/domain"
)

type componentRoleAnalysis struct {
	Role       domain.ComponentRole
	Confidence int
	Evidence   []domain.ComponentEvidence
}

func analyzeComponentRole(path, name string, preferredRole domain.ComponentRole) componentRoleAnalysis {
	name = strings.ToLower(strings.TrimSpace(name))
	var frontendScore, backendScore int
	var evidence []domain.ComponentEvidence

	add := func(kind, source, detail string, frontend, backend int) {
		if frontend > 0 {
			evidence = append(evidence, domain.ComponentEvidence{
				Kind:   kind,
				Source: source,
				Detail: "frontend: " + detail,
				Score:  frontend,
			})
			frontendScore += frontend
		}
		if backend > 0 {
			evidence = append(evidence, domain.ComponentEvidence{
				Kind:   kind,
				Source: source,
				Detail: "backend: " + detail,
				Score:  backend,
			})
			backendScore += backend
		}
	}

	if isFrontendFolderName(name) {
		add("folder", path, name, 35, 0)
	}
	if isBackendFolderName(name) {
		add("folder", path, name, 0, 35)
	}

	if hasFrontendStructure(path) {
		add("structure", path, "frontend klasor yapisi", 25, 0)
	}
	if hasBackendStructure(path) {
		add("structure", path, "backend klasor yapisi", 0, 25)
	}

	analyzePackageEvidence(path, add)
	analyzeEnvEvidence(path, add)
	analyzeConfigEvidence(path, add)

	if preferredRole == domain.ComponentRoleFrontend {
		add("preferred-role", path, "scanner frontend adayi", 20, 0)
	}
	if preferredRole == domain.ComponentRoleBackend {
		add("preferred-role", path, "scanner backend adayi", 0, 20)
	}

	role := domain.ComponentRoleUnknown
	winner := frontendScore
	loser := backendScore
	if backendScore > frontendScore {
		role = domain.ComponentRoleBackend
		winner = backendScore
		loser = frontendScore
	} else if frontendScore > backendScore {
		role = domain.ComponentRoleFrontend
	}

	if role == domain.ComponentRoleUnknown && preferredRole != domain.ComponentRoleUnknown {
		role = preferredRole
		winner = maxInt(frontendScore, backendScore)
	}

	confidence := componentConfidence(winner, loser)
	if role == domain.ComponentRoleUnknown {
		confidence = 0
	}

	return componentRoleAnalysis{
		Role:       role,
		Confidence: confidence,
		Evidence:   evidence,
	}
}

func analyzePackageEvidence(path string, add func(kind, source, detail string, frontend, backend int)) {
	data, err := os.ReadFile(filepath.Join(path, "package.json"))
	if err != nil {
		return
	}

	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return
	}

	allDeps := make(map[string]bool)
	for dep := range pkg.Dependencies {
		allDeps[strings.ToLower(dep)] = true
	}
	for dep := range pkg.DevDependencies {
		allDeps[strings.ToLower(dep)] = true
	}
	for dep := range pkg.OptionalDependencies {
		allDeps[strings.ToLower(dep)] = true
	}

	for _, dep := range frontendDependencySignals {
		if allDeps[strings.ToLower(dep)] {
			add("dependency", "package.json", dep, 16, 0)
		}
	}
	for _, dep := range backendDependencySignals {
		if allDeps[strings.ToLower(dep)] {
			add("dependency", "package.json", dep, 0, 16)
		}
	}
	for _, dep := range []string{"react", "next", "vue", "vite", "nuxt", "svelte", "astro", "@angular/core"} {
		if allDeps[dep] {
			add("dependency", "package.json", dep, 20, 0)
		}
	}
	for _, dep := range []string{"@nestjs/core", "express", "fastify", "hono", "koa"} {
		if allDeps[dep] {
			add("dependency", "package.json", dep, 0, 20)
		}
	}

	for scriptName, scriptCmd := range pkg.Scripts {
		name := strings.ToLower(scriptName)
		cmd := strings.ToLower(scriptCmd)
		if isNonStartScriptName(name) {
			continue
		}
		if matchesRoleCommandContent(cmd, domain.ComponentRoleFrontend) {
			add("script", "package.json scripts."+scriptName, scriptCmd, 18, 0)
		}
		if matchesRoleCommandContent(cmd, domain.ComponentRoleBackend) {
			add("script", "package.json scripts."+scriptName, scriptCmd, 0, 18)
		}
		if strings.Contains(name, "web") || strings.Contains(name, "client") || strings.Contains(name, "frontend") {
			add("script", "package.json scripts."+scriptName, scriptName, 12, 0)
		}
		if strings.Contains(name, "api") || strings.Contains(name, "server") || strings.Contains(name, "backend") {
			add("script", "package.json scripts."+scriptName, scriptName, 0, 12)
		}
	}
}

func analyzeEnvEvidence(path string, add func(kind, source, detail string, frontend, backend int)) {
	envFiles := []string{".env", ".env.local", ".env.development", ".env.example"}
	for _, envFile := range envFiles {
		data, err := os.ReadFile(filepath.Join(path, envFile))
		if err != nil {
			continue
		}
		content := string(data)
		for _, signal := range frontendEnvSignals {
			if strings.Contains(content, signal) {
				add("env", envFile, signal, 14, 0)
			}
		}
		for _, signal := range backendEnvSignals {
			if strings.Contains(content, signal) {
				add("env", envFile, signal, 0, 14)
			}
		}
	}
}

func analyzeConfigEvidence(path string, add func(kind, source, detail string, frontend, backend int)) {
	frontendFiles := []string{"next.config.js", "next.config.mjs", "vite.config.ts", "vite.config.js", "nuxt.config.ts", "svelte.config.js", "angular.json"}
	backendFiles := []string{"nest-cli.json", "prisma/schema.prisma", "drizzle.config.ts", "go.mod", "requirements.txt"}

	for _, file := range frontendFiles {
		if fileExists(filepath.Join(path, file)) {
			add("config", file, file, 18, 0)
		}
	}
	for _, file := range backendFiles {
		if fileExists(filepath.Join(path, file)) {
			add("config", file, file, 0, 18)
		}
	}
}

func componentConfidence(winner, loser int) int {
	if winner <= 0 {
		return 0
	}
	margin := winner - loser
	confidence := 45 + winner/2 + margin/3
	if confidence > 99 {
		return 99
	}
	if confidence < 35 {
		return 35
	}
	return confidence
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
