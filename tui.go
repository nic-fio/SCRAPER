package main

import (
	"os"
	"os/signal"
	"strings"
	"unicode/utf8"
)

// styled è una stringa eventualmente colorata con la sua larghezza visibile.
type styled struct {
	s string
	n int
}

func plain(s string) styled  { return styled{s, utf8.RuneCountInString(s)} }
func blankLine() styled      { return styled{"", 0} }

// ---- modello dei contenuti (schede) ----

type tabDef struct {
	name  string
	build func(innerW int, color bool) []styled
}

func flagLine(flag string, color bool) styled {
	s := " " + col(color, aBold+aGreen, flag)
	return styled{s, 1 + utf8.RuneCountInString(flag)}
}

func headLine(text string, color bool) styled {
	return styled{col(color, aBold+aYel, text), utf8.RuneCountInString(text)}
}

func optLines(rows []helpRow, innerW int, color bool) []styled {
	var out []styled
	for i, r := range rows {
		if i > 0 {
			out = append(out, blankLine())
		}
		out = append(out, flagLine(r.flag, color))
		for _, l := range wrapText(r.desc, innerW, 4) {
			out = append(out, plain(l))
		}
	}
	return out
}

func buildTabs() []tabDef {
	tabs := []tabDef{
		{"Info", func(innerW int, color bool) []styled {
			var out []styled
			out = append(out, headLine("NOME", color), plain("   scrap — il coltellino svizzero del download"), blankLine())
			out = append(out, headLine("SINTASSI", color), plain("   scrap [opzioni] URL [URL...]"), plain("   scrap [opzioni] -i lista.txt"), blankLine())
			out = append(out, headLine("DESCRIZIONE", color))
			for _, l := range wrapText(descrizione, innerW, 3) {
				out = append(out, plain(l))
			}
			return out
		}},
	}
	short := map[string]string{
		"Download e multi-segmentazione": "Download",
		"Crawling":                       "Crawl",
		"Filtri":                         "Filtri",
		"Autenticazione e cookie":        "Auth",
		"Generali":                       "Generali",
	}
	for _, sec := range helpSections {
		sec := sec
		name := short[sec.title]
		if name == "" {
			name = sec.title
		}
		tabs = append(tabs, tabDef{name, func(innerW int, color bool) []styled {
			out := []styled{headLine(sec.title, color), blankLine()}
			return append(out, optLines(sec.rows, innerW, color)...)
		}})
	}
	tabs = append(tabs, tabDef{"Esempi", func(innerW int, color bool) []styled {
		var out []styled
		for i, ex := range helpExamples {
			if i > 0 {
				out = append(out, blankLine())
			}
			out = append(out, flagLine(ex.cmd, color))
			for _, l := range wrapText(ex.note, innerW, 4) {
				out = append(out, plain(l))
			}
		}
		return out
	}})
	return tabs
}

// ---- la TUI ----

type tui struct {
	w, h   int
	color  bool
	tab    int
	scroll []int
	tabs   []tabDef
}

// codici tasto
const (
	kNone = iota
	kQuit
	kUp
	kDown
	kLeft
	kRight
	kPgUp
	kPgDn
	kHome
	kEnd
	kTab
	kShiftTab
)

func runTUI() error {
	w, h := termSize()
	if w < 44 || h < 10 {
		return errf2("terminale troppo piccolo")
	}
	old, err := enterRaw(0)
	if err != nil {
		return err
	}
	defer restoreTerm(0, old)

	t := &tui{w: w, h: h, color: os.Getenv("NO_COLOR") == "", tabs: buildTabs()}
	t.scroll = make([]int, len(t.tabs))

	out := os.Stdout
	out.WriteString("\033[?1049h\033[?25l") // schermo alternativo, nascondi cursore
	defer out.WriteString("\033[?25h\033[?1049l")

	winch := make(chan os.Signal, 1)
	if len(winchSignals) > 0 {
		signal.Notify(winch, winchSignals...)
		defer signal.Stop(winch)
	}

	death := make(chan os.Signal, 1)
	signal.Notify(death, deathSignals...)
	defer signal.Stop(death)

	keyCh := make(chan [2]int, 8)
	go t.readLoop(keyCh)

	t.draw()
	for {
		select {
		case <-winch:
			t.w, t.h = termSize()
			t.draw()
		case <-death:
			return nil // i defer ripristinano il terminale
		case ev := <-keyCh:
			if t.handle(ev[0], ev[1]) {
				return nil
			}
			t.draw()
		}
	}
}

