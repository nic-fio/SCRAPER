package main

import (
	"net/url"
	"testing"
)

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", s, err)
	}
	return u
}

func TestAllowType(t *testing.T) {
	cases := []struct {
		types []string
		ct    string
		want  bool
	}{
		{nil, "text/html", true},                              // nessun filtro: tutto passa
		{[]string{"image/*"}, "image/png", true},              // wildcard
		{[]string{"image/*"}, "text/html", false},             // wildcard non combacia
		{[]string{"text/html"}, "text/html; charset=utf-8", true}, // parametri ignorati
		{[]string{"pdf"}, "application/pdf", true},            // sottostringa
		{[]string{"text/html"}, "application/json", false},
	}
	for _, c := range cases {
		f := &Filters{types: c.types}
		if got := f.allowType(c.ct); got != c.want {
			t.Errorf("allowType(types=%v, ct=%q) = %v, want %v", c.types, c.ct, got, c.want)
		}
	}
}

func TestAllowSize(t *testing.T) {
	f := &Filters{minSize: 100, maxSize: 1000}
	cases := []struct {
		n    int64
		want bool
	}{
		{-1, true}, // sconosciuta: non escludibile
		{50, false},
		{100, true},
		{500, true},
		{1000, true},
		{1001, false},
	}
	for _, c := range cases {
		if got := f.allowSize(c.n); got != c.want {
			t.Errorf("allowSize(%d) = %v, want %v", c.n, got, c.want)
		}
	}
}

func TestAllowDownloadExt(t *testing.T) {
	accept := &Filters{accept: extsToSet("jpg,png")}
	if !accept.allowDownloadExt(mustURL(t, "http://x/a.jpg")) {
		t.Error("jpg dovrebbe essere ammesso dalla whitelist")
	}
	if accept.allowDownloadExt(mustURL(t, "http://x/a.gif")) {
		t.Error("gif non dovrebbe passare la whitelist jpg,png")
	}

	reject := &Filters{reject: extsToSet("exe,zip")}
	if reject.allowDownloadExt(mustURL(t, "http://x/a.exe")) {
		t.Error("exe dovrebbe essere escluso dalla blacklist")
	}
	if !reject.allowDownloadExt(mustURL(t, "http://x/a.txt")) {
		t.Error("txt non e' nella blacklist, dovrebbe passare")
	}
}

func TestHostInList(t *testing.T) {
	list := []string{"example.com"}
	cases := []struct {
		host string
		want bool
	}{
		{"example.com", true},
		{"www.example.com", true},  // sottodominio
		{"a.b.example.com", true},  // sottodominio profondo
		{"notexample.com", false},  // suffisso senza il punto: no
		{"example.org", false},
	}
	for _, c := range cases {
		if got := hostInList(c.host, list); got != c.want {
			t.Errorf("hostInList(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

func TestUrlExt(t *testing.T) {
	cases := map[string]string{
		"http://x/a.JPG":      "jpg", // minuscolo
		"http://x/a.tar.gz":   "gz",
		"http://x/dir/":       "",
		"http://x/noext":      "",
		"http://x/a.PdF?q=1":  "pdf",
	}
	for in, want := range cases {
		if got := urlExt(mustURL(t, in)); got != want {
			t.Errorf("urlExt(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReserveFileAndQuota(t *testing.T) {
	f := &Filters{maxFiles: 2}
	if !f.reserveFile() || !f.reserveFile() {
		t.Fatal("i primi due file dovrebbero rientrare nel limite")
	}
	if f.reserveFile() {
		t.Error("il terzo file dovrebbe superare max-files=2")
	}

	q := &Filters{quota: 100}
	if !q.addBytes(60) {
		t.Error("60 byte sono sotto la quota di 100")
	}
	if q.addBytes(50) {
		t.Error("110 byte totali superano la quota di 100")
	}
	if !q.quotaExceeded() {
		t.Error("quotaExceeded dovrebbe essere vero dopo il superamento")
	}
}
