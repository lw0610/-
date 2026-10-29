import glob, os
import rasterio
from rasterio.merge import merge

tile_dir = '/home/shared-user/研究生/QGIS-agent/Eino/autocarto_data/data/sessions/Test1-b9d4ef3a555f47f6/ab4faf48-a77b-4beb-b130-04966ccf6a6f/output/integrations/gee/run-1061946282'
tiles = sorted(glob.glob(tile_dir + '/wuhan_tile_*.tif'))
print('N_TILES', len(tiles))

# 检查一个数据块（002 之前为 15.8MB 数据块）
sample = None
for t in tiles:
    if os.path.getsize(t) > 100000:
        sample = t
        break
print('SAMPLE', os.path.basename(sample))
with rasterio.open(sample) as src:
    print('DTYPE', src.dtypes, 'NODATA', src.nodata, 'COUNT', src.count, 'SIZE', src.width, src.height)
    print('CRS', src.crs)
    print('TRANSFORM', list(src.transform))
    crs = src.crs

# 合并
nodata = 0
datasets = [rasterio.open(t) for t in tiles]
mosaic, out_transform = merge(datasets, method='first', nodata=nodata, dtype='uint16')
for d in datasets:
    d.close()

out_path = tile_dir + '/wuhan_s2_rgb_6m_mosaic.tif'
with rasterio.open(out_path, 'w', driver='GTiff',
                   height=mosaic.shape[1], width=mosaic.shape[2], count=mosaic.shape[0],
                   dtype='uint16', crs=crs, transform=out_transform, nodata=nodata,
                   compress='deflate', tiled=True, bigtiff='IF_SAFER') as dst:
    dst.write(mosaic)
    dst.set_band_description(1, 'B4_Red')
    dst.set_band_description(2, 'B3_Green')
    dst.set_band_description(3, 'B2_Blue')

print('MOSAIC_PATH', out_path)
with rasterio.open(out_path) as d:
    print('MOSAIC_SIZE', d.width, 'x', d.height, 'count', d.count, 'dtype', d.dtypes, 'nodata', d.nodata)
    print('MOSAIC_BOUNDS', list(d.bounds))
print('MOSAIC_BYTES', os.path.getsize(out_path))
