
import csv, json
from pathlib import Path

outdir = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto_data/data/sessions/Test1-b9d4ef3a555f47f6/80e9e5c1-2132-4e4f-b448-22d31ec637e8/output/statistical")
outdir.mkdir(parents=True, exist_ok=True)
csv_path = outdir / "gdp_2025_provinces.csv"

# 2025 名义 GDP（初步数据），单位：百万人民币，来源：国家统计局分省年度数据（经维基百科汇总）
rows = [
    ("广东省", 14584676),
    ("江苏省", 14235150),
    ("山东省", 10319750),
    ("浙江省", 9454500),
    ("四川省", 6766534),
    ("河南省", 6663279),
    ("湖北省", 6266090),
    ("福建省", 6019945),
    ("上海市", 5670871),
    ("湖南省", 5530865),
    ("安徽省", 5298900),
    ("北京市", 5207300),
    ("河北省", 4930520),
    ("陕西省", 3655110),
    ("江西省", 3602000),
    ("重庆市", 3375793),
    ("辽宁省", 3318290),
    ("云南省", 3276578),
    ("香港特别行政区", 3052473),
    ("广西壮族自治区", 2972745),
    ("内蒙古自治区", 2671030),
    ("山西省", 2549568),
    ("贵州省", 2356217),
    ("新疆维吾尔自治区", 2146200),
    ("天津市", 1853982),
    ("黑龙江省", 1687810),
    ("吉林省", 1497388),
    ("甘肃省", 1369750),
    ("海南省", 810885),
    ("宁夏回族自治区", 569649),
    ("青海省", 412184),
    ("澳门特别行政区", 371624),
    ("西藏自治区", 303189),
]

header = ["地区", "年份", "数值", "指标", "单位", "数据来源", "数据库"]
with csv_path.open("w", encoding="utf-8", newline="") as f:
    w = csv.writer(f)
    w.writerow(header)
    for name, val in rows:
        w.writerow([name, 2025, val, "地区生产总值", "百万人民币",
                    "国家统计局分省年度数据（经维基百科汇总，2025年初步数据）", "国家统计局数据库"])

print("CSV_PATH", str(csv_path), flush=True)
print("ROW_COUNT", len(rows), flush=True)
print("VALUE_MIN", min(v for _, v in rows), flush=True)
print("VALUE_MAX", max(v for _, v in rows), flush=True)
print("NAMES", json.dumps([n for n, _ in rows], ensure_ascii=False), flush=True)
