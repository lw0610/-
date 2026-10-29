import shutil
from pathlib import Path

import numpy as np
import rasterio
from rasterio.merge import merge

WORK = Path("/home/shared-user/研究生/QGIS-agent/数据/底图数据/gee_wuhan")

# 1) 复制北带 UTM 结果（早前单带验证已成功）
north_src = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto_data/data/sessions/Test1-b9d4ef3a555f47f6/07a0bc47-e59f-4455-9994-d0b183341269/output/integrations/gee/run-559157474/wuhan_s2_20251129_north_utm_rgb10m.tif")
north_dest = WORK / "wuhan_s2_20251129_north_utm_rgb10m.tif"
shutil.copy2(north_src, north_dest)
print("north_utm exists:", north_dest.exists(), "bytes:", north_dest.stat().st_size, flush=True)

# 2) 清理旧的错位 WGS84 产物，避免混淆
for old in [
    "wuhan_s2_20251129_south_rgb10m.tif",
    "wuhan_s2_20251129_middle_rgb10m.tif",
    "wuhan_s2_20251129_north_rgb10m.tif",
    "wuhan_s2_20251129_rgb10m.tif",
]:
    p = WORK / old
    if p.exists():
        p.unlink()
        print("removed old:", old, flush=True)

# 3) 合并三带 UTM（左/右边界一致，栅格对齐）
files = [
    WORK / "wuhan_s2_20251129_south_utm_rgb10m.tif",
    WORK / "wuhan_s2_20251129_middle_utm_rgb10m.tif",
    north_dest,
]
srcs = [rasterio.open(f) for f in files]
mosaic, out_trans = merge(srcs, method="first", nodata=0)
out_meta = srcs[0].meta.copy()
out_meta.update({
    "driver": "GTiff",
    "height": mosaic.shape[1],
    "width": mosaic.shape[2],
    "transform": out_trans,
    "count": mosaic.shape[0],
    "dtype": mosaic.dtype,
    "nodata": 0,
})
for s in srcs:
    s.close()

out_path = WORK / "wuhan_s2_20251129_rgb10m.tif"
with rasterio.open(out_path, "w", **out_meta) as dst:
    dst.write(mosaic)

# 4) 核验
with rasterio.open(out_path) as ds:
    print("\n=== MERGED VERIFY ===", flush=True)
    print("merged_path:", str(out_path), flush=True)
    print("crs:", str(ds.crs), flush=True)
    print("bands:", ds.count, flush=True)
    print("dtype:", [str(d) for d in ds.dtypes], flush=True)
    print("nodata:", ds.nodata, flush=True)
    print("w/h:", ds.width, ds.height, flush=True)
    print("res:", ds.res, flush=True)
    print("bounds:", ds.bounds, flush=True)
    print("size_bytes:", out_path.stat().st_size, flush=True)
    b = ds.read(1)
    valid = b != ds.nodata
    print("valid_fraction_band1:", float(valid.mean()), flush=True)
    # 分带检查有效比例，确认无水平错位缝隙
    h = ds.height
    for name, r0, r1 in [("south", 0, h//3), ("middle", h//3, 2*h//3), ("north", 2*h//3, h)]:
        seg = b[r0:r1, :]
        v = (seg != ds.nodata).mean()
        print(f"valid_fraction_{name}_third: {float(v):.4f}", flush=True)

# 列出最终产物
print("\n=== FINAL FILES ===", flush=True)
for p in sorted(WORK.iterdir()):
    print(p.name, p.stat().st_size if p.is_file() else "<dir>", flush=True)
