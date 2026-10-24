#!/usr/bin/env python3
"""Run fixed inputs through the existing handlers in a disposable QGIS process."""
from __future__ import annotations
from autocarto.tests.support.resource_handles import load_handle, resolve_handle

import argparse
import csv
import hashlib
import json
import os
from pathlib import Path
import sys
from types import SimpleNamespace

if __package__:
    from .qgis_bootstrap import GoldenUnavailable, isolated_qgis, repository_root
else:
    from qgis_bootstrap import GoldenUnavailable, isolated_qgis, repository_root

HERE = Path(__file__).resolve().parent
FIXTURES = HERE / "fixtures"
NEW_DRIVER_READY = True  # T44：新排版已启用；数据、样式及路线绘制仍由对应后续卡替换。


def checked(handler, **params):
    result = handler(params)
    if not isinstance(result, dict) or result.get("success") is not True:
        raise RuntimeError(f"{handler.__name__}: {json.dumps(result, ensure_ascii=False, default=str)}")
    return result


def fixture_hashes():
    return {str(path.relative_to(FIXTURES)): hashlib.sha256(path.read_bytes()).hexdigest()
            for path in sorted(FIXTURES.rglob("*")) if path.is_file()}


def external_route_inputs():
    paths = []
    for variable in ("AUTOCARTO_GOLDEN_BASEMAP_DIR", "AUTOCARTO_ROUTE_RENDERER_DIR"):
        value = os.environ.get(variable, "").strip()
        if not value or not Path(value).is_dir():
            raise GoldenUnavailable(f"路线样例缺少有效环境变量 {variable}")
        paths.append(Path(value).resolve())
    if not (paths[1] / "services" / "combined_plot_service.py").is_file():
        raise GoldenUnavailable("路线渲染插件缺少 services/combined_plot_service.py")
    return tuple(paths)


def _layout_handlers():
    from autocarto.tools.system._shared import print_layout_impl as layout
    from autocarto.tools.system._shared import print_layout_items_impl as items
    from autocarto.tools.system._shared import print_layout_advanced_impl as advanced
    return layout, items, advanced


def _text(layout_name, item_id, text, size_mm, width=None):
    _, items, advanced = _layout_handlers()
    checked(items.add_label_handler, layout=layout_name, item_id=item_id, text=text)
    params = dict(layout=layout_name, item_id=item_id, font_size=size_mm,
                  font_family="Noto Sans CJK SC", font_color="#202020")
    if width is not None:
        params["width"] = width
    checked(advanced.set_label_style_handler, **params)


def _place(layout_name, item_id, x, y):
    _, items, _ = _layout_handlers()
    checked(items.move_item_handler, layout=layout_name, item_id=item_id, x=x, y=y)


def _rect(layout_name, item_id):
    _, _, advanced = _layout_handlers()
    return checked(advanced.get_item_properties_handler, layout=layout_name, item_id=item_id)["data"]["rect_mm"]


