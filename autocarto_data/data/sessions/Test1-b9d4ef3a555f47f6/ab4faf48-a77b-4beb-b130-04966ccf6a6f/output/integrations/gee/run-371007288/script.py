import ee, datetime

center = ee.Geometry.Point([114.3055, 30.5928])
gaul = ee.FeatureCollection('FAO/GAUL/2015/level2')
wuhan_fc = gaul.filter(ee.Filter.eq('ADM2_NAME', 'Wuhan'))
region = wuhan_fc.geometry()

now = datetime.datetime.now(datetime.timezone.utc)
start = now - datetime.timedelta(days=180)
col = (ee.ImageCollection('COPERNICUS/S2_SR_HARMONIZED')
       .filterBounds(region)
       .filterDate(start.strftime('%Y-%m-%d'), now.strftime('%Y-%m-%d'))
       .filter(ee.Filter.lt('CLOUDY_PIXEL_PERCENTAGE', 20))
       .select(['B4', 'B3', 'B2']))
composite = col.median().round().toUint16().clip(region)

print('COMPOSITE_PROJECTION:', composite.projection().getInfo())
print('COMPOSITE_BANDS:', [b.get('id') for b in composite.bandTypes().getInfo().values()] if False else composite.bandTypes().getInfo())

# 一个"满尺寸"分块（左下角）
tile = ee.Geometry.Rectangle([113.644966, 30.010651, 113.885244, 30.217262])

for label, kwargs in [
    ('scale10+crs4326', dict(scale=10, crs='EPSG:4326')),
    ('scale10_nocrs', dict(scale=10)),
    ('dim2300+crs4326', dict(dimensions=2300, crs='EPSG:4326')),
    ('dim2300_nocrs', dict(dimensions=2300)),
]:
    params = {"format": "GEO_TIFF", "filePerBand": False, "region": tile}
    params.update(kwargs)
    try:
        u = composite.getDownloadURL(params)
        print('OK  ', label, '| url_len', len(u))
    except Exception as e:
        print('FAIL', label)
        print('     ', str(e)[:400])
