from autocarto.tests.support.resource_handles import load_handle, resolve_handle
import importlib
import os
import sys
import tempfile
import types
import unittest
from unittest.mock import patch
from pathlib import Path

class _FakeDocument:
    def __init__(self):
        self.text = ""

    def setContent(self, text):
        self.text = text
        if "<Layout" not in text:
            return False, "expected Layout root", 1, 1
        return True, "", 0, 0

    def documentElement(self):
        return types.SimpleNamespace(tagName=lambda: "Layout")

    def elementsByTagName(self, _tag):
        return types.SimpleNamespace(count=lambda: 0)


class _FakeRect:
    def x(self): return 10.0
    def y(self): return 20.0
    def width(self): return 30.0
    def height(self): return 40.0


class _FakeItem:
    def __init__(self, item_id):
        self._item_id = item_id

    def id(self):
        return self._item_id

    def setId(self, value):
        self._item_id = value

    def uuid(self):
        return "fake_uuid"

    def rect(self):
        return _FakeRect()

    def mapRectToScene(self, rect):
        return rect


class _FakeLabel(_FakeItem):
    ModeHtml = 1

    def __init__(self, item_id, text="{{标题}} / {{综述}} / {{数据来源}}"):
        super().__init__(item_id)
        self._text = text

    def text(self):
        return self._text

    def setText(self, text):
        self._text = text

    def mode(self):
        return 0


class _FakeMap(_FakeItem):
    pass


class _FakeLegend(_FakeItem):
    pass


class _FakeReadWriteContext:
    def setPathResolver(self, _resolver):
        pass


class _FakePageCollection:
    def __init__(self, page_count=0):
        self._page_count = page_count

    def pageCount(self):
        return self._page_count


class _FakeLayout:
    def __init__(self, project):
        self.project = project
        self._name = ""
        self._pages = _FakePageCollection()
        self._items = []
        self.load_count = 0

    def initializeDefaults(self):
        self._pages = _FakePageCollection(1)

    def loadFromTemplate(self, document, _context, clearExisting=True):
        self.load_count += 1
        if "LOAD_ERROR" in document.text:
            raise RuntimeError("template load failed")
        if clearExisting:
            self._items = []
        self._pages = _FakePageCollection(1)
        self._items.extend([_FakeMap("main_map"), _FakeLabel("title")])
        return list(self._items)

    def setName(self, name):
        self._name = name

    def name(self):
        return self._name

    def pageCollection(self):
        return self._pages

    def items(self):
        return list(self._items)

    def itemById(self, item_id):
        return next((item for item in self._items if item.id() == item_id), None)

    def convertFromLayoutUnits(self, value, _unit):
        return types.SimpleNamespace(length=lambda: value)


class _FakeLayoutManager:
    def __init__(self):
        self.layouts = []

    def layoutByName(self, name):
        return next((layout for layout in self.layouts if layout.name() == name), None)

    def addLayout(self, layout):
        if self.layoutByName(layout.name()) is not None:
            return False
        self.layouts.append(layout)
        return True

    def removeLayout(self, layout):
        self.layouts.remove(layout)


class _FakeProjectInstance:
    def __init__(self):
        self._manager = _FakeLayoutManager()

    def layoutManager(self):
        return self._manager


class _FakeQgsProject:
    current = _FakeProjectInstance()

    @classmethod
    def instance(cls):
        return cls.current


class _FakeUnitTypes:
    LayoutMillimeters = 0
    LayoutCentimeters = 1
    LayoutInches = 2
    LayoutPixels = 3


