# Documentazione di scrap

GitHub mostra i file `.html` come codice sorgente: non visualizza le pagine web
salvate in un repository. I manuali (in inglese) si leggono in uno di questi due modi.

**Online** (GitHub Pages, sempre allineati a `main`):

| Documento | Link |
|---|---|
| Manuale utente | https://nic-fio.github.io/SCRAPER/User%20Manual.html |
| Manuale tecnico | https://nic-fio.github.io/SCRAPER/Technical%20Manual.html |
| Pagina iniziale | https://nic-fio.github.io/SCRAPER/ |

**Senza rete**: clona il repository e apri `docs/User Manual.html` nel
browser. Ogni manuale è un unico file con stile e script incorporati; i
diagrammi usano la copia locale di Mermaid in `docs/assets/vendor`, quindi
funzionano anche offline.

[Decisioni e storia](decisions-and-history.md) è in Markdown, quindi GitHub lo
mostra già formattato.

Dentro il programma, `scrap --help` mostra la guida integrata a schede.

## File

| File | Cosa |
|---|---|
| `index.html` | Pagina iniziale del sito della documentazione. |
| `User Manual.html` | Installare e usare scrap; riferimento di tutte le opzioni. |
| `Technical Manual.html` | Architettura, funzionamento interno, test, convenzioni, limiti noti. |
| `decisions-and-history.md` | Perché scrap è fatto così. |
| `assets/vendor/` | Copia locale di Mermaid (MIT), per i diagrammi dei manuali. |

`tools/check-docs.py` (eseguito da `make test`) verifica che i manuali siano
allineati al codice.
