package service

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"devterminal/pkg/domain"

	"gopkg.in/yaml.v3"
)

type Doctor struct {
	Config *domain.Config
}

func NewDoctor(cfg *domain.Config) *Doctor {
	return &Doctor{Config: cfg}
}

type NpmOutdatedResult map[string]struct {
	Current string `json:"current"`
	Wanted  string `json:"wanted"`
	Latest  string `json:"latest"`
	Kind    string `json:"kind"` // "direct", "dev"
}

// CheckDependencies runs package-manager-specific dependency checks in the project path.
func (d *Doctor) CheckDependencies(p *domain.Project) (NpmOutdatedResult, error) {
	// 1. Flutter Check
	if p.Type == domain.TypeFlutter || (p.FrontendType == domain.TypeFlutter) {
		// Use FrontendPath which points to the directory containing pubspec.yaml
		targetPath := p.FrontendPath
		if targetPath == "" {
			targetPath = p.Path
		}
		return d.checkFlutterDependencies(targetPath)
	}

	// 2. Node.js Check (Default)
	targetPath := d.nodeDependencyPath(p)
	return d.checkNodeDependencies(targetPath)
}

func (d *Doctor) checkFlutterDependencies(path string) (NpmOutdatedResult, error) {
	// pubspec.yaml kontrolü
	if _, err := os.Stat(filepath.Join(path, "pubspec.yaml")); os.IsNotExist(err) {
		return nil, fmt.Errorf("pubspec.yaml bulunamadı")
	}

	// 1. Get ALL installed packages using `flutter pub deps --json`
	depsCmd := exec.Command("flutter", "pub", "deps", "--json")
	depsCmd.Dir = path
	depsOutput, err := depsCmd.CombinedOutput()
	if err != nil {
		// Fallback: Parse pubspec.yaml directly if pub deps fails
		// This allows us to at least list the packages so user can try to update them to fix conflicts
		return d.parsePubspec(path)
	}

	type FlutterPubDeps struct {
		Root     string `json:"root"`
		Packages []struct {
			Name         string   `json:"name"`
			Version      string   `json:"version"`
			Kind         string   `json:"kind"` // "root", "direct", "transitive", "dev"
			Dependencies []string `json:"dependencies"`
		} `json:"packages"`
	}

	var deps FlutterPubDeps
	if err := json.Unmarshal(depsOutput, &deps); err != nil {
		return nil, fmt.Errorf("flutter pub deps çıktısı parse edilemedi: %v", err)
	}

	// 2. Run flutter pub outdated
	cmd := exec.Command("flutter", "pub", "outdated", "--json")
	cmd.Dir = path
	output, _ := cmd.CombinedOutput() // Ignore error

	var outdatedRes struct {
		Packages []struct {
			Package string `json:"package"`
			Current struct {
				Version string `json:"version"`
			} `json:"current"`
			Upgradable struct {
				Version string `json:"version"`
			} `json:"upgradable"`
			Latest struct {
				Version string `json:"version"`
			} `json:"latest"`
		} `json:"packages"`
	}
	_ = json.Unmarshal(output, &outdatedRes)

	// 3. Prepare Final Result
	finalRes := make(NpmOutdatedResult)

	outdatedMap := make(map[string]struct {
		Wanted string
		Latest string
	})
	for _, pkg := range outdatedRes.Packages {
		outdatedMap[pkg.Package] = struct {
			Wanted string
			Latest string
		}{
			Wanted: pkg.Upgradable.Version,
			Latest: pkg.Latest.Version,
		}
	}

	// 4. Iterate over ALL properties
	for _, pkg := range deps.Packages {
		if pkg.Kind == "root" || pkg.Kind == "transitive" {
			continue
		}

		current := pkg.Version
		wanted := current
		latest := current

		if info, ok := outdatedMap[pkg.Name]; ok {
			wanted = info.Wanted
			latest = info.Latest
		}

		if current == "" {
			current = "?"
		}
		if latest == "" {
			latest = current
		}
		if wanted == "" {
			wanted = current
		}

		finalRes[pkg.Name] = struct {
			Current string `json:"current"`
			Wanted  string `json:"wanted"`
			Latest  string `json:"latest"`
			Kind    string `json:"kind"`
		}{
			Current: current,
			Wanted:  wanted,
			Latest:  latest,
			Kind:    pkg.Kind,
		}
	}

	return finalRes, nil
}

