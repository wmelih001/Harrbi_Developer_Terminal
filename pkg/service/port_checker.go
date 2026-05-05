package service

import (
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// PortInfo port kullanım bilgilerini tutar
type PortInfo struct {
	Port      int
	InUse     bool
	ProcessID int
	Process   string
	Label     string
}

// CommonPorts yaygın geliştirme portları
var CommonPorts = map[string][]int{
	"frontend": {3000, 3001, 5173, 5174, 8080, 4200, 4000, 4001},
	"backend":  {3000, 3001, 4000, 4001, 5000, 8000, 8080, 9000},
}

// IsPortInUse verilen portun kullanımda olup olmadığını kontrol eder
func IsPortInUse(port int) bool {
	// 1. Basit TCP Listen kontrolü (127.0.0.1)
	if isPortBound("127.0.0.1", port) {
		return true
	}

	// 2. 0.0.0.0 kontrolü (Tüm IPv4 arayüzleri)
	if isPortBound("0.0.0.0", port) {
		return true
	}

	// 3. IPv6 kontrolü (::1)
	if isPortBound("::1", port) {
		return true
	}

	return false
}

// isPortBound tries to listen on an address/port to check availability
func isPortBound(host string, port int) bool {
	address := net.JoinHostPort(host, strconv.Itoa(port))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return true // Port kullanımda (Hata aldıysak bind edemiyoruz demektir)
	}
	defer listener.Close()
	return false // Port boş
}

// GetPortInfo verilen port hakkında detaylı bilgi döndürür
func GetPortInfo(port int) PortInfo {
	info := PortInfo{
		Port:  port,
		InUse: IsPortInUse(port),
	}

	if !info.InUse {
		return info
	}

	// Windows'ta port kullanan process'i bul
	if runtime.GOOS == "windows" {
		info.ProcessID, info.Process = getProcessUsingPortWindows(port)
	} else {
		info.ProcessID, info.Process = getProcessUsingPortUnix(port)
	}

	return info
}

// getProcessUsingPortWindows Windows'ta portu kullanan process'i bulur
func getProcessUsingPortWindows(port int) (int, string) {
	// netstat -ano | findstr :PORT
	cmd := exec.Command("cmd", "/c", fmt.Sprintf("netstat -ano | findstr :%d", port))
	output, err := cmd.Output()
	if err != nil {
		return 0, ""
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// LISTENING satırını bul
		if strings.Contains(line, "LISTENING") {
			fields := strings.Fields(line)
			if len(fields) >= 5 {
				pid, _ := strconv.Atoi(fields[4])
				if pid > 0 {
					processName := getProcessNameByPID(pid)
					return pid, processName
				}
			}
		}
	}
	return 0, ""
}

// getProcessUsingPortUnix Unix/Linux/Mac'te portu kullanan process'i bulur
func getProcessUsingPortUnix(port int) (int, string) {
	// lsof -i :PORT
	cmd := exec.Command("lsof", "-i", fmt.Sprintf(":%d", port))
	output, err := cmd.Output()
	if err != nil {
		return 0, ""
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return 0, ""
	}

	// İkinci satır (header'dan sonra)
	fields := strings.Fields(lines[1])
	if len(fields) >= 2 {
		pid, _ := strconv.Atoi(fields[1])
		processName := fields[0]
		return pid, processName
	}

	return 0, ""
}

// getProcessNameByPID Windows'ta PID'den process adını alır
func getProcessNameByPID(pid int) string {
	cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	line := strings.TrimSpace(string(output))
	if line == "" || strings.Contains(line, "No tasks") {
		return ""
	}

	// CSV format: "process.exe","PID","Session Name","Session#","Mem Usage"
	parts := strings.Split(line, ",")
	if len(parts) >= 1 {
		name := strings.Trim(parts[0], "\"")
		return name
	}

	return ""
}

// CheckProjectPorts proje için yaygın portları kontrol eder
func CheckProjectPorts(hasFrontend, hasBackend bool) []PortInfo {
	var results []PortInfo
	checked := make(map[int]bool)

	if hasFrontend {
		for _, port := range CommonPorts["frontend"] {
			if !checked[port] {
				checked[port] = true
				info := GetPortInfo(port)
				if info.InUse {
					results = append(results, info)
				}
			}
		}
	}

	if hasBackend {
		for _, port := range CommonPorts["backend"] {
			if !checked[port] {
				checked[port] = true
				info := GetPortInfo(port)
				if info.InUse {
					results = append(results, info)
				}
			}
		}
	}

	return results
}

// FormatPortWarning port uyarı mesajını formatlar
func FormatPortWarning(info PortInfo) string {
	if info.Process != "" {
		return fmt.Sprintf("⚠️ Port %d kullanımda (%s, PID: %d)", info.Port, info.Process, info.ProcessID)
	}
	return fmt.Sprintf("⚠️ Port %d kullanımda", info.Port)
}

func FormatDependencyPortStatus(info PortInfo) string {
	label := strings.TrimSpace(info.Label)
	if label == "" {
		label = "Docker bağımlılığı"
	}
	if info.InUse {
		if info.Process != "" {
			return fmt.Sprintf("%s %d hazır (%s, PID: %d)", label, info.Port, info.Process, info.ProcessID)
		}
		return fmt.Sprintf("%s %d hazır", label, info.Port)
	}
	return fmt.Sprintf("%s %d bekleniyor", label, info.Port)
}

// KillPort belirtilen portu kullanan işlemi sonlandırmaya çalışır
func KillPort(port int) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// Windows: netstat ile PID bul ve taskkill ile öldür
		command := fmt.Sprintf("for /f \"tokens=5\" %%a in ('netstat -aon ^| find \":%d\" ^| find \"LISTENING\"') do taskkill /F /PID %%a", port)
		cmd = exec.Command("cmd", "/C", command)
	} else {
		// Unix: lsof -t -i:PORT | xargs kill -9
		cmd = exec.Command("sh", "-c", fmt.Sprintf("lsof -t -i:%d | xargs kill -9", port))
	}
	return cmd.Run()
}
