package main

import (
	"bufio"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Jar è un cookie jar minimale ma corretto a sufficienza: gestisce
// dominio/path/secure/scadenza e sa serializzarsi in formato Netscape.
type Jar struct {
	mu      sync.Mutex
	entries []*cookieEntry
}

type cookieEntry struct {
	Name, Value   string
	Domain, Path  string
	Expires       time.Time
	Secure        bool
	HostOnly      bool
	HasExpiry     bool
}

func NewJar() *Jar { return &Jar{} }

func defaultPath(p string) string {
	if p == "" || p[0] != '/' {
		return "/"
	}
	i := strings.LastIndex(p, "/")
	if i <= 0 {
		return "/"
	}
	return p[:i]
}

func (j *Jar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.mu.Lock()
	defer j.mu.Unlock()
	host := u.Hostname()
	for _, c := range cookies {
		e := &cookieEntry{Name: c.Name, Value: c.Value, Secure: c.Secure}
		if c.Domain != "" {
			e.Domain = strings.TrimPrefix(strings.ToLower(c.Domain), ".")
			e.HostOnly = false
		} else {
			e.Domain = strings.ToLower(host)
			e.HostOnly = true
		}
		if c.Path != "" {
			e.Path = c.Path
		} else {
			e.Path = defaultPath(u.Path)
		}
		switch {
		case c.MaxAge < 0:
			j.delete(e.Name, e.Domain, e.Path)
			continue
		case c.MaxAge > 0:
			e.Expires = time.Now().Add(time.Duration(c.MaxAge) * time.Second)
			e.HasExpiry = true
		case !c.Expires.IsZero():
			if c.Expires.Before(time.Now()) {
				j.delete(e.Name, e.Domain, e.Path)
				continue
			}
			e.Expires = c.Expires
			e.HasExpiry = true
		}
		j.upsert(e)
	}
}

func (j *Jar) upsert(e *cookieEntry) {
	for i, x := range j.entries {
		if x.Name == e.Name && x.Domain == e.Domain && x.Path == e.Path {
			j.entries[i] = e
			return
		}
	}
	j.entries = append(j.entries, e)
}

func (j *Jar) delete(name, domain, path string) {
	out := j.entries[:0]
	for _, x := range j.entries {
		if x.Name == name && x.Domain == domain && x.Path == path {
			continue
		}
		out = append(out, x)
	}
	j.entries = out
}

func domainMatch(host string, e *cookieEntry) bool {
	host = strings.ToLower(host)
	if e.HostOnly {
		return host == e.Domain
	}
	return host == e.Domain || strings.HasSuffix(host, "."+e.Domain)
}

func (j *Jar) Cookies(u *url.URL) []*http.Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	now := time.Now()
	host := u.Hostname()
	path := u.Path
	if path == "" {
		path = "/"
	}
	var out []*http.Cookie
	for _, e := range j.entries {
		if e.HasExpiry && e.Expires.Before(now) {
			continue
		}
		if e.Secure && u.Scheme != "https" {
			continue
		}
		if !domainMatch(host, e) {
			continue
		}
		if !(path == e.Path || strings.HasPrefix(path, strings.TrimSuffix(e.Path, "/")+"/") || e.Path == "/") {
			continue
		}
		out = append(out, &http.Cookie{Name: e.Name, Value: e.Value})
	}
	return out
}

// Load legge un file cookies.txt in formato Netscape.
func (j *Jar) Load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	j.mu.Lock()
	defer j.mu.Unlock()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		httpOnly := false
		if strings.HasPrefix(line, "#HttpOnly_") {
			httpOnly = true
			line = strings.TrimPrefix(line, "#HttpOnly_")
		}
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 7 {
			continue
		}
		_ = httpOnly
		dom := f[0]
		hostOnly := !strings.HasPrefix(dom, ".")
		dom = strings.TrimPrefix(strings.ToLower(dom), ".")
		e := &cookieEntry{
			Domain:   dom,
			HostOnly: hostOnly,
			Path:     f[2],
			Secure:   strings.EqualFold(f[3], "TRUE"),
			Name:     f[5],
			Value:    f[6],
		}
		if exp, err := strconv.ParseInt(f[4], 10, 64); err == nil && exp > 0 {
			e.Expires = time.Unix(exp, 0)
			e.HasExpiry = true
		}
		j.upsert(e)
	}
	return sc.Err()
}

// Save scrive il jar in formato Netscape cookies.txt.
func (j *Jar) Save(path string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	fmt.Fprintln(w, "# Netscape HTTP Cookie File")
	fmt.Fprintln(w, "# generato da scrap")
	for _, e := range j.entries {
		dom := e.Domain
		flag := "FALSE"
		if !e.HostOnly {
			dom = "." + dom
			flag = "TRUE"
		}
		sec := "FALSE"
		if e.Secure {
			sec = "TRUE"
		}
		var exp int64
		if e.HasExpiry {
			exp = e.Expires.Unix()
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
			dom, flag, e.Path, sec, exp, e.Name, e.Value)
	}
	return w.Flush()
}
