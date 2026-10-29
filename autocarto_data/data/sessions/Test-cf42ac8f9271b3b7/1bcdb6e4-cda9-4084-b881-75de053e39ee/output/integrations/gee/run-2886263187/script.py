from pathlib import Path
import json, runpy

cn_skill = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto/skills/system/china-admin-boundaries")
selector = runpy.run_path(str(cn_skill / "templates" / "select_boundary.py"))

for name in ["天津市", "天津"]:
    for level in ["province", None]:
        try:
            c = selector["find_candidates"](name=name, level=level)
            print(f"name={name!r} level={level!r}: {json.dumps(c, ensure_ascii=False)}")
        except Exception as e:
            print(f"name={name!r} level={level!r} -> ERROR {e}")

# contains 查看所有层级含"天津"的候选
c = selector["find_candidates"](name="天津", contains=True)
print("contains 天津 (all levels):", json.dumps(c, ensure_ascii=False))