func (t *tui) readLoop(keyCh chan<- [2]int) {
	buf := make([]byte, 64)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			keyCh <- [2]int{kQuit, -1}
			return
		}
		for _, ev := range parseKeys(buf[:n]) {
			keyCh <- ev
		}
	}
}

func (t *tui) contentHeight() int { return t.h - 6 }

func (t *tui) handle(code, digit int) (quit bool) {
	lines := t.tabs[t.tab].build(t.w-4, t.color)
	maxScroll := len(lines) - t.contentHeight()
	if maxScroll < 0 {
		maxScroll = 0
	}
	clamp := func() {
		if t.scroll[t.tab] < 0 {
			t.scroll[t.tab] = 0
		}
		if t.scroll[t.tab] > maxScroll {
			t.scroll[t.tab] = maxScroll
		}
	}
	switch code {
	case kQuit:
		return true
	case kLeft, kShiftTab:
		t.tab = (t.tab - 1 + len(t.tabs)) % len(t.tabs)
	case kRight, kTab:
		t.tab = (t.tab + 1) % len(t.tabs)
	case kUp:
		t.scroll[t.tab]--
		clamp()
	case kDown:
		t.scroll[t.tab]++
		clamp()
	case kPgUp:
		t.scroll[t.tab] -= t.contentHeight() - 1
		clamp()
	case kPgDn:
		t.scroll[t.tab] += t.contentHeight() - 1
		clamp()
	case kHome:
		t.scroll[t.tab] = 0
	case kEnd:
		t.scroll[t.tab] = maxScroll
	case kNone:
		if digit >= 0 && digit < len(t.tabs) {
			t.tab = digit
		}
	}
	return false
}

// parseKeys interpreta TUTTI i tasti contenuti nel buffer (una Read può
// contenere più pressioni o una sequenza di escape multi-byte).
func parseKeys(b []byte) [][2]int {
	var out [][2]int
	i := 0
	for i < len(b) {
		c := b[i]
		if c == 0x1b { // ESC
			// sequenza CSI/SS3: ESC [ … oppure ESC O …
			if i+2 < len(b) && (b[i+1] == '[' || b[i+1] == 'O') {
				switch b[i+2] {
				case 'A':
					out = append(out, [2]int{kUp, -1})
				case 'B':
					out = append(out, [2]int{kDown, -1})
				case 'C':
					out = append(out, [2]int{kRight, -1})
				case 'D':
					out = append(out, [2]int{kLeft, -1})
				case 'H':
					out = append(out, [2]int{kHome, -1})
				case 'F':
					out = append(out, [2]int{kEnd, -1})
				case 'Z':
					out = append(out, [2]int{kShiftTab, -1})
				case '5':
					out = append(out, [2]int{kPgUp, -1})
				case '6':
					out = append(out, [2]int{kPgDn, -1})
				}
				// consuma fino alla '~' finale per le sequenze tipo ESC [ 5 ~
				j := i + 2
				for j < len(b) && b[j] != '~' && !(b[j] >= 'A' && b[j] <= 'Z') {
					j++
				}
				i = j + 1
				continue
			}
			// ESC isolato = esci
			out = append(out, [2]int{kQuit, -1})
			i++
			continue
		}
		switch c {
		case 'q', 'Q', 3:
			out = append(out, [2]int{kQuit, -1})
		case '\t':
			out = append(out, [2]int{kTab, -1})
		case 'h':
			out = append(out, [2]int{kLeft, -1})
		case 'l', '\r', '\n':
			out = append(out, [2]int{kRight, -1})
		case 'j':
			out = append(out, [2]int{kDown, -1})
		case 'k':
			out = append(out, [2]int{kUp, -1})
		case ' ':
			out = append(out, [2]int{kPgDn, -1})
		case 'b':
			out = append(out, [2]int{kPgUp, -1})
		case 'g':
			out = append(out, [2]int{kHome, -1})
		case 'G':
			out = append(out, [2]int{kEnd, -1})
		default:
			if c >= '1' && c <= '9' {
				out = append(out, [2]int{kNone, int(c - '1')})
			}
		}
		i++
	}
	return out
}

