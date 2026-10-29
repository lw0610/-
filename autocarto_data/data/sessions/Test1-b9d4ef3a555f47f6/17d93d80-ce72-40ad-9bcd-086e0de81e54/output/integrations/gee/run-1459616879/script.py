from pathlib import Path
import json

skill_base_dir = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto/skills/system/china-admin-boundaries")
data_dir = skill_base_dir / "data"

out_dir = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto_data/data/sessions/Test1-b9d4ef3a555f47f6/17d93d80-ce72-40ad-9bcd-086e0de81e54/output/henan_population")
out_dir.mkdir(parents=True, exist_ok=True)

def load(name):
    return json.loads((data_dir / name).read_text())

# 1) province
prov = load("china_province.geojson")
henan_prov_feats = [f for f in prov["features"] if f["properties"].get("adcode") == "410000"]

# 2) city (prefecture level, 河南省 41xx)
city = load("china_city.geojson")
henan_city_feats = [f for f in city["features"] if str(f["properties"].get("adcode", "")).startswith("41")]

# 3) county (区县, 41xxxx)
county = load("china_county.geojson")
henan_county_feats = [f for f in county["features"] if str(f["properties"].get("adcode", "")).startswith("41")]

def write(feats, fname):
    fc = {"type": "FeatureCollection", "name": fname, "features": feats}
    p = out_dir / fname
    p.write_text(json.dumps(fc, ensure_ascii=False))
    return p

p1 = write(henan_prov_feats, "henan_province.geojson")
p2 = write(henan_city_feats, "henan_city.geojson")
p3 = write(henan_county_feats, "henan_county.geojson")

print("province:", len(henan_prov_feats), p1)
print("city:", len(henan_city_feats), p2)
print("county:", len(henan_county_feats), p3)

# 打印城市清单及其 adcode
city_names = sorted((f["properties"].get("adcode"), f["properties"].get("name")) for f in henan_city_feats)
print("city list:", json.dumps(city_names, ensure_ascii=False))

# 检查属性字段
print("sample city props keys:", list(henan_city_feats[0]["properties"].keys()))
print("sample county props keys:", list(henan_county_feats[0]["properties"].keys()))
