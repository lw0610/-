import json
from pathlib import Path

import numpy as np
import rasterio
from rasterio.merge import merge
from pyproj import Transformer

WORK = Path("/home/shared-user/研究生/QGIS-agent/数据/底图数据/gee_wuhan")

files = {
    "south": WORK / "wuhan_s2_20251129_south_rgb10m.tif",
    "middle": WORK / "wuhan_s2_20251129_middle_rgb10m.tif",
    "north": WORK / "wuhan_s2_20251129_north_rgb10m.tif",
}

# ROI 四角 WGS84 -> UTM 50N
transformer = Transformer.from_crs("EPSG:4326", "EPSG:32650", always_xy=True)
corners = {
    "SW": (113.683333, 29.966667),
    "SE": (115.083333, 29.966667),
    "NE": (115.083333, 31.366667),
    "NW": (113.683333, 31.366667),
}
utm_corners = {k: transformer.transform(lon, lat) for k, (lon, lat) in corners.items()}
xs = [p[0] for p in utm_corners.values()]
ys = [p[1] for p in utm_corners.values()]
roi_bbox = (min(xs), min(ys), max(xs), max(ys))
print("ROI_UTM_bbox (left,bottom,right,top):", roi_bbox, flush=True)

# 每个子带有效数据范围（非 nodata=0）
for name, f in files.items():
    with rasterio.open(f) as ds:
        print(f"\n=== {name} ===", flush=True)
        print("full_bounds:", ds.bounds, flush=True)
        print("res:", ds.res, "w/h:", ds.width, ds.height, flush=True)
        # 读取第一波段，找非零范围
        band = ds.read(1)
        valid = band != ds.nodata
        if valid.any():
            rows = np.any(valid, axis=1)
            cols = np.any(valid, axis=0)
            r0, r1 = np.where(rows)[0][[0, -1]]
            c0, c1 = np.where(cols)[0][[0, -1]]
            # 像素中心坐标
            t = ds.transform
            left = t.c + c0 * t.a
            right = t.c + (c1 + 1) * t.a
            top = t.f + r0 * t.e
            bottom = t.f + (r1 + 1) * t.e
            print(f"valid_cols: {c0}..{c1}  valid_rows: {r0}..{r1}", flush=True)
            print(f"valid_bbox (left,bottom,right,top): ({left:.1f},{bottom:.1f},{right:.1f},{top:.1f})", flush=True)
            print("valid_fraction:", float(valid.mean()), flush=True)
        else:
            print("NO VALID DATA", flush=True)

# 合并
srcs = [rasterio.open(f) for f in files.values()]
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
out_path = WORK / "wuhan_s2_20251129_rgb10m.tif"
with rasterio.open(out_path, "w", **out_meta) as dst:
    dst.write(mosaic)
for s in srcs:
    s.close()

with rasterio.open(out_path) as ds:
    print("\n=== MERGED ===", flush=True)
    print("merged_path:", out_path, flush=True)
    print("merged_bounds:", ds.bounds, flush=True)
    print("merged_w/h:", ds.width, ds.height, flush=True)
    print("merged_res:", ds.res, flush=True)
    print("merged_size_bytes:", out_path.stat().st_size, flush=True)
    b = ds.read(1)
    valid = b != ds.nodata
    print("merged_valid_fraction:", float(valid.mean()), flush=True)
    # 检查覆盖 ROI 范围：合并结果是否包含 ROI bbox
    mb = ds.bounds
    cover_left = mb.left <= roi_bbox[0] + 1e-6
    cover_right = mb.right >= roi_bbox[2] - 1e-6
    cover_bottom = mb.bottom <= roi_bbox[1] + 1e-6
    cover_top = mb.top >= roi_bbox[3] - 1e-6
    print("covers_ROI_left/right/bottom/top:", cover_left, cover_right, cover_bottom, cover_top, flush=True)
