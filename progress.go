package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// gProg è il progress attivo: logf/errf vi instradano i messaggi così da non
// rovinare l'area viva delle barre.
var gProg *Progress

// segProg è lo stato di un singolo segmento di un file.
type segProg struct {
	start, end int64
	done       int64 // atomico
}

func (s *segProg) frac() float64 {
	tot := s.end - s.start + 1
	if tot <= 0 {
		return 1
	}
	d := atomic.LoadInt64(&s.done)
	if d > tot {
		d = tot
	}
	return float64(d) / float64(tot)
}

// fileProg è lo stato di un download in corso.
type fileProg struct {
	name string
	size int64 // -1 se ignota
	done int64 // atomico
	segs []*segProg
	t0   time.Time

	lastBytes int64
	lastTime  time.Time
	speed     float64
}

// Progress gestisce i contatori globali e il rendering dell'area viva.
type Progress struct {
	mu        sync.Mutex
	files     []*fileProg
	tty       bool
	color     bool
	quiet     bool
	start     time.Time
	liveLines int

	bytes int64 // aggregato atomico
	done  int64 // file completati atomico

	aggLastBytes int64
	aggLastTime  time.Time
	aggSpeed     float64

	stop chan struct{}
	once sync.Once
}

func NewProgress(quiet bool) *Progress {
	tty := isTTY(os.Stderr)
	return &Progress{
		tty:         tty && !quiet,
		color:       tty && !quiet && os.Getenv("NO_COLOR") == "",
		quiet:       quiet,
		start:       time.Now(),
		aggLastTime: time.Now(),
		stop:        make(chan struct{}),
	}
}

func (p *Progress) addBytes(n int64) { atomic.AddInt64(&p.bytes, n) }

func (p *Progress) run() {
	if !p.tty {
		return
	}
	t := time.NewTicker(120 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-t.C:
			p.mu.Lock()
			p.render()
			p.mu.Unlock()
		}
	}
}

// startFile registra un download e restituisce il suo stato da aggiornare.
func (p *Progress) startFile(name string, size int64, segs []*segProg) *fileProg {
	fp := &fileProg{name: filepath.Base(name), size: size, segs: segs, t0: time.Now(), lastTime: time.Now()}
	p.mu.Lock()
	p.files = append(p.files, fp)
	p.mu.Unlock()
	return fp
}

// finishFile rimuove il file dall'area viva; se ok, lo conteggia e ne stampa
// la riga di completamento nella cronologia.
func (p *Progress) finishFile(fp *fileProg, ok bool) {
	p.mu.Lock()
	for i, f := range p.files {
		if f == fp {
			p.files = append(p.files[:i], p.files[i+1:]...)
			break
		}
	}
	if ok {
		atomic.AddInt64(&p.done, 1)
		p.logLocked(p.doneLine(fp))
	} else {
		p.render()
	}
	p.mu.Unlock()
}

// quickDone conteggia un file "istantaneo" (es. una pagina HTML salvata).
func (p *Progress) quickDone(name string, size int64) {
	atomic.AddInt64(&p.done, 1)
	if p.quiet {
		return
	}
	p.mu.Lock()
	icon := col(p.color, aGreen, "✓")
	p.logLocked(fmt.Sprintf(" %s %s  %s", icon, filepath.Base(name), col(p.color, aDim, humanShort(size))))
	p.mu.Unlock()
}

func (p *Progress) doneLine(fp *fileProg) string {
	d := atomic.LoadInt64(&fp.done)
	el := time.Since(fp.t0).Seconds()
	avg := float64(0)
	if el > 0 {
		avg = float64(d) / el
	}
	icon := col(p.color, aGreen, "✓")
	return fmt.Sprintf(" %s %s  %s  %s",
		icon, fp.name,
		col(p.color, aBold, humanShort(d)),
		col(p.color, aDim, humanShort(int64(avg))+"/s"))
}

// Log stampa un messaggio sopra l'area viva.
func (p *Progress) Log(s string) {
	p.mu.Lock()
	p.logLocked(s)
	p.mu.Unlock()
}

