package main

// Le funzionalità. Ogni funzione gira nella goroutine del task (vedi runTask):
// riceve i dati già letti dalla UI e comunica solo via appendToLog / setProgress.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Setup PC
// ---------------------------------------------------------------------------

const highPerfGUID = "8c5e7fda-e8bf-4a96-9a85-a6e23a8c635c"

// runHighPerformance: sui portatili con Modern Standby lo schema "Prestazioni elevate"
// non esiste finché non lo si duplica dal modello.
func runHighPerformance() {
	appendToLog("Piano di alimentazione: Prestazioni elevate...")
	if runQuiet("powercfg", "/setactive", highPerfGUID) != nil {
		appendToLog("Schema non presente: creazione dal modello...")
		out, err := runCapture(timeoutShortPS, "powercfg", "/duplicatescheme", highPerfGUID)
		guid := findGUID(out)
		if err != nil || guid == "" {
			appendToLog("Creazione schema fallita: " + strings.TrimSpace(out))
			return
		}
		if runCommand("powercfg", "/setactive", guid) != nil {
			return
		}
	}
	appendToLog("Piano Prestazioni elevate attivo.")
	setProgress(100)
}

// runStartDot3 avvia l'autenticazione cablata e applica resources\8021x.xml a ogni scheda Ethernet fisica:
// equivale a Proprietà scheda > Autenticazione > niente verifica certificato server + "Autenticazione computer".
// Il file si rigenera da un PC configurato a mano con: netsh lan export profile folder=C:\temp
func runStartDot3() {
	appendToLog("Servizio Dot3Svc (autenticazione 802.1X cablata): avvio automatico...")
	runCommand("sc", "config", "dot3svc", "start=", "auto")
	setProgress(20)
	profile := filepath.Join(resourcesDir(), "8021x.xml")
	if !fileExists(profile) {
		appendToLog("ERRORE: profilo 802.1X non trovato: " + profile)
		return
	}
	// Start-Service attende che il servizio sia avviato: netsh lan fallisce se dot3svc non è ancora attivo.
	runPowerShell(`$f = ` + escapePS(profile) + `
Start-Service dot3svc
$ifs = @(Get-NetAdapter -Physical | Where-Object MediaType -eq '802.3')
if ($ifs.Count -eq 0) { Write-Output 'Nessuna scheda di rete cablata trovata.' }
foreach ($a in $ifs) {
  Write-Output ('Profilo 802.1X su "' + $a.Name + '" (' + $a.InterfaceDescription + ')...')
  netsh lan add profile $f $a.Name  # parametri posizionali: PowerShell quota da sé i percorsi con spazi
}`)
	setProgress(100)
}

func openNetworkSettings() {
	startDetached("control.exe", "ncpa.cpl")
}

const adobeKey = "adobereader"

func runSoftwareInstall(files []string) {
	for i, item := range files {
		appendToLog(fmt.Sprintf("Software %d di %d", i+1, len(files)))
		if item == adobeKey {
			installAdobe()
		} else if path := filepath.Join(softwaresDir(), item); fileExists(path) {
			appendToLog("Installazione " + item + "...")
			if runMsiexec(fmt.Sprintf(`/i "%s" /qn /norestart`, path)) != nil {
				appendToLog("Installazione fallita: " + item)
			}
		} else {
			appendToLog("Mancante: " + path)
		}
		setProgress((i + 1) * 100 / len(files))
	}
}

// installAdobe usa la prima patch .msp presente: aggiornare la patch non richiede modifiche al codice.
func installAdobe() {
	dir := filepath.Join(softwaresDir(), adobeKey)
	msi := filepath.Join(dir, "AcroPro.msi")
	if !fileExists(msi) {
		appendToLog("Mancante: " + msi)
		return
	}
	args := fmt.Sprintf(`/i "%s" /qn /norestart`, msi)
	if msps, _ := filepath.Glob(filepath.Join(dir, "*.msp")); len(msps) > 0 {
		args += fmt.Sprintf(` PATCH="%s"`, msps[0])
		appendToLog("Installazione Adobe Reader con patch " + filepath.Base(msps[0]) + "...")
	} else {
		appendToLog("Installazione Adobe Reader (nessuna patch .msp trovata)...")
	}
	if runMsiexec(args) != nil {
		appendToLog("Installazione Adobe Reader fallita.")
	}
}

