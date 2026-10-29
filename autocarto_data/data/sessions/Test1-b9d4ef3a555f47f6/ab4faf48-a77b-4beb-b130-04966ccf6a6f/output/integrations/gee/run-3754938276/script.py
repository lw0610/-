import os
import numpy as np
import rasterio

p = '/home/shared-user/研究生/QGIS-agent/Eino/autocarto_data/data/sessions/Test1-b9d4ef3a555f47f6/ab4faf48-a77b-4beb-b130-04966ccf6a6f/output/integrations/gee/run-1061946282/wuhan_s2_rgb_6m_mosaic.tif'
with rasterio.open(p) as src:
    print('SIZE', src.width, 'x', src.height, 'count', src.count)
    print('CRS', src.crs, 'NODATA', src.nodata)
    print('PIXEL_SIZE_DEG', abs(src.transform.a), abs(src.transform.e))
    # 逐波段统计（跳过 nodata=0）
    for i in range(1, src.count + 1):
        arr = src.read(i)
        valid = arr[arr != 0]
        print('BAND', i, 'min', int(valid.min()), 'max', int(valid.max()),
              'mean', round(float(valid.mean()), 1), 'valid_px', valid.size)
print('BYTES', os.path.getsize(p))
