from pathlib import Path
import json, runpy, ee
from datetime import datetime, timezone

cn_skill = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto/skills/system/china-admin-boundaries")

# 1) 导出天津市边界（当前会话 output_dir）
selector = runpy.run_path(str(cn_skill / "templates" / "select_boundary.py"))
b = selector["export_boundary"](str(output_dir), adcode="120000", level="province", expected_name="天津市")
print("BOUNDARY_SELECTED:", json.dumps(b["selected"], ensure_ascii=False))
print("BOUNDARY_GEOM:", json.dumps(b["geometry"], ensure_ascii=False))
roi = load_region(b["boundary_path"]).geometry()

# 2) SCL 云掩膜（去云影3、中云8、高云9、薄卷云10）
def mask_s2_clouds_scl(image):
    scl = image.select("SCL")
    m = scl.neq(3).And(scl.neq(8)).And(scl.neq(9)).And(scl.neq(10))
    return image.updateMask(m)

BANDS = ["B2", "B3", "B4", "B8"]
START, END = "2024-06-01", "2024-08-31"

# 3) 筛选候选
with heartbeat("筛选 Sentinel-2 候选影像"):
    col = (ee.ImageCollection("COPERNICUS/S2_SR_HARMONIZED")
        .filterDate(START, END)
        .filterBounds(roi)
        .filter(ee.Filter.lt("CLOUDY_PIXEL_PERCENTAGE", 20))
        .map(mask_s2_clouds_scl))
    n = col.size().getInfo()
print("IMAGE_COUNT(cloud<20%):", n)
if n == 0:
    raise RuntimeError("夏季 2024 无满足云量条件的 S2 影像，需调整时间窗")

# 收集候选影像 ID 与日期（有界元数据查询）
ids = col.aggregate_array("system:index").getInfo()
times = col.aggregate_array("system:time_start").getInfo()
dates = sorted(datetime.fromtimestamp(t/1000, tz=timezone.utc).strftime("%Y-%m-%d") for t in times)
print("DATES_RANGE:", dates[0], "->", dates[-1], "count:", len(dates))

# 4) 中值合成
with heartbeat("逐像元中值合成"):
    composite = col.select(BANDS).median().clip(roi)

# 5) 覆盖检查（SCL 掩膜共享于各波段，用 B4 代表；scale=30 记录）
with heartbeat("覆盖检查"):
    cov = check_coverage(composite, roi, band="B4", scale=30)
print("COVERAGE_RAW:", cov)

# 6) 覆盖不足时用更宽时间窗补洞（多日期合成，记录补充时段）
filled_flag = False
if cov < 0.99:
    filled_flag = True
    with heartbeat("扩展时间窗补洞"):
        backup = (ee.ImageCollection("COPERNICUS/S2_SR_HARMONIZED")
            .filterDate("2023-06-01", "2024-09-30")
            .filterBounds(roi)
            .filter(ee.Filter.lt("CLOUDY_PIXEL_PERCENTAGE", 30))
            .map(mask_s2_clouds_scl)
            .select(BANDS).median().clip(roi))
        composite = composite.unmask(backup, sameFootprint=False)
    with heartbeat("补洞后覆盖检查"):
        cov_final = check_coverage(composite, roi, band="B4", scale=30)
    print("COVERAGE_FINAL:", cov_final)
else:
    cov_final = cov

# 7) 下载（geedim 分块，UTM 50N，10m，int16）
with heartbeat("下载影像"):
    out = download_image(composite, "tianjin_s2_summer2024_median_10m.tif",
                         region=roi, scale=10, crs="EPSG:32650", dtype="int16")
print("DOWNLOAD_RESULT:", out)

# 8) 元数据落盘
meta = {
    "dataset": "COPERNICUS/S2_SR_HARMONIZED",
    "dataset_desc": "Sentinel-2 Level-2A Surface Reflectance (Harmonized)",
    "region": "天津市 (Tianjin)", "adcode": "120000", "level": "province",
    "time_range": [START, END],
    "gapfill_range": ["2023-06-01", "2024-09-30"] if filled_flag else None,
    "composition": "per-pixel median (SCL cloud mask: 3,8,9,10 removed)",
    "bands": BANDS, "resolution_m": 10, "crs": "EPSG:32650", "dtype": "int16",
    "scale_note": "DN 0-10000; reflectance = DN / 10000",
    "image_count": n, "dates_first_last": [dates[0], dates[-1]],
    "coverage_check_scale_m": 30, "coverage_raw": cov, "coverage_final": cov_final,
    "bounds_wgs84": b["geometry"]["bounds"],
    "boundary_path": b["boundary_path"], "provenance_path": b["provenance_path"],
    "download_result": out,
}
meta_path = output_dir / "metadata.json"
meta_path.write_text(json.dumps(meta, ensure_ascii=False, indent=2), encoding="utf-8")
print("METADATA_PATH:", meta_path)
print("ALL_DONE")
