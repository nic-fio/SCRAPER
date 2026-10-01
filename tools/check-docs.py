#!/usr/bin/env python3
"""Controlla che i manuali siano allineati al codice. Eseguito da 'make test'.

Fallisce se:
  - un'opzione della guida integrata (helpSections in help.go) non ha la sua
    voce nel riferimento del manuale utente, o il manuale documenta
    un'opzione che non esiste;
  - un'opzione definita in main.go non compare nella guida integrata;
  - un file .go manca dalla mappa dei file del manuale tecnico, o il numero
    di righe indicato si scosta di più del 10% da quello vero;
  - l'eseguibile ./scrap (registrato nel repository) manca, non è un ELF
    x86-64 o non contiene la versione di main.go;
  - la versione di main.go non coincide con quella di README.md,
    docs/index.html e dei due manuali;
  - un manuale non dichiara lang="en" (i manuali sono in inglese);
  - un link interno #ancora dei manuali punta a un id inesistente, o un link
    a una pagina locale (manuali e docs/index.html) punta a un file che non
    esiste in docs/.
"""
import pathlib
import re
import sys
import urllib.parse

ROOT = pathlib.Path(__file__).resolve().parent.parent
DOCS = ROOT / "docs"
USER = DOCS / "User Manual.html"
TECH = DOCS / "Technical Manual.html"

errors = []


def err(msg):
    errors.append(msg)


help_go = (ROOT / "help.go").read_text()
main_go = (ROOT / "main.go").read_text()
user = USER.read_text()
tech = TECH.read_text()

# ---- opzioni: guida integrata <-> manuale utente ----
sections = help_go[help_go.index("var helpSections"):help_go.index("type helpExample")]
help_flags = set()
for spec in re.findall(r'\{"(-[^"]+)",', sections):
    for part in spec.split(","):
        help_flags.add(part.strip().split()[0])

ref = user[user.index('id="riferimento"'):user.index('id="file-usati"')]
doc_flags = set()
for idx in re.findall(r'<dt[^>]*data-kind="opzione"[^>]*data-index="([^"]+)"', ref):
    doc_flags.update(x.strip() for x in idx.split(";") if x.strip())

for f in sorted(help_flags - doc_flags):
    err(f"manuale utente: manca l'opzione {f} nel riferimento")
for f in sorted(doc_flags - help_flags):
    err(f"manuale utente: l'opzione {f} non esiste nella guida integrata")

# ---- opzioni: codice (main.go) <-> guida integrata ----
defined = set()
for kind, args in re.findall(r'\b(fs\.\w+Var|fs\.Var|intVar|strVar|boolVar)\((.*)\)', main_go):
    names = re.findall(r'"([^"]*)"', args)
    if kind in ("intVar", "strVar", "boolVar"):
        defined.update(names[:2])
    elif names:
        defined.add(names[0])
help_names = {f.lstrip("-").split("=")[0] for f in help_flags}
for name in sorted(defined - help_names):
    err(f"help.go: l'opzione -{name} è definita in main.go ma manca nella guida")

# ---- mappa dei file ----
fmap = tech[tech.index('id="file-map"'):]
fmap = fmap[:fmap.index("</table>")]
rows = dict(re.findall(r"<tr><td><code>([\w.]+\.go)</code></td><td>(\d+)</td>", fmap))
for go in sorted(p.name for p in ROOT.glob("*.go")):
    real = len((ROOT / go).read_text().splitlines())
    if go not in rows:
        err(f"manuale tecnico: {go} ({real} righe) manca dalla mappa dei file")
        continue
    doc = int(rows.pop(go))
    if abs(doc - real) > max(3, real // 10):
        err(f"manuale tecnico: {go} ha {real} righe, la mappa dice {doc}")
for go in sorted(rows):
    err(f"manuale tecnico: la mappa cita {go}, che non esiste")

# ---- versione ----
m = re.search(r'appVersion\s*=\s*"(\d+\.\d+(?:\.\d+)?)"', main_go)
version = m.group(1) if m else None
if not version:
    err('main.go: costante appVersion non trovata')
else:
    v = re.escape(version)
    # i manuali sono in inglese: copertina e piè di pagina dello stile comune
    manual = [rf"<span>Version</span><b>{v}</b>", r'<footer class="doc-foot">[^<]* · Version ' + v + r" · "]
    checks = {
        "README.md": [rf"Versione {v}\b"],
        "docs/index.html": [rf"<span>Versione</span><b>{v}</b>"],
        "docs/User Manual.html": manual,
        "docs/Technical Manual.html": manual,
    }
    for rel, pats in checks.items():
        text = (ROOT / rel).read_text()
        for pat in pats:
            if not re.search(pat, text):
                err(f"{rel}: la versione non è {version} (come in main.go)")

# ---- eseguibile registrato ----
exe = ROOT / "scrap"
if not exe.is_file():
    err("manca l'eseguibile ./scrap: esegui 'make' e registralo")
else:
    data = exe.read_bytes()
    # ELF, 64 bit, x86-64 (e_machine = 0x3E)
    if data[:4] != b"\x7fELF" or data[4] != 2 or data[18:20] != b"\x3e\x00":
        err("./scrap non è un eseguibile Linux x86-64: rigeneralo con 'make'")
    elif version and ("scrap/" + version + " (+").encode() not in data:
        err(f"./scrap non contiene la versione {version}: rigeneralo con 'make'")

# ---- lingua dei manuali ----
for path, text in ((USER, user), (TECH, tech)):
    if '<html lang="en">' not in text:
        err(f'{path.name}: manca <html lang="en"> (i manuali sono in inglese)')

# ---- link a pagine locali (i nomi dei manuali contengono spazi: %20) ----
INDEX = DOCS / "index.html"
for path, text in ((USER, user), (TECH, tech), (INDEX, INDEX.read_text())):
    # lo script comune dei manuali compone i link a pezzi: non sono link veri
    text = re.sub(r"<script\b.*?</script>", "", text, flags=re.S)
    for href in sorted(set(re.findall(r'href="([^"#]+)(?:#[^"]*)?"', text))):
        if re.match(r"[a-z][a-z0-9+.-]*:", href, re.I):
            continue  # http:, https:, mailto: ...
        if not (DOCS / urllib.parse.unquote(href)).exists():
            err(f"{path.name}: link a {href}, che non esiste in docs/")

# ---- ancore interne ----
for path, text in ((USER, user), (TECH, tech)):
    ids = set(re.findall(r'\bid="([^"]+)"', text))
    for anchor in sorted(set(re.findall(r'href="#([^"]+)"', text))):
        if anchor not in ids:
            err(f"{path.name}: link a #{anchor}, che non esiste")
    other = TECH if path == USER else USER
    other_ids = set(re.findall(r'\bid="([^"]+)"', other.read_text()))
    other_href = re.escape(urllib.parse.quote(other.name))
    for anchor in sorted(set(re.findall(r'href="' + other_href + r'#([^"]+)"', text))):
        if anchor not in other_ids:
            err(f"{path.name}: link a {other.name}#{anchor}, che non esiste")

if errors:
    print("controlli della documentazione: FALLITI")
    for e in errors:
        print("  - " + e)
    sys.exit(1)
print(f"controlli della documentazione: ok ({len(help_flags)} opzioni, "
      f"{len(list(ROOT.glob('*.go')))} file, versione {version})")
