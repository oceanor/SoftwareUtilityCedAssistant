package main

// Finestra larga a colonne: tutto visibile senza scroll a 1366x768 (anche al 125%).
// Lo ScrollView per tab resta come rete di sicurezza per schermi più piccoli.
// I click handler girano sul thread UI: leggono i widget qui e passano valori ai task.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
)

type ShortcutItem struct {
	Label    string
	URL      string
	Checked  bool
	CheckBox *walk.CheckBox
}

type ShortcutSection struct {
	Title string
	Items []*ShortcutItem
}

var (
	mainWindow  *walk.MainWindow
	logText     *walk.TextEdit
	progressBar *walk.ProgressBar
	statusLabel *walk.Label

	busy       bool // un task per volta; letto e scritto solo sul thread UI
	buttonRefs []**walk.PushButton

	btnAutoConfig, btnJoinDom, btnAddRDP, btnShowRDP, btnRetrust       *walk.PushButton
	inNewHostname, inDomainName, inDomainUser, inDomainPass, inRDPUser *walk.LineEdit
	chkShowPass, chkInstallAdobe                                       *walk.CheckBox
	softwareChecks                                                     []*walk.CheckBox
	debloatChecks, uiChecks, securityChecks                            []*walk.CheckBox
	shortcutSections                                                   []*ShortcutSection
)

var btnFont = Font{Family: "Segoe UI", PointSize: 9, Bold: true}

// btn crea un pulsante d'azione a tutta larghezza, disabilitato mentre un task è in corso.
func btn(ref **walk.PushButton, text string, onClick walk.EventHandler) PushButton {
	if ref == nil {
		ref = new(*walk.PushButton)
	}
	buttonRefs = append(buttonRefs, ref)
	return PushButton{AssignTo: ref, Text: text, Font: btnFont, MinSize: Size{Height: 34}, OnClicked: onClick}
}

func group(title string, layout Layout, children ...Widget) GroupBox {
	return GroupBox{Title: title, Layout: layout, Children: children}
}

func updateButtons() {
	for _, r := range buttonRefs {
		if *r != nil {
			(*r).SetEnabled(!busy)
		}
	}
	if btnJoinDom != nil && inDomainPass != nil {
		btnJoinDom.SetEnabled(!busy && inDomainPass.Text() != "")
	}
}

// runTask esegue action in background; un solo task per volta.
func runTask(name string, action func()) {
	if busy {
		return
	}
	busy = true
	updateButtons()
	progressRange(0, 100)
	setBarState(pbstPaused) // giallo = in corso
	statusLabel.SetTextColor(walk.RGB(0x9A, 0x60, 0x00))
	appendToLog("--- AVVIO: " + name + " ---")
	start := time.Now()
	done := make(chan struct{})
	// Il cronometro che avanza ogni secondo mostra che il programma sta ancora lavorando.
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for n := 0; ; n++ {
			text := fmt.Sprintf("IN CORSO%-3s  %s  (%s) - attendere", strings.Repeat(".", n%4), name, elapsed(start))
			mainWindow.Synchronize(func() {
				select {
				case <-done: // task già finito: non sovrascrivere "FINITO"
				default:
					statusLabel.SetText(text)
				}
			})
			select {
			case <-done:
				return
			case <-t.C:
			}
		}
	}()
	go func() {
		failed := false
		defer func() {
			if r := recover(); r != nil {
				failed = true
				appendToLog(fmt.Sprintf("ERRORE INTERNO: %v", r))
			}
			close(done)
			appendToLog("--- FINE: " + name + " ---")
			mainWindow.Synchronize(func() {
				if failed {
					setBarState(pbstError)
					statusLabel.SetTextColor(walk.RGB(0xC0, 0x00, 0x00))
					statusLabel.SetText("✖ INTERROTTO: " + name + " - vedere il log")
				} else {
					progressBar.SetValue(100)
					setBarState(pbstNormal)
					statusLabel.SetTextColor(walk.RGB(0x00, 0x80, 0x00))
					statusLabel.SetText(fmt.Sprintf("✔ FINITO: %s  (%s) - esito nel log", name, elapsed(start)))
				}
				busy = false
				updateButtons()
			})
		}()
		action()
	}()
}

// Stati del controllo progress bar (PBM_SETSTATE, CommCtrl.h): verde, rosso, giallo.
const (
	pbmSetState = win.WM_USER + 16
	pbstNormal  = 1
	pbstError   = 2
	pbstPaused  = 3
)

