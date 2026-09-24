# Copyright e componenti di terzi

scrap
Copyright (c) 2026 nic-fio. Tutti i diritti riservati.

Il codice è pubblicato perché si possa leggere e recuperare. Non è concessa
alcuna licenza d'uso, copia, modifica o distribuzione: per qualunque uso
diverso dalla lettura serve un permesso esplicito dell'autore (apri una
issue su https://github.com/nic-fio/SCRAPER/issues).

Nota storica: la versione 1.0 (giugno 2026) era pubblicata con la licenza
PolyForm Noncommercial 1.0.0. Dalla migrazione in questo repository
(settembre 2026) il codice è pubblicato senza licenza.

## Componenti di terzi

Questi componenti mantengono le proprie licenze: quanto scritto sopra vale per
scrap, non per loro.

| Componente | Dove | Licenza |
|---|---|---|
| `golang.org/x/net` v0.30.0 (tokenizer HTML) | compilato dentro il programma | BSD 3-Clause, testo qui sotto |
| Certificati radice di Mozilla, estratti da curl.se | `cacerts.pem`, incorporato nel programma | Mozilla Public License 2.0, https://mozilla.org/MPL/2.0/ (origine dei dati nell'intestazione del file) |
| Mermaid (diagrammi dei manuali) | `docs/assets/vendor/mermaid.min.js` | MIT, vedi `docs/assets/vendor/mermaid.LICENSE` |

### Licenza di golang.org/x/net

```
Copyright 2009 The Go Authors.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Google LLC nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```
