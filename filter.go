package main

import (
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync/atomic"
)

// Filters raccoglie tutte le regole di selezione compilate.
type Filters struct {
	accept    map[string]bool // estensioni ammesse
	reject    map[string]bool // estensioni escluse
	acceptRe  *regexp.Regexp
	rejectRe  *regexp.Regexp
	domains   []string
	exDomains []string
	types     []string // sottostringhe content-type, supporta "image/*"
	minSize   int64
	maxSize   int64

	maxFiles int64
	quota    int64

	// stato runtime
	fileCount int64
	byteCount int64
}

func extsToSet(csv string) map[string]bool {
	m := map[string]bool{}
	for _, e := range strings.Split(csv, ",") {
		e = strings.TrimSpace(strings.ToLower(strings.TrimPrefix(e, ".")))
		if e != "" {
			m[e] = true
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

func csvList(s string) []string {
	var out []string
	for _, e := range strings.Split(s, ",") {
		if e = strings.TrimSpace(strings.ToLower(e)); e != "" {
			out = append(out, e)
		}
	}
	return out
}

func urlExt(u *url.URL) string {
	e := strings.ToLower(path.Ext(u.Path))
	return strings.TrimPrefix(e, ".")
}

// allowCrawl: l'URL può essere visitato/seguito per il crawling?
func (f *Filters) allowCrawl(u *url.URL, seed *url.URL, cfg *Config, depth int) bool {
	if depth > cfg.Level {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if len(f.domains) > 0 && !hostInList(host, f.domains) {
		return false
	}
	if len(f.exDomains) > 0 && hostInList(host, f.exDomains) {
		return false
	}
	if cfg.NoParent && seed != nil && !strings.HasPrefix(u.Path, dirOf(seed.Path)) {
		return false
	}
	if f.rejectRe != nil && f.rejectRe.MatchString(u.String()) {
		return false
	}
	if f.acceptRe != nil && !f.acceptRe.MatchString(u.String()) {
		return false
	}
	return true
}

// allowDownload: questa risorsa (non-HTML) va salvata in base a estensione?
func (f *Filters) allowDownloadExt(u *url.URL) bool {
	ext := urlExt(u)
	if f.reject != nil && f.reject[ext] {
		return false
	}
	if f.accept != nil && !f.accept[ext] {
		return false
	}
	return true
}

func (f *Filters) allowSize(n int64) bool {
	if n < 0 {
		return true // dimensione sconosciuta: non possiamo escludere
	}
	if f.minSize > 0 && n < f.minSize {
		return false
	}
	if f.maxSize > 0 && n > f.maxSize {
		return false
	}
	return true
}

func (f *Filters) allowType(ct string) bool {
	if len(f.types) == 0 {
		return true
	}
	ct = strings.ToLower(ct)
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	for _, t := range f.types {
		if strings.HasSuffix(t, "/*") {
			if strings.HasPrefix(ct, strings.TrimSuffix(t, "*")) {
				return true
			}
		} else if strings.Contains(ct, t) {
			return true
		}
	}
	return false
}

// reserve verifica i limiti globali (max-files, quota) e li aggiorna.
// Ritorna false se il limite è già stato raggiunto.
func (f *Filters) reserveFile() bool {
	if f.maxFiles > 0 {
		if atomic.AddInt64(&f.fileCount, 1) > f.maxFiles {
			return false
		}
	}
	return true
}

func (f *Filters) addBytes(n int64) bool {
	if f.quota > 0 {
		return atomic.AddInt64(&f.byteCount, n) <= f.quota
	}
	atomic.AddInt64(&f.byteCount, n)
	return true
}

func (f *Filters) quotaExceeded() bool {
	return f.quota > 0 && atomic.LoadInt64(&f.byteCount) >= f.quota
}

func hostInList(host string, list []string) bool {
	for _, d := range list {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

func dirOf(p string) string {
	if p == "" {
		return "/"
	}
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return "/"
	}
	return p[:i+1]
}
