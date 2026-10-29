"""D14 real QGIS readback against the seven published basemap styles."""
from pathlib import Path
import pytest
from autocarto.tests.test_style_set_qgis import style_qgis_runtime, style_project, _layer

CASES = __import__('json').loads((Path(__file__).parent / 'fixtures/basemap_symbology.json').read_text())

ROOT = Path(__file__).resolve().parents[2]


@pytest.mark.parametrize('name,geometry,item', CASES, ids=[x[0] for x in CASES])
def test_a1_parameters_match_published_style_with_documented_label_corrections(style_project, name, geometry, item):
    from autocarto.tools.system._shared.symbology import read_symbology, set_symbology_handler, symbology_differences
    layer = _layer(geometry, name)
    reference = _layer(geometry, 'reference')
    message, ok = reference.loadNamedStyle(str(ROOT / 'autocarto/resources/system/styles' / name / (name + '.qml')))
    assert ok, message
    result = set_symbology_handler({'items': [dict(item, layer=layer.id())]})
    assert result['success'], result
    written = result['data']['items'][0]
    assert not written['differences'], written
    if name == '省域面':
        assert layer.labeling().settings().format().font().weight() == 75
        assert layer.labeling().settings().obstacle is False
    elif item['labels'] is not None:
        assert layer.labeling().settings().format().font().weight() == 50
        assert layer.labeling().settings().obstacle is True
    old = read_symbology(reference)
    actual = read_symbology(layer)
    assert not symbology_differences(old['symbol'], actual['symbol'])
    if item['labels'] is not None:
        # Font replacement is deliberate. QML styles use macOS UI alias.
        old['labels'].pop('font')
        if not old['labels']['buffer']['enabled']:
            old['labels']['buffer'] = {'enabled': False}
        assert not symbology_differences(old['labels'], actual['labels'])
    else:
        assert not actual['labels']['enabled']
        if name == '河流面':
            assert old['labels']['enabled'], 'Known v6 mismatch must not disappear silently'


@pytest.mark.parametrize('name,geometry,item', CASES, ids=[x[0] for x in CASES])
def test_a4_readback_scales_physical_sizes_and_applies_floors(style_project, name, geometry, item):
    from autocarto.tools.system._shared.symbology import set_symbology_handler
    layer = _layer(geometry, name)
    result = set_symbology_handler({'items': [dict(item, layer=layer.id())], 'scale': .35})
    written = result['data']['items'][0]
    assert result['success'] and not written['differences'], result
    actual = written['actual']
    for raw, got in zip(item['symbol']['layers'], actual['symbol']['layers']):
        for key in ('width', 'outline_width'):
            if key in raw:
                assert got[key + '_mm'] == pytest.approx(max(.1, raw[key] * .35))
        for key in ('offset', 'size'):
            if key in raw:
                assert got[key + '_mm'] == pytest.approx(raw[key] * .35)
    if item['labels'] is not None:
        raw = item['labels']
        assert actual['labels']['size_mm'] == pytest.approx(max(1.8, raw['size'] * (.3527777777777778 if raw.get('unit') == 'pt' else 1) * .35))


def test_auto_field_prefers_manifest_then_real_field_and_reports_missing(style_project):
    from qgis.core import QgsVectorLayer, QgsProject
    from autocarto.tools.system._shared.symbology import set_symbology_handler
    layer = QgsVectorLayer('Polygon?field=name:string&field=name:zh:string&crs=EPSG:4326', '只有小写name', 'memory')
    QgsProject.instance().addMapLayer(layer)
    layer.setCustomProperty('autocarto/label_field', 'name:zh')
    result = set_symbology_handler({'items': [{'layer': layer.id(), 'labels': {'field': 'auto'}}]})
    assert result['data']['items'][0]['actual']['labels']['field'] == 'name:zh'
    layer.removeCustomProperty('autocarto/label_field')
    result = set_symbology_handler({'items': [{'layer': layer.id(), 'labels': {'field': 'auto'}}]})
    assert result['data']['items'][0]['actual']['labels']['field'] == 'name'
    result = set_symbology_handler({'items': [{'layer': layer.id(), 'labels': {'field': 'does_not_exist'}}]})
    item = result['data']['items'][0]
    assert item['warnings'] and item['differences']
    assert not item['actual']['labels']['enabled']