func (d *Doctor) checkNodeDependencies(path string) (NpmOutdatedResult, error) {
	packageJSONPath := filepath.Join(path, "package.json")
	if _, err := os.Stat(packageJSONPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("package.json bulunamadı. Bu proje bir Node.js projesi değil")
	}

	manager := detectPackageManagerName(path)
	finalRes, err := dependenciesFromPackageJSON(path)
	if err != nil {
		return nil, err
	}

	if listCmd := d.packageManagerListCmd(manager, path); listCmd != nil {
		if output, err := listCmd.CombinedOutput(); err == nil && len(output) > 0 {
			mergePackageListOutput(finalRes, output)
		}
	}

	if outdatedCmd := d.packageManagerOutdatedCmd(manager, path); outdatedCmd != nil {
		output, _ := outdatedCmd.CombinedOutput()
		if len(output) > 0 {
			mergeOutdatedOutput(finalRes, manager, output)
		}
	}

	return finalRes, nil
}

func (d *Doctor) nodeDependencyPath(p *domain.Project) string {
	candidates := []string{p.Path, p.BackendPath, p.FrontendPath}
	for _, path := range candidates {
		if path == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(path, "package.json")); err == nil {
			return path
		}
	}
	return p.Path
}

func (d *Doctor) packageManagerListCmd(manager, path string) *exec.Cmd {
	switch manager {
	case packageManagerNpm, packageManagerPnpm:
		cmd := newPackageManagerCmd(manager, "list", "--depth=0", "--json")
		cmd.Dir = path
		return cmd
	default:
		return nil
	}
}

func (d *Doctor) packageManagerOutdatedCmd(manager, path string) *exec.Cmd {
	var cmd *exec.Cmd
	switch manager {
	case packageManagerPnpm:
		cmd = newPackageManagerCmd(manager, "outdated", "--format", "json")
	case packageManagerYarn:
		cmd = newPackageManagerCmd(manager, "outdated", "--json")
	case packageManagerBun:
		cmd = newPackageManagerCmd(manager, "outdated")
	default:
		cmd = newPackageManagerCmd(packageManagerNpm, "outdated", "--json")
	}
	cmd.Dir = path
	return cmd
}

func (d *Doctor) nodeUpdateCommand(path string, pkgs []string) (*exec.Cmd, []string) {
	manager := detectPackageManagerName(path)
	var args []string
	var finalPkgs []string

	switch manager {
	case packageManagerPnpm:
		args = []string{"update", "--latest"}
		for _, pkg := range pkgs {
			args = append(args, pkg)
			finalPkgs = append(finalPkgs, pkg)
		}
	case packageManagerYarn:
		if isModernYarnProject(path) {
			args = []string{"up"}
			if len(pkgs) == 0 {
				args = append(args, "*")
			}
			for _, pkg := range pkgs {
				args = append(args, pkg)
				finalPkgs = append(finalPkgs, pkg)
			}
		} else {
			args = []string{"upgrade", "--latest"}
			for _, pkg := range pkgs {
				args = append(args, pkg)
				finalPkgs = append(finalPkgs, pkg)
			}
		}
	case packageManagerBun:
		args = []string{"update", "--latest"}
		for _, pkg := range pkgs {
			args = append(args, pkg)
			finalPkgs = append(finalPkgs, pkg)
		}
	default:
		if len(pkgs) > 0 {
			args = []string{"install"}
			for _, pkg := range pkgs {
				args = append(args, pkg+"@latest")
				finalPkgs = append(finalPkgs, pkg)
			}
		} else {
			args = []string{"update"}
		}
	}

	cmd := newPackageManagerCmd(manager, args...)
	cmd.Dir = path
	return cmd, finalPkgs
}

