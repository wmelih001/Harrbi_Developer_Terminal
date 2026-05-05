# DEVELOPER TERMINAL //_

![Developer Terminal Banner](assets/banner.gif)

<div align="center">

[![Go Version](https://img.shields.io/badge/go-1.21+-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue?style=for-the-badge&color=7b2cbf)](LICENSE)
[![Release](https://img.shields.io/github/v/release/wmelih001/developer_terminal?style=for-the-badge&color=f72585)](https://github.com/wmelih001/developer_terminal/releases/latest)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-green?style=for-the-badge&color=4cc9f0)](http://makeapullrequest.com)

**Eski PowerShell betiklerinizi bir kenara bırakın.**
Modern ve klavye odaklı yeni nesil Geliştirici Kontrol Paneli.

[Özellikler](#-özellikler) • [Kurulum](#-kurulum) • [Yapılandırma](#yapilandirma) • [Teknoloji](#-teknoloji-yığını)

</div>

---

## <img src="assets/icons/info.png" width="25"> Nedir?

**Developer Terminal**, Go ve Bubble Tea ile geliştirilmiş yüksek performanslı bir CLI aracıdır. Geliştirme ortamınızı tek bir merkezden yönetmenizi sağlar; proje başlatmaktan veritabanı araçlarına, port yönetiminden AI bağlamı oluşturmaya kadar her şey parmaklarınızın ucunda.

## <img src="assets/icons/features.png" width="25"> Özellikler

### <img src="assets/icons/launch.png" width="20"> Akıllı Proje Başlatıcı
Çalışma alanınızı saniyeler içinde tarar. Tek tuşla projelerinizi Windows Terminal sekmelerinde başlatır.
- **Component-Aware Algılama:** `api`, `web`, `apps/*` gibi yapıları frontend/backend bileşeni olarak tanır.
- **Akıllı Komut Seçimi:** Script adı, script içeriği, klasör adı ve role bilgisiyle doğru start komutunu seçer.
- **Full Stack Modu:** Terminali ikiye bölerek hem client hem server'ı aynı anda kaldırır.

### <img src="assets/icons/script.png" width="20"> Script & Task Runner
`scripts` karmaşasına son.
- **Auto-Discovery:** Projenizdeki tüm scriptleri (`dev`, `lint`, `test`) otomatik listeler.
- **Context-Aware:** `framework/client` veya `framework/server` gibi alt dizinlerdeki scriptleri algılar ve doğru dizinde çalıştırır.
- **Hızlı Arama:** `Tab` tuşu ile yüzlerce script arasında anında filtreleme yapın.

### <img src="assets/icons/ai.png" width="20"> AI Context Generator
LLM'ler (ChatGPT, Claude) için kod tabanınızı hazırlayın.
- `.gitignore` kurallarına sadık kalarak temiz bir ASCII ağacı oluşturur.
- Çıktıyı anında panoya kopyalar, prompt'unuza yapıştırmaya hazırdır.

### <img src="assets/icons/health.png" width="20"> Proje Sağlık Merkezi
- **Bağımlılık Doktoru:** Root, frontend ve backend bileşenleri için ayrı dependency kontrolü yapar.
- **Package Manager Desteği:** `npm`, `pnpm`, `yarn`, `bun` ve gerektiğinde `corepack` fallback kullanır.
- **Sağlık Skoru:** Genel proje, frontend, backend ve monorepo kriterlerine göre 100 üzerinden puanlar.

### <img src="assets/icons/shield.png" width="20"> Port & Tünel Yönetimi
- **Dinamik Port Kontrolü:** Script, env, framework config ve docker-compose içinden portları çıkarır.
- **Ngrok Entegrasyonu:** Tünel durumunu ve public URL'inizi doğrudan panodan izleyin.

### <img src="assets/icons/tools.png" width="20"> Gömülü Geliştirici Araçları
Veritabanı ve UI araçlarınıza `F` tuşlarıyla erişin.
| Tuş | Araç | Açıklama |
| :--- | :--- | :--- |
| `F1` | **Prisma Studio** | Veritabanı GUI |
| `F2` | **Drizzle Studio** | Drizzle ORM Yönetimi |
| `F3` | **Hasura Console** | GraphQL Konsolu |
| `F4` | **Supabase** | Yerel Durum Kontrolü |
| `F5` | **Storybook** | UI Bileşen Geliştirme |

## <img src="assets/icons/install.png" width="25"> Kurulum

### Ön Gereksinimler
- **Go** 1.21+
- **Windows Terminal + Powershell 7+** (Önerilen)
- **Nerd Fonts** (İkon desteği için)

### Hızlı Kurulum

```bash
# Projeyi klonlayın
git clone https://github.com/wmelih001/developer_terminal.git

# Dizin değiştirin
cd developer_terminal

# Derleyin ve kurun
go install
```

<a id="yapilandirma"></a>
## <img src="assets/icons/settings.png" width="25"> Yapılandırma

İlk çalıştırmada Developer Terminal sizi karşılar ve yapılandırmayı **otomatik** oluşturur.
Manuel düzenleme için `~/.devterminal/config.yaml` dosyasını kullanabilirsiniz.

```yaml
# Örnek config.yaml
projects_paths:
  - M:\Projeler  # Projelerinizin ana dizini

ignored_files:
  - .git
  - node_modules

# Opsiyonel: Ngrok CLI yolu
ngrok_path: C:\Users\Kullanici\AppData\Local\Microsoft\WinGet\Links\ngrok.exe

# Otomatik oluşturulan proje ayarları
project_overrides:
  m:\projeler\developer_terminal:
    frontend: ""
    backend: go run .
  m:\projeler\nextjs-app:
    frontend: npm run dev
    backend: npm run start:dev

# Son açılan projeler (Otomatik güncellenir)
last_opened:
  m:\projeler\developer_terminal: 2026-01-12T22:06:11.3039435+03:00
```

## <img src="assets/icons/tech.png" width="25"> Tech Stack

<div align="center">

| Çekirdek | UI / TUI | Yapılandırma |
| :---: | :---: | :---: |
| **Go (Golang)** | **Bubble Tea** & **Lipgloss** | **Viper** |
| ![Go](assets/icons/go.png) | ![TUI](assets/icons/tui.png) | ![Config](assets/icons/config.png) |

</div>

## <img src="assets/icons/contribute.png" width="25"> Katkıda Bulunma

Her türlü katkıya açığız! Bir hata bulduysanız veya yeni bir özellik eklemek istiyorsanız lütfen [Issue](https://github.com/wmelih001/developer_terminal/issues) açın veya [Pull Request](https://github.com/wmelih001/developer_terminal/pulls) gönderin.

---

<div align="center">

*Developer Terminal, geliştiriciler tarafından geliştiriciler için yapıldı.* ❤️

</div>
