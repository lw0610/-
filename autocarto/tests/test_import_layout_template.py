import importlib
import sys
import tempfile
import types
import unittest
from pathlib import Path

class _FakeDocument:
    def __init__(self):
        self.text = ""

    def setContent(self, text):
        self.text = text
        if "<Layout" not in text:
            return False, "expected Layout root", 1, 1
        return True, "", 0, 0


class _FakeItem:
    def __init__(self, item_id):
        self._item_id = item_id

    def id(self):
        return self._item_id


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
        self._items.extend([_FakeItem("main_map"), _FakeItem("title")])
        return list(self._items)

    def setName(self, name):
        self._name = name

    def name(self):
        return self._name

    def pageCollection(self):
        return self._pages

    def items(self):
        return list(self._items)


class _FakeLayoutManager:
    def __init__(self):
        self.layouts = {}

    def layoutByName(self, name):
        return self.layouts.get(name)

    def addLayout(self, layout):
        if layout.name() in self.layouts:
            return False
        self.layouts[layout.name()] = layout
        return True


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
    core.QgsPrintLayout = _FakeLayout
    core.QgsProject = _FakeQgsProject
    core.QgsReadWriteContext = object
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

    def _write_template(self, content="<Layout name='template'/>"):
        path = Path(self.temp_dir.name) / "template.qpt"
        path.write_text(content, encoding="utf-8")
        return path

    def test_imports_template_as_new_layout(self):
        path = self._write_template()

        result = self.impl.import_layout_template_handler(
            {"template": str(path), "name": "模板测试"}
        )

        self.assertTrue(result["success"])
        self.assertEqual(result["output"], "模板测试")
        self.assertFalse(result["data"]["replaced"])
        self.assertEqual(result["data"]["page_count"], 1)
        self.assertEqual(result["data"]["item_ids"], ["main_map", "title"])
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
            {"template": str(path), "name": "已有布局"}
        )

        self.assertTrue(result["success"])
        self.assertTrue(result["data"]["replaced"])
        self.assertIs(manager.layoutByName("已有布局"), existing)
        self.assertEqual(result["data"]["item_ids"], ["main_map", "title"])
        self.assertEqual(existing.load_count, 1)

    def test_invalid_xml_does_not_create_or_replace_layout(self):
        path = self._write_template("<broken>")

        result = self.impl.import_layout_template_handler(
            {"template": str(path), "name": "无效模板"}
        )

        self.assertFalse(result["success"])
        self.assertIn("解析模板文件失败", result["info"])
        manager = _FakeQgsProject.current.layoutManager()
        self.assertIsNone(manager.layoutByName("无效模板"))

    def test_requires_absolute_qpt_path(self):
        relative = self.impl.import_layout_template_handler(
            {"template": "template.qpt", "name": "相对路径"}
        )
        wrong_extension = self.impl.import_layout_template_handler(
            {
                "template": str(Path(self.temp_dir.name) / "template.xml"),
                "name": "错误格式",
            }
        )

        self.assertFalse(relative["success"])
        self.assertIn("绝对路径", relative["info"])
        self.assertFalse(wrong_extension["success"])
        self.assertIn(".qpt", wrong_extension["info"])


class ImportLayoutTemplateDefinitionTestCase(unittest.TestCase):
    def test_tool_definition_and_skill_registration_match(self):
        plugin_root = Path(__file__).resolve().parents[1]
        definition_path = (
            plugin_root
            / "tools"
            / "system"
            / "cartography"
            / "import_layout_template.yaml"
        )
        definition_text = definition_path.read_text(encoding="utf-8")

        self.assertIn("name: import_layout_template", definition_text)
        self.assertIn("runtime: qgis", definition_text)
        self.assertIn(
            "handler: handlers.layout.import_layout_template_handler",
            definition_text,
        )
        self.assertIn("- name: template\n    type: string", definition_text)
        self.assertIn("- name: name\n    type: string", definition_text)

        skill_text = (
            plugin_root
            / "skills"
            / "system"
            / "cartography-styling"
            / "SKILL.md"
        ).read_text(encoding="utf-8")
        self.assertIn("- import_layout_template", skill_text)


if __name__ == "__main__":
    unittest.main()