// ---- disegno ----

func repeatRune(r string, n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(r, n)
}

func hborder(left, right, title string, w int, color bool) string {
	inner := w - 2
	var fill string
	if title != "" {
		tt := "─ " + title + " "
		tn := utf8.RuneCountInString(tt)
		if tn > inner {
			tn = inner
		}
		fill = col(color, aBold+aCyan, tt) + repeatRune("─", inner-tn)
	} else {
		fill = repeatRune("─", inner)
	}
	return col(color, aDim, left) + (func() string {
		if title != "" {
			return fill
		}
		return col(color, aDim, fill)
	}()) + col(color, aDim, right)
}

func (t *tui) boxRow(inner styled) string {
	innerW := t.w - 4
	pad := innerW - inner.n
	if pad < 0 {
		pad = 0
	}
	v := col(t.color, aDim, "│")
	return v + " " + inner.s + repeatRune(" ", pad) + " " + v
}

func (t *tui) tabBar() styled {
	var sb strings.Builder
	n := 0
	for i, tb := range t.tabs {
		label := " " + tb.name + " "
		w := utf8.RuneCountInString(label)
		switch {
		case i == t.tab && t.color:
			sb.WriteString("\033[7m\033[1m" + label + "\033[0m")
		case i == t.tab:
			sb.WriteString(">" + tb.name + "<")
		case t.color:
			sb.WriteString("\033[2m" + label + "\033[0m")
		default:
			sb.WriteString(label)
		}
		n += w
	}
	return styled{sb.String(), n}
}

func (t *tui) draw() {
	lines := t.tabs[t.tab].build(t.w-4, t.color)
	ch := t.contentHeight()
	off := t.scroll[t.tab]

	var rows []string
	rows = append(rows, hborder("┌", "┐", "scrap — guida", t.w, t.color))
	rows = append(rows, t.boxRow(t.tabBar()))
	rows = append(rows, hborder("├", "┤", "", t.w, t.color))
	for i := 0; i < ch; i++ {
		idx := off + i
		if idx < len(lines) {
			rows = append(rows, t.boxRow(lines[idx]))
		} else {
			rows = append(rows, t.boxRow(blankLine()))
		}
	}
	rows = append(rows, hborder("├", "┤", "", t.w, t.color))

	// barra di stato
	total := len(lines)
	bottom := off + ch
	if bottom > total {
		bottom = total
	}
	pos := "righe " + itoa(off+1) + "-" + itoa(bottom) + "/" + itoa(total)
	hint := "←/→ schede · ↑/↓ scorri · PgUp/PgDn · g/G · q esci"
	status := hint
	if utf8.RuneCountInString(hint)+utf8.RuneCountInString(pos)+3 <= t.w-4 {
		gap := (t.w - 4) - utf8.RuneCountInString(hint) - utf8.RuneCountInString(pos)
		status = hint + repeatRune(" ", gap) + pos
	}
	rows = append(rows, t.boxRow(styled{col(t.color, aDim, status), utf8.RuneCountInString(stripANSI(status))}))
	rows = append(rows, hborder("└", "┘", "", t.w, t.color))

	os.Stdout.WriteString("\033[H" + strings.Join(rows, "\r\n") + "\033[J")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// Le primitive di basso livello (termios, ioctl, segnali) sono definite nei
// file term_linux.go / term_other.go in base alla piattaforma.