func isModernYarnProject(path string) bool {
	data, err := os.ReadFile(filepath.Join(path, "package.json"))
	if err != nil {
		return false
	}
	var pkg struct {
		PackageManager string `json:"packageManager"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return false
	}
	value := strings.ToLower(strings.TrimSpace(pkg.PackageManager))
	if !strings.HasPrefix(value, packageManagerYarn+"@") {
		return false
	}
	version := strings.TrimPrefix(value, packageManagerYarn+"@")
	major := strings.SplitN(version, ".", 2)[0]
	return major != "" && major != "0" && major != "1"
}

func newPackageManagerCmd(manager string, args ...string) *exec.Cmd {
	parts := parseArgs(packageManagerCommand(manager))
	parts = append(parts, args...)
	return exec.Command(parts[0], parts[1:]...)
}

func dependenciesFromPackageJSON(path string) (NpmOutdatedResult, error) {
	data, err := os.ReadFile(filepath.Join(path, "package.json"))
	if err != nil {
		return nil, fmt.Errorf("package.json okunamadı: %v", err)
	}

	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("package.json parse edilemedi: %v", err)
	}

	result := make(NpmOutdatedResult)
	for name, version := range pkg.Dependencies {
		setPackageInfo(result, name, cleanDependencySpec(version), "", "", "direct")
	}
	for name, version := range pkg.DevDependencies {
		setPackageInfo(result, name, cleanDependencySpec(version), "", "", "dev")
	}
	for name, version := range pkg.OptionalDependencies {
		setPackageInfo(result, name, cleanDependencySpec(version), "", "", "optional")
	}
	return result, nil
}

func mergePackageListOutput(result NpmOutdatedResult, output []byte) {
	type dep struct {
		Version string `json:"version"`
	}
	type listResult struct {
		Dependencies         map[string]dep `json:"dependencies"`
		DevDependencies      map[string]dep `json:"devDependencies"`
		OptionalDependencies map[string]dep `json:"optionalDependencies"`
	}

	merge := func(list listResult) {
		for name, info := range list.Dependencies {
			setPackageInfo(result, name, info.Version, "", "", "direct")
		}
		for name, info := range list.DevDependencies {
			setPackageInfo(result, name, info.Version, "", "", "dev")
		}
		for name, info := range list.OptionalDependencies {
			setPackageInfo(result, name, info.Version, "", "", "optional")
		}
	}

	var single listResult
	if err := json.Unmarshal(output, &single); err == nil {
		merge(single)
	}

	var many []listResult
	if err := json.Unmarshal(output, &many); err == nil {
		for _, item := range many {
			merge(item)
		}
	}
}

func mergeOutdatedOutput(result NpmOutdatedResult, manager string, output []byte) {
	switch manager {
	case packageManagerYarn:
		if mergeYarnOutdatedJSON(result, output) {
			return
		}
	case packageManagerBun:
		if mergeBunOutdatedTable(result, output) {
			return
		}
	}
	if mergeGenericOutdatedJSON(result, output) {
		return
	}
	mergeGenericOutdatedTable(result, output)
}

func mergeGenericOutdatedJSON(result NpmOutdatedResult, output []byte) bool {
	var keyed map[string]map[string]interface{}
	if err := json.Unmarshal(output, &keyed); err == nil && len(keyed) > 0 {
		for name, info := range keyed {
			setPackageInfo(
				result,
				firstNonEmptyString(name, valueString(info["package"], info["name"], info["packageName"])),
				valueString(info["current"]),
				valueString(info["wanted"], info["update"], info["upgradable"]),
				valueString(info["latest"]),
				dependencyKind(valueString(info["kind"], info["type"], info["dependencyType"], info["packageType"])),
			)
		}
		return true
	}

	var list []map[string]interface{}
	if err := json.Unmarshal(output, &list); err == nil && len(list) > 0 {
		for _, info := range list {
			setPackageInfo(
				result,
				valueString(info["name"], info["package"], info["packageName"]),
				valueString(info["current"]),
				valueString(info["wanted"], info["update"], info["upgradable"]),
				valueString(info["latest"]),
				dependencyKind(valueString(info["kind"], info["type"], info["dependencyType"], info["packageType"])),
			)
		}
		return true
	}

	return false
}

func mergeYarnOutdatedJSON(result NpmOutdatedResult, output []byte) bool {
	merged := false
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event struct {
			Type string `json:"type"`
			Data struct {
				Body [][]string `json:"body"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil || event.Type != "table" {
			continue
		}
		for _, row := range event.Data.Body {
			if len(row) < 4 {
				continue
			}
			kind := ""
			if len(row) >= 5 {
				kind = dependencyKind(row[4])
			}
			setPackageInfo(result, row[0], row[1], row[2], row[3], kind)
			merged = true
		}
	}
	return merged
}

