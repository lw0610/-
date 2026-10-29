from pathlib import Path
import json
from collections import Counter

p = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto_data/data/sessions/Test-cf42ac8f9271b3b7/f21dc5d5-8f3a-4c91-b645-64e495ceaf7f/output/intermediate/statistical/statistics-2020-fcndjcvf/statistical_polygons.geojson")
data = json.loads(p.read_text(encoding="utf-8"))
feats = data["features"]

status_counter = Counter()
pop_values = []
excluded_detail = []
for f in feats:
    pr = f["properties"]
    st = pr.get("data_status")
    status_counter[st] += 1
    pop = pr.get("population")
    if st == "valid":
        pop_values.append(pop)
    else:
        excluded_detail.append((pr.get("name"), pr.get("stat_region"), pop, st))

print("TOTAL", len(feats), flush=True)
print("STATUS_COUNTS", dict(status_counter), flush=True)
print("VALID_POP_COUNT", len(pop_values), flush=True)
print("VALID_POP_MIN", min(pop_values), flush=True)
print("VALID_POP_MAX", max(pop_values), flush=True)
print("VALID_POP_NULLS", sum(1 for v in pop_values if v is None), flush=True)
print("NON_VALID_DETAIL", excluded_detail, flush=True)
# sample
for f in feats[:3]:
    pr = f["properties"]
    print("SAMPLE", pr.get("name"), pr.get("stat_region"), pr.get("population"), pr.get("data_status"), pr.get("stat_year"), flush=True)