def run_legacy_statistical(output_dir, canvas, *, compose=False):
    from qgis.core import QgsCoordinateReferenceSystem, QgsProject
    from autocarto.tools.system._shared import data_loading_impl as data
    from autocarto.tools.system._shared import statistical_data_impl as preparation
    from autocarto.tools.system._shared import style_basic_impl as basic, style_thematic_impl as thematic
    layout, items, advanced = _layout_handlers()
    fixture = FIXTURES / "statistical"
    # The deliberately unmatched name is first reported without writing, then
    # explicitly allowed. Missing values remain null and zero remains valid.
    params = dict(geojson_path=str(fixture / "regions.geojson"), csv_path=str(fixture / "values.csv"),
                  year=2020, output_dir=str(output_dir / "derived"), validate_only=True)
    refused = preparation.prepare_statistical_handler(params)
    if refused.get("success") or refused.get("data", {}).get("unmatched_csv_regions") != ["未匹配示例区"]:
        raise AssertionError(f"未匹配地区没有被准确报告: {refused}")
    prepared = checked(preparation.prepare_statistical_handler, **{**params, "validate_only": False, "allow_unmatched": True})["data"]
    if prepared["counts"]["missing_count"] != 1 or prepared["counts"]["valid_count"] != 16:
        raise AssertionError(prepared)
    (output_dir / "data_validation.json").write_text(json.dumps(prepared, ensure_ascii=False, indent=2), encoding="utf-8")
    derived = prepared["paths"]["polygons_geojson"]
    checked(data.load_vector_handler, path=derived, layer_name="统计值")
    checked(data.load_vector_handler, path=derived, layer_name="缺失值底色", reuse_existing=False)
    checked(basic.set_layer_style_handler, layer="缺失值底色", style_type="simple", color="#e5e7eb")
    checked(thematic.graduated_style_handler, layer="统计值", field="stat_value", classes=5, method="quantile", color_ramp="Blues")
    project = QgsProject.instance()
    project.setCrs(QgsCoordinateReferenceSystem("EPSG:3857"))
    canvas.setDestinationCrs(project.crs())
    name = "统计黄金样例"
    invariants = {"unmatched_csv_regions": prepared["unmatched_csv_regions"], "missing_count": prepared["counts"]["missing_count"], "valid_count": prepared["counts"]["valid_count"]}
    if compose:
        from autocarto.tools.system._shared.layout_compose_impl import compose_layout_handler
        result = checked(compose_layout_handler, layout=name, spec=load_handle("statistical"), paper="A4",
                         title="湖北省 2020 年合成指标", map_layers=["统计值", "缺失值底色"], extent_layers=["统计值"],
                         legend_layers=["统计值"], legend_title="合成指标（测试单位）",
                         overview_text="来源：合成回归夹具；方法：五级分位数；灰色表示缺失，零为有效数值。")
        (output_dir/"compose_result.json").write_text(json.dumps(result["data"], ensure_ascii=False, indent=2), encoding="utf-8")
        return project.layoutManager().layoutByName(name), invariants
    checked(layout.create_layout_handler, name=name, page_width=297, page_height=210)
    checked(items.add_map_item_handler, layout=name, item_id="main_map")
    _place(name, "main_map", 12, 32)
    checked(items.resize_item_handler, layout=name, item_id="main_map", width=205, height=155)
    checked(advanced.set_map_layers_handler, layout=name, item_id="main_map", layers=["统计值", "缺失值底色"])
    checked(items.set_map_extent_handler, layout=name, item_id="main_map", layer="统计值", padding=0.03)
    checked(items.add_legend_handler, layout=name, map_id="main_map", item_id="legend", title="合成指标（测试单位）",
            x=223, y=55, width=60, height=70, auto_update_model=False, resize_to_contents=True)
    checked(advanced.set_legend_style_handler, layout=name, item_id="legend", legend_layers=["统计值"], title_font_size=3.5, body_font_size=2.5)
    _text(name, "title", "湖北省 2020 年合成指标", 5.5)
    _place(name, "title", 12, 8)
    _text(name, "overview", "来源：合成回归夹具；方法：五级分位数；灰色表示缺失，零为有效数值。", 2.5, 275)
    _place(name, "overview", 12, 194)
    checked(items.add_scalebar_handler, layout=name, map_id="main_map", item_id="scalebar", x=145, y=173, width=50)
    checked(advanced.set_scalebar_style_handler, layout=name, item_id="scalebar", font_family="Noto Sans CJK SC", font_size=2)
    checked(items.add_north_arrow_handler, layout=name, item_id="north_arrow", x=194, y=36, width=15, height=22)
    return project.layoutManager().layoutByName(name), invariants


