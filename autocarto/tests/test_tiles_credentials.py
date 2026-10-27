import importlib
import io
import json
import logging
import sys
from urllib.parse import quote

import pytest

from autocarto.utils.redaction import RedactingFormatter, redact_sensitive_text

SECRET = "synthetic-fixture-key-not-real"


def definition(tmp_path, **changes):
    payload = dict(type="tianditu_xyz", style="vec", secret_ref="tianditu", requires_project_key_confirmation=True)
    payload.update(changes)
    path = tmp_path / "tiles.layer.json"
    path.write_text(json.dumps(payload))
    return str(path)


def load(params):
    return importlib.import_module("autocarto.tools.system.data-acquisition.handlers.tiles").load_tiles_handler(params)


@pytest.mark.parametrize("value", [
    "https://example.test/wmts?tk=" + SECRET + "&x=1",
    "type=xyz&url=" + quote("https://example.test/wmts?tk=" + SECRET + "&x=1", safe=""),
    quote(quote("https://example.test/wmts?tk=" + SECRET, safe=""), safe=""),
    "{'__private_credentials': {'tianditu': '" + SECRET + "'}}",
    "password='" + SECRET + "' user='test'",
    "https://test:" + SECRET + "@example.test/path",
])
def test_sensitive_sources_and_parameters_are_redacted(value):
    assert SECRET not in redact_sensitive_text(value)
    assert "redacted" in redact_sensitive_text(value)


def test_formatter_redacts_nested_parameters_and_tracebacks_without_mutation():
    output = io.StringIO()
    handler = logging.StreamHandler(output)
    handler.setFormatter(RedactingFormatter("%(message)s"))
    log = logging.getLogger("tiles.synthetic")
    log.addHandler(handler)
    log.propagate = False
    args = {"__private_credentials": {"tianditu": SECRET}, "source": "https://example.test/?tk=" + SECRET}
    try:
        log.warning("params=%s", args)
        try:
            raise ValueError("failed https://example.test/?tk=" + SECRET)
        except ValueError:
            log.exception("failed")
    finally:
        log.removeHandler(handler)
    assert SECRET not in output.getvalue()
    assert args["__private_credentials"]["tianditu"] == SECRET


def test_tool_result_boundary_scrubs_sources_and_credentials():
    from autocarto.core.tool_engine.runtime import normalize_tool_result
    result = normalize_tool_result({"success": True, "data": {"source": "url=https%3A%2F%2Fexample.test%3Ftk%3D" + SECRET, "__private_credentials": {"tianditu": SECRET}}})
    assert SECRET not in json.dumps(result)


@pytest.mark.parametrize("confirmed,authcfg", [(False, ""), (False, "abc1234"), ("true", ""), (None, "")])
def test_confirmation_is_required_even_with_unverified_authcfg(tmp_path, confirmed, authcfg):
    params = dict(definition_path=definition(tmp_path), confirmed=confirmed, authcfg=authcfg, __private_credentials={"tianditu": SECRET})
    result = load(params)
    assert not result["success"] and result["error_type"] == "tiles_confirmation_required"
    assert "__private_credentials" not in params
    assert SECRET not in json.dumps(result)


def test_public_key_and_missing_private_key_are_rejected(tmp_path):
    result = load(dict(definition_path=definition(tmp_path), confirmed=True, key=SECRET, tk=SECRET))
    assert not result["success"] and result["error_type"] == "tiles_private_credential_required"
    assert SECRET not in json.dumps(result)


@pytest.mark.parametrize("changes", [{"url": "https://untrusted.test"}, {"style": "bad"}, {"secret_ref": "other"}, {"requires_project_key_confirmation": False}])
def test_definition_cannot_redirect_credential(tmp_path, changes):
    result = load(dict(definition_path=definition(tmp_path, **changes), confirmed=True, __private_credentials={"tianditu": SECRET}))
    assert not result["success"] and result["error_type"] == "tiles_definition_invalid"


def test_provider_failure_never_formats_credential(tmp_path, monkeypatch):
    from types import ModuleType
    core = ModuleType("qgis.core")
    class Project:
        @staticmethod
        def instance():
            raise ValueError(SECRET)
    core.QgsProject, core.QgsRasterLayer = Project, object
    monkeypatch.setitem(sys.modules, "qgis.core", core)
    result = load(dict(definition_path=definition(tmp_path), confirmed=True, __private_credentials={"tianditu": SECRET}))
    assert not result["success"] and result["error_type"] == "tiles_load_failed"
    assert SECRET not in json.dumps(result)
