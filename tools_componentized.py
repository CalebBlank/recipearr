"""Find componentized Tandoor recipes (ingredient-section headers), back them up in full, list them,
and optionally delete them so they can be re-imported with the correct allocation.

  py -3 tools_componentized.py scan     # list + FULL backup, no deletion
  py -3 tools_componentized.py delete   # re-scan + FULL backup, then DELETE them
"""
import sys, json, sqlite3, os, urllib.request, urllib.error, time

HERE = os.path.dirname(os.path.abspath(__file__))
S = dict(sqlite3.connect(os.path.join(HERE, "data", "recipearr.db")).execute("select key,value from settings"))
base = S["tandoor_url"].rstrip("/")
tok = S["tandoor_token"]
mode = sys.argv[1] if len(sys.argv) > 1 else "scan"

def req(method, path):
    r = urllib.request.Request(base + path, method=method)
    r.add_header("Authorization", "Bearer " + tok)
    try:
        with urllib.request.urlopen(r, timeout=40) as resp:
            raw = resp.read()
            return resp.status, (json.loads(raw) if raw else None)
    except urllib.error.HTTPError as e:
        return e.code, None

HEADER_PREFIXES = ("for the ", "for serving", "to garnish", "to serve", "to assemble", "to finish", "to top")

def is_header(ing):
    if ing.get("is_header"):
        return True
    amt = ing.get("amount")
    if amt not in (0, 0.0, None, "", "0"):
        return False
    if (ing.get("unit") or {}) and (ing.get("unit") or {}).get("name", "").strip():
        return False
    ot = (ing.get("original_text") or "").strip()
    probe = (ot or ((ing.get("food") or {}).get("name", ""))).strip().lower()
    if ot.endswith(":"):
        return True
    if any(probe.startswith(p) for p in HEADER_PREFIXES):
        return True
    return probe in ("garnish", "topping", "toppings")

def componentized(rec):
    for st in rec.get("steps", []):
        for ing in st.get("ingredients", []):
            if is_header(ing):
                return True
    return False

# collect all recipe ids
ids, page = [], 1
while True:
    st, data = req("GET", "/api/recipe/?page_size=100&page=%d" % page)
    if not data:
        break
    ids += [r["id"] for r in data.get("results", [])]
    if not data.get("next"):
        break
    page += 1
print("scanning %d recipes..." % len(ids))

comp = []
for rid in ids:
    st, rec = req("GET", "/api/recipe/%d/" % rid)
    if rec and componentized(rec):
        comp.append(rec)

print("\n=== %d componentized recipe(s) ===" % len(comp))
for rec in comp:
    line = "  [%d] %-45s  %s" % (rec["id"], (rec.get("name") or "")[:45], rec.get("source_url") or "(no source url)")
    print(line.encode("ascii", "replace").decode())

# full backup
bdir = os.path.join("data", "componentized-backups")
os.makedirs(bdir, exist_ok=True)
bfile = os.path.join(bdir, "componentized-%d.json" % int(time.time()))
json.dump(comp, open(bfile, "w", encoding="utf-8"), indent=1)
print("\nFULL backup of all %d -> %s" % (len(comp), bfile))

urls = [r.get("source_url") for r in comp if r.get("source_url")]
print("\n=== %d source URL(s) to re-import ===" % len(urls))
for u in urls:
    print("  " + u)

if mode == "delete":
    print("\n=== DELETING %d recipe(s) ===" % len(comp))
    deleted = 0
    for rec in comp:
        st, _ = req("DELETE", "/api/recipe/%d/" % rec["id"])
        if st in (200, 204):
            deleted += 1
        else:
            print("  delete %d -> HTTP %s" % (rec["id"], st))
        time.sleep(0.1)
    print("deleted %d of %d (backup kept at %s)" % (deleted, len(comp), bfile))
else:
    print("\n(scan only — re-run with 'delete' to remove them)")
