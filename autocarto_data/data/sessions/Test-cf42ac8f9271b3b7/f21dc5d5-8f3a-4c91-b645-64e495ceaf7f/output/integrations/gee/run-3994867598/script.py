from pathlib import Path
import json

# 人口数据来源：国家统计局《第七次全国人口普查公报（第三号）》（2021-05-11）
# 口径：2020-11-01 零时大陆 31 个省、自治区、直辖市 常住人口（单位：人）
# 注：全国合计 1411778724 含现役军人 2000000，不含港澳台；本表仅取 31 个省级行政区。

rows = [
    ("北京市", 21893095),
    ("天津市", 13866009),
    ("河北省", 74610235),
    ("山西省", 34915616),
    ("内蒙古自治区", 24049155),
    ("辽宁省", 42591407),
    ("吉林省", 24073453),
    ("黑龙江省", 31850088),
    ("上海市", 24870895),
    ("江苏省", 84748016),
    ("浙江省", 64567588),
    ("安徽省", 61027171),
    ("福建省", 41540086),
    ("江西省", 45188635),
    ("山东省", 101527453),
    ("河南省", 99365519),
    ("湖北省", 57752557),
    ("湖南省", 66444864),
    ("广东省", 126012510),
    ("广西壮族自治区", 50126804),
    ("海南省", 10081232),
    ("重庆市", 32054159),
    ("四川省", 83674866),
    ("贵州省", 38562148),
    ("云南省", 47209277),
    ("西藏自治区", 3648100),
    ("陕西省", 39528999),
    ("甘肃省", 25019831),
    ("青海省", 5923957),
    ("宁夏回族自治区", 7202654),
    ("新疆维吾尔自治区", 25852345),
]

out_dir = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto_data/data/sessions/Test-cf42ac8f9271b3b7/f21dc5d5-8f3a-4c91-b645-64e495ceaf7f/output")
out_dir.mkdir(parents=True, exist_ok=True)
csv_path = out_dir / "china_province_population_2020_census.csv"

lines = ["年份,地区,数值"]
for name, val in rows:
    lines.append(f"2020,{name},{val}")

csv_path.write_text("\n".join(lines) + "\n", encoding="utf-8")

# 校验：合计（31 省，不含现役军人）
total = sum(v for _, v in rows)
print("CSV_PATH", csv_path, flush=True)
print("ROW_COUNT", len(rows), flush=True)
print("SUM", total, flush=True)
print("EXPECTED_SUM_31PROV", 1411778724 - 2000000, flush=True)
print("MATCH", total == (1411778724 - 2000000), flush=True)
