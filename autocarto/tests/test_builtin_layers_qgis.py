"""Builtin data remain unchanged while registered CRS/style/labels are applied."""
from pathlib import Path
import hashlib
import json
import os
import yaml
import pytest
from autocarto.tests.test_style_set_qgis import style_qgis_runtime, style_project


def test_published_layers_have_previews_and_valid_readonly_data(style_project, monkeypatch):
    from autocarto.tools.system._shared.builtin_layers import registrations, resolve_registration, open_layer, assets_root
    items = registrations()
    assert len(items) == 8
    for item in items:
        manifest, path = resolve_registration(item['name'])
        layer = open_layer(path, item['name'], item.get('sublayer', ''), item['crs'])
        assert layer.featureCount() == item['feature_count'] and layer.readOnly()
        assert all(not feature.geometry().isEmpty() for feature in layer.getFeatures())
        assert (Path(item['_directory']) / 'preview.png').stat().st_size > 1000
    assert all('县级城市驻地' not in i.get('aliases',[]) for i in items if '近似' in i['name'])


@pytest.mark.parametrize('level', ['province', 'city', 'county'])
def test_generated_and_published_boundaries_preserve_all_source_rings(style_project, level, tmp_path):
    from qgis.core import QgsVectorLayer
    from scripts.layers.build_system_layers import build

    root = Path(__file__).resolve().parents[2]
    source_path = root / f'autocarto/tools/system/admin-boundaries/assets/中国行政区边界/data/china_{level}.geojson'
    source_bytes = source_path.read_bytes()
    original_hash = hashlib.sha256(source_bytes).hexdigest()
    source = json.loads(source_bytes)
    expected = {}
    for feature in source['features']:
        geometry = feature['geometry']
        polygons = geometry['coordinates'] if geometry['type'] == 'MultiPolygon' else [geometry['coordinates']]
        expected[(str(feature['properties']['adcode']), feature['properties']['name'])] = [
            [(point[0], point[1]) for point in ring] for polygon in polygons for ring in polygon
        ]
    build(tmp_path / 'generated', selected=[level + '-boundary'])
    for directory in (tmp_path / 'generated', root / 'autocarto/data/layers/system/china-admin'):
        layer = QgsVectorLayer(str(directory / (level + '-boundary') / 'data.gpkg') + '|layername=boundary', 'boundary', 'ogr')
        assert layer.isValid()
        actual = {}
        for feature in layer.getFeatures():
            geometry = feature.geometry()
            assert not geometry.isEmpty(), feature['name']
            actual[(str(feature['adcode']), feature['name'])] = [
                [(point.x(), point.y()) for point in line] for line in geometry.asMultiPolyline()
            ]
        # Every exterior and interior ring, including islands/enclaves, retains
        # its original coordinates. Merely counting rows would miss empty shapes.
        assert actual == expected
    assert hashlib.sha256(source_path.read_bytes()).hexdigest() == original_hash


def test_load_style_label_clip_and_digest_preserve_original(style_project, monkeypatch):
    from autocarto.tools.system._shared.builtin_layers import layer_load_builtin_handler, resolve_registration, data_digest
    project, root = style_project
    item, source = resolve_registration('省域面')
    before = data_digest(source)
    from autocarto.tests.support.resource_handles import resolve_handle
    # Exercise the real Go resolver, as the production tool boundary does.
    style = resolve_handle(Path(__file__).resolve().parents[1] / 'resources/system/styles/省域面/省域面.qml', 'style')
    result = layer_load_builtin_handler({'name':'省域面', 'style':style})
    assert result['success'], result
    layer = project.mapLayer(result['data']['layer_id'])
    assert layer.readOnly() and layer.labelsEnabled()
    assert layer.labeling().settings().fieldName == 'name'
    assert layer.customProperty('autocarto/label_field') == item['label_field']
    from autocarto.tools.system._shared.symbology import set_symbology_handler
    applied = set_symbology_handler({'items': [{'layer': layer.id(), 'labels': {'field': 'auto'}}]})
    assert applied['success'] and not applied['data']['items'][0]['differences']
    assert applied['data']['items'][0]['actual']['labels']['field'] == 'name'
    again = layer_load_builtin_handler({'name':'省域面', 'style':style})
    assert again['data']['layer_id'] == layer.id() and len(project.mapLayers()) == 1
    assert result['data']['shown'] is True
    shown = layer_load_builtin_handler({'name':'省域面', 'style':style, 'show':False})
    assert shown['success'] and shown['data']['layer_id'] == layer.id(), shown
    assert project.layerTreeRoot().findLayer(layer.id()) is not None
    clipped = layer_load_builtin_handler({'name':'省域面','style':style,'clip':True,'extent':[112,32,116,36],'session_dir':str(root/'session'),'session_id':'test','show':False})
    assert clipped['success'], clipped
    derived = project.mapLayer(clipped['data']['layer_id'])
    assert derived.source() != str(source) and '.gpkg' in derived.source()
    assert project.layerTreeRoot().findLayer(derived.id()) is None
    assert data_digest(source) == before


