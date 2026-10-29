from pathlib import Path
import json, shutil

session_out = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto_data/data/sessions/Test1-b9d4ef3a555f47f6/17d93d80-ce72-40ad-9bcd-086e0de81e54/output/henan_population")
fallback_gpkg = Path("/home/shared-user/研究生/QGIS-agent/数据/底图数据/autoc/fallback/exports/henan_population_2020.gpkg")

print("fallback gpkg exists:", fallback_gpkg.exists(), "size:", fallback_gpkg.stat().st_size if fallback_gpkg.exists() else None)

# 尝试用 ogr 检查 gpkg 内容
try:
    from osgeo import ogr
    ds = ogr.Open(str(fallback_gpkg))
    if ds:
        for i in range(ds.GetLayerCount()):
            lyr = ds.GetLayerByIndex(i)
            print("gpkg layer:", lyr.GetName(), "feat:", lyr.GetFeatureCount())
    else:
        print("ogr open failed")
except Exception as e:
    print("osgeo unavailable:", type(e).__name__, e)

# 列出会话输出目录的最终产物
print("\n=== session output dir contents ===")
for p in sorted(session_out.iterdir()):
    print(p.name, p.stat().st_size, "bytes")

# 将 gpkg 复制到会话输出目录，统一交付位置
target = session_out / "henan_population_2020.gpkg"
if fallback_gpkg.exists():
    shutil.copy2(fallback_gpkg, target)
    print("\ncopied gpkg ->", target, target.stat().st_size, "bytes")
