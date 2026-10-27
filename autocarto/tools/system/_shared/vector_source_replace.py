"""Replace an explicitly identified session GeoJSON and refresh its QGIS users."""

import hashlib
import json
import os
from pathlib import Path
import shutil
import tempfile
from datetime import datetime, timezone

from qgis.core import QgsMapLayerStyle, QgsProject, QgsProviderRegistry, QgsVectorFileWriter, QgsVectorLayer

from autocarto.tools.system._shared.canvas_refresh import refresh_canvas
from autocarto.tools.system._shared.data_loading_support import DataLoadingError, normalized_data_source, split_data_source


def _digest(path):
    digest = hashlib.sha256()
    with open(path, "rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _target_path(params, session):
    raw = params.get("replace_source")
    if not isinstance(raw, str) or not raw.strip() or "\x00" in raw or not os.path.isabs(raw):
        raise DataLoadingError("invalid_replace_source", "replace_source 须为要替换的已有 GeoJSON 文件的绝对路径")
    if not session or not getattr(session, "session_dir", ""):
        raise DataLoadingError("replace_session_required", "替换生成文件需要可信的当前会话目录")
    root = Path(os.path.realpath(session.session_dir))
    target = Path(os.path.abspath(raw.strip()))
    try:
        relative = Path(os.path.realpath(target)).relative_to(root)
    except ValueError:
        raise DataLoadingError("replace_source_outside_session", "只能替换本会话生成的 GeoJSON，不能覆盖外部输入文件")
    if len(relative.parts) < 2 or relative.parts[0] not in {"output", "exports", "intermediate", "temp"}:
        raise DataLoadingError("replace_source_outside_outputs", "替换目标须位于当前会话的 output、exports、intermediate 或 temp 目录")
    # Canonicalize the trusted root's ancestors (e.g. macOS /var aliases),
    # while still rejecting symlinks encountered inside that session.
    current = target
    while True:
        if current.is_symlink():
            raise DataLoadingError("replace_source_symlink", "替换目标及其会话内父目录不能是符号链接")
        if Path(os.path.realpath(current)) == root:
            break
        if current == current.parent:
            raise DataLoadingError("replace_source_outside_session", "无法确认替换目标属于当前会话")
        current = current.parent
    if target.suffix.lower() != ".geojson" or not target.is_file():
        raise DataLoadingError("replace_source_format", "只支持替换已存在的单文件 .geojson 数据，其他格式请导出为新文件")
    output = params.get("output")
    if output and (not isinstance(output, str) or os.path.realpath(output.strip()) != os.path.realpath(target)):
        raise DataLoadingError("replace_output_conflict", "使用 replace_source 时 output 须留空或与替换目标完全一致")
    output_format = str(params.get("format") or "").strip()
    if output_format and output_format.lower() != "geojson":
        raise DataLoadingError("replace_format_conflict", "替换 GeoJSON 时 format 须为 GeoJSON 或留空")
    source = Path(str(target) + ".source.json")
    if source.is_symlink() or (source.exists() and not source.is_file()):
        raise DataLoadingError("replace_metadata_invalid", "来源说明必须是普通文件，不能是符号链接或目录")
    return target, source


def _inspect(path):
    layer = QgsVectorLayer(str(path), "replacement-validation", "ogr")
    try:
        if not layer.isValid():
            raise DataLoadingError("replace_validation_failed", "替换数据无法重新加载为有效的矢量图层")
        if layer.storageType().lower() != "geojson":
            raise DataLoadingError("replace_source_format", "替换数据的实际格式须为单文件 GeoJSON")
        details = QgsProviderRegistry.instance().querySublayers(str(path))
        return {"feature_count": int(layer.featureCount()), "geometry_type": layer.geometryType(),
                "fields": [field.name() for field in layer.fields()], "crs": layer.crs(),
                "source_layer_name": details[0].name() if details else Path(path).stem}
    finally:
        # Explicitly release OGR's file handle before Windows/macOS replacement.
        layer.setDataSource("", "replacement-validation", "ogr", False)


def _write_candidate(layer, path, source_layer_name):
    options = QgsVectorFileWriter.SaveVectorOptions()
    options.driverName = "GeoJSON"
    options.fileEncoding = "UTF-8"
    options.layerName = source_layer_name
    result = QgsVectorFileWriter.writeAsVectorFormatV3(
        layer, str(path), QgsProject.instance().transformContext(), options)
    if result[0] != QgsVectorFileWriter.NoError:
        raise DataLoadingError("replace_export_failed", "修正数据临时导出失败: " + str(result[1]))


def _affected_layers(target):
    def file_identity(uri):
        filename, _ = split_data_source(uri)
        return os.path.normcase(os.path.realpath(normalized_data_source(filename)[0]))

    identity = file_identity(str(target))
    snapshots = []
    for layer in QgsProject.instance().mapLayers().values():
        if not isinstance(layer, QgsVectorLayer) or layer.providerType() != "ogr":
            continue
        try:
            matches = file_identity(layer.source()) == identity
        except (ValueError, OSError):
            matches = False
        if not matches:
            continue
        if layer.isEditable():
            raise DataLoadingError("replace_layer_editing", "引用目标文件的图层正在编辑，请先保存或撤销编辑: " + layer.name())
        style = QgsMapLayerStyle()
        style.readFromLayer(layer)
        snapshots.append({"layer": layer, "uri": layer.source(), "name": layer.name(),
                          "provider": layer.providerType(), "style": style, "subset": layer.subsetString()})
    return snapshots


def _detach(snapshots):
    for snapshot in snapshots:
        # Keep the QgsMapLayer object (and its ID, tree/layout references), but
        # release its provider before replacing the underlying file.
        snapshot["layer"].setDataSource("", snapshot["name"], snapshot["provider"], False)


def _restore_layer(snapshot):
    layer = snapshot["layer"]
    layer.setDataSource(snapshot["uri"], snapshot["name"], snapshot["provider"], False)
    if not layer.isValid():
        raise RuntimeError("重新绑定图层失败: " + snapshot["name"])
    snapshot["style"].writeToLayer(layer)
    if snapshot["subset"] and not layer.setSubsetString(snapshot["subset"]):
        raise RuntimeError("恢复图层筛选失败: " + snapshot["name"])
    layer.updateFields()
    layer.updateExtents()
    layer.triggerRepaint()


def replace_vector_source(layer, params, session):
    """Stage and validate first; roll back both files and providers on failure."""
    target, source = _target_path(params, session)
    old = _inspect(target)
    affected = _affected_layers(target)
    metadata = {}
    source_existed = source.exists()
    if source_existed:
        try:
            metadata = json.loads(source.read_text(encoding="utf-8"))
        except (ValueError, OSError) as exc:
            raise DataLoadingError("replace_metadata_invalid", "原来源说明无法读取，已保留原文件: " + str(exc))
        if not isinstance(metadata, dict) or not isinstance(metadata.get("corrections", []), list):
            raise DataLoadingError("replace_metadata_invalid", "原来源说明或修正记录格式无效，已保留原文件")
    temporary = Path(tempfile.mkdtemp(prefix=".autocarto-replace-", dir=str(target.parent)))
    staged, backup = temporary / "candidate.geojson", temporary / "original.geojson"
    staged_source, backup_source = temporary / "candidate.source.json", temporary / "original.source.json"
    detached, committed, keep_recovery = False, False, False
    try:
        shutil.copy2(target, backup)
        if source_existed:
            shutil.copy2(source, backup_source)
        previous_hash = _digest(backup)
        _write_candidate(layer, staged, old["source_layer_name"])
        checked = _inspect(staged)
        if checked["feature_count"] != int(layer.featureCount()) or checked["fields"] != [field.name() for field in layer.fields()]:
            raise DataLoadingError("replace_validation_failed", "临时数据的要素数或字段与修正图层不一致，已保留原文件")
        if checked["geometry_type"] != old["geometry_type"] or checked["crs"] != layer.crs():
            raise DataLoadingError("replace_validation_failed", "临时数据的几何类型或坐标系与预期不一致，已保留原文件")
        metadata["feature_count"] = checked["feature_count"]
        metadata["kept_fields"] = checked["fields"]
        metadata.setdefault("corrections", []).append({
            "corrected_at": datetime.now(timezone.utc).isoformat(), "tool": "data_export_vector",
            "method": "replace_source", "input_layer_id": layer.id(), "input_layer_name": layer.name(),
            "previous_feature_count": old["feature_count"], "feature_count": checked["feature_count"],
            "previous_sha256": previous_hash, "sha256": _digest(staged),
        })
        staged_source.write_text(json.dumps(metadata, ensure_ascii=False, indent=2), encoding="utf-8")
        _target_path(params, session)
        if _digest(target) != previous_hash or source.exists() != source_existed or (source_existed and _digest(source) != _digest(backup_source)):
            raise DataLoadingError("replace_source_changed", "原文件在准备替换期间发生变化，已停止替换")
        # Once providers or files can change, retain recovery material until
        # either the commit or the entire rollback is confirmed complete.
        keep_recovery = True
        detached = True
        _detach(affected)
        committed = True
        os.replace(staged, target)
        os.replace(staged_source, source)
        for snapshot in affected:
            _restore_layer(snapshot)
        QgsProject.instance().setDirty(True)
        refresh_canvas()
        keep_recovery = False
        return {"success": True, "output": str(target), "info": "修正数据已替换原文件，并刷新引用它的图层: " + str(target),
                "data": {"replaced_source": str(target), "source_metadata": str(source),
                         "previous_feature_count": old["feature_count"], "feature_count": checked["feature_count"],
                         "updated_layer_ids": [item["layer"].id() for item in affected]}}
    except BaseException as exc:
        recovery_errors = []
        if detached:
            try:
                _detach(affected)
                if committed:
                    os.replace(backup, target)
                    if source_existed:
                        os.replace(backup_source, source)
                    elif source.exists():
                        source.unlink()
            except Exception as recovery:
                recovery_errors.append(str(recovery))
            for snapshot in affected:
                try:
                    _restore_layer(snapshot)
                except Exception as recovery:
                    recovery_errors.append(str(recovery))
            refresh_canvas()
        if recovery_errors:
            keep_recovery = True
            if not isinstance(exc, Exception):
                raise
            raise DataLoadingError("replace_recovery_failed", "替换失败；恢复未全部完成，原件备份保留于 " + str(temporary),
                                   {"cause": str(exc), "recovery_errors": recovery_errors}) from exc
        keep_recovery = False
        if not isinstance(exc, Exception):
            raise
        if isinstance(exc, DataLoadingError):
            raise
        raise DataLoadingError("replace_failed", "替换失败，原文件及图层引用已保留: " + str(exc)) from exc
    finally:
        if not keep_recovery:
            shutil.rmtree(temporary, ignore_errors=True)