def test_custom_dash_shapes_and_partial_update_are_read_from_qgis(style_project):
    from autocarto.tools.system._shared.symbology import set_symbology_handler, read_symbology
    line = _layer('LineString', 'dashed')
    point = _layer('Point', 'star')
    result = set_symbology_handler({'scale': .35, 'items': [
        {'layer': line.id(), 'symbol': {'type': 'line', 'layers': [{'color': '#336699', 'width': .4, 'dash': [4, 4], 'offset': -.4, 'cap': 'round'}]}},
        {'layer': point.id(), 'symbol': {'type': 'marker', 'layers': [{'shape': 'star', 'fill': None, 'size': 4, 'outline_width': .2}]}}
    ]})
    assert result['success'], result
    assert all(not x['differences'] for x in result['data']['items'])
    assert read_symbology(line)['symbol']['layers'][0]['dash_mm'] == [1.4, 1.4]
    before = read_symbology(line)['symbol']
    result = set_symbology_handler({'items': [{'layer': line.id(), 'labels': None}]})
    assert result['success'] and read_symbology(line)['symbol'] == before


def test_invalid_request_does_not_partially_replace_symbols(style_project):
    from autocarto.tools.system._shared.symbology import set_symbology_handler, read_symbology
    layer = _layer('LineString', 'line')
    before = read_symbology(layer)
    result = set_symbology_handler({'items': [{'layer': layer.id(), 'symbol': {'type': 'line', 'layers': [{'color': 'definitely-not-a-color'}]}}]})
    assert not result['success']
    assert read_symbology(layer) == before


def test_cjk_font_selection_ignores_apple_ui_alias_and_works_without_it(style_project):
    from qgis.PyQt.QtGui import QFontDatabase, QRawFont, QFont
    from autocarto.tools.system._shared.style_labeling import select_label_font
    family, warnings = select_label_font('.AppleSystemUIFont')
    assert family != '.AppleSystemUIFont' and family in QFontDatabase().families()
    assert warnings
    font = QRawFont.fromFont(QFont(family))
    assert all(font.supportsCharacter(ord(char)) for char in '河南省黄河')


def test_qml_loading_has_same_readback_font_fallback_and_railway_scaling(style_project):
    from autocarto.tools.system._shared.style_basic_impl import load_style_handler, load_styles_handler
    from autocarto.tests.support.resource_handles import resolve_handle
    railway = _layer('LineString', '铁路')
    province = _layer('Polygon', '省域')
    rail_handle = resolve_handle(ROOT / 'autocarto/resources/system/styles/铁路', 'style')
    province_handle = resolve_handle(ROOT / 'autocarto/resources/system/styles/省域面', 'style')
    result = load_styles_handler({'items': [{'layer': railway.id(), 'style': rail_handle, 'label_field': '无'},
                                          {'layer': province.id(), 'style': province_handle, 'label_field': 'name'}], 'scale': .35})
    assert result['success'], result
    rail, area = [r['data'] for r in result['data']['items']]
    assert not rail['differences'] and not area['differences'], result
    assert rail['actual']['symbol']['layers'][1]['dash_mm'] == pytest.approx([1.4, 1.4])
    assert rail['actual']['symbol']['layers'][0]['width_mm'] == pytest.approx(.14)
    assert area['actual']['labels']['font'] != '.AppleSystemUIFont'
    assert area['actual']['labels']['size_mm'] == pytest.approx(1.82)
    assert area['actual']['labels']['buffer']['size_mm'] == pytest.approx(.245)
    assert area['actual']['labels']['field'] == 'name'
    assert area['warnings']


def test_new_tool_dispatches_through_real_engine_contract(style_project):
    from autocarto.core.tool_engine.engine import QGISToolEngine, execute_tool
    from autocarto.core.paths import get_plugin_root
    layer = _layer('LineString', 'contract-line')
    engine = QGISToolEngine(load_interactive_tools=False)
    engine.update_contract(execution_manifest=[{'name': 'layer_set_symbology', 'runtime': 'qgis', 'domain': 'cartography',
        'handler_file': str(get_plugin_root() / 'tools/system/cartography/handlers/style.py'),
        'handler_function': 'set_symbology_handler'}])
    result = execute_tool('layer_set_symbology', {'items': [{'layer': layer.id(),
        'symbol': {'type': 'line', 'layers': [{'color': '#A6CEE3', 'width': .46}]}, 'labels': None}], 'scale': .35}, engine=engine)
    assert result['success'], result
    item = result['output']['data']['items'][0]
    assert not item['differences'] and item['actual']['symbol']['layers'][0]['width_mm'] == pytest.approx(.161)


if __name__ == '__main__':
    raise SystemExit(pytest.main([__file__, '-q', '-p', 'no:cacheprovider']))
