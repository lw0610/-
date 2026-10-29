from pathlib import Path
import json, runpy, sys

skill_base_dir = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto/skills/system/china-admin-boundaries")
print("data dir exists:", (skill_base_dir / "data").exists())
for f in ["china_province.geojson", "china_city.geojson", "china_county.geojson"]:
    p = skill_base_dir / "data" / f
    print(f, "exists:", p.exists(), "size:", p.stat().st_size if p.exists() else None)

print("output_dir:", Path.cwd())
print("cwd:", Path.cwd())

# 读取模板并查看候选
selector = runpy.run_path(str(skill_base_dir / "templates" / "select_boundary.py"))
cands = selector["find_candidates"](name="河南省", level="province")
print(json.dumps(cands, ensure_ascii=False), flush=True)