def run_legacy_route(output_dir, canvas, basemap, renderer, *, compose=False):
    from qgis.core import QgsCoordinateReferenceSystem, QgsProject
    from autocarto.tools.system._shared import data_loading_impl as data
    from autocarto.tools.system._shared import basemap_presets_impl as style
    layout, items, advanced = _layout_handlers()
    specifications = [
        ("省域.shp", "省域", "fill", {"layer_type": "省", "label_field": "NAME"}),
        ("河流面.shp", "河流面", "fill", {"layer_type": "河流"}),
        ("河流线.shp", "河流线", "line", {"line_type": "河流", "label_field": "NAME"}),
        ("公路.shp", "公路", "line", {"line_type": "高速公路"}),
        ("国道.shp", "国道", "line", {"line_type": "国道"}),
        ("铁路.shp", "铁路", "line", {"line_type": "铁路"}),
        ("地区界线.shp", "地区界线", "line", {"line_type": "省界"}),
        ("县级城市驻地.shp", "县级城市驻地", "marker", {"layer_type": "county", "label_field": "name"}),
    ]
    dispatch = {"fill": style.set_fill_symbol_handler, "line": style.set_line_symbol_handler, "marker": style.set_marker_symbol_handler}
    for filename, layer_name, kind, params in specifications:
        path = basemap / filename
        if not path.is_file():
            raise GoldenUnavailable(f"底图缺少 {filename}")
        checked(data.load_vector_handler, path=str(path), layer_name=layer_name)
        if not compose:
            checked(dispatch[kind], layer=layer_name, **params)
    if compose:
        from autocarto.tools.system._shared.style_set_impl import apply_style_set_handler
        checked(apply_style_set_handler, style_set=load_handle("gb-basemap"),
                layers=[spec[1] for spec in specifications], strict=True)
    project = QgsProject.instance()
    project.setCrs(project.mapLayersByName("省域")[0].crs())
    canvas.setDestinationCrs(project.crs())
    name = "路线黄金样例"
    if compose:
        # 独立路线、点、排版和图文标注入口的真实回放。
        from autocarto.tools.system._shared.layout_compose_impl import compose_layout_handler
        extent = items._get_project_combined_extent()
        from autocarto.tools.system._shared.route_drawing_impl import route_draw_handler
        from autocarto.tools.system._shared.route_points_impl import route_points_build_handler
        from autocarto.tools.system._shared.callouts_impl import layout_add_callouts_handler
        drawn = checked(route_draw_handler, route_style=load_handle("red-blue"), dataset_source="explicit", dataset_dir=str(FIXTURES/"route_dataset"),
                        renderer_plugin_dir=str(renderer), label_layer_name="国军路线名称", strict=True)
        points = checked(route_points_build_handler, symbols=load_handle("military"), dataset_source="explicit", dataset_dir=str(FIXTURES/"route_dataset"),
                         points_layer="points", default_symbol_types=[], rendezvous_label_rule="会师军队名称", strict=True)
        if drawn["data"]["routes"]["failed_count"]:
            raise AssertionError(drawn)
        base_names = [spec[1] for spec in reversed(specifications)]
        extras = sorted(layer.name() for layer in project.mapLayers().values() if layer.name() not in base_names)
        result = checked(compose_layout_handler, layout=name, spec=load_handle("four-corner"), paper="A1",
                         title="固定路线数据回归样例", overview_text="固定六表包含路线、专题点和两条图文注记；本图用于检查旧工具的版面与样式输出。",
                         map_layers=extras+base_names,
                         extent={"xmin": extent.xMinimum(), "ymin": extent.yMinimum(), "xmax": extent.xMaximum(), "ymax": extent.yMaximum(), "crs": project.crs().authid()}, padding=0,
                         legend_layers=["points", "河流线", "铁路", "省域"],
                         legend_symbols=load_handle("route-arrow"), legend_extra_items=[{"label": "军标箭头", "symbol_type": "route_arrow", "size_mm": 7}],
                         elements={"grid": False})
        annotations = checked(layout_add_callouts_handler, dataset_source="explicit", dataset_dir=str(FIXTURES/"route_dataset"),
                              layout=name, map_item_id="main_map", avoid_item_ids=[])
        if annotations["data"]["failed_count"]:
            raise AssertionError(annotations)
        (output_dir/"compose_result.json").write_text(json.dumps(result["data"], ensure_ascii=False, indent=2), encoding="utf-8")
        return project.layoutManager().layoutByName(name), {"route_count": drawn["data"]["routes"]["count"], "point_count": points["data"]["points_layer"]["created_count"], "callout_count": annotations["data"]["created_count"]}
    checked(layout.create_layout_handler, name=name, page_width=841, page_height=594)
    checked(items.add_map_item_handler, layout=name, item_id="main_map")
    from autocarto.tools.system._shared.route_drawing_impl import route_draw_handler
    from autocarto.tools.system._shared.route_points_impl import route_points_build_handler
    from autocarto.tools.system._shared.callouts_impl import layout_add_callouts_handler
    drawn = checked(route_draw_handler, route_style=load_handle("red-blue"), dataset_source="explicit", dataset_dir=str(FIXTURES/"route_dataset"),
                    renderer_plugin_dir=str(renderer), label_layer_name="国军路线名称", strict=True)
    points = checked(route_points_build_handler, symbols=load_handle("military"), dataset_source="explicit", dataset_dir=str(FIXTURES/"route_dataset"),
                     points_layer="points", default_symbol_types=[], rendezvous_label_rule="会师军队名称", strict=True)
    annotations = checked(layout_add_callouts_handler, dataset_source="explicit", dataset_dir=str(FIXTURES/"route_dataset"),
                          layout=name, map_item_id="main_map", avoid_item_ids=[])
    if drawn["data"]["routes"]["failed_count"] or annotations["data"]["failed_count"]:
        raise AssertionError((drawn, annotations))
    real_layout = project.layoutManager().layoutByName(name)
    # Explicit project draw order avoids QGIS's randomly generated layer IDs.
    base_names = [spec[1] for spec in reversed(specifications)]
    extras = sorted([layer.name() for layer in project.mapLayers().values() if layer.name() not in base_names])
    names = extras + base_names
    checked(advanced.set_map_layers_handler, layout=name, item_id="main_map", layers=names)
    checked(items.add_decorative_border_handler, layout=name, item_id="border")
    main = _rect(name, "main_map")
    # The skill requires the actual outside edge to expand exactly 5 mm.
    # Existing border symbol expands beyond its shape rectangle; measure it.
    border_properties = checked(advanced.get_item_properties_handler, layout=name, item_id="border")["data"]
    outer = border_properties.get("outer_rect_mm", border_properties["rect_mm"])
    raw = border_properties["rect_mm"]
    bleed = (outer["width"] - raw["width"]) / 2
    _place(name, "border", main["x"] - 5 + bleed, main["y"] - 5 + bleed)
    checked(items.resize_item_handler, layout=name, item_id="border", width=main["width"] + 10 - 2 * bleed, height=main["height"] + 10 - 2 * bleed)
    _text(name, "title", "固定路线数据回归样例", 11.5)
    _text(name, "overview", "固定六表包含路线、专题点和两条图文注记；本图用于检查旧工具的版面与样式输出。", 2.5, main["width"] / 8)
    checked(advanced.set_label_style_handler, layout=name, item_id="overview", background_color="#ffffff", frame_enabled=True, frame_width=0.3)
    checked(items.add_legend_handler, layout=name, map_id="main_map", item_id="legend", title="图例", x=main["x"], y=main["y"], width=55, height=50,
            auto_update_model=False, resize_to_contents=True)
    checked(advanced.set_legend_style_handler, layout=name, item_id="legend", legend_layers=["points", "河流线", "铁路", "省域"],
            legend_symbols=load_handle("route-arrow"), legend_extra_items=[{"label": "军标箭头", "symbol_type": "route_arrow", "size_mm": 7}],
            title_font_size=3.5, body_font_size=2.5)
    checked(items.add_scalebar_handler, layout=name, map_id="main_map", item_id="scalebar", width=80)
    checked(advanced.set_scalebar_style_handler, layout=name, item_id="scalebar", font_family="Noto Sans CJK SC", font_size=2)
    checked(items.add_north_arrow_handler, layout=name, item_id="north_arrow", width=20, height=30)
    checked(advanced.set_north_arrow_style_handler, layout=name, item_id="north_arrow", style=2)
    border = checked(advanced.get_item_properties_handler, layout=name, item_id="border")["data"]["outer_rect_mm"]
    title, overview, legend = [_rect(name, item_id) for item_id in ("title", "overview", "legend")]
    _place(name, "title", border["x"] + (border["width"] - title["width"]) / 2, border["y"] - title["height"])
    _place(name, "overview", main["x"] + main["width"] - overview["width"], main["y"])
    _place(name, "legend", main["x"], main["y"] + main["height"] - legend["height"])
    _place(name, "north_arrow", main["x"], main["y"])
    for alignment in ("right", "bottom"):
        checked(items.align_items_handler, layout=name, item_ids=["scalebar"], reference_item_id="main_map", alignment=alignment)
    return real_layout, {"route_count": drawn["data"]["routes"]["count"], "point_count": points["data"]["points_layer"]["created_count"],
                         "callout_count": annotations["data"]["created_count"]}