func mergeBunOutdatedTable(result NpmOutdatedResult, output []byte) bool {
	merged := false
	for _, line := range strings.Split(stripANSI(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "|") || strings.Contains(line, "---") || strings.Contains(line, "Package") {
			continue
		}
		parts := splitTableLine(line)
		if len(parts) < 4 {
			continue
		}
		name := parts[0]
		kind := "direct"
		if strings.Contains(name, "(dev)") {
			kind = "dev"
			name = strings.TrimSpace(strings.ReplaceAll(name, "(dev)", ""))
		}
		setPackageInfo(result, name, parts[1], parts[2], parts[3], kind)
		merged = true
	}
	return merged
}

func mergeGenericOutdatedTable(result NpmOutdatedResult, output []byte) {
	for _, line := range strings.Split(stripANSI(string(output)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || strings.EqualFold(fields[0], "Package") {
			continue
		}
		kind := ""
		if len(fields) >= 5 {
			kind = dependencyKind(fields[4])
		}
		setPackageInfo(result, fields[0], fields[1], fields[2], fields[3], kind)
	}
}

func splitTableLine(line string) []string {
	raw := strings.Split(line, "|")
	var parts []string
	for _, part := range raw {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func setPackageInfo(result NpmOutdatedResult, name, current, wanted, latest, kind string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}

	existing := result[name]
	if current == "" {
		current = existing.Current
	}
	if wanted == "" {
		wanted = existing.Wanted
	}
	if latest == "" {
		latest = existing.Latest
	}
	if kind == "" {
		kind = existing.Kind
	}
	if current == "" {
		current = "?"
	}
	if wanted == "" {
		wanted = current
	}
	if latest == "" {
		latest = wanted
	}
	if kind == "" {
		kind = "direct"
	}

	result[name] = struct {
		Current string `json:"current"`
		Wanted  string `json:"wanted"`
		Latest  string `json:"latest"`
		Kind    string `json:"kind"`
	}{
		Current: current,
		Wanted:  wanted,
		Latest:  latest,
		Kind:    kind,
	}
}

func cleanDependencySpec(spec string) string {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "?"
	}
	lower := strings.ToLower(spec)
	if strings.HasPrefix(lower, "workspace:") ||
		strings.HasPrefix(lower, "file:") ||
		strings.HasPrefix(lower, "link:") ||
		strings.HasPrefix(lower, "portal:") ||
		strings.HasPrefix(lower, "git") ||
		strings.Contains(lower, "github:") {
		return spec
	}
	return strings.TrimLeft(spec, "^~>=< ")
}

func dependencyKind(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case "dev", "devdependency", "devdependencies":
		return "dev"
	case "optional", "optionaldependency", "optionaldependencies":
		return "optional"
	case "dependencies", "dependency", "prod", "production", "direct":
		return "direct"
	default:
		return kind
	}
}

