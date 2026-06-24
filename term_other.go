//go:build !linux

package main

import (
	"errors"
	"os"
)

// Su piattaforme diverse da Linux la TUI interattiva non è disponibile: si
// ripiega automaticamente sulla guida testuale (pager / testo semplice). Il
// resto del programma (download, crawling, filtri) funziona normalmente.

type termState struct{}

var (
	winchSignals = []os.Signal{}
	deathSignals = []os.Signal{os.Interrupt}
)

func termSize() (int, int) { return 80, 24 }

func enterRaw(fd int) (*termState, error) {
	return nil, errors.New("TUI non disponibile su questa piattaforma")
}

func restoreTerm(fd int, old *termState) {}
