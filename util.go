package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
)

// parseSize converte stringhe tipo "10M", "1.5G", "500k", "2048" in byte.
func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	mult := float64(1)
	last := s[len(s)-1]
	switch last {
	case 'k', 'K':
		mult = 1 << 10
		s = s[:len(s)-1]
	case 'm', 'M':
		mult = 1 << 20
		s = s[:len(s)-1]
	case 'g', 'G':
		mult = 1 << 30
		s = s[:len(s)-1]
	case 't', 'T':
		mult = 1 << 40
		s = s[:len(s)-1]
	case 'b', 'B':
		// consenti forme tipo "10MB"
		s = strings.TrimSpace(s[:len(s)-1])
		if s != "" {
			switch s[len(s)-1] {
			case 'k', 'K':
				mult = 1 << 10
				s = s[:len(s)-1]
			case 'm', 'M':
				mult = 1 << 20
				s = s[:len(s)-1]
			case 'g', 'G':
				mult = 1 << 30
				s = s[:len(s)-1]
			case 't', 'T':
				mult = 1 << 40
				s = s[:len(s)-1]
			}
		}
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("dimensione non valida: %q", s)
	}
	return int64(v * mult), nil
}

// humanBytes formatta un numero di byte in forma leggibile.
func humanBytes(n int64) string {
	const u = 1024
	if n < u {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(u), 0
	for x := n / u; x >= u; x /= u {
		div *= u
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// RateLimiter è un token-bucket globale condiviso da tutti i lettori.
type RateLimiter struct {
	mu     sync.Mutex
	rate   float64 // byte/sec, 0 = illimitato
	tokens float64
	max    float64
	last   time.Time
}

func NewRateLimiter(rate int64) *RateLimiter {
	if rate <= 0 {
		return &RateLimiter{}
	}
	return &RateLimiter{
		rate:   float64(rate),
		tokens: float64(rate),
		max:    float64(rate),
		last:   time.Now(),
	}
}

// Take blocca finché n byte non sono "permessi" dalla banda configurata.
func (r *RateLimiter) Take(n int) {
	if r.rate <= 0 {
		return
	}
	r.mu.Lock()
	now := time.Now()
	r.tokens += now.Sub(r.last).Seconds() * r.rate
	r.last = now
	if r.tokens > r.max {
		r.tokens = r.max
	}
	// Prenota i token consentendo un deficit negativo: così più segmenti che
	// attendono in parallelo accumulano l'attesa invece di moltiplicare la
	// banda. L'attesa è proporzionale al deficit totale.
	r.tokens -= float64(n)
	var wait time.Duration
	if r.tokens < 0 {
		wait = time.Duration(-r.tokens / r.rate * float64(time.Second))
	}
	r.mu.Unlock()
	if wait > 0 {
		time.Sleep(wait)
	}
}

// meterReader avvolge un Reader applicando rate-limit e contatore globale.
type meterReader struct {
	r    io.Reader
	lim  *RateLimiter
	prog *Progress
}

func (m *meterReader) Read(p []byte) (int, error) {
	n, err := m.r.Read(p)
	if n > 0 {
		if m.lim != nil {
			m.lim.Take(n)
		}
		if m.prog != nil {
			m.prog.addBytes(int64(n))
		}
	}
	return n, err
}
