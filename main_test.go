package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Quando SCRAP_TEST_MAIN è impostata, questo processo di test fa da programma:
// esegue main() con gli argomenti dopo "--". Così si provano stati di uscita e
// messaggi del programma intero, senza compilare un binario a parte.
func TestMain(m *testing.M) {
	if os.Getenv("SCRAP_TEST_MAIN") == "1" {
		for i, a := range os.Args {
			if a == "--" {
				os.Args = append([]string{"scrap"}, os.Args[i+1:]...)
				break
			}
		}
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runScrap esegue il programma con args e restituisce stato di uscita e stderr.
func runScrap(t *testing.T, dir string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], append([]string{"-test.run=^$", "--"}, args...)...)
	cmd.Env = append(os.Environ(), "SCRAP_TEST_MAIN=1")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), string(out)
	} else if err != nil {
		t.Fatal(err)
	}
	return 0, string(out)
}

func TestExitCodesAndMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/manca") {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("contenuto"))
	}))
	defer srv.Close()
	ok, missing := srv.URL+"/ok.txt", srv.URL+"/manca.txt"

	cases := []struct {
		name string
		args []string
		code int
		msg  string // deve comparire nell'uscita ("" = nessun controllo)
	}{
		{"riuscito", []string{ok}, 0, "1 file"},
		{"404", []string{ok, missing}, 1, "1 falliti"},
		{"regex sbagliata", []string{"--accept-re", "(", ok}, 2, "--accept-re: espressione regolare non valida"},
		{"dimensione sbagliata", []string{"--max-size", "abc", ok}, 2, `--max-size: dimensione non valida "abc"`},
		{"livello sbagliato", []string{"-l", "tre", "-r", ok}, 2, "-l/--level"},
		{"jobs zero", []string{"-j", "0", ok}, 2, "-j/--jobs"},
		{"auth sconosciuto", []string{"--auth", "ntlm", ok}, 2, "--auth"},
		{"-o con due URL", []string{"-o", "x", ok, missing}, 2, "-o vale per un solo indirizzo"},
		{"-o con -r", []string{"-o", "x", "-r", ok}, 2, "-o non si usa con -r"},
		{"URL non valido", []string{"ftp://x/y"}, 1, "URL non valido"},
		{"-qa silenzioso", []string{"-qa", ok, missing}, 1, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, out := runScrap(t, t.TempDir(), append([]string{"-d", "dl"}, c.args...)...)
			if code != c.code {
				t.Errorf("stato di uscita %d, atteso %d; uscita:\n%s", code, c.code, out)
			}
			if c.msg != "" && !strings.Contains(out, c.msg) {
				t.Errorf("manca %q nell'uscita:\n%s", c.msg, out)
			}
			if strings.Contains(out, "panic") || strings.Contains(out, "goroutine") {
				t.Errorf("il programma è andato in crash:\n%s", out)
			}
			if c.name == "-qa silenzioso" && out != "" {
				t.Errorf("-qa deve tacere del tutto, invece:\n%s", out)
			}
		})
	}
}

func TestQuietErrShowsOnlyErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/manca" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("x"))
	}))
	defer srv.Close()
	_, out := runScrap(t, t.TempDir(), "-qe", "-d", "dl", srv.URL+"/a.txt", srv.URL+"/manca")
	if strings.TrimSpace(out) != "scrap: errore su "+srv.URL+"/manca: HTTP 404" {
		t.Errorf("-qe deve mostrare solo l'errore, invece:\n%s", out)
	}
}