func runOfficeInstall() {
	iso := filepath.Join(softwaresDir(), appConfig.OfficeISO)
	xml := filepath.Join(softwaresDir(), "config.xml")
	if appConfig.OfficeISO == "" || !fileExists(iso) {
		appendToLog("ERRORE: ISO Office non trovata: " + iso)
		return
	}
	if !fileExists(xml) {
		appendToLog("ERRORE: config.xml non trovato: " + xml)
		return
	}
	removeClickToRunOffice()
	setProgress(10)

	appendToLog("Montaggio ISO Office...")
	drive, err := mountISO(iso)
	localCopy := ""
	if err != nil {
		// Alcuni volumi (es. file segnaposto OneDrive, alcuni file system della chiavetta) non si montano.
		appendToLog("Montaggio dalla chiavetta fallito: " + err.Error())
		localCopy = filepath.Join(os.Getenv("SystemRoot"), "Temp", "CedOffice.iso")
		appendToLog("Copia dell'ISO in " + localCopy + " e nuovo tentativo...")
		if err := copyFile(iso, localCopy); err != nil {
			appendToLog("ERRORE copia ISO: " + err.Error())
			return
		}
		iso = localCopy
		if drive, err = mountISO(iso); err != nil {
			appendToLog("ERRORE montaggio ISO: " + err.Error())
			dismountISO(iso)
			os.Remove(localCopy)
			return
		}
	}
	defer func() {
		dismountISO(iso)
		if localCopy != "" {
			os.Remove(localCopy)
		}
	}()
	setProgress(30)

	setup := drive + `:\setup.exe`
	if !fileExists(setup) {
		appendToLog("ERRORE: setup.exe non presente nell'ISO montata (" + setup + ")")
		return
	}
	appendToLog("Installazione Office in corso (10-20 minuti, nessuna finestra visibile)...")
	if err := runCommandTimed(timeoutOfficeSetup, setup, "/config", xml); err != nil && exitCode(err) != 3010 {
		appendToLog("ERRORE installazione Office. Righe significative del log di setup:")
		reportOfficeSetupLog()
		return
	}
	appendToLog("Office installato.")
	setProgress(80)
	activateOffice()
	setProgress(100)
}

// removeClickToRunOffice: i PC nuovi hanno spesso Microsoft 365 o OneNote Click-to-Run preinstallati,
// e Office 2016 MSI non si installa accanto a Office 16 Click-to-Run (il setup esce con 30066).
// Si usa la stessa disinstallazione di "App installate", resa silenziosa da DisplayLevel=False.
func removeClickToRunOffice() {
	runPowerShell(`$keys = 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*', 'HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*'
function Find-Uninstall($filter) { @(Get-ItemProperty $keys -ErrorAction SilentlyContinue | Where-Object -FilterScript $filter) }
# Una voce per prodotto e per lingua (spesso 5-6): togliere una lingua può togliere anche le altre, quindi si ricontrolla ogni voce.
foreach ($p in Find-Uninstall { $_.UninstallString -like '*OfficeClickToRun.exe*' }) {
  if (-not (Test-Path $p.PSPath)) { continue }
  if ($p.UninstallString -match '^"([^"]+)"\s*(.*)$' -and (Test-Path $Matches[1])) {
    Write-Output ('Rimozione Office Click-to-Run preinstallato: ' + $p.DisplayName + '...')
    Start-Process -FilePath $Matches[1] -ArgumentList ($Matches[2] + ' DisplayLevel=False') -Wait
  }
}
# Componenti MSI che Click-to-Run a volte lascia dietro di sé: bloccano anch'essi il setup.
foreach ($p in Find-Uninstall { $_.DisplayName -like 'Office 16 Click-to-Run*' }) {
  Write-Output ('Rimozione ' + $p.DisplayName + '...')
  Start-Process -FilePath msiexec.exe -ArgumentList ('/x ' + $p.PSChildName + ' /qn /norestart') -Wait
}
foreach ($p in Find-Uninstall { $_.UninstallString -like '*OfficeClickToRun.exe*' -or $_.DisplayName -like 'Office 16 Click-to-Run*' }) {
  Write-Output ('ATTENZIONE: ancora presente, rimuoverlo da App installate: ' + $p.DisplayName)
}`)
}

