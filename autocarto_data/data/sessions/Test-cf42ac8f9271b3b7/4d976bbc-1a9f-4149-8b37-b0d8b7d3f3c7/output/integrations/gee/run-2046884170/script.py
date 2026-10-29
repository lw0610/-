import ee, json
from pathlib import Path

region = load_region('/home/shared-user/研究生/QGIS-agent/enio2/autocarto_data/data/boundaries/boundary_province_440000.geojson')
roi = region.geometry()
rivers = ee.FeatureCollection('WWF/HydroSHEDS/v1/FreeFlowingRivers')

info = roi.bounds().getInfo()
coords = info['coordinates'][0]
xs = [p[0] for p in coords]; ys = [p[1] for p in coords]
xmin, xmax = min(xs), max(xs)
ymin, ymax = min(ys), max(ys)
print('bbox', xmin, ymin, xmax, ymax)

all_features = []
seen_ids = set()

def collect(x0, y0, x1, y1, depth=0):
    rect = ee.Geometry.Rectangle([x0, y0, x1, y1])
    sub = rivers.filterBounds(roi).filterBounds(rect)
    n = sub.size().getInfo()
    if n == 0:
        return 0
    if n <= 4500 or depth >= 6:
        gj = sub.getInfo()
        cnt = 0
        for f in gj.get('features', []):
            fid = f.get('id')
            if fid not in seen_ids:
                seen_ids.add(fid)
                all_features.append(f)
                cnt += 1
        print(f'depth={depth} n={n} added={cnt} bbox=({x0:.2f},{y0:.2f},{x1:.2f},{y1:.2f})', flush=True)
        return cnt
    if (x1 - x0) >= (y1 - y0):
        mid = (x0 + x1) / 2
        collect(x0, y0, mid, y1, depth+1)
        collect(mid, y0, x1, y1, depth+1)
    else:
        mid = (y0 + y1) / 2
        collect(x0, y0, x1, mid, depth+1)
        collect(x0, mid, x1, y1, depth+1)

with heartbeat('分块导出广东河流'):
    collect(xmin, ymin, xmax, ymax)

out = Path(output_dir) / 'guangdong_rivers_freeflowing.geojson'
gj = {'type': 'FeatureCollection', 'features': all_features}
with open(out, 'w', encoding='utf-8') as f:
    json.dump(gj, f, ensure_ascii=False)

print('total_features:', len(all_features), flush=True)
print('written:', out, flush=True)
print('size_bytes:', out.stat().st_size, flush=True)
