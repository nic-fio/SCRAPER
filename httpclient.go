package main

import (
	"context"
	"crypto/md5"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// Client incapsula http.Client con la configurazione di scrap.
type Client struct {
	hc      *http.Client
	cfg     *Config
	jar     *Jar
	ncCount uint64 // contatore per il cnonce digest
}

// NewClient prepara il client HTTP. --timeout NON limita la durata di una
// richiesta (un file di molti gigabyte deve poter durare quanto serve): vale
// per la connessione, l'handshake TLS, l'attesa delle intestazioni di risposta
// e, durante il trasferimento, per il tempo massimo senza ricevere byte (vedi
// idleBody). Con 0 questi limiti sono disattivati.
func NewClient(cfg *Config, jar *Jar) *Client {
	t := time.Duration(cfg.Timeout) * time.Second
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: t, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: cfg.Insecure},
		TLSHandshakeTimeout:   t,
		ResponseHeaderTimeout: t,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   cfg.Split + cfg.Jobs,
		ForceAttemptHTTP2:     true,
	}
	hc := &http.Client{
		Transport: tr,
		Jar:       jar,
	}
	return &Client{hc: hc, cfg: cfg, jar: jar}
}

// newRequest costruisce una richiesta con tutti gli header comuni.
func (c *Client) newRequest(method, rawurl string) (*http.Request, error) {
	req, err := http.NewRequest(method, rawurl, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	if c.cfg.Referer != "" {
		req.Header.Set("Referer", c.cfg.Referer)
	}
	for _, h := range c.cfg.Headers {
		if i := strings.IndexByte(h, ':'); i > 0 {
			req.Header.Set(strings.TrimSpace(h[:i]), strings.TrimSpace(h[i+1:]))
		}
	}
	if c.cfg.Bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.Bearer)
	}
	if c.cfg.Cookie != "" {
		req.Header.Set("Cookie", c.cfg.Cookie)
	}
	// Basic preventivo: copre il caso più comune; se il server vuole Digest
	// lo gestiamo sul 401.
	if c.cfg.User != "" && c.cfg.Bearer == "" && c.cfg.AuthMode != "digest" {
		req.SetBasicAuth(c.cfg.User, c.cfg.Password)
	}
	return req, nil
}

// Do esegue la richiesta gestendo eventuale challenge Digest sul 401. Il
// corpo della risposta è sorvegliato da idleBody: se per --timeout secondi non
// arriva nessun byte, la richiesta viene interrotta con un errore chiaro (che
// withRetry tratta come transitorio).
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithCancel(req.Context())
	resp, err := c.do(req.WithContext(ctx))
	if err != nil {
		cancel()
		return nil, err
	}
	resp.Body = newIdleBody(resp.Body, time.Duration(c.cfg.Timeout)*time.Second, cancel)
	return resp, nil
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized && c.cfg.User != "" {
		wa := resp.Header.Get("WWW-Authenticate")
		if strings.HasPrefix(strings.ToLower(wa), "digest") {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			req2, err := c.cloneForRetry(req)
			if err != nil {
				return nil, err
			}
			req2 = req2.WithContext(req.Context())
			req2.Header.Set("Authorization", c.digestHeader(wa, req2.Method, req2.URL))
			return c.hc.Do(req2)
		}
	}
	return resp, nil
}

// idleBody interrompe la richiesta (tramite cancel) quando per un intervallo
// d non arriva nessun byte: protegge dai server che smettono di mandare dati
// senza chiudere la connessione, senza limitare la durata totale.
type idleBody struct {
	rc     io.ReadCloser
	d      time.Duration
	timer  *time.Timer
	fired  atomic.Bool
	cancel context.CancelFunc
}

func newIdleBody(rc io.ReadCloser, d time.Duration, cancel context.CancelFunc) io.ReadCloser {
	b := &idleBody{rc: rc, d: d, cancel: cancel}
	if d > 0 {
		b.timer = time.AfterFunc(d, func() {
			b.fired.Store(true)
			cancel()
		})
	}
	return b
}