// reportOfficeSetupLog copia sulla chiavetta l'ultimo SetupExe(*).log (in %TEMP%, dove il setup lo scrive)
// e riporta nel log le righe con il motivo dell'errore.
func reportOfficeSetupLog() {
	dest := ""
	if logFilePath != "" {
		dest = strings.TrimSuffix(logFilePath, ".log") + "-SetupExe.log"
	}
	runPowerShell(`$f = Get-ChildItem $env:TEMP -Filter 'SetupExe(*).log' -ErrorAction SilentlyContinue | Sort-Object LastWriteTime | Select-Object -Last 1
if (-not $f) { Write-Output ('Nessun SetupExe(*).log in ' + $env:TEMP); exit }
$dest = ` + escapePS(dest) + `
if ($dest) { Copy-Item $f.FullName $dest -Force; Write-Output ('Log completo copiato in ' + $dest) }
Get-Content $f.FullName | Select-String -Pattern 'error|alongside|prereq|fail' | Select-Object -Last 15 | ForEach-Object { $_.Line.Trim() }`)
}

// activateOffice usa le classi WMI di licenza (le stesse di ospp.vbs):
// funziona anche con Windows Script Host disattivato.
func activateOffice() {
	if appConfig.OfficeKey == "" {
		appendToLog("Nessuna chiave Office in config.json: attivazione saltata.")
		return
	}
	appendToLog("Attivazione Office...")
	runPowerShellTimed(timeoutShortPS, fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$svc = Get-CimInstance -ClassName SoftwareLicensingService
Invoke-CimMethod -InputObject $svc -MethodName InstallProductKey -Arguments @{ ProductKey = %s } | Out-Null
Invoke-CimMethod -InputObject $svc -MethodName RefreshLicenseStatus | Out-Null
Write-Output 'Chiave Office installata.'
$f = "ApplicationID='0ff1ce15-a989-479d-af46-f275c6370663' AND PartialProductKey IS NOT NULL"
foreach ($p in Get-CimInstance -ClassName SoftwareLicensingProduct -Filter $f) {
  try { Invoke-CimMethod -InputObject $p -MethodName Activate | Out-Null; Write-Output ('Attivato: ' + $p.Name) }
  catch { Write-Output ('Attivazione non riuscita per ' + $p.Name + ': ' + $_.Exception.Message) }
}`, escapePS(appConfig.OfficeKey)))
}

// ---------------------------------------------------------------------------
// Dominio
// ---------------------------------------------------------------------------

func credUser(user, domain string) string {
	if domain == "" || strings.ContainsAny(user, `\@`) {
		return user
	}
	return domain + `\` + user
}

func needsRename(newName string) bool {
	n := strings.TrimSpace(newName)
	return n != "" && !strings.EqualFold(n, currentHostname())
}

func runRenamePC(newName, user, pass, domain string) {
	var err error
	if isComputerInDomain() {
		if pass == "" {
			appendToLog("Il PC è in dominio: per rinominarlo serve la password dell'amministratore di dominio.")
			return
		}
		appendToLog("PC in dominio: rinomina con credenziali di dominio...")
		err = runPowerShellWithCredential(timeoutDomainOp, credUser(user, domain), pass,
			fmt.Sprintf("Rename-Computer -NewName %s -DomainCredential $cred -Force -ErrorAction Stop", escapePS(newName)))
	} else {
		appendToLog("PC in workgroup: rinomina locale...")
		err = runPowerShellTimed(timeoutDomainOp,
			fmt.Sprintf("Rename-Computer -NewName %s -Force -ErrorAction Stop", escapePS(newName)))
	}
	setProgress(100)
	if err != nil {
		appendToLog("Rinomina PC fallita.")
		return
	}
	appendToLog("Rinomina completata: riavviare il PC per applicarla.")
}

// waitForDomain: appena applicato il profilo 802.1X la scheda si riautentica e per qualche secondo la rete manca.
func waitForDomain(domain string) {
	appendToLog("Attesa della rete di dominio (cavo di rete collegato)...")
	runPowerShellTimed(timeoutShortPS, `$d = `+escapePS(domain)+`
for ($i = 0; $i -lt 24; $i++) {
  if (Resolve-DnsName -Name $d -DnsOnly -ErrorAction SilentlyContinue) { Write-Output ('Dominio ' + $d + ' raggiungibile.'); exit }
  Start-Sleep -Seconds 5
}
Write-Output ('ATTENZIONE: dominio ' + $d + ' non raggiungibile dopo 2 minuti: controllare cavo e presa di rete.')`)
}

// runJoinDomain unisce il PC al dominio con il nuovo nome in un solo passo (Add-Computer -NewName).
// Rinominare e poi unire in due passi registra in AD il nome vecchio e rompe la relazione di trust al riavvio.
func runJoinDomain(newName, user, pass, domain string) {
	if domain == "" || user == "" || pass == "" {
		appendToLog("ERRORE: compilare dominio, utente e password.")
		return
	}
	if isComputerInDomain() {
		appendToLog("Il PC è già in dominio.")
		if needsRename(newName) {
			runRenamePC(newName, user, pass, domain)
		}
		return
	}
	body := fmt.Sprintf("Add-Computer -DomainName %s -Credential $cred -Force -ErrorAction Stop", escapePS(domain))
	if needsRename(newName) {
		body += " -NewName " + escapePS(newName)
		appendToLog(fmt.Sprintf("Aggiunta al dominio %s con il nome %s...", domain, newName))
	} else {
		appendToLog(fmt.Sprintf("Aggiunta al dominio %s...", domain))
	}
	err := runPowerShellWithCredential(timeoutDomainOp, credUser(user, domain), pass, body)
	setProgress(100)
	if err != nil {
		appendToLog("Aggiunta al dominio fallita.")
		return
	}
	appendToLog("Aggiunta al dominio completata: riavviare il PC.")
}

// runRetrustDomain rimette a posto la relazione di trust di un PC che non si connette
// al dominio da tempo: lo toglie dal dominio e lo rimette, riusando runJoinDomain.
func runRetrustDomain(hostname, user, pass, domain string) {
	if !isComputerInDomain() {
		appendToLog("Il PC non è in dominio: usare direttamente AGGIUNGI A DOMINIO.")
		return
	}
	appendToLog("Rimozione dal dominio in corso...")
	body := "Remove-Computer -UnjoinDomainCredential $cred -WorkgroupName WORKGROUP -Force -ErrorAction Stop"
	if err := runPowerShellWithCredential(timeoutDomainOp, credUser(user, domain), pass, body); err != nil {
		appendToLog("ERRORE: rimozione dal dominio fallita.")
		return
	}
	appendToLog("Rimosso dal dominio. Rientro in corso...")
	runJoinDomain(hostname, user, pass, domain)
}

// runAddRDPUser aggiunge un utente al gruppo locale "Utenti desktop remoto" (SID ben
// noto S-1-5-32-555, indipendente dalla lingua di Windows) per abilitargli l'RDP su questo PC.
func runAddRDPUser(domain, username string) {
	member := domain + `\` + username
	script := `try {
  Add-LocalGroupMember -SID 'S-1-5-32-555' -Member ` + escapePS(member) + ` -ErrorAction Stop
  Write-Output 'OK: aggiunto'
} catch {
  if ($_.Exception.GetType().Name -eq 'MemberExistsException') { Write-Output 'OK: già membro' }
  else { Write-Output ('ERRORE: ' + $_.Exception.Message); exit 1 }
}`
	out, err := runPowerShellCapture(timeoutShortPS, script)
	if err != nil {
		appendToLog("ERRORE aggiunta a Desktop Remoto: " + err.Error())
		return
	}
	appendToLog(out)
}

// listRDPUsers elenca i membri del gruppo locale "Utenti desktop remoto" (SID ben noto).
func listRDPUsers() (string, error) {
	out, err := runPowerShellCapture(timeoutShortPS,
		`Get-LocalGroupMember -SID 'S-1-5-32-555' -ErrorAction Stop | Select-Object -ExpandProperty Name`)
	if err != nil {
		return "", err
	}
	if out == "" {
		return "(nessun utente nel gruppo)", nil
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// SCCM e Windows Update
// ---------------------------------------------------------------------------

// runSCCMInstall usa installa.bat come unica fonte dei parametri di ccmsetup.
func runSCCMInstall() {
	src := filepath.Join(resourcesDir(), "xtmp")
	dst := `C:\xtmp`
	clientDir := filepath.Join(dst, "Client")
	os.MkdirAll(dst, 0755)
	appendToLog("Copia client SCCM in " + dst + "...")
	setProgress(20)
	if runCommandTimed(timeoutXcopy, "xcopy", src, dst, "/E", "/I", "/Y", "/Q") != nil {
		appendToLog("Copia SCCM non riuscita.")
	}
	setProgress(60)
	if !fileExists(filepath.Join(clientDir, "installa.bat")) {
		appendToLog("ERRORE: installa.bat non trovato in " + clientDir)
		return
	}
	appendToLog("Avvio ccmsetup (parametri da installa.bat)...")
	runCmd(clientDir, nil, timeoutCmdDefault, "cmd", "/c", "installa.bat")
	appendToLog(`ccmsetup prosegue in background. Log: C:\Windows\ccmsetup\Logs\ccmsetup.log`)
	setProgress(100)
}

// runWindowsUpdate usa l'API COM di Windows Update: stessa sorgente del PC (WSUS/SCCM o Microsoft),
// nessun modulo da scaricare da PowerShell Gallery.
func runWindowsUpdate() {
	appendToLog("Windows Update: ricerca aggiornamenti (può richiedere diversi minuti)...")
	runPowerShellTimed(timeoutPowerShellWU, `$ErrorActionPreference = 'Stop'
$s = New-Object -ComObject Microsoft.Update.Session
$s.ClientApplicationID = 'CedSetupTool'
$r = $s.CreateUpdateSearcher().Search("IsInstalled=0 and Type='Software' and IsHidden=0")
if ($r.Updates.Count -eq 0) { Write-Output 'Nessun aggiornamento disponibile.'; exit 0 }
$c = New-Object -ComObject Microsoft.Update.UpdateColl
foreach ($u in $r.Updates) {
  if (-not $u.EulaAccepted) { $u.AcceptEula() }
  Write-Output ('- ' + $u.Title)
  [void]$c.Add($u)
}
Write-Output ('Download di ' + $c.Count + ' aggiornamenti...')
$d = $s.CreateUpdateDownloader(); $d.Updates = $c
Write-Output ('Download: esito ' + $d.Download().ResultCode + ' (2 = OK)')
Write-Output 'Installazione aggiornamenti in corso: può durare molti minuti, attendere...'
$i = $s.CreateUpdateInstaller(); $i.Updates = $c
$res = $i.Install()
Write-Output ('Installazione: esito ' + $res.ResultCode + ' (2 = OK, 3 = OK con errori, 4 = fallita)')
if ($res.RebootRequired) { Write-Output 'Riavvio richiesto.' }`)
	setProgress(100)
}

// ---------------------------------------------------------------------------
// Ottimizzazione: ogni voce è un'etichetta, un default e un'azione.
// Le impostazioni utente passano da regSetUser (utente corrente + profilo Default).
// ---------------------------------------------------------------------------

type tweak struct {
	label string
	on    bool
	run   func()
}

func disableServices(names ...string) {
	for _, svc := range names {
		runQuiet("sc", "stop", svc)
		runCommand("sc", "config", svc, "start=", "disabled")
	}
}

const (
	explorerAdvanced = `Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`
	oneDriveCLSID    = `CLSID\{018D5C66-4533-4307-9B53-224DE2ED1FE6}`
)

var debloatTweaks = []tweak{
	{"HDD: disattiva indicizzazione e SysMain", true, func() {
		out, err := runPowerShellCapture(timeoutShortPS, `$d = Get-Disk | Where-Object IsBoot | Select-Object -First 1
if ($d) { (Get-PhysicalDisk | Where-Object DeviceId -eq ([string]$d.Number) | Select-Object -First 1).MediaType }`)
		if err != nil || !strings.EqualFold(out, "HDD") {
			appendToLog(fmt.Sprintf("  Disco di avvio: %q, non HDD: servizi lasciati attivi.", out))
			return
		}
		appendToLog("  Disco di avvio HDD: disattivo i servizi pesanti.")
		disableServices("WSearch", "SysMain", "MapsBroker", "PcaSvc")
	}},
	{"Disinstalla OneNote", true, func() {
		runPowerShell(`$s = (Get-ItemProperty HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\* | Where-Object { $_.DisplayName -like "*One*Note*" } | Select-Object -First 1).UninstallString
if ($s) { $exe = ($s -split '"')[1]; $arg = ($s -split '"')[2] + " DisplayLevel=False"; Start-Process $exe $arg -Wait } else { Write-Output 'OneNote non installato.' }`)
	}},
	{"Disattiva telemetria e raccolta dati", true, func() {
		regSet("HKLM", `SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\DataCollection`, "AllowTelemetry", "REG_DWORD", "0")
		regSet("HKLM", `SOFTWARE\Policies\Microsoft\Windows\DataCollection`, "AllowTelemetry", "REG_DWORD", "0")
		for _, tn := range []string{
			`Microsoft\Windows\Application Experience\Microsoft Compatibility Appraiser`,
			`Microsoft\Windows\Customer Experience Improvement Program\Consolidator`,
			`Microsoft\Windows\Customer Experience Improvement Program\UsbCeip`,
			`Microsoft\Windows\DiskDiagnostic\Microsoft-Windows-DiskDiagnosticDataCollector`,
			`Microsoft\Windows\Feedback\Siuf\DmClient`,
			`Microsoft\Windows\Maps\MapsToastTask`,
			`Microsoft\Windows\Maps\MapsUpdateTask`,
		} {
			runQuiet("schtasks", "/Change", "/TN", tn, "/Disable") // alcune attività non esistono su tutte le versioni
		}
		disableServices("DiagTrack", "dmwappushservice", "wisvc", "RetailDemo")
	}},
	{"Disattiva Cortana", true, func() {
		regSet("HKLM", `SOFTWARE\Policies\Microsoft\Windows\Windows Search`, "AllowCortana", "REG_DWORD", "0")
		runPowerShell(`Get-AppxPackage -AllUsers "Microsoft.549981C3F5F10" | Remove-AppxPackage -AllUsers`)
	}},
	{"Disinstalla OneDrive", false, func() {
		runQuiet("taskkill", "/F", "/IM", "OneDrive.exe")
		sysRoot := os.Getenv("SystemRoot")
		for _, p := range []string{filepath.Join(sysRoot, "SysWOW64", "OneDriveSetup.exe"), filepath.Join(sysRoot, "System32", "OneDriveSetup.exe")} {
			if fileExists(p) {
				runCommand(p, "/uninstall")
				break
			}
		}
		regSet("HKCR", oneDriveCLSID, "System.IsPinnedToNameSpaceTree", "REG_DWORD", "0")
		regSet("HKCR", `Wow6432Node\`+oneDriveCLSID, "System.IsPinnedToNameSpaceTree", "REG_DWORD", "0")
	}},
	{"Nascondi Meet Now e Chat", true, func() {
		regSet("HKLM", `SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\Explorer`, "HideSCAMeetNow", "REG_DWORD", "1")
		regSet("HKLM", `SOFTWARE\Policies\Microsoft\Windows\Windows Chat`, "ChatIcon", "REG_DWORD", "3")
	}},
	{"Disattiva ricerca Bing nel menu Start", true, func() {
		regSet("HKLM", `SOFTWARE\Policies\Microsoft\Windows\Windows Search`, "DisableWebSearch", "REG_DWORD", "1")
		regSetUser(`Software\Policies\Microsoft\Windows\Explorer`, "DisableSearchBoxSuggestions", "REG_DWORD", "1")
	}},
	{"Rimuovi funzionalità Xbox", true, func() {
		runPowerShell(`Get-AppxPackage -AllUsers "Microsoft.XboxApp" | Remove-AppxPackage -AllUsers`)
		disableServices("XblAuthManager", "XblGameSave", "XboxNetApiSvc")
	}},
	{"Disattiva avvio anticipato di Edge", true, func() {
		regSet("HKLM", `SOFTWARE\Policies\Microsoft\Edge`, "StartupBoostEnabled", "REG_DWORD", "0")
		regSet("HKLM", `SOFTWARE\Policies\Microsoft\Edge`, "BackgroundModeEnabled", "REG_DWORD", "0")
	}},
	{"Disattiva animazioni", true, func() {
		regSetUser(`Software\Microsoft\Windows\CurrentVersion\Explorer\VisualEffects`, "VisualFXSetting", "REG_DWORD", "2")
		regSetUser(`Control Panel\Desktop`, "UserPreferencesMask", "REG_BINARY", "90 12 03 80 10 00 00 00")
		regSetUser(`Control Panel\Desktop\WindowMetrics`, "MinAnimate", "REG_SZ", "0")
		regSetUser(explorerAdvanced, "TaskbarAnimations", "REG_DWORD", "0")
	}},
	{"Disattiva trasparenza", true, func() {
		regSetUser(`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, "EnableTransparency", "REG_DWORD", "0")
	}},
	{"Disattiva Game Bar e Game DVR", true, func() {
		regSetUser(`Software\Microsoft\Windows\CurrentVersion\GameDVR`, "AppCaptureEnabled", "REG_DWORD", "0")
		regSetUser(`System\GameConfigStore`, "GameDVR_Enabled", "REG_DWORD", "0")
		regSet("HKLM", `SOFTWARE\Policies\Microsoft\Windows\GameDVR`, "AllowGameDVR", "REG_DWORD", "0")
	}},
	{"Disattiva app in background", true, func() {
		regSet("HKLM", `SOFTWARE\Policies\Microsoft\Windows\AppPrivacy`, "LetAppsRunInBackground", "REG_DWORD", "2")
	}},
	{"Disattiva pubblicità e app suggerite", true, func() {
		regSet("HKLM", `SOFTWARE\Policies\Microsoft\Windows\CloudContent`, "DisableWindowsConsumerFeatures", "REG_DWORD", "1")
	}},
}

