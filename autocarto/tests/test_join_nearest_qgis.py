"""The advertised nearest join must execute its actual registered QGIS algorithm."""
from pathlib import Path
from types import SimpleNamespace
import pytest
import yaml
from autocarto.tests.test_style_set_qgis import style_qgis_runtime, style_project


@pytest.mark.parametrize('output_kind', ['memory', 'gpkg'])
def test_nearest_join_real_yaml_fields_distance_and_unmatched_features(style_project, monkeypatch, tmp_path, output_kind):
    from qgis.core import QgsApplication, QgsVectorLayer, QgsFeature, QgsGeometry, QgsProject, NULL
    from autocarto.core.tool_engine.engine import QGISToolEngine, execute_tool
    from autocarto.tools.system._shared import output_manager
    monkeypatch.setattr(output_manager, '_current_session', SimpleNamespace(
        session_id='nearest-test', session_dir=str(tmp_path), project_path='', session_subdirs={'temp': str(tmp_path)}))
    path = Path(__file__).resolve().parents[1] / 'tools/system/vector-fields/join_attributes_by_nearest.yaml'
    definition = yaml.safe_load(path.read_text())
    execution = definition['execution']
    algorithm = QgsApplication.processingRegistry().algorithmById('native:joinbynearest')
    assert algorithm is not None
    # Check public parameter wiring against the live registry, not a mocked ID.
    assert set(execution['param_mapping']).issubset({p.name() for p in algorithm.parameterDefinitions()})
    import processing
    native_run = processing.run
    actual_calls = []
    def record_native_run(algorithm_id, params, **kwargs):
        actual_calls.append((algorithm_id, params))
        return native_run(algorithm_id, params, **kwargs)
    monkeypatch.setattr(processing, 'run', record_native_run)
    source = QgsVectorLayer('Point?crs=EPSG:3857&field=source_id:integer', '待连接点', 'memory')
    targets = QgsVectorLayer('Point?crs=EPSG:3857&field=line_name:string&field=ignored:string', '参照点', 'memory')
    for layer, rows in [(source, [(0, 0, [1]), (100, 0, [2])]),
                        (targets, [(3, 4, ['线路甲', '不复制']), (300, 0, ['线路乙', '不复制'])])]:
        features = []
        for x, y, attrs in rows:
            feature = QgsFeature(layer.fields()); feature.setAttributes(attrs)
            feature.setGeometry(QgsGeometry.fromWkt(f'POINT({x} {y})')); features.append(feature)
        assert layer.dataProvider().addFeatures(features)[0]
        layer.updateExtents(); QgsProject.instance().addMapLayer(layer)
    engine = QGISToolEngine(load_interactive_tools=False)
    engine.update_contract(execution_manifest=[{**definition['metadata'], 'parameters': definition['spec']['parameters'],
        'execution': execution}])
    output = 'memory:nearest-result' if output_kind == 'memory' else str(tmp_path / 'nearest.gpkg')
    envelope = execute_tool('join_attributes_by_nearest', {'INPUT': source.id(), 'INPUT_2': targets.id(),
        'FIELDS_TO_COPY': 'line_name', 'MAX_DISTANCE': 10, 'NEIGHBORS': 1, 'OUTPUT': output}, engine=engine)
    assert envelope['success'], envelope
    result = envelope['output']
    found = [layer for layer in QgsProject.instance().mapLayers().values()
             if layer.id() not in (source.id(), targets.id())]
    assert len(found) == 1, result
    joined = found[0]
    assert actual_calls[0][0] == 'native:joinbynearest'
    assert actual_calls[0][1]['FIELDS_TO_COPY'] == ['line_name']
    assert result['OUTPUT']['layer_id'] == joined.id()
    assert result['JOINED_COUNT'] == 1
    assert result['UNJOINABLE_COUNT'] == 1
    assert envelope['output_layers'][0]['layer_id'] == joined.id()
    if output_kind == 'gpkg':
        assert Path(output).is_file()
    assert joined.featureCount() == 2
    assert 'line_name' in joined.fields().names() and 'ignored' not in joined.fields().names()
    by_id = {feature['source_id']: feature for feature in joined.getFeatures()}
    assert by_id[1]['line_name'] == '线路甲'
    assert by_id[1]['distance'] == pytest.approx(5)
    assert by_id[2]['line_name'] == NULL and by_id[2]['distance'] == NULL
    assert source.featureCount() == 2 and targets.featureCount() == 2
