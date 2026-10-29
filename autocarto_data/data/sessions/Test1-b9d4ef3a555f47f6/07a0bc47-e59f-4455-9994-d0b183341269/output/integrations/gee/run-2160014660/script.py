import json
from pathlib import Path
from datetime import datetime, timezone

# 稳定工作目录
work_dir = Path("/home/shared-user/研究生/QGIS-agent/数据/底图数据/gee_wuhan")
work_dir.mkdir(parents=True, exist_ok=True)

# 武汉市行政范围外接矩形（用户指定，WGS84）
# 东经 113°41'–115°05'，北纬 29°58'–31°22'
west = 113.0 + 41.0 / 60.0    # 113.683333
east = 115.0 + 5.0 / 60.0     # 115.083333
south = 29.0 + 58.0 / 60.0    # 29.966667
north = 31.0 + 22.0 / 60.0    # 31.366667

ring = [
    [west, south],
    [east, south],
    [east, north],
    [west, north],
    [west, south],
]

geojson = {
    "type": "FeatureCollection",
    "name": "wuhan_bbox",
    "features": [
        {
            "type": "Feature",
            "properties": {"name": "武汉市行政区范围（外接矩形）", "source": "用户指定经纬度范围"},
            "geometry": {"type": "Polygon", "coordinates": [ring]},
        }
    ],
}

boundary_path = work_dir / "wuhan_bounds.geojson"
boundary_path.write_text(json.dumps(geojson, ensure_ascii=False, indent=2), encoding="utf-8")
print("boundary_path:", boundary_path)

# 用 GeoPandas 验证边界（load_region 校验单要素 Polygon 并转 WGS84）
roi = load_region(str(boundary_path)).geometry()
info = roi.getInfo()
print("ROI geometry type:", info.get("type"))
print("bounding_box_WGS84: W=%.6f E=%.6f S=%.6f N=%.6f" % (west, east, south, north))

now = datetime.now(timezone.utc)
print("now_utc:", now.isoformat())
print("today_utc:", now.date().isoformat())
