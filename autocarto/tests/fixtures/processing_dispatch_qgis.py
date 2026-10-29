"""Go integration worker: consume the real authority manifest and public dispatch."""
import contextlib
import json
import os
from pathlib import Path
import sys
from types import SimpleNamespace

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT))
request = json.load(sys.stdin)
os.environ['QT_QPA_PLATFORM'] = 'offscreen'
def run(request):
    from qgis.core import QgsFeature, QgsGeometry, QgsProject, QgsVectorLayer
    project = QgsProject.instance()
    from autocarto.core.tool_engine.engine import QGISToolEngine, execute_tool
    from autocarto.tools.system._shared.output_manager import set_session_context
    for name in ['cities', 'stations']:
        layer = QgsVectorLayer('Point?crs=EPSG:3857&field=name:string', name, 'memory')
        features = []
        for index, title in enumerate(['郑州市', '开封市']):
            feature = QgsFeature(layer.fields())
            feature['name'] = title
            feature.setGeometry(QgsGeometry.fromWkt(f'POINT({index * 100} 0)'))
            features.append(feature)
        assert layer.dataProvider().addFeatures(features)[0]
        project.addMapLayer(layer)
    directory = request['directory']
    set_session_context(SimpleNamespace(session_id='dispatch-test', session_dir=directory, project_path='',
                                       session_subdirs={'exports': directory, 'temp': directory}))
    engine = QGISToolEngine(load_interactive_tools=False)
    engine.update_contract(execution_manifest=request['manifest'])
    response = execute_tool(request['name'], request['params'], engine=engine)
    assert response['success'], response
    output = response['output']
    if request['name'] == 'join_attributes_by_nearest':
        joined = project.mapLayer(output['OUTPUT']['layer_id'])
        assert joined is not None and joined.featureCount() == 2
        assert output['JOINED_COUNT'] == 2 and output['UNJOINABLE_COUNT'] == 0
        assert response['output_layers'][0]['layer_id'] == joined.id()
    else:
        path = Path(output['OUTPUT'])
        assert path.is_file(), output
        assert response['output_layers'] == [{'path': str(path), 'auto_load': False}]
        uris = output.get('OUTPUT_LAYERS', [str(path)])
        assert len(uris) == (2 if request['name'] == 'package_layers' else 1)
        for uri in uris:
            reopened = QgsVectorLayer(uri, 'verified', 'ogr')
            assert reopened.isValid() and reopened.featureCount() == 2, uri
            assert {feature['name'] for feature in reopened.getFeatures()} == {'郑州市', '开封市'}
    return response

from autocarto.tests.golden.qgis_bootstrap import isolated_qgis
with contextlib.redirect_stdout(sys.stderr), isolated_qgis(Path(request['directory'])):
    response = run(request)
print(json.dumps(response, ensure_ascii=False))
