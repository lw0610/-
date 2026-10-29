"""专题点图层准备与汇合点标签。"""
from typing import (
    Any,
    Dict,
    List,
)
from pathlib import Path
from qgis.PyQt.QtCore import QVariant
from qgis.PyQt.QtGui import (
    QColor,
    QFont,
)
from qgis.core import (
    Qgis,
    QgsFeature,
    QgsField,
    QgsGeometry,
    QgsUnitTypes,
    QgsPalLayerSettings,
    QgsProject,
    QgsPointXY,
    QgsTextBufferSettings,
    QgsTextFormat,
    QgsVectorLayer,
    QgsRuleBasedLabeling,
    QgsVectorLayerSimpleLabeling,
    QgsMarkerSymbol, QgsSingleSymbolRenderer, QgsRuleBasedRenderer,
)
from autocarto.tools.system._shared.table_io import (
    _find_col,
    _get_col_value,
    _read_csv_rows,
    _to_bool,
)


from autocarto.knowledge.visual_presets import load_symbol_styles, renderer_defaults
from autocarto.tools.system._shared.resource_handle import require_handle, handle_summary
from autocarto.knowledge.dataset_handoff import DatasetHandoffError, resolve_route_dataset
from autocarto.tools.system._shared.style_common import _wrap_main_thread
from autocarto.tools.system._shared.route_runtime_support import dataset_failure, label_format


def _label_rendezvous_points(point_layer, rule_name="汇合点标注"):
    """Keep place-name labeling and add participant names only for real rendezvous."""
    if point_layer.fields().indexFromName("meeting_label") < 0:
        return {"count": 0}
    count = sum(bool(str(feature["meeting_label"]).strip()) and
                str(feature["meeting_label"]) not in ("NULL", "None")
                for feature in point_layer.getFeatures())
    if not count:
        return {"count": 0}
    existing = point_layer.labeling()
    if isinstance(existing, QgsRuleBasedLabeling):
        root = existing.rootRule().clone()
        for index in range(len(root.children()) - 1, -1, -1):
            if root.children()[index].description() == rule_name:
                root.removeChildAt(index)
    else:
        root = QgsRuleBasedLabeling.Rule(QgsPalLayerSettings())
        if existing and point_layer.labelsEnabled():
            root.appendChild(QgsRuleBasedLabeling.Rule(existing.settings()))
    settings = QgsPalLayerSettings(); settings.fieldName = "meeting_label"
    settings.placement = Qgis.LabelPlacement.OrderedPositionsAroundPoint
    settings.dist = 4.0; settings.distUnits = QgsUnitTypes.RenderMillimeters
    settings.priority = 9; settings.wrapChar = "\n"
    settings.setFormat(label_format("rendezvous"))
    rule = QgsRuleBasedLabeling.Rule(settings)
    rule.setDescription(rule_name)
    rule.setFilterExpression('trim(coalesce("meeting_label", \'\')) <> \'\'')
    root.appendChild(rule)
    point_layer.setLabeling(QgsRuleBasedLabeling(root)); point_layer.setLabelsEnabled(True)
    point_layer.triggerRepaint()
    return {"count": count}