var uiTweaks = []tweak{
	{"Barra: mostra etichette, non combinare", true, func() {
		regSetUser(explorerAdvanced, "TaskbarGlomLevel", "REG_DWORD", "2")
	}},
	{"Barra: icone piccole (solo Windows 10)", true, func() {
		regSetUser(explorerAdvanced, "TaskbarSmallIcons", "REG_DWORD", "1")
	}},
	{"Esplora risorse: mostra estensioni file", true, func() {
		regSetUser(explorerAdvanced, "HideFileExt", "REG_DWORD", "0")
	}},
	{"Desktop: mostra icona Questo PC", true, func() {
		regSetUser(`Software\Microsoft\Windows\CurrentVersion\Explorer\HideDesktopIcons\NewStartPanel`, "{20D04FE0-3AEA-1069-A2D8-08002B30309D}", "REG_DWORD", "0")
	}},
	{"Disattiva Widget e Notizie e interessi", true, func() {
		regSet("HKLM", `SOFTWARE\Policies\Microsoft\Dsh`, "AllowNewsAndInterests", "REG_DWORD", "0")
		regSet("HKLM", `SOFTWARE\Policies\Microsoft\Windows\Windows Feeds`, "EnableFeeds", "REG_DWORD", "0")
	}},
}

var securityTweaks = []tweak{
	// Spento di default: rompe gli script di logon .vbs di dominio.
	{"Disattiva Windows Script Host (VBS)", false, func() {
		regSet("HKLM", `SOFTWARE\Microsoft\Windows Script Host\Settings`, "Enabled", "REG_DWORD", "0")
	}},
	{"Disattiva IPv6", true, func() {
		runPowerShell(`Disable-NetAdapterBinding -Name "*" -ComponentID "ms_tcpip6"`)
	}},
	{"Disattiva Segnalazione errori", true, func() {
		regSet("HKLM", `SOFTWARE\Microsoft\Windows\Windows Error Reporting`, "Disabled", "REG_DWORD", "1")
	}},
	{"Disattiva condivisione P2P aggiornamenti", true, func() {
		// 0 = solo HTTP, nessun peer. Il vecchio valore 100 (Bypass) è deprecato su Windows 11.
		regSet("HKLM", `SOFTWARE\Policies\Microsoft\Windows\DeliveryOptimization`, "DODownloadMode", "REG_DWORD", "0")
	}},
}

