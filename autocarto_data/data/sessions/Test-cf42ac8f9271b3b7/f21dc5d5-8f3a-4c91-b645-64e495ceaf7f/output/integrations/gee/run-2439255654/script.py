from pathlib import Path
import json

p = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto/skills/system/china-admin-boundaries/data/china_province.geojson")
data = json.loads(p.read_text(encoding="utf-8"))
feats = data["features"]
print("TOTAL_FEATURES", len(feats), flush=True)
for f in feats:
    props = f["properties"]
    print(props.get("adcode"), "|", props.get("name"), "|", props.get("level"), "|", f["geometry"]["type"], flush=True)
