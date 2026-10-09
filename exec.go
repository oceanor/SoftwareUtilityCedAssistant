package main

// Esecuzione di processi esterni, PowerShell, registro, ISO e percorsi di sistema.
// Tutte queste funzioni girano nella goroutine del task: non toccano i widget.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	timeoutCmdDefault   = 15 * time.Minute
	timeoutMsiexec      = 45 * time.Minute
	timeoutOfficeSetup  = 60 * time.Minute
	timeoutPowerShell   = 45 * time.Minute
	timeoutPowerShellWU = 120 * time.Minute
	timeoutMountISO     = 5 * time.Minute
	timeoutXcopy        = 30 * time.Minute
	timeoutShortPS      = 3 * time.Minute
	timeoutDomainOp     = 10 * time.Minute

	// CREATE_NO_WINDOW: i figli console non aprono una finestra CMD.
	// Non si usa HideWindow: nasconderebbe anche Word, ncpa.cpl ed explorer.
	createNoWindow = 0x08000000
)

func subprocessSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: createNoWindow}
}

// logWriter riversa nel log l'output di un processo, una riga alla volta.
type logWriter struct {
	prefix string
	buf    []byte
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		w.emit(w.buf[:i])
		w.buf = w.buf[i+1:]
	}
}

func (w *logWriter) flush() {
	w.emit(w.buf)
	w.buf = nil
}

func (w *logWriter) emit(line []byte) {
	if s := strings.TrimRight(string(line), "\r\n\t "); strings.TrimSpace(s) != "" {
		appendToLog(w.prefix + s)
	}
}

// execLogged esegue cmd (creato con ctx) riversando stdout/stderr nel log.
// WaitDelay evita il blocco quando un processo nipote tiene aperte le pipe (setup Office, ccmsetup).
func execLogged(ctx context.Context, cmd *exec.Cmd, timeout time.Duration) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= createNoWindow
	out, errw := &logWriter{prefix: "> "}, &logWriter{prefix: "! "}
	cmd.Stdout, cmd.Stderr = out, errw
	cmd.WaitDelay = 5 * time.Second
	err := cmd.Run()
	out.flush()
	errw.flush()
	name := filepath.Base(cmd.Path)
	if ctx.Err() == context.DeadlineExceeded {
		appendToLog(fmt.Sprintf("TIMEOUT %s (oltre %v)", name, timeout))
		return ctx.Err()
	}
	if err != nil {
		if code := exitCode(err); code > 0 {
			appendToLog(fmt.Sprintf("%s: codice di uscita %d", name, code))
		} else {
			appendToLog(fmt.Sprintf("%s: %v", name, err))
		}
	}
	return err
}

func runCmd(dir string, stdin io.Reader, timeout time.Duration, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdin = stdin
	return execLogged(ctx, cmd, timeout)
}

func runCommand(name string, args ...string) error {
	return runCmd("", nil, timeoutCmdDefault, name, args...)
}

func runCommandTimed(timeout time.Duration, name string, args ...string) error {
	return runCmd("", nil, timeout, name, args...)
}

// runQuiet esegue senza log (per comandi il cui fallimento è atteso).
func runQuiet(name string, args ...string) error {
	c := exec.Command(name, args...)
	c.SysProcAttr = subprocessSysProcAttr()
	return c.Run()
}

// runCapture restituisce stdout+stderr combinati, senza log.
func runCapture(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	c := exec.CommandContext(ctx, name, args...)
	c.SysProcAttr = subprocessSysProcAttr()
	out, err := c.CombinedOutput()
	return string(out), err
}

func startDetached(name string, args ...string) {
	c := exec.Command(name, args...)
	c.SysProcAttr = subprocessSysProcAttr()
	if err := c.Start(); err != nil {
		appendToLog(fmt.Sprintf("Avvio %s: %v", name, err))
		return
	}
	go c.Wait()
}

func openBrowser(url string) {
	startDetached("rundll32", "url.dll,FileProtocolHandler", url)
}

func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	if err != nil {
		return -1
	}
	return 0
}

// runMsiexec: 3010 (riavvio richiesto) è un successo; 1618 (altra installazione in corso,
// tipico al primo avvio di un PC nuovo) viene ritentato.
// La riga di comando è scritta a mano: msiexec non accetta "PROP=valore" tra virgolette intere.
func runMsiexec(cmdArgs string) error {
	for attempt := 1; ; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), timeoutMsiexec)
		cmd := exec.CommandContext(ctx, "msiexec")
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: "msiexec " + cmdArgs}
		err := execLogged(ctx, cmd, timeoutMsiexec)
		cancel()
		switch exitCode(err) {
		case 0:
			return nil
		case 3010:
			appendToLog("Installazione riuscita (3010: riavvio richiesto).")
			return nil
		case 1618:
			if attempt >= 10 {
				return err
			}
			appendToLog("Un'altra installazione è in corso (1618): nuovo tentativo tra 30 secondi...")
			time.Sleep(30 * time.Second)
		default:
			return err
		}
	}
}

