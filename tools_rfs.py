"""Capture the RAW recipe-from-source output Tandoor's parser produces for a recipe's source URL --
i.e. the exact import input the allocator receives (headers + original ingredient order). Token from
the local DB (not printed). Saves the JSON next to this file for use as a Go test fixture."""
import sys, json, sqlite3, os, urllib.request, urllib.parse

HERE = os.path.dirname(os.path.abspath(__file__))
DB = os.path.join(HERE, "data", "recipearr.db")
con = sqlite3.connect(DB)
S = {k: v for k, v in con.execute("SELECT key,value FROM settings")}
con.close()
base = S.get("tandoor_url", "").rstrip("/")
token = S.get("tandoor_token", "")

def req(method, path, payload=None):
    data = json.dumps(payload).encode() if payload is not None else None
    r = urllib.request.Request(base + path, data=data, method=method)
    r.add_header("Authorization", "Bearer " + token)
    if data:
        r.add_header("Content-Type", "application/json")
    with urllib.request.urlopen(r, timeout=60) as resp:
        return json.loads(resp.read())

rid = int(sys.argv[1])
rec = req("GET", "/api/recipe/%d/" % rid)
url = rec.get("source_url") or ""
print("recipe:", rec["name"], "| source:", url)
if not url:
    sys.exit("no source_url")

# Fetch the page HTML ourselves (browser UA dodges anti-bot), pass as data — like Recipearr does.
hreq = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"})
html = urllib.request.urlopen(hreq, timeout=40).read().decode("utf-8", "replace")
rfs = req("POST", "/api/recipe-from-source/", {"url": url, "data": html})
recipe = rfs.get("recipe") or {}
steps = recipe.get("steps", [])
out = {"name": recipe.get("name"), "source": url, "steps": []}
print("\n=== %d step(s) ===" % len(steps))
for i, st in enumerate(steps, 1):
    instr = (st.get("instruction") or "").replace("\n", " ")
    print("\n--- STEP %d ---\n%s" % (i, instr[:300]))
    sout = {"instruction": st.get("instruction", ""), "ingredients": []}
    for ing in st.get("ingredients", []):
        food = (ing.get("food") or {}).get("name", "") if ing.get("food") else ""
        unit = (ing.get("unit") or {}).get("name", "") if ing.get("unit") else ""
        amt = ing.get("amount")
        ot = ing.get("original_text") or ""
        note = ing.get("note") or ""
        line = "      amt=%-6s unit=%-10s food=%-32s | orig=%r" % (str(amt), unit, food[:32], ot[:50])
        print(line.encode("ascii", "replace").decode())
        sout["ingredients"].append({"amount": amt, "unit": unit, "food": food, "original_text": ot, "note": note})
    out["steps"].append(sout)

fn = os.path.join(HERE, "testdata_rfs_%d.json" % rid)
os.makedirs(os.path.dirname(fn), exist_ok=True) if os.path.dirname(fn) else None
json.dump(out, open(fn, "w", encoding="utf-8"), indent=2)
print("\nsaved fixture:", fn)