func restartExplorer() {
	runQuiet("taskkill", "/F", "/IM", "explorer.exe")
	time.Sleep(time.Second)
	startDetached("explorer.exe")
}

// bloatwareScript legge l'elenco dei pacchetti una sola volta e confronta per sottostringa
// ("Netflix" trova "4DF9E0F8.Netflix"). Rimuove per tutti gli utenti e dal provisioning.
func bloatwareScript(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = escapePS(n)
	}
	return "$names = @(" + strings.Join(quoted, ",") + ")\n" + `$pk = Get-AppxPackage -AllUsers
$prov = Get-AppxProvisionedPackage -Online
foreach ($n in $names) {
  foreach ($p in ($pk | Where-Object { $_.Name -like "*$n*" })) {
    try { Remove-AppxPackage -Package $p.PackageFullName -AllUsers -ErrorAction Stop; Write-Output ('Rimosso: ' + $p.Name) }
    catch { Write-Output ('Non rimosso ' + $p.Name + ': ' + $_.Exception.Message) }
  }
  foreach ($p in ($prov | Where-Object { $_.DisplayName -like "*$n*" })) {
    try { Remove-AppxProvisionedPackage -Online -PackageName $p.PackageName -ErrorAction Stop | Out-Null; Write-Output ('Rimosso dal provisioning: ' + $p.DisplayName) }
    catch { Write-Output ('Provisioning non rimosso ' + $p.DisplayName + ': ' + $_.Exception.Message) }
  }
}
# McAfee preinstallato dall'OEM può essere anche un programma classico, fuori dalla portata di Remove-AppxPackage.
Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*', 'HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*' -ErrorAction SilentlyContinue |
  Where-Object { $_.DisplayName -like '*McAfee*' } |
  ForEach-Object { Write-Output ('ATTENZIONE: programma installato, da rimuovere da App installate: ' + $_.DisplayName) }`
}

