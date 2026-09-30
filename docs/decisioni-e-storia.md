# scrap — decisioni e storia

> Perché scrap è fatto così: il contesto, le decisioni e la storia del progetto.
> Il funzionamento in dettaglio è nel [manuale tecnico](https://nic-fio.github.io/SCRAPER/Technical%20Manual.html).
> Ricavato dal diario di sviluppo di giugno 2026 (solo le parti tecniche) e
> aggiornato con la migrazione di settembre 2026.

---

## 1. L'idea

Punto di partenza:

> «Né wget né aria2 mi soddisfano. Quello che fa l'uno non fa l'altro. Voglio un
> piccolo binario autocontenuto che combini il meglio dei due mondi: la capacità
> di crawling di wget e la multi-segmentazione di aria2, più tanti filtri per
> farne un vero coltellino svizzero.»

`wget` sa copiare un sito seguendo i link ma scarica ogni file su una sola
connessione; `aria2` è velocissimo grazie alle connessioni multiple ma non
segue i link. scrap mette le due cose nello stesso eseguibile, con la stessa
sintassi.

| | `wget` | `aria2` | **`scrap`** |
|---|:---:|:---:|:---:|
| Download multi-connessione (range HTTP paralleli) | ✗ | ✓ | **✓** |
| Crawling / mirror ricorsivo di un sito | ✓ | ✗ | **✓** |
| Filtri (estensione, regex, dominio, dimensione, quota) | parziale | ✗ | **✓** |
| Ripresa e tentativi per segmento | ✓ | ✓ | **✓** |
| Binario singolo, zero dipendenze | ✗ | ✗ | **✓** |

## 2. Decisioni di design (prese prima di scrivere il codice)

- **Linguaggio: Go.** Un binario statico, nessuna dipendenza a runtime,
  compilazione incrociata banale per tutte le architetture.
- **Ambito della prima versione: tutto insieme** — download multi-segmento,
  crawler e filtri.
- **Filtri:** estensione e regex sull'URL, dimensione, content-type, dominio,
  profondità, quota totale, numero massimo di file.
- **Autenticazione:** HTTP Basic/Digest, login da modulo, token Bearer,
  intestazioni arbitrarie.
- **Cookie:** jar di sessione automatico, lettura/scrittura del formato
  Netscape (`cookies.txt`), cookie manuale.
- **Nome:** `scrap`, in tema con la cartella di lavoro originale
  (`/media/SCRAPER`), da cui anche il nome del repository.
- **Guida `--help` come interfaccia a schede** nel terminale, scritta da zero
  con la sola libreria standard (termios, schermo alternativo); fuori dal
  terminale diventa una pagina di manuale testuale.
- **User-Agent onesto** (`scrap/VERSIONE (+URL del progetto)`), come i bot ben
  educati, invece di fingersi un browser; resta cambiabile con `-U`.
- **Certificati radice incorporati** (`cacerts.pem` via `go:embed`), usati solo
  come riserva quando il sistema non ne ha: l'HTTPS funziona anche su sistemi
  minimi.
- **Parser HTML vero** (`golang.org/x/net/html`) invece delle espressioni
  regolari: niente falsi positivi dagli URL dentro il JavaScript, HTML
  malformato gestito. È l'unica dipendenza esterna.
- **Tentativi intelligenti:** backoff esponenziale con jitter, rispetto di
  `Retry-After`, nessun tentativo sugli errori permanenti (4xx).

## 3. Funzionalità provate nella prima sessione (giugno 2026)

- **Multi-segmentazione:** 8 segmenti che coprono esattamente il file,
  impronta SHA-256 corretta.
- **Ripresa:** processo ucciso a metà e ripreso, riscaricati solo i byte
  mancanti, integrità verificata.
- **Limite di banda globale** (token bucket condiviso).
- **Crawling ricorsivo e mirror:** albero `host/percorso` ricostruito, link
  seguiti.
- **Filtri:** `--reject`, `--max-size` e gli altri.
- **Autenticazione e cookie:** Basic, Bearer, intestazioni, cookie manuale,
  login da modulo con cattura e riuso della sessione.
- **TLS reale:** download da go.dev e da Cloudflare (3 MB a più segmenti).
- **Guida a schede** (Info/Download/Crawl/Filtri/Auth/Generali/Esempi),
  scorrevole, ridimensionabile.
- **Uscita dal vivo:** una barra per file, la riga dei segmenti
  (`└ seg ▰▰▱▱ …`), il riepilogo.

## 4. Bug trovati e risolti durante lo sviluppo

1. **Limite di banda moltiplicato per i segmenti.** Le attese parallele fuori
   dal lock non «prenotavano» i token, quindi la banda era circa N volte quella
   richiesta. Risolto con la prenotazione a deficit negativo (da 1,9 MB/s a
   circa 600 KB/s, come configurato).
2. **Tasti della TUI.** Una singola lettura con più byte (incolla, tasti
   rapidi) veniva interpretata solo per il primo tasto e la TUI si bloccava.
   Risolto interpretando *tutti* i tasti del buffer (`parseKeys`).
3. **Falso allarme 404 sui mirror Debian.** Indagato a fondo: scrap risolveva
   i link correttamente; i 404 dipendevano dall'invocazione, non da un bug.

## 5. Multi-piattaforma

Il codice dipendente dal sistema (termios, ioctl, segnali) è isolato in
`term_linux.go` (reale) e `term_other.go` (stub). Il programma si compila per
Linux, macOS e Windows; la TUI interattiva c'è solo su Linux, altrove `--help`
mostra la pagina testuale. Fino alla 1.1.0 le release contenevano nove binari
(sei architetture Linux, macOS, Windows); su richiesta dell'utente, dal
settembre 2026 sono solo due: Linux amd64 e arm64. Gli altri si costruiscono
dal sorgente.

## 6. Luglio 2026: robustezza

Dopo la prima pubblicazione sono arrivati (e sono rimasti a lungo non
registrati in git, fino alla migrazione): il parser HTML vero al posto delle
regex, i tentativi con backoff e `Retry-After` (`retry.go`), i certificati
incorporati (`roots.go`), `-qe`/`-qa` al posto di `-q`, `--auth`,
il salvataggio delle pagine HTML solo se passano i filtri, i primi test
automatici e `make dist` per sei architetture Linux.

## 7. Settembre 2026: migrazione in questo repository

Il progetto viveva solo sul tablet (una copia in `~/Video/scrap`, con 4 commit
e molte modifiche non registrate). Il vecchio repository pubblico non esisteva
più. Obiettivo: poter recuperare tutto con un `git clone` se il tablet si
guasta. Stesso impianto di NG-EFI_SHELL, hoster e mterm.

Decisioni dell'utente:

- **Repository pubblico `nic-fio/SCRAPER`**, manuali in italiano pubblicati con
  GitHub Pages.
- **Nessuna licenza** (tutti i diritti riservati), come hoster e mterm; la
  PolyForm Noncommercial della 1.0 non si applica più.
- **Storia conservata** ma con l'indirizzo email personale tolto, sia dalla
  firma dei commit sia dal testo dei file vecchi.
- **Del diario di sviluppo solo le parti tecniche** (questo file); materiale
  promozionale e vicende di pubblicazione restano fuori.
- **L'eseguibile nel repository** (`./scrap`, Linux x86-64), così il clone
  basta anche senza Go.

### Difetti trovati studiando il codice e corretti (versione 1.1)

Tutti con un test che fallisce sul codice 1.0:

- **File corrotti con una sola connessione.** Il `.part` era aperto in coda e
  ogni nuovo tentativo ripartiva dall'offset iniziale: dopo una caduta i dati
  venivano scritti due volte e il file corrotto veniva rinominato come
  completo (1.200.000 byte invece di 800.000 nel test).
- **`--timeout` come durata massima della richiesta.** Era il timeout totale
  del client HTTP: un segmento che durava più di 60 secondi falliva a ogni
  tentativo, quindi un'ISO da 4 GB a 10 MB/s non si poteva scaricare con i
  valori normali. Ora vale per connessione, risposta e trasferimento fermo.
- **Segmento servito per intero** (`200` invece di `206`) scritto nel punto
  sbagliato del file.
- **`--convert-links`** che con una sostituzione testuale in ordine casuale
  alterava gli indirizzi più lunghi.
- **Minori:** stato di uscita sempre 0; crash con una regex sbagliata o con
  `-j 0`; dimensioni e livello scritti male ignorati; `-qe`/`-qa` non
  silenziosi; `-o` con più indirizzi; `--page-requisites` accettata ma senza
  effetto; pagina salvata due volte nel mirror; `-c` che riscaricava i file
  completi; indirizzo non valido ritentato per un minuto e segnalato due volte.

## 8. Cose in sospeso

- Candidati v2 dal diario: `--page-requisites` cross-host completo,
  `--convert-links` più completo, proxy SOCKS, deduplicazione per impronta,
  statistiche in JSON.
- Gli altri punti aperti sono nel capitolo *Limiti noti* del manuale tecnico.
