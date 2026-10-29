import shutil
from pathlib import Path
import rasterio

WORK = Path("/home/shared-user/研究生/QGIS-agent/数据/底图数据/gee_wuhan")
WORK.mkdir(parents=True, exist_ok=True)

def mask_s2_clouds_scl(image):
    scl = image.select("SCL")
    keep = scl.neq(0).And(scl.neq(1))
    for v in (3, 8, 9, 10):
        keep = keep.And(scl.neq(v))
    return image.updateMask(keep)

dataset = "COPERNICUS/S2_SR_HARMONIZED"
bands = ["B4", "B3", "B2"]
date = "2025-11-29"

left = 179900.0
right = 317800.0
bottom = 3367900.0
top = 3423700.0
tag = "middle_utm"

roi_utm = ee.Geometry.Rectangle([left, bottom, right, top], proj="EPSG:32650", geodesic=False)
collection = (ee.ImageCollection(dataset)
              .filterBounds(roi_utm)
              .filterDate(date, ee.Date(date).advance(1, "day")))
masked = collection.sort("CLOUDY_PIXEL_PERCENTAGE", False).map(mask_s2_clouds_scl)
image = masked.mosaic().select(bands).clip(roi_utm)

fname = f"wuhan_s2_20251129_{tag}_rgb10m.tif"
with heartbeat(f"下载 UTM {tag}"):
    path = download_image(image, fname, region=roi_utm, scale=10, crs="EPSG:32650")

src = Path(path)
dest = WORK / fname
shutil.copy2(src, dest)
print("copied_to:", str(dest), flush=True)
print("size_bytes:", dest.stat().st_size, flush=True)
with rasterio.open(dest) as ds:
    print("bounds:", ds.bounds, flush=True)
    print("w/h:", ds.width, ds.height, flush=True)