func runBloatwareRemover(names []string) {
	if len(names) == 0 {
		appendToLog("Elenco bloatware vuoto in config.json.")
		return
	}
	appendToLog("Rimozione bloatware in corso...")
	setProgress(10)
	runPowerShell(bloatwareScript(names))
	setProgress(100)
}

// ---------------------------------------------------------------------------
// Collegamenti
// ---------------------------------------------------------------------------

// officeShortcutURL è la voce Word/Excel: i percorsi si risolvono al momento della creazione,
// così funziona anche se Office è stato installato nella stessa sessione.
const officeShortcutURL = "@office"

type shortcutTarget struct {
	label string
	path  string
}

func resolveShortcutURL(url string) string {
	if url != officeShortcutURL {
		return url
	}
	var parts []string
	if p := getOfficePath("Winword.exe"); p != "" {
		parts = append(parts, "Microsoft Word>"+p)
	}
	if p := getOfficePath("Excel.exe"); p != "" {
		parts = append(parts, "Microsoft Excel>"+p)
	}
	return strings.Join(parts, "|")
}

// parseShortcutTargets interpreta "Etichetta>percorso|Etichetta2>percorso2" o un semplice URL.
func parseShortcutTargets(label, url string) []shortcutTarget {
	var out []shortcutTarget
	for _, raw := range strings.Split(url, "|") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		t := shortcutTarget{label, raw}
		if i := strings.Index(raw, ">"); i >= 0 {
			t = shortcutTarget{raw[:i], raw[i+1:]}
		}
		out = append(out, t)
	}
	return out
}

