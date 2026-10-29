import json
import math
import runpy
from pathlib import Path

import rasterio

skill_base = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto/skills/system/gee-scripting")

# 复用单日期筛选模板
template = runpy.run_path(
    str(skill_base / "templates" / "single_date_image.py"),
    init_globals={"ee": ee, "heartbeat": heartbeat, "check_coverage": check_coverage},
)

boundary_path = "/home/shared-user/研究生/QGIS-agent/数据/底图数据/gee_wuhan/wuhan_bounds.geojson"
roi = load_region(boundary_path).geometry()

dataset = "COPERNICUS/S2_SR_HARMONIZED"
bands = ["B4", "B3", "B2"]
start_date = "2025-10-01"
end_date = "2026-10-06"  # exclusive
cloud_limit = 70
coverage_scale = 30
export_crs = "EPSG:32650"
export_scale = 10

collection = (
    ee.ImageCollection(dataset)
    .filterBounds(roi)
    .filterDate(start_date, end_date)
    .filter(ee.Filter.lt("CLOUDY_PIXEL_PERCENTAGE", cloud_limit))
)

with heartbeat("单日期影像筛选与覆盖检查"):
    image, report = template["select_single_date"](
        collection, roi, bands=bands, coverage_scale=coverage_scale,
    )

report.update({
    "dataset": dataset,
    "search_start": start_date,
    "search_end_exclusive": end_date,
    "cloud_filter_lt": cloud_limit,
    "export_scale_m": export_scale,
    "export_crs": export_crs,
    "roi": "武汉市行政区外接矩形（用户指定 WGS84 范围）",
    "band_meaning": {"B4": "Red", "B3": "Green", "B2": "Blue"},
})

report_path = Path(output_dir, "selection_report.json")
report_path.write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
print(json.dumps(report, ensure_ascii=False, indent=2), flush=True)

if report["coverage"] <= 0:
    raise RuntimeError("最佳候选在研究区内无有效像元，停止下载")

filename = f"wuhan_s2_{report['date']}_rgb10m.tif"
with heartbeat("下载影像到本地"):
    path = download_image(image, filename, region=roi, scale=export_scale, crs=export_crs)

final_path = Path(path)
print("downloaded_path:", str(final_path), flush=True)
print("file_exists:", final_path.exists(), flush=True)
print("file_size_bytes:", final_path.stat().st_size if final_path.exists() else -1, flush=True)

with rasterio.open(final_path) as ds:
    print("verify_crs:", str(ds.crs), flush=True)
    print("verify_band_count:", ds.count, flush=True)
    print("verify_dtypes:", [str(d) for d in ds.dtypes], flush=True)
    print("verify_width_height:", ds.width, ds.height, flush=True)
    print("verify_res:", ds.res, flush=True)
    print("verify_bounds:", ds.bounds, flush=True)
    band = ds.read(1)
    nodata = ds.nodata
    valid_frac = float((band != nodata).mean()) if nodata is not None else float((band != 0).mean())
    print("verify_valid_pixel_fraction_band1:", round(valid_frac, 6), flush=True)
