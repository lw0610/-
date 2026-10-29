from pathlib import Path

base = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto/skills/system/gee-scripting")

for rel in ["references/image-composite.md", "templates/single-date-image.md", "templates/single_date_image.py"]:
    p = base.joinpath(rel)
    print("=" * 80)
    print("FILE:", rel)
    print("EXISTS:", p.exists())
    print("=" * 80)
    if p.exists():
        print(p.read_text(encoding="utf-8"))
    print()