// ---------------------------------------------------------------------------
// PowerShell
// ---------------------------------------------------------------------------

// Prefisso comune: niente barre di avanzamento (finirebbero su stderr come CLIXML)
// e output UTF-8 per non perdere le lettere accentate nei messaggi di errore.
const psPrefix = "$ProgressPreference='SilentlyContinue'; try { [Console]::OutputEncoding = [Text.Encoding]::UTF8 } catch {}\n"

// psArgs usa -EncodedCommand: nessun problema di virgolette tra Go, riga di comando e PowerShell.
func psArgs(script string) []string {
	u := utf16.Encode([]rune(psPrefix + script))
	b := make([]byte, len(u)*2)
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[i*2:], c)
	}
	return []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-EncodedCommand", base64.StdEncoding.EncodeToString(b)}
}

func runPowerShell(script string) error {
	return runPowerShellTimed(timeoutPowerShell, script)
}

func runPowerShellTimed(timeout time.Duration, script string) error {
	return runCmd("", nil, timeout, "powershell", psArgs(script)...)
}

// runPowerShellCapture restituisce lo stdout; lo stderr finisce nell'errore.
func runPowerShellCapture(timeout time.Duration, script string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	c := exec.CommandContext(ctx, "powershell", psArgs(script)...)
	c.SysProcAttr = subprocessSysProcAttr()
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil && stderr.Len() > 0 {
		err = fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), err
}

// runPowerShellWithCredential passa la password su stdin (mai sulla riga di comando)
// e rende disponibile $cred allo script body.
func runPowerShellWithCredential(timeout time.Duration, user, password, body string) error {
	passB64 := base64.StdEncoding.EncodeToString([]byte(password))
	script := "$line = [Console]::In.ReadLine()\n" +
		"$plain = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($line))\n" +
		"$sec = ConvertTo-SecureString -String $plain -AsPlainText -Force\n" +
		"$cred = New-Object System.Management.Automation.PSCredential(" + escapePS(user) + ", $sec)\n" +
		body
	return runCmd("", strings.NewReader(passB64+"\n"), timeout, "powershell", psArgs(script)...)
}