def snapshot(layout, scenario, driver, invariants, output_dir, replacements):
    from qgis.PyQt.QtCore import Qt
    from qgis.core import Qgis, QgsLayoutExporter, QgsLayoutItemLabel, QgsLayoutItemLegend, QgsLayoutItemMap, QgsLayoutItemScaleBar, QgsLegendStyle, QgsProject, QgsTextFormat
    _, _, advanced = _layout_handlers()

    def portable(text):
        for path, token in sorted(replacements.items(), key=lambda pair: len(pair[0]), reverse=True):
            text = text.replace(path, token)
        return text

    def text_size(fmt):
        return {"size": round(float(fmt.size()), 6), "unit": int(fmt.sizeUnit())}

    elements = {}
    for item in layout.items():
        item_id = item.id() if hasattr(item, "id") else ""
        if not item_id:
            continue
        if item_id in elements:
            raise AssertionError(f"重复版面元素 ID: {item_id}")
        properties = advanced._item_properties(item)
        entry = {"type": type(item).__name__, "rect_mm": {key: round(float(value), 6) for key, value in properties["rect_mm"].items()}}
        if isinstance(item, QgsLayoutItemLabel):
            entry["font"] = text_size(item.textFormat())
            entry["text"] = item.text()
        elif isinstance(item, QgsLayoutItemScaleBar):
            entry["font"] = text_size(item.textFormat())
            entry["segments"] = item.numberOfSegments()
            entry["units_per_segment"] = round(item.unitsPerSegment(), 6)
        elif isinstance(item, QgsLayoutItemLegend):
            # textFormat() is a borrowed reference owned by the returned style.
            # Keep that SIP wrapper alive until the format has been copied.
            entry["fonts"] = {}
            for font_role, kind in (("title", QgsLegendStyle.Title), ("body", QgsLegendStyle.SymbolLabel)):
                legend_style = item.style(kind)
                copied_format = QgsTextFormat(legend_style.textFormat())
                entry["fonts"][font_role] = text_size(copied_format)
            entry["entries"] = [{"layer": node.layer().name(), "labels": [str(label.data(Qt.DisplayRole) or "") for label in item.model().layerLegendNodes(node)]}
                                for node in item.model().rootGroup().findLayers()]
        if "outer_rect_mm" in properties:
            entry["outer_rect_mm"] = {key: round(float(value), 6) for key, value in properties["outer_rect_mm"].items()}
        elements[item_id] = entry
    map_item = layout.itemById("main_map")
    extent = map_item.extent()
    layers = {}
    for layer in QgsProject.instance().mapLayers().values():
        if not hasattr(layer, "renderer"):
            continue
        renderer = layer.renderer()
        dump = portable(renderer.dump()) if renderer is not None else ""
        labeling = layer.labeling() if hasattr(layer, "labeling") else None
        label_field = labeling.settings().fieldName if labeling is not None and hasattr(labeling, "settings") else ""
        layers[layer.name()] = {"renderer_type": renderer.type() if renderer is not None else "", "renderer_sha256": hashlib.sha256(dump.encode()).hexdigest(),
                                "renderer_dump": dump, "feature_count": int(layer.featureCount()), "labels_enabled": bool(layer.labelsEnabled()),
                                "label_field": label_field, "labeling_type": labeling.type() if labeling else None}
    page = layout.pageCollection().page(0).pageSize()
    payload = {"schema_version": "autocarto-golden/v1", "scenario": scenario, "driver": driver,
               "page_size_mm": {"width": page.width(), "height": page.height()}, "elements": elements,
               "main_map": {"layers": [layer.name() for layer in map_item.layers()], "crs": map_item.crs().authid(),
                            "extent": dict(zip(("xmin", "ymin", "xmax", "ymax"), [extent.xMinimum(), extent.yMinimum(), extent.xMaximum(), extent.yMaximum()]))},
               "layers": layers, "invariants": invariants,
               "metadata": {"qgis_version": Qgis.QGIS_VERSION, "fixture_sha256": fixture_hashes(), "preview_dpi": 72,
                            "external_inputs": {token: "provided by environment; never copied" for token in replacements.values() if token.startswith("<external:")}}}
    settings = QgsLayoutExporter.ImageExportSettings()
    settings.dpi = 72
    result = QgsLayoutExporter(layout).exportToImage(str(output_dir / "preview.png"), settings)
    if result != QgsLayoutExporter.Success:
        raise RuntimeError(f"预览导出失败: {result}")
    (output_dir / "snapshot.json").write_text(json.dumps(payload, ensure_ascii=False, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return payload


def validate_composed_title(layout, scenario, output_dir):
    """旧基线漏了标题加粗；独立校验新标题字重和真实几何锚点。"""
    _, _, advanced = _layout_handlers()
    title = layout.itemById("title")
    actual = advanced._item_properties(title)["rect_mm"]
    if scenario == "route":
        border = advanced._item_properties(layout.itemById("border"))["outer_rect_mm"]
        error = max(abs(actual["x"]+actual["width"]/2-border["x"]-border["width"]/2), abs(actual["y"]+actual["height"]-border["y"]))
    else:
        error = max(abs(actual["x"]-12), abs(actual["y"]-8))
    result = {"title_bold": title.textFormat().font().bold(), "title_anchor_error_mm": error,
              "title_font_locked": bool(title.customProperty("autocarto/font_locked", False)),
              "title_anchor": json.loads(title.customProperty("autocarto/anchor", "{}"))}
    if not result["title_bold"] or not result["title_font_locked"] or error > 1:
        raise AssertionError(result)
    (output_dir/"compose_validation.json").write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding="utf-8")


def main(argv=None):
    parser = argparse.ArgumentParser(description="运行固定输入并生成真实 QGIS 黄金样例快照")
    parser.add_argument("--scenario", choices=("route", "statistical"), required=True)
    parser.add_argument("--driver", choices=("legacy", "new"), default="legacy")
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args(argv)
    try:
        external = external_route_inputs() if args.scenario == "route" else None
        output_dir = args.out.expanduser().resolve()
        if output_dir == FIXTURES or FIXTURES in output_dir.parents:
            raise ValueError("输出目录不能覆盖固定输入夹具")
        if (output_dir / "snapshot.json").exists():
            raise FileExistsError("输出目录已有 snapshot.json，请使用新目录以免覆盖基线")
        output_dir.mkdir(parents=True, exist_ok=True)
        before = fixture_hashes()
        with isolated_qgis(output_dir) as (_, canvas):
            from autocarto.tools.system._shared import output_manager
            output_manager.set_session_context(SimpleNamespace(session_id="golden", session_dir=str(output_dir), session_subdirs={}, project_path=""))
            if args.scenario == "route":
                real_layout, invariants = run_legacy_route(output_dir, canvas, *external, compose=args.driver == "new")
            else:
                real_layout, invariants = run_legacy_statistical(output_dir, canvas, compose=args.driver == "new")
            replacements = {str(FIXTURES): "<fixtures>", str(output_dir): "<run>", str(repository_root()): "<repository>"}
            if external:
                replacements.update({str(external[0]): "<external:basemap>", str(external[1]): "<external:renderer>"})
            result = snapshot(real_layout, args.scenario, args.driver, invariants, output_dir, replacements)
            if args.driver == "new":
                validate_composed_title(real_layout, args.scenario, output_dir)
            if before != fixture_hashes():
                raise AssertionError("固定输入在回归运行中被修改")
            # Explicitly drop SIP wrappers before QApplication is destroyed.
            real_layout = None
        print(json.dumps({"success": True, "scenario": args.scenario, "elements": len(result["elements"]), "output_dir": str(output_dir)}, ensure_ascii=False))
        return 0
    except GoldenUnavailable as exc:
        print(json.dumps({"skipped": True, "reason": str(exc)}, ensure_ascii=False))
        return 77
    except Exception as exc:
        print(json.dumps({"success": False, "error": str(exc)}, ensure_ascii=False), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