func setBarState(state uintptr) {
	progressBar.SendMessage(pbmSetState, state, 0)
}

func elapsed(start time.Time) string {
	d := time.Since(start).Round(time.Second)
	return fmt.Sprintf("%02d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

func warn(msg string) {
	walk.MsgBox(mainWindow, "Attenzione", msg, walk.MsgBoxIconWarning)
}

func runMainWindow() error {
	initShortcutDataFromConfig()
	err := MainWindow{
		AssignTo: &mainWindow,
		Title:    "Software Utility Ced Assistant",
		Size:     Size{Width: 1000, Height: 560},
		MinSize:  Size{Width: 800, Height: 420},
		Layout:   VBox{Margins: Margins{Left: 6, Top: 6, Right: 6, Bottom: 6}, Spacing: 4},
		Children: []Widget{
			TabWidget{
				StretchFactor: 1,
				Pages:         []TabPage{setupPage(), optimizePage(), shortcutsPage()},
			},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					// Larghezza fissa: il testo cambia ogni secondo e la barra non deve spostarsi.
					Label{AssignTo: &statusLabel, Text: "Pronto.", Font: Font{Family: "Segoe UI", PointSize: 9, Bold: true},
						MinSize: Size{Width: 480}, MaxSize: Size{Width: 480}, EllipsisMode: EllipsisEnd,
						// Senza uno sfondo esplicito walk lascia a Windows il colore del testo (sempre nero).
						Background: SystemColorBrush{Color: walk.SysColorBtnFace}},
					ProgressBar{AssignTo: &progressBar, MaxValue: 100, MaxSize: Size{Height: 16}},
				},
			},
			TextEdit{
				AssignTo:   &logText,
				ReadOnly:   true,
				VScroll:    true,
				MinSize:    Size{Height: 64},
				MaxSize:    Size{Height: 64},
				Background: SolidColorBrush{Color: walk.RGB(255, 255, 255)},
			},
		},
	}.Create()
	if err != nil {
		return err
	}

	if ic := loadAppIcon(); ic != nil {
		mainWindow.SetIcon(ic)
	}
	fitToWorkArea()
	updateButtons()
	if startupJSONErr != nil {
		walk.MsgBox(mainWindow, "Errore configurazione",
			"Il file resources\\config.json non è valido:\n\n"+startupJSONErr.Error(), walk.MsgBoxIconWarning)
	}
	if logFilePath != "" {
		appendToLog("Pronto. Log anche su file: logs\\" + filepath.Base(logFilePath))
	}
	mainWindow.Run()
	return nil
}

func loadAppIcon() *walk.Icon {
	if exe, err := os.Executable(); err == nil {
		if ic, err := walk.NewIconExtractedFromFile(exe, 0, 0); err == nil {
			return ic
		}
	}
	ic, _ := walk.NewIconFromSysDLL("shell32", 23)
	return ic
}

// fitToWorkArea centra la finestra e la riduce se supera l'area utile (schermo meno barra delle applicazioni).
func fitToWorkArea() {
	const spiGetWorkArea = 0x0030
	var wa win.RECT
	if !win.SystemParametersInfo(spiGetWorkArea, 0, unsafe.Pointer(&wa), 0) {
		return
	}
	waW, waH := int(wa.Right-wa.Left), int(wa.Bottom-wa.Top)
	b := mainWindow.BoundsPixels()
	b.Width, b.Height = min(b.Width, waW), min(b.Height, waH)
	b.X = int(wa.Left) + (waW-b.Width)/2
	b.Y = int(wa.Top) + (waH-b.Height)/2
	mainWindow.SetBoundsPixels(b)
}

func scrollPage(title string, layout Layout, children ...Widget) TabPage {
	return TabPage{
		Title:  title,
		Layout: VBox{MarginsZero: true},
		Children: []Widget{
			// Niente HorizontalFixed: in walk uno ScrollView senza scorrimento orizzontale non si allarga
			// e resta centrato con bordi vuoti ai lati (difetto della versione precedente).
			ScrollView{Layout: layout, Children: children},
		},
	}
}

func column(children ...Widget) Composite {
	return Composite{Layout: VBox{MarginsZero: true, Spacing: 6}, Children: append(children, VSpacer{})}
}

// ---------------------------------------------------------------------------
// Tab "Setup PC"
// ---------------------------------------------------------------------------