def _fake_modules():
    qgis = types.ModuleType("qgis")
    pyqt = types.ModuleType("qgis.PyQt")
    qtxml = types.ModuleType("qgis.PyQt.QtXml")
    core = types.ModuleType("qgis.core")
    runtime = types.ModuleType("autocarto.tools.system._shared.layout_runtime")

    qtxml.QDomDocument = _FakeDocument
    core.QgsLayoutSize = object
    core.QgsLayoutItem = _FakeItem
    core.QgsLayoutItemLabel = _FakeLabel
    core.QgsLayoutItemMap = _FakeMap
    core.QgsLayoutItemLegend = _FakeLegend
    core.QgsLayoutItemRegistry = types.SimpleNamespace(LayoutLabel=65641)
    core.QgsPrintLayout = _FakeLayout
    core.QgsProject = _FakeQgsProject
    core.QgsReadWriteContext = _FakeReadWriteContext
    core.QgsPathResolver = lambda value: value
    core.QgsUnitTypes = _FakeUnitTypes
    runtime.run_in_main_thread = lambda fn: fn()

    qgis.PyQt = pyqt
    qgis.core = core
    pyqt.QtXml = qtxml

    return {
        "qgis": qgis,
        "qgis.PyQt": pyqt,
        "qgis.PyQt.QtXml": qtxml,
        "qgis.core": core,
        "autocarto.tools.system._shared.layout_runtime": runtime,
    }


class ImportLayoutTemplateHandlerTestCase(unittest.TestCase):
    module_name = "autocarto.tools.system._shared.print_layout_impl"

    def setUp(self):
        self.temp_dir = tempfile.TemporaryDirectory()
        self.saved_modules = {
            name: sys.modules.get(name)
            for name in (*_fake_modules().keys(), self.module_name)
        }
        sys.modules.update(_fake_modules())
        sys.modules.pop(self.module_name, None)
        self.impl = importlib.import_module(self.module_name)
        _FakeQgsProject.current = _FakeProjectInstance()

    def tearDown(self):
        sys.modules.pop(self.module_name, None)
        for name, module in self.saved_modules.items():
            if module is None:
                sys.modules.pop(name, None)
            else:
                sys.modules[name] = module
        self.temp_dir.cleanup()

    def _write_template(self, content="<Layout name='template'><LayoutItem id='main_map'/></Layout>"):
        path = Path(self.temp_dir.name) / "template.qpt"
        path.write_text(content, encoding="utf-8")
        return path

    def test_imports_template_as_new_layout(self):
        path = self._write_template()

        result = self.impl.import_layout_template_handler(
            {"template": resolve_handle(path, "layout"), "name": "模板测试"}
        )

        self.assertTrue(result["success"])
        self.assertEqual(result["output"], "模板测试")
        self.assertFalse(result["data"]["replaced"])
        self.assertEqual(result["data"]["page_count"], 1)
        self.assertEqual(result["data"]["item_ids"], ["main_map", "title"])
        self.assertEqual(result["data"]["rects_mm"]["title"], {"x": 10, "y": 20, "width": 30, "height": 40})
        self.assertEqual(result["data"]["unreplaced_placeholders"], ["数据来源", "标题", "综述"])
        self.assertIn("{{数据来源}}", result["data"]["warnings"][0])
        manager = _FakeQgsProject.current.layoutManager()
        self.assertIsNotNone(manager.layoutByName("模板测试"))

    def test_overwrites_existing_layout_after_candidate_validation(self):
        path = self._write_template()
        manager = _FakeQgsProject.current.layoutManager()
        existing = _FakeLayout(_FakeQgsProject.current)
        existing.initializeDefaults()
        existing.setName("已有布局")
        existing._items = [_FakeItem("old_item")]
        manager.addLayout(existing)

        result = self.impl.import_layout_template_handler(
            {"template": resolve_handle(path, "layout"), "name": "已有布局"}
        )

        self.assertTrue(result["success"])
        self.assertTrue(result["data"]["replaced"])
        self.assertIsNot(manager.layoutByName("已有布局"), existing)
        self.assertEqual(result["data"]["item_ids"], ["main_map", "title"])
        self.assertEqual(existing.load_count, 0)
        self.assertEqual(len(manager.layouts), 1)

    def test_invalid_xml_does_not_create_or_replace_layout(self):
        path = self._write_template()
        handle = resolve_handle(path, "layout")
        path.write_text("<broken>")

        result = self.impl.import_layout_template_handler(
            {"template": handle, "name": "无效模板"}
        )

        self.assertFalse(result["success"])
        self.assertIn("解析模板文件失败", result["info"])
        manager = _FakeQgsProject.current.layoutManager()
        self.assertIsNone(manager.layoutByName("无效模板"))

    def test_relative_template_uses_project_root_and_checks_extension(self):
        self._write_template()
        previous = os.getcwd()
        try:
            os.chdir(Path(self.temp_dir.name).parent)
            with patch.dict(os.environ, {"AUTOCARTO_PROJECT_ROOT": self.temp_dir.name}):
                relative = self.impl.import_layout_template_handler({"template": resolve_handle(Path(self.temp_dir.name)/"template.qpt", "layout"), "name": "相对路径"})
        finally:
            os.chdir(previous)
        wrong_extension = self.impl.import_layout_template_handler(
            {
                "template": load_handle("four-corner"),
                "name": "错误格式",
            }
        )

        self.assertTrue(relative["success"], relative)
        self.assertEqual(relative["data"]["template"], str((Path(self.temp_dir.name)/"template.qpt").resolve()))
        self.assertFalse(wrong_extension["success"])
        self.assertIn("qpt", wrong_extension["info"])

    def test_text_replacements_are_literal_and_report_remaining_placeholders(self):
        result = self.impl.import_layout_template_handler({"template": resolve_handle(self._write_template(), "layout"), "name": "文字测试",
                                                         "texts": {"标题": "A < B & C", "综述": "真实综述\n第二行"}})
        self.assertTrue(result["success"], result)
        layout = _FakeQgsProject.current.layoutManager().layoutByName("文字测试")
        self.assertEqual(layout.itemById("title").text(), "A < B & C / 真实综述\n第二行 / {{数据来源}}")
        self.assertEqual(result["data"]["unreplaced_placeholders"], ["数据来源"])
        self.assertEqual(self.impl._replace_template_text("{{ 标题 }} / {{综述}}", {"标题": "{{综述}}", "综述": "后续"}), "{{综述}} / 后续")
        self.assertEqual(self.impl._replace_template_text("<b>{{标题}}</b>", {"标题": "A < B & C\n续"}, html_mode=True), "<b>A &lt; B &amp; C<br/>续</b>")

    def test_invalid_settings_preserve_existing_layout(self):
        path = self._write_template()
        manager = _FakeQgsProject.current.layoutManager()
        existing = _FakeLayout(_FakeQgsProject.current)
        existing.setName("保留布局")
        manager.addLayout(existing)
        for settings in ({"texts": []}, {"texts": {"标题": 7}}, {"texts": {"": "空"}},
                         {"map_item_id": "不存在"}, {"map_item_id": ""}):
            result = self.impl.import_layout_template_handler({"template": resolve_handle(path, "layout"), "name": "保留布局", **settings})
            self.assertFalse(result["success"], result)
            self.assertIs(manager.layoutByName("保留布局"), existing)
            self.assertEqual(existing.load_count, 0)


