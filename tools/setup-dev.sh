#!/bin/sh
# Prepara una copia appena clonata su una macchina Debian o Ubuntu.
#
# Un clone porta con sé sorgenti, test, documentazione, l'eseguibile pronto e
# tutta la storia, ma non due cose: i pacchetti che servono a ricostruire il
# programma e l'identità git di questo repository (sta in .git/config, che non
# viene clonato). Questo script dice cosa manca e, con --install, installa i
# pacchetti con apt.
#
#     tools/setup-dev.sh            # dice cosa manca
#     tools/setup-dev.sh --install  # lo installa (chiede sudo)
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

# pacchetto : un comando che fornisce : a cosa serve
PACKAGES="
golang-go:go:compilatore Go (serve la 1.23 o successiva)
build-essential:gcc:i test con -race (usano cgo)
make:make:i comandi make build / test / dist
python3:python3:i controlli della documentazione (tools/check-docs.py)
git:git:il repository
"

NAME=${SCRAP_GIT_NAME:-nic-fio}
EMAIL=${SCRAP_GIT_EMAIL:-315794250+nic-fio@users.noreply.github.com}

missing=
echo "Pacchetti:"
while IFS=: read -r pkg cmd why; do
    [ -z "$pkg" ] && continue
    if command -v "$cmd" >/dev/null 2>&1; then ok=yes; else ok=no; fi
    [ "$ok" = no ] && missing="$missing $pkg"
    printf '  [%s] %-16s %s\n' "$([ "$ok" = yes ] && echo ' ok ' || echo MANC)" "$pkg" "$why"
done <<LIST
$PACKAGES
LIST

if [ -n "$missing" ]; then
    if [ "${1:-}" = "--install" ]; then
        sudo apt-get update
        # shellcheck disable=SC2086
        sudo apt-get install -y --no-install-recommends $missing
    else
        echo
        echo "  sudo apt-get install -y --no-install-recommends$missing"
    fi
else
    echo "  non manca niente"
fi

if command -v go >/dev/null 2>&1; then
    echo
    echo "Go: $(go version)"
    echo "  (go.mod chiede la 1.23: con una versione più vecchia, ma almeno 1.21,"
    echo "   Go scarica da solo il toolchain giusto alla prima build)"
fi

echo
echo "Identità git di questa copia:"
if git -C "$ROOT" rev-parse --git-dir >/dev/null 2>&1; then
    if [ -z "$(git -C "$ROOT" config --local --get user.email || true)" ]; then
        git -C "$ROOT" config --local user.name "$NAME"
        git -C "$ROOT" config --local user.email "$EMAIL"
        echo "  impostata a $NAME <$EMAIL>"
        echo "  (tiene l'indirizzo personale fuori dalla storia pubblica;"
        echo "   SCRAP_GIT_NAME e SCRAP_GIT_EMAIL la cambiano)"
    else
        echo "  già impostata: $(git -C "$ROOT" config --local --get user.name) <$(git -C "$ROOT" config --local --get user.email)>"
    fi
    git -C "$ROOT" config --local core.hooksPath .githooks
    echo "  gancio pre-commit attivo: controlla i manuali prima di ogni commit"
else
    echo "  non è una copia git, saltata"
fi

cat <<'END'

Poi verifica che tutto funzioni davvero:

    make            # ricostruisce ./scrap dal sorgente
    make test       # vet, gofmt, test con -race, controlli dei manuali
    ./scrap --help  # la guida a schede
END
