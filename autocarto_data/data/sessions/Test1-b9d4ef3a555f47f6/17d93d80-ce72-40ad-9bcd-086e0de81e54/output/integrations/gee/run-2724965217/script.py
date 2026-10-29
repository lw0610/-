from pathlib import Path
import json, csv

out_dir = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto_data/data/sessions/Test1-b9d4ef3a555f47f6/17d93d80-ce72-40ad-9bcd-086e0de81e54/output/henan_population")
city_path = out_dir / "henan_city.geojson"
csv_path = out_dir / "henan_population_2020.csv"

city = json.loads(city_path.read_text())

pop_by_adcode = {}
with csv_path.open(encoding="utf-8-sig") as f:
    for row in csv.DictReader(f):
        pop_by_adcode[row["adcode"]] = {
            "pop_2020": int(row["pop_2020"]),
            "share_pct": float(row["share_pct"]),
        }

SOURCE = "河南省第七次全国人口普查公报（第二号）——地区常住人口情况（河南省统计局，2021-05-14）"

# 校验 adcode 一一对应
city_adcodes = {f["properties"]["adcode"] for f in city["features"]}
missing = city_adcodes - set(pop_by_adcode.keys())
extra = set(pop_by_adcode.keys()) - city_adcodes
assert not missing and not extra, f"adcode mismatch missing={missing} extra={extra}"

for feat in city["features"]:
    props = feat["properties"]
    adcode = props["adcode"]
    p = pop_by_adcode[adcode]
    props["pop_2020"] = p["pop_2020"]              # 常住人口（人）
    props["pop_10k"] = round(p["pop_2020"] / 10000.0, 2)   # 常住人口（万人）
    props["share_pct"] = p["share_pct"]            # 占全省比重（%）
    props["data_year"] = 2020
    props["data_source"] = SOURCE
    props.pop("gb", None)
    props.pop("level", None)

out_path = out_dir / "henan_city_population_2020.geojson"
out_path.write_text(json.dumps(city, ensure_ascii=False), encoding="utf-8")

# 汇总验证
total = sum(f["properties"]["pop_2020"] for f in city["features"])
print("features:", len(city["features"]))
print("total pop:", total, "=", round(total/10000, 2), "万")
print("fields:", list(city["features"][0]["properties"].keys()))
print("output:", out_path)

# 打印各市人口
for f in sorted(city["features"], key=lambda x: -x["properties"]["pop_2020"]):
    p = f["properties"]
    print(f"{p['name']}\t{p['adcode']}\t{p['pop_2020']}\t{p['pop_10k']}万\t{p['share_pct']}%")
