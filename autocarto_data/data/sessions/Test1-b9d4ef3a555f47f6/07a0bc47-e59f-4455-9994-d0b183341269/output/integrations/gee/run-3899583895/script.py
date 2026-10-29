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

west = 113.683333
east = 115.083333
south = 30.896000
north = 31.366667
tag = "north"

roi = ee.Geometry.Rectangle([west, south, east, north], proj="EPSG:4326", geodesic=False)

collection = (ee.ImageCollection(dataset)
              .filterBounds(roi)
              .filterDate(date, ee.Date(date).advance(1, "day")))
masked = collection.sort("CLOUDY_PIXEL_PERCENTAGE", False).map(mask_s2_clouds_scl)
image = masked.mosaic().select(bands).clip(roi)

fname = f"wuhan_s2_20251129_{tag}_rgb10m.tif"
with heartbeat(f"下载子区域 {tag}"):
    path = download_image(image, fname, region=roi, scale=10, crs="EPSG:32650")

src = Path(path)
dest = WORK / fname
shutil.copy2(src, dest)
print("downloaded:", str(src), flush=True)
print("copied_to:", str(dest), flush=True)
print("size_bytes:", dest.stat().st_size, flush=True)

with rasterio.open(dest) as ds:
    print("crs:", str(ds.crs), flush=True)
    print("bands:", ds.count, flush=True)
    print("dtype:", [str(d) for d in ds.dtypes], flush=True)
    print("nodata:", ds.nodata, flush=True)
    print("w/h:", ds.width, ds.height, flush=True)
    print("bounds:", ds.bounds, flush=True)
