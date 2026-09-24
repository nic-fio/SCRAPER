package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var stderr = os.Stderr

// gQuietAll, impostato da -qa, sopprime ogni output, errori inclusi.
var gQuietAll bool

// Identità dell'applicazione, centralizzata in un solo punto.
const (
	appName    = "scrap"
	appVersion = "1.1"
	appURL     = "https://github.com/nic-fio/SCRAPER"
)

// defaultUserAgent identifica onestamente lo scraper con la convenzione usata
// dai bot ben educati (nome/versione più un URL informativo), invece di fingersi
// un browser. Molti server sono più permissivi con un UA riconoscibile e
// contattabile, e resta comunque sovrascrivibile con -U per i casi in cui serve.
var defaultUserAgent = appName + "/" + appVersion + " (+" + appURL + ")"

// Config raccoglie tutte le opzioni a riga di comando.
type Config struct {
	// download / multi-segmento
	Split     int
	Jobs      int
	MinSplit  int64
	Output    string
	Dir       string
	Continue  bool
	Retries   int
	RetryWait int
	Rate      int64
	Timeout   int

	// crawl
	Recursive      bool
	Level          int
	Mirror         bool
	MirrorDirs     bool
	NoParent       bool
	SpanHosts      bool
	PageRequisites bool
	ConvertLinks   bool
	Robots         bool

	// auth / cookie
	User        string
	Password    string
	AuthMode    string
	LoginURL    string
	LoginData   string
	Bearer      string
	Headers     []string
	Cookie      string
	LoadCookies string
	SaveCookies string

	// generali
	UserAgent string
	Referer   string
	Insecure  bool
	QuietErr  bool // -qe: nessun output tranne gli errori
	QuietAll  bool // -qa: nessun output, errori inclusi
	Verbose   bool
	InputFile string
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func logf(cfg *Config, format string, args ...any) {
	if gQuietAll || cfg == nil || !cfg.Verbose {
		return
	}
	s := fmt.Sprintf(format, args...)
	if gProg != nil {
		gProg.Log(s)
	} else {
		fmt.Fprintln(stderr, s)
	}
}
func errf(format string, args ...any) {
	if gQuietAll {
		return
	}
	s := "scrap: " + fmt.Sprintf(format, args...)
	if gProg != nil {
		gProg.Log(s)
	} else {
		fmt.Fprintln(stderr, s)
	}
}
func errf2(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

func main() {
	// intercetta richieste d'aiuto prima del parsing dei flag, così possiamo
	// mostrare la pagina impaginata e scrollabile.
	if wantsHelp(os.Args[1:]) {
		displayHelp()
		os.Exit(0)
	}

	cfg := &Config{}
	fs := flag.NewFlagSet("scrap", flag.ExitOnError)
	fs.Usage = usage

	// stringhe per dimensioni/livello, convertite dopo
	var minSplitS, rateS, minSizeS, maxSizeS, quotaS, levelS string
	var headers stringList
	var accept, reject, acceptRe, rejectRe, domains, exDomains, types string

	// download
	intVar(fs, &cfg.Split, "s", "split", 4, "segmenti paralleli per file")
	intVar(fs, &cfg.Jobs, "j", "jobs", 4, "file scaricati in parallelo")
	fs.StringVar(&minSplitS, "min-split", "1M", "soglia minima per segmentare")
	strVar(fs, &cfg.Output, "o", "output", "", "nome file di output (URL singolo)")
	strVar(fs, &cfg.Dir, "d", "dir", ".", "cartella di destinazione")
	boolVar(fs, &cfg.Continue, "c", "continue", false, "riprende download interrotti")
	fs.IntVar(&cfg.Retries, "retries", 5, "tentativi per segmento (stop sugli errori permanenti)")
	fs.IntVar(&cfg.RetryWait, "retry-wait", 2, "attesa iniziale (s) del backoff esponenziale")
	fs.StringVar(&rateS, "rate", "0", "limite banda globale (es. 2M)")
	fs.IntVar(&cfg.Timeout, "timeout", 60, "timeout di rete (s)")

	// crawl
	boolVar(fs, &cfg.Recursive, "r", "recursive", false, "crawling ricorsivo")
	fs.StringVar(&levelS, "l", "5", "profondità massima (inf = illimitata)")
	fs.StringVar(&levelS, "level", "5", "profondità massima (inf = illimitata)")
	boolVar(fs, &cfg.Mirror, "m", "mirror", false, "mirror: -r -l inf + struttura cartelle")
	fs.BoolVar(&cfg.NoParent, "no-parent", false, "non risale alle cartelle superiori")
	fs.BoolVar(&cfg.SpanHosts, "span-hosts", false, "consente di uscire dall'host iniziale")
	fs.BoolVar(&cfg.PageRequisites, "page-requisites", false, "scarica le risorse della pagina")
	fs.BoolVar(&cfg.ConvertLinks, "convert-links", false, "riscrive i link per uso offline")
	fs.BoolVar(&cfg.Robots, "robots", true, "rispetta robots.txt")

	// filtri
	fs.StringVar(&accept, "A", "", "estensioni accettate (csv)")
	fs.StringVar(&accept, "accept", "", "estensioni accettate (csv)")
	fs.StringVar(&reject, "R", "", "estensioni escluse (csv)")
	fs.StringVar(&reject, "reject", "", "estensioni escluse (csv)")
	fs.StringVar(&acceptRe, "accept-re", "", "regex che l'URL deve soddisfare")
	fs.StringVar(&rejectRe, "reject-re", "", "regex che l'URL non deve soddisfare")
	fs.StringVar(&domains, "domains", "", "host ammessi (csv)")
	fs.StringVar(&exDomains, "exclude-domains", "", "host esclusi (csv)")
	fs.StringVar(&minSizeS, "min-size", "0", "dimensione minima file")
	fs.StringVar(&maxSizeS, "max-size", "0", "dimensione massima file")
	fs.StringVar(&types, "types", "", "content-type ammessi (es. image/*,application/pdf)")
	var maxFiles int
	fs.IntVar(&maxFiles, "max-files", 0, "ferma dopo N file")
	fs.StringVar(&quotaS, "quota", "0", "ferma dopo N byte totali")

	// auth / cookie
	fs.StringVar(&cfg.User, "user", "", "utente (HTTP Basic/Digest)")
	fs.StringVar(&cfg.Password, "password", "", "password (HTTP Basic/Digest)")
	fs.StringVar(&cfg.AuthMode, "auth", "auto", "modo auth: auto|basic|digest")
	fs.StringVar(&cfg.LoginURL, "login-url", "", "URL per il login via form")
	fs.StringVar(&cfg.LoginData, "login-data", "", "dati form login (a=1&b=2)")
	fs.StringVar(&cfg.Bearer, "bearer", "", "token Bearer")
	fs.Var(&headers, "H", "header HTTP aggiuntivo (ripetibile)")
	fs.Var(&headers, "header", "header HTTP aggiuntivo (ripetibile)")
	fs.StringVar(&cfg.Cookie, "cookie", "", "Cookie header manuale")
	fs.StringVar(&cfg.LoadCookies, "load-cookies", "", "carica cookies.txt (Netscape)")
	fs.StringVar(&cfg.SaveCookies, "save-cookies", "", "salva cookies.txt (Netscape)")

	// generali
	strVar(fs, &cfg.UserAgent, "U", "user-agent", defaultUserAgent, "User-Agent")
	fs.StringVar(&cfg.Referer, "referer", "", "Referer")
	fs.BoolVar(&cfg.Insecure, "insecure", false, "non verifica i certificati TLS")
	fs.BoolVar(&cfg.QuietErr, "qe", false, "silenzioso: nessun output tranne gli errori")
	fs.BoolVar(&cfg.QuietAll, "qa", false, "silenzioso totale: nessun output, errori inclusi")
	boolVar(fs, &cfg.Verbose, "v", "verbose", false, "log dettagliato")
	strVar(fs, &cfg.InputFile, "i", "input-file", "", "legge gli URL da file")

	fs.Parse(os.Args[1:])
	cfg.Headers = headers
	gQuietAll = cfg.QuietAll

	// conversioni e controlli: un valore sbagliato ferma il programma con
	// stato 2 e un messaggio che dice quale opzione correggere.
	size := func(opt, v string) int64 {
		n, err := parseSize(v)
		if err != nil || n < 0 {
			badArg("--%s: dimensione non valida %q (esempi: 500k, 10M, 1.5G)", opt, v)
		}
		return n
	}
	cfg.MinSplit = size("min-split", minSplitS)
	cfg.Rate = size("rate", rateS)
	minSize := size("min-size", minSizeS)
	maxSize := size("max-size", maxSizeS)
	quota := size("quota", quotaS)

	if strings.EqualFold(levelS, "inf") {
		cfg.Level = 1 << 30
	} else if n, err := strconv.Atoi(levelS); err == nil && n >= 0 {
		cfg.Level = n
	} else {
		badArg("-l/--level: valore non valido %q (un numero da 0 in su, oppure inf)", levelS)
	}
	switch {
	case cfg.Split < 1:
		badArg("-s/--split: serve almeno 1 connessione (dato %d)", cfg.Split)
	case cfg.Jobs < 1:
		badArg("-j/--jobs: serve almeno 1 file alla volta (dato %d)", cfg.Jobs)
	case cfg.Retries < 0:
		badArg("--retries: il numero di tentativi non può essere negativo (dato %d)", cfg.Retries)
	case cfg.RetryWait < 0:
		badArg("--retry-wait: l'attesa non può essere negativa (dato %d)", cfg.RetryWait)
	case cfg.Timeout < 0:
		badArg("--timeout: non può essere negativo (dato %d; 0 = nessun limite)", cfg.Timeout)
	case maxFiles < 0:
		badArg("--max-files: non può essere negativo (dato %d)", maxFiles)
	}
	switch cfg.AuthMode {
	case "auto", "basic", "digest":
	default:
		badArg("--auth: modo sconosciuto %q (auto, basic o digest)", cfg.AuthMode)
	}
	compile := func(opt, expr string) *regexp.Regexp {
		if expr == "" {
			return nil
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			badArg("--%s: espressione regolare non valida: %v", opt, err)
		}
		return re
	}
	acceptRx := compile("accept-re", acceptRe)
	rejectRx := compile("reject-re", rejectRe)

	// --page-requisites da solo (senza -r/-m): la pagina indicata più le sue
	// risorse, come se fosse un crawl di profondità 0.
	if cfg.PageRequisites && !cfg.Recursive && !cfg.Mirror {
		cfg.Recursive = true
		cfg.Level = 0
	}
	if cfg.Mirror {
		cfg.Recursive = true
		cfg.MirrorDirs = true
		if levelS == "5" {
			cfg.Level = 1 << 30
		}
	}
	if cfg.Recursive && !cfg.Mirror {
		cfg.MirrorDirs = true // in modalità ricorsiva ricostruiamo le cartelle
	}

	// raccogli i seed
	seeds := fs.Args()
	if cfg.InputFile != "" {
		more, err := readURLFile(cfg.InputFile)
		if err != nil {
			fatal(err)
		}
		seeds = append(seeds, more...)
	}
	if len(seeds) == 0 {
		usage()
		os.Exit(2)
	}
	if cfg.Output != "" {
		if cfg.Recursive {
			badArg("-o non si usa con -r, -m o --page-requisites: per scegliere la cartella usa -d")
		}
		if len(seeds) > 1 {
			badArg("-o vale per un solo indirizzo (ne sono stati dati %d): per scegliere la cartella usa -d", len(seeds))
		}
	}

	// filtri
	filters := &Filters{
		accept:    extsToSet(accept),
		reject:    extsToSet(reject),
		acceptRe:  acceptRx,
		rejectRe:  rejectRx,
		domains:   csvList(domains),
		exDomains: csvList(exDomains),
		types:     csvList(types),
		minSize:   minSize,
		maxSize:   maxSize,
		maxFiles:  int64(maxFiles),
		quota:     quota,
	}

	// cookie jar
	jar := NewJar()
	if cfg.LoadCookies != "" {
		if err := jar.Load(cfg.LoadCookies); err != nil {
			errf("impossibile caricare i cookie: %v", err)
		}
	}

	client := NewClient(cfg, jar)
	if cfg.LoginURL != "" {
		if err := client.formLogin(); err != nil {
			fatal(err)
		}
	}

	prog := NewProgress(cfg.QuietErr || cfg.QuietAll)
	gProg = prog
	go prog.run()

	rl := NewRateLimiter(cfg.Rate)
	engine := NewEngine(cfg, client, filters, prog, rl)
	if cfg.Robots {
		engine.initRobots()
	}
	engine.Run(seeds)

	prog.finish()

	if cfg.SaveCookies != "" {
		if err := jar.Save(cfg.SaveCookies); err != nil {
			errf("impossibile salvare i cookie: %v", err)
			os.Exit(1)
		}
	}
	// stato 1 se almeno un indirizzo non è stato scaricato
	if prog.Failed() > 0 {
		os.Exit(1)
	}
}

// helper per registrare flag con alias corto+lungo
func intVar(fs *flag.FlagSet, p *int, short, long string, def int, usage string) {
	fs.IntVar(p, short, def, usage)
	fs.IntVar(p, long, def, usage)
}
func strVar(fs *flag.FlagSet, p *string, short, long, def, usage string) {
	fs.StringVar(p, short, def, usage)
	fs.StringVar(p, long, def, usage)
}
func boolVar(fs *flag.FlagSet, p *bool, short, long string, def bool, usage string) {
	fs.BoolVar(p, short, def, usage)
	fs.BoolVar(p, long, def, usage)
}

func readURLFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out, sc.Err()
}

func fatal(err error) {
	errf("%v", err)
	os.Exit(1)
}

// badArg segnala un'opzione con un valore sbagliato ed esce con stato 2.
func badArg(format string, args ...any) {
	errf(format, args...)
	os.Exit(2)
}

// wantsHelp riconosce le richieste d'aiuto (e il caso "nessun argomento").
func wantsHelp(args []string) bool {
	if len(args) == 0 {
		return true
	}
	for _, a := range args {
		if a == "--" {
			break
		}
		switch a {
		case "-h", "-help", "--help", "help":
			return true
		}
	}
	return false
}

// usage stampa l'aiuto in forma semplice su stderr (usato sugli errori dei flag).
func usage() {
	fmt.Fprint(stderr, stripANSI(buildHelp(false, 80)))
}
