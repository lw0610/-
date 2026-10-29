import ee, datetime, math, json

# ---------- 武汉市范围 ----------
center = ee.Geometry.Point([114.3055, 30.5928])
gaul = ee.FeatureCollection('FAO/GAUL/2015/level2')
wuhan_fc = gaul.filter(ee.Filter.eq('ADM2_NAME', 'Wuhan'))
n = wuhan_fc.size().getInfo()
region = wuhan_fc.geometry() if n > 0 else center.buffer(60000)

# ---------- 时间范围与中值合成（UInt16 原生 DN） ----------
now = datetime.datetime.now(datetime.timezone.utc)
start = now - datetime.timedelta(days=180)
col = (ee.ImageCollection('COPERNICUS/S2_SR_HARMONIZED')
       .filterBounds(region)
       .filterDate(start.strftime('%Y-%m-%d'), now.strftime('%Y-%m-%d'))
       .filter(ee.Filter.lt('CLOUDY_PIXEL_PERCENTAGE', 20))
       .select(['B4', 'B3', 'B2']))
composite = col.median().round().toUint16().clip(region)

# ---------- 边界框与分块网格（用 dimensions 控制像素，≈10m） ----------
info = region.bounds().getInfo()['coordinates'][0]
lons = [p[0] for p in info]
lats = [p[1] for p in info]
lon_min, lon_max = min(lons), max(lons)
lat_min, lat_max = min(lats), max(lats)
print('BBOX:', [lon_min, lat_min, lon_max, lat_max])

lat_c = (lat_min + lat_max) / 2.0
deg_lat = 10.0 / 111320.0
deg_lon = 10.0 / (111320.0 * math.cos(math.radians(lat_c)))

TILE_PX = 2300
tile_dlon = TILE_PX * deg_lon
tile_dlat = TILE_PX * deg_lat
print('TILE_DEG:', tile_dlon, tile_dlat)

paths = []
i = 0
y = lat_min
while y < lat_max:
    x = lon_min
    while x < lon_max:
        x2 = min(x + tile_dlon, lon_max)
        y2 = min(y + tile_dlat, lat_max)
        W = max(1, int(round((x2 - x) / deg_lon)))
        H = max(1, int(round((y2 - y) / deg_lat)))
        r = ee.Geometry.Rectangle([x, y, x2, y2])
        fn = 'wuhan_tile_%03d.tif' % i
        try:
            p = download_image(composite, fn, region=r, dimensions=[W, H], crs='EPSG:4326')
            paths.append(p)
            print('OK', fn, W, H, flush=True)
        except Exception as e:
            print('SKIP', fn, repr(e)[:140], flush=True)
        i += 1
        x += tile_dlon
    y += tile_dlat

print('TILES_TOTAL', i, 'TILES_OK', len(paths), flush=True)
print('TILE_PATHS', json.dumps(paths), flush=True)
