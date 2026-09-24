package main

import (
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func toSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

func TestExtractLinksWithBase(t *testing.T) {
	page, _ := url.Parse("http://example.com/dir/index.html")
	body := []byte(`
<html><head>
  <base href="http://example.com/base/">
  <link rel="stylesheet" href="style.css">
  <style>body { background: url("bg.png"); }</style>
</head><body>
  <a href="page.html">rel</a>
  <a href="#frag">frammento</a>
  <a href="mailto:a@b.com">mail</a>
  <a href="javascript:void(0)">js</a>
  <a href="tel:+123">tel</a>
  <img src="/img/a.png" srcset="/img/a-1x.png 1x, /img/a-2x.png 2x">
  <video poster="poster.jpg"></video>
  <div style="background:url('inline.gif')"></div>
  <script>var u = "http://tracker.example/evil.js";</script>
  <a href="https://other.example/abs">abs</a>
</body></html>`)

	got := toSet(extractLinks(page, body))
	want := []string{
		"http://example.com/base/style.css",
		"http://example.com/base/bg.png",
		"http://example.com/base/page.html",
		"http://example.com/img/a.png",
		"http://example.com/img/a-1x.png",
		"http://example.com/img/a-2x.png",
		"http://example.com/base/poster.jpg",
		"http://example.com/base/inline.gif",
		"https://other.example/abs",
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("link atteso mancante: %s", w)
		}
	}
	if len(got) != len(want) {
		keys := make([]string, 0, len(got))
		for k := range got {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		t.Errorf("numero di link = %d, atteso %d; ottenuti: %v", len(got), len(want), keys)
	}
	// Il parser non deve raccogliere URL presenti solo nel corpo di <script>.
	if got["http://tracker.example/evil.js"] {
		t.Error("URL nel <script> non deve essere estratto (falso positivo evitato)")
	}
	// Le esclusioni per schema non devono comparire.
	for bad := range got {
		if bad == "" {
			t.Error("URL vuoto estratto")
		}
	}
}

func TestExtractLinksNoBase(t *testing.T) {
	page, _ := url.Parse("http://example.com/dir/index.html")
	body := []byte(`<a href="../up.html">u</a><a href="sibling.html">s</a>`)
	got := toSet(extractLinks(page, body))
	// Senza <base> i relativi si risolvono contro l'URL della pagina.
	if !got["http://example.com/up.html"] {
		t.Error("../up.html dovrebbe risolvere a http://example.com/up.html")
	}
	if !got["http://example.com/dir/sibling.html"] {
		t.Error("sibling.html dovrebbe risolvere a http://example.com/dir/sibling.html")
	}
}

func TestExtractLinksMalformed(t *testing.T) {
	page, _ := url.Parse("http://example.com/")
	// HTML volutamente malformato: tag non chiusi, virgolette miste.
	body := []byte(`<div><a href='a.html'><img src="b.png"><p>testo <a href=c.html>`)
	got := toSet(extractLinks(page, body))
	for _, w := range []string{
		"http://example.com/a.html",
		"http://example.com/b.png",
		"http://example.com/c.html",
	} {
		if !got[w] {
			t.Errorf("link atteso mancante da HTML malformato: %s", w)
		}
	}
}

func TestConvertLinksWholeURLsOnly(t *testing.T) {
	for i := 0; i < 30; i++ { // l'ordine delle mappe è casuale: ripeti
		dir := t.TempDir()
		e := testEngine(t, func(c *Config) { c.Dir = dir })
		root := filepath.Join(dir, "x.test")
		page := filepath.Join(root, "index.html")
		os.MkdirAll(filepath.Join(root, "img"), 0o755)
		os.WriteFile(page, []byte(`<a href="http://x.test/">h</a><img src="http://x.test/img/a.png">`+
			`<a href="http://x.test/altro.html">non scaricata</a><a href='http://x.test/#top'>t</a>`), 0o644)
		e.downloaded["http://x.test/"] = page
		e.downloaded["http://x.test/img/a.png"] = filepath.Join(root, "img", "a.png")
		e.htmlFiles[page] = true
		e.convertLinks()
		got, _ := os.ReadFile(page)
		want := `<a href="index.html">h</a><img src="img/a.png">` +
			`<a href="http://x.test/altro.html">non scaricata</a><a href='index.html#top'>t</a>`
		if string(got) != want {
			t.Fatalf("link convertiti male:\n got  %s\n want %s", got, want)
		}
	}
}
