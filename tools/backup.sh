#!/bin/sh
# Impacchetta tutto il progetto in un solo file: un git bundle con ogni ramo,
# ogni tag e la storia completa. Si ripristina senza rete e senza GitHub:
#
#     tools/backup.sh ~/backup                 # scrive scrap-AAAA-MM-GG.bundle
#     git clone scrap-2026-09-24.bundle SCRAPER
#     cd SCRAPER && ./scrap --help
#
# Il bundle contiene solo ciò che è registrato nel repository: i file di
# cookie (cookies.txt e simili) e i download restano fuori, come su GitHub.
set -eu

DEST=${1:-.}
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DAY=$(date +%F)
OUT="$DEST/scrap-$DAY.bundle"

[ -d "$DEST" ] || mkdir -p "$DEST"
git -C "$ROOT" bundle create "$OUT" --all
git -C "$ROOT" bundle verify "$OUT" >/dev/null

if [ -n "$(git -C "$ROOT" status --porcelain)" ]; then
    echo "attenzione: le modifiche non ancora registrate NON sono nel bundle:"
    git -C "$ROOT" status --short
fi
if [ -n "$(git -C "$ROOT" log --oneline '@{upstream}..HEAD' 2>/dev/null)" ]; then
    echo "nota: il bundle ha commit che non sono ancora su GitHub."
fi

printf '%s: %s, %s commit, tag: %s\n' "$OUT" "$(du -h "$OUT" | cut -f1)" \
    "$(git -C "$ROOT" rev-list --count HEAD)" \
    "$(git -C "$ROOT" tag | tr '\n' ' ')"
