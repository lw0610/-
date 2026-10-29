import ee
import datetime

# ---------- 1. 武汉市空间范围 ----------
center = ee.Geometry.Point([114.3055, 30.5928])

# 优先尝试 FAO/GAUL 二级行政边界（地级市）
gaul = ee.FeatureCollection('FAO/GAUL/2015/level2')
wuhan_fc = gaul.filter(ee.Filter.eq('ADM2_NAME', 'Wuhan'))
n = wuhan_fc.size().getInfo()

if n > 0:
    region = wuhan_fc.geometry()
    region_desc = 'FAO/GAUL level2 武汉行政边界'
else:
    # 尝试中文名
    wuhan_fc_cn = gaul.filter(ee.Filter.eq('ADM2_NAME', '武汉市'))
    n_cn = wuhan_fc_cn.size().getInfo()
    if n_cn > 0:
        region = wuhan_fc_cn.geometry()
        region_desc = 'FAO/GAUL level2 武汉市行政边界'
        n = n_cn
    else:
        region = center.buffer(60000)  # 60km 缓冲
        region_desc = '武汉市中心 60km 缓冲范围'

print('区域来源:', region_desc, '| 匹配要素数:', n)

# ---------- 2. 时间范围（最近 6 个月） ----------
now = datetime.datetime.now(datetime.timezone.utc)
start = now - datetime.timedelta(days=180)
start_str = start.strftime('%Y-%m-%d')
end_str = now.strftime('%Y-%m-%d')
print('时间范围:', start_str, '至', end_str)

# ---------- 3. Sentinel-2 L2A 筛选与合成 ----------
col = (ee.ImageCollection('COPERNICUS/S2_SR_HARMONIZED')
       .filterBounds(region)
       .filterDate(start_str, end_str)
       .filter(ee.Filter.lt('CLOUDY_PIXEL_PERCENTAGE', 20))
       .select(['B4', 'B3', 'B2']))

count = col.size().getInfo()
print('过滤后影像数(云量<20%):', count)

# 实际可用日期范围
if count > 0:
    dates = col.aggregate_array('system:time_start').getInfo()
    dmin = datetime.datetime.fromtimestamp(min(dates)/1000, datetime.timezone.utc).strftime('%Y-%m-%d')
    dmax = datetime.datetime.fromtimestamp(max(dates)/1000, datetime.timezone.utc).strftime('%Y-%m-%d')
    print('实际影像日期范围:', dmin, '至', dmax)
else:
    print('警告: 无满足条件的影像')

# 中值合成 -> 地表反射率（L2A 缩放因子 0.0001）-> 裁剪到区域
composite = col.median().multiply(0.0001).clip(region).float()

# ---------- 4. 本地下载 GeoTIFF ----------
paths = download_image(
    image=composite,
    region=region,
    scale=10,
    crs='EPSG:4326',
    filename='wuhan_s2_rgb_6m.tif'
)
print('DOWNLOAD_PATHS:', paths)