func (p *Progress) logLocked(s string) {
	if !p.tty {
		fmt.Fprintln(stderr, s)
		return
	}
	var b strings.Builder
	if p.liveLines > 1 {
		fmt.Fprintf(&b, "\033[%dA", p.liveLines-1)
	}
	b.WriteString("\r\033[J")
	b.WriteString(s)
	b.WriteString("\n")
	p.liveLines = 0
	fmt.Fprint(stderr, b.String())
	p.render()
}

// render ridisegna l'area viva (deve essere chiamata con il lock).
func (p *Progress) render() {
	if !p.tty {
		return
	}
	lines := p.buildLines()
	var b strings.Builder
	if p.liveLines > 1 {
		fmt.Fprintf(&b, "\033[%dA", p.liveLines-1)
	}
	b.WriteString("\r\033[J")
	for i, ln := range lines {
		b.WriteString(ln)
		if i < len(lines)-1 {
			b.WriteString("\n")
		}
	}
	p.liveLines = len(lines)
	fmt.Fprint(stderr, b.String())
}

const maxBars = 6

func (p *Progress) buildLines() []string {
	now := time.Now()
	w := termWidthOr(80)

	var lines []string
	shown := p.files
	extra := 0
	if len(shown) > maxBars {
		extra = len(shown) - maxBars
		shown = shown[:maxBars]
	}
	for _, fp := range shown {
		lines = append(lines, p.fileLine(fp, w, now))
		if len(fp.segs) > 1 {
			lines = append(lines, p.segLine(fp, w))
		}
	}
	if extra > 0 {
		lines = append(lines, col(p.color, aDim, fmt.Sprintf("    …e altri %d in corso", extra)))
	}

	// riepilogo aggregato
	b := atomic.LoadInt64(&p.bytes)
	dt := now.Sub(p.aggLastTime).Seconds()
	if dt > 0 {
		inst := float64(b-p.aggLastBytes) / dt
		if p.aggSpeed == 0 {
			p.aggSpeed = inst
		} else {
			p.aggSpeed = 0.6*p.aggSpeed + 0.4*inst
		}
		p.aggLastBytes, p.aggLastTime = b, now
	}
	done := atomic.LoadInt64(&p.done)
	active := len(p.files)
	sep := col(p.color, aDim, strings.Repeat("─", min(w-1, 72)))
	summary := fmt.Sprintf(" %s attivi · %s fatti · %s · %s",
		col(p.color, aBold, itoa(active)),
		col(p.color, aBold, itoa(int(done))),
		humanShort(b),
		col(p.color, aCyan, humanShort(int64(p.aggSpeed))+"/s"))
	lines = append(lines, sep, summary)
	return lines
}

func (p *Progress) fileLine(fp *fileProg, w int, now time.Time) string {
	d := atomic.LoadInt64(&fp.done)

	// velocità per-file con media esponenziale
	ddt := now.Sub(fp.lastTime).Seconds()
	if ddt >= 0.1 {
		inst := float64(d-fp.lastBytes) / ddt
		if fp.speed == 0 {
			fp.speed = inst
		} else {
			fp.speed = 0.6*fp.speed + 0.4*inst
		}
		fp.lastBytes, fp.lastTime = d, now
	}

	nameW := 18
	name := fitName(fp.name, nameW)
	barW := w - nameW - 46
	if barW < 8 {
		barW = 8
	}
	if barW > 32 {
		barW = 32
	}

	var frac float64
	if fp.size > 0 {
		frac = float64(d) / float64(fp.size)
	}
	bar := mainBar(frac, barW, p.color)

	var sizes, pct, eta string
	if fp.size > 0 {
		pct = fmt.Sprintf("%3.0f%%", frac*100)
		sizes = humanShort(d) + "/" + humanShort(fp.size)
		if fp.speed > 0 {
			eta = etaStr(float64(fp.size-d) / fp.speed)
		} else {
			eta = "--"
		}
	} else {
		pct = "  · "
		sizes = humanShort(d)
		eta = ""
	}
	spd := humanShort(int64(fp.speed)) + "/s"

	icon := col(p.color, aCyan, "⬇")
	return fmt.Sprintf(" %s %s  %s %s  %s  %s  %s",
		icon, name, bar, pct,
		col(p.color, aBold, sizes),
		col(p.color, aGreen, spd),
		col(p.color, aDim, eta))
}

