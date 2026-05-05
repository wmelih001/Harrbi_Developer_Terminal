package domain

import "time"

// Config uygulama konfigürasyonunu tutar
type Config struct {
	ProjectsPaths    []string                   `mapstructure:"projects_paths"`
	Commands         Commands                   `mapstructure:"commands"`
	IgnoredFiles     []string                   `mapstructure:"ignored_files"`
	NgrokPath        string                     `mapstructure:"ngrok_path"`
	CustomRules      []CustomRule               `mapstructure:"custom_rules"`
	ProjectOverrides map[string]ProjectOverride `mapstructure:"project_overrides"`
	LastOpened       map[string]time.Time       `mapstructure:"last_opened"`
}

// ProjectOverride proje bazlı komut özelleştirmelerini tutar
type ProjectOverride struct {
	Frontend string `mapstructure:"frontend"`
	Backend  string `mapstructure:"backend"`
}

// CustomRule kullanıcı tanımlı tespit kuralını temsil eder
type CustomRule struct {
	Name         string   `mapstructure:"name"`         // Kural adı (örn: "My Framework")
	Type         string   `mapstructure:"type"`         // "frontend" veya "backend"
	Folders      []string `mapstructure:"folders"`      // Klasör adı ipuçları
	Files        []string `mapstructure:"files"`        // Dosya varlık kontrolü
	Dependencies []string `mapstructure:"dependencies"` // package.json dependency kontrolü
	Icon         string   `mapstructure:"icon"`         // Özel ikon (opsiyonel)
}

// Commands özel komut şablonlarını tutar
type Commands struct {
	LaunchFrontend string `mapstructure:"launch_frontend"`
	LaunchBackend  string `mapstructure:"launch_backend"`
	LaunchFull     string `mapstructure:"launch_full"`
}

// DetectedTech tespit edilen bir teknolojiyi temsil eder
type DetectedTech struct {
	Type    ProjectType
	Version string
}

// SubProject monorepo içindeki bir alt projeyi temsil eder
type SubProject struct {
	Name       string      // Alt proje adı (klasör adı)
	Path       string      // Alt proje yolu
	WorkDir    string      // Komutun çalışacağı dizin
	Type       ProjectType // Teknoloji tipi
	Version    string      // Versiyon
	StartCmd   string      // Başlatma komutu
	IsFrontend bool        // Frontend mi Backend mi
}

// ComponentRole proje içindeki bileşenin görevini belirtir.
type ComponentRole string

const (
	ComponentRoleFrontend ComponentRole = "frontend"
	ComponentRoleBackend  ComponentRole = "backend"
	ComponentRoleTool     ComponentRole = "tool"
	ComponentRoleLibrary  ComponentRole = "library"
	ComponentRoleUnknown  ComponentRole = "unknown"
)

// ComponentEvidence bileşen tespit kararının dayandığı sinyali temsil eder.
type ComponentEvidence struct {
	Kind   string
	Source string
	Detail string
	Score  int
}

// ProjectComponent frontend, backend, tool veya library gibi proje bileşenlerini temsil eder.
type ProjectComponent struct {
	Name        string
	Path        string
	Role        ComponentRole
	Type        ProjectType
	Version     string
	StartCmd    string
	WorkDir     string
	IsPrimary   bool
	Confidence  int
	Evidence    []ComponentEvidence
	EnvWarnings []string
}

// Project diskteki bir geliştirici projesini temsil eder
type Project struct {
	Name         string
	Path         string
	Type         ProjectType
	Tags         []string
	HasFrontend  bool
	HasBackend   bool
	HasDocker    bool // Docker desteği var mı
	HasPrisma    bool // Prisma ORM desteği var mı
	HasDrizzle   bool // Drizzle ORM desteği var mı
	HasHasura    bool // Hasura desteği var mı
	HasSupabase  bool // Supabase desteği var mı
	HasStorybook bool // Storybook desteği var mı

	// Tool Paths (Sub-directory support)
	PrismaPath    string
	DrizzlePath   string
	HasuraPath    string
	SupabasePath  string
	StorybookPath string

	FrontendVer  string
	BackendVer   string
	FrontendType ProjectType // Algılanan frontend teknolojisi (Next.js, React, Vue vb.)
	BackendType  ProjectType // Algılanan backend teknolojisi (NestJS, Go, Django vb.)
	FrontendCmd  string
	BackendCmd   string
	FrontendPath string
	BackendPath  string
	// Tespit edilen tüm teknolojiler (ana teknoloji hariç diğerleri)
	DetectedFrontendTechs []DetectedTech
	DetectedBackendTechs  []DetectedTech
	// Monorepo desteği
	IsMonorepo   bool         // Monorepo projesi mi?
	AllFrontends []SubProject // Tüm frontend alt projeleri
	AllBackends  []SubProject // Tüm backend alt projeleri
	Components   []ProjectComponent
	// Proje sağlık skoru
	HealthScore   int      // 0-100 arası sağlık puanı
	HealthDetails []string // Sağlık skoru detayları (hangi kriterler var/yok)
	// Port uyarıları
	PortWarnings []string // Kullanımda olan portlar

	// Package Scripts
	Scripts map[string]string // package.json scripts (key: script name, value: command)
}

// ProjectType teknoloji yığınını tanımlar
type ProjectType string

const (
	// Frontend
	TypeReact       ProjectType = "React"
	TypeNext        ProjectType = "Next.js"
	TypeVue         ProjectType = "Vue"
	TypeVite        ProjectType = "Vite"
	TypeReactNative ProjectType = "React Native"
	TypeMobile      ProjectType = "Mobile"
	TypeHTML        ProjectType = "HTML"
	TypeTypeScript  ProjectType = "TypeScript"
	TypeAngular     ProjectType = "Angular"
	TypeSvelte      ProjectType = "Svelte"
	TypeSolidJS     ProjectType = "SolidJS"
	TypeAstro       ProjectType = "Astro"
	TypeRemix       ProjectType = "Remix"
	TypeNuxt        ProjectType = "Nuxt"

	// Backend
	TypeNest    ProjectType = "NestJS"
	TypeExpress ProjectType = "Express"
	TypeGo      ProjectType = "Go"
	TypeDjango  ProjectType = "Django"
	TypeFlask   ProjectType = "Flask"
	TypeLaravel ProjectType = "Laravel"
	TypeSpring  ProjectType = "Spring"
	TypePHP     ProjectType = "PHP"
	TypeFastAPI ProjectType = "FastAPI"
	TypeFiber   ProjectType = "Fiber"
	TypeHono    ProjectType = "Hono"
	TypeKoa     ProjectType = "Koa"

	// Mobile
	TypeFlutter ProjectType = "Flutter"
	TypeExpo    ProjectType = "Expo"

	// Infrastructure
	TypeDocker ProjectType = "Docker"

	// Unknown
	TypeUnknown ProjectType = "Bilinmeyen"
)