def _prepare_route_points_layer(
    dataset_dir: Path,
    points_layer_name: str,
    overwrite_points_layer: bool,
    points_input_crs: str,
    run_symbols: bool,
    run_routes: bool,
    result: Dict[str, Any],
):
    """准备旧组合入口的专题点图层，并更新该阶段的原始回执。"""
    from qgis.core import QgsFeature, QgsGeometry, QgsVectorLayer, QgsField
    from qgis.PyQt.QtCore import QVariant

    # 0) 专题点图层准备：优先以 dataset_dir/points.csv 为准
    points_csv = dataset_dir / "points.csv"
    if points_csv.exists():
        point_rows = _read_csv_rows(points_csv)

        # 最小必需列检查
        required_cols = ["name", "x", "y", "symbol_type", "priority"]
        if point_rows:
            sample = point_rows[0]
            missing = [c for c in required_cols if _find_col(sample, [c]) is None]
            if missing:
                raise ValueError(f"points.csv 缺少必需列: {', '.join(missing)}")

        proj = QgsProject.instance()
        existing = proj.mapLayersByName(points_layer_name)
        if existing and overwrite_points_layer:
            for lyr in existing:
                proj.removeMapLayer(lyr.id())
        if existing and (not overwrite_points_layer):
            result.update({
                "success": True,
                "executed": True,
                "layer_name": points_layer_name,
                "created_count": 0,
                "skipped_count": 0,
                "warnings": [f"图层 {points_layer_name} 已存在且 overwrite_points_layer=False，复用现有图层"],
            })
        else:
            vlayer = QgsVectorLayer(f"Point?crs={points_input_crs}", points_layer_name, "memory")
            provider = vlayer.dataProvider()

            # 保留 points.csv 原字段
            all_field_names: List[str] = []
            for r in point_rows:
                for k in (r or {}).keys():
                    kn = str(k or "").strip()
                    if kn and kn not in all_field_names:
                        all_field_names.append(kn)

            qfields = [QgsField(fn, QVariant.String) for fn in all_field_names]
            provider.addAttributes(qfields)
            vlayer.updateFields()

            created_count = 0
            skipped_count = 0
            feats: List[QgsFeature] = []
            for row in point_rows:
                try:
                    x = float(_get_col_value(row, ["x"], required=True))
                    y = float(_get_col_value(row, ["y"], required=True))
                except Exception:
                    skipped_count += 1
                    continue

                feat = QgsFeature(vlayer.fields())
                feat.setGeometry(QgsGeometry.fromPointXY(QgsPointXY(x, y)))
                attrs = []
                for fn in all_field_names:
                    attrs.append(row.get(fn, ""))
                feat.setAttributes(attrs)
                feats.append(feat)
                created_count += 1

            if feats:
                provider.addFeatures(feats)
            vlayer.updateExtents()
            proj.addMapLayer(vlayer)

            result.update({
                "success": True,
                "executed": True,
                "layer_name": points_layer_name,
                "created_count": created_count,
                "skipped_count": skipped_count,
                "warnings": [],
            })
    else:
        warn = f"未找到 points.csv: {points_csv}"
        warnings = [warn]
        if run_symbols:
            warnings.append("run_symbols=True 但缺少 points.csv，符号化将失败")
        if run_routes:
            warnings.append("run_routes=True：路线仍基于 places/routes/route_paths 生成，但缺少专题点图层参照")
        result.update({
            "success": True,
            "executed": False,
            "warnings": warnings,
        })

from autocarto.tools.system._shared.symbolize_mapping_impl import symbolize_points_by_mapping_handler


def _symbol_rows(params, dataset_dir, dataset_table):
    if params.get("symbols"):
        _, _, sources, rows = load_symbol_styles(params["symbols"])
        return [dict(row, svg_path=str(sources[row["symbol_type"]])) for row in rows]
    return _read_csv_rows(dataset_table)


