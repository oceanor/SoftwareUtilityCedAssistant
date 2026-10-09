# SoftwareUtilityCedAssistant

Utility Windows (Go) per la configurazione automatica di PC nuovi: Office, software, bloatware, collegamenti, 802.1X, dominio.

## Configurazione

1. Copia `resources/config.example.json` in `resources/config.json` e compilalo (chiave Office, dominio, utente admin, elenchi). Al primo avvio senza file ne viene creato uno con valori di esempio.
2. Copia `resources/xtmp/Client/installa.bat.example` in `installa.bat` con i valori del tuo server SCCM.
3. Metti ISO e installer nelle cartelle indicate nel config (`resources/softwares/`).

`config.json`, `installa.bat`, eseguibili, installer e `logs/` sono ignorati da git: non vanno committati.

## Build

`compile.bat` (richiede Go).