func setupPage() TabPage {
	swChildren := []Widget{CheckBox{AssignTo: &chkInstallAdobe, Text: "Adobe Reader", Checked: true}}
	softwareChecks = make([]*walk.CheckBox, len(appConfig.Softwares))
	for i, sw := range appConfig.Softwares {
		swChildren = append(swChildren, CheckBox{AssignTo: &softwareChecks[i], Text: sw.Name, Checked: sw.Checked})
	}

	return scrollPage("Setup PC", VBox{Margins: Margins{Left: 6, Top: 6, Right: 6, Bottom: 6}, Spacing: 6},
		Composite{
			Layout: HBox{MarginsZero: true, Spacing: 8},
			Children: []Widget{
				column(
					group("1. Sistema e rete", HBox{},
						btn(nil, "ALTE PRESTAZIONI", func() { runTask("Prestazioni elevate", runHighPerformance) }),
						btn(nil, "CONFIGURA 802.1X", func() { runTask("Dot3Svc e profilo 802.1X", runStartDot3) }),
						btn(nil, "IMPOSTAZIONI DI RETE", func() { openNetworkSettings() }),
					),
					group("2. Software e applicazioni", VBox{Alignment: AlignHNearVCenter},
						Composite{Layout: Grid{Columns: 3, MarginsZero: true, Alignment: AlignHNearVCenter}, Children: swChildren},
						btn(nil, "INSTALLA SELEZIONATI", onInstallSoftware),
					),
					group("3. Microsoft Office Pro Plus 2016", VBox{},
						btn(nil, "INSTALLA E ATTIVA OFFICE 2016", func() { runTask("Installazione Office", runOfficeInstall) }),
					),
				),
				column(
					group("4. Nome PC e dominio", Grid{Columns: 4, Spacing: 6},
						Label{Text: "Admin dominio:"},
						LineEdit{AssignTo: &inDomainUser, Text: appConfig.DomainAdminUser},
						Label{Text: "Password:"},
						Composite{Layout: HBox{MarginsZero: true, Spacing: 4}, Children: []Widget{
							LineEdit{AssignTo: &inDomainPass, PasswordMode: true, OnTextChanged: updateButtons},
							CheckBox{AssignTo: &chkShowPass, Text: "Mostra", OnCheckedChanged: func() {
								inDomainPass.SetPasswordMode(!chkShowPass.Checked())
								inDomainPass.Invalidate() // EM_SETPASSWORDCHAR non ridisegna da solo
							}},
						}},
						Label{Text: "Nuovo nome PC:"},
						LineEdit{AssignTo: &inNewHostname, Text: currentHostname(), MaxLength: 15},
						Label{Text: "Dominio:"},
						LineEdit{AssignTo: &inDomainName, Text: appConfig.DomainName},
						Composite{ColumnSpan: 4, Layout: HBox{MarginsZero: true}, Children: []Widget{
							btn(nil, "RINOMINA PC", onRename),
							btn(&btnJoinDom, "AGGIUNGI A DOMINIO (con il nuovo nome)", onJoinDomain),
						}},
					),
					group("5. Altro", VBox{Spacing: 8},
						Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
							btn(nil, "INSTALLA SYSTEM CENTER", func() { runTask("Installazione System Center", runSCCMInstall) }),
							btn(nil, "AGGIORNAMENTI WINDOWS", func() { runTask("Windows Update", runWindowsUpdate) }),
						}},
						btn(&btnRetrust, "RE-TRUST DOMINIO (rimuovi e rimetti a dominio)", onRetrustDomain),
						Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
							Label{Text: "Utente:"},
							LineEdit{AssignTo: &inRDPUser},
							btn(&btnAddRDP, "AGGIUNGI A DESKTOP REMOTO", onAddRDPUser),
							btn(&btnShowRDP, "VEDI UTENTI AUTORIZZATI", onShowRDPUsers),
						}},
					),
				),
			},
		},
		btn(&btnAutoConfig, "★ CONFIGURAZIONE AUTOMATICA: prestazioni, software, Office, 802.1X, dominio, SCCM, Windows Update ★", onAutoConfig),
	)
}

func selectedSoftwareFiles() []string {
	var files []string
	if chkInstallAdobe.Checked() {
		files = append(files, adobeKey)
	}
	for i, sw := range appConfig.Softwares {
		if softwareChecks[i].Checked() {
			files = append(files, sw.Files...)
		}
	}
	return files
}

func onInstallSoftware() {
	files := selectedSoftwareFiles()
	if len(files) == 0 {
		warn("Nessun software selezionato.")
		return
	}
	runTask("Installazione software", func() { runSoftwareInstall(files) })
}

