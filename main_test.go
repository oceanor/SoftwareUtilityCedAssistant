package main

import (
	"strings"
	"testing"
)

func TestIsValidHostname(t *testing.T) {
	for name, want := range map[string]bool{
		"PC-CED-01": true, "ab": true, "": false, "-PC": false, "PC-": false,
		"1234": false, "NOMETROPPOLUNGO16": false, "PC_01": false, "PC 01": false,
	} {
		if got, _ := isValidHostname(name); got != want {
			t.Errorf("isValidHostname(%q) = %v, atteso %v", name, got, want)
		}
	}
}

func TestExtractDriveLetter(t *testing.T) {
	for in, want := range map[string]string{"E": "E", " f\r\n": "F", "": "", "123": ""} {
		if got := extractDriveLetter(in); got != want {
			t.Errorf("extractDriveLetter(%q) = %q, atteso %q", in, got, want)
		}
	}
}

func TestFindGUID(t *testing.T) {
	out := "GUID combinazione per il risparmio di energia: 1c1f7e2a-6b2d-4f7e-9a3b-0c1d2e3f4a5b  (Prestazioni elevate)\r\n"
	if got := findGUID(out); got != "1c1f7e2a-6b2d-4f7e-9a3b-0c1d2e3f4a5b" {
		t.Errorf("findGUID = %q", got)
	}
	if findGUID("Parametro non valido") != "" {
		t.Error("findGUID deve restituire stringa vuota senza GUID")
	}
}

func TestParseShortcutTargets(t *testing.T) {
	got := parseShortcutTargets("Office", `Microsoft Word>C:\Word.exe|Microsoft Excel>C:\Excel.exe`)
	if len(got) != 2 || got[0] != (shortcutTarget{"Microsoft Word", `C:\Word.exe`}) || got[1].label != "Microsoft Excel" {
		t.Errorf("targets multipli: %+v", got)
	}
	got = parseShortcutTargets("Webmail", "https://webmail.example/login.php")
	if len(got) != 1 || got[0] != (shortcutTarget{"Webmail", "https://webmail.example/login.php"}) {
		t.Errorf("URL semplice: %+v", got)
	}
	if len(parseShortcutTargets("x", "")) != 0 {
		t.Error("URL vuoto (Office non installato) deve dare zero target")
	}
}

func TestBloatwareScript(t *testing.T) {
	s := bloatwareScript([]string{"Netflix", "King's"})
	if !strings.Contains(s, `-like "*$n*"`) {
		t.Error("il confronto deve essere per sottostringa")
	}
	if strings.Count(s, "Get-AppxProvisionedPackage") != 1 {
		t.Error("Get-AppxProvisionedPackage va chiamato una sola volta")
	}
	if !strings.Contains(s, "'Netflix','King''s'") {
		t.Errorf("nomi non quotati correttamente: %s", strings.SplitN(s, "\n", 2)[0])
	}
}

func TestSanitizeFilename(t *testing.T) {
	for in, want := range map[string]string{
		"Home Page Servizio Inf.": "Home Page Servizio Inf",
		`A/B:C*D`:                 "A_B_C_D",
		"ALPI - Calcolo Tariffe":  "ALPI - Calcolo Tariffe",
	} {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitizeFilename(%q) = %q, atteso %q", in, got, want)
		}
	}
}

func TestCredUser(t *testing.T) {
	for _, c := range [][3]string{
		{"admin", "dom.it", `dom.it\admin`},
		{`DOM\admin`, "dom.it", `DOM\admin`},
		{"admin@dom.it", "dom.it", "admin@dom.it"},
		{"admin", "", "admin"},
	} {
		if got := credUser(c[0], c[1]); got != c[2] {
			t.Errorf("credUser(%q,%q) = %q, atteso %q", c[0], c[1], got, c[2])
		}
	}
}
