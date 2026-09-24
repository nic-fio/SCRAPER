package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// testEngine costruisce un Engine come fa main, senza barre e senza rete reale.
func testEngine(t *testing.T, mod func(*Config)) *Engine {
	t.Helper()
	cfg := &Config{
		Split: 4, Jobs: 4, MinSplit: 1 << 20, Dir: t.TempDir(),
		Retries: 1, RetryWait: 1, Timeout: 10, Level: 5, Robots: true,
		UserAgent: defaultUserAgent,
	}
	if mod != nil {
		mod(cfg)
	}
	e := NewEngine(cfg, NewClient(cfg, NewJar()), &Filters{}, NewProgress(true), NewRateLimiter(0))
	if cfg.Robots {
		e.initRobots()
	}
	return e
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

// rangeServer serve data con il supporto ai Range di http.ServeContent e conta
// i byte effettivamente inviati.
func rangeServer(data []byte, sent *int64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cw := &countingWriter{ResponseWriter: w, n: sent}
		http.ServeContent(cw, r, "file.bin", time.Time{}, bytes.NewReader(data))
	}))
}

type countingWriter struct {
	http.ResponseWriter
	n *int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	atomic.AddInt64(c.n, int64(len(p)))
	return c.ResponseWriter.Write(p)
}

func TestSegmentedDownloadIdentical(t *testing.T) {
	data := randomBytes(t, 3<<20+12345) // non multiplo del numero di segmenti
	var sent int64
	srv := rangeServer(data, &sent)
	defer srv.Close()

	e := testEngine(t, nil)
	dest := filepath.Join(e.cfg.Dir, "file.bin")
	if err := e.downloadFile(srv.URL+"/file.bin", dest, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("il file scaricato a segmenti differisce dall'originale")
	}
	for _, leftover := range []string{dest + ".part", dest + ".part.scrap"} {
		if _, err := os.Stat(leftover); err == nil {
			t.Errorf("file di lavoro rimasto: %s", leftover)
		}
	}
}

func TestSegmentedResumeDownloadsOnlyMissing(t *testing.T) {
	data := randomBytes(t, 2<<20)
	var sent int64
	srv := rangeServer(data, &sent)
	defer srv.Close()

	e := testEngine(t, func(c *Config) { c.Continue = true })
	dest := filepath.Join(e.cfg.Dir, "file.bin")
	part := dest + ".part"

	// Simula un'interruzione: ogni segmento ha già scritto la sua prima metà.
	segs := makeSegs(int64(len(data)), e.cfg.Split)
	buf := make([]byte, len(data))
	done := make([]int64, len(segs))
	var already int64
	for i, s := range segs {
		half := (s.end - s.start + 1) / 2
		copy(buf[s.start:s.start+half], data[s.start:s.start+half])
		done[i] = half
		already += half
	}
	if err := os.WriteFile(part, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	meta, _ := json.Marshal(segMeta{URL: srv.URL + "/file.bin", Size: int64(len(data)), Done: done})
	if err := os.WriteFile(part+".scrap", meta, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := e.downloadFile(srv.URL+"/file.bin", dest, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Fatal("il file ripreso differisce dall'originale")
	}
	// Oltre alla sonda (1 byte) il server deve aver mandato solo la parte mancante.
	if want := int64(len(data)) - already + 1; atomic.LoadInt64(&sent) != want {
		t.Errorf("byte inviati dal server = %d, attesi %d (solo la parte mancante)", sent, want)
	}
}

func TestSingleStreamWithoutRanges(t *testing.T) {
	data := randomBytes(t, 1500000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data) // ignora Range: risponde sempre 200 con tutto il file
	}))
	defer srv.Close()

	e := testEngine(t, nil)
	dest := filepath.Join(e.cfg.Dir, "x.bin")
	if err := e.downloadFile(srv.URL+"/x.bin", dest, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Fatal("download a flusso singolo errato")
	}
}

func TestPermanentErrorIsNotRetried(t *testing.T) {
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	e := testEngine(t, func(c *Config) { c.Retries = 3 })
	err := e.downloadFile(srv.URL+"/manca", filepath.Join(e.cfg.Dir, "manca"), nil)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("atteso HTTP 404, ottenuto %v", err)
	}
	if hits != 1 {
		t.Errorf("un 404 non va ritentato: %d richieste", hits)
	}
}

