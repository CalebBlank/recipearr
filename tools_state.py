"""Quick health/state check after the concurrent reprocess-apply + bulk re-import."""
import json, sqlite3, os, urllib.request, urllib.error
HERE = os.path.dirname(os.path.abspath(__file__))
S = dict(sqlite3.connect(os.path.join(HERE, "data", "recipearr.db")).execute("select key,value from settings"))
tbase = S["tandoor_url"].rstrip("/"); tok = S["tandoor_token"]
RA = "http://192.168.0.31:8585"

def jget(url, auth=None, timeout=20):
    r = urllib.request.Request(url)
    if auth: r.add_header("Authorization", auth)
    try:
        with urllib.request.urlopen(r, timeout=timeout) as resp:
            return resp.status, json.loads(resp.read())
    except urllib.error.HTTPError as e:
        return e.code, None
    except Exception as e:
        return 0, str(e)

st, h = jget(RA + "/api/health")
print("Recipearr health:", st, h)
st, items = jget(RA + "/api/items?limit=30")
if isinstance(items, dict):
    counts = items.get("counts", {})
    print("Recipearr item counts:", counts)
    print("recent items:")
    for it in (items.get("items") or [])[:20]:
        line = "  [%s] %-44s %s" % (it.get("status"), (it.get("title") or it.get("url") or "")[:44], (it.get("error") or it.get("filter_reason") or ""))
        print(line.encode("ascii", "replace").decode())
else:
    print("Recipearr items: error", st, items)
st, rc = jget(tbase + "/api/recipe/?page_size=1", auth="Bearer " + tok)
if isinstance(rc, dict):
    print("\nTandoor recipe count:", rc.get("count"))
else:
    print("\nTandoor count: error", st)
