"""Robust serial re-import: POST each pending URL to Recipearr's SYNCHRONOUS single-import endpoint,
one at a time, waiting for each result. Survives add-on restarts (we drive the loop; no fire-and-
forget goroutine). Retries once on network error. Usage: py -3 tools_reimport_serial.py [limit]"""
import json, os, sys, time, urllib.request, urllib.error

HERE = os.path.dirname(os.path.abspath(__file__))
RA = "http://192.168.0.31:8585"
urls = json.load(open(os.path.join(HERE, "data", "reimport-pending.json")))
limit = int(sys.argv[1]) if len(sys.argv) > 1 else len(urls)
urls = urls[:limit]
print("re-importing %d URL(s) serially via %s/api/import" % (len(urls), RA), flush=True)

def imp(u):
    body = json.dumps({"url": u}).encode()
    r = urllib.request.Request(RA + "/api/import", data=body, method="POST")
    r.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(r, timeout=170) as resp:
            return resp.status, json.loads(resp.read())
    except urllib.error.HTTPError as e:
        try:
            return e.code, json.loads(e.read())
        except Exception:
            return e.code, None

buckets = {}
for i, u in enumerate(urls, 1):
    data, err = None, None
    for attempt in range(2):
        try:
            code, data = imp(u)
            break
        except Exception as e:
            err = str(e)
            time.sleep(3)
    if data is None:
        buckets.setdefault("error", []).append((u, err))
        print("[%d/%d] ERROR %s -> %s" % (i, len(urls), u, err), flush=True)
        continue
    st = data.get("status") or "?"
    title = data.get("title") or u
    detail = data.get("error") or data.get("filter_reason") or ""
    buckets.setdefault(st, []).append((title, detail, u))
    line = "[%d/%d] %-11s %s %s" % (i, len(urls), st, title[:48], ("| " + detail[:45]) if detail else "")
    print(line.encode("ascii", "replace").decode(), flush=True)

print("\n=== SUMMARY ===", flush=True)
for k, v in buckets.items():
    print("  %-11s %d" % (k, len(v)), flush=True)
json.dump({k: v for k, v in buckets.items()}, open(os.path.join(HERE, "data", "reimport-results.json"), "w"), indent=2)
print("wrote data/reimport-results.json", flush=True)
