"""Delete the backed-up componentized recipes from Tandoor, then bulk re-import their source URLs
through Recipearr (which re-fetches + applies the new component-aware allocation). Uses the most
recent data/componentized-backups/*.json (already a full backup) as the source of ids + urls."""
import json, glob, os, sqlite3, urllib.request, urllib.error, time

HERE = os.path.dirname(os.path.abspath(__file__))
S = dict(sqlite3.connect(os.path.join(HERE, "data", "recipearr.db")).execute("select key,value from settings"))
tbase = S["tandoor_url"].rstrip("/")
tok = S["tandoor_token"]
RECIPEARR = "http://192.168.0.31:8585"

backups = sorted(glob.glob(os.path.join(HERE, "data", "componentized-backups", "*.json")))
if not backups:
    raise SystemExit("no componentized backup found")
recipes = json.load(open(backups[-1], encoding="utf-8"))
print("using backup:", backups[-1], "->", len(recipes), "recipes")

def tdel(rid):
    r = urllib.request.Request(tbase + "/api/recipe/%d/" % rid, method="DELETE")
    r.add_header("Authorization", "Bearer " + tok)
    try:
        with urllib.request.urlopen(r, timeout=30) as resp:
            return resp.status
    except urllib.error.HTTPError as e:
        return e.code

# 1) delete
deleted, failed = 0, []
for rec in recipes:
    st = tdel(rec["id"])
    if st in (200, 204):
        deleted += 1
    else:
        failed.append((rec["id"], st))
    time.sleep(0.08)
print("deleted %d / %d" % (deleted, len(recipes)))
if failed:
    print("delete failures:", failed)

# 2) bulk re-import the source URLs through Recipearr
urls = [r.get("source_url") for r in recipes if r.get("source_url")]
body = json.dumps({"urls": urls}).encode()
r = urllib.request.Request(RECIPEARR + "/api/import", data=body, method="POST")
r.add_header("Content-Type", "application/json")
try:
    with urllib.request.urlopen(r, timeout=30) as resp:
        print("re-import triggered:", resp.read().decode())
except Exception as e:
    print("re-import trigger failed:", e)
print("Recipearr is now importing %d URLs in the background — watch the Activity tab." % len(urls))
