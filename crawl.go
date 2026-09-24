package main

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/net/html"
)

// Engine è il cuore condiviso da download e crawling.
type Engine struct {
	cfg     *Config
	client  *Client
	filters *Filters
	prog    *Progress
	rl      *RateLimiter

	seedHosts map[string]bool
	firstSeed *url.URL

	visited map[string]bool
	vmu     sync.Mutex

	downloaded map[string]string // url assoluto -> path locale (per convert-links)
	htmlFiles  map[string]bool   // path locali html salvati
	pagesSeen  map[string]bool   // path locali delle pagine già esplorate
	dmu        sync.Mutex

	sem     chan struct{}
	wg      sync.WaitGroup
	stopped int32

	robots map[string]*robotRules
	rmu    sync.Mutex
}

func NewEngine(cfg *Config, client *Client, filters *Filters, prog *Progress, rl *RateLimiter) *Engine {
	return &Engine{
		cfg:        cfg,
		client:     client,
		filters:    filters,
		prog:       prog,
		rl:         rl,
		seedHosts:  map[string]bool{},
		visited:    map[string]bool{},
		downloaded: map[string]string{},
		htmlFiles:  map[string]bool{},
		pagesSeen:  map[string]bool{},
		sem:        make(chan struct{}, cfg.Jobs),
	}
}

func (e *Engine) stop()           { atomic.StoreInt32(&e.stopped, 1) }
func (e *Engine) isStopped() bool { return atomic.LoadInt32(&e.stopped) == 1 }

func (e *Engine) recordDownloaded(rawurl, dest string) {
	e.dmu.Lock()
	e.downloaded[rawurl] = dest
	e.dmu.Unlock()
}

// Run avvia l'elaborazione dei seed.
func (e *Engine) Run(seeds []string) {
	var valid []string
	for _, s := range seeds {
		u, err := url.Parse(s)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			errf("URL non valido: %s", s)
			e.prog.fail()
			continue
		}
		e.seedHosts[strings.ToLower(u.Hostname())] = true
		if e.firstSeed == nil {
			e.firstSeed = u
		}
		valid = append(valid, s)
	}
	// Restrizione host di default: se non si consente lo span e non sono stati
	// indicati domini espliciti, restringi agli host dei seed.
	if !e.cfg.SpanHosts && len(e.filters.domains) == 0 {
		for h := range e.seedHosts {
			e.filters.domains = append(e.filters.domains, h)
		}
	}

	for _, s := range valid {
		if e.cfg.Recursive {
			e.enqueue(s, 0, false)
		} else {
			e.enqueueDownload(s)
		}
	}
	e.wg.Wait()

	if e.cfg.ConvertLinks {
		e.convertLinks()
	}
}

// enqueueDownload: download diretto (modalità non ricorsiva).
func (e *Engine) enqueueDownload(rawurl string) {
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.sem <- struct{}{}
		defer func() { <-e.sem }()
		dest := e.localPath(mustParse(rawurl), false)
		if e.cfg.Output != "" {
			dest = e.cfg.Output
		}
		if err := e.downloadFile(rawurl, dest, nil); err != nil {
			e.handleErr(rawurl, err)
		}
	}()
}

// enqueue inserisce un URL nel crawl rispettando dedup e limiti. Un
// requisito (req) è una risorsa necessaria a mostrare una pagina (immagine,
// foglio di stile, script…) accodata da --page-requisites: viene scaricato
// anche oltre la profondità massima e fuori da --no-parent, ma non esplorato.
func (e *Engine) enqueue(rawurl string, depth int, req bool) {
	if e.isStopped() {
		return
	}
	u, err := url.Parse(rawurl)
	if err != nil {
		return
	}
	u.Fragment = ""
	key := u.String()
	e.vmu.Lock()
	if e.visited[key] {
		e.vmu.Unlock()
		return
	}
	e.visited[key] = true
	e.vmu.Unlock()

	if !e.filters.allowCrawl(u, e.firstSeed, e.cfg, depth, req) {
		return
	}
	if e.cfg.Robots && !e.allowedByRobots(u) {
		logf(e.cfg, "bloccato da robots.txt: %s", u)
		return
	}

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.sem <- struct{}{}
		defer func() { <-e.sem }()
		e.process(u, depth, req)
	}()
}

func (e *Engine) process(u *url.URL, depth int, req bool) {
	if e.isStopped() {
		return
	}
	info, err := e.probe(u.String())
	if err != nil {
		e.handleErr(u.String(), err)
		return
	}
	isHTML := strings.Contains(strings.ToLower(info.ct), "text/html") ||
		strings.Contains(strings.ToLower(info.ct), "application/xhtml")

	if isHTML && depth <= e.cfg.Level && !req {
		e.crawlHTML(u, depth)
		return
	}
	// risorsa scaricabile
	if !e.filters.allowDownloadExt(u) {
		logf(e.cfg, "scartato per estensione: %s", u)
		return
	}
	dest := e.localPath(u, isHTML)
	if err := e.downloadFile(u.String(), dest, info); err != nil {
		e.handleErr(u.String(), err)
	}
}