func (p *Progress) segLine(fp *fileProg, w int) string {
	var parts []string
	for _, s := range fp.segs {
		parts = append(parts, miniBar(s.frac(), 4, p.color))
	}
	tail := col(p.color, aDim, fmt.Sprintf("(%d connessioni)", len(fp.segs)))
	return "    " + col(p.color, aDim, "└ seg ") + strings.Join(parts, " ") + "  " + tail
}

func (p *Progress) finish() {
	p.once.Do(func() { close(p.stop) })
	if p.quiet {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tty && p.liveLines > 0 {
		var b strings.Builder
		if p.liveLines > 1 {
			fmt.Fprintf(&b, "\033[%dA", p.liveLines-1)
		}
		b.WriteString("\r\033[J")
		fmt.Fprint(stderr, b.String())
		p.liveLines = 0
	}
	b := atomic.LoadInt64(&p.bytes)
	f := atomic.LoadInt64(&p.done)
	el := time.Since(p.start).Seconds()
	avg := float64(0)
	if el > 0 {
		avg = float64(b) / el
	}
	icon := col(p.color, aBold+aGreen, "✓")
	fmt.Fprintf(stderr, "%s %s file · %s in %s · media %s\n",
		icon, itoa(int(f)), humanShort(b), etaStr(el), humanShort(int64(avg))+"/s")
}

// ---- barre e formattazione ----

var barEighths = []rune{' ', '▏', '▎', '▍', '▌', '▋', '▊', '▉'}

func mainBar(frac float64, width int, color bool) string {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	full := frac * float64(width)
	fullCells := int(full)
	var filled strings.Builder
	for i := 0; i < fullCells && i < width; i++ {
		filled.WriteRune('█')
	}
	rem := width - fullCells
	var partial string
	if fullCells < width {
		idx := int((full - float64(fullCells)) * 8)
		if idx > 0 {
			partial = string(barEighths[idx])
			rem--
		}
	}
	empty := strings.Repeat("░", max(rem, 0))
	left := col(color, aDim, "▕")
	right := col(color, aDim, "▏")
	return left + col(color, aGreen, filled.String()+partial) + col(color, aDim, empty) + right
}

func miniBar(frac float64, width int, color bool) string {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	k := int(frac*float64(width) + 0.5)
	if k > width {
		k = width
	}
	return col(color, aGreen, strings.Repeat("▰", k)) + col(color, aDim, strings.Repeat("▱", width-k))
}

// humanShort formatta i byte in forma compatta: 1.8G, 940M, 512K, 64B.
func humanShort(n int64) string {
	const u = 1024.0
	f := float64(n)
	if n < 1024 {
		return itoa(int(n)) + "B"
	}
	units := "KMGTPE"
	i := -1
	for f >= u && i < len(units)-1 {
		f /= u
		i++
	}
	if f >= 100 {
		return fmt.Sprintf("%.0f%c", f, units[i])
	}
	return fmt.Sprintf("%.1f%c", f, units[i])
}

func etaStr(sec float64) string {
	if sec < 0 || sec > 99*3600 {
		return "--"
	}
	s := int(sec + 0.5)
	h, m, ss := s/3600, (s%3600)/60, s%60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh%02dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm%02ds", m, ss)
	default:
		return fmt.Sprintf("%ds", ss)
	}
}

// fitName tronca/allarga un nome a esattamente n celle (conteggio rune).
func fitName(s string, n int) string {
	r := []rune(s)
	if len(r) == n {
		return s
	}
	if len(r) > n {
		if n <= 1 {
			return string(r[:n])
		}
		return string(r[:n-1]) + "…"
	}
	return s + strings.Repeat(" ", n-len(r))
}

func termWidthOr(def int) int {
	w, _ := termSize()
	if w <= 0 {
		return def
	}
	return w
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var _ = utf8.RuneCountInString
