# scrap

[![CI](https://github.com/nic-fio/SCRAPER/actions/workflows/ci.yml/badge.svg)](https://github.com/nic-fio/SCRAPER/actions/workflows/ci.yml)
[Documentazione](https://nic-fio.github.io/SCRAPER/) · [Download](https://github.com/nic-fio/SCRAPER/releases/latest)

**Il coltellino svizzero del download.** Un programma da terminale, un **unico
eseguibile** senza niente da installare, che unisce tre cose: **scaricare file
a più connessioni** (con ripresa e nuovi tentativi), **copiare siti interi**
seguendo i link e una batteria di **filtri** per scegliere esattamente cosa
tenere.

```
$ scrap -s 8 https://esempio.it/debian.iso
 ⬇ debian.iso          ▕██████████▎░░░░░░▏  64%  503M/781M  17.0M/s  16s
    └ seg ▰▰▱▱ ▰▰▰▱ ▰▰▰▰ ▰▰▱▱ ▰▰▱▱ ▰▰▰▱ ▰▰▰▰ ▰▰▱▱  (8 connessioni)
────────────────────────────────────────────────────────────────────────
 1 attivi · 0 fatti · 503M · 17.0M/s
```

## In breve

- **Più connessioni per file**: divide un file in N pezzi scaricati insieme;
  ripresa con `-c`, nuovi tentativi automatici, limite di banda.
- **Copia di siti** (`-r`, `-m`): segue i link, ricostruisce le cartelle,
  rispetta `robots.txt`, converte i link per la consultazione senza rete.
- **Filtri**: estensione, espressione regolare, sito, dimensione, tipo di
  contenuto, numero di file, quota totale.
- **Accesso riservato**: utente e password (Basic/Digest), login da modulo,
  token, intestazioni, cookie in formato `cookies.txt`.
- **Guida a schede** nel terminale: `scrap --help`.

## Documentazione

| Documento | Per |
|---|---|
| [Manuale utente](https://nic-fio.github.io/SCRAPER/User%20Manual.html) | Installare e usare scrap: scaricare, copiare siti, filtri, accesso riservato, problemi e soluzioni, tutte le opzioni. |
| [Manuale tecnico](https://nic-fio.github.io/SCRAPER/Technical%20Manual.html) | Come è fatto dentro: architettura, motore di download, crawler, filtri, test, limiti noti. |
| [Decisioni e storia](docs/decisions-and-history.md) | Perché scrap è fatto così. |
| [Logo](logos/scraper-logo.png) | Il logo del progetto (PNG 2172×724, sfondo bianco). |

I manuali, in inglese, sono pagine HTML che GitHub mostra come codice sorgente: leggili
online su **https://nic-fio.github.io/SCRAPER/**, oppure apri
`docs/User Manual.html` nel browser da un clone (funzionano anche senza rete).

## Installazione

**Il programma pronto è nel repository**: il file `scrap` (Linux x86-64, PC e
tablet) è già compilato. Clonare basta:

```
git clone https://github.com/nic-fio/SCRAPER.git
cd SCRAPER
./scrap --help
```

**Solo il programma, senza clonare**: dall'[ultima release](https://github.com/nic-fio/SCRAPER/releases/latest)
scarica `scrap-linux-amd64` (PC e tablet x86-64) o `scrap-linux-arm64`
(Raspberry Pi 4/5 e altri ARM a 64 bit), poi:

```
mv scrap-linux-amd64 scrap && chmod +x scrap
./scrap --help
```

**Dal sorgente** (serve Go 1.23 o successivo):

```
git clone https://github.com/nic-fio/SCRAPER.git
cd SCRAPER
tools/setup-dev.sh --install    # pacchetti (chiede sudo) e identità git
make && make test               # ricostruisce ./scrap, poi tutti i controlli
```

## Recupero dopo un guasto

Il repository contiene **tutto**: l'eseguibile pronto, sorgenti, test,
manuali, strumenti, la storia completa. Per ripartire su un altro computer o
tablet:

```
git clone https://github.com/nic-fio/SCRAPER.git
cd SCRAPER && ./scrap --help
```

Per ricostruirlo dal sorgente: `tools/setup-dev.sh --install && make test`.
`tools/backup.sh` crea anche un backup su file, che si ripristina senza rete. I
file di cookie e i download non sono nel repository, per scelta. I dettagli
sono nel capitolo *Recupero su un nuovo dispositivo* del manuale utente.

## Uso

```
scrap -s 8 -c https://esempio.it/file.iso              # 8 connessioni, con ripresa
scrap -i lista.txt -j 6 --rate 2M -d ./dl              # una lista, 6 alla volta, max 2 MB/s
scrap -m --convert-links https://esempio.it/           # copia del sito, navigabile offline
scrap -r -A jpg,png,pdf --max-size 5M https://esempio.it/   # solo immagini e PDF fino a 5 MB
scrap --help                                           # guida a schede
```

Stato di uscita: 0 tutto scaricato, 1 almeno un indirizzo fallito, 2 opzioni
sbagliate.

## Lavorare al progetto

[CLAUDE.md](CLAUDE.md) raccoglie come si lavora al progetto: gli accordi, le
decisioni già prese e cosa controllare prima di registrare una modifica.

## Copyright

Copyright (c) 2026 nic-fio. **Tutti i diritti riservati**: il codice è
pubblico perché si possa leggere e recuperare, ma non è concessa alcuna
licenza d'uso, copia o modifica. I componenti di terzi mantengono le proprie
licenze, elencate in [NOTICE.md](NOTICE.md).

## Stato

Versione 1.1. Rispetto alla 1.0 corregge un difetto che poteva produrre file
corrotti nei download a una connessione e il timeout che faceva fallire i
download lunghi, più diversi difetti minori (elenco nel manuale tecnico).
32 test automatici che non usano la rete; provato dal vivo in HTTPS su go.dev
(impronta SHA-256 verificata).