func valueString(values ...interface{}) string {
	for _, value := range values {
		switch v := value.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		case map[string]interface{}:
			if s := valueString(v["version"]); s != "" {
				return s
			}
		}
	}
	return ""
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func stripANSI(input string) string {
	var b strings.Builder
	inEscape := false
	for i := 0; i < len(input); i++ {
		c := input[i]
		if inEscape {
			if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
				inEscape = false
			}
			continue
		}
		if c == 0x1b {
			inEscape = true
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func (d *Doctor) parsePubspec(path string) (NpmOutdatedResult, error) {
	pubspecPath := filepath.Join(path, "pubspec.yaml")
	data, err := os.ReadFile(pubspecPath)
	if err != nil {
		return nil, fmt.Errorf("pubspec.yaml okunamadı: %v", err)
	}

	type Pubspec struct {
		Dependencies    map[string]interface{} `yaml:"dependencies"`
		DevDependencies map[string]interface{} `yaml:"dev_dependencies"`
	}

	var p Pubspec
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("pubspec.yaml parse edilemedi: %v", err)
	}

	finalRes := make(NpmOutdatedResult)

	// Add dependencies
	for pkg, v := range p.Dependencies {
		if pkg == "flutter" {
			continue
		}

		version := "?"
		if s, ok := v.(string); ok {
			version = s
		} else if m, ok := v.(map[string]interface{}); ok {
			// Handle complex dependencies (git, sdk, path)
			if _, ok := m["sdk"]; ok {
				version = "sdk"
			} else if _, ok := m["path"]; ok {
				version = "path"
			} else if _, ok := m["git"]; ok {
				version = "git"
			}
		}

		finalRes[pkg] = struct {
			Current string `json:"current"`
			Wanted  string `json:"wanted"`
			Latest  string `json:"latest"`
			Kind    string `json:"kind"`
		}{
			Current: version,
			Wanted:  "?",
			Latest:  "?",
			Kind:    "direct",
		}
	}

	// Add dev_dependencies
	for pkg, v := range p.DevDependencies {
		if pkg == "flutter_test" || pkg == "flutter_driver" || pkg == "integration_test" {
			continue
		}

		version := "?"
		if s, ok := v.(string); ok {
			version = s
		}

		finalRes[pkg] = struct {
			Current string `json:"current"`
			Wanted  string `json:"wanted"`
			Latest  string `json:"latest"`
			Kind    string `json:"kind"`
		}{
			Current: version,
			Wanted:  "?",
			Latest:  "?",
			Kind:    "dev",
		}
	}

	return finalRes, nil
}

// GetUpdateCommands prepares the commands to update dependencies but does not execute them.
// It returns the commands to be executed and the list of packages that will be updated (filtered).
func (d *Doctor) GetUpdateCommands(p *domain.Project, pkgs []string) ([]*exec.Cmd, []string, error) {
	isFlutter := p.Type == domain.TypeFlutter || (p.FrontendType == domain.TypeFlutter)

	// Determine correct path
	targetPath := p.Path
	if isFlutter {
		if p.FrontendPath != "" {
			targetPath = p.FrontendPath
		}
	} else {
		targetPath = d.nodeDependencyPath(p)
	}

	var commands []*exec.Cmd
	var finalPkgs []string

	if isFlutter {
		if len(pkgs) > 0 {
			// Intelligent Update:
			// We merge Prod and Dev packages into one command to allow solver to resolve mutual constraints
			// Merge Prod and Dev packages into one command to allow solver to resolve mutual constraints
			// (e.g. updating analyzer and retrofit_generator together)
			var allPkgs []string

			for _, pkgName := range pkgs {
				// Filter SDK packages manually
				if pkgName == "flutter" || pkgName == "flutter_test" || pkgName == "flutter_driver" || pkgName == "integration_test" {
					continue
				}
				allPkgs = append(allPkgs, pkgName)
			}

			if len(allPkgs) > 0 {
				args := []string{"pub", "upgrade", "--major-versions"}
				args = append(args, allPkgs...)
				cmd := exec.Command("flutter", args...)
				cmd.Dir = targetPath
				commands = append(commands, cmd)
				finalPkgs = append(finalPkgs, allPkgs...)
			}
		} else {
			// Fallback: update all compatible
			cmd := exec.Command("flutter", "pub", "upgrade", "--major-versions")
			cmd.Dir = targetPath
			commands = append(commands, cmd)
			// No specific packages to list
		}
	} else {
		// Node.js or Go
		// Check for go.mod
		goMod := filepath.Join(targetPath, "go.mod")
		if _, err := os.Stat(goMod); err == nil {
			// Go Project
			goCmd := "go"
			var cmd *exec.Cmd
			if len(pkgs) > 0 {
				args := []string{"get"}
				for _, pkg := range pkgs {
					args = append(args, pkg) // go get pkg (updates to latest compatible or major if specified?)
					// usually 'go get pkg@latest' is better
					// But we need to handle versions. For now use @latest
					// args[i] = pkg + "@latest" -> No, append new arg
				}
				// Reset args to implement @latest loop properly
				args = []string{"get"}
				for _, pkg := range pkgs {
					args = append(args, pkg+"@latest")
					finalPkgs = append(finalPkgs, pkg)
				}
				cmd = exec.Command(goCmd, args...)
			} else {
				// Update all? 'go get -u ./...'
				cmd = exec.Command(goCmd, "get", "-u", "./...")
			}
			cmd.Dir = targetPath
			commands = append(commands, cmd)
		} else {
			cmd, updatedPkgs := d.nodeUpdateCommand(targetPath, pkgs)
			cmd.Dir = targetPath
			commands = append(commands, cmd)
			finalPkgs = append(finalPkgs, updatedPkgs...)
		}
	}

	return commands, finalPkgs, nil
}

// UpdatePackages updates the specified packages (or all if pkgs is empty)
func (d *Doctor) UpdatePackages(p *domain.Project, pkgs []string) error {
	isFlutter := p.Type == domain.TypeFlutter || (p.FrontendType == domain.TypeFlutter)

	// Determine correct path
	targetPath := p.Path

	// Flutter Specific
	if isFlutter {
		if p.FrontendPath != "" {
			targetPath = p.FrontendPath
		}
	} else {
		targetPath = d.nodeDependencyPath(p)
	}

	var cmd *exec.Cmd

	if isFlutter {
		if len(pkgs) > 0 {
			// Intelligent Update:
			// 1. Analyze current dependencies to identify Kind (direct vs dev)
			deps, err := d.CheckDependencies(p)
			if err != nil {
				return fmt.Errorf("bağımlılık analizi yapılamadı: %v", err)
			}

			var prodPkgs []string
			var devPkgs []string

			for _, pkgName := range pkgs {
				// Filter SDK packages manually
				if pkgName == "flutter" || pkgName == "flutter_test" || pkgName == "flutter_driver" || pkgName == "integration_test" {
					continue
				}

				if info, ok := deps[pkgName]; ok {
					if info.Kind == "dev" {
						devPkgs = append(devPkgs, pkgName)
					} else {
						prodPkgs = append(prodPkgs, pkgName)
					}
				} else {
					// New package? Default to prod
					prodPkgs = append(prodPkgs, pkgName)
				}
			}

			// 2. Execute Updates
			// Prod Packages
			if len(prodPkgs) > 0 {
				args := []string{"pub", "add"}
				args = append(args, prodPkgs...)
				cmdProd := exec.Command("flutter", args...)
				cmdProd.Dir = targetPath
				output, err := cmdProd.CombinedOutput()
				if err != nil {
					return fmt.Errorf("prod paketleri güncellenemedi: %v\n%s", err, string(output))
				}
			}

			// Dev Packages
			if len(devPkgs) > 0 {
				args := []string{"pub", "add", "--dev"}
				args = append(args, devPkgs...)
				cmdDev := exec.Command("flutter", args...)
				cmdDev.Dir = targetPath
				output, err := cmdDev.CombinedOutput()
				if err != nil {
					return fmt.Errorf("dev paketleri güncellenemedi: %v\n%s", err, string(output))
				}
			}

			return nil // Success if we handled everything
		} else {
			// Fallback: update all compatible
			// Use --major-versions to ignore constraints if possible
			cmd = exec.Command("flutter", "pub", "upgrade", "--major-versions")
		}
	} else {
		cmd, _ = d.nodeUpdateCommand(targetPath, pkgs)
	}

	cmd.Dir = targetPath

	output, err := cmd.CombinedOutput()

	// Temporary Debug Logging
	debugLog := fmt.Sprintf("CMD: %s %v\nDIR: %s\nERR: %v\nOUT: %s\n--------------------\n", cmd.Path, cmd.Args, cmd.Dir, err, string(output))

	home, _ := os.UserHomeDir()
	logPath := filepath.Join(home, "devterminal_doctor_debug.log")
	f, _ := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if f != nil {
		f.WriteString(debugLog)
		f.Close()
	}

	if err != nil {
		return fmt.Errorf("güncelleme başarısız: %v\nOutput: %s", err, string(output))
	}

	return nil
}
