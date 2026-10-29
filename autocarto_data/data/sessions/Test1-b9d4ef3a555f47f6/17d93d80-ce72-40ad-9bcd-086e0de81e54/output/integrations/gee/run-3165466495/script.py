from pathlib import Path
import json, csv

out_dir = Path("/home/shared-user/研究生/QGIS-agent/Eino/autocarto_data/data/sessions/Test1-b9d4ef3a555f47f6/17d93d80-ce72-40ad-9bcd-086e0de81e54/output/henan_population")

# 河南省第七次全国人口普查公报（第二号）——地区常住人口情况（河南省统计局，2021-05-14）
# 常住人口，2020年11月1日零时标准时点（第七次全国人口普查）
rows = [
    ("410100", "郑州市",   12600574, 12.68),
    ("410200", "开封市",    4824016,  4.85),
    ("410300", "洛阳市",    7056699,  7.10),
    ("410400", "平顶山市",  4987137,  5.02),
    ("410500", "安阳市",    5477614,  5.51),
    ("410600", "鹤壁市",    1565973,  1.58),
    ("410700", "新乡市",    6251929,  6.29),
    ("410800", "焦作市",    3521078,  3.54),
    ("410900", "濮阳市",    3772088,  3.80),
    ("411000", "许昌市",    4379998,  4.41),
    ("411100", "漯河市",    2367490,  2.38),
    ("411200", "三门峡市",  2034872,  2.05),
    ("411300", "南阳市",    9713112,  9.78),
    ("411400", "商丘市",    7816831,  7.87),
    ("411500", "信阳市",    6234401,  6.27),
    ("411600", "周口市",    9026015,  9.08),
    ("411700", "驻马店市",  7008427,  7.05),
    ("419001", "济源市",     727265,  0.73),
]

csv_path = out_dir / "henan_population_2020.csv"
with csv_path.open("w", newline="", encoding="utf-8-sig") as f:
    w = csv.writer(f)
    w.writerow(["adcode", "name", "pop_2020", "share_pct"])
    for r in rows:
        w.writerow(list(r))

# 校验合计
total = sum(r[2] for r in rows)
print("city count:", len(rows))
print("total pop:", total, "=", round(total/10000, 2), "万")
print("csv:", csv_path)

# 来源元数据
meta = {
    "data_year": 2020,
    "reference_time": "2020-11-01 00:00 (第七次全国人口普查标准时点)",
    "indicator": "常住人口",
    "unit": "人",
    "source_name": "河南省第七次全国人口普查公报（第二号）——地区常住人口情况",
    "publisher": "河南省统计局 / 河南省第七次全国人口普查领导小组办公室",
    "publish_date": "2021-05-14",
    "source_url": "https://tjj.henan.gov.cn/2021/05-14/2144517.html",
    "note": "济源市为省直辖县级行政区划；各省辖市数据与济源市分列，合计约9936.55万人",
}
meta_path = out_dir / "henan_population_2020_source.json"
meta_path.write_text(json.dumps(meta, ensure_ascii=False, indent=2), encoding="utf-8")
print("meta:", meta_path)
