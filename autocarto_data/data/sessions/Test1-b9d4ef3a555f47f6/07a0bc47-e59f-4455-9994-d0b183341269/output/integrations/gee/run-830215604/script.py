import json
from pathlib import Path

report_path = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto_data/data/sessions/Test1-b9d4ef3a555f47f6/07a0bc47-e59f-4455-9994-d0b183341269/output/integrations/gee/run-307871134/selection_report.json")
report = json.loads(report_path.read_text(encoding="utf-8"))

print("best_date:", report.get("date"))
print("coverage:", report.get("coverage"))
print("meets_target:", report.get("meets_target"))
print("scene_count:", report.get("scene_count"))
print("bands:", report.get("bands"))
print("checked_candidates:")
for c in report.get("checked_candidates", []):
    print("  ", c["date"], "cloud=%.1f%%" % c["mean_cloud"], "coverage=%.2f%%" % (c["coverage"]*100))
print("image_ids:")
for i in report.get("image_ids", []):
    print("  ", i)
