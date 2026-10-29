import ee, json
from pathlib import Path

region = load_region('/home/shared-user/研究生/QGIS-agent/enio2/autocarto_data/data/boundaries/boundary_province_440000.geojson')
roi = region.geometry()

rivers = ee.FeatureCollection('WWF/HydroSHEDS/v1/FreeFlowingRivers')
gd = rivers.filterBounds(roi)

props = gd.first().propertyNames().getInfo()
print('properties:', props)
print('count:', gd.size().getInfo())

with heartbeat('导出广东河流GeoJSON'):
    geojson = gd.getInfo()

out = Path(output_dir) / 'guangdong_rivers_freeflowing.geojson'
with open(out, 'w', encoding='utf-8') as f:
    json.dump(geojson, f, ensure_ascii=False)

print('written:', out)
print('features:', len(geojson.get('features', [])))
print('size_bytes:', out.stat().st_size)
