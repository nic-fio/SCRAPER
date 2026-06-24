package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// codici ANSI
const (
	aReset = "\033[0m"
	aBold  = "\033[1m"
	aDim   = "\033[2m"
	aUnder = "\033[4m"
	aCyan  = "\033[36m"
	aGreen = "\033[32m"
	aYel   = "\033[33m"
)

type helpRow struct{ flag, desc string }
type helpSection struct {
	title string
	rows  []helpRow
}

// gruppi di opzioni (diventano sotto-titoli dentro OPZIONI)
var helpSections = []helpSection{
	{"Download e multi-segmentazione", []helpRow{
		{"-s, --split N", "Scarica ogni file in N segmenti paralleli (default 4)."},
		{"-j, --jobs N", "Numero di file scaricati contemporaneamente (default 4)."},
		{"--min-split SIZE", "Non segmentare file più piccoli di SIZE (default 1M)."},
		{"-o, --output FILE", "Salva con questo nome (solo con un singolo URL)."},
		{"-d, --dir DIR", "Cartella di destinazione (default la corrente)."},
		{"-c, --continue", "Riprende un download interrotto da dove si era fermato."},
		{"--retries N", "Tentativi per ciascun segmento prima di rinunciare (default 5)."},
		{"--rate SIZE", "Limita la banda complessiva (es. 2M = 2 MiB/s)."},
		{"--timeout SEC", "Timeout di connessione e lettura (default 60)."},
	}},
	{"Crawling", []helpRow{
		{"-r, --recursive", "Segue ricorsivamente i link delle pagine HTML."},
		{"-l, --level N", "Profondità massima del crawl ('inf' = illimitata, default 5)."},
		{"-m, --mirror", "Mirror completo: equivale a -r -l inf più la struttura di cartelle."},
		{"--no-parent", "Non risale alle cartelle superiori a quella di partenza."},
		{"--span-hosts", "Permette di seguire link verso host diversi da quello iniziale."},
		{"--page-requisites", "Scarica le risorse necessarie a mostrare la pagina."},
		{"--convert-links", "Riscrive i link verso i file scaricati per la consultazione offline."},
		{"--robots=false", "Ignora robots.txt (di default viene rispettato)."},
	}},
	{"Filtri", []helpRow{
		{"-A, --accept LISTA", "Estensioni da accettare, separate da virgola (es. jpg,png,pdf)."},
		{"-R, --reject LISTA", "Estensioni da escludere, separate da virgola."},
		{"--accept-re REGEX", "Scarica solo gli URL che corrispondono alla regex."},
		{"--reject-re REGEX", "Scarta gli URL che corrispondono alla regex."},
		{"--domains LISTA", "Limita il crawl agli host indicati."},
		{"--exclude-domains LISTA", "Esclude gli host indicati."},
		{"--min-size SIZE", "Salta i file più piccoli di SIZE (verificato via HEAD)."},
		{"--max-size SIZE", "Salta i file più grandi di SIZE (verificato via HEAD)."},
		{"--types LISTA", "Content-type ammessi (es. image/*,application/pdf)."},
		{"--max-files N", "Si ferma dopo aver scaricato N file."},
		{"--quota SIZE", "Si ferma dopo aver scaricato SIZE byte in totale."},
	}},
	{"Autenticazione e cookie", []helpRow{
		{"--user UTENTE", "Nome utente per l'autenticazione HTTP Basic o Digest."},
		{"--password PASS", "Password per l'autenticazione HTTP Basic o Digest."},
		{"--login-url URL", "Esegue un login via form POST a questo URL prima di iniziare."},
		{"--login-data DATI", "Campi del form di login (es. user=foo&pass=bar)."},
		{"--bearer TOKEN", "Aggiunge l'intestazione Authorization: Bearer TOKEN."},
		{"-H, --header 'K: V'", "Aggiunge un'intestazione HTTP arbitraria (ripetibile)."},
		{"--cookie 'k=v; ..'", "Invia un'intestazione Cookie costruita a mano."},
		{"--load-cookies FILE", "Carica i cookie da un file in formato Netscape (cookies.txt)."},
		{"--save-cookies FILE", "Salva i cookie di sessione in un file Netscape al termine."},
	}},
	{"Generali", []helpRow{
		{"-U, --user-agent UA", "Imposta lo User-Agent (default scrap/1.0)."},
		{"--referer URL", "Imposta l'intestazione Referer."},
		{"--insecure", "Non verifica i certificati TLS dei server."},
		{"-q, --quiet", "Modalità silenziosa: mostra solo gli errori."},
		{"-v, --verbose", "Mostra un log dettagliato delle operazioni."},
		{"-i, --input-file FILE", "Legge l'elenco degli URL da un file (uno per riga)."},
	}},
}

