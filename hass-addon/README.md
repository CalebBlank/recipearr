# Recipearr — Home Assistant local add-on

Runs Recipearr on the HA Yellow, with the SQLite DB in the add-on's **Supervisor-persisted `/data`**
— which fixes the data loss you got from running the binary inside a Nextcloud-synced folder.

This add-on uses a **prebuilt `aarch64` binary** (`recipearr-arm64`), so the build on the Yellow is
trivial (copy into a distroless image — no Go toolchain, no compile on the Pi). The binary is a build
artifact (git-ignored), so build it first.

## Install

1. **Build the arm64 binary** (from the repo root):
   ```sh
   CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o hass-addon/recipearr-arm64 .
   ```
2. **Copy this folder to the Yellow** as `/addons/recipearr/` (it must contain `config.yaml`,
   `Dockerfile`, and `recipearr-arm64`). Easiest options:
   - **Samba add-on**: open `\\<yellow-ip>\addons`, make a `recipearr` folder, copy the 3 files in.
   - **File Editor / VS Code add-on**: create `/addons/recipearr/` and upload the files.
3. In HA: **Settings → Add-ons → Add-on Store**, then top-right **⋮ → Check for updates**, and reload
   the page. Recipearr appears under **Local add-ons**.
4. Open it → **Install** (the Supervisor builds the image — seconds). Then **Start**. Turn on
   *Start on boot* (and *Watchdog* if you like).
5. Open **http://<yellow-ip>:8585**.

## Bring your sources + Tandoor settings over

With the **old** `recipearr.exe` still running on Windows (so its DB is readable), from the repo:

```sh
py -3 hass-addon/migrate-data.py http://<yellow-ip>:8585
```

This copies your Tandoor URL/token + enrichment settings and re-adds all sources with
`backlog_on_add=false` (it seeds + watches forward, so it will **not** re-import recipes you already
have in Tandoor).

6. Confirm the 8 sources + Settings look right in the new UI, then **stop the old `recipearr.exe`**
   (don't leave both polling, or they'll double-import). The Windows DB in Nextcloud can be deleted.

## Updating later

Rebuild the binary and replace it in `/addons/recipearr/`, bump `version:` in `config.yaml`, then
**Rebuild** the add-on in HA:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o hass-addon/recipearr-arm64 .
```
