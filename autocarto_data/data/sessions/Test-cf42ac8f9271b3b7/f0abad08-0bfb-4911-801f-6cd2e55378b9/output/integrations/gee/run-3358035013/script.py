from pathlib import Path
import json

boundary_path = "/home/shared-user/研究生/QGIS-agent/enio2/autocarto_data/data/boundaries/boundary_county_411525.geojson"
roi = load_region(boundary_path).geometry()

def mask_s2_clouds_scl(image):
    scl = image.select("SCL")
    mask = scl.neq(3).And(scl.neq(8)).And(scl.neq(9)).And(scl.neq(10))
    return image.updateMask(mask)

START = "2024-06-01"
END = "2025-06-01"
BANDS = ["B4", "B3", "B2"]

with heartbeat("筛选与云掩膜"):
    clean_col = (ee.ImageCollection("COPERNICUS/S2_SR_HARMONIZED")
        .filterDate(START, END)
        .filterBounds(roi)
        .filter(ee.Filter.lt("CLOUDY_PIXEL_PERCENTAGE", 30))
        .map(mask_s2_clouds_scl)
        .select(BANDS))
    img_count = clean_col.size().getInfo()
print(f"筛选后影像数: {img_count}", flush=True)

if img_count == 0:
    raise RuntimeError("检索窗口内无可用影像")

with heartbeat("计算中值合成"):
    composite = clean_col.median().clip(roi)

coverage = check_coverage(composite, roi, bands=BANDS, scale=30)
print(f"有效覆盖(补洞前): {coverage:.4f}", flush=True)

report = {
    "dataset": "COPERNICUS/S2_SR_HARMONIZED",
    "bands": BANDS,
    "method": "median composite",
    "search_start": START,
    "search_end_exclusive": END,
    "cloud_filter_lt": 30,
    "image_count": img_count,
    "coverage_before_fill": coverage,
    "coverage_check_scale": 30,
}

if coverage < 0.99:
    print("覆盖不足，使用更长时间窗口补洞...", flush=True)
    with heartbeat("计算备份合成并补洞"):
        backup = (ee.ImageCollection("COPERNICUS/S2_SR_HARMONIZED")
            .filterDate("2023-06-01", END)
            .filterBounds(roi)
            .map(mask_s2_clouds_scl)
            .select(BANDS)
            .median().clip(roi))
        composite = composite.unmask(backup, sameFootprint=False)
    report["backup_window"] = "2023-06-01 to " + END

with heartbeat("下载 GeoTIFF"):
    path = download_image(composite, "gushi_county_imagery_rgb.tif",
                          region=roi, scale=10, crs="EPSG:32650")

report["output"] = str(path)
print(json.dumps(report, ensure_ascii=False), flush=True)
Path(output_dir, "gushi_imagery_report.json").write_text(
    json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
print("DONE", flush=True)
