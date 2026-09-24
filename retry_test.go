package main

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestParseRetryAfter(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"", -1},
		{"   ", -1},
		{"120", 120 * time.Second},
		{"0", 0},
		{"-5", -1},
		{"abc", -1},
		{"Mon, 02 Jan 2006 15:04:05 GMT", 0}, // data nel passato -> 0
	}
	for _, c := range cases {
		if got := parseRetryAfter(c.in); got != c.want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", c.in, got, c.want)
		}
	}
	// data ampiamente nel futuro: deve restituire un valore positivo.
	if got := parseRetryAfter("Fri, 31 Dec 2100 23:59:59 GMT"); got <= 0 {
		t.Errorf("parseRetryAfter(futuro) = %v, want > 0", got)
	}
}

func TestIsRetryable(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{&httpStatusError{code: 400}, false},
		{&httpStatusError{code: 401}, false},
		{&httpStatusError{code: 403}, false},
		{&httpStatusError{code: 404}, false},
		{&httpStatusError{code: http.StatusRequestTimeout}, true},  // 408
		{&httpStatusError{code: http.StatusTooManyRequests}, true}, // 429
		{&httpStatusError{code: 500}, true},
		{&httpStatusError{code: 502}, true},
		{&httpStatusError{code: 503}, true},
		{errors.New("connection reset"), true}, // errori di rete: transitori
	}
	for _, c := range cases {
		if got := isRetryable(c.err); got != c.want {
			t.Errorf("isRetryable(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestBackoffRetryAfter(t *testing.T) {
	e := &Engine{cfg: &Config{RetryWait: 2}}
	// Retry-After esplicito viene rispettato tale e quale.
	if got := e.backoff(1, &httpStatusError{code: 503, retryAfter: 3 * time.Second}); got != 3*time.Second {
		t.Errorf("backoff con Retry-After=3s = %v, want 3s", got)
	}
	// Retry-After oltre il tetto viene limitato a maxBackoff.
	if got := e.backoff(1, &httpStatusError{code: 503, retryAfter: 120 * time.Second}); got != maxBackoff {
		t.Errorf("backoff con Retry-After=120s = %v, want %v", got, maxBackoff)
	}
}

func TestBackoffExponential(t *testing.T) {
	e := &Engine{cfg: &Config{RetryWait: 2}}
	// Senza Retry-After: base 2s, raddoppio a ogni tentativo, jitter di +-20%.
	// attempt 1 -> ~2s, attempt 3 -> ~8s. Verifichiamo i limiti su molte iterazioni
	// per coprire il jitter casuale.
	check := func(attempt int, center time.Duration) {
		lo := time.Duration(float64(center) * 0.8)
		hi := time.Duration(float64(center) * 1.2)
		for i := 0; i < 200; i++ {
			d := e.backoff(attempt, errors.New("boom"))
			if d < lo || d > hi {
				t.Fatalf("backoff(attempt=%d) = %v, atteso in [%v, %v]", attempt, d, lo, hi)
			}
		}
	}
	check(1, 2*time.Second)
	check(2, 4*time.Second)
	check(3, 8*time.Second)
}

func TestBackoffCap(t *testing.T) {
	e := &Engine{cfg: &Config{RetryWait: 2}}
	// Con molti tentativi la crescita esponenziale non deve superare maxBackoff
	// (piu' il jitter, che qui e' comunque limitato dal tetto sull'esponenziale).
	hi := time.Duration(float64(maxBackoff) * 1.2)
	for i := 0; i < 200; i++ {
		d := e.backoff(20, errors.New("boom"))
		if d > hi {
			t.Fatalf("backoff(attempt=20) = %v, oltre il tetto atteso %v", d, hi)
		}
	}
}
