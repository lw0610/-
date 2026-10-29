import os, glob
base = '/home/shared-user/研究生/QGIS-agent/Eino/autocarto_data/data/sessions/Test1-b9d4ef3a555f47f6/ab4faf48-a77b-4beb-b130-04966ccf6a6f/output/integrations/gee/run-2895783148'
for f in sorted(glob.glob(base + '/wuhan_tile_*.tif')):
    print(os.path.basename(f), os.path.getsize(f), 'bytes')