func escapePS(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// ---------------------------------------------------------------------------
// Registro
// ---------------------------------------------------------------------------

func regSet(hive, key, val, typ, data string) {
	var h registry.Key
	switch hive {
	case "HKLM":
		h = registry.LOCAL_MACHINE
	case "HKCU":
		h = registry.CURRENT_USER
	case "HKCR":
		h = registry.CLASSES_ROOT
	case "HKU":
		h = registry.USERS
	default:
		return
	}
	k, _, err := registry.CreateKey(h, key, registry.SET_VALUE)
	if err != nil {
		appendToLog(fmt.Sprintf("Registro %s\\%s: %v", hive, key, err))
		return
	}
	defer k.Close()

	switch typ {
	case "REG_DWORD":
		var dw uint64
		if dw, err = strconv.ParseUint(data, 10, 32); err == nil {
			err = k.SetDWordValue(val, uint32(dw))
		}
	case "REG_SZ":
		err = k.SetStringValue(val, data)
	case "REG_BINARY":
		var b []byte
		if b, err = hex.DecodeString(strings.ReplaceAll(data, " ", "")); err == nil {
			err = k.SetBinaryValue(val, b)
		}
	}
	if err != nil {
		appendToLog(fmt.Sprintf("Registro %s\\%s [%s]: %v", hive, key, val, err))
	}
}

// Profilo Default: le impostazioni utente scritte qui valgono per ogni nuovo account (anche di dominio).
const defaultHiveKey = "CedDefault"

var defaultHiveLoaded bool // usato solo dal task in corso (un task per volta)

func loadDefaultHive() {
	dir, err := readRegistrySZExpanded(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList`, "Default")
	if err != nil || dir == "" {
		dir = filepath.Join(os.Getenv("SystemDrive")+`\`, "Users", "Default")
	}
	runQuiet("reg", "unload", `HKU\`+defaultHiveKey) // residuo di un'esecuzione interrotta
	if err := runCommand("reg", "load", `HKU\`+defaultHiveKey, filepath.Join(dir, "NTUSER.DAT")); err != nil {
		appendToLog("Profilo Default non caricato: le impostazioni valgono solo per l'utente corrente.")
		return
	}
	defaultHiveLoaded = true
}

func unloadDefaultHive() {
	if !defaultHiveLoaded {
		return
	}
	defaultHiveLoaded = false
	for i := 0; i < 3; i++ {
		if runQuiet("reg", "unload", `HKU\`+defaultHiveKey) == nil {
			return
		}
		time.Sleep(time.Second)
	}
	appendToLog("ATTENZIONE: profilo Default ancora caricato (HKU\\" + defaultHiveKey + "). Riavviare il PC prima di creare nuovi utenti.")
}

// regSetUser scrive un'impostazione utente per l'account corrente e per il profilo Default.
func regSetUser(key, val, typ, data string) {
	regSet("HKCU", key, val, typ, data)
	if defaultHiveLoaded {
		regSet("HKU", defaultHiveKey+`\`+key, val, typ, data)
	}
}

func readRegistrySZExpanded(h registry.Key, subKey, valueName string) (string, error) {
	k, err := registry.OpenKey(h, subKey, registry.READ)
	if err != nil {
		return "", err
	}
	defer k.Close()
	s, valType, err := k.GetStringValue(valueName)
	if err != nil {
		return "", err
	}
	if valType == registry.EXPAND_SZ {
		return registry.ExpandString(s)
	}
	return s, nil
}

// ---------------------------------------------------------------------------
// ISO
// ---------------------------------------------------------------------------

// mountISO monta l'ISO (o riusa il montaggio già presente) e restituisce la lettera di unità.
// Attende che Windows assegni la lettera: subito dopo Mount-DiskImage può essere ancora vuota.
func mountISO(iso string) (string, error) {
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$p = %s
if (-not (Get-DiskImage -ImagePath $p).Attached) { Mount-DiskImage -ImagePath $p | Out-Null }
for ($i = 0; $i -lt 20; $i++) {
  $l = (Get-DiskImage -ImagePath $p | Get-Volume -ErrorAction SilentlyContinue).DriveLetter
  if ($l) { [Console]::Out.Write([string]$l); exit 0 }
  Start-Sleep -Milliseconds 500
}
[Console]::Error.WriteLine('ISO montata ma nessuna lettera di unita assegnata')
exit 2`, escapePS(iso))
	out, err := runPowerShellCapture(timeoutMountISO, script)
	if err != nil {
		return "", err
	}
	d := extractDriveLetter(out)
	if d == "" {
		return "", fmt.Errorf("lettera unità non valida: %q", out)
	}
	return d, nil
}

func dismountISO(iso string) {
	runPowerShellTimed(timeoutShortPS, fmt.Sprintf(`Dismount-DiskImage -ImagePath %s -ErrorAction SilentlyContinue | Out-Null`, escapePS(iso)))
}

func extractDriveLetter(raw string) string {
	for _, ch := range strings.TrimSpace(raw) {
		switch {
		case ch >= 'A' && ch <= 'Z':
			return string(ch)
		case ch >= 'a' && ch <= 'z':
			return strings.ToUpper(string(ch))
		}
	}
	return ""
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

// ---------------------------------------------------------------------------
// Sistema
// ---------------------------------------------------------------------------

func isComputerInDomain() bool {
	var nameBuffer *uint16
	var joinStatus uint32
	if err := windows.NetGetJoinInformation(nil, &nameBuffer, &joinStatus); err != nil {
		return false
	}
	defer windows.NetApiBufferFree((*byte)(unsafe.Pointer(nameBuffer)))
	return joinStatus == windows.NetSetupDomainName
}

func currentHostname() string {
	h, _ := os.Hostname()
	return h
}

func getOfficePath(executable string) string {
	keyPath := `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\` + executable
	for _, access := range []uint32{registry.QUERY_VALUE, registry.QUERY_VALUE | registry.WOW64_32KEY} {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, keyPath, access)
		if err != nil {
			continue
		}
		path, _, err := k.GetStringValue("")
		k.Close()
		path = strings.Trim(path, `"`)
		if err == nil && path != "" {
			if _, err := os.Stat(path); err == nil {
				return path
			}
		}
	}
	return ""
}

// findDesktopPath: Desktop pubblico (visibile a tutti gli utenti, attuali e futuri);
// in mancanza, il desktop dell'utente corrente.
func findDesktopPath() (string, error) {
	for _, id := range []*windows.KNOWNFOLDERID{windows.FOLDERID_PublicDesktop, windows.FOLDERID_Desktop} {
		if p, err := windows.KnownFolderPath(id, 0); err == nil {
			if info, err := os.Stat(p); err == nil && info.IsDir() {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("nessuna cartella Desktop valida trovata")
}

var guidRe = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

func findGUID(s string) string {
	return guidRe.FindString(s)
}
