
import json
from pathlib import Path

p = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto/skills/system/china-admin-boundaries/data/china_province.geojson")
data = json.loads(p.read_text(encoding="utf-8"))
feats = data.get("features", [])
print("TOTAL_FEATURES", len(feats), flush=True)
for f in feats:
    props = f.get("properties", {})
    print(json.dumps(props, ensure_ascii=False), flush=True)
