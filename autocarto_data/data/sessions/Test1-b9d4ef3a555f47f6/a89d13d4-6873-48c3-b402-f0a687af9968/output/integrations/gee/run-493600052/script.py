from pathlib import Path
import json
import runpy

china_base = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto/skills/system/china-admin-boundaries")
gee_base = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto/skills/system/gee-scripting")

# 1. 导出武汉市本地边界并加载研究区
selector = runpy.run_path(str(china_base / "templates/select_boundary.py"))
boundary = selector["export_boundary"](
    output_dir,
    adcode="420100",
    level="city",
    expected_name="武汉市",
)
print("BOUNDARY:", json.dumps({k: boundary.get(k) for k in
    ("boundary_path", "provenance_path", "selected", "geometry")}, ensure_ascii=False), flush=True)
roi = load_region(boundary["boundary_path"]).geometry()

# 2. 有界检索窗口：最近一年（end 不含当日）
now = ee.Date('now')
start_date = now.advance(-365, 'day')
end_date = now
start_str = start_date.format('YYYY-MM-dd').getInfo()
end_str = end_date.format('YYYY-MM-dd').getInfo()
print(f"检索窗口: [{start_str}, {end_str})", flush=True)

roi_bounds = roi.bounds().getInfo()
print("ROI_BOUNDS:", json.dumps(roi_bounds, ensure_ascii=False), flush=True)

# 3. 筛选候选集合（Sentinel-2 L2A，整景云量 < 20%）
CLOUD_LIMIT = 20
collection = (ee.ImageCollection("COPERNICUS/S2_SR_HARMONIZED")
              .filterBounds(roi)
              .filterDate(start_date, end_date)
              .filter(ee.Filter.lt("CLOUDY_PIXEL_PERCENTAGE", CLOUD_LIMIT)))

# 4. 单日期选优（同日 SCL 云掩膜拼接 + 覆盖检查）
template = runpy.run_path(
    str(gee_base / "templates" / "single_date_image.py"),
    init_globals={"ee": ee, "heartbeat": heartbeat, "check_coverage": check_coverage},
)
BANDS = ["B4", "B3", "B2", "B8"]
image, report = template["select_single_date"](
    collection, roi, bands=BANDS, coverage_scale=30,
)
report.update({
    "dataset": "COPERNICUS/S2_SR_HARMONIZED",
    "sensor": "Sentinel-2 MSI",
    "search_start": start_str,
    "search_end_exclusive": end_str,
    "cloud_filter_lt": CLOUD_LIMIT,
    "export_crs": "EPSG:32650",
    "export_scale": 10,
    "roi_bounds": roi_bounds,
    "boundary_source": "Tianditu (china-admin-boundaries skill)",
    "boundary_adcode": "420100",
    "boundary_name": "武汉市",
})
Path(output_dir, "selection_report.json").write_text(
    json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
print("SELECTION_REPORT:", json.dumps(report, ensure_ascii=False), flush=True)

if not report["meets_target"]:
    print(f"提示：最佳单日期 {report['date']} 有效覆盖 {report['coverage']:.2%}，"
          f"未达 {report['target_coverage']:.0%} 目标；将下载并保留真实缺口。", flush=True)

# 5. 下载（10m，UTM 50N）
with heartbeat("下载武汉市 Sentinel-2 影像"):
    path = download_image(image, f"s2_wuhan_{report['date']}.tif",
                          region=roi, scale=10, crs="EPSG:32650")
print("DOWNLOAD_PATH:", path, flush=True)

# 6. 核验
verification = {"path": str(path)}
try:
    import rasterio
    with rasterio.open(path) as ds:
        verification.update({
            "width": int(ds.width),
            "height": int(ds.height),
            "band_count": int(ds.count),
            "band_descriptions": list(ds.descriptions),
            "crs": str(ds.crs),
            "resolution": [float(ds.res[0]), float(ds.res[1])],
            "bounds": {
                "left": float(ds.bounds.left),
                "bottom": float(ds.bounds.bottom),
                "right": float(ds.bounds.right),
                "top": float(ds.bounds.top),
            },
            "dtype": str(ds.dtypes[0]),
            "nodata": ds.nodata,
        })
    print("VERIFICATION:", json.dumps(verification, ensure_ascii=False), flush=True)
except Exception as e:
    import os
    verification["rasterio_error"] = str(e)
    verification["file_size_bytes"] = os.path.getsize(path)
    print("VERIFICATION:", json.dumps(verification, ensure_ascii=False), flush=True)

Path(output_dir, "verification.json").write_text(
    json.dumps(verification, ensure_ascii=False, indent=2), encoding="utf-8")
print("DONE", flush=True)
