from pathlib import Path
import json

p = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto_data/data/sessions/Test-cf42ac8f9271b3b7/f21dc5d5-8f3a-4c91-b645-64e495ceaf7f/output/intermediate/statistical/statistics-2020-fcndjcvf/manifest.json")
print(p.read_text(encoding="utf-8"), flush=True)
