"""Fetch a recipe from Tandoor and print its steps + per-step ingredient allocation, so we can see
why ingredient->step allocation went wrong. Token read from the local recipearr DB (not printed)."""
import sys, json, sqlite3, os, urllib.request, urllib.parse

DB = os.path.join(os.path.dirname(os.path.abspath(__file__)), "data", "recipearr.db")
con = sqlite3.connect(DB)
S = {k: v for k, v in con.execute("SELECT key,value FROM settings")}
base = S.get("tandoor_url", "").rstrip("/")
token = S.get("tandoor_token", "")
con.close()
if not base or not token:
    sys.exit("no tandoor url/token in db")

def get(path):
    r = urllib.request.Request(base + path)
    r.add_header("Authorization", "Bearer " + token)
    with urllib.request.urlopen(r, timeout=30) as resp:
        return json.loads(resp.read())

q = sys.argv[1] if len(sys.argv) > 1 else "Chocolate Peanut Butter Oatmeal Bars"
res = get("/api/recipe/?query=" + urllib.parse.quote(q) + "&page_size=5")
items = res.get("results", res if isinstance(res, list) else [])
if not items:
    sys.exit("no recipe found for: " + q)
print("matches:", [(it["id"], it["name"]) for it in items])
rid = items[0]["id"]
rec = get("/api/recipe/%d/" % rid)
print("\n=== %s (id=%d) ===" % (rec["name"], rid))
for i, st in enumerate(rec.get("steps", []), 1):
    instr = (st.get("instruction") or "").replace("\n", " ")
    print("\n--- STEP %d ---\n%s" % (i, instr[:400]))
    for ing in st.get("ingredients", []):
        food = (ing.get("food") or {}).get("name", "?")
        note = ing.get("note") or ""
        amt = ing.get("amount") or ""
        unit = (ing.get("unit") or {}).get("name", "") if ing.get("unit") else ""
        print("      * %s %s %s%s" % (amt, unit, food, (" ["+note+"]") if note else ""))