type helpExample struct{ cmd, note string }

var helpExamples = []helpExample{
	{"scrap -s 8 -c https://host/file.iso", "Scarica un file in 8 segmenti, con ripresa se interrotto."},
	{"scrap -m --convert-links https://sito/", "Crea un mirror completo del sito, navigabile offline."},
	{"scrap -r -A jpg,png,pdf --max-size 5M https://sito/", "Raccoglie solo immagini e PDF fino a 5 MiB."},
	{"scrap -i urls.txt -j 6 --rate 2M -d ./dl", "Scarica una lista di URL, 6 alla volta, a 2 MiB/s."},
	{"scrap --login-url https://s/login --login-data 'u=a&p=b' -m https://s/area/", "Accede a un'area riservata via form e ne fa il mirror."},
}

const descrizione = "scrap è uno strumento da riga di comando che combina lo scaricamento " +
	"multi-segmento (più connessioni parallele per ogni file), il crawling ricorsivo dei siti " +
	"e una ricca batteria di filtri per selezionare con precisione cosa scaricare. Produce un " +
	"singolo binario autocontenuto, senza dipendenze esterne. Ogni URL indicato viene scaricato; " +
	"con -r o -m, le pagine HTML vengono esplorate e i link seguiti secondo i filtri impostati."

func col(color bool, code, s string) string {
	if !color {
		return s
	}
	return code + s + aReset
}

// threeCol compone una riga a tre colonne (sinistra, centro, destra) come le
// testate delle pagine man.
func threeCol(left, mid, right string, width int) string {
	if width < len(left)+len(mid)+len(right)+2 {
		width = len(left) + len(mid) + len(right) + 2
	}
	gapTot := width - len(left) - len(mid) - len(right)
	l := gapTot / 2
	r := gapTot - l
	return left + strings.Repeat(" ", l) + mid + strings.Repeat(" ", r) + right
}

// wrapText manda a capo il testo a una certa larghezza, con rientro.
func wrapText(s string, width, indent int) []string {
	pad := strings.Repeat(" ", indent)
	avail := width - indent
	if avail < 24 {
		avail = 24
	}
	var lines []string
	cur := ""
	for _, w := range strings.Fields(s) {
		switch {
		case cur == "":
			cur = w
		case len(cur)+1+len(w) <= avail:
			cur += " " + w
		default:
			lines = append(lines, pad+cur)
			cur = w
		}
	}
	if cur != "" {
		lines = append(lines, pad+cur)
	}
	return lines
}