func (e *Engine) crawlHTML(u *url.URL, depth int) {
	var body []byte
	var base *url.URL
	var ct string
	err := e.withRetry(func() error {
		req, err := e.client.newRequest("GET", u.String())
		if err != nil {
			return err
		}
		resp, err := e.client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return statusError(resp)
		}
		b, err := io.ReadAll(&meterReader{r: io.LimitReader(resp.Body, 32<<20), lim: e.rl, prog: e.prog})
		if err != nil {
			return err
		}
		body, base, ct = b, resp.Request.URL, resp.Header.Get("Content-Type")
		return nil
	})
	if err != nil {
		e.handleErr(u.String(), err)
		return
	}

	// La pagina è già stata scaricata perché serve per estrarre i link: la
	// salviamo su disco solo se supera i filtri utente (estensione/dimensione/
	// content-type). Il budget --max-files viene consumato solo se salviamo davvero.
	dest := e.localPath(base, true)
	e.dmu.Lock()
	dup := e.pagesSeen[dest]
	e.pagesSeen[dest] = true
	e.dmu.Unlock()
	if dup {
		// Due indirizzi della stessa pagina (es. "/" e "/index.html", o dopo
		// un redirect): già salvata ed esplorata.
		return
	}
	if e.filters.allowSaveHTML(base, ct, int64(len(body))) {
		if e.filters.reserveFile() {
			if os.MkdirAll(filepath.Dir(dest), 0o755) == nil {
				if os.WriteFile(dest, body, 0o644) == nil {
					e.prog.quickDone(dest, int64(len(body)))
					e.dmu.Lock()
					e.downloaded[base.String()] = dest
					e.htmlFiles[dest] = true
					e.dmu.Unlock()
				}
			}
		} else {
			e.stop()
			return
		}
	} else {
		logf(e.cfg, "pagina non salvata (fuori dai filtri), seguo solo i link: %s", base)
	}

	links, requisites := extractPageLinks(base, body)
	if depth >= e.cfg.Level {
		// ultimo livello: niente altri link, ma con --page-requisites le
		// risorse che servono a mostrare la pagina vengono scaricate.
		if e.cfg.PageRequisites {
			for _, link := range requisites {
				e.enqueue(link, depth+1, true)
			}
		}
		return
	}
	for _, link := range links {
		e.enqueue(link, depth+1, false)
	}
}

func (e *Engine) handleErr(rawurl string, err error) {
	if err == errQuota {
		e.stop()
		logf(e.cfg, "limite raggiunto, mi fermo")
		return
	}
	errf("errore su %s: %v", rawurl, err)
	e.prog.fail()
}

// ---- estrazione link ----

// reCSSurl cattura gli url(...) nel CSS (attributi style inline e blocchi <style>).
var reCSSurl = regexp.MustCompile(`(?i)url\(\s*["']?([^"')]+)`)

// extractLinks estrae gli URL assoluti referenziati dalla pagina usando un vero
// parser HTML tollerante all'HTML malformato: raccoglie href/src/poster e le voci
// di srcset, rispetta un eventuale <base href> per la risoluzione dei relativi, e
// recupera gli url(...) sia dagli attributi style inline sia dal contenuto dei
// blocchi <style>. Rispetto alla vecchia estrazione a regex evita i falsi positivi
// (es. URL dentro il codice JavaScript) e gestisce correttamente tag annidati,
// virgolette miste ed entità.
func extractLinks(base *url.URL, body []byte) []string {
	links, _ := extractPageLinks(base, body)
	return links
}

