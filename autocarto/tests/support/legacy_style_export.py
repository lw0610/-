"""Frozen v5 test oracle; never imported by production code."""
"""Write QGIS styles and their mapping; resource tables are resolved only by Go."""
import csv
from pathlib import Path
import re
import shutil
import tempfile
import unicodedata

from qgis.core import QgsProject, QgsVectorLayer, QgsVectorLayerSimpleLabeling

from autocarto.tools.system._shared.style_common import _wrap_main_thread


def find_style_layer(project, selector):
    selector = str(selector or "").strip()
    if not selector:
        raise ValueError("请指定图层名称或 ID")
    layer = project.mapLayer(selector)
    if layer is not None:
        return layer
    matches = project.mapLayersByName(selector)
    if len(matches) != 1:
        raise ValueError(f"图层不存在或名称重复，请使用图层 ID：{selector}")
    return matches[0]


def _filename(name, used):
    stem = unicodedata.normalize("NFC", name)
    stem = re.sub(r'[<>:"/\\|?*\x00-\x1f\x7f]', "_", stem).strip(" .")[:100] or "图层"
    if re.fullmatch(r"(?i)(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])(?:\..*)?", stem):
        stem = "_" + stem
    candidate, number = stem, 2
    while candidate.casefold() in used:
        candidate = f"{stem}_{number}"
        number += 1
    used.add(candidate.casefold())
    return candidate + ".qml"


def _label_field(layer):
    labeling = layer.labeling()
    if layer.labelsEnabled() and isinstance(labeling, QgsVectorLayerSimpleLabeling):
        settings = labeling.settings()
        if not settings.isExpression and layer.fields().indexFromName(settings.fieldName) >= 0:
            return settings.fieldName
    return ""


@_wrap_main_thread
def export_style_set_handler(params):
    staging = None
    try:
        directory_text = str(params.get("directory") or "").strip()
        if not directory_text:
            raise ValueError("请提供样式集输出文件夹")
        directory = Path(directory_text).expanduser().absolute()
        if directory.exists() and (not directory.is_dir() or any(directory.iterdir())):
            raise ValueError("输出文件夹必须为空，避免覆盖已有文件")
        project = QgsProject.instance()
        selectors = params.get("layers")
        if selectors is None:
            layers = [node.layer() for node in project.layerTreeRoot().findLayers()
                      if node.isVisible() and isinstance(node.layer(), QgsVectorLayer)]
        else:
            if not isinstance(selectors, list) or not all(isinstance(value, str) for value in selectors):
                raise ValueError("layers 必须是图层名称或 ID 的列表")
            layers = [find_style_layer(project, selector) for selector in selectors]
        layers = list({layer.id(): layer for layer in layers}.values())
        if not layers:
            raise ValueError("没有可导出的矢量图层，请选择图层或显示图层")
        names = set()
        for layer in layers:
            if not isinstance(layer, QgsVectorLayer) or not layer.isValid():
                raise ValueError(f"样式集只能导出有效的矢量图层：{layer.name()}")
            name = layer.name().strip()
            if not name or name in names:
                raise ValueError("样式集中的图层名称必须非空且唯一，请重命名或只选择一个同名图层")
            names.add(name)
        directory.parent.mkdir(parents=True, exist_ok=True)
        staging = Path(tempfile.mkdtemp(prefix=".style-set-", dir=str(directory.parent)))
        used, entries = set(), []
        for layer in layers:
            filename = _filename(layer.name(), used)
            message, ok = layer.saveNamedStyle(str(staging / filename))
            if not ok:
                raise ValueError(f"无法保存图层 {layer.name()} 的样式：{message}")
            entries.append({"layer_id": layer.id(), "layer": layer.name().strip(), "alias": "",
                            "style_file": filename, "label_field": _label_field(layer), "description": ""})
        with (staging / "对照表.csv").open("w", encoding="utf-8-sig", newline="") as output:
            writer = csv.writer(output)
            writer.writerow(["图层", "别名", "样式文件", "标注字段", "说明"])
            writer.writerows([entry[key] for key in ("layer", "alias", "style_file", "label_field", "description")]
                             for entry in entries)
        directory.mkdir(exist_ok=True)
        for file in staging.iterdir():
            file.rename(directory / file.name)
        return {"success": True, "output": str(directory), "info": f"已导出 {len(entries)} 个图层样式",
                "data": {"directory": str(directory), "mapping_file": str(directory / "对照表.csv"), "entries": entries}}
    except Exception as exc:
        return {"success": False, "output": "", "info": f"导出样式集失败：{exc}"}
    finally:
        if staging is not None:
            shutil.rmtree(staging, ignore_errors=True)
