"""Precise breakdown: of the 69 backed-up componentized recipes, which are now back in Tandoor
vs still missing (re-import stalled). Matches by source_url first, name second. Writes the
still-missing source_urls to data/reimport-pending.json for the recovery re-import."""
import json, glob, os, sqlite3, urllib.request, urllib.error

HERE = os.path.dirname(os.path.abspath(__file__))
S = dict(sqlite3.connect(os.path.join(HERE, "data", "recipearr.db")).execute("select key,value from settings"))
tbase = S["tandoor_url"].rstrip("/"); tok = S["tandoor_token"]

def norm(s): return "".join((s or "").lower().split())

backup = sorted(glob.glob(os.path.join(HERE, "data", "componentized-backups", "*.json")))[-1]
recipes = json.load(open(backup, encoding="utf-8"))
print("backup:", os.path.basename(backup), "->", len(recipes), "recipes")

# pull all current Tandoor recipes (name + source_url)
def jget(url):
    r = urllib.request.Request(url); r.add_header("Authorization", "Bearer " + tok)
    with urllib.request.urlopen(r, timeout=40) as resp: return json.loads(resp.read())

names, urls = set(), set()
url = tbase + "/api/recipe/?page_size=500"
while url:
    d = jget(url)
    for r in d.get("results", []):
        names.add(norm(r.get("name")))
        if r.get("source_url"): urls.add(r["source_url"].rstrip("/"))
    url = d.get("next")
print("current Tandoor recipes:", len(names))

back, missing = [], []
serious = []
for rec in recipes:
    su = (rec.get("source_url") or "").rstrip("/")
    nm = rec.get("name") or rec.get("title") or ""
    here = (su and su in urls) or (norm(nm) in names)
    if here:
        back.append(nm)
    else:
        missing.append({"name": nm, "source_url": rec.get("source_url")})
        if "seriouseats.com" in su: serious.append(nm)

print("\n=== RESULT ===")
print("re-imported (back in Tandoor): %d / %d" % (len(back), len(recipes)))
print("still missing:                 %d / %d" % (len(missing), len(recipes)))
print("  of which Serious Eats (will keep failing):", len(serious))
print("\nstill-missing recipes:")
for m in missing:
    tag = "  [SeriousEats]" if "seriouseats.com" in (m["source_url"] or "") else ""
    print((" - " + (m["name"] or m["source_url"] or "")[:60] + tag).encode("ascii", "replace").decode())

pend = [m["source_url"] for m in missing if m["source_url"] and "seriouseats.com" not in m["source_url"]]
json.dump(pend, open(os.path.join(HERE, "data", "reimport-pending.json"), "w"), indent=2)
print("\nwrote %d re-importable (non-SeriousEats) URLs -> data/reimport-pending.json" % len(pend))