class ImportLayoutTemplateDefinitionTestCase(unittest.TestCase):
    def test_tool_definition_and_skill_registration_match(self):
        plugin_root = Path(__file__).resolve().parents[1]
        definition_path = (
            plugin_root
            / "tools"
            / "system"
            / "print-layout"
            / "import_layout_template.yaml"
        )
        import yaml
        definition = yaml.safe_load(definition_path.read_text(encoding="utf-8"))
        self.assertEqual(definition["metadata"]["name"], "import_layout_template")
        self.assertEqual(definition["metadata"]["runtime"], "qgis")
        self.assertEqual(definition["metadata"]["domain"], "print-layout")
        self.assertEqual(definition["execution"]["handler"], "handlers.layout.import_layout_template_handler")
        parameters = {entry["name"]: entry for entry in definition["spec"]["parameters"]}
        for name in ("template", "name", "texts", "map_item_id", "map_layers", "extent_layers", "extent", "padding", "legend_layers"):
            self.assertIn(name, parameters)
            self.assertEqual(definition["execution"]["param_mapping"][name], "{{ "+name+" }}")
        self.assertEqual(parameters["template"]["type"], "resource")
        self.assertEqual(parameters["template"]["resource_type"], "layout:qpt")
        self.assertFalse((plugin_root/"tools/system/cartography/import_layout_template.yaml").exists())
        raw = (plugin_root/"skills/system/print-layout/SKILL.md").read_text()
        self.assertIn("import_layout_template", yaml.safe_load(raw.split("---", 2)[1])["tools"])


if __name__ == "__main__":
    unittest.main()
