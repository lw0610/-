"""V6 resource tools exercised in one disposable QGIS process."""
from pathlib import Path
import pytest
from autocarto.tests.test_style_set_qgis import style_qgis_runtime, style_project, _layer, _state
from autocarto.tests.support.resource_handles import resolve_handle, load_handle

ROOT = Path(__file__).resolve().parents[2]


def builtin(name):
    return resolve_handle(ROOT / 'autocarto/resources/system/styles' / name, 'style')


def test_single_style_explicit_field_fallback_and_missing_field(style_project):
    from qgis.core import QgsVectorLayer, QgsProject
    from autocarto.tools.system._shared.style_basic_impl import load_style_handler
    layer = QgsVectorLayer('LineString?crs=EPSG:4326&field=name: string'.replace(': ', ':'), '道路', 'memory')
    QgsProject.instance().addMapLayer(layer)
    handle = builtin('河流线')
    result = load_style_handler({'layer':layer.id(), 'style':handle})
    assert result['success'] and result['label_field'] == 'name', result
    assert layer.labelsEnabled() and layer.labeling().settings().fieldName == 'name'
    before = _state(layer)
    rejected = load_style_handler({'layer':layer.id(), 'style':handle, 'label_field':'not_here'})
    assert not rejected['success'] and _state(layer) == before
    off = load_style_handler({'layer':layer.id(), 'style':handle, 'label_field':'无'})
    assert off['success'] and not layer.labelsEnabled()
    empty = QgsVectorLayer('LineString?crs=EPSG:4326&field=amount:int', '没有名称', 'memory')
    QgsProject.instance().addMapLayer(empty)
    result = load_style_handler({'layer':empty.id(), 'style':handle})
    assert result['success'] and result['warnings'] and not empty.labelsEnabled(), result


def test_batch_style_preserves_order_and_reports_each_failure(style_project):
    from autocarto.tools.system._shared.style_basic_impl import load_styles_handler
    line = _layer('LineString','线')
    point = _layer('Point','点')
    result = load_styles_handler({'items':[{'layer':line.id(),'style':builtin('河流线'),'label_field':'custom'}, {'layer':point.id(),'style':builtin('省域面')}, {'layer':'missing','style':builtin('铁路')}]})
    assert not result['success']
    assert [r['success'] for r in result['data']['items']] == [True,False,False]
    assert result['data']['items'][0]['data']['label_field'] == 'custom'
    assert line.labeling().settings().fieldName == 'custom'


def test_qml_expression_is_preserved_unless_field_explicitly_overridden(style_project, tmp_path):
    from qgis.core import QgsVectorLayerSimpleLabeling, QgsPalLayerSettings
    from autocarto.tools.system._shared.style_basic_impl import load_style_handler
    layer = _layer('Point','表达式')
    settings = QgsPalLayerSettings();settings.fieldName='upper(name)';settings.isExpression=True
    layer.setLabeling(QgsVectorLayerSimpleLabeling(settings));layer.setLabelsEnabled(True)
    path=tmp_path/'expression.qml';assert layer.saveNamedStyle(str(path))[1]
    handle=resolve_handle(path,'style')
    result=load_style_handler({'layer':layer.id(),'style':handle})
    assert result['success'] and layer.labeling().settings().isExpression, result
    assert layer.labeling().settings().fieldName=='upper(name)'
    result=load_style_handler({'layer':layer.id(),'style':handle,'label_field':'custom'})
    assert result['success'] and not layer.labeling().settings().isExpression
    assert layer.labeling().settings().fieldName=='custom'


def test_layout_tools_accept_both_managed_formats(style_project):
    from autocarto.tools.system._shared.layout_compose_impl import compose_layout_handler
    from autocarto.tools.system._shared.print_layout_impl import import_layout_template_handler
    project,_=style_project
    from qgis.core import QgsCoordinateReferenceSystem
    project.setCrs(QgsCoordinateReferenceSystem('EPSG:4326'))
    _layer('Polygon','区域')
    template=load_handle('layout-template')
    a=compose_layout_handler({'layout':'模板自动识别','spec':template,'title':'示例','map_layers':['区域'],'extent_layers':['区域']})
    assert a['success'] and project.layoutManager().layoutByName('模板自动识别'),a
    b=import_layout_template_handler({'name':'声明式自动识别','template':load_handle('four-corner'),'texts':{'标题':'示例'},'map_layers':['区域'],'extent_layers':['区域']})
    assert b['success'] and project.layoutManager().layoutByName('声明式自动识别'),b


@pytest.mark.parametrize('name', ['省域面','河流线','县级城市驻地'])
def test_style_preview_leaves_active_project_unchanged(style_project,tmp_path,name):
    from autocarto.tools.system._shared.resource_preview_impl import resource_preview_handler
    from qgis.PyQt.QtGui import QImage
    project,_=style_project; _layer('Point','用户图层');project.setDirty(False)
    handle=builtin(name);before=set(project.mapLayers());output=tmp_path/(name+'.png')
    result=resource_preview_handler({'resource_path':handle['entry'],'resource_type':'style','geometry':handle['geometry'],'label_field':handle['label_field'],'output_path':str(output)})
    assert result['success'],result
    image=QImage(str(output));assert not image.isNull() and image.width()==960 and image.height()==520
    assert result['data']['mode']=='legend' and result['data']['legend_count']==1
    assert set(project.mapLayers())==before and not project.isDirty() and not project.layoutManager().layouts()


@pytest.mark.parametrize('fixture',['four-corner','statistical','layout-template'])
def test_layout_preview_leaves_active_project_and_layout_unchanged(style_project,tmp_path,fixture):
    from autocarto.tools.system._shared.resource_preview_impl import resource_preview_handler
    from qgis.core import QgsPrintLayout
    from qgis.PyQt.QtGui import QImage
    project,_=style_project;_layer('Point','用户图层');layout=QgsPrintLayout(project);layout.initializeDefaults();layout.setName('用户版面');project.layoutManager().addLayout(layout);project.setDirty(False)
    handle=load_handle(fixture);before=set(project.mapLayers());output=tmp_path/(fixture+'.png')
    result=resource_preview_handler({'resource_path':handle['entry'],'resource_type':'layout','output_path':str(output)})
    assert result['success'],result
    assert not QImage(str(output)).isNull()
    assert set(project.mapLayers())==before and project.layoutManager().layouts()==[layout] and not project.isDirty()


def test_raster_style_preview_is_real_png_and_keeps_project(style_project,tmp_path):
    from autocarto.tests.test_data_loading_qgis import _raster
    from qgis.core import QgsRasterLayer
    from qgis.PyQt.QtGui import QImage
    from autocarto.tools.system._shared.resource_preview_impl import resource_preview_handler
    from autocarto.tools.system._shared.style_basic_impl import load_style_handler
    project,_=style_project
    path=_raster(tmp_path/'height.tif');layer=QgsRasterLayer(str(path),'影像','gdal');project.addMapLayer(layer)
    qml=tmp_path/'raster.qml';assert layer.saveNamedStyle(str(qml))[1]
    handle=resolve_handle(qml,'style');assert handle['geometry']=='raster'
    assert load_style_handler({'layer':layer.id(),'style':handle})['success']
    project.setDirty(False);before=set(project.mapLayers());out=tmp_path/'raster.png'
    result=resource_preview_handler({'resource_path':str(qml),'resource_type':'style','output_path':str(out)})
    assert result['success'] and result['data']['geometry']=='raster',result
    assert not QImage(str(out)).isNull() and set(project.mapLayers())==before and not project.isDirty()
