//go:build linux

package main

import (
	"os"
	"syscall"
	"unsafe"
)

// termState conserva lo stato del terminale da ripristinare.
type termState = syscall.Termios

type winsize struct{ Row, Col, Xpix, Ypix uint16 }

// segnali gestiti dalla TUI su Linux.
var (
	winchSignals = []os.Signal{syscall.SIGWINCH}
	deathSignals = []os.Signal{syscall.SIGTERM, syscall.SIGHUP}
)

// termSize restituisce (colonne, righe) del terminale, con fallback a 80x24.
func termSize() (int, int) {
	for _, f := range []*os.File{os.Stdout, os.Stderr, os.Stdin} {
		ws := &winsize{}
		_, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(),
			uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(ws)))
		if e == 0 && ws.Col > 0 {
			return int(ws.Col), int(ws.Row)
		}
	}
	if tty, err := os.Open("/dev/tty"); err == nil {
		defer tty.Close()
		ws := &winsize{}
		_, _, e := syscall.Syscall(syscall.SYS_IOCTL, tty.Fd(),
			uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(ws)))
		if e == 0 && ws.Col > 0 {
			return int(ws.Col), int(ws.Row)
		}
	}
	return 80, 24
}

// enterRaw mette il terminale in raw mode e restituisce lo stato precedente.
func enterRaw(fd int) (*termState, error) {
	old := &termState{}
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd),
		uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(old))); e != 0 {
		return nil, e
	}
	raw := *old
	raw.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Iflag &^= syscall.IXON | syscall.ICRNL | syscall.BRKINT | syscall.INPCK | syscall.ISTRIP
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd),
		uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&raw))); e != 0 {
		return nil, e
	}
	return old, nil
}

// restoreTerm ripristina lo stato del terminale.
func restoreTerm(fd int, old *termState) {
	if old != nil {
		syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd),
			uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(old)))
	}
}
