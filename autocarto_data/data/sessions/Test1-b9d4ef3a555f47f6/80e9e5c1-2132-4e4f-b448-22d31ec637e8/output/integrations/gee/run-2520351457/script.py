
import json
from pathlib import Path

base = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto_data/data/sessions/Test1-b9d4ef3a555f47f6/80e9e5c1-2132-4e4f-b448-22d31ec637e8/output/statistical/statistics-2025-jz5lv99h")

# manifest
man = json.loads((base / "manifest.json").read_text(encoding="utf-8"))
print("=== MANIFEST ===", flush=True)
print(json.dumps(man, ensure_ascii=False, indent=2), flush=True)

# polygons data_status distribution
poly = json.loads((base / "statistical_polygons.geojson").read_text(encoding="utf-8"))
feats = poly["features"]
from collections import Counter
status = Counter(f["properties"].get("data_status") for f in feats)
print("=== DATA_STATUS DIST ===", flush=True)
print(json.dumps(status, ensure_ascii=False), flush=True)

# null gdp rows
nulls = [f["properties"].get("stat_region") for f in feats if f["properties"].get("gdp") is None]
print("=== NULL_GDP_REGIONS ===", flush=True)
print(json.dumps(nulls, ensure_ascii=False), flush=True)

# count valid numeric
valid = [f["properties"].get("gdp") for f in feats if f["properties"].get("gdp") is not None]
print("VALID_COUNT", len(valid), "MIN", min(valid), "MAX", max(valid), flush=True)
