#!/usr/bin/env python3
"""Migrate Recipearr config (Tandoor settings + sources) from the local SQLite DB to a fresh
instance — e.g. the new Home Assistant add-on on the Yellow.

  py -3 migrate-data.py http://192.168.0.31:8585 [path/to/recipearr.db]

- Pushes Tandoor URL/token + enrichment settings (PUT /api/settings).
- Re-creates each source (POST /api/sources) with backlog_on_add=FALSE, so the new instance just
  SEEDS (marks the current feed as seen) and watches forward — it will NOT re-import recipes you
  already have in Tandoor.
Run it AFTER the add-on is installed and running. Idempotent-ish: re-running re-adds sources
(creating duplicates), so only run once.
"""
import sys, os, json, sqlite3, urllib.request, urllib.error

if len(sys.argv) < 2:
    print(__doc__); sys.exit(1)
TARGET = sys.argv[1].rstrip("/")
DB = sys.argv[2] if len(sys.argv) > 2 else os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "data", "recipearr.db")


def req(method, path, payload):
    data = json.dumps(payload).encode("utf-8")
    r = urllib.request.Request(TARGET + path, data=data, method=method)
    r.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(r, timeout=30) as resp:
            return resp.status, resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")
    except Exception as e:
        return 0, str(e)


con = sqlite3.connect(DB)
con.row_factory = sqlite3.Row
S = {row["key"]: row["value"] for row in con.execute("SELECT key, value FROM settings")}


def b(key, default=True):
    v = S.get(key)
    if v is None:
        return default
    return v == "1" or v.lower() == "true"


def jload(key, default):
    raw = S.get(key)
    if not raw:
        return default
    try:
        return json.loads(raw)
    except Exception:
        return default


# 1) settings (Tandoor + enrichment) — must go first so source-add can reach Tandoor.
settings = {
    "tandoor_url": S.get("tandoor_url", ""),
    "tandoor_token": S.get("tandoor_token", ""),
    "enrich_communize": b("enrich_communize"),
    "enrich_clean_description": b("enrich_clean_description"),
    "enrich_curate_tags": b("enrich_curate_tags"),
    "enrich_allocate_steps": b("enrich_allocate_steps"),
    "enrich_description_mode": S.get("enrich_description_mode", "clean"),
    "enrich_tag_denylist": jload("enrich_tag_denylist", []),
    "enrich_aliases": jload("enrich_aliases", {}),
    "organize_into_books": b("organize_into_books", False),
}
st, body = req("PUT", "/api/settings", settings)
print("settings -> %s  (tandoor_url=%s, token=%s)" %
      (st, settings["tandoor_url"], "set" if settings["tandoor_token"] else "MISSING"))

# 2) sources — backlog_on_add False so we seed+watch, not re-import.
rows = con.execute("SELECT name,feed_url,site_url,enabled,poll_interval_minutes,backlog_limit,default_keywords FROM sources ORDER BY id")
ok = 0
for r in rows:
    payload = {
        "name": r["name"], "feed_url": r["feed_url"], "site_url": r["site_url"] or "",
        "enabled": bool(r["enabled"]), "poll_interval_minutes": r["poll_interval_minutes"] or 60,
        "backlog_on_add": False, "backlog_limit": r["backlog_limit"] or 25,
        "default_keywords": r["default_keywords"] or "",
    }
    st, body = req("POST", "/api/sources", payload)
    print("  source %-32s -> %s" % (r["name"][:32], st))
    if st in (200, 201):
        ok += 1
con.close()
print("\nMigrated %d source(s). Open %s and confirm." % (ok, TARGET))
