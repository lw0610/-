import ee, datetime, math, json, time
from concurrent.futures import ThreadPoolExecutor, as_completed

center = ee.Geometry.Point([114.3055, 30.5928])
gaul = ee.FeatureCollection('FAO/GAUL/2015/level2')
wuhan_fc = gaul.filter(ee.Filter.eq('ADM2_NAME', 'Wuhan'))
n = wuhan_fc.size().getInfo()
region = wuhan_fc.geometry() if n > 0 else center.buffer(60000)

now = datetime.datetime.now(datetime.timezone.utc)
start = now - datetime.timedelta(days=180)
col = (ee.ImageCollection('COPERNICUS/S2_SR_HARMONIZED')
       .filterBounds(region)
       .filterDate(start.strftime('%Y-%m-%d'), now.strftime('%Y-%m-%d'))
       .filter(ee.Filter.lt('CLOUDY_PIXEL_PERCENTAGE', 20))
       .select(['B4', 'B3', 'B2']))
composite = col.median().round().toUint16().clip(region)

info = region.bounds().getInfo()['coordinates'][0]
lons = [p[0] for p in info]; lats = [p[1] for p in info]
lon_min, lon_max, lat_min, lat_max = min(lons), max(lons), min(lats), max(lats)

lat_c = (lat_min + lat_max) / 2.0
deg_lat = 10.0 / 111320.0
deg_lon = 10.0 / (111320.0 * math.cos(math.radians(lat_c)))
TILE_PX = 2300
tile_dlon = TILE_PX * deg_lon
tile_dlat = TILE_PX * deg_lat

tiles = []
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
        tiles.append((i, r, W, H, 'wuhan_tile_%03d.tif' % i))
        i += 1
        x += tile_dlon
    y += tile_dlat
print('TOTAL_TILES', len(tiles), flush=True)

def work(t):
    idx, r, W, H, fn = t
    for attempt in range(3):
        try:
            p = download_image(composite, fn, region=r, dimensions=[W, H], crs='EPSG:4326')
            return idx, 'OK', p
        except Exception as e:
            last = repr(e)[:120]
            time.sleep(1.5 * (attempt + 1))
    return idx, 'FAIL', last

results = {}
with ThreadPoolExecutor(max_workers=8) as ex:
    futs = {ex.submit(work, t): t[0] for t in tiles}
    for fut in as_completed(futs):
        idx, status, val = fut.result()
        results[idx] = (status, val)
        print(status, 'tile', idx, flush=True)

ok = [v[1] for _, v in sorted(results.items()) if v[0] == 'OK']
fail = [k for k, v in sorted(results.items()) if v[0] == 'FAIL']
print('TILES_OK', len(ok), 'TILES_FAIL', fail, flush=True)
print('TILE_PATHS', json.dumps(ok), flush=True)
