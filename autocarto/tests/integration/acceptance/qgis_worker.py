"""离线验收角色适配器。按资源中的动作调用真实工具，不按流程名称分支。"""
from __future__ import annotations

import contextlib
import hashlib
import importlib.util
import json
import os
import shutil
from pathlib import Path
import sys
from types import SimpleNamespace

ROOT = Path(__file__).resolve().parents[4]
sys.path.insert(0, str(ROOT))
from autocarto.tests.golden.qgis_bootstrap import isolated_qgis


class Worker:
    def __init__(self, scenario, canvas):
        from qgis.core import QgsCoordinateReferenceSystem, QgsProject, QgsVectorLayer
        from autocarto.core.tool_engine.engine import QGISToolEngine
        from autocarto.tools.system._shared import output_manager
        self.scenario, self.canvas = scenario, canvas
        self.project = QgsProject.instance()
        self.out = Path(scenario["out"])
        self.vars = dict(scenario["variables"])
        self.vars.update(inputs=scenario["inputs"])
        self.calls = []
        self.symbology_checks = []
        self.engine = QGISToolEngine(load_interactive_tools=False)
        self.engine.update_contract(execution_manifest=json.loads((self.out/"authority-manifest.json").read_text()))
        output_manager.set_session_context(SimpleNamespace(session_id=scenario["id"], session_dir=str(self.out), session_subdirs={}, project_path=""))
        initial = scenario.get("project_crs", "EPSG:3857")
        if initial.startswith("source:"):
            crs = QgsVectorLayer(initial[7:], "只读坐标系夹具", "ogr").crs()
        else:
            crs = QgsCoordinateReferenceSystem(initial)
        assert crs.isValid(), initial
        self.project.setCrs(crs)
        canvas.setDestinationCrs(crs)
        for preset in scenario.get("tool_presets", []):
            assert preset["kind"] in ("styles", "symbols") and Path(preset["name"]).name == preset["name"]
            destination = Path(os.environ["AUTOCARTO_HOME"]) / "presets/route-cartography" / preset["kind"] / (preset["name"] + ".csv")
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(preset["source"], destination)
        self.plan = None
        self.resource_parameters = {}
        self.linked_resources = []
        self.builtin_styles = {}

    def resolve(self, value):
        if isinstance(value, dict):
            if set(value) == {"$ref"}:
                current = self.vars
                for key in value["$ref"].split("."):
                    current = current[int(key)] if isinstance(current, list) else current[key]
                return current
            if set(value) == {"$json"}:
                return json.dumps(self.resolve({"$ref": value["$json"]}), ensure_ascii=False)
            return {k: self.resolve(v) for k, v in value.items()}
        if isinstance(value, list):
            return [self.resolve(v) for v in value]
        return value

    def resource_args(self, name, params):
        # Consume actual Go Library handles, including nested array parameters.
        def resolve(selected, expected):
            if isinstance(selected, dict):
                return selected
            assert isinstance(selected, str), f"验收资源应为名称：{name}"
            expected = expected.split(":")
            matches = [item["handle"] for item in self.linked_resources
                       if item["handle"] and item["handle"]["type"] == expected[0]
                       and selected in (item["key"], item["label"], item["handle"]["id"], item["handle"]["name"])]
            assert len(matches) == 1, f"本步没有唯一已链接资源：{name}={selected}"
            handle = matches[0]
            assert len(expected)==1 or handle["format"]==expected[1]
            return handle
        def nested(value, schema):
            if schema.get("x-resource-type"):
                return resolve(value, schema["x-resource-type"])
            if isinstance(value,list):
                return [nested(item,schema.get("items",{})) for item in value]
            if isinstance(value,dict):
                return {key:nested(item,schema.get("properties",{}).get(key,{})) for key,item in value.items()}
            return value
        params = dict(params)
        if name == "layer_load_builtin" and not params.get("style"):
            selected = self.builtin_styles.get(params["name"])
            if selected is not None:
                params["style"] = selected
        for parameter in self.resource_parameters.get(name, []):
            key=parameter["name"]
            selected=params.get(key) or parameter.get("default_resource")
            if selected is None:
                assert not parameter.get("required"), f"缺少资源参数：{name}.{key}"
                continue
            params[key]=resolve(selected,parameter["resource_type"]) if parameter.get("type")=="resource" else nested(selected,parameter.get("schema",{}))
        return params

    def call(self, name, params, allowed=None, allow_failure=False):
        from autocarto.core.tool_engine.engine import execute_tool
        if allowed is not None:
            assert name in allowed, f"角色没有工具权限：{name}"
        params = self.resource_args(name, params)
        if name in ("prepare_quality_review", "load_review_images"):
            # CLI 与 QGIS 在生产通过不同传输；此处调用相同 CLI 实现，避免另起解释器改变环境。
            path = ROOT/"autocarto/tools/system/quality-feedback/scripts/quality_review.py"
            spec = importlib.util.spec_from_file_location("acceptance_quality_review", path)
            module = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(module)
            result = module.prepare_review(params) if name == "prepare_quality_review" else module.load_review_images(params)
        else:
            envelope = execute_tool(name, params, engine=self.engine)
            assert envelope["success"] or allow_failure, envelope
            result = envelope["output"]
            assert allow_failure or not isinstance(result, dict) or result.get("success", True), result
        if name in ("layer_set_symbology", "layer_load_style", "layer_load_styles") and isinstance(result, dict) and result.get("success"):
            from autocarto.tools.system._shared.symbology import read_symbology, symbology_differences
            data = result["data"]
            rows = data["items"] if name != "layer_load_style" else [data]
            for row in rows:
                item = row["data"] if name == "layer_load_styles" else row
                assert not item["differences"], item
                layer = self.project.mapLayer(item["layer_id"])
                assert layer is not None, item["layer_id"]
                actual = read_symbology(layer)
                differences = symbology_differences(item["requested"], actual)
                self.symbology_checks.append({"tool": name, "layer": layer.name(), "scale": params.get("scale", 1),
                                               "requested": item["requested"], "actual": actual, "differences": differences,
                                               "warnings": item.get("warnings", [])})
                assert not differences, self.symbology_checks[-1]
        self.calls.append({"tool": name, "params": params, "result": result})
        return result

    def layers(self, names=None):
        layers = list(self.project.mapLayers().values())
        if names is not None:
            layers = [next(layer for layer in layers if layer.name() == name) for name in names]
        return [{"id": layer.id(), "name": layer.name()} for layer in layers]

    def action(self, action, allowed):
        action = self.resolve(action)
        op = action.get("op", "tool")
        if op == "tool":
            result = self.call(action["tool"], action.get("params", {}), allowed, action.get("allow_failure",False))
            if not action.get("allow_failure"):
                assert not isinstance(result, dict) or result.get("success", True), result
        elif op == "set":
            result = action["value"]
        elif op == "layers":
            result = self.layers(action.get("names"))
        elif op == "extent":
            from autocarto.tools.system._shared.print_layout_items_impl import _get_project_combined_extent
            extent = _get_project_combined_extent()
            result = dict(zip(("xmin", "ymin", "xmax", "ymax"), (extent.xMinimum(), extent.yMinimum(), extent.xMaximum(), extent.yMaximum())))
            result["crs"] = self.project.crs().authid()
        elif op == "map_order":
            base = action["base_names"]
            result = sorted(layer.name() for layer in self.project.mapLayers().values() if layer.name() not in base) + list(reversed(base))
        elif op == "review_initial":
            prepared = action["prepared"]
            # 此固定结论只验证审查结构及原图保护链，绝不声称完成多模态语义审图。
            result = {"dimension_results": {name: {"has_issue": False, "evidence": "确定性结构验收；未调用真实多模态模型"}
                      for name in ("overlap", "layout", "alignment_spacing", "visual_hierarchy", "typography", "color", "map_readability", "style_consistency")},
                      "suggestions": [], "review_manifest_path": prepared["review_manifest_path"],
                      "reserved_final_image_path": prepared["reserved_final_image_path"]}
        elif op == "review_final":
            manifest = json.loads(Path(action["manifest"]).read_text())
            protected = manifest["images"]["original"]
            assert hashlib.sha256(Path(protected["path"]).read_bytes()).hexdigest() == protected["sha256"]
            result = {"final_passed": True, "remaining_issues": [], "suggestion_results": [],
                      "scope": "确定性结构与文件保护验收；不代表真实多模态质量结论"}
        elif op == "assert_fields":
            layer = self.project.mapLayersByName(action["layer"])[0]
            result = []
            for feature in layer.getFeatures():
                actual = float(feature[action["field"]])
                expected = float(feature[action["numerator"]])/float(feature[action["denominator"]])
                assert abs(actual-expected) <= max(1e-6, abs(expected)*1e-10)
                result.append({"id": feature.id(), "actual": actual, "expected": expected})
            assert len(result) == action["count"]
        else:
            raise ValueError(f"未知验收动作：{op}")
        if "save" in action:
            self.vars[action["save"]] = result
        return result

    def handle(self, request):
        op = request["op"]
        if op == "init":
            self.plan = request["plan"]
            self.resource_parameters = request["resource_parameters"]
            self.builtin_styles = request["builtin_styles"]
            return {"ok": True}
        if op == "verify":
            return {"ok": True, "result": self.call(request["tool"], request["params"])}
        if op == "step":
            step = request["step"]
            self.linked_resources = [item for item in request["resources"] if item["key"] in step["refs"].get("resources", [])]
            recipe = self.scenario["steps"][step["id"]]
            for action in recipe["actions"]:
                self.action(action, request["role"]["tools"])
            original_outputs = self.resolve(recipe["outputs"])
            outputs = {key: original_outputs[label] for key,label in request["output_names"].items()}
            required = {item["label"] for item in step["expected_outputs"] if item["required"]}
            assert required <= set(outputs), (required, outputs)
            return {"ok": True, "receipt": {"status": "completed", "summary": "真实工具执行并交接实际产出；确定性角色适配器", "outputs": outputs, "issues": []}}
        if op == "finalize":
            from autocarto.tests.golden.run_golden import snapshot, validate_composed_title, FIXTURES
            from autocarto.tests.golden.compare_golden import compare_directories, compare_snapshots
            layout = self.project.layoutManager().layoutByName(self.scenario["layout"])
            assert layout is not None
            replacements = {str(ROOT): "<repository>", str(self.out): "<run>", str(FIXTURES): "<fixtures>"}
            for key in self.scenario.get("external_inputs", []):
                replacements[self.scenario[key]] = "<external:"+key+">"
            invariants = self.resolve(self.scenario.get("invariants", {}))
            result = snapshot(layout, self.scenario.get("golden", "acceptance"), "new", invariants, self.out, replacements)
            validate_composed_title(layout, self.scenario.get("title_geometry", self.scenario.get("golden", "statistical")), self.out)
            if "expected_map_order" in self.scenario:
                expected_order = self.resolve(self.scenario["expected_map_order"])
                assert result["main_map"]["layers"] == expected_order, result["main_map"]
            if "golden" in self.scenario:
                report = compare_directories(ROOT/"autocarto/tests/golden/baseline"/self.scenario["golden"], self.out)
                if "expected_map_order" in self.scenario:
                    # v7/05 explicitly moves boundaries below water/roads. Keep
                    # the raw v6 delta; all other structure stays under comparison.
                    report["v6_differences"] = report["differences"]
                    expected = json.loads((ROOT/"autocarto/tests/golden/baseline"/self.scenario["golden"]/"snapshot.json").read_text())
                    expected["main_map"]["layers"] = expected_order
                    report["differences"] = compare_snapshots(expected, result)
                    report["success"] = not report["differences"]
                    report["expected_map_order"] = expected_order
                (self.out/"comparison.json").write_text(json.dumps(report, ensure_ascii=False, indent=2)+"\n")
                assert report["success"], report
            if "symbology_scale" in self.scenario:
                assert len(self.symbology_checks) == 8, self.symbology_checks
                assert all(item["scale"] == self.scenario["symbology_scale"] for item in self.symbology_checks)
                (self.out/"symbology-readback.json").write_text(json.dumps(self.symbology_checks, ensure_ascii=False, indent=2)+"\n")
            project_path = str(self.out/"project.qgz")
            assert self.project.write(project_path)
            (self.out/"tool-calls.json").write_text(json.dumps(self.calls, ensure_ascii=False, indent=2, default=str)+"\n")
            (self.out/"adapter-variables.json").write_text(json.dumps(self.vars, ensure_ascii=False, indent=2, default=str)+"\n")
            return {"ok": True, "elements": len(result["elements"]), "project": project_path}
        raise ValueError(op)


def main():
    scenario = json.loads(Path(sys.argv[1]).read_text())
    protocol = sys.stdout
    with contextlib.redirect_stdout(sys.stderr):
        with isolated_qgis(Path(scenario["out"])) as (_, canvas):
            os.environ["AUTOCARTO_HOME"] = str(Path(scenario["out"])/"controller-runtime")
            worker = Worker(scenario, canvas)
            for line in sys.stdin:
                try:
                    response = worker.handle(json.loads(line))
                except Exception as exc:
                    import traceback
                    traceback.print_exc()
                    response = {"ok": False, "error": str(exc)}
                print(json.dumps(response, ensure_ascii=False, default=str), file=protocol, flush=True)


if __name__ == "__main__":
    main()
