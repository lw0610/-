from pathlib import Path
import json

skill_base = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto/skills/system/gee-scripting")
china_base = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto/skills/system/china-admin-boundaries")

# 读取影像工作流参考
for ref in ["references/image-composite.md", "templates/single-date-image.md"]:
    p = skill_base / ref
    print("="*80)
    print("FILE:", ref)
    print("="*80)
    print(p.read_text(encoding="utf-8"))
    print()

# 读取单日期模板脚本
print("="*80)
print("FILE: templates/single_date_image.py")
print("="*80)
print((skill_base / "templates/single_date_image.py").read_text(encoding="utf-8"))
