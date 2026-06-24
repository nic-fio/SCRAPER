package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var errQuota = errors.New("quota raggiunta")

type probeInfo struct {
	size     int64
	ranges   bool
	ct       string
	finalURL string
}

// probe scopre dimensione, content-type e supporto ai range con una sola
// richiesta GET Range: bytes=0-0 (poi chiude il corpo).
func (e *Engine) probe(rawurl string) (*probeInfo, error) {
	req, err := e.client.newRequest("GET", rawurl)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
	}()
	info := &probeInfo{size: -1, ct: resp.Header.Get("Content-Type"), finalURL: resp.Request.URL.String()}
	switch resp.StatusCode {
	case http.StatusPartialContent: // 206
		info.ranges = true
		if cr := resp.Header.Get("Content-Range"); cr != "" {
			if i := strings.LastIndex(cr, "/"); i >= 0 {
				if n, err := strconv.ParseInt(strings.TrimSpace(cr[i+1:]), 10, 64); err == nil {
					info.size = n
				}
			}
		}
	case http.StatusOK: // 200: niente range
		info.ranges = false
		if resp.ContentLength >= 0 {
			info.size = resp.ContentLength
		}
	case http.StatusRequestedRangeNotSatisfiable: // 416: file vuoto
		info.size = 0
	default:
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return info, nil
}

// downloadFile salva una risorsa su dest, usando multi-segmento se possibile.
func (e *Engine) downloadFile(rawurl, dest string, info *probeInfo) error {
	if _, err := os.Stat(dest); err == nil && !e.cfg.Continue {
		logf(e.cfg, "esiste già, salto: %s", dest)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if info == nil {
		var err error
		info, err = e.probe(rawurl)
		if err != nil {
			return err
		}
	}
	if !e.filters.allowSize(info.size) {
		logf(e.cfg, "scartato per dimensione (%s): %s", humanBytes(info.size), rawurl)
		return nil
	}
	if !e.filters.allowType(info.ct) {
		logf(e.cfg, "scartato per content-type (%s): %s", info.ct, rawurl)
		return nil
	}
	if !e.filters.reserveFile() {
		return errQuota
	}

	part := dest + ".part"
	useSeg := info.ranges && info.size >= e.cfg.MinSplit && e.cfg.Split > 1 && info.size > 0

	var segs []*segProg
	if useSeg {
		segs = makeSegs(info.size, e.cfg.Split)
	}
	fp := e.prog.startFile(dest, info.size, segs)

	var err error
	if useSeg {
		err = e.segmented(rawurl, dest, part, info.size, fp)
	} else {
		err = e.singleStream(rawurl, dest, part, info, fp)
	}
	e.prog.finishFile(fp, err == nil)
	if err != nil {
		return err
	}
	e.recordDownloaded(rawurl, dest)
	return nil
}

func makeSegs(size int64, n int) []*segProg {
	segs := make([]*segProg, n)
	chunk := size / int64(n)
	for i := 0; i < n; i++ {
		s := int64(i) * chunk
		en := s + chunk - 1
		if i == n-1 {
			en = size - 1
		}
		segs[i] = &segProg{start: s, end: en}
	}
	return segs
}

type segMeta struct {
	URL  string  `json:"url"`
	Size int64   `json:"size"`
	Done []int64 `json:"done"`
}

func (e *Engine) segmented(rawurl, dest, part string, size int64, fp *fileProg) error {
	segs := fp.segs
	n := len(segs)
	f, err := os.OpenFile(part, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	if err := f.Truncate(size); err != nil {
		f.Close()
		return err
	}

	metaPath := part + ".scrap"
	if e.cfg.Continue {
		if data, err := os.ReadFile(metaPath); err == nil {
			var m segMeta
			if json.Unmarshal(data, &m) == nil && m.Size == size && len(m.Done) == n {
				var tot int64
				for i := range segs {
					atomic.StoreInt64(&segs[i].done, m.Done[i])
					tot += m.Done[i]
				}
				atomic.StoreInt64(&fp.done, tot)
				logf(e.cfg, "resume: %s", dest)
			}
		}
	}

	var metaMu sync.Mutex
	flush := func() {
		m := segMeta{URL: rawurl, Size: size, Done: make([]int64, n)}
		for i := range segs {
			m.Done[i] = atomic.LoadInt64(&segs[i].done)
		}
		data, _ := json.Marshal(m)
		metaMu.Lock()
		os.WriteFile(metaPath, data, 0o644)
		metaMu.Unlock()
	}

	done := make(chan struct{})
	go func() {
		t := time.NewTicker(1500 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				flush()
			}
		}
	}()

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := segs[i]
			errs[i] = e.withRetry(func() error {
				from := s.start + atomic.LoadInt64(&s.done)
				if from > s.end {
					return nil // segmento già completo
				}
				return e.fetchRange(rawurl, f, from, s.end, func(nb int) {
					atomic.AddInt64(&s.done, int64(nb))
					atomic.AddInt64(&fp.done, int64(nb))
					e.filters.addBytes(int64(nb))
				})
			})
		}(i)
	}
	wg.Wait()
	close(done)
	f.Close()

	for _, er := range errs {
		if er != nil {
			flush()
			return er
		}
	}
	os.Remove(metaPath)
	return os.Rename(part, dest)
}

