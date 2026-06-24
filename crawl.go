package main

import (
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
		sem:        make(chan struct{}, cfg.Jobs),
	}
}

func (e *Engine) stop()        { atomic.StoreInt32(&e.stopped, 1) }
func (e *Engine) isStopped() bool { return atomic.LoadInt32(&e.stopped) == 1 }

func (e *Engine) recordDownloaded(rawurl, dest string) {
	e.dmu.Lock()
	e.downloaded[rawurl] = dest
	e.dmu.Unlock()
}

// Run avvia l'elaborazione dei seed.
func (e *Engine) Run(seeds []string) {
	for i, s := range seeds {
		u, err := url.Parse(s)
		if err != nil || u.Host == "" {
			errf("URL non valido: %s", s)
			continue
		}
		e.seedHosts[strings.ToLower(u.Hostname())] = true
		if i == 0 {
			e.firstSeed = u
		}
	}
	// Restrizione host di default: se non si consente lo span e non sono stati
	// indicati domini espliciti, restringi agli host dei seed.
	if !e.cfg.SpanHosts && len(e.filters.domains) == 0 {
		for h := range e.seedHosts {
			e.filters.domains = append(e.filters.domains, h)
		}
	}

	for _, s := range seeds {
		if e.cfg.Recursive {
			e.enqueue(s, 0)
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

// enqueue inserisce un URL nel crawl rispettando dedup e limiti.
func (e *Engine) enqueue(rawurl string, depth int) {
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

	if !e.filters.allowCrawl(u, e.firstSeed, e.cfg, depth) {
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
		e.process(u, depth)
	}()
}

func (e *Engine) process(u *url.URL, depth int) {
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

	if isHTML && depth <= e.cfg.Level {
		e.crawlHTML(u, depth)
		return
	}
	// risorsa scaricabile
	if !e.filters.allowDownloadExt(u) {
		logf(e.cfg, "scartato per estensione: %s", u)
		return
	}
	dest := e.localPath(u, false)
	if err := e.downloadFile(u.String(), dest, info); err != nil {
		e.handleErr(u.String(), err)
	}
}

func (e *Engine) crawlHTML(u *url.URL, depth int) {
	req, err := e.client.newRequest("GET", u.String())
	if err != nil {
		return
	}
	resp, err := e.client.Do(req)
	if err != nil {
		e.handleErr(u.String(), err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		e.handleErr(u.String(), errf2("HTTP %d", resp.StatusCode))
		return
	}
	body, err := io.ReadAll(&meterReader{r: io.LimitReader(resp.Body, 32<<20), lim: e.rl, prog: e.prog})
	if err != nil {
		return
	}
	base := resp.Request.URL

	// salva la pagina
	if e.filters.reserveFile() {
		dest := e.localPath(base, true)
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

	if depth >= e.cfg.Level {
		return
	}
	for _, link := range extractLinks(base, body) {
		e.enqueue(link, depth+1)
	}
}

func (e *Engine) handleErr(rawurl string, err error) {
	if err == errQuota {
		e.stop()
		logf(e.cfg, "limite raggiunto, mi fermo")
		return
	}
	errf("errore su %s: %v", rawurl, err)
}

// ---- estrazione link ----

var (
	reHref   = regexp.MustCompile(`(?i)(?:href|src)\s*=\s*["']?([^"'\s>]+)`)
	reSrcset = regexp.MustCompile(`(?i)srcset\s*=\s*["']([^"']+)["']`)
	reCSSurl = regexp.MustCompile(`(?i)url\(\s*["']?([^"')]+)`)
)

func extractLinks(base *url.URL, body []byte) []string {
	s := string(body)
	set := map[string]bool{}
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
		abs := base.ResolveReference(ref)
		abs.Fragment = ""
		if abs.Scheme == "http" || abs.Scheme == "https" {
			set[abs.String()] = true
		}
	}
	for _, m := range reHref.FindAllStringSubmatch(s, -1) {
		add(m[1])
	}
	for _, m := range reSrcset.FindAllStringSubmatch(s, -1) {
		for _, part := range strings.Split(m[1], ",") {
			fields := strings.Fields(part)
			if len(fields) > 0 {
				add(fields[0])
			}
		}
	}
	for _, m := range reCSSurl.FindAllStringSubmatch(s, -1) {
		add(m[1])
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
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

// convertLinks riscrive (in modo basilare) i link assoluti delle pagine
// salvate verso i percorsi locali corrispondenti.
func (e *Engine) convertLinks() {
	e.dmu.Lock()
	defer e.dmu.Unlock()
	for htmlPath := range e.htmlFiles {
		data, err := os.ReadFile(htmlPath)
		if err != nil {
			continue
		}
		content := string(data)
		dir := filepath.Dir(htmlPath)
		for absURL, local := range e.downloaded {
			rel, err := filepath.Rel(dir, local)
			if err != nil {
				continue
			}
			content = strings.ReplaceAll(content, absURL, rel)
		}
		os.WriteFile(htmlPath, []byte(content), 0o644)
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
