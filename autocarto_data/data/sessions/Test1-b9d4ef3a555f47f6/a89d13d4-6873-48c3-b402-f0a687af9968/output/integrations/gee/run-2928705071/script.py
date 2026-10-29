from pathlib import Path
import json
import runpy

china_base = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto/skills/system/china-admin-boundaries")

selector = runpy.run_path(str(china_base / "templates/select_boundary.py"))
candidates = selector["find_candidates"](name="武汉市", level="city")
print(json.dumps(candidates, ensure_ascii=False), flush=True)