// domainInput legge e valida i campi del riquadro dominio.
func domainInput(needPass bool) (host, user, pass, domain string, ok bool) {
	host = strings.TrimSpace(inNewHostname.Text())
	user = strings.TrimSpace(inDomainUser.Text())
	pass = inDomainPass.Text()
	domain = strings.TrimSpace(inDomainName.Text())
	if valid, msg := isValidHostname(host); !valid {
		warn("Nome PC non valido: " + msg)
		return
	}
	if needPass && (user == "" || pass == "" || domain == "") {
		warn("Compilare admin di dominio, password e dominio.")
		return
	}
	ok = true
	return
}

func onRename() {
	host, user, pass, domain, ok := domainInput(false)
	if !ok {
		return
	}
	if !needsRename(host) {
		warn("Il nuovo nome coincide con quello attuale.")
		return
	}
	runTask("Rinomina PC", func() { runRenamePC(host, user, pass, domain) })
}

func onJoinDomain() {
	host, user, pass, domain, ok := domainInput(true)
	if !ok {
		return
	}
	runTask("Aggiunta a dominio", func() { runJoinDomain(host, user, pass, domain) })
}

func onRetrustDomain() {
	host, user, pass, domain, ok := domainInput(true)
	if !ok {
		return
	}
	runTask("Re-trust dominio", func() { runRetrustDomain(host, user, pass, domain) })
}

func onAddRDPUser() {
	domain := strings.TrimSpace(inDomainName.Text())
	user := strings.TrimSpace(inRDPUser.Text())
	if domain == "" || user == "" {
		warn("Compilare dominio e nome utente.")
		return
	}
	runTask("Aggiungi utente a Desktop Remoto", func() { runAddRDPUser(domain, user) })
}

func onShowRDPUsers() {
	runTask("Utenti desktop remoto", func() {
		list, err := listRDPUsers()
		if err != nil {
			appendToLog("ERRORE lettura utenti desktop remoto: " + err.Error())
			return
		}
		appendToLog("Utenti desktop remoto:\n" + list)
		mainWindow.Synchronize(func() {
			walk.MsgBox(mainWindow, "Utenti autorizzati a Desktop Remoto", list, walk.MsgBoxIconInformation)
		})
	})
}

func onAutoConfig() {
	host, user, pass, domain, ok := domainInput(true)
	if !ok {
		return
	}
	files := selectedSoftwareFiles()
	runTask("Configurazione automatica", func() {
		// Tratti della barra proporzionati alla durata tipica: Office e Windows Update sono i passi più lunghi.
		progressRange(0, 1)
		runHighPerformance()
		progressRange(1, 12)
		runSoftwareInstall(files)
		progressRange(12, 40)
		runOfficeInstall()
		progressRange(40, 41)
		runStartDot3()
		progressRange(41, 44)
		waitForDomain(domain)
		runJoinDomain(host, user, pass, domain)
		progressRange(44, 55)
		runSCCMInstall()
		progressRange(55, 100)
		runWindowsUpdate()
		appendToLog("Configurazione completata. Riavviare il PC.")
	})
}

// ---------------------------------------------------------------------------
// Tab "Ottimizzazione"
// ---------------------------------------------------------------------------

func tweakGroup(title string, list []tweak, checks *[]*walk.CheckBox, onClick walk.EventHandler) GroupBox {
	*checks = make([]*walk.CheckBox, len(list))
	children := make([]Widget, 0, len(list)+1)
	for i, t := range list {
		// Altezza massima: di suo walk dà 25 px a ogni checkbox e le 14 voci di "Pulizia" non starebbero in 560 px.
		children = append(children, CheckBox{AssignTo: &(*checks)[i], Text: t.label, Checked: t.on, MaxSize: Size{Height: 19}})
	}
	children = append(children, btn(nil, "APPLICA", onClick))
	return GroupBox{Title: title, Layout: VBox{Spacing: 2, Alignment: AlignHNearVCenter}, Children: children}
}

// runTweaks legge le spunte qui (thread UI) ed esegue le voci scelte nel task.
func runTweaks(name string, list []tweak, checks []*walk.CheckBox, defaultProfile bool, after func()) {
	var sel []tweak
	for i, t := range list {
		if checks[i].Checked() {
			sel = append(sel, t)
		}
	}
	if len(sel) == 0 {
		warn("Nessuna opzione selezionata.")
		return
	}
	runTask(name, func() {
		if defaultProfile {
			loadDefaultHive()
			defer unloadDefaultHive()
		}
		for i, t := range sel {
			appendToLog("• " + t.label)
			t.run()
			setProgress((i + 1) * 100 / len(sel))
		}
		if after != nil {
			after()
		}
	})
}

