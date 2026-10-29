import importlib
import json
from urllib.parse import unquote

import pytest
from autocarto.tests.golden.qgis_bootstrap import isolated_qgis, qgis_available


@pytest.fixture(scope="module", autouse=True)
def runtime(tmp_path_factory):
    if not qgis_available():
        pytest.skip("需要 QGIS")
    with isolated_qgis(tmp_path_factory.mktemp("tiles-runtime")):
        yield


def test_xyz_layer_load_reuse_handle_and_project_source_redaction(tmp_path):
    from qgis.core import QgsProject
    from autocarto.tools.system._shared.data_loading_impl import load_list_handler
    from autocarto.tools.system._shared.project_state_impl import _get_layer_detailed_info
    handler = importlib.import_module("autocarto.tools.system.data-acquisition.handlers.tiles").load_tiles_handler
    definition = tmp_path / "tiles.layer.json"
    definition.write_text(json.dumps(dict(type="tianditu_xyz", style="vec", secret_ref="tianditu", requires_project_key_confirmation=True)))
    secret = "synthetic-fixture-key-not-real"
    def invoke():
        params = dict(definition_path=str(definition), confirmed=True, __private_credentials={"tianditu": secret})
        result = handler(params)
        assert "__private_credentials" not in params
        assert secret not in json.dumps(result)
        assert result["success"], result
        return result
    first = invoke()
    layer = QgsProject.instance().mapLayer(first["data"]["layer_id"])
    assert layer.isValid() and layer.providerType() == "wms"
    assert "tk=" + secret in unquote(layer.source())
    second = invoke()
    assert second["data"]["reused"] and second["data"]["layer_id"] == layer.id()
    from autocarto.core.tool_engine.engine import QGISToolEngine, execute_tool
    engine = QGISToolEngine(load_interactive_tools=False)
    engine.update_contract(execution_manifest=[{"name": "data_load_tiles", "execution": {"handler": "autocarto.tools.system.data-acquisition.handlers.tiles.load_tiles_handler"}}], public_tool_catalog=[])
    private_params = dict(definition_path=str(definition), confirmed=True, __private_credentials={"tianditu": secret})
    envelope = execute_tool("data_load_tiles", private_params, engine=engine)
    assert envelope["success"] and envelope["output"]["data"]["layer_id"] == layer.id(), envelope
    assert "__private_credentials" not in private_params
    assert not engine.list_tools()
    assert secret not in json.dumps(envelope)
    assert secret not in json.dumps(_get_layer_detailed_info(layer))
    row = dict(name="在线底图", path=str(definition), kind="raster", exists=True, required=True, layer_id=layer.id())
    reused = load_list_handler({"items": [row]})
    assert reused["success"] and reused["data"]["reused_count"] == 1, reused
    assert reused["data"]["entries"][0]["layer_id"] == layer.id()
    assert secret not in json.dumps(reused)
    QgsProject.instance().removeMapLayer(layer.id())
    missing = load_list_handler({"items": [row]})
    assert not missing["success"] and missing["error_type"] == "data_list_required_missing"
    assert not QgsProject.instance().mapLayers()
