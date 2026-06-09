# Recipearr

A self-hosted **"\*arr" for recipes**. Point it at recipe websites, and it watches their RSS/Atom
feeds, filters new recipes by keyword (whitelist/blacklist), and imports the ones you want into
your [Tandoor](https://tandoor.dev) server — cleaning them up on the way in.

Think Sonarr/Radarr, but the library is Tandoor and the "indexers" are recipe blogs.

| \*arr concept       | Recipearr                                   |
|--------------------|----------------------------------------------|
| Media library      | **Tandoor** (the destination)                |
| Indexers           | **Watched sites** (RSS/Atom feeds)           |
| Quality profiles   | **Keyword whitelist/blacklist rules**        |
| Monitoring         | **Scheduled polling** for new recipes        |
| Grab + import      | Parse → filter → **enrich** → create recipe  |

## Why not just use Tandoor's URL import?

Tandoor's built-in importer stores whatever `recipe-scrapers` emits. Recipearr adds the layer that
makes a recipe *nice*:

- **Communized foods** — strips leaked weights/parentheticals (`(1.75 oz; 50g) sugar` → `sugar`),
  resolves `X or Y`, moves prep words to notes, and reuses your existing Tandoor foods so duplicates
  collapse.
- **Ingredients on the right steps** — attaches each ingredient to the instruction step that uses it,
  instead of one undifferentiated block.
- **Clean descriptions** — strips HTML, entities, and blog boilerplate ("Jump to Recipe", affiliate
  notices).
- **Better tags** — keeps the meaningful tags (cuisine, course), drops domain/author noise, and adds
  diet/style tags (`vegan`, `gluten-free`, `quick`, `one-pot`…) detected from the recipe.

Each enricher is individually toggleable in **Settings**.

## How it works

```
RSS/Atom feed ─▶ dedupe ─▶ blacklist pre-filter (title+tags)
              ─▶ fetch page HTML ─▶ Tandoor recipe-from-source (parser)
              ─▶ full filter (title+description+tags+ingredients)
              ─▶ enrich ─▶ POST /api/recipe/ ─▶ attach image ─▶ log
```

Filtering and the created recipe both derive from Tandoor's own parser output, so they're always
consistent. We fetch pages ourselves and hand Tandoor the HTML (`data`), which sidesteps its
server-side anti-bot blocks and URL-import rate limit.

## Quick start (binary)

```sh
# Build (Go 1.22+; pure Go, no CGO)
go build -o recipearr .

# Run — keep the data dir OUTSIDE any synced folder; it holds the DB with your API token.
RECIPEARR_DATA_DIR=/var/lib/recipearr \
TANDOOR_URL=http://your-tandoor:9928 \
TANDOOR_TOKEN=tda_xxx \
./recipearr
```

Open <http://localhost:8585>. (The `TANDOOR_*` vars only seed first-run config; you can also set them
in **Settings**. The token is stored in `recipearr.db`, never sent back to the browser.)

Get a Tandoor API token at: Tandoor → your account → **API Token**.

## Docker

```sh
docker compose up -d        # uses ./data for the DB
# or
docker build -t recipearr .
docker run -d -p 8585:8585 -v recipearr-data:/data \
  -e TANDOOR_URL=http://your-tandoor:9928 -e TANDOOR_TOKEN=tda_xxx recipearr
```

Cross-compile for a NAS:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o recipearr-arm64 .
```

## Configuration

Environment (process-level):

| Var                   | Default          | Purpose                                  |
|-----------------------|------------------|------------------------------------------|
| `RECIPEARR_DATA_DIR`  | `data`           | Directory for the SQLite DB              |
| `RECIPEARR_ADDR`      | `0.0.0.0:8585`   | Listen address                           |
| `TANDOOR_URL`         | —                | First-run seed for the Tandoor URL       |
| `TANDOOR_TOKEN`       | —                | First-run seed for the API token         |

Everything else (Tandoor connection, enrichment toggles, filter rules, sources) is configured in the
web UI and stored in the database.

## Adding a source

Each source is an RSS/Atom feed URL, a poll interval, and an "import recent recipes on add" toggle:

- **On** → imports the recent feed window (up to *backlog limit*), then watches for new posts.
- **Off** → seeds the current window as "already seen" and only imports posts published *after* you
  add it.

An RSS feed only exposes the most recent ~10–25 posts, so this can't reach a blog's full archive. To
grab specific older recipes, use **Activity → Import a URL** (single or bulk paste).

## Filtering

Rules are global or per-source, and match against the recipe **title, description, tags, or
ingredients**:

- **Blacklist wins** — any matching blacklist rule rejects the recipe.
- **Whitelist** — if any whitelist rule exists, the recipe must match at least one.
- Match types: contains, whole-word, regex.

> Filters match **author-applied labels and text**, not a nutritional analysis. A recipe that happens
> to be vegan but isn't labelled won't match `vegan`, and absence claims like `gluten-free` only match
> when the source actually says so.

## Development

```sh
go test ./...                                   # unit tests (filter, enrich)
TANDOOR_URL=… TANDOOR_TOKEN=… go test ./internal/pipeline/   # live integration tests
RECIPEARR_LIVE=1 go test ./internal/feed/        # live feed parsing
```

The live tests create and then delete a recipe in your Tandoor, leaving it clean.

## Limitations

- A few aggressively bot-protected publishers (e.g. Serious Eats) block both Tandoor's fetch and ours;
  those import as `failed` with the reason. No headless-browser evasion in v1.
- Sitemap/full-archive crawling and OPML import-from-file are not in v1.
