import importlib
import json
from pathlib import Path

import pytest
from autocarto.tests.golden.qgis_bootstrap import isolated_qgis, qgis_available


@pytest.fixture(scope='module', autouse=True)
def runtime(tmp_path_factory):
    if not qgis_available(): pytest.skip('需要 QGIS')
    with isolated_qgis(tmp_path_factory.mktemp('acquisition-runtime')): yield


def normalize(params):
    return importlib.import_module('autocarto.tools.system.data-acquisition.handlers.normalize').normalize_handler(params)


def derive(params):
    return importlib.import_module('autocarto.tools.system.data-acquisition.handlers.derive').derive_handler(params)


def test_osm_filter_clip_fields_and_readonly(tmp_path):
    from qgis.core import QgsVectorLayer, QgsProject
    source = tmp_path/'rail.osm'
    source.write_text('''<?xml version="1.0"?><osm version="0.6" generator="fixture">
<node id="1" lat="30" lon="110"/><node id="2" lat="30" lon="112"/><node id="3" lat="31" lon="110"/><node id="4" lat="31" lon="112"/>
<way id="1"><nd ref="1"/><nd ref="2"/><tag k="railway" v="rail"/><tag k="name" v="Railway"/><tag k="name:zh" v="测试铁路"/></way>
<way id="2"><nd ref="3"/><nd ref="4"/><tag k="highway" v="primary"/><tag k="name" v="Road"/></way></osm>''')
    before = source.read_bytes(); output=tmp_path/'rail.shp'
    result=normalize(dict(input={'path':str(source),'layer':'lines'},output=str(output),filter='"railway" = \'rail\'',fields={'NAME':['name:zh','name']},clip={'bbox':[110.5,29,111.5,32]},expected_geometry='line'))
    assert result['success'], result
    layer=QgsVectorLayer(str(output),'test','ogr')
    assert layer.featureCount()==1 and layer.crs().authid()=='EPSG:4326'
    assert layer.fields().names()==['NAME']
    feature=next(layer.getFeatures()); assert feature['NAME']=='测试铁路'
    assert feature.geometry().boundingBox().xMinimum()==110.5
    assert feature.geometry().boundingBox().xMaximum()==111.5
    assert output.with_suffix('.cpg').read_text().strip()=='UTF-8'
    assert source.read_bytes()==before and not QgsProject.instance().mapLayers()
    assert not normalize(dict(input=str(output),output=str(output)))['success']


def test_nullable_chinese_name_uses_first_nonnull_candidate(tmp_path):
    from qgis.core import QgsVectorLayer, QgsVariantUtils, NULL
    module = importlib.import_module('autocarto.tools.system.data-acquisition.handlers.normalize')
    assert module._present(0) and module._present(False)
    assert not module._present(NULL) and not module._present(None)
    source = tmp_path / 'nullable-names.geojson'
    rows = [{'name:zh': '中文优先', 'name': 'English'},
            {'name:zh': None, 'name': '医学院'}, {'name:zh': None, 'name': None}]
    source.write_text(json.dumps({'type': 'FeatureCollection', 'features': [
        {'type': 'Feature', 'properties': dict(row, case=i),
         'geometry': {'type': 'Point', 'coordinates': [113.5 + i / 10, 34.7]}}
        for i, row in enumerate(rows)]}, ensure_ascii=False))
    result = normalize({'input': str(source), 'output': str(tmp_path / 'names.gpkg'),
                        'fields': {'name': ['name:zh', 'name'], 'case': ['case']}})
    assert result['success'], result
    layer = QgsVectorLayer(result['output'], 'names', 'ogr')
    actual = {int(f['case']): f['name'] for f in layer.getFeatures()}
    assert actual[0] == '中文优先' and actual[1] == '医学院'
    assert QgsVariantUtils.isNull(actual[2])


def test_real_metro_raw_keeps_all_239_available_station_names(tmp_path):
    from qgis.core import QgsVectorLayer, QgsVariantUtils
    source = Path(__file__).parent / 'fixtures/metro-stations-osm.geojson'
    before = source.read_bytes()
    result = normalize({'input': str(source), 'output': str(tmp_path / 'metro.gpkg'),
                        'fields': {'name': ['name:zh', 'name']}, 'expected_geometry': 'point'})
    assert result['success'], result
    layer = QgsVectorLayer(result['output'], 'metro', 'ogr')
    names = [f['name'] for f in layer.getFeatures()]
    assert len(names) == 240
    assert sum(not QgsVariantUtils.isNull(name) for name in names) == 239
    assert '医学院' in names and '博学路' in names
    assert result['data']['fields']['name'] == pytest.approx(239 / 240)
    assert source.read_bytes() == before


def test_polygon_derivatives(tmp_path):
    from qgis.core import QgsVectorLayer
    path=tmp_path/'areas.geojson'
    features=[]
    for x,name in [(0,'甲县'),(1,'乙县')]:
        features.append(dict(type='Feature',properties={'NAME':name},geometry=dict(type='Polygon',coordinates=[[[x,0],[x+1,0],[x+1,1],[x,1],[x,0]]])))
    path.write_text(json.dumps(dict(type='FeatureCollection',features=features)))
    output=tmp_path/'boundary.gpkg'; result=derive(dict(input=str(path),method='polygon_to_boundary_lines',output=str(output)))
    assert result['success'],result
    layer=QgsVectorLayer(str(output),'lines','ogr'); rows=list(layer.getFeatures())
    assert len(rows)==1 and rows[0]['kind']=='省界' and rows[0].geometry().length()==1
    output=tmp_path/'points.gpkg'; result=derive(dict(input=str(path),method='point_on_surface',output=str(output)))
    assert result['success'] and result['data']['approximate'], result
    assert {f['NAME'] for f in QgsVectorLayer(str(output),'points','ogr').getFeatures()}=={'甲县','乙县'}


