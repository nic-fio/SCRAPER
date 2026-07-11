package main

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxBackoff limita l'attesa massima tra due tentativi, così un Retry-After
// esagerato (o un backoff cresciuto troppo) non blocca il crawler all'infinito.
const maxBackoff = 60 * time.Second

// httpStatusError rappresenta una risposta HTTP con codice di errore e trasporta
// l'eventuale indicazione Retry-After del server, in modo che la logica di retry
// possa distinguere gli errori permanenti da quelli transitori e rispettare i
// tempi suggeriti.
type httpStatusError struct {
	code       int
	retryAfter time.Duration // <0 se il server non l'ha indicato
}

func (e *httpStatusError) Error() string { return fmt.Sprintf("HTTP %d", e.code) }

// statusError costruisce l'errore a partire dalla risposta, leggendo Retry-After.
func statusError(resp *http.Response) *httpStatusError {
	return &httpStatusError{
		code:       resp.StatusCode,
		retryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
	}
}

// parseRetryAfter interpreta l'header Retry-After nelle due forme ammesse dallo
// standard: numero di secondi oppure data HTTP. Restituisce <0 se assente o non
// valido, 0 se la data è già passata.
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return -1
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return -1
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
		return 0
	}
	return -1
}

// isRetryable stabilisce se ha senso ritentare per un dato errore. I 4xx sono
// permanenti (credenziali, URL, permessi: es. 401/403/404) e ritentarli identici
// non cambierebbe l'esito; fanno eccezione 408 (timeout) e 429 (troppe
// richieste), che dipendono dal ritmo. I 5xx e gli errori di rete/timeout sono
// invece considerati transitori.
func isRetryable(err error) bool {
	var he *httpStatusError
	if errors.As(err, &he) {
		if he.code >= 400 && he.code < 500 {
			return he.code == http.StatusRequestTimeout || he.code == http.StatusTooManyRequests
		}
		return true
	}
	return true
}

// withRetry esegue fn riprovando con backoff esponenziale e jitter finché
// l'errore è transitorio e non è stato superato il numero di tentativi. Si ferma
// subito sugli errori permanenti (4xx non recuperabili) e su errQuota, e
// rispetta l'header Retry-After quando presente.
func (e *Engine) withRetry(fn func() error) error {
	var err error
	for attempt := 0; attempt <= e.cfg.Retries; attempt++ {
		if attempt > 0 {
			d := e.backoff(attempt, err)
			logf(e.cfg, "ritento (%d/%d) tra %s dopo: %v", attempt, e.cfg.Retries, d.Round(time.Millisecond), err)
			time.Sleep(d)
		}
		err = fn()
		if err == nil || errors.Is(err, errQuota) {
			return err
		}
		if !isRetryable(err) {
			return err
		}
	}
	return err
}

// backoff calcola l'attesa prima del tentativo indicato (attempt >= 1). Se il
// server ha fornito Retry-After lo rispetta (limitato a maxBackoff); altrimenti
// applica un backoff esponenziale a partire da RetryWait, con tetto maxBackoff e
// un jitter di ±20% per evitare che più worker ripartano in sincrono.
func (e *Engine) backoff(attempt int, err error) time.Duration {
	var he *httpStatusError
	if errors.As(err, &he) && he.retryAfter >= 0 {
		if he.retryAfter > maxBackoff {
			return maxBackoff
		}
		return he.retryAfter
	}
	base := time.Duration(e.cfg.RetryWait) * time.Second
	if base <= 0 {
		base = time.Second
	}
	d := base
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= maxBackoff {
			d = maxBackoff
			break
		}
	}
	if span := int64(d) / 5; span > 0 {
		d += time.Duration(rand.Int64N(2*span+1)) - time.Duration(span)
	}
	return d
}