def route_points_build_handler(params):
    try:
        if params.get("symbols"):
            params = dict(params)
            params["symbols"] = require_handle(params["symbols"], "symbols")
        resolved = resolve_route_dataset(params)
        dataset_dir = resolved.dataset_dir
        points_layer_name = str(params.get("points_layer") or "专题点")
        strict = _to_bool(params.get("strict", True))
        result = {"success": True, "executed": False, "layer_name": points_layer_name,
                  "created_count": 0, "skipped_count": 0, "warnings": []}
        _prepare_route_points_layer(dataset_dir, points_layer_name, _to_bool(params.get("overwrite", True)),
                                    str(params.get("input_crs") or "EPSG:4326"), True, False, result)
        points_csv, symbols_csv = dataset_dir / "points.csv", dataset_dir / "symbol_mapping.csv"
        if not points_csv.is_file() or not (symbols_csv.is_file() or params.get("symbols")):
            raise ValueError("缺少 points.csv 或符号对照表")
        point_layer = QgsProject.instance().mapLayersByName(points_layer_name)[0]
        if params.get("default_symbol_types") is None or params.get("default_symbol_types"):
            point_layer.setRenderer(QgsSingleSymbolRenderer(QgsMarkerSymbol.createSimple(renderer_defaults()["default_point_symbol"])))
        results = {"points_layer": result}
        point_rows = _read_csv_rows(points_csv)
        symbol_rows_raw = _symbol_rows(params, dataset_dir, symbols_csv)
        symbol_rows: List[Dict[str, Any]] = []
        for srow in symbol_rows_raw:
            s2 = dict(srow or {})
            svg_path_raw = str(s2.get("svg_path", "") or "").strip()
            if svg_path_raw and (not Path(svg_path_raw).is_absolute()):
                s2["svg_path"] = str((dataset_dir / svg_path_raw).resolve())
            symbol_rows.append(s2)

        spayload = {
            "layer": points_layer_name,
            "point_rows": point_rows,
            "symbol_rows": symbol_rows,
            "name_field": "name",
            "symbol_type_field": str(params.get("symbol_type_field", "symbol_type") or "symbol_type"),
            "default_symbol_types": params.get("default_symbol_types") if params.get("default_symbol_types") is not None else ["waypoint"],
            "strict": strict,
            "hide_unmatched": _to_bool(params.get("hide_unmatched", True)),
            "fallback_size": float(params.get("fallback_size", 4) or 4),
        }
        sres = symbolize_points_by_mapping_handler(spayload)
        results["symbols"] = {
            "success": bool((sres or {}).get("success")),
            "executed": True,
            "info": (sres or {}).get("info", ""),
            "output": (sres or {}).get("output", ""),
            "data": (sres or {}).get("data") or {},
        }
        defaults = spayload["default_symbol_types"]
        renderer = point_layer.renderer()
        if sres.get("success") and defaults and isinstance(renderer, QgsRuleBasedRenderer):
            quoted = ", ".join("'" + str(value).replace("'", "''") + "'" for value in defaults)
            rule = QgsRuleBasedRenderer.Rule(QgsMarkerSymbol.createSimple(renderer_defaults()["default_point_symbol"]))
            rule.setLabel("普通点")
            rule.setFilterExpression('"symbol_type" IN (' + quoted + ')')
            renderer.rootRule().appendChild(rule)
        if bool((sres or {}).get("success")) and any(
            str(row.get("symbol_type") or "") == "waypoint"
            for row in point_rows
        ):
            point_layers = QgsProject.instance().mapLayersByName(points_layer_name)
            if point_layers:
                point_layer = point_layers[0]
                label_settings = QgsPalLayerSettings()
                label_settings.fieldName = "name"
                label_settings.priority = 10
                label_settings.setFormat(label_format("point"))
                point_layer.setLabeling(QgsVectorLayerSimpleLabeling(label_settings))
                point_layer.setLabelsEnabled(True)

                # 路线插件可能在点层之后新建箭头面层，须让小圆点压在箭头上。
                root = QgsProject.instance().layerTreeRoot()
                node = root.findLayer(point_layer.id())
                if node is not None:
                    node.setItemVisibilityChecked(True)
                    if node.parent() == root and root.children()[0] != node:
                        root.insertChildNode(0, node.clone())
                        root.removeChildNode(node)
                    if root.hasCustomLayerOrder():
                        layer_order = list(root.customLayerOrder() or [])
                        root.setCustomLayerOrder(
                            [point_layer]
                            + [layer for layer in layer_order if layer.id() != point_layer.id()]
                        )
                point_layer.triggerRepaint()
                results["symbols"]["data"]["route_point_labels_enabled"] = True
        if bool((sres or {}).get("success")):
            point_layers = QgsProject.instance().mapLayersByName(points_layer_name)
            if point_layers:
                try:
                    results["meeting_labels"] = _label_rendezvous_points(point_layers[0], str(params.get("rendezvous_label_rule") or "汇合点标注"))
                except Exception as exc:
                    results["meeting_labels"] = {"count": 0, "warnings": [f"汇合点标注未完成：{exc}"]}
        point_layers = QgsProject.instance().mapLayersByName(points_layer_name)
        layer = point_layers[0] if point_layers else None
        data = dict(results, dataset=resolved.to_dict(), layers=[{"name": layer.name(), "id": layer.id()}] if layer else [])
        return {"success": bool(sres.get("success")), "output": points_layer_name,
                "info": sres.get("info", ""), "data": data,
                **({"resource": handle_summary(params["symbols"])} if params.get("symbols") else {})}
    except DatasetHandoffError as exc:
        return dataset_failure(exc)
    except Exception as exc:
        return {"success": False, "output": "", "info": str(exc), "message": str(exc), "error_type": getattr(exc, "code", "route_points_failed")}


route_points_build_handler = _wrap_main_thread(route_points_build_handler)
