from pathlib import Path
gee_skill = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto/skills/system/gee-scripting")
ref = (gee_skill / "references" / "image-composite.md").read_text(encoding="utf-8")
print("总长度:", len(ref))
print("=== 16000 之后 ===")
print(ref[16000:])
