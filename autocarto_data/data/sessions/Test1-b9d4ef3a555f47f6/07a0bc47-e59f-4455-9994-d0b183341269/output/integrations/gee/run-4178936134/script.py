def mask_s2_clouds_scl(image):
    scl = image.select("SCL")
    keep = scl.neq(0).And(scl.neq(1))
    for v in (3, 8, 9, 10):
        keep = keep.And(scl.neq(v))
    return image.updateMask(keep)

dataset = "COPERNICUS/S2_SR_HARMONIZED"
bands = ["B4", "B3", "B2"]
date = "2025-11-29"

# 全 ROI（WGS84）
full_roi = load_region("/home/shared-user/研究生/QGIS-agent/数据/底图数据/gee_wuhan/wuhan_bounds.geojson").geometry()
collection = (ee.ImageCollection(dataset)
              .filterBounds(full_roi)
              .filterDate(date, ee.Date(date).advance(1, "day")))
masked = collection.sort("CLOUDY_PIXEL_PERCENTAGE", False).map(mask_s2_clouds_scl)
image = masked.mosaic().select(bands).clip(full_roi)

# 北带西部窄条（UTM 50N 下 easting 179900–187240，即本地文件显示为 nodata 的区域）
west_strip = ee.Geometry.Rectangle([179900.0, 3419700.0, 187240.0, 3475000.0], proj="EPSG:32650", geodesic=False)
with heartbeat("检查北带西部窄条覆盖"):
    cov = check_coverage(image, west_strip, scale=30, bands=bands)
print("north_west_strip_coverage:", round(float(cov), 6), flush=True)

# 中带西部窄条（easting 179900–181410）
mid_strip = ee.Geometry.Rectangle([179900.0, 3367900.0, 181410.0, 3423700.0], proj="EPSG:32650", geodesic=False)
with heartbeat("检查中带西部窄条覆盖"):
    cov2 = check_coverage(image, mid_strip, scale=30, bands=bands)
print("middle_west_strip_coverage:", round(float(cov2), 6), flush=True)

# 全 ROI 覆盖（与筛选报告对照）
with heartbeat("检查全 ROI 覆盖"):
    cov3 = check_coverage(image, full_roi, scale=30, bands=bands)
print("full_roi_coverage:", round(float(cov3), 6), flush=True)
