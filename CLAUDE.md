# Lavorare a scrap

Come si sviluppa questo progetto: gli accordi, le decisioni già prese e cosa
controllare prima di registrare una modifica. Un clone più questo file sono
tutto il contesto di lavoro: niente di ciò che serve per continuare sta fuori
dal repository.

## Come si lavora

- **Si parla con l'utente in italiano, sempre.** L'utente non capisce
  l'inglese: niente frasi di passaggio in inglese, nemmeno brevi. Anche
  codice, commenti, messaggi del programma e documentazione sono in italiano.
- **Testi per l'utente senza gergo da programmatori** (preferenza esplicita):
  help, messaggi e manuale utente sono "copy di prodotto", non note tecniche.
- **Verificare, non dichiarare.** Ogni affermazione sul comportamento viene da
  un'esecuzione: i test, un download reale, un server locale. "Dovrebbe
  funzionare" non è un risultato.
- **La documentazione conta quanto il codice.** Una funzione non è finita
  finché i manuali non la descrivono.

## Decisioni prese: non riaprirle

Il perché di ciascuna è in [docs/decisioni-e-storia.md](docs/decisioni-e-storia.md).

| Decisione | |
|---|---|
| **Un solo binario statico** | `CGO_ENABLED=0`, certificati radice incorporati (`roots.go`). Unica libreria esterna: `golang.org/x/net` per il tokenizer HTML. |
| **Mai un file corrotto** | Si scrive in `.part` con `WriteAt` a posizione nota (mai in coda) e si rinomina solo a download completo. |
| **`--timeout` non limita la durata** | Vale per connessione, risposta e trasferimento fermo (`idleBody`), mai come `http.Client.Timeout`. |
| **Buon cittadino del web** | User-Agent onesto, `robots.txt` rispettato di default, niente tentativi sui 4xx, `Retry-After` rispettato. |
| **Stati di uscita** | 0 tutto scaricato, 1 almeno un indirizzo fallito o errore fatale, 2 opzioni sbagliate (`badArg`). |
| **L'eseguibile sta nel repository** | `./scrap` (Linux x86-64, statico) è registrato, così un `git clone` basta anche senza Go. `make` lo rigenera. |
| **Nessuna licenza** | Copyright nic-fio, tutti i diritti riservati; il codice è pubblico per poterlo leggere e recuperare. La PolyForm della 1.0 non si applica più. |
| **Mai dati personali nel repository** | Niente email personale (i commit usano l'indirizzo noreply), niente file di cookie. |
| **Manuali in italiano, tema chiaro** | Stesso impianto di NG-EFI_SHELL e hoster (HTML in `docs/`, GitHub Pages). |

## Il repository

| Dove | Cosa |
|---|---|
| `*.go` | Il programma (`package main`): vedi la mappa dei file nel manuale tecnico. |
| `*_test.go` | 32 test senza rete; `main_test.go` prova il programma intero in un sottoprocesso. |
| `docs/` | `manuale-utente.html`, `manuale-tecnico.html`, `decisioni-e-storia.md`, `assets/`. Pubblicati con GitHub Pages. |
| `tools/` | `setup-dev.sh` (pacchetti e identità git), `backup.sh` (bundle git), `check-docs.py` (controlli dei manuali). |
| `cacerts.pem` | Certificati radice incorporati; aggiornare con `curl -fsSL -o cacerts.pem https://curl.se/ca/cacert.pem`. |
| `scrap` | L'eseguibile Linux x86-64, **registrato**: va rigenerato con `make` e registrato insieme a ogni modifica del codice. |
| `dist/` | Binari per le release, mai registrati. |

## Prima di registrare una modifica

1. `make test`: `go vet`, `gofmt`, test con `-race` e i controlli dei manuali
   (ogni opzione definita in `main.go` deve comparire nella guida integrata e
   nel riferimento del manuale utente, ogni file `.go` nella mappa del manuale
   tecnico, la versione deve coincidere ovunque). **Fallisce se i manuali non
   sono aggiornati.**
2. Se hai cambiato il codice: `make` e registra anche `./scrap`, che deve
   corrispondere al sorgente (`check-docs.py` verifica che esista e che
   contenga la versione di `main.go`).
3. Ogni correzione ha il suo test, che deve fallire sul codice di prima.
4. Controlla che `git status` non mostri file di cookie o download. Mai
   `git add -A` senza guardare.
5. I commit usano l'identità locale impostata da `tools/setup-dev.sh`
   (indirizzo noreply di GitHub), che tiene fuori quello personale.

Rilascio: aggiorna `appVersion` in `main.go`, la versione in `README.md`,
`docs/index.html` e nei due manuali, `make`, poi crea un tag annotato `vX.Y.Z`
e fai push del tag: la CI costruisce i binari Linux amd64 e arm64 e pubblica la release, con
il messaggio del tag come note.

## A che punto è

Versione 1.1 (settembre 2026): migrazione nel repository pubblico con
correzione dei difetti trovati studiando il codice (elenco nel manuale
tecnico, capitolo *Limiti noti e punti aperti*). Punti aperti: nessun `fsync`
dello stato di ripresa, un goroutine per URL nei crawl enormi, `robots.txt`
solo a prefissi, login da modulo senza token anti-CSRF.