func TestCrawlFiltersAndRobots(t *testing.T) {
	jpg := randomBytes(t, 4000)
	pages := map[string]string{
		"/":           `<a href="sub/">s</a><a href="a.jpg">a</a><a href="b.txt">b</a><a href="priv/x.jpg">p</a><a href="https://altro.example/y.jpg">y</a>`,
		"/sub/":       `<img src="c.jpg"><img srcset="d.jpg 2x">`,
		"/robots.txt": "User-agent: *\nDisallow: /priv/\n",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/robots.txt":
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(pages[r.URL.Path]))
		case pages[r.URL.Path] != "":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(pages[r.URL.Path]))
		case strings.HasSuffix(r.URL.Path, ".jpg"):
			http.ServeContent(w, r, "x.jpg", time.Time{}, bytes.NewReader(jpg))
		case strings.HasSuffix(r.URL.Path, ".txt"):
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("testo"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	e := testEngine(t, func(c *Config) { c.Recursive = true; c.MirrorDirs = true })
	e.filters.accept = extsToSet("jpg")
	e.Run([]string{srv.URL + "/"})

	var got []string
	filepath.Walk(e.cfg.Dir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			rel, _ := filepath.Rel(e.cfg.Dir, p)
			got = append(got, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(got)
	want := []string{"127.0.0.1/a.jpg", "127.0.0.1/sub/c.jpg", "127.0.0.1/sub/d.jpg"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("file salvati = %v, attesi %v (solo jpg, niente pagine, niente priv/ né altri host)", got, want)
	}
}

// dropServer serve data; la prima GET completa (non la sonda) si interrompe a
// metà. Con ranges=false ignora ogni Range e manda sempre tutto il file.
func dropServer(t *testing.T, data []byte, ranges bool) *httptest.Server {
	var full int64
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isProbe := r.Header.Get("Range") == "bytes=0-0"
		if ranges && r.Header.Get("Range") != "" {
			http.ServeContent(w, r, "f.bin", time.Time{}, bytes.NewReader(data))
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		if !isProbe && atomic.AddInt64(&full, 1) == 1 {
			w.Write(data[:len(data)/2])
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler) // connessione chiusa a metà
		}
		w.Write(data)
	}))
}

func TestSingleStreamRetryResumesWithoutDuplicates(t *testing.T) {
	data := randomBytes(t, 800000)
	srv := dropServer(t, data, true)
	defer srv.Close()
	e := testEngine(t, func(c *Config) { c.Split = 1; c.RetryWait = 0 })
	dest := filepath.Join(e.cfg.Dir, "f.bin")
	if err := e.downloadFile(srv.URL+"/f.bin", dest, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Fatalf("file ripreso errato: %d byte invece di %d (dati duplicati?)", len(got), len(data))
	}
}

func TestSingleStreamRetryWithoutRangesRestarts(t *testing.T) {
	data := randomBytes(t, 800000)
	srv := dropServer(t, data, false)
	defer srv.Close()
	e := testEngine(t, func(c *Config) { c.RetryWait = 0 })
	dest := filepath.Join(e.cfg.Dir, "f.bin")
	if err := e.downloadFile(srv.URL+"/f.bin", dest, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Fatalf("senza Range il nuovo tentativo deve ricominciare da zero: %d byte invece di %d", len(got), len(data))
	}
}

// slowServer manda data a pezzi con una pausa tra un pezzo e l'altro; se
// stallOnce è vero, la prima GET completa si blocca a metà per 3 secondi.
func slowServer(data []byte, pause time.Duration, stallOnce bool) *httptest.Server {
	var n int64
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "bytes=0-0" {
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.WriteHeader(http.StatusOK) // niente Range: un solo flusso
			return
		}
		first := atomic.AddInt64(&n, 1) == 1
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		chunk := len(data) / 8
		for i := 0; i < len(data); i += chunk {
			end := min(i+chunk, len(data))
			if _, err := w.Write(data[i:end]); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			if stallOnce && first && i >= len(data)/2 {
				select {
				case <-r.Context().Done():
				case <-time.After(3 * time.Second):
				}
				return
			}
			time.Sleep(pause)
		}
	}))
}

func TestTimeoutDoesNotLimitTotalDuration(t *testing.T) {
	data := randomBytes(t, 80000)
	srv := slowServer(data, 300*time.Millisecond, false) // ~2,4 s in tutto
	defer srv.Close()
	e := testEngine(t, func(c *Config) { c.Timeout = 1; c.Retries = 0 })
	dest := filepath.Join(e.cfg.Dir, "lento.bin")
	if err := e.downloadFile(srv.URL+"/lento.bin", dest, nil); err != nil {
		t.Fatalf("un download costante più lungo di --timeout deve riuscire: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Fatal("file lento errato")
	}
}

func TestStalledTransferIsRetried(t *testing.T) {
	data := randomBytes(t, 80000)
	srv := slowServer(data, 0, true)
	defer srv.Close()
	e := testEngine(t, func(c *Config) { c.Timeout = 1; c.RetryWait = 0 })
	dest := filepath.Join(e.cfg.Dir, "fermo.bin")
	start := time.Now()
	if err := e.downloadFile(srv.URL+"/fermo.bin", dest, nil); err != nil {
		t.Fatalf("dopo un blocco il nuovo tentativo deve riuscire: %v", err)
	}
	if time.Since(start) > 2500*time.Millisecond {
		t.Errorf("il blocco non è stato interrotto dopo --timeout: %v", time.Since(start))
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Fatal("file errato dopo il blocco")
	}
}

func TestSegmentIgnoringRangeIsRejected(t *testing.T) {
	data := randomBytes(t, 2<<20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "bytes=0-0" {
			http.ServeContent(w, r, "x", time.Time{}, bytes.NewReader(data)) // la sonda dice 206
			return
		}
		w.Write(data) // ma i pezzi arrivano come 200 con tutto il file
	}))
	defer srv.Close()
	e := testEngine(t, func(c *Config) { c.Retries = 0 })
	dest := filepath.Join(e.cfg.Dir, "x.bin")
	if err := e.downloadFile(srv.URL+"/x.bin", dest, nil); err == nil {
		t.Fatal("un pezzo servito con 200 deve essere un errore")
	}
	if _, err := os.Stat(dest); err == nil {
		t.Error("non deve nascere un file finale corrotto")
	}
}

