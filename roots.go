package main

import (
	"crypto/x509"
	_ "embed"
)

// caBundle contiene un insieme di root CA pubblici (estratto da Mozilla e
// distribuito da curl.se) incorporato direttamente nel binario tramite go:embed.
// Aggiornare con: curl -fsSL -o cacerts.pem https://curl.se/ca/cacert.pem
//
//go:embed cacerts.pem
var caBundle []byte

// init registra i root CA incorporati come fallback per la verifica TLS. Go li
// usa soltanto quando sulla macchina non è disponibile un pool di certificati di
// sistema (es. immagini Linux minimali o container "scratch" senza il pacchetto
// ca-certificates): sulle installazioni normali restano prioritari i root del
// sistema, che ricevono gli aggiornamenti dell'OS. Così l'HTTPS funziona ovunque
// senza dipendenze esterne.
func init() {
	pool := x509.NewCertPool()
	if pool.AppendCertsFromPEM(caBundle) {
		x509.SetFallbackRoots(pool)
	}
}
