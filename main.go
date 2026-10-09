package main

// Software Utility Ced Assistant: configurazione rapida dei PC nuovi.
// main.go: configurazione, log, helper generici. UI in ui.go, funzionalità in tasks.go,
// processi/registro/ISO in exec.go.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lxn/walk"
)

type SoftwareConfig struct {
	Name    string   `json:"name"`
	Files   []string `json:"files"`
	Checked bool     `json:"checked"`
}

type AppConfig struct {
	OfficeKey       string                `json:"office_key"`
	OfficeISO       string                `json:"office_iso"`
	DomainName      string                `json:"domain_name"`
	DomainAdminUser string                `json:"domain_admin_user"`
	Softwares       []SoftwareConfig      `json:"softwares"`
	Bloatware       []string              `json:"bloatware"`
	Shortcuts       []ShortcutJsonSection `json:"shortcuts"`
}

type ShortcutJsonItem struct {
	Label   string `json:"label"`
	Url     string `json:"url"`
	Checked bool   `json:"checked"`
}

type ShortcutJsonSection struct {
	Title string             `json:"title"`
	Items []ShortcutJsonItem `json:"items"`
}

const logTailMaxLines = 200

var (
	appConfig      AppConfig
	startupJSONErr error
	baseDir        string // cartella dell'exe (radice della chiavetta)

	logFile     *os.File
	logFilePath string
	logMu       sync.Mutex
)

func main() {
	exe, _ := os.Executable()
	baseDir = filepath.Dir(exe)
	loadConfig()
	openLogFile()
	if err := runMainWindow(); err != nil {
		walk.MsgBox(nil, "Errore avvio", err.Error(), walk.MsgBoxIconError)
	}
}

func resourcesDir() string { return filepath.Join(baseDir, "resources") }
func softwaresDir() string { return filepath.Join(resourcesDir(), "softwares") }

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func loadConfig() {
	configPath := filepath.Join(resourcesDir(), "config.json")
	data, err := os.ReadFile(configPath)
	if err == nil {
		startupJSONErr = json.Unmarshal(data, &appConfig)
		return
	}
	if !os.IsNotExist(err) {
		startupJSONErr = err
		return
	}
	// Primo avvio senza config: crea un modello da completare.
	appConfig = AppConfig{
		Softwares: []SoftwareConfig{{Name: "Esempio", Files: []string{"setup.msi"}, Checked: true}},
		Bloatware: []string{"Microsoft.3DBuilder", "Microsoft.BingNews"},
		Shortcuts: []ShortcutJsonSection{{
			Title: "Generica",
			Items: []ShortcutJsonItem{{Label: "Intranet", Url: "https://intranet.example.local/", Checked: true}},
		}},
	}
	os.MkdirAll(resourcesDir(), 0755)
	if data, err := json.MarshalIndent(appConfig, "", "  "); err == nil {
		os.WriteFile(configPath, data, 0644)
	}
}

// openLogFile: il log resta sulla chiavetta, un file per PC. Se la chiavetta è protetta
// da scrittura si prosegue con il solo log a video.
func openLogFile() {
	dir := filepath.Join(baseDir, "logs")
	if os.MkdirAll(dir, 0755) != nil {
		return
	}
	path := filepath.Join(dir, currentHostname()+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	logFile, logFilePath = f, path
	fmt.Fprintf(f, "\r\n===== %s =====\r\n", time.Now().Format("2006-01-02 15:04:05"))
}

func appendToLog(text string) {
	line := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), text)
	logMu.Lock()
	if logFile != nil {
		logFile.WriteString(line + "\r\n")
	}
	logMu.Unlock()
	if mainWindow == nil {
		return
	}
	mainWindow.Synchronize(func() {
		if logText == nil {
			return
		}
		logText.AppendText(line + "\r\n")
		trimLogTextLines(logText, logTailMaxLines)
		n := len(logText.Text())
		logText.SetTextSelection(n, n)
	})
}

func trimLogTextLines(te *walk.TextEdit, maxLines int) {
	lines := strings.Split(strings.ReplaceAll(te.Text(), "\r\n", "\n"), "\n")
	if len(lines) > maxLines {
		te.SetText(strings.Join(lines[len(lines)-maxLines:], "\r\n"))
	}
}

// Intervallo della barra riservato al passo corrente: ogni funzione di task ragiona
// in 0-100 e setProgress lo riporta nel suo tratto (usato dalla configurazione automatica).
// Scritti solo dalla goroutine del task, o dal thread UI prima che parta.
var progressFrom, progressTo = 0, 100

func progressRange(from, to int) {
	progressFrom, progressTo = from, to
	setProgress(0)
}

func setProgress(value int) {
	if mainWindow == nil {
		return
	}
	value = progressFrom + value*(progressTo-progressFrom)/100
	mainWindow.Synchronize(func() {
		if progressBar != nil {
			progressBar.SetValue(value)
		}
	})
}

// isValidHostname: regole NetBIOS di Windows (max 15 caratteri, lettere/numeri/trattino, non solo cifre).
func isValidHostname(name string) (bool, string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return false, "Il nome non può essere vuoto."
	}
	if len(name) > 15 {
		return false, "Il nome non può superare 15 caratteri."
	}
	if name[0] == '-' || name[len(name)-1] == '-' {
		return false, "Il nome non può iniziare o finire con un trattino."
	}
	allDigits := true
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-') {
			return false, "Solo lettere, numeri e trattini sono consentiti."
		}
		if r < '0' || r > '9' {
			allDigits = false
		}
	}
	if allDigits {
		return false, "Il nome non può essere solo numeri."
	}
	return true, ""
}

func sanitizeFilename(name string) string {
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '_'
		}
		return r
	}, name)
	return strings.TrimRight(strings.TrimSpace(name), ".")
}
