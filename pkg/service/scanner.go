package service

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"devterminal/pkg/config"
	"devterminal/pkg/domain"
)

// Scanner handles project discovery
type Scanner struct {
	Config *domain.Config
}

func NewScanner(cfg *domain.Config) *Scanner {
	return &Scanner{Config: cfg}
}

// packageJSON minimal struct for parsing
type packageJSON struct {
	Dependencies         map[string]string `json:"dependencies"`
	DevDependencies      map[string]string `json:"devDependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
	Scripts              map[string]string `json:"scripts"`
}

// composerJSON for PHP projects
type composerJSON struct {
	Require    map[string]string `json:"require"`
	RequireDev map[string]string `json:"require-dev"`
}

// TechSignature represents a technology detection signature
type TechSignature struct {
	Type       domain.ProjectType
	IsFrontend bool                             // true = Frontend, false = Backend
	CheckFunc  func(path string) (bool, string) // Returns (found, version)
}

// ProjectCache cache yapısı
type ProjectCache struct {
	Version          int               `json:"version"` // Cache versiyonu (yapısal değişiklikler için)
	Projects         []domain.Project  `json:"projects"`
	Timestamp        int64             `json:"timestamp"`
	RootPath         string            `json:"root_path"`
	RootModTimes     map[string]int64  `json:"root_mod_times"`
	FileFingerprints map[string]string `json:"file_fingerprints"`
}

const (
	CurrentCacheVersion = 13 // Versiyon 13: Docker servis durumlari ayri model alani oldu
	cacheFileName       = ".devterminal_cache.json"
)

// loadCache cache'den projeleri yükler
func (s *Scanner) loadCache(rootPath string) ([]domain.Project, bool) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, false
	}
	cachePath := filepath.Join(homeDir, cacheFileName)

	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, false
	}

	var cache ProjectCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, false
	}

	// Versiyon kontrolü
	if cache.Version != CurrentCacheVersion {
		return nil, false // Versiyon değişmiş, yeniden tara
	}

	// Root path (bileşimi) değiştiyse cache geçersiz
	if cache.RootPath != rootPath {
		return nil, false
	}

	if cache.FileFingerprints == nil {
		return nil, false
	}
	if !fingerprintsEqual(cache.FileFingerprints, criticalFileFingerprints(s.Config.ProjectsPaths)) {
		return nil, false
	}

	if len(cache.RootModTimes) > 0 {
		for _, path := range s.Config.ProjectsPaths {
			if info, err := os.Stat(path); err == nil {
				currentModTime := info.ModTime().Unix()
				if cachedTime, ok := cache.RootModTimes[path]; !ok || cachedTime != currentModTime {
					if !fingerprintsEqual(cache.FileFingerprints, criticalFileFingerprints(s.Config.ProjectsPaths)) {
						return nil, false
					}
				}
			}
		}
	}

	return cache.Projects, true
}

// saveCache projeleri cache'e kaydeder
func (s *Scanner) saveCache(rootPath string, projects []domain.Project) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	cachePath := filepath.Join(homeDir, cacheFileName)

	// Kök klasörlerin mod time'larını al
	modTimes := make(map[string]int64)
	for _, path := range s.Config.ProjectsPaths {
		if info, err := os.Stat(path); err == nil {
			modTimes[path] = info.ModTime().Unix()
		}
	}

	cache := ProjectCache{
		Version:          CurrentCacheVersion,
		Projects:         projects,
		Timestamp:        time.Now().Unix(),
		RootPath:         rootPath,
		RootModTimes:     modTimes,
		FileFingerprints: criticalFileFingerprints(s.Config.ProjectsPaths),
	}

	data, _ := json.Marshal(cache)
	_ = os.WriteFile(cachePath, data, 0644)
}

func criticalFileFingerprints(rootPaths []string) map[string]string {
	fingerprints := make(map[string]string)
	for _, rootPath := range rootPaths {
		_ = filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}

			name := d.Name()
			if d.IsDir() {
				if path != rootPath && shouldSkipFingerprintDir(name) {
					return filepath.SkipDir
				}
				return nil
			}

			if !isCriticalCacheFile(name) {
				return nil
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			rel, err := filepath.Rel(rootPath, path)
			if err != nil {
				rel = path
			}
			key := filepath.Clean(rel)
			if len(rootPaths) > 1 {
				key = filepath.Clean(rootPath) + string(os.PathSeparator) + key
			}
			sum := sha256.Sum256(append([]byte(key), data...))
			fingerprints[key] = fmt.Sprintf("%x", sum[:])
			return nil
		})
	}
	return fingerprints
}

func shouldSkipFingerprintDir(name string) bool {
	switch strings.ToLower(name) {
	case ".git", "node_modules", "vendor", "dist", "build", ".next", ".turbo", ".cache", "coverage":
		return true
	default:
		return false
	}
}

func isCriticalCacheFile(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case "package.json",
		"go.mod",
		"go.sum",
		"requirements.txt",
		"pyproject.toml",
		"poetry.lock",
		"pubspec.yaml",
		"pubspec.lock",
		"composer.json",
		"composer.lock",
		"cargo.toml",
		"cargo.lock",
		"package-lock.json",
		"npm-shrinkwrap.json",
		"pnpm-lock.yaml",
		"pnpm-workspace.yaml",
		"yarn.lock",
		"bun.lock",
		"bun.lockb",
		"turbo.json",
		"nx.json",
		"lerna.json",
		".env.example",
		".env.sample",
		".env.template",
		"docker-compose.yml",
		"docker-compose.yaml",
		"compose.yml",
		"compose.yaml":
		return true
	default:
		return false
	}
}

func fingerprintsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

// Klasör adı bazlı ipuçları
var frontendFolderHints = []string{
	"frontend", "client", "web", "app", "ui",
	"next", "react", "vue", "dashboard", "portal",
	"website", "site", "front", "www",
}

var backendFolderHints = []string{
	"backend", "api", "server", "service", "services",
	"nest", "express", "core", "gateway", "microservice",
	"rest", "graphql", "api-server", "back",
}

// isFrontendFolderName klasör adının frontend ipucu verip vermediğini kontrol eder
func isFrontendFolderName(folderName string) bool {
	name := strings.ToLower(folderName)
	for _, hint := range frontendFolderHints {
		if name == hint || strings.Contains(name, hint) {
			return true
		}
	}
	return false
}

// isBackendFolderName klasör adının backend ipucu verip vermediğini kontrol eder
func isBackendFolderName(folderName string) bool {
	name := strings.ToLower(folderName)
	for _, hint := range backendFolderHints {
		if name == hint || strings.Contains(name, hint) {
			return true
		}
	}
	return false
}

// Monorepo klasör desenleri
var monorepoFolderPatterns = []string{
	"apps",
	"packages",
	"services",
	"libs",
	"modules",
	"workspaces",
}

// isMonorepoFolder klasörün monorepo yapısında olup olmadığını kontrol eder
func isMonorepoFolder(folderName string) bool {
	name := strings.ToLower(folderName)
	for _, pattern := range monorepoFolderPatterns {
		if name == pattern {
			return true
		}
	}
	return false
}

// hasMonorepoStructure projenin monorepo yapısında olup olmadığını kontrol eder
func hasMonorepoStructure(projectPath string) bool {
	entries, err := os.ReadDir(projectPath)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if entry.IsDir() && isMonorepoFolder(entry.Name()) {
			return true
		}
	}

	// pnpm-workspace.yaml veya lerna.json varsa monorepo
	if _, err := os.Stat(filepath.Join(projectPath, "pnpm-workspace.yaml")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(projectPath, "lerna.json")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(projectPath, "turbo.json")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(projectPath, "nx.json")); err == nil {
		return true
	}

	return false
}

// Dosya yapısı bazlı backend sinyalleri
var backendStructureSignals = []string{
	"controllers",
	"routes",
	"services",
	"prisma",
	"migrations",
	"models",
	"middleware",
	"middlewares",
	"guards",
	"interceptors",
	"pipes",
	"decorators",
	"entities",
	"repositories",
	"dto",
	"schemas",
}

// Dosya yapısı bazlı frontend sinyalleri
var frontendStructureSignals = []string{
	"components",
	"pages",
	"app",
	"public",
	"styles",
	"css",
	"assets",
	"hooks",
	"contexts",
	"store",
	"redux",
	"views",
	"layouts",
}

// hasBackendStructure klasör içinde backend yapısı olup olmadığını kontrol eder
func hasBackendStructure(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}

	// src/ klasörü varsa, onu da kontrol et
	srcPath := filepath.Join(path, "src")
	srcEntries, srcErr := os.ReadDir(srcPath)

	// Her iki kaynaktan gelen klasörleri birleştir
	allDirs := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() {
			allDirs[strings.ToLower(entry.Name())] = true
		}
	}
	if srcErr == nil {
		for _, entry := range srcEntries {
			if entry.IsDir() {
				allDirs[strings.ToLower(entry.Name())] = true
			}
		}
	}

	// Backend sinyallerini kontrol et
	signalCount := 0
	for _, signal := range backendStructureSignals {
		if allDirs[signal] {
			signalCount++
		}
	}

	// En az 2 sinyal varsa backend olarak kabul et
	return signalCount >= 2
}

// hasFrontendStructure klasör içinde frontend yapısı olup olmadığını kontrol eder
func hasFrontendStructure(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}

	// src/ klasörü varsa, onu da kontrol et
	srcPath := filepath.Join(path, "src")
	srcEntries, srcErr := os.ReadDir(srcPath)

	// Her iki kaynaktan gelen klasörleri birleştir
	allDirs := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() {
			allDirs[strings.ToLower(entry.Name())] = true
		}
	}
	if srcErr == nil {
		for _, entry := range srcEntries {
			if entry.IsDir() {
				allDirs[strings.ToLower(entry.Name())] = true
			}
		}
	}

	// Frontend sinyallerini kontrol et
	signalCount := 0
	for _, signal := range frontendStructureSignals {
		if allDirs[signal] {
			signalCount++
		}
	}

	// En az 2 sinyal varsa frontend olarak kabul et
	return signalCount >= 2
}

// Dependency bazlı backend sinyalleri (package.json dependencies)
var backendDependencySignals = []string{
	// Server/API
	"cors",
	"helmet",
	"cookie-parser",
	"body-parser",
	"compression",
	"morgan",
	"winston",
	// Auth
	"jsonwebtoken",
	"bcrypt",
	"bcryptjs",
	"passport",
	"passport-jwt",
	// Database
	"prisma",
	"@prisma/client",
	"typeorm",
	"sequelize",
	"mongoose",
	"pg",
	"mysql2",
	"mongodb",
	"redis",
	"ioredis",
	// Validation
	"class-validator",
	"class-transformer",
	"joi",
	"yup",
	// Queue/Messaging
	"bull",
	"amqplib",
	"kafkajs",
}

// Dependency bazlı frontend sinyalleri (package.json dependencies)
var frontendDependencySignals = []string{
	// Styling
	"tailwindcss",
	"sass",
	"styled-components",
	"@emotion/react",
	"@emotion/styled",
	// Animation
	"framer-motion",
	"gsap",
	"animate.css",
	// UI Libraries
	"@mui/material",
	"@chakra-ui/react",
	"antd",
	"@radix-ui/react-dialog",
	"shadcn",
	// State Management (Frontend specific)
	"zustand",
	"recoil",
	"jotai",
	"@tanstack/react-query",
	"swr",
	// Form
	"react-hook-form",
	"formik",
	// Icons
	"lucide-react",
	"react-icons",
	"@heroicons/react",
	// Charts
	"recharts",
	"chart.js",
	"react-chartjs-2",
}

// hasBackendDependencies package.json içinde backend dependency'leri olup olmadığını kontrol eder
func (s *Scanner) hasBackendDependencies(path string) bool {
	pkgPath := filepath.Join(path, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return false
	}

	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return false
	}

	// Tüm dependency'leri birleştir
	allDeps := make(map[string]bool)
	for dep := range pkg.Dependencies {
		allDeps[dep] = true
	}
	for dep := range pkg.DevDependencies {
		allDeps[dep] = true
	}

	// Backend sinyallerini say
	signalCount := 0
	for _, signal := range backendDependencySignals {
		if allDeps[signal] {
			signalCount++
		}
	}

	// En az 2 backend dependency varsa backend olarak kabul et
	return signalCount >= 2
}

// hasFrontendDependencies package.json içinde frontend dependency'leri olup olmadığını kontrol eder
func (s *Scanner) hasFrontendDependencies(path string) bool {
	pkgPath := filepath.Join(path, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return false
	}

	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return false
	}

	// Tüm dependency'leri birleştir
	allDeps := make(map[string]bool)
	for dep := range pkg.Dependencies {
		allDeps[dep] = true
	}
	for dep := range pkg.DevDependencies {
		allDeps[dep] = true
	}

	// Frontend sinyallerini say
	signalCount := 0
	for _, signal := range frontendDependencySignals {
		if allDeps[signal] {
			signalCount++
		}
	}

	// En az 2 frontend dependency varsa frontend olarak kabul et
	return signalCount >= 2
}

// .env dosyasındaki backend sinyalleri
var backendEnvSignals = []string{
	// Database
	"DATABASE_URL",
	"DB_HOST",
	"DB_PORT",
	"DB_NAME",
	"DB_USER",
	"DB_PASSWORD",
	"MONGODB_URI",
	"MONGO_URL",
	"REDIS_URL",
	"REDIS_HOST",
	// Auth
	"JWT_SECRET",
	"JWT_EXPIRES_IN",
	"ACCESS_TOKEN_SECRET",
	"REFRESH_TOKEN_SECRET",
	"SESSION_SECRET",
	// API
	"API_PORT",
	"PORT",
	"API_KEY",
	"SECRET_KEY",
	// Email
	"SMTP_HOST",
	"SMTP_PORT",
	"MAIL_HOST",
	"SENDGRID_API_KEY",
	// Cloud
	"AWS_ACCESS_KEY",
	"AWS_SECRET_KEY",
	"S3_BUCKET",
	"CLOUDINARY_URL",
}

// .env dosyasındaki frontend sinyalleri
var frontendEnvSignals = []string{
	// Next.js public vars
	"NEXT_PUBLIC_",
	"NEXT_PUBLIC_API_URL",
	"NEXT_PUBLIC_APP_URL",
	// Vite public vars
	"VITE_",
	"VITE_API_URL",
	"VITE_APP_URL",
	// React public vars
	"REACT_APP_",
	"REACT_APP_API_URL",
	// Analytics
	"NEXT_PUBLIC_GA_ID",
	"NEXT_PUBLIC_ANALYTICS",
}

// hasBackendEnvConfig .env dosyasında backend config'leri olup olmadığını kontrol eder
func hasBackendEnvConfig(path string) bool {
	envFiles := []string{".env", ".env.local", ".env.development", ".env.example"}

	for _, envFile := range envFiles {
		envPath := filepath.Join(path, envFile)
		data, err := os.ReadFile(envPath)
		if err != nil {
			continue
		}

		content := string(data)
		signalCount := 0
		lines := strings.Split(content, "\n")

		for _, line := range lines {
			line = strings.TrimSpace(line)
			// Yorum satırlarını atla
			if strings.HasPrefix(line, "#") {
				continue
			}

			for _, signal := range backendEnvSignals {
				if strings.Contains(line, signal) {
					signalCount++
				}
			}
		}

		// En az 2 backend env var varsa backend olarak kabul et
		if signalCount >= 2 {
			return true
		}
	}

	return false
}

// hasFrontendEnvConfig .env dosyasında frontend config'leri olup olmadığını kontrol eder
func hasFrontendEnvConfig(path string) bool {
	envFiles := []string{".env", ".env.local", ".env.development", ".env.example"}

	for _, envFile := range envFiles {
		envPath := filepath.Join(path, envFile)
		data, err := os.ReadFile(envPath)
		if err != nil {
			continue
		}

		content := string(data)
		lines := strings.Split(content, "\n")

		for _, line := range lines {
			line = strings.TrimSpace(line)
			// Yorum satırlarını atla
			if strings.HasPrefix(line, "#") {
				continue
			}

			// Frontend prefix'lerini kontrol et (NEXT_PUBLIC_, VITE_, REACT_APP_)
			for _, signal := range frontendEnvSignals {
				if strings.Contains(line, signal) {
					return true // Tek bir frontend prefix bile yeterli
				}
			}
		}
	}

	return false
}

// ============================================
// AKILLI VERSİYON TESPİTİ FONKSİYONLARI
// ============================================

// getGoVersion go.mod dosyasından Go versiyonunu okur
func getGoVersion(path string) string {
	goModPath := filepath.Join(path, "go.mod")
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return ""
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "go ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1] // "go 1.21" -> "1.21"
			}
		}
	}
	return ""
}

// getPythonVersion Python versiyonunu çeşitli kaynaklardan okur
func getPythonVersion(path string) string {
	// 1. .python-version dosyası (pyenv)
	pyenvPath := filepath.Join(path, ".python-version")
	if data, err := os.ReadFile(pyenvPath); err == nil {
		ver := strings.TrimSpace(string(data))
		if ver != "" {
			return ver
		}
	}

	// 2. runtime.txt dosyası (Heroku)
	runtimePath := filepath.Join(path, "runtime.txt")
	if data, err := os.ReadFile(runtimePath); err == nil {
		content := strings.TrimSpace(string(data))
		// python-3.11.0 -> 3.11.0
		if strings.HasPrefix(content, "python-") {
			return strings.TrimPrefix(content, "python-")
		}
		return content
	}

	// 3. pyproject.toml dosyası
	pyprojectPath := filepath.Join(path, "pyproject.toml")
	if data, err := os.ReadFile(pyprojectPath); err == nil {
		content := string(data)
		// python = "^3.11" veya requires-python = ">=3.11"
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			if strings.Contains(line, "python") && strings.Contains(line, "=") {
				// Basit regex yerine string işleme
				if idx := strings.Index(line, "\""); idx != -1 {
					rest := line[idx+1:]
					if endIdx := strings.Index(rest, "\""); endIdx != -1 {
						ver := rest[:endIdx]
						ver = strings.TrimPrefix(ver, "^")
						ver = strings.TrimPrefix(ver, ">=")
						ver = strings.TrimPrefix(ver, "~")
						return ver
					}
				}
			}
		}
	}

	return ""
}

// getNodeVersion Node.js versiyonunu çeşitli kaynaklardan okur
func (s *Scanner) getNodeVersion(path string) string {
	// 1. .nvmrc dosyası
	nvmrcPath := filepath.Join(path, ".nvmrc")
	if data, err := os.ReadFile(nvmrcPath); err == nil {
		ver := strings.TrimSpace(string(data))
		ver = strings.TrimPrefix(ver, "v")
		if ver != "" {
			return ver
		}
	}

	// 2. .node-version dosyası
	nodeVerPath := filepath.Join(path, ".node-version")
	if data, err := os.ReadFile(nodeVerPath); err == nil {
		ver := strings.TrimSpace(string(data))
		ver = strings.TrimPrefix(ver, "v")
		if ver != "" {
			return ver
		}
	}

	// 3. package.json engines alanı
	pkgPath := filepath.Join(path, "package.json")
	if data, err := os.ReadFile(pkgPath); err == nil {
		var pkg struct {
			Engines struct {
				Node string `json:"node"`
			} `json:"engines"`
		}
		if err := json.Unmarshal(data, &pkg); err == nil && pkg.Engines.Node != "" {
			ver := pkg.Engines.Node
			// ">=18.0.0" -> "18.0.0"
			ver = strings.TrimPrefix(ver, ">=")
			ver = strings.TrimPrefix(ver, "^")
			ver = strings.TrimPrefix(ver, "~")
			ver = strings.TrimPrefix(ver, "v")
			// "18.x" veya "18" olabilir
			return ver
		}
	}

	return ""
}

// getDockerBaseImage Dockerfile'dan base image bilgisini okur
func getDockerBaseImage(path string) string {
	dockerfilePath := filepath.Join(path, "Dockerfile")
	data, err := os.ReadFile(dockerfilePath)
	if err != nil {
		return ""
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(line), "FROM ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				image := parts[1]
				// node:18-alpine -> node:18-alpine
				// Sadece ilk FROM'u al (multi-stage build için)
				return image
			}
		}
	}
	return ""
}

// ============================================
// PROJE SAĞLIK SKORU HESAPLAMA
// ============================================

// calculateHealthScore proje sağlık skorunu hesaplar (0-100)
func (s *Scanner) calculateHealthScore(projectPath string, p *domain.Project) {
	// Use the unified HealthService to avoid inconsistencies
	hs := NewHealthService()
	report := hs.CheckProjectHealth(*p)

	p.HealthScore = report.Score
	p.HealthDetails = report.PassedItems
}

// checkCustomRules kullanıcı tanımlı kuralları kontrol eder
func (s *Scanner) checkCustomRules(projectPath string, p *domain.Project) {
	if s.Config == nil || len(s.Config.CustomRules) == 0 {
		return
	}

	for _, rule := range s.Config.CustomRules {
		matched := false

		// 1. Klasör adı kontrolü
		for _, folder := range rule.Folders {
			if strings.EqualFold(filepath.Base(projectPath), folder) {
				matched = true
				break
			}
		}

		// 2. Dosya varlık kontrolü
		if !matched && len(rule.Files) > 0 {
			for _, file := range rule.Files {
				if _, err := os.Stat(filepath.Join(projectPath, file)); err == nil {
					matched = true
					break
				}
			}
		}

		// 3. Dependency kontrolü
		if !matched && len(rule.Dependencies) > 0 {
			pkgPath := filepath.Join(projectPath, "package.json")
			if data, err := os.ReadFile(pkgPath); err == nil {
				var pkg packageJSON
				if err := json.Unmarshal(data, &pkg); err == nil {
					for _, dep := range rule.Dependencies {
						if _, ok := pkg.Dependencies[dep]; ok {
							matched = true
							break
						}
						if _, ok := pkg.DevDependencies[dep]; ok {
							matched = true
							break
						}
					}
				}
			}
		}

		// Eğer kural eşleşti ise, projeye ekle
		if matched {
			customType := domain.ProjectType(rule.Name)
			if rule.Type == "frontend" && !p.HasFrontend {
				p.HasFrontend = true
				p.FrontendType = customType
				p.FrontendVer = "Custom"
				p.FrontendPath = projectPath
				p.FrontendCmd = s.detectStartCommand(projectPath, true, false)
			} else if rule.Type == "backend" && !p.HasBackend {
				p.HasBackend = true
				p.BackendType = customType
				p.BackendVer = "Custom"
				p.BackendPath = projectPath
				p.BackendCmd = s.detectStartCommand(projectPath, false, true)
			}
		}
	}
}
func (s *Scanner) scanMonorepo(projectPath string, p *domain.Project) {
	entries, err := os.ReadDir(projectPath)
	if err != nil {
		return
	}

	frontendSigs := s.getFrontendSignatures()
	backendSigs := s.getBackendSignatures()

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Monorepo klasörlerini bul (apps/, packages/, services/)
		if isMonorepoFolder(entry.Name()) {
			monorepoPath := filepath.Join(projectPath, entry.Name())
			subEntries, err := os.ReadDir(monorepoPath)
			if err != nil {
				continue
			}

			// Her alt klasörü tara
			for _, subEntry := range subEntries {
				if !subEntry.IsDir() {
					continue
				}

				subPath := filepath.Join(monorepoPath, subEntry.Name())
				subName := subEntry.Name()

				// Skip common non-project directories
				name := strings.ToLower(subName)
				if name == "node_modules" || name == ".git" || name == "dist" || name == "build" || name == ".next" {
					continue
				}

				isFrontendHint := isFrontendFolderName(subName)
				isBackendHint := isBackendFolderName(subName)

				// Backend ipucu varsa frontend imzalarıyla karıştırma.
				if isBackendHint {
					foundBackend := false
					for _, sig := range backendSigs {
						if found, ver := sig.CheckFunc(subPath); found {
							subProject := domain.SubProject{
								Name:       subName,
								Path:       subPath,
								Type:       sig.Type,
								Version:    nonEmptyVersion(ver),
								StartCmd:   s.detectStartCommand(subPath, false, true),
								IsFrontend: false,
							}
							s.applyMonorepoRootScript(projectPath, &subProject)
							p.AllBackends = append(p.AllBackends, subProject)
							foundBackend = true
							break
						}
					}
					if !foundBackend {
						if _, err := os.Stat(filepath.Join(subPath, "package.json")); err == nil {
							subProject := domain.SubProject{
								Name:       subName,
								Path:       subPath,
								Type:       domain.TypeUnknown,
								Version:    "Var",
								StartCmd:   s.detectStartCommand(subPath, false, true),
								IsFrontend: false,
							}
							s.applyMonorepoRootScript(projectPath, &subProject)
							p.AllBackends = append(p.AllBackends, subProject)
						}
					}
					continue
				}

				// Frontend ipucu varsa backend imzalarıyla karıştırma.
				if isFrontendHint {
					foundFrontend := false
					for _, sig := range frontendSigs {
						if found, ver := sig.CheckFunc(subPath); found {
							subProject := domain.SubProject{
								Name:       subName,
								Path:       subPath,
								Type:       sig.Type,
								Version:    nonEmptyVersion(ver),
								StartCmd:   s.detectStartCommand(subPath, true, false),
								IsFrontend: true,
							}
							s.applyMonorepoRootScript(projectPath, &subProject)
							p.AllFrontends = append(p.AllFrontends, subProject)
							foundFrontend = true
							break
						}
					}
					if !foundFrontend {
						if _, err := os.Stat(filepath.Join(subPath, "package.json")); err == nil {
							subProject := domain.SubProject{
								Name:       subName,
								Path:       subPath,
								Type:       domain.TypeUnknown,
								Version:    "Var",
								StartCmd:   s.detectStartCommand(subPath, true, false),
								IsFrontend: true,
							}
							s.applyMonorepoRootScript(projectPath, &subProject)
							p.AllFrontends = append(p.AllFrontends, subProject)
						}
					}
					continue
				}

				if subProject, ok := s.detectMonorepoSubProject(subName, subPath, frontendSigs, true); ok {
					s.applyMonorepoRootScript(projectPath, &subProject)
					p.AllFrontends = append(p.AllFrontends, subProject)
				}
				if subProject, ok := s.detectMonorepoSubProject(subName, subPath, backendSigs, false); ok {
					s.applyMonorepoRootScript(projectPath, &subProject)
					p.AllBackends = append(p.AllBackends, subProject)
				}
			}
		}
	}

	// Monorepo olarak işaretle
	if len(p.AllFrontends) > 0 || len(p.AllBackends) > 0 {
		p.IsMonorepo = true

		// Ana frontend/backend'i ayarla (ilk bulunan)
		if len(p.AllFrontends) > 0 && !p.HasFrontend {
			first := p.AllFrontends[0]
			p.HasFrontend = true
			p.FrontendType = first.Type
			p.FrontendVer = first.Version
			p.FrontendPath = subProjectWorkDir(first)
			p.FrontendCmd = first.StartCmd
		}
		if len(p.AllBackends) > 0 && !p.HasBackend {
			first := p.AllBackends[0]
			p.HasBackend = true
			p.BackendType = first.Type
			p.BackendVer = first.Version
			p.BackendPath = subProjectWorkDir(first)
			p.BackendCmd = first.StartCmd
		}
	}
	syncProjectComponents(p)
}

func (s *Scanner) detectMonorepoSubProject(subName, subPath string, sigs []TechSignature, isFrontend bool) (domain.SubProject, bool) {
	for _, sig := range sigs {
		if found, ver := sig.CheckFunc(subPath); found {
			return domain.SubProject{
				Name:       subName,
				Path:       subPath,
				Type:       sig.Type,
				Version:    nonEmptyVersion(ver),
				StartCmd:   s.detectStartCommand(subPath, isFrontend, !isFrontend),
				IsFrontend: isFrontend,
			}, true
		}
	}
	return domain.SubProject{}, false
}

func (s *Scanner) applyMonorepoRootScript(rootPath string, subProject *domain.SubProject) {
	role := domain.ComponentRoleBackend
	if subProject.IsFrontend {
		role = domain.ComponentRoleFrontend
	}
	plan := NewCommandResolver().ResolveStartCommand(CommandResolveRequest{
		RootPath:      rootPath,
		ComponentName: subProject.Name,
		ComponentPath: subProject.Path,
		Role:          role,
	})
	if plan.Command == "" {
		return
	}
	subProject.WorkDir = plan.Cwd
	subProject.StartCmd = plan.Command
}

func (s *Scanner) findMonorepoRootScript(rootPath, subName string, isFrontend bool) string {
	data, err := os.ReadFile(filepath.Join(rootPath, "package.json"))
	if err != nil {
		return ""
	}

	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return ""
	}
	if len(pkg.Scripts) == 0 {
		return ""
	}

	name := strings.ToLower(subName)
	roleNames := []string{name}
	if isFrontend {
		roleNames = append(roleNames, "web", "client", "frontend")
	} else {
		roleNames = append(roleNames, "api", "server", "backend")
	}

	for _, candidate := range monorepoRootScriptCandidates(roleNames) {
		if _, ok := pkg.Scripts[candidate]; ok {
			return candidate
		}
	}

	bestScript := ""
	bestScore := 0
	for scriptName, scriptCmd := range pkg.Scripts {
		score := scoreMonorepoRootScript(scriptName, scriptCmd, name, roleNames)
		if score > bestScore {
			bestScore = score
			bestScript = scriptName
		}
	}
	if bestScore >= 100 {
		return bestScript
	}
	return ""
}

func monorepoRootScriptCandidates(roleNames []string) []string {
	seen := make(map[string]bool)
	var candidates []string
	add := func(scriptName string) {
		if !seen[scriptName] {
			seen[scriptName] = true
			candidates = append(candidates, scriptName)
		}
	}
	for _, name := range roleNames {
		add("dev:" + name)
		add(name + ":dev")
		add("start:" + name)
		add(name + ":start")
		add(name)
	}
	return candidates
}

func scoreMonorepoRootScript(scriptName, scriptCmd, subName string, roleNames []string) int {
	lowerName := strings.ToLower(scriptName)
	lowerCmd := strings.ToLower(scriptCmd)
	score := 0

	if strings.Contains(lowerName, "test") ||
		strings.Contains(lowerName, "lint") ||
		strings.Contains(lowerName, "build") ||
		strings.Contains(lowerName, "typecheck") ||
		strings.Contains(lowerName, "type-check") ||
		strings.Contains(lowerName, "verify") {
		score -= 500
	}

	if lowerName == "dev:"+subName || lowerName == subName+":dev" {
		score += 300
	}
	for _, roleName := range roleNames {
		if lowerName == "dev:"+roleName || lowerName == roleName+":dev" {
			score += 220
		}
		if strings.Contains(lowerName, roleName) && strings.Contains(lowerName, "dev") {
			score += 140
		}
		if strings.Contains(lowerCmd, "apps/"+roleName+"/") ||
			strings.Contains(lowerCmd, "apps\\"+roleName+"\\") ||
			strings.Contains(lowerCmd, "apps/"+roleName+" ") ||
			strings.Contains(lowerCmd, "apps\\"+roleName+" ") {
			score += 160
		}
	}
	if strings.Contains(lowerName, "dev") {
		score += 40
	}
	return score
}

func nonEmptyVersion(version string) string {
	if version == "" {
		return "Var"
	}
	return version
}

func syncProjectComponents(p *domain.Project) {
	p.Components = nil

	addComponent := func(sub domain.SubProject, role domain.ComponentRole, isPrimary bool) {
		analysis := analyzeComponentRole(sub.Path, sub.Name, role)
		if analysis.Role != domain.ComponentRoleUnknown {
			role = analysis.Role
		}
		confidence := analysis.Confidence
		if confidence == 0 {
			confidence = 70
		}
		evidence := analysis.Evidence
		if len(evidence) == 0 {
			evidence = []domain.ComponentEvidence{
				{
					Kind:   "scanner",
					Source: sub.Path,
					Detail: string(role) + " component",
					Score:  confidence,
				},
			}
		}
		envWarnings := componentEnvWarnings(p.Path, sub.Path, role)

		p.Components = append(p.Components, domain.ProjectComponent{
			Name:        sub.Name,
			Path:        sub.Path,
			Role:        role,
			Type:        sub.Type,
			Version:     sub.Version,
			StartCmd:    sub.StartCmd,
			WorkDir:     subProjectWorkDir(sub),
			IsPrimary:   isPrimary,
			Confidence:  confidence,
			Evidence:    evidence,
			EnvWarnings: envWarnings,
		})
	}

	for i, sub := range p.AllFrontends {
		addComponent(sub, domain.ComponentRoleFrontend, i == 0)
	}
	for i, sub := range p.AllBackends {
		addComponent(sub, domain.ComponentRoleBackend, i == 0)
	}

	if len(p.Components) > 0 {
		return
	}

	if p.HasFrontend {
		addComponent(domain.SubProject{
			Name:       componentName(p.Name, p.FrontendPath, "frontend"),
			Path:       p.FrontendPath,
			WorkDir:    p.FrontendPath,
			Type:       p.FrontendType,
			Version:    nonEmptyVersion(p.FrontendVer),
			StartCmd:   p.FrontendCmd,
			IsFrontend: true,
		}, domain.ComponentRoleFrontend, true)
	}
	if p.HasBackend {
		addComponent(domain.SubProject{
			Name:     componentName(p.Name, p.BackendPath, "backend"),
			Path:     p.BackendPath,
			WorkDir:  p.BackendPath,
			Type:     p.BackendType,
			Version:  nonEmptyVersion(p.BackendVer),
			StartCmd: p.BackendCmd,
		}, domain.ComponentRoleBackend, true)
	}
}

func subProjectWorkDir(sub domain.SubProject) string {
	if sub.WorkDir != "" {
		return sub.WorkDir
	}
	return sub.Path
}

func componentEnvWarnings(rootPath, componentPath string, role domain.ComponentRole) []string {
	if role != domain.ComponentRoleBackend {
		return nil
	}
	report := NewEnvResolver().Resolve(rootPath, componentPath)
	var warnings []string
	for _, req := range report.Missing {
		warnings = append(warnings, req.Name+" eksik")
	}
	return warnings
}

func refreshProjectRuntimePorts(p *domain.Project) {
	p.PortWarnings = nil
	p.DependencyPortStatuses = nil
	p.DockerServices = nil

	portInfos := CheckProjectPortsForProject(*p, "full")
	for _, info := range portInfos {
		p.PortWarnings = append(p.PortWarnings, FormatPortWarning(info))
	}

	dependencyPortInfos := CheckProjectDependencyPortsForProject(*p, "full")
	for _, info := range dependencyPortInfos {
		p.DependencyPortStatuses = append(p.DependencyPortStatuses, FormatDependencyPortStatus(info))

		name := strings.TrimSpace(info.Label)
		if name == "" {
			name = "Docker"
		}
		status := "Bekleniyor"
		if info.InUse {
			status = "Hazır"
		}
		p.DockerServices = append(p.DockerServices, domain.DockerServiceStatus{
			Name:      name,
			Port:      info.Port,
			Status:    status,
			Process:   info.Process,
			ProcessID: info.ProcessID,
			InUse:     info.InUse,
		})
	}
}

func componentName(projectName, componentPath, fallback string) string {
	if componentPath == "" {
		return fallback
	}
	name := filepath.Base(componentPath)
	if name == "." || name == string(os.PathSeparator) || name == projectName {
		return fallback
	}
	return name
}

func (s *Scanner) ScanProjects() []domain.Project {
	// Cache Key: Proje yollarının birleşimi
	cacheKey := strings.Join(s.Config.ProjectsPaths, "|")

	// 1. Cache Kontrolü
	if projects, ok := s.loadCache(cacheKey); ok {
		// Cache'den gelse bile sağlık skorunu güncel mantıkla tekrar hesapla
		for i := range projects {
			s.calculateHealthScore(projects[i].Path, &projects[i])
			s.checkTools(projects[i].Path, &projects[i])
			// Scriptleri her zaman taze tut
			projects[i].Scripts = s.scanPackageScripts(&projects[i])
			syncProjectComponents(&projects[i])
			refreshProjectRuntimePorts(&projects[i])
		}

		// Cache'den gelen projeler için de config senkronizasyonu yap
		if s.syncProjectsWithConfig(projects) {
			_ = config.SaveConfig(s.Config)
		}

		return projects
	}

	var allProjects []domain.Project
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Global deduplication map
	seenPaths := make(map[string]bool)
	var mapMu sync.Mutex

	for _, path := range s.Config.ProjectsPaths {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			projects := s.scanDirectory(p)

			mu.Lock()
			for _, proj := range projects {
				mapMu.Lock()
				if !seenPaths[proj.Name] {
					seenPaths[proj.Name] = true
					allProjects = append(allProjects, proj)
				}
				mapMu.Unlock()
			}
			mu.Unlock()
		}(path)
	}
	wg.Wait()

	// 2. Cache Kaydetme
	s.saveCache(cacheKey, allProjects)

	// 3. Global Config Senkronizasyonu
	if s.syncProjectsWithConfig(allProjects) {
		_ = config.SaveConfig(s.Config)
	}

	return allProjects
}

// ClearCache cache dosyasını siler (Manuel yenileme için)
func (s *Scanner) ClearCache() {
	homeDir, _ := os.UserHomeDir()
	cachePath := filepath.Join(homeDir, cacheFileName)
	_ = os.Remove(cachePath)
}

// getFrontendSignatures returns all frontend technology signatures
func (s *Scanner) getFrontendSignatures() []TechSignature {
	return []TechSignature{
		// Next.js
		{Type: domain.TypeNext, IsFrontend: true, CheckFunc: func(path string) (bool, string) {
			ver := s.getPackageVersion(path, "next")
			return ver != "", ver
		}},
		// React
		{Type: domain.TypeReact, IsFrontend: true, CheckFunc: func(path string) (bool, string) {
			// React ama Next değilse
			if s.getPackageVersion(path, "next") != "" {
				return false, ""
			}
			ver := s.getPackageVersion(path, "react")
			return ver != "", ver
		}},
		// Vue
		{Type: domain.TypeVue, IsFrontend: true, CheckFunc: func(path string) (bool, string) {
			ver := s.getPackageVersion(path, "vue")
			return ver != "", ver
		}},
		// Vite (config file check)
		{Type: domain.TypeVite, IsFrontend: true, CheckFunc: func(path string) (bool, string) {
			if _, err := os.Stat(filepath.Join(path, "vite.config.ts")); err == nil {
				return true, "Var"
			}
			if _, err := os.Stat(filepath.Join(path, "vite.config.js")); err == nil {
				return true, "Var"
			}
			return false, ""
		}},
		// React Native
		{Type: domain.TypeReactNative, IsFrontend: true, CheckFunc: func(path string) (bool, string) {
			ver := s.getPackageVersion(path, "react-native")
			return ver != "", ver
		}},
		// Flutter
		{Type: domain.TypeFlutter, IsFrontend: true, CheckFunc: func(path string) (bool, string) {
			if _, err := os.Stat(filepath.Join(path, "pubspec.yaml")); err == nil {
				androidPath := filepath.Join(path, "android")
				iosPath := filepath.Join(path, "ios")
				hasAndroid := false
				hasIOS := false
				if info, err := os.Stat(androidPath); err == nil && info.IsDir() {
					hasAndroid = true
				}
				if info, err := os.Stat(iosPath); err == nil && info.IsDir() {
					hasIOS = true
				}
				if hasAndroid && hasIOS {
					return true, "iOS & Android"
				} else if hasAndroid {
					return true, "Android"
				} else if hasIOS {
					return true, "iOS"
				}
				return true, "Var"
			}
			return false, ""
		}},
		// Mobile (Native - Generic Fallback)
		{Type: domain.TypeMobile, IsFrontend: true, CheckFunc: func(path string) (bool, string) {
			androidPath := filepath.Join(path, "android")
			iosPath := filepath.Join(path, "ios")
			hasAndroid := false
			hasIOS := false
			if info, err := os.Stat(androidPath); err == nil && info.IsDir() {
				hasAndroid = true
			}
			if info, err := os.Stat(iosPath); err == nil && info.IsDir() {
				hasIOS = true
			}
			// Only valid if it's NOT a web project (to avoid false positives in some hybrid structures)
			// But for now, since we reordered checks, this should catch only true native apps
			if hasAndroid && hasIOS {
				return true, "iOS & Android"
			} else if hasAndroid {
				return true, "Android"
			} else if hasIOS {
				return true, "iOS"
			}
			return false, ""
		}},
		// Angular
		{Type: domain.TypeAngular, IsFrontend: true, CheckFunc: func(path string) (bool, string) {
			ver := s.getPackageVersion(path, "@angular/core")
			return ver != "", ver
		}},
		// Svelte
		{Type: domain.TypeSvelte, IsFrontend: true, CheckFunc: func(path string) (bool, string) {
			ver := s.getPackageVersion(path, "svelte")
			return ver != "", ver
		}},
		// SolidJS
		{Type: domain.TypeSolidJS, IsFrontend: true, CheckFunc: func(path string) (bool, string) {
			ver := s.getPackageVersion(path, "solid-js")
			return ver != "", ver
		}},
		// Astro
		{Type: domain.TypeAstro, IsFrontend: true, CheckFunc: func(path string) (bool, string) {
			ver := s.getPackageVersion(path, "astro")
			return ver != "", ver
		}},
		// Remix
		{Type: domain.TypeRemix, IsFrontend: true, CheckFunc: func(path string) (bool, string) {
			ver := s.getPackageVersion(path, "@remix-run/react")
			return ver != "", ver
		}},
		// Nuxt
		{Type: domain.TypeNuxt, IsFrontend: true, CheckFunc: func(path string) (bool, string) {
			ver := s.getPackageVersion(path, "nuxt")
			return ver != "", ver
		}},
		// Flutter signatures moved up

		// Expo
		{Type: domain.TypeExpo, IsFrontend: true, CheckFunc: func(path string) (bool, string) {
			ver := s.getPackageVersion(path, "expo")
			return ver != "", ver
		}},
		// HTML (Static Website)
		{Type: domain.TypeHTML, IsFrontend: true, CheckFunc: func(path string) (bool, string) {
			// Check for index.html - indicates static HTML project
			if _, err := os.Stat(filepath.Join(path, "index.html")); err == nil {
				// Make sure it's not a framework project (no package.json with frameworks)
				if s.getPackageVersion(path, "next") != "" ||
					s.getPackageVersion(path, "react") != "" ||
					s.getPackageVersion(path, "vue") != "" {
					return false, ""
				}
				return true, "Var"
			}
			return false, ""
		}},
		// TypeScript (Standalone TS project)
		{Type: domain.TypeTypeScript, IsFrontend: true, CheckFunc: func(path string) (bool, string) {
			// Check for tsconfig.json - indicates TypeScript project
			if _, err := os.Stat(filepath.Join(path, "tsconfig.json")); err == nil {
				// Make sure it's not a framework project
				if s.getPackageVersion(path, "next") != "" ||
					s.getPackageVersion(path, "react") != "" ||
					s.getPackageVersion(path, "vue") != "" ||
					s.getPackageVersion(path, "@nestjs/core") != "" {
					return false, ""
				}
				return true, "Var"
			}
			return false, ""
		}},
	}
}

// getBackendSignatures returns all backend technology signatures
func (s *Scanner) getBackendSignatures() []TechSignature {
	return []TechSignature{
		// NestJS
		{Type: domain.TypeNest, IsFrontend: false, CheckFunc: func(path string) (bool, string) {
			ver := s.getPackageVersion(path, "@nestjs/core")
			return ver != "", ver
		}},
		// Express
		{Type: domain.TypeExpress, IsFrontend: false, CheckFunc: func(path string) (bool, string) {
			// Express ama Nest değilse
			if s.getPackageVersion(path, "@nestjs/core") != "" {
				return false, ""
			}
			ver := s.getPackageVersion(path, "express")
			return ver != "", ver
		}},
		// Go
		{Type: domain.TypeGo, IsFrontend: false, CheckFunc: func(path string) (bool, string) {
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
				ver := getGoVersion(path)
				if ver == "" {
					ver = "Var"
				}
				return true, ver
			}
			return false, ""
		}},
		// Django
		{Type: domain.TypeDjango, IsFrontend: false, CheckFunc: func(path string) (bool, string) {
			if _, err := os.Stat(filepath.Join(path, "manage.py")); err == nil {
				ver := getPythonVersion(path)
				if ver == "" {
					ver = "Var"
				}
				return true, ver
			}
			return false, ""
		}},
		// Flask
		{Type: domain.TypeFlask, IsFrontend: false, CheckFunc: func(path string) (bool, string) {
			// Check for app.py or wsgi.py
			hasAppPy := false
			if _, err := os.Stat(filepath.Join(path, "app.py")); err == nil {
				hasAppPy = true
			}
			if _, err := os.Stat(filepath.Join(path, "wsgi.py")); err == nil {
				hasAppPy = true
			}
			if !hasAppPy {
				return false, ""
			}
			// Check requirements.txt for flask
			reqPath := filepath.Join(path, "requirements.txt")
			if data, err := os.ReadFile(reqPath); err == nil {
				if strings.Contains(strings.ToLower(string(data)), "flask") {
					ver := getPythonVersion(path)
					if ver == "" {
						ver = "Var"
					}
					return true, ver
				}
			}
			return false, ""
		}},
		// Laravel
		{Type: domain.TypeLaravel, IsFrontend: false, CheckFunc: func(path string) (bool, string) {
			if _, err := os.Stat(filepath.Join(path, "artisan")); err == nil {
				return true, "Var"
			}
			return false, ""
		}},
		// PHP (Generic)
		{Type: domain.TypePHP, IsFrontend: false, CheckFunc: func(path string) (bool, string) {
			// Laravel değilse ve composer.json varsa
			if _, err := os.Stat(filepath.Join(path, "artisan")); err == nil {
				return false, "" // Laravel olarak algılansın
			}
			if _, err := os.Stat(filepath.Join(path, "composer.json")); err == nil {
				return true, "Var"
			}
			return false, ""
		}},
		// Spring (Java)
		{Type: domain.TypeSpring, IsFrontend: false, CheckFunc: func(path string) (bool, string) {
			// Check pom.xml or build.gradle for spring-boot
			pomPath := filepath.Join(path, "pom.xml")
			gradlePath := filepath.Join(path, "build.gradle")
			if data, err := os.ReadFile(pomPath); err == nil {
				if strings.Contains(string(data), "spring-boot") {
					return true, "Var"
				}
			}
			if data, err := os.ReadFile(gradlePath); err == nil {
				if strings.Contains(string(data), "spring-boot") {
					return true, "Var"
				}
			}
			return false, ""
		}},
		// FastAPI (Python)
		{Type: domain.TypeFastAPI, IsFrontend: false, CheckFunc: func(path string) (bool, string) {
			reqPath := filepath.Join(path, "requirements.txt")
			if data, err := os.ReadFile(reqPath); err == nil {
				if strings.Contains(strings.ToLower(string(data)), "fastapi") {
					ver := getPythonVersion(path)
					if ver == "" {
						ver = "Var"
					}
					return true, ver
				}
			}
			// pyproject.toml kontrolü
			pyprojectPath := filepath.Join(path, "pyproject.toml")
			if data, err := os.ReadFile(pyprojectPath); err == nil {
				if strings.Contains(strings.ToLower(string(data)), "fastapi") {
					ver := getPythonVersion(path)
					if ver == "" {
						ver = "Var"
					}
					return true, ver
				}
			}
			return false, ""
		}},
		// Fiber (Go)
		{Type: domain.TypeFiber, IsFrontend: false, CheckFunc: func(path string) (bool, string) {
			goModPath := filepath.Join(path, "go.mod")
			if data, err := os.ReadFile(goModPath); err == nil {
				if strings.Contains(string(data), "github.com/gofiber/fiber") {
					ver := getGoVersion(path)
					if ver == "" {
						ver = "Var"
					}
					return true, ver
				}
			}
			return false, ""
		}},
		// Hono
		{Type: domain.TypeHono, IsFrontend: false, CheckFunc: func(path string) (bool, string) {
			ver := s.getPackageVersion(path, "hono")
			return ver != "", ver
		}},
		// Koa
		{Type: domain.TypeKoa, IsFrontend: false, CheckFunc: func(path string) (bool, string) {
			ver := s.getPackageVersion(path, "koa")
			return ver != "", ver
		}},
	}
}

func (s *Scanner) scanDirectory(root string) []domain.Project {
	var results []domain.Project
	seen := make(map[string]bool)

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		fullPath := filepath.Join(root, entry.Name())
		if seen[fullPath] {
			continue
		}
		seen[fullPath] = true

		p := domain.Project{
			Name: entry.Name(),
			Path: fullPath,
			Type: domain.TypeUnknown,
		}

		// ========================================
		// ADIM 1: Alt klasörleri tara (Signature-Based)
		// ========================================
		s.scanSubdirectories(fullPath, &p)

		// ========================================
		// ADIM 2: Monorepo kontrolü
		// ========================================
		if hasMonorepoStructure(fullPath) {
			s.scanMonorepo(fullPath, &p)
		}

		// ========================================
		// ADIM 3: Root dizini tara (Monorepo olmayan projeler)
		// ========================================
		if !p.HasFrontend && !p.HasBackend {
			s.scanRootDirectory(fullPath, &p)
		}

		// ========================================
		// ADIM 4: Custom Rule Kontrolü
		// ========================================
		s.checkCustomRules(fullPath, &p)

		// ========================================
		// ADIM 5: Tip Belirleme
		// ========================================
		s.determineProjectType(&p)
		syncProjectComponents(&p)

		// ========================================
		// ADIM 6: Sadece proje olarak algılananları ekle
		// ========================================
		isProject := p.HasFrontend || p.HasBackend || p.HasDocker ||
			p.Type != domain.TypeUnknown ||
			p.FrontendType != "" || p.BackendType != ""

		if isProject {
			// Sağlık skoru hesapla
			s.calculateHealthScore(fullPath, &p)

			// Araç kontrolü (Prisma, Drizzle, vb.)
			s.checkTools(fullPath, &p)

			// Yerel config kalıntısı temizliği yapılabilir ama şimdilik gerek yok
			// s.manageProjectConfig(&p) kaldırıldı.

			refreshProjectRuntimePorts(&p)

			// Package Scripts taraması
			p.Scripts = s.scanPackageScripts(&p)

			results = append(results, p)
		}
	}
	return results
}

// scanSubdirectories scans all immediate subdirectories for tech signatures
func (s *Scanner) scanSubdirectories(projectPath string, p *domain.Project) {
	entries, err := os.ReadDir(projectPath)
	if err != nil {
		return
	}

	frontendSigs := s.getFrontendSignatures()
	backendSigs := s.getBackendSignatures()

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		subPath := filepath.Join(projectPath, entry.Name())

		// Skip common non-project directories
		name := strings.ToLower(entry.Name())
		if name == "node_modules" || name == ".git" || name == "dist" || name == "build" || name == ".next" {
			continue
		}

		// Klasör adı ipuçlarını kontrol et
		isFrontendHint := isFrontendFolderName(entry.Name())
		isBackendHint := isBackendFolderName(entry.Name())

		// ========================================================
		// KURAL 1 & 2: Karşılıklı Dışlama ve Erken Durma
		// ========================================================
		// Eğer klasör adı NET bir şekilde backend diyorsa (api, server vb.),
		// içinde frontend aramaya çalışma ve burayı backend olarak işaretle.
		if isBackendHint && !p.HasBackend {
			foundBackend := false
			// Önce backend imzalarını kontrol et
			for _, sig := range backendSigs {
				if found, ver := sig.CheckFunc(subPath); found {
					p.HasBackend = true
					p.BackendPath = subPath
					p.BackendVer = ver
					p.BackendType = sig.Type
					if p.BackendVer == "" {
						p.BackendVer = "Var"
					}
					p.BackendCmd = s.detectStartCommand(subPath, false, true)
					foundBackend = true
					break
				}
			}

			// İmza bulunamasa bile, klasör adı "api" ise ve içinde package.json varsa backend kabul et
			if !foundBackend {
				if _, err := os.Stat(filepath.Join(subPath, "package.json")); err == nil {
					p.HasBackend = true
					p.BackendPath = subPath
					p.BackendVer = "Var"
					p.BackendType = domain.TypeUnknown
					p.BackendCmd = s.detectStartCommand(subPath, false, true)
				}
			}

			// Backend bulunduysa veya bu klasör net backend adayıysa,
			// artık frontend kontrolü yapma ve döngüden çık (continue)
			continue
		}

		// Eğer klasör adı NET bir şekilde frontend diyorsa (web, ui, client vb.),
		// içinde backend aramaya çalışma (Örn: web/app/api klasörü backend sanılmasın diye).
		if isFrontendHint && !p.HasFrontend {
			foundFrontend := false
			for _, sig := range frontendSigs {
				if found, ver := sig.CheckFunc(subPath); found {
					p.HasFrontend = true
					p.FrontendPath = subPath
					p.FrontendVer = ver
					p.FrontendType = sig.Type
					if p.FrontendVer == "" {
						p.FrontendVer = "Var"
					}
					p.FrontendCmd = s.detectStartCommand(subPath, true, false)
					foundFrontend = true
					break
				}
			}

			// İmza bulunamasa bile, klasör adı "web" ise ve içinde package.json varsa frontend kabul et
			if !foundFrontend {
				if _, err := os.Stat(filepath.Join(subPath, "package.json")); err == nil {
					p.HasFrontend = true
					p.FrontendPath = subPath
					p.FrontendVer = "Var"
					p.FrontendType = domain.TypeUnknown
					p.FrontendCmd = s.detectStartCommand(subPath, true, false)
				}
			}

			// Frontend bulunduysa veya bu klasör net frontend adayıysa,
			// backend kontrolü yapma ve devam et
			continue
		}

		// Klasör adı ipucu vermiyorsa, normal (Generic) tarama yap
		if !isFrontendHint && !isBackendHint {
			// Check for ALL Frontend signatures
			for _, sig := range frontendSigs {
				if found, ver := sig.CheckFunc(subPath); found {
					if !p.HasFrontend {
						p.HasFrontend = true
						p.FrontendPath = subPath
						p.FrontendVer = ver
						p.FrontendType = sig.Type
						if p.FrontendVer == "" {
							p.FrontendVer = "Var"
						}
						p.FrontendCmd = s.detectStartCommand(subPath, true, false)
					}
				}
			}

			// Check for ALL Backend signatures
			for _, sig := range backendSigs {
				if found, ver := sig.CheckFunc(subPath); found {
					if !p.HasBackend {
						p.HasBackend = true
						p.BackendPath = subPath
						p.BackendVer = ver
						p.BackendType = sig.Type
						if p.BackendVer == "" {
							p.BackendVer = "Var"
						}
						p.BackendCmd = s.detectStartCommand(subPath, false, true)
					}
				}
			}
		}

		// Docker check
		if _, err := os.Stat(filepath.Join(subPath, "Dockerfile")); err == nil {
			p.HasDocker = true
		}
		if _, err := os.Stat(filepath.Join(subPath, "docker-compose.yml")); err == nil {
			p.HasDocker = true
		}
		if _, err := os.Stat(filepath.Join(subPath, "docker-compose.yaml")); err == nil {
			p.HasDocker = true
		}
	}
}

// scanRootDirectory checks the project root for tech signatures (single-folder projects)
func (s *Scanner) scanRootDirectory(projectPath string, p *domain.Project) {
	frontendSigs := s.getFrontendSignatures()
	backendSigs := s.getBackendSignatures()

	// Check ALL Frontend signatures
	for _, sig := range frontendSigs {
		if found, ver := sig.CheckFunc(projectPath); found {
			if !p.HasFrontend {
				// İlk bulunan ana frontend olsun
				p.HasFrontend = true
				p.FrontendPath = projectPath
				p.FrontendVer = ver
				p.FrontendType = sig.Type
				if p.FrontendVer == "" {
					p.FrontendVer = "Var"
				}
				p.FrontendCmd = s.detectStartCommand(projectPath, true, false)
				p.Type = sig.Type
			} else {
				// Diğerleri ek frontend teknolojileri olarak kaydedilsin
				verToStore := ver
				if verToStore == "" {
					verToStore = "Var"
				}
				p.DetectedFrontendTechs = append(p.DetectedFrontendTechs, domain.DetectedTech{
					Type:    sig.Type,
					Version: verToStore,
				})
			}
		}
	}

	// Check ALL Backend signatures
	for _, sig := range backendSigs {
		if found, ver := sig.CheckFunc(projectPath); found {
			if !p.HasBackend {
				// İlk bulunan ana backend olsun
				p.HasBackend = true
				p.BackendPath = projectPath
				p.BackendVer = ver
				p.BackendType = sig.Type
				if p.BackendVer == "" {
					p.BackendVer = "Var"
				}
				p.BackendCmd = s.detectStartCommand(projectPath, false, true)
				if p.Type == domain.TypeUnknown {
					p.Type = sig.Type
				}
			} else {
				// Diğerleri ek backend teknolojileri olarak kaydedilsin
				verToStore := ver
				if verToStore == "" {
					verToStore = "Var"
				}
				p.DetectedBackendTechs = append(p.DetectedBackendTechs, domain.DetectedTech{
					Type:    sig.Type,
					Version: verToStore,
				})
			}
		}
	}

	// Docker check for root
	if _, err := os.Stat(filepath.Join(projectPath, "Dockerfile")); err == nil {
		p.HasDocker = true
	}
	if _, err := os.Stat(filepath.Join(projectPath, "docker-compose.yml")); err == nil {
		p.HasDocker = true
	}
	if _, err := os.Stat(filepath.Join(projectPath, "docker-compose.yaml")); err == nil {
		p.HasDocker = true
	}
}

// determineProjectType sets the main project type based on detected components
func (s *Scanner) determineProjectType(p *domain.Project) {
	if p.Type != domain.TypeUnknown {
		return // Already set
	}

	// If both frontend and backend exist, keep as Unknown (Fullstack)
	if p.HasFrontend && p.HasBackend {
		p.Type = domain.TypeUnknown
		return
	}

	// Detect from root if still unknown
	if !p.HasFrontend && !p.HasBackend {
		p.Type = domain.TypeUnknown
	}
}

func (s *Scanner) getPackageVersion(path, pkgName string) string {
	pkgPath := filepath.Join(path, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return ""
	}
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return ""
	}

	if v, ok := pkg.Dependencies[pkgName]; ok {
		return strings.Trim(v, "^~")
	}
	if v, ok := pkg.DevDependencies[pkgName]; ok {
		return strings.Trim(v, "^~")
	}
	return ""
}

// detectPackageManager projenin kullandığı paket yöneticisini tespit eder
func (s *Scanner) detectPackageManager(path string) string {
	return detectPackageManagerName(path)
}

// detectStartCommand determines the best command to start the project
func (s *Scanner) detectStartCommand(path string, isFrontend, isBackend bool) string {
	// 0. Special Types (Flutter, Mobile, etc.)
	if _, err := os.Stat(filepath.Join(path, "pubspec.yaml")); err == nil {
		// Flutter project
		return "flutter run"
	}
	// React Native (often has index.js or App.tsx at root/src but package.json handles it usually)
	// But if we want to force specific RN commands:
	if _, err := os.Stat(filepath.Join(path, "node_modules", "react-native")); err == nil {
		// It's a React Native project.
		// Prefer "npm start" (Metro bundler) or platform specific
		// Let the standard package.json parser below decide, usually "start" is present.
	}

	// 1. JS/TS Projects (Next, Nest, React, Vue, etc.)
	pkgPath := filepath.Join(path, "package.json")
	if _, err := os.Stat(pkgPath); err == nil {
		role := domain.ComponentRoleUnknown
		if isFrontend {
			role = domain.ComponentRoleFrontend
		}
		if isBackend {
			role = domain.ComponentRoleBackend
		}
		plan := NewCommandResolver().ResolveStartCommand(CommandResolveRequest{
			RootPath:      path,
			ComponentName: filepath.Base(path),
			ComponentPath: path,
			Role:          role,
		})
		if plan.Command != "" {
			return plan.Command
		}
	}

	// 2. Go Projects
	if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
		if _, err := os.Stat(filepath.Join(path, "main.go")); err == nil {
			return "go run main.go"
		}
		if _, err := os.Stat(filepath.Join(path, "cmd", "server", "main.go")); err == nil {
			return "go run cmd/server/main.go"
		}
		return "go run ."
	}

	// 3. Python Projects
	if _, err := os.Stat(filepath.Join(path, "manage.py")); err == nil {
		return "python manage.py runserver" // Django
	}
	if _, err := os.Stat(filepath.Join(path, "main.py")); err == nil {
		return "python main.py"
	}
	if _, err := os.Stat(filepath.Join(path, "app.py")); err == nil {
		// Check for Uvicorn/FastAPI in this file or deps
		reqPath := filepath.Join(path, "requirements.txt")
		isFastAPI := false
		if data, err := os.ReadFile(reqPath); err == nil {
			content := strings.ToLower(string(data))
			if strings.Contains(content, "uvicorn") || strings.Contains(content, "fastapi") {
				isFastAPI = true
			}
		}

		if isFastAPI {
			return "uvicorn app:app --host 0.0.0.0 --port 8000 --reload"
		}
		return "python app.py" // Flask
	}

	// 3.5 Python FastAPI (main.py case)
	if _, err := os.Stat(filepath.Join(path, "main.py")); err == nil {
		reqPath := filepath.Join(path, "requirements.txt")
		if data, err := os.ReadFile(reqPath); err == nil {
			content := strings.ToLower(string(data))
			if strings.Contains(content, "uvicorn") || strings.Contains(content, "fastapi") {
				// Genellikle app.main:app veya main:app olur
				// Eğer main.py içinde "app =" varsa "main:app"
				if mainData, err := os.ReadFile(filepath.Join(path, "main.py")); err == nil {
					if strings.Contains(string(mainData), "app =") {
						return "uvicorn main:app --host 0.0.0.0 --port 8000 --reload"
					}
				}
				// Belki app klasörü içindedir
				if _, err := os.Stat(filepath.Join(path, "app", "main.py")); err == nil {
					return "uvicorn app.main:app --host 0.0.0.0 --port 8000 --reload"
				}
			}
		}
	}

	// 4. PHP/Laravel
	if _, err := os.Stat(filepath.Join(path, "artisan")); err == nil {
		return "php artisan serve"
	}

	// 5. Java/Spring
	if _, err := os.Stat(filepath.Join(path, "pom.xml")); err == nil {
		return "mvn spring-boot:run"
	}
	if _, err := os.Stat(filepath.Join(path, "build.gradle")); err == nil {
		return "./gradlew bootRun"
	}

	// 6. Docker
	hasCompose := false
	if _, err := os.Stat(filepath.Join(path, "docker-compose.yml")); err == nil {
		hasCompose = true
	} else if _, err := os.Stat(filepath.Join(path, "docker-compose.yaml")); err == nil {
		hasCompose = true
	}

	if hasCompose {
		return "docker-compose up"
	}

	// Default fallback
	return "echo [DevTerminal] Başlatma komutu bulunamadı"
}

// syncProjectsWithConfig updates the global config with detected commands and applies overrides
// Returns true if the config was modified
func (s *Scanner) syncProjectsWithConfig(projects []domain.Project) bool {
	if s.Config.ProjectOverrides == nil {
		s.Config.ProjectOverrides = make(map[string]domain.ProjectOverride)
	}

	modified := false

	for i := range projects {
		p := &projects[i]
		p.FrontendCmd = normalizePackageManagerCommand(p.FrontendCmd)
		p.BackendCmd = normalizePackageManagerCommand(p.BackendCmd)
		// Key'i her zaman normalize et (küçük harf)
		key := strings.ToLower(p.Path)

		override, exists := s.Config.ProjectOverrides[key]
		changed := false

		if exists {
			normalizedFrontend := normalizePackageManagerCommand(override.Frontend)
			if normalizedFrontend != override.Frontend {
				override.Frontend = normalizedFrontend
				changed = true
			}
			normalizedBackend := normalizePackageManagerCommand(override.Backend)
			if normalizedBackend != override.Backend {
				override.Backend = normalizedBackend
				changed = true
			}
			if shouldRepairBackendOverride(*p, override.Backend) {
				override.Backend = p.BackendCmd
				changed = true
			}

			// Override varsa uygula
			if override.Frontend != "" {
				p.FrontendCmd = override.Frontend
			}
			if override.Backend != "" {
				p.BackendCmd = override.Backend
			}

			// Eksik alanları tamamla (Config dosyasını zenginleştir)
			if override.Frontend == "" && p.FrontendCmd != "" {
				override.Frontend = p.FrontendCmd
				changed = true
			}
			if override.Backend == "" && p.BackendCmd != "" {
				override.Backend = p.BackendCmd
				changed = true
			}

			if changed {
				s.Config.ProjectOverrides[key] = override
				modified = true
			}
		} else {
			// Yeni proje, config'e ekle
			if p.FrontendCmd != "" || p.BackendCmd != "" {
				s.Config.ProjectOverrides[key] = domain.ProjectOverride{
					Frontend: p.FrontendCmd,
					Backend:  p.BackendCmd,
				}
				modified = true
			}
		}
	}

	return modified
}

func shouldRepairBackendOverride(p domain.Project, backendOverride string) bool {
	if !p.IsMonorepo || backendOverride == "" || p.BackendCmd == "" {
		return false
	}
	if normalizePackageManagerCommand(backendOverride) == normalizePackageManagerCommand(p.BackendCmd) {
		return false
	}
	overrideScript := packageManagerScriptName(backendOverride)
	detectedScript := packageManagerScriptName(p.BackendCmd)
	if overrideScript != "dev" || detectedScript == "" {
		return false
	}
	return isBackendRoleScript(detectedScript)
}

func isBackendRoleScript(scriptName string) bool {
	name := strings.ToLower(scriptName)
	return name == "dev:api" ||
		name == "api:dev" ||
		name == "dev:server" ||
		name == "server:dev" ||
		name == "dev:backend" ||
		name == "backend:dev" ||
		(strings.Contains(name, "api") && strings.Contains(name, "dev")) ||
		(strings.Contains(name, "server") && strings.Contains(name, "dev")) ||
		(strings.Contains(name, "backend") && strings.Contains(name, "dev"))
}

// checkTools checks for various tools (Prisma, Drizzle, Hasura, Supabase, Storybook)
func (s *Scanner) checkTools(path string, p *domain.Project) {
	// Temizle
	p.PrismaPath = ""
	p.DrizzlePath = ""
	p.HasuraPath = ""
	p.SupabasePath = ""
	p.StorybookPath = ""

	_ = filepath.WalkDir(path, func(fPath string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		name := d.Name()

		// Skip heavy directories
		if d.IsDir() && fPath != path {
			if name == "node_modules" || name == ".git" || name == "vendor" || name == "dist" || name == "build" || name == ".next" {
				return filepath.SkipDir
			}
			// Skip hidden folders except specific tool folders
			if strings.HasPrefix(name, ".") {
				if name != ".config" && name != ".storybook" && name != ".hasura" {
					return filepath.SkipDir
				}
			}
		}

		// Depth check (max 3 levels)
		rel, _ := filepath.Rel(path, fPath)
		depth := strings.Count(rel, string(os.PathSeparator))
		if depth > 3 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		dir := filepath.Dir(fPath)

		// --- Check Dirs ---
		if d.IsDir() {
			if name == ".storybook" && p.StorybookPath == "" {
				p.StorybookPath = dir // .storybook folder is inside the project root usually
				p.HasStorybook = true
			}
			if name == "supabase" && p.SupabasePath == "" {
				p.SupabasePath = dir // run npx from parent of supabase folder
				p.HasSupabase = true
			}
			if name == "hasura" && p.HasuraPath == "" {
				p.HasuraPath = dir // run hasura console from parent
				p.HasHasura = true
			}
		}

		// --- Check Files ---
		if !d.IsDir() {
			// Prisma
			if name == "schema.prisma" && p.PrismaPath == "" {
				// If schema is in prisma/schema.prisma, use parent folder
				if filepath.Base(dir) == "prisma" {
					p.PrismaPath = filepath.Dir(dir)
				} else {
					p.PrismaPath = dir
				}
				p.HasPrisma = true
			}
			// Drizzle
			if strings.HasPrefix(name, "drizzle.config") && p.DrizzlePath == "" {
				p.DrizzlePath = dir
				p.HasDrizzle = true
			}
			// Package.json dependencies
			if name == "package.json" {
				data, err := os.ReadFile(fPath)
				if err == nil {
					var pkg packageJSON
					if json.Unmarshal(data, &pkg) == nil {
						// Check Deps
						for dep := range pkg.Dependencies {
							if strings.Contains(dep, "prisma") && p.PrismaPath == "" {
								p.PrismaPath = dir
								p.HasPrisma = true
							}
							if strings.Contains(dep, "drizzle-orm") && p.DrizzlePath == "" {
								p.DrizzlePath = dir
								p.HasDrizzle = true
							}
						}
						// Check DevDeps
						for dep := range pkg.DevDependencies {
							if strings.Contains(dep, "prisma") && p.PrismaPath == "" {
								p.PrismaPath = dir
								p.HasPrisma = true
							}
							if strings.Contains(dep, "drizzle-orm") && p.DrizzlePath == "" {
								p.DrizzlePath = dir
								p.HasDrizzle = true
							}
							if strings.Contains(dep, "storybook") && p.StorybookPath == "" {
								p.StorybookPath = dir
								p.HasStorybook = true
							}
						}
					}
				}
			}
		}
		return nil
	})
}

// scanPackageScripts reads scripts from package.json
// scanPackageScripts reads scripts from package.json in root, frontend, and backend paths
func (s *Scanner) scanPackageScripts(p *domain.Project) map[string]string {
	scripts := make(map[string]string)

	// Helper to scan a specific path
	scan := func(path string, prefix string) {
		if path == "" {
			return
		}
		pkgPath := filepath.Join(path, "package.json")
		data, err := os.ReadFile(pkgPath)
		if err != nil {
			return
		}

		var pkg packageJSON
		if err := json.Unmarshal(data, &pkg); err != nil {
			return
		}

		for k, v := range pkg.Scripts {
			key := k
			if prefix != "" {
				key = fmt.Sprintf("%s:%s", prefix, k)
			}
			scripts[key] = v
		}
	}

	// 1. Root
	scan(p.Path, "")

	// 2. Frontend (only if different from root)
	if p.FrontendPath != "" && p.FrontendPath != p.Path {
		scan(p.FrontendPath, "client")
	}

	// 3. Backend (only if different from root)
	if p.BackendPath != "" && p.BackendPath != p.Path {
		scan(p.BackendPath, "server")
	}

	return scripts
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
