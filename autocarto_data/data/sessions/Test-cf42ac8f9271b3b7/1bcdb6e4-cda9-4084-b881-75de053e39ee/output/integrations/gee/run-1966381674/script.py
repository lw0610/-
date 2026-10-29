from pathlib import Path
import json, runpy

gee_skill = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto/skills/system/gee-scripting")
cn_skill = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto/skills/system/china-admin-boundaries")

# 读取影像参考流程
ref = (gee_skill / "references" / "image-composite.md").read_text(encoding="utf-8")
print("=== image-composite.md (前 8000 字) ===")
print(ref[:8000])

print("\n\n=== select_boundary.py 模板 ===")
print((cn_skill / "templates" / "select_boundary.py").read_text(encoding="utf-8"))

# 定位天津市候选
selector = runpy.run_path(str(cn_skill / "templates" / "select_boundary.py"))
cands = selector["find_candidates"](name="天津", level="province")
print("\n=== 天津市 province 候选 ===")
print(json.dumps(cands, ensure_ascii=False))
