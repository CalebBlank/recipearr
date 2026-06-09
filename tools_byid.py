"""Dump a Tandoor recipe's step->ingredient allocation by id (for COPY-test inspection)."""
import sys, json, sqlite3, os, urllib.request
HERE = os.path.dirname(os.path.abspath(__file__))
S = dict(sqlite3.connect(os.path.join(HERE, "data", "recipearr.db")).execute("select key,value from settings"))
base = S["tandoor_url"].rstrip("/"); tok = S["tandoor_token"]

def get(p):
    r = urllib.request.Request(base + p)
    r.add_header("Authorization", "Bearer " + tok)
    return json.loads(urllib.request.urlopen(r, timeout=30).read())

rid = int(sys.argv[1])
rec = get("/api/recipe/%d/" % rid)
print("# %s (id=%d)" % (rec.get("name"), rid))
for i, st in enumerate(rec.get("steps", []), 1):
    parts = []
    for ing in st.get("ingredients", []):
        if ing.get("is_header"):
            parts.append("[HEADER:%s]" % (ing.get("note", "")))
        else:
            f = (ing.get("food") or {}).get("name", "") if ing.get("food") else ""
            note = ing.get("note", "")
            parts.append(f + ((" (" + note + ")") if note else ""))
    line = "step %d: %s" % (i, " | ".join(parts))
    print(line.encode("ascii", "replace").decode())
