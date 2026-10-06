import importlib
import os
import sys
import types
import unittest
import uuid


class _FakeSignal:
    def connect(self, *_args, **_kwargs):
        return None

    def emit(self, *_args, **_kwargs):
        return None


def _fake_pyqt_signal(*_args, **_kwargs):
    return _FakeSignal()


class _FakeQCoreApplication:
    @staticmethod
    def instance():
        return None


class _FakeQThread:
    @staticmethod
    def currentThread():
        return object()


class _FakeQTimer:
    def __init__(self):
        self.timeout = _FakeSignal()

    def setSingleShot(self, *_args, **_kwargs):
        return None

    def start(self, *_args, **_kwargs):
        return None

    def isActive(self):
        return False

    def stop(self):
        return None

    @staticmethod
    def singleShot(*_args, **_kwargs):
        return None


class _FakeQt:
    QueuedConnection = 0


class _FakeQRectF:
    def __init__(self, *_args, **_kwargs):
        pass


class _FakeQColor:
    def __init__(self, *_args, **_kwargs):
        pass


class _FakeQFont:
    def __init__(self, *_args, **_kwargs):
        pass


class _FakeQgsLayoutMeasurement:
    def __init__(self, *_args, **_kwargs):
        pass


class _FakeQgsUnitTypes:
    LayoutMillimeters = 0


class _FakeLayer:
    def __init__(self, name: str, source: str):
        self._name = name
        self._source = source

    def name(self):
        return self._name

    def source(self):
        return self._source


class _FakeProject:
    def __init__(self, layers=None):
        self._layers = layers or {}

    def mapLayers(self):
        return self._layers

    def fileName(self):
        return ""


class _FakeQgsProject:
    _instance = _FakeProject()

    @classmethod
    def instance(cls):
        return cls._instance


class _FakeQgsLayoutItemLabel:
    def __init__(self, *_args, **_kwargs):
        pass


def _install_fake_qgis_modules():
    fake_qgis = types.ModuleType("qgis")
    fake_core = types.ModuleType("qgis.core")
    fake_pyqt = types.ModuleType("qgis.PyQt")
    fake_qtcore = types.ModuleType("qgis.PyQt.QtCore")
    fake_qtgui = types.ModuleType("qgis.PyQt.QtGui")
    fake_utils = types.ModuleType("qgis.utils")

    setattr(fake_qtcore, "QRectF", _FakeQRectF)
    setattr(fake_qtcore, "QCoreApplication", _FakeQCoreApplication)
    setattr(fake_qtcore, "QEventLoop", object)
    setattr(fake_qtcore, "QObject", object)
    setattr(fake_qtcore, "QThread", _FakeQThread)
    setattr(fake_qtcore, "Qt", _FakeQt)
    setattr(fake_qtcore, "QTimer", _FakeQTimer)
    setattr(fake_qtcore, "pyqtSignal", _fake_pyqt_signal)
    setattr(fake_qtgui, "QColor", _FakeQColor)
    setattr(fake_qtgui, "QFont", _FakeQFont)
    setattr(fake_utils, "iface", None)

    setattr(fake_core, "QgsLayoutItemLabel", _FakeQgsLayoutItemLabel)
    setattr(fake_core, "QgsLayoutMeasurement", _FakeQgsLayoutMeasurement)
    setattr(fake_core, "QgsProject", _FakeQgsProject)
    setattr(fake_core, "QgsUnitTypes", _FakeQgsUnitTypes)
    setattr(fake_core, "QgsLayoutPoint", object)
    setattr(fake_core, "QgsLayoutSize", object)

    setattr(fake_qgis, "core", fake_core)
    setattr(fake_qgis, "PyQt", fake_pyqt)
    setattr(fake_qgis, "utils", fake_utils)
    setattr(fake_pyqt, "QtCore", fake_qtcore)
    setattr(fake_pyqt, "QtGui", fake_qtgui)

    sys.modules["qgis"] = fake_qgis
    sys.modules["qgis.core"] = fake_core
    sys.modules["qgis.PyQt"] = fake_pyqt
    sys.modules["qgis.PyQt.QtCore"] = fake_qtcore
    sys.modules["qgis.PyQt.QtGui"] = fake_qtgui
    sys.modules["qgis.utils"] = fake_utils


class LayoutFileOverviewImplTestCase(unittest.TestCase):
    module = None

    def setUp(self):
        _install_fake_qgis_modules()
        import autocarto.tools.system._shared.layout_file_overview_impl as impl

        self.module = importlib.reload(impl)

    def test_non_text_overview_uses_chinese_spatial_summary(self):
        tmp_root = os.path.join(os.getcwd(), "tmp_gcc_test")
        os.makedirs(tmp_root, exist_ok=True)
        tmp_dir = os.path.join(tmp_root, f"layout_overview_{uuid.uuid4().hex}")
        os.makedirs(tmp_dir, exist_ok=True)
        path = os.path.join(tmp_dir, "henan_county_transport_routes.shx")
        with open(path, "wb") as handle:
            handle.write(b"\x01\x02\x03")

        _FakeQgsProject._instance = _FakeProject(
            {
                "layer1": _FakeLayer(
                    "河南省县级交通路线",
                    os.path.join(tmp_dir, "henan_county_transport_routes.shp"),
                )
            }
        )

        def _force_rule(*_args, **_kwargs):
            raise RuntimeError("skip llm in unit test")

        self.module._summarize_non_text_with_llm = _force_rule
        title, overview_text = self.module._build_overview_text(path, {})

        self.assertTrue(title.startswith("数据综述："))
        self.assertIn("河南省", overview_text)
        self.assertIn("交通", overview_text)
        self.assertIn("县级", overview_text)
        self.assertNotIn("This file is not plain text", overview_text)


if __name__ == "__main__":
    unittest.main()
