BINARY := scrap
LDFLAGS := -s -w

# CGO disabilitato + go:embed dei root CA => binari statici e autosufficienti,
# senza dipendenze da glibc/musl né dal pacchetto ca-certificates del sistema.
GOBUILD := CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)"

.PHONY: build install clean test dist

build:
	$(GOBUILD) -o $(BINARY) .

test:
	go test ./...

install: build
	install -m 0755 $(BINARY) /usr/local/bin/$(BINARY)

# dist compila binari statici per tutte le principali architetture Linux.
# Girano su qualsiasi distribuzione con quella CPU, senza nulla di preinstallato.
dist:
	rm -rf dist && mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64          go build -ldflags "$(LDFLAGS)" -o dist/scrap-linux-amd64   .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64          go build -ldflags "$(LDFLAGS)" -o dist/scrap-linux-arm64   .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm   GOARM=7  go build -ldflags "$(LDFLAGS)" -o dist/scrap-linux-armv7   .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm   GOARM=6  go build -ldflags "$(LDFLAGS)" -o dist/scrap-linux-armv6   .
	CGO_ENABLED=0 GOOS=linux GOARCH=386            go build -ldflags "$(LDFLAGS)" -o dist/scrap-linux-386     .
	CGO_ENABLED=0 GOOS=linux GOARCH=riscv64        go build -ldflags "$(LDFLAGS)" -o dist/scrap-linux-riscv64 .
	@echo "Binari statici in dist/:" && ls -1 dist/

clean:
	rm -f $(BINARY)
	rm -rf dist