def test_probe_missing_and_custom_crs_and_scale(style_project):
    from qgis.core import QgsVectorLayer, QgsFeature, QgsGeometry, QgsVectorFileWriter, QgsProject, QgsCoordinateReferenceSystem
    from autocarto.tools.system._shared.builtin_layers import inspect_layer, data_digest
    _, root = style_project
    layer = QgsVectorLayer('LineString?crs=EPSG:4326&field=NAME:string','河流线','memory')
    f = QgsFeature(layer.fields());f.setAttributes(['河']);f.setGeometry(QgsGeometry.fromWkt('LINESTRING(112 33,112.01 33.01,112.03 33.02)'))
    layer.dataProvider().addFeatures([f]);layer.updateExtents()
    path=root/'河流线.shp'
    options=QgsVectorFileWriter.SaveVectorOptions();options.driverName='ESRI Shapefile'
    assert QgsVectorFileWriter.writeAsVectorFormatV3(layer,str(path),QgsProject.instance().transformContext(),options)[0]==QgsVectorFileWriter.NoError
    before=data_digest(path)
    result=inspect_layer({'path':str(path)})
    assert result['feature_count']==1 and result['geometry']=='line'
    assert result['scale']['best_from'] > 0 and not result['needs_confirmation']
    assert 'NAME' in result['available_fields'] and data_digest(path)==before
    path.with_suffix('.prj').unlink()
    missing=inspect_layer({'path':str(path)})
    assert missing['needs_confirmation'] and missing['messages']
    confirmed=inspect_layer({'path':str(path),'crs':'EPSG:4490'})
    assert confirmed['crs']=='EPSG:4490' and confirmed['coverage']['bbox']


def test_inspect_project_memory_layer_uses_clone(style_project):
    from qgis.core import QgsVectorLayer, QgsFeature, QgsGeometry
    from autocarto.tools.system._shared.builtin_layers import inspect_layer
    project, _ = style_project
    layer=QgsVectorLayer('Point?crs=EPSG:4326&field=name:string','内存路线节点','memory')
    for x,y in [(112,32),(114,35)]:
        f=QgsFeature(layer.fields());f.setAttributes(['节点']);f.setGeometry(QgsGeometry.fromWkt(f'POINT({x} {y})'));layer.dataProvider().addFeatures([f])
    layer.updateExtents();project.addMapLayer(layer)
    before=layer.crs().authid()
    result=inspect_layer({'layer_id':layer.id(),'crs':'EPSG:4490'})
    assert result['geometry']=='point' and result['feature_count']==2
    assert result['coverage']['bbox']==pytest.approx([112,32,114,35])
    assert layer.crs().authid()==before and len(project.mapLayers())==1


def test_cleanup_removes_exact_ids_without_deleting_source(style_project):
    from qgis.core import QgsVectorLayer
    from autocarto.tools.system._shared.builtin_layers import layer_remove_created_handler
    project, root = style_project
    path=root/'existing.geojson';path.write_text('{"type":"FeatureCollection","features":[]}')
    existing=QgsVectorLayer(str(path),'旧图层','ogr');project.addMapLayer(existing)
    created=QgsVectorLayer('Point?crs=EPSG:4326','新图层','memory');project.addMapLayer(created)
    created_id=created.id()
    result=layer_remove_created_handler({'layer_ids':[created_id]})
    assert result['data']['removed_ids']==[created_id]
    assert project.mapLayer(existing.id()) is existing and project.mapLayer(created_id) is None
    assert path.is_file()


def test_cleanup_keeps_layers_referenced_by_produced_layout(style_project):
    from qgis.core import QgsVectorLayer, QgsPrintLayout, QgsLayoutItemMap, QgsLayoutItemLegend
    from autocarto.tools.system._shared.builtin_layers import layer_remove_created_handler
    project, _ = style_project
    layers = [QgsVectorLayer('Point?crs=EPSG:4326', name, 'memory') for name in ('地图数据','图例辅助层','失败草稿')]
    for layer in layers: project.addMapLayer(layer)
    ids = [layer.id() for layer in layers]
    layout=QgsPrintLayout(project);layout.initializeDefaults();layout.setName('成果版面')
    map_item=QgsLayoutItemMap(layout);layout.addLayoutItem(map_item);map_item.setLayers([layers[0]]);map_item.setKeepLayerSet(True)
    legend=QgsLayoutItemLegend(layout);layout.addLayoutItem(legend);legend.setAutoUpdateModel(False)
    legend.model().rootGroup().removeAllChildren();legend.model().rootGroup().addLayer(layers[1])
    project.layoutManager().addLayout(layout)
    result=layer_remove_created_handler({'layer_ids':ids,'keep_layouts':['成果版面']})
    assert result['data']['removed_ids']==[ids[2]]
    assert result['data']['retained_ids']==ids[:2]
    assert legend.model().rootGroup().findLayers()[0].layer() is layers[1]
    # A failed attempt does not pass successful layout outputs and retains none.
    failed=layer_remove_created_handler({'layer_ids':ids[:2]})
    assert failed['data']['removed_ids']==ids[:2] and not failed['data']['retained_ids']