// buildHelp produce la pagina di manuale, con o senza colori ANSI.
func buildHelp(color bool, width int) string {
	if width < 60 {
		width = 60
	}
	if width > 100 {
		width = 100
	}
	const ti = 7  // rientro corpo sezione (tag)
	const di = 14 // rientro descrizioni (.TP)

	var b strings.Builder
	w := func(s string) { b.WriteString(s) }
	hdr := func(s string) { w("\n" + col(color, aBold, strings.ToUpper(s)) + "\n") }
	sub := func(s string) { w("\n" + strings.Repeat(" ", ti) + col(color, aBold+aYel, s) + "\n") }
	para := func(s string, ind int) {
		for _, l := range wrapText(s, width, ind) {
			w(l + "\n")
		}
	}
	// voce .TP: tag (grassetto) su una riga, descrizione rientrata sotto
	tp := func(tag, desc string) {
		w(strings.Repeat(" ", ti) + col(color, aBold+aGreen, tag) + "\n")
		para(desc, di)
	}

	// testata
	w(col(color, aBold, threeCol("SCRAP(1)", "Manuale di scrap", "SCRAP(1)", width)) + "\n")

	hdr("Nome")
	para("scrap — il coltellino svizzero del download", ti)

	hdr("Sintassi")
	syn := func(parts ...string) {
		w(strings.Repeat(" ", ti) +
			col(color, aBold, "scrap") + " " +
			col(color, aDim, "[opzioni]") + " ")
		for i, p := range parts {
			if i > 0 {
				w(" ")
			}
			w(col(color, aUnder, p))
		}
		w("\n")
	}
	syn("URL", "[URL...]")
	syn("-i", "lista.txt")

	hdr("Descrizione")
	para(descrizione, ti)

	hdr("Opzioni")
	for _, sec := range helpSections {
		sub(sec.title)
		for _, r := range sec.rows {
			tp(r.flag, r.desc)
		}
	}

	hdr("Esempi")
	for _, ex := range helpExamples {
		tp(ex.cmd, ex.note)
	}

	hdr("File")
	tp("FILE.part", "File parziale creato durante il download; rinominato a FILE al termine.")
	tp("FILE.part.scrap", "Metadati di ripresa (offset dei segmenti) usati da --continue.")
	tp("cookies.txt", "Formato Netscape letto/scritto da --load-cookies e --save-cookies.")

	hdr("Ambiente")
	tp("PAGER", "Pager usato per impaginare questa guida (default: less).")
	tp("NO_COLOR", "Se impostata, disabilita i colori nell'output.")
	tp("HTTP_PROXY, HTTPS_PROXY, NO_PROXY", "Proxy HTTP/HTTPS e relative eccezioni.")

	hdr("Stato di uscita")
	tp("0", "Completato con successo.")
	tp("1", "Errore durante l'esecuzione.")
	tp("2", "Argomenti o opzioni non validi.")

	hdr("Vedere anche")
	para(col(color, aBold, "curl")+"(1)", ti)

	w("\n" + col(color, aBold, threeCol("scrap 1.0", "", "SCRAP(1)", width)) + "\n")
	return b.String()
}

var reANSI = regexp.MustCompile(`\033\[[0-9;]*m`)

func stripANSI(s string) string { return reANSI.ReplaceAllString(s, "") }

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// termWidth rileva la larghezza del terminale (colonne), con fallback a 80.
func termWidth() int {
	w, _ := termSize()
	if w <= 0 {
		return 80
	}
	return w
}

// displayHelp mostra la guida. In un terminale avvia la TUI interattiva a
// schede; se non è possibile, ripiega su un pager (man-style); fuori dal
// terminale stampa testo semplice.
func displayHelp() {
	if !isTTY(os.Stdout) || !isTTY(os.Stdin) {
		fmt.Print(stripANSI(buildHelp(false, 80)))
		return
	}
	if err := runTUI(); err == nil {
		return
	}
	// fallback: pager con la pagina man-style colorata
	color := os.Getenv("NO_COLOR") == ""
	text := buildHelp(color, termWidth())
	cmd := pagerCommand()
	if cmd == nil {
		fmt.Print(text)
		return
	}
	cmd.Stdin = strings.NewReader(text)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Print(text)
	}
}

// pagerCommand sceglie il pager: $PAGER, poi less (a colori, non distruttivo),
// poi more. Restituisce nil se nessuno è disponibile.
func pagerCommand() *exec.Cmd {
	if p := os.Getenv("PAGER"); p != "" {
		return exec.Command("sh", "-c", p)
	}
	if p, err := exec.LookPath("less"); err == nil {
		return exec.Command(p, "-R", "-F", "-X")
	}
	if p, err := exec.LookPath("more"); err == nil {
		return exec.Command(p)
	}
	return nil
}