// extractPageLinks restituisce tutti i link della pagina e, a parte, i suoi
// requisiti: le risorse che servono a mostrarla (img, script, fogli di stile,
// icone, audio/video, iframe, url() nel CSS). Sono link di navigazione solo gli
// href di <a>, <area> e dei <link> che non sono stylesheet, icon, preload o
// manifest.
func extractPageLinks(base *url.URL, body []byte) (links, requisites []string) {
	set := map[string]bool{}
	reqSet := map[string]bool{}
	resolveBase := base
	nav := false // il prossimo add è un link di navigazione?
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.HasPrefix(raw, "#") ||
			strings.HasPrefix(strings.ToLower(raw), "javascript:") ||
			strings.HasPrefix(strings.ToLower(raw), "mailto:") ||
			strings.HasPrefix(strings.ToLower(raw), "data:") ||
			strings.HasPrefix(strings.ToLower(raw), "tel:") {
			return
		}
		ref, err := url.Parse(raw)
		if err != nil {
			return
		}
		abs := resolveBase.ResolveReference(ref)
		abs.Fragment = ""
		if abs.Scheme == "http" || abs.Scheme == "https" {
			set[abs.String()] = true
			if !nav {
				reqSet[abs.String()] = true
			}
		}
	}
	addSrcset := func(v string) {
		for _, part := range strings.Split(v, ",") {
			if f := strings.Fields(part); len(f) > 0 {
				add(f[0])
			}
		}
	}
	addCSS := func(v string) {
		for _, m := range reCSSurl.FindAllStringSubmatch(v, -1) {
			add(m[1])
		}
	}

	z := html.NewTokenizer(bytes.NewReader(body))
	inStyle := false
	for {
		switch z.Next() {
		case html.ErrorToken:
			// ErrorToken segnala la fine del documento (io.EOF) o un errore
			// irrecuperabile del tokenizer: in entrambi i casi abbiamo finito.
			for k := range set {
				links = append(links, k)
			}
			for k := range reqSet {
				requisites = append(requisites, k)
			}
			return links, requisites
		case html.TextToken:
			if inStyle {
				addCSS(string(z.Text()))
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			if tag == "style" {
				inStyle = true
			}
			var href, src, poster, srcset, style, rel string
			for hasAttr {
				var k, v []byte
				k, v, hasAttr = z.TagAttr()
				switch string(k) {
				case "href":
					href = string(v)
				case "src":
					src = string(v)
				case "poster":
					poster = string(v)
				case "srcset":
					srcset = string(v)
				case "style":
					style = string(v)
				case "rel":
					rel = strings.ToLower(string(v))
				}
			}
			// <base href> ridefinisce la base per i link successivi e non è esso
			// stesso una risorsa da scaricare.
			if tag == "base" {
				if href != "" {
					if u, err := url.Parse(strings.TrimSpace(href)); err == nil {
						resolveBase = base.ResolveReference(u)
					}
				}
				continue
			}
			if href != "" {
				nav = tag == "a" || tag == "area" || (tag == "link" && !isRequisiteRel(rel))
				add(href)
				nav = false
			}
			if src != "" {
				add(src)
			}
			if poster != "" {
				add(poster)
			}
			if srcset != "" {
				addSrcset(srcset)
			}
			if style != "" {
				addCSS(style)
			}
		case html.EndTagToken:
			if name, _ := z.TagName(); string(name) == "style" {
				inStyle = false
			}
		}
	}
}

// isRequisiteRel dice se un <link rel=…> punta a una risorsa della pagina.
func isRequisiteRel(rel string) bool {
	for _, r := range strings.Fields(rel) {
		switch r {
		case "stylesheet", "icon", "apple-touch-icon", "preload", "modulepreload", "manifest":
			return true
		}
	}
	return false
}

// ---- mappatura su filesystem ----

var reUnsafe = regexp.MustCompile(`[^A-Za-z0-9._\-/]`)

func (e *Engine) localPath(u *url.URL, isHTML bool) string {
	p := u.Path
	if p == "" || strings.HasSuffix(p, "/") {
		p += "index.html"
	}
	if u.RawQuery != "" {
		p += "@" + u.RawQuery
	}
	if isHTML {
		ext := strings.ToLower(path.Ext(p))
		if ext != ".html" && ext != ".htm" && ext != ".xhtml" {
			p += ".html"
		}
	}
	// sanitizza ogni segmento
	clean := reUnsafe.ReplaceAllString(p, "_")
	clean = strings.TrimPrefix(clean, "/")

	base := e.cfg.Dir
	if e.cfg.MirrorDirs {
		base = filepath.Join(e.cfg.Dir, u.Hostname())
		return filepath.Join(base, filepath.FromSlash(clean))
	}
	// senza mirror: salva tutto in Dir usando solo il basename
	return filepath.Join(base, filepath.Base(filepath.FromSlash(clean)))
}

// reAbsURL trova gli indirizzi assoluti nel testo di una pagina: finiscono al
// primo spazio, virgoletta, parentesi o segno di tag.
var reAbsURL = regexp.MustCompile(`https?://[^\s"'<>()]+`)

// convertLinks riscrive, nelle pagine salvate, gli indirizzi assoluti dei file
// scaricati in percorsi relativi, per la consultazione offline. Sostituisce
// solo indirizzi interi (un indirizzo più corto non può alterarne uno più
// lungo che lo contiene), conserva l'eventuale #frammento e lascia intatti gli
// indirizzi dei file non scaricati. I link relativi restano come sono: la
// struttura delle cartelle ricalca quella del sito.
func (e *Engine) convertLinks() {
	e.dmu.Lock()
	defer e.dmu.Unlock()
	for htmlPath := range e.htmlFiles {
		data, err := os.ReadFile(htmlPath)
		if err != nil {
			continue
		}
		dir := filepath.Dir(htmlPath)
		out := reAbsURL.ReplaceAllStringFunc(string(data), func(m string) string {
			addr, frag, hasFrag := strings.Cut(m, "#")
			local, ok := e.downloaded[addr]
			if !ok {
				local, ok = e.downloaded[html.UnescapeString(addr)] // &amp; negli attributi
			}
			if !ok {
				return m
			}
			rel, err := filepath.Rel(dir, local)
			if err != nil {
				return m
			}
			rel = filepath.ToSlash(rel)
			if hasFrag {
				rel += "#" + frag
			}
			return rel
		})
		os.WriteFile(htmlPath, []byte(out), 0o644)
	}
	logf(e.cfg, "link convertiti in %d pagine", len(e.htmlFiles))
}

func mustParse(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		return &url.URL{Path: s}
	}
	return u
}

var _ = http.StatusOK
