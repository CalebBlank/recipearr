"""Capture recipe-from-source for a raw URL (recipe need not exist in Tandoor). Saves a fixture in
the same shape as testdata_rfs_*.json and prints step-0 structure so we can see how each source
prepends its summary. Usage: py -3 tools_rfs_url.py <slug> <url>"""
import sys, json, sqlite3, os, urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
S = {k: v for k, v in sqlite3.connect(os.path.join(HERE, "data", "recipearr.db")).execute("SELECT key,value FROM settings")}
base = S.get("tandoor_url", "").rstrip("/"); token = S.get("tandoor_token", "")

slug, url = sys.argv[1], sys.argv[2]
hreq = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"})
html = urllib.request.urlopen(hreq, timeout=40).read().decode("utf-8", "replace")
r = urllib.request.Request(base + "/api/recipe-from-source/", data=json.dumps({"url": url, "data": html}).encode(), method="POST")
r.add_header("Authorization", "Bearer " + token); r.add_header("Content-Type", "application/json")
rfs = json.loads(urllib.request.urlopen(r, timeout=60).read())
recipe = rfs.get("recipe") or {}
desc = recipe.get("description") or ""
steps = recipe.get("steps", [])
out = {"name": recipe.get("name"), "source": url, "description": desc, "steps": []}
print("NAME:", recipe.get("name"))
print("DESCRIPTION (top-level):", repr(desc)[:200])
print("N STEPS:", len(steps))
for i, st in enumerate(steps):
    instr = st.get("instruction", "")
    sout = {"instruction": instr, "ingredients": []}
    for ing in st.get("ingredients", []):
        sout["ingredients"].append({
            "amount": ing.get("amount"),
            "unit": (ing.get("unit") or {}).get("name", "") if ing.get("unit") else "",
            "food": (ing.get("food") or {}).get("name", "") if ing.get("food") else "",
            "original_text": ing.get("original_text") or "",
            "note": ing.get("note") or "",
        })
    out["steps"].append(sout)
    if i == 0:
        print("\n--- STEP 0 instruction (full) ---")
        print(repr(instr)[:900].encode("ascii", "replace").decode())
fn = os.path.join(HERE, "testdata_rfs_%s.json" % slug)
json.dump(out, open(fn, "w", encoding="utf-8"), indent=2)
print("\nsaved:", os.path.basename(fn))