func isWebTarget(path string) bool {
	p := strings.ToLower(path)
	return strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://")
}

func openShortcutTarget(url string) {
	targets := parseShortcutTargets("", resolveShortcutURL(url))
	if len(targets) == 0 {
		appendToLog("Destinazione non disponibile (programma non installato?).")
		return
	}
	if isWebTarget(targets[0].path) {
		openBrowser(targets[0].path)
	} else {
		startDetached(targets[0].path)
	}
}

func runCreateShortcuts(jobs []shortcutTarget) {
	dir, err := findDesktopPath()
	if err != nil {
		appendToLog("ERRORE: " + err.Error())
		return
	}
	appendToLog("Destinazione: " + dir)

	var lnk strings.Builder
	for _, j := range jobs {
		name := sanitizeFilename(j.label)
		if isWebTarget(j.path) {
			content := "[InternetShortcut]\r\nURL=" + j.path + "\r\n"
			if err := os.WriteFile(filepath.Join(dir, name+".url"), []byte(content), 0644); err != nil {
				appendToLog("Errore " + name + ": " + err.Error())
			} else {
				appendToLog("Creato: " + name)
			}
			continue
		}
		fmt.Fprintf(&lnk, "$s = $w.CreateShortcut(%s); $s.TargetPath = %s; $s.Save(); Write-Output %s\n",
			escapePS(filepath.Join(dir, name+".lnk")), escapePS(j.path), escapePS("Creato: "+name))
	}
	if lnk.Len() > 0 {
		runPowerShell("$w = New-Object -ComObject WScript.Shell\n" + lnk.String())
	}
	setProgress(100)
}