func (b *idleBody) Read(p []byte) (int, error) {
	n, err := b.rc.Read(p)
	if n > 0 && b.timer != nil {
		b.timer.Reset(b.d)
	}
	if err != nil && err != io.EOF && b.fired.Load() {
		err = fmt.Errorf("nessun dato ricevuto per %s", b.d)
	}
	return n, err
}

func (b *idleBody) Close() error {
	if b.timer != nil {
		b.timer.Stop()
	}
	err := b.rc.Close()
	b.cancel()
	return err
}

func (c *Client) cloneForRetry(req *http.Request) (*http.Request, error) {
	r2, err := c.newRequest(req.Method, req.URL.String())
	if err != nil {
		return nil, err
	}
	if rng := req.Header.Get("Range"); rng != "" {
		r2.Header.Set("Range", rng)
	}
	return r2, nil
}

func h32(s string) string { return fmt.Sprintf("%x", md5.Sum([]byte(s))) }

// digestHeader calcola l'header Authorization per Digest (qop=auth, MD5).
func (c *Client) digestHeader(challenge, method string, u *url.URL) string {
	p := parseChallenge(challenge)
	realm, nonce, opaque, qop, algo := p["realm"], p["nonce"], p["opaque"], p["qop"], p["algorithm"]
	uri := u.RequestURI()
	ha1 := h32(c.cfg.User + ":" + realm + ":" + c.cfg.Password)
	if strings.EqualFold(algo, "MD5-sess") {
		ha1 = h32(ha1 + ":" + nonce + ":")
	}
	ha2 := h32(method + ":" + uri)
	nc := fmt.Sprintf("%08x", atomic.AddUint64(&c.ncCount, 1))
	cnonce := h32(fmt.Sprintf("%s%d", nonce, time.Now().UnixNano()))[:16]
	var resp string
	if qop != "" {
		// se il server offre più qop, usiamo "auth"
		qop = "auth"
		resp = h32(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
	} else {
		resp = h32(ha1 + ":" + nonce + ":" + ha2)
	}
	var b strings.Builder
	fmt.Fprintf(&b, `Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s"`,
		c.cfg.User, realm, nonce, uri, resp)
	if algo != "" {
		fmt.Fprintf(&b, `, algorithm=%s`, algo)
	}
	if qop != "" {
		fmt.Fprintf(&b, `, qop=%s, nc=%s, cnonce="%s"`, qop, nc, cnonce)
	}
	if opaque != "" {
		fmt.Fprintf(&b, `, opaque="%s"`, opaque)
	}
	return b.String()
}

// parseChallenge estrae le coppie chiave=valore da un header WWW-Authenticate.
func parseChallenge(s string) map[string]string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, ' '); i >= 0 {
		s = s[i+1:]
	}
	m := map[string]string{}
	for _, part := range splitChallenge(s) {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.TrimSpace(kv[0])
		v := strings.Trim(strings.TrimSpace(kv[1]), `"`)
		m[strings.ToLower(k)] = v
	}
	return m
}

// splitChallenge separa per virgola ignorando le virgole dentro le virgolette.
func splitChallenge(s string) []string {
	var out []string
	var cur strings.Builder
	inQ := false
	for _, r := range s {
		switch r {
		case '"':
			inQ = !inQ
			cur.WriteRune(r)
		case ',':
			if inQ {
				cur.WriteRune(r)
			} else {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// formLogin esegue il login via form: POST delle credenziali, i cookie di
// sessione restituiti finiscono nel jar e vengono riutilizzati.
func (c *Client) formLogin() error {
	if c.cfg.LoginURL == "" {
		return nil
	}
	data := c.cfg.LoginData
	req, err := http.NewRequest("POST", c.cfg.LoginURL, strings.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, h := range c.cfg.Headers {
		if i := strings.IndexByte(h, ':'); i > 0 {
			req.Header.Set(strings.TrimSpace(h[:i]), strings.TrimSpace(h[i+1:]))
		}
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("login fallito: HTTP %d", resp.StatusCode)
	}
	logf(c.cfg, "login eseguito su %s (HTTP %d)", c.cfg.LoginURL, resp.StatusCode)
	return nil
}
