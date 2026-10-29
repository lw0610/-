import glob, os

tile_dir = '/home/shared-user/研究生/QGIS-agent/Eino/autocarto_data/data/sessions/Test1-b9d4ef3a555f47f6/ab4faf48-a77b-4beb-b130-04966ccf6a6f/output/integrations/gee/run-1061946282'
tiles = sorted(glob.glob(tile_dir + '/wuhan_tile_*.tif'))
print('N_TILES', len(tiles))

has_gdal = False
try:
    from osgeo import gdal
    print('GDAL', gdal.__version__)
    has_gdal = True
except Exception as e:
    print('NO_GDAL', repr(e))

try:
    import rasterio
    print('RASTERIO', rasterio.__version__)
except Exception as e:
    print('NO_RASTERIO', repr(e))

if has_gdal:
    ds = gdal.Open(tiles[2])
    print('SAMPLE', os.path.basename(tiles[2]), 'size', ds.RasterXSize, 'x', ds.RasterYSize, 'bands', ds.RasterCount)
    b = ds.GetRasterBand(1)
    print('DTYPE', gdal.GetDataTypeName(b.DataType), 'NODATA', b.GetNoDataValue())
    ds = None

    vrt_path = tile_dir + '/mosaic.vrt'
    out_path = tile_dir + '/wuhan_s2_rgb_6m_mosaic.tif'
    vrt = gdal.BuildVRT(vrt_path, tiles)
    if vrt is None:
        print('BUILDVRT_FAILED')
    else:
        vrt = None
        gdal.Translate(out_path, vrt_path, format='GTiff',
                      creationOptions=['COMPRESS=DEFLATE', 'TILED=YES', 'BIGTIFF=IF_SAFER'])
        d = gdal.Open(out_path)
        print('MOSAIC', out_path)
        print('MOSAIC_SIZE', d.RasterXSize, 'x', d.RasterYSize, 'bands', d.RasterCount)
        print('MOSAIC_GEO', d.GetGeoTransform())
        print('MOSAIC_BAND_NODATA', d.GetRasterBand(1).GetNoDataValue())
        print('MOSAIC_BYTES', os.path.getsize(out_path))
        d = None