def test_raster_preserves_crs_and_stats(tmp_path):
    from osgeo import gdal,osr
    path=tmp_path/'dem.tif'; ds=gdal.GetDriverByName('GTiff').Create(str(path),20,20,1,gdal.GDT_Byte)
    crs=osr.SpatialReference(); crs.ImportFromEPSG(4326); ds.SetProjection(crs.ExportToWkt()); ds.SetGeoTransform([110,.1,0,32,0,-.1]);ds.GetRasterBand(1).Fill(100);ds=None
    result=normalize(dict(input=str(path),output=str(tmp_path/'clipped.tif'),clip={'bbox':[110.5,30.5,111.5,31.5]}))
    assert result['success'],result
    assert result['data']['width']==10 and result['data']['height']==10 and result['data']['nodata_ratio']==0
    assert derive(dict(input=str(path),method='hillshade',output=str(tmp_path/'shade.tif')))['success']


def test_within_extract_without_loading(tmp_path):
    from autocarto.tools.system._shared.admin_boundary_impl import extract_boundary_handler
    from qgis.core import QgsProject
    result = extract_boundary_handler({'within':'全国','level':'省','load':False,'output':str(tmp_path/'provinces.geojson')})
    assert result['success'], result
    assert result['data']['feature_count']==34 and len(json.loads(Path(result['output']).read_text())['features'])==34
    assert not QgsProject.instance().mapLayers()


def test_national_road_ref_regex_uses_other_tags(tmp_path):
    path=tmp_path/'roads.geojson'
    path.write_text(json.dumps(dict(type='FeatureCollection',features=[dict(type='Feature',properties={'other_tags':'"ref"=>"'+ref+'","name:zh"=>"国道"','highway':'primary'},geometry=dict(type='LineString',coordinates=[[110,30],[111,31]])) for ref in ['G107','G4','S101']])))
    result=normalize(dict(input=str(path),output=str(tmp_path/'national.gpkg'),filter='regexp_match("ref", \'^G[0-9]{3}$\')',fields={'NAME':['name:zh'],'ref':['ref']},expected_geometry='line'))
    assert result['success'] and result['data']['count']==1, result


def test_no_field_requirements_keep_all_fields_and_wgs84_extent(tmp_path):
    from osgeo import ogr,osr
    path=tmp_path/'projected.gpkg'; ds=ogr.GetDriverByName('GPKG').CreateDataSource(str(path)); crs=osr.SpatialReference(); crs.ImportFromEPSG(3857)
    layer=ds.CreateLayer('roads',crs,ogr.wkbLineString);layer.CreateField(ogr.FieldDefn('name',ogr.OFTString));layer.CreateField(ogr.FieldDefn('ref',ogr.OFTString))
    feature=ogr.Feature(layer.GetLayerDefn());feature.SetField('name','测试线');feature.SetField('ref','G107');feature.SetGeometry(ogr.CreateGeometryFromWkt('LINESTRING (0 0,111319.490793 111325.142866)'));layer.CreateFeature(feature);ds=None
    result=normalize(dict(input=str(path),output=str(tmp_path/'copy.gpkg')))
    assert result['success'],result
    assert {'name','ref'}.issubset(result['data']['fields']) and result['data']['extent'][2]>100000
    assert result['data']['extent_wgs84'][2]==pytest.approx(1,abs=1e-6) and result['data']['extent_wgs84'][3]==pytest.approx(1,abs=1e-6)


def test_all_counties_are_verified_from_one_source_pass(tmp_path, monkeypatch):
    from autocarto.tools.system._shared import admin_boundary_impl as boundaries
    original = boundaries._verify_geometry
    verified = []
    def check(path, code, layer=None, matches=None):
        assert matches is not None, "bulk verification must not rescan the GeoJSON per county"
        verified.append(code)
        return original(path, code, layer, matches)
    monkeypatch.setattr(boundaries, "_verify_geometry", check)
    result = boundaries.extract_boundary_handler({'within':'全国','level':'县','load':False,'output':str(tmp_path/'counties.geojson')})
    assert result['success'], result
    assert result['data']['feature_count'] == 2891
    assert len(verified) == len(set(verified)) == 2891


def test_shapefile_keeps_osm_long_fields_with_recorded_names(tmp_path):
    from qgis.core import QgsVectorLayer
    source = tmp_path/'transport.geojson'
    properties = {'name':'铁路','name:zh':'中文铁路','public_transport':'station','public_transport_id':'123','railway:class':'main','中文来源字段':'测试'}
    source.write_text(json.dumps({'type':'FeatureCollection','features':[{'type':'Feature','properties':properties,'geometry':{'type':'LineString','coordinates':[[110,30],[111,31]]}}]},ensure_ascii=False))
    original = source.read_bytes()
    output = tmp_path/'rail.shp'
    result = normalize({'input':str(source),'output':str(output)})
    assert result['success'], result
    layer = QgsVectorLayer(str(output),'rail','ogr')
    fields = layer.fields().names()
    assert len(fields) == len(properties) and len({name.casefold() for name in fields}) == len(fields)
    assert all(len(name.encode()) <= 10 for name in fields)
    renames = result['data']['field_renames']
    feature = next(layer.getFeatures())
    for name, value in properties.items():
        assert feature[renames.get(name,name)] == value
    assert set(result['data']['fields']) == set(fields)
    assert source.read_bytes() == original
    rejected = normalize({'input':str(source),'output':str(tmp_path/'explicit.shp'),'fields':{'public_transport':['public_transport']}})
    assert not rejected['success'] and not (tmp_path/'explicit.shp').exists()
