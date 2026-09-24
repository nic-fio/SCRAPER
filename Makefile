# scrap: build, test e controlli della documentazione.
#
#   make            ./scrap, il binario statico Linux x86-64 registrato nel repository
#   make test       go vet, gofmt, test con -race, controlli dei manuali
#   make docs-check solo i controlli dei manuali
#   make dist       binari statici per Linux (6 architetture), macOS e Windows in dist/
#   make install    copia ./scrap in /usr/local/bin (chiede sudo se serve)
#   make clean      elimina dist/ (./scrap resta: fa parte del repository)
#
# CGO disabilitato + go:embed dei certificati radice (roots.go) => binari
# statici e autosufficienti, senza dipendenze da glibc/musl né dal pacchetto
# ca-certificates del sistema.

GO      ?= go
LDFLAGS := -s -w
BUILD   := CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)"

# sistema/architettura[/variante ARM] : nome del file in dist/
TARGETS := linux/amd64:scrap-linux-amd64 linux/arm64:scrap-linux-arm64 \
           linux/arm/7:scrap-linux-armv7 linux/arm/6:scrap-linux-armv6 \
           linux/386:scrap-linux-386 linux/riscv64:scrap-linux-riscv64 \
           darwin/amd64:scrap-darwin-amd64 darwin/arm64:scrap-darwin-arm64 \
           windows/amd64:scrap-windows-amd64.exe

.PHONY: all build test vet fmt-check docs-check dist install clean

all: build

build:
	GOOS=linux GOARCH=amd64 $(BUILD) -o scrap .

vet:
	$(GO) vet ./...

fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "file da formattare con gofmt:"; echo "$$out"; exit 1; fi

docs-check:
	python3 tools/check-docs.py

test: vet fmt-check docs-check
	$(GO) test -race -count=1 ./...

dist:
	rm -rf dist && mkdir -p dist
	for t in $(TARGETS); do \
	  spec=$${t%%:*}; name=$${t#*:}; \
	  os=$${spec%%/*}; rest=$${spec#*/}; arch=$${rest%%/*}; arm=$${rest#*/}; \
	  [ "$$arm" = "$$rest" ] && arm=; \
	  GOOS=$$os GOARCH=$$arch GOARM=$$arm $(BUILD) -o dist/$$name . || exit 1; \
	done
	cd dist && sha256sum scrap-* > SHA256SUMS && cat SHA256SUMS

install: build
	install -m 0755 scrap /usr/local/bin/scrap

clean:
	rm -rf dist
