"""System-only TianDiTu XYZ loading after explicit project-key consent."""
import json
from pathlib import Path
import re
from urllib.parse import quote

from autocarto.utils.redaction import redact_sensitive_text


def _failure(code, message):
    return {"success": False, "output": "", "info": message, "error": message, "error_type": code}


def load_tiles_handler(params):
    # The Go bridge injects this only after routing to the bound instance. Never
    # retain it in tool arguments, return values, exceptions or a definition file.
    credentials = params.pop("__private_credentials", None)
    key = credentials.pop("tianditu", "") if isinstance(credentials, dict) else ""
    if isinstance(credentials, dict):
        credentials.clear()
    if params.get("confirmed") is not True:
        return _failure("tiles_confirmation_required", "天地图 Key 会进入 QGIS 工程的图层地址，需先明确确认。当前未验证可用于 tk 查询参数的 QGIS 认证配置。")
    if not isinstance(key, str) or not re.fullmatch(r"[A-Za-z0-9_-]{1,256}", key):
        return _failure("tiles_private_credential_required", "缺少可信通道提供的天地图凭据，请检查服务配置。")
    try:
        path = params.get("definition_path")
        if not isinstance(path, str) or not Path(path).is_absolute() or not Path(path).is_file():
            return _failure("tiles_definition_invalid", "在线底图定义路径无效。")
        with open(path, "rb") as stream:
            raw = stream.read(32769)
        if len(raw) > 32768:
            return _failure("tiles_definition_invalid", "在线底图定义过大。")
        definition = json.loads(raw)
        allowed = {"type", "style", "secret_ref", "requires_project_key_confirmation"}
        if (not isinstance(definition, dict) or set(definition) - allowed
                or definition.get("type") != "tianditu_xyz"
                or definition.get("secret_ref") != "tianditu"
                or definition.get("requires_project_key_confirmation") is not True
                or definition.get("style") not in {"vec", "img", "ter"}):
            return _failure("tiles_definition_invalid", "在线底图定义不受支持；不接受自定义地址或密钥。")
        style = definition["style"]
        url = ("https://t0.tianditu.gov.cn/" + style + "_w/wmts?SERVICE=WMTS&REQUEST=GetTile&VERSION=1.0.0"
               + "&LAYER=" + style + "&STYLE=default&TILEMATRIXSET=w&FORMAT=tiles"
               + "&TILECOL={x}&TILEROW={y}&TILEMATRIX={z}&tk=" + key)
        uri = "type=xyz&zmin=0&zmax=18&url=" + quote(url, safe="")
        from qgis.core import QgsProject, QgsRasterLayer
        project = QgsProject.instance()
        layer = next((existing for existing in project.mapLayers().values()
                      if isinstance(existing, QgsRasterLayer) and existing.isValid()
                      and existing.source() == uri), None)
        reused = layer is not None
        if layer is None:
            title = {"vec": "天地图矢量底图", "img": "天地图影像底图", "ter": "天地图地形底图"}[style]
            layer = QgsRasterLayer(uri, title, "wms")
            if not layer.isValid():
                return _failure("tiles_layer_invalid", "无法建立在线底图，请检查 QGIS 的 WMS/XYZ 支持。")
            project.addMapLayer(layer)
        return {"success": True, "output": layer.id(), "info": "在线底图已加载；实际瓦片显示还取决于密钥权限与网络。工程地址包含已确认保存的 Key。",
                "data": {"layer_id": layer.id(), "layer_name": layer.name(), "source": redact_sensitive_text(layer.source()),
                         "provider": "wms", "crs": layer.crs().authid(), "reused": reused, "online": True}}
    except Exception:
        # Provider and file exceptions may contain a URI. Do not return/format it.
        return _failure("tiles_load_failed", "在线底图加载失败，请检查定义与 QGIS 实例。")
    finally:
        key = ""
