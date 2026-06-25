# scrap

[![release](https://img.shields.io/github/v/release/nicfio/Scraper)](https://github.com/nicfio/Scraper/releases/latest)
[![license: PolyForm Noncommercial](https://img.shields.io/badge/license-PolyForm%20Noncommercial-blue)](LICENSE)
![platforms](https://img.shields.io/badge/platforms-linux%20%7C%20macOS%20%7C%20windows-lightgrey)

**Il coltellino svizzero del download.** Un singolo binario statico (~6 MB, zero
dipendenze) che combina **download multi-segmento**, **crawling ricorsivo** dei
siti e una batteria di **filtri** per scegliere esattamente cosa scaricare.

> 🇬🇧 *The Swiss-army knife of downloading — a single zero-dependency static binary
> that combines a **multi-connection download manager** (parallel HTTP ranges,
> resume, rate-limit) with a **recursive website crawler/mirror** and powerful
> **filters**. A modern command-line **alternative to `wget` and `aria2`** that
> rolls both into one tool. Linux · macOS · Windows.*

> ⚖️ **Licenza:** software *source-available* sotto **PolyForm Noncommercial
> 1.0.0** — libero per qualsiasi uso **non commerciale**; per l'uso commerciale
> serve una licenza a pagamento. Vedi [Licenza](#licenza).

## Download

Binari precompilati, nessun Go richiesto → **[Releases](https://github.com/nicfio/Scraper/releases/latest)**

| Sistema | Architettura | |
|---|---|---|
| **Linux** | x86-64 · ARM64 | [scrap-linux-amd64](https://github.com/nicfio/Scraper/releases/latest/download/scrap-linux-amd64) · [scrap-linux-arm64](https://github.com/nicfio/Scraper/releases/latest/download/scrap-linux-arm64) |
| **macOS** | Intel · Apple Silicon | [scrap-darwin-amd64](https://github.com/nicfio/Scraper/releases/latest/download/scrap-darwin-amd64) · [scrap-darwin-arm64](https://github.com/nicfio/Scraper/releases/latest/download/scrap-darwin-arm64) |
| **Windows** | x86-64 | [scrap-windows-amd64.exe](https://github.com/nicfio/Scraper/releases/latest/download/scrap-windows-amd64.exe) |

```sh
chmod +x scrap-linux-amd64 && ./scrap-linux-amd64 --help
```

Verifica l'integrità con [`SHA256SUMS`](https://github.com/nicfio/Scraper/releases/latest/download/SHA256SUMS): `sha256sum -c SHA256SUMS`.

## Demo

**Guida interattiva** (`scrap --help`) — una TUI a schede, navigabile e scrollabile:

```text
┌─ scrap — guida ──────────────────────────────────────────────────────────────┐
│  Info  Download  Crawl  Filtri  Auth  Generali  Esempi                       │
├──────────────────────────────────────────────────────────────────────────────┤
│ NOME                                                                         │
│    scrap — il coltellino svizzero del download                               │
│                                                                              │
│ SINTASSI                                                                     │
│    scrap [opzioni] URL [URL...]                                              │
│    scrap [opzioni] -i lista.txt                                              │
│                                                                              │
│ DESCRIZIONE                                                                  │
│    scrap è uno strumento da riga di comando che combina lo scaricamento      │
│    multi-segmento (più connessioni parallele per ogni file), il crawling     │
│    ricorsivo dei siti e una ricca batteria di filtri per selezionare con     │
│    precisione cosa scaricare.                                                │
├──────────────────────────────────────────────────────────────────────────────┤
│ ←/→ schede · ↑/↓ scorri · PgUp/PgDn · g/G · q esci             righe 1-15/15 │
└──────────────────────────────────────────────────────────────────────────────┘
```

**Download in corso** — una barra per file, con i segmenti che si riempiono in parallelo:

```text
 ⬇ medium.dat          ▕██████████▎░░░░░▏  64%  503K/781K  17.0K/s  16s
 ⬇ big.bin             ▕████████▌░░░░░░░▏  53%  2.7M/5.0M  508K/s  5s
    └ seg ▰▰▱▱ ▰▰▱▱ ▰▰▰▰ ▰▰▱▱ ▰▰▱▱ ▰▰▱▱  (6 connessioni)
────────────────────────────────────────────────────────────────────────
 2 attivi · 0 fatti · 3.2M · 525K/s
```

## Perché scrap? (vs wget e aria2)

`wget` sa fare crawling ricorsivo ma scarica ogni file su una singola connessione;
`aria2` è velocissimo grazie al multi-connessione ma non fa mirroring di un sito.
Di solito ti servono **entrambi** — e due tool diversi, con due sintassi diverse.

`scrap` mette le due cose nello stesso binario: il **download multi-connessione**
in stile aria2 **e** il **crawling/mirror ricorsivo** in stile wget, più una
batteria di filtri, autenticazione e cookie. Un solo eseguibile statico, zero
dipendenze runtime, stessa sintassi per tutto.

| | `wget` | `aria2` | **`scrap`** |
|---|:---:|:---:|:---:|
| Download multi-connessione (range HTTP paralleli) | ✗ | ✓ | **✓** |
| Crawling / mirror ricorsivo di un sito | ✓ | ✗ | **✓** |
| Filtri (estensione, regex, dominio, dimensione, quota) | parziale | ✗ | **✓** |
| Resume + retry per-segmento | ✓ | ✓ | **✓** |
| Binario singolo, zero dipendenze | ✗ | ✗ | **✓** |

## Cosa fa

- **Multi-segmentazione**: spezza un file in N range HTTP scaricati in parallelo,
  con resume, retry per-segmento e rate-limit globale.
- **Crawling ricorsivo**: segue i link (`a`, `img`, `link`, `script`, `srcset`,
  CSS `url()`), ricostruisce l'albero delle cartelle, rispetta `robots.txt`,
  opzionalmente converte i link per la navigazione offline.
- **Filtri** (il coltellino): per estensione, regex sull'URL, dominio,
  content-type, dimensione, profondità, quota totale e numero massimo di file.
- **Output live**: una barra di avanzamento per ogni download attivo, con
  percentuale, velocità ed ETA, e — per i file multi-segmento — una riga che
  mostra i singoli segmenti riempirsi in parallelo. Fuori dal terminale (pipe,
  log) stampa righe semplici.
- **Autenticazione & cookie**: HTTP Basic/Digest, login via form (cattura il
  cookie di sessione), token Bearer, header arbitrari, cookie manuali e
  load/save in formato Netscape `cookies.txt`.

## Build

Richiede solo il toolchain Go (≥ 1.21):

```sh
cd scrap
CGO_ENABLED=0 go build -ldflags "-s -w" -o scrap .
```

Il risultato è un eseguibile statico autocontenuto: copialo dove vuoi
(`sudo install -m755 scrap /usr/local/bin/`).

## Esempi

```sh
# Download multi-segmento (8 connessioni) con resume
scrap -s 8 -c https://example.com/file.iso

# Mirror completo di un sito, link offline
scrap -m --convert-links https://sito.example/

# Crawl solo immagini e PDF fino a 5 MB, max 200 file
scrap -r -l 3 -A jpg,png,pdf --max-size 5M --max-files 200 https://sito/

# Scarica un'intera lista, 6 file in parallelo, banda capata a 2 MB/s
scrap -i urls.txt -j 6 --rate 2M -d ./downloads

# Area riservata: login via form e riuso della sessione
scrap --login-url https://sito/login --login-data 'user=foo&pass=bar' \
      --save-cookies cj.txt -m https://sito/area/
scrap --load-cookies cj.txt https://sito/area/altro

# API con token Bearer
scrap --bearer "$TOKEN" -o data.json https://api.sito/v1/export
```

## Guida interattiva

`scrap --help` (o `-h`, o senza argomenti) apre una **TUI a schede** a tutto
schermo, navigabile da tastiera:

- `←/→` (o `Tab`, o i tasti `1`–`9`) cambia scheda
- `↑/↓`, `PgUp/PgDn`, `g/G` scorrono il contenuto
- `q` o `Esc` esce

È scritta interamente con la libreria standard (raw mode via termios, schermo
alternativo, gestione del ridimensionamento). Quando l'output non è un terminale
(es. `scrap --help | less`) viene invece stampata una pagina di manuale testuale.

## Note

- In modalità ricorsiva ricostruisce l'albero `host/percorso`; di default resta
  sull'host di partenza (`--span-hosts` per uscirne).
- `-o` è il percorso esatto del file di output; per scegliere solo la cartella
  usa `-d`.
- `--convert-links` riscrive i link assoluti verso i file scaricati: è una
  conversione di base, sufficiente per la maggior parte dei mirror.

## Licenza

Copyright © 2026 **Nicola Fiorillo**.

`scrap` è rilasciato con licenza **[PolyForm Noncommercial 1.0.0](LICENSE)**, una
licenza *source-available*:

- ✅ **Libero** per ogni uso **non commerciale**: uso personale, studio, ricerca,
  progetti hobbistici, enti no-profit, scuole, pubblica amministrazione.
- 💼 **Uso commerciale**: richiede una **licenza commerciale a pagamento**.

Per una licenza commerciale, scrivi a **Nicola Fiorillo — github.com/nic-fio**.

> Nota: PolyForm Noncommercial *non* è una licenza open source secondo la
> definizione OSI (che impone di consentire anche l'uso commerciale). Il codice
> è pubblico e modificabile, ma l'uso commerciale è riservato.

## Autore

**Nicola Fiorillo** · github.com/nic-fio · [github.com/nicfio](https://github.com/nicfio)

---

<sub>*Keywords: wget alternative · aria2 alternative · multi-connection / segmented
download manager · parallel HTTP range downloader · recursive website crawler &
mirror · CLI download tool · single static Go binary · resume, rate-limit, filters
· Linux, macOS, Windows.*</sub>
