import ee, datetime

# ---------- 1. 武汉市范围 ----------
center = ee.Geometry.Point([114.3055, 30.5928])
gaul = ee.FeatureCollection('FAO/GAUL/2015/level2')
wuhan_fc = gaul.filter(ee.Filter.eq('ADM2_NAME', 'Wuhan'))
n = wuhan_fc.size().getInfo()
region = wuhan_fc.geometry() if n > 0 else center.buffer(60000)
print('区域来源:', 'FAO/GAUL level2 武汉行政边界' if n > 0 else '60km缓冲', '| 要素数:', n)

# ---------- 2. 时间范围 ----------
now = datetime.datetime.now(datetime.timezone.utc)
start = now - datetime.timedelta(days=180)
start_str = start.strftime('%Y-%m-%d')
end_str = now.strftime('%Y-%m-%d')
print('时间范围:', start_str, '至', end_str)

# ---------- 3. S2 L2A 筛选与中值合成（原生 UInt16 DN，反射率×10000） ----------
col = (ee.ImageCollection('COPERNICUS/S2_SR_HARMONIZED')
       .filterBounds(region)
       .filterDate(start_str, end_str)
       .filter(ee.Filter.lt('CLOUDY_PIXEL_PERCENTAGE', 20))
       .select(['B4', 'B3', 'B2']))
print('过滤后影像数(云量<20%):', col.size().getInfo())

composite = col.median().clip(region)

# ---------- 4. 导出到 Drive 并落地 ----------
res = export_image_to_drive(
    composite,
    'wuhan_s2_rgb_6m',
    region=region,
    scale=10,
    crs='EPSG:4326',
    folder='gee_exports',
    fileNamePrefix='wuhan_s2_rgb_6m',
    maxPixels=1e9,
    fileFormat='GEO_TIFF'
)
print('EXPORT_RESULT:', res)