func optimizePage() TabPage {
	return scrollPage("Ottimizzazione PC", HBox{Margins: Margins{Left: 6, Top: 6, Right: 6, Bottom: 6}, Spacing: 8},
		column(tweakGroup("1. Pulizia generale", debloatTweaks, &debloatChecks, func() {
			runTweaks("Pulizia sistema", debloatTweaks, debloatChecks, true, nil)
		})),
		column(
			tweakGroup("2. Interfaccia (utente corrente e nuovi utenti)", uiTweaks, &uiChecks, func() {
				runTweaks("Interfaccia", uiTweaks, uiChecks, true, restartExplorer)
			}),
			tweakGroup("3. Sicurezza", securityTweaks, &securityChecks, func() {
				runTweaks("Sicurezza", securityTweaks, securityChecks, false, nil)
			}),
		),
		column(group("4. Rimozione bloatware", VBox{Spacing: 4},
			Label{Text: "App rimosse per tutti gli utenti (da config.json):"},
			TextEdit{Text: strings.Join(appConfig.Bloatware, "\r\n"), ReadOnly: true, VScroll: true, MinSize: Size{Height: 230}},
			btn(nil, "RIMUOVI BLOATWARE", func() {
				names := appConfig.Bloatware
				runTask("Rimozione bloatware", func() { runBloatwareRemover(names) })
			}),
		)),
	)
}

// ---------------------------------------------------------------------------
// Tab "Collegamenti"
// ---------------------------------------------------------------------------

func initShortcutDataFromConfig() {
	shortcutSections = nil
	for _, sec := range appConfig.Shortcuts {
		s := &ShortcutSection{Title: sec.Title}
		for _, it := range sec.Items {
			s.Items = append(s.Items, &ShortcutItem{Label: it.Label, URL: it.Url, Checked: it.Checked})
		}
		if strings.EqualFold(sec.Title, "Generica") {
			s.Items = append(s.Items, &ShortcutItem{Label: "Microsoft Office (Word/Excel)", URL: officeShortcutURL, Checked: true})
		}
		shortcutSections = append(shortcutSections, s)
	}
}

func shortcutsPage() TabPage {
	var sections []Widget
	for n, sec := range shortcutSections {
		var items []Widget
		for _, it := range sec.Items {
			it := it
			items = append(items, Composite{
				Layout: HBox{MarginsZero: true, Spacing: 2},
				Children: []Widget{
					CheckBox{AssignTo: &it.CheckBox, Text: it.Label, Checked: it.Checked, MaxSize: Size{Height: 20}},
					HSpacer{},
					PushButton{
						Text:        "➜",
						MinSize:     Size{Width: 24, Height: 20},
						MaxSize:     Size{Width: 24, Height: 20},
						ToolTipText: "Apri: " + it.Label,
						OnClicked:   func() { openShortcutTarget(it.URL) },
					},
				},
			})
		}
		// Riga e colonna esplicite: il builder di walk, dopo un figlio con layout Grid, va sempre a capo.
		sections = append(sections, GroupBox{
			Row:      n / 2,
			Column:   n % 2,
			Title:    sec.Title,
			Layout:   Grid{Columns: 2, Spacing: 2, Alignment: AlignHNearVNear},
			Children: items,
		})
	}
	return scrollPage("Collegamenti", VBox{Margins: Margins{Left: 6, Top: 6, Right: 6, Bottom: 6}, Spacing: 6},
		Composite{Layout: Grid{Columns: 2, MarginsZero: true, Spacing: 8}, Children: sections},
		btn(nil, "CREA COLLEGAMENTI SUL DESKTOP (tutti gli utenti)", onCreateShortcuts),
	)
}

func onCreateShortcuts() {
	var jobs []shortcutTarget
	for _, sec := range shortcutSections {
		for _, it := range sec.Items {
			if !it.CheckBox.Checked() {
				continue
			}
			targets := parseShortcutTargets(it.Label, resolveShortcutURL(it.URL))
			if len(targets) == 0 {
				appendToLog("Saltato " + it.Label + ": programma non installato.")
			}
			jobs = append(jobs, targets...)
		}
	}
	if len(jobs) == 0 {
		warn("Nessun collegamento selezionato.")
		return
	}
	runTask("Creazione collegamenti", func() { runCreateShortcuts(jobs) })
}
