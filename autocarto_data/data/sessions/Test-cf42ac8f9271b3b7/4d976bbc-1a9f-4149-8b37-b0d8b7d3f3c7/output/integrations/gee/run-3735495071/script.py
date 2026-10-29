import ee

region = load_region('/home/shared-user/研究生/QGIS-agent/enio2/autocarto_data/data/boundaries/boundary_province_440000.geojson')
roi = region.geometry()

candidates = {
    'HydroRIVERS': 'WWF/HydroSHEDS/v1/HydroRIVERS',
    'FreeFlowingRivers': 'WWF/HydroSHEDS/v1/FreeFlowingRivers',
}

for name, cid in candidates.items():
    try:
        fc = ee.FeatureCollection(cid)
        gd = fc.filterBounds(roi)
        n = gd.size().getInfo()
        print(f'{name} ({cid}): count={n}')
    except Exception as e:
        print(f'{name} ({cid}): ERROR {type(e).__name__}: {e}')
