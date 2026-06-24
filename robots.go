package main

import (
	"bufio"
	"io"
	"net/url"
	"strings"
)

// robotRules contiene i prefissi Disallow/Allow per User-agent: * .
type robotRules struct {
	disallow []string
	allow    []string
}

func (e *Engine) initRobots() {
	e.robots = map[string]*robotRules{}
}

// allowedByRobots verifica se l'URL è consentito dal robots.txt dell'host.
func (e *Engine) allowedByRobots(u *url.URL) bool {
	host := u.Scheme + "://" + u.Host
	e.rmu.Lock()
	rr, ok := e.robots[host]
	e.rmu.Unlock()
	if !ok {
		rr = e.fetchRobots(host)
		e.rmu.Lock()
		e.robots[host] = rr
		e.rmu.Unlock()
	}
	if rr == nil {
		return true
	}
	p := u.Path
	if p == "" {
		p = "/"
	}
	// la regola più lunga vince (Allow batte Disallow a parità non gestita)
	best, allowed := -1, true
	for _, d := range rr.disallow {
		if d != "" && strings.HasPrefix(p, d) && len(d) > best {
			best, allowed = len(d), false
		}
	}
	for _, a := range rr.allow {
		if a != "" && strings.HasPrefix(p, a) && len(a) >= best {
			best, allowed = len(a), true
		}
	}
	return allowed
}

func (e *Engine) fetchRobots(host string) *robotRules {
	req, err := e.client.newRequest("GET", host+"/robots.txt")
	if err != nil {
		return nil
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	rr := &robotRules{}
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	applies := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		switch k {
		case "user-agent":
			applies = v == "*"
		case "disallow":
			if applies && v != "" {
				rr.disallow = append(rr.disallow, v)
			}
		case "allow":
			if applies && v != "" {
				rr.allow = append(rr.allow, v)
			}
		}
	}
	return rr
}
