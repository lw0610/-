import numpy as np
import rasterio

def mask_s2_clouds_scl(image):
    scl = image.select("SCL")
    keep = scl.neq(0).And(scl.neq(1))
    for v in (3, 8, 9, 10):
        keep = keep.And(scl.neq(v))
    return image.updateMask(keep)

dataset = "COPERNICUS/S2_SR_HARMONIZED"
bands = ["B4", "B3", "B2"]
date = "2025-11-29"

# UTM 50N 子区域：水平边界三带完全一致，垂直方向留重叠
left = 179900.0
right = 317800.0
bottom = 3419700.0
top = 3475000.0

roi_utm = ee.Geometry.Rectangle([left, bottom, right, top], proj="EPSG:32650", geodesic=False)

collection = (ee.ImageCollection(dataset)
              .filterBounds(roi_utm)
              .filterDate(date, ee.Date(date).advance(1, "day")))
masked = collection.sort("CLOUDY_PIXEL_PERCENTAGE", False).map(mask_s2_clouds_scl)
image = masked.mosaic().select(bands).clip(roi_utm)

fname = "wuhan_s2_20251129_north_utm_rgb10m.tif"
with heartbeat("下载 UTM 北带"):
    path = download_image(image, fname, region=roi_utm, scale=10, crs="EPSG:32650")

print("path:", path, flush=True)
with rasterio.open(path) as ds:
    print("bounds:", ds.bounds, flush=True)
    print("res:", ds.res, "w/h:", ds.width, ds.height, flush=True)
    band = ds.read(1)
    valid = band != ds.nodata
    rows = np.any(valid, axis=1)
    cols = np.any(valid, axis=0)
    r0, r1 = np.where(rows)[0][[0, -1]]
    c0, c1 = np.where(cols)[0][[0, -1]]
    t = ds.transform
    print(f"valid_cols {c0}..{c1}  valid_rows {r0}..{r1}", flush=True)
    print(f"valid_left_easting={t.c + c0*t.a:.1f}  valid_right_easting={t.c + (c1+1)*t.a:.1f}", flush=True)
    print("valid_fraction:", float(valid.mean()), flush=True)