// fetchRange scarica l'intervallo [from,end] scrivendolo a offset nel file.
func (e *Engine) fetchRange(rawurl string, f *os.File, from, end int64, cb func(int)) error {
	req, err := e.client.newRequest("GET", rawurl)
	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", from, end))
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d sul range", resp.StatusCode)
	}
	r := &meterReader{r: resp.Body, lim: e.rl, prog: e.prog}
	buf := make([]byte, 64*1024)
	off := from
	for {
		nr, er := r.Read(buf)
		if nr > 0 {
			if _, ew := f.WriteAt(buf[:nr], off); ew != nil {
				return ew
			}
			off += int64(nr)
			cb(nr)
			if e.filters.quotaExceeded() {
				return errQuota
			}
		}
		if er == io.EOF {
			return nil
		}
		if er != nil {
			return er
		}
	}
}

func (e *Engine) singleStream(rawurl, dest, part string, info *probeInfo, fp *fileProg) error {
	var off int64
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if e.cfg.Continue {
		if st, err := os.Stat(part); err == nil && info.ranges && info.size > st.Size() {
			off = st.Size()
			flags = os.O_WRONLY | os.O_APPEND
			logf(e.cfg, "resume da %s: %s", humanBytes(off), dest)
		}
	}
	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return err
	}

	err = e.withRetry(func() error {
		req, err := e.client.newRequest("GET", rawurl)
		if err != nil {
			return err
		}
		if off > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", off))
		}
		resp, err := e.client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		r := &meterReader{r: resp.Body, lim: e.rl, prog: e.prog}
		buf := make([]byte, 64*1024)
		for {
			nr, er := r.Read(buf)
			if nr > 0 {
				if _, ew := f.Write(buf[:nr]); ew != nil {
					return ew
				}
				atomic.AddInt64(&fp.done, int64(nr))
				e.filters.addBytes(int64(nr))
				if e.filters.quotaExceeded() {
					return errQuota
				}
			}
			if er == io.EOF {
				return nil
			}
			if er != nil {
				return er
			}
		}
	})
	f.Close()
	if err != nil {
		return err
	}
	return os.Rename(part, dest)
}

// withRetry riprova un'operazione fino a cfg.Retries volte.
func (e *Engine) withRetry(fn func() error) error {
	var err error
	for attempt := 0; attempt <= e.cfg.Retries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(e.cfg.RetryWait) * time.Second)
		}
		err = fn()
		if err == nil || errors.Is(err, errQuota) {
			return err
		}
	}
	return err
}
