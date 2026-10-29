from pathlib import Path
import json, runpy

gee_skill = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto/skills/system/gee-scripting")
cn_skill = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto/skills/system/china-admin-boundaries")

# 读取参考剩余部分
ref = (gee_skill / "references" / "image-composite.md").read_text(encoding="utf-8")
print("=== image-composite.md (8000 字之后) ===")
print(ref[8000:16000])

# 导出天津市边界
selector = runpy.run_path(str(cn_skill / "templates" / "select_boundary.py"))
res = selector["export_boundary"](
    str(Path(".").resolve() / "output"),  # 占位，实际使用 output_dir
    adcode="120000",
    level="province",
    expected_name="天津市",
)
print("\n=== 边界导出(占位目录，忽略) ===")
print(json.dumps(res, ensure_ascii=False))