func TestExistingFileSkippedEvenWithContinue(t *testing.T) {
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.Write([]byte("nuovo"))
	}))
	defer srv.Close()
	e := testEngine(t, func(c *Config) { c.Continue = true })
	dest := filepath.Join(e.cfg.Dir, "a.txt")
	os.WriteFile(dest, []byte("vecchio"), 0o644)
	if err := e.downloadFile(srv.URL+"/a.txt", dest, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dest); string(got) != "vecchio" || hits != 0 {
		t.Errorf("un file completo non va riscaricato: contenuto %q, richieste %d", got, hits)
	}
}

func TestExtractPageLinksRequisites(t *testing.T) {
	page, _ := url.Parse("http://x.test/p/")
	body := []byte(`<link rel="stylesheet" href="s.css"><link rel="icon" href="f.ico">
<link rel="next" href="p2.html"><a href="altra.html">a</a><area href="mappa.html">
<img src="i.png"><script src="j.js"></script><iframe src="fr.html"></iframe>
<div style="background:url(bg.gif)"></div>`)
	links, reqs := extractPageLinks(page, body)
	if len(links) != 9 {
		t.Errorf("link totali = %d, attesi 9: %v", len(links), links)
	}
	got := toSet(reqs)
	for _, w := range []string{"s.css", "f.ico", "i.png", "j.js", "fr.html", "bg.gif"} {
		if !got["http://x.test/p/"+w] {
			t.Errorf("%s deve essere un requisito della pagina", w)
		}
	}
	for _, n := range []string{"p2.html", "altra.html", "mappa.html"} {
		if got["http://x.test/p/"+n] {
			t.Errorf("%s è un link di navigazione, non un requisito", n)
		}
	}
}

// siteServer serve un piccolo sito: pagine HTML dalla mappa, tutto il resto
// come file binari di 3000 byte; i percorsi che iniziano con /manca danno 404.
func siteServer(pages map[string]string) *httptest.Server {
	blob := bytes.Repeat([]byte("x"), 3000)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if html, ok := pages[r.URL.Path]; ok {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(html))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/manca") || r.URL.Path == "/robots.txt" {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, "b.bin", time.Time{}, bytes.NewReader(blob))
	}))
}

func savedFiles(t *testing.T, dir string) string {
	t.Helper()
	var got []string
	filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			got = append(got, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(got)
	return strings.Join(got, " ")
}

func TestPageRequisitesAtLastLevel(t *testing.T) {
	srv := siteServer(map[string]string{
		"/":           `<link rel="stylesheet" href="s.css"><img src="i.png"><a href="altra.html">a</a>`,
		"/altra.html": `<img src="non.png">`,
	})
	defer srv.Close()
	e := testEngine(t, func(c *Config) { c.Recursive = true; c.MirrorDirs = true; c.Level = 0; c.PageRequisites = true })
	e.Run([]string{srv.URL + "/"})
	want := "127.0.0.1/i.png 127.0.0.1/index.html 127.0.0.1/s.css"
	if got := savedFiles(t, e.cfg.Dir); got != want {
		t.Errorf("salvati %q, attesi %q", got, want)
	}
}

func TestMirrorSavesSamePageOnce(t *testing.T) {
	home := `<a href="/index.html">home</a><a href="b.bin">b</a>`
	srv := siteServer(map[string]string{"/": home, "/index.html": home})
	defer srv.Close()
	e := testEngine(t, func(c *Config) { c.Recursive = true; c.MirrorDirs = true })
	e.Run([]string{srv.URL + "/"})
	if got := atomic.LoadInt64(&e.prog.done); got != 2 {
		t.Errorf("file contati = %d, attesi 2 (index.html una volta sola, b.bin)", got)
	}
}

func TestFailuresAreCounted(t *testing.T) {
	srv := siteServer(map[string]string{"/": `<a href="manca.pdf">x</a><a href="ok.bin">y</a>`})
	defer srv.Close()
	e := testEngine(t, func(c *Config) { c.Recursive = true; c.MirrorDirs = true })
	e.Run([]string{srv.URL + "/", "non-un-url"})
	if got := e.prog.Failed(); got != 2 {
		t.Errorf("falliti = %d, attesi 2 (il 404 e l'indirizzo non valido)", got)
	}
}
