"""Create an isolated, headless QGIS application before importing plugin tools."""
from __future__ import annotations

from contextlib import contextmanager
import os
from pathlib import Path
import sys


class GoldenUnavailable(RuntimeError):
    """A required external runtime or fixture is unavailable (CLI exit 77)."""


def repository_root() -> Path:
    return Path(__file__).resolve().parents[3]


def qgis_available() -> bool:
    try:
        from qgis.core import QgsApplication  # noqa: F401
        return True
    except ImportError:
        return False


@contextmanager
def isolated_qgis(output_dir: Path):
    # In particular, do this before autocarto.utils.logger is imported. Nothing
    # may discover the installed application's runtime or the user's project.
    root = repository_root()
    runtime = output_dir / "runtime"
    profile = runtime / "profile"
    profile.mkdir(parents=True, exist_ok=True)
    os.environ["QT_QPA_PLATFORM"] = "offscreen"
    os.environ["AUTOCARTO_HOME"] = str(runtime)
    os.environ["AUTOCARTO_OUTPUT_ROOT"] = str(output_dir / "exports")
    sys.dont_write_bytecode = True
    sys.path.insert(0, str(root))
    try:
        from qgis.core import QgsApplication, QgsProject
        from qgis.gui import QgsMapCanvas
        import qgis.utils
    except ImportError as exc:
        raise GoldenUnavailable("需要在 QGIS Python 环境运行") from exc
    if QgsApplication.instance() is not None:
        raise GoldenUnavailable("黄金样例必须在独立进程中运行，拒绝复用已有 QGIS 应用")
    contents = Path(sys.executable).resolve().parents[1]
    bundled_data = contents / "Resources" / "qgis"
    if sys.platform == "darwin" and bundled_data.is_dir():
        QgsApplication.setPrefixPath(str(contents / "MacOS"), True)
        QgsApplication.setPkgDataPath(str(bundled_data))
        QgsApplication.setPluginPath(str(contents / "PlugIns" / "qgis"))
    app = QgsApplication([], False, str(profile))
    app.initQgis()
    plugin_python = Path(QgsApplication.pkgDataPath()) / "python" / "plugins"
    sys.path.insert(0, str(plugin_python))
    from processing.core.Processing import Processing
    Processing.initialize()
    canvas = QgsMapCanvas()

    class IsolatedIface:
        def mapCanvas(self):
            return canvas

        def activeLayer(self):
            return None

    previous_iface = getattr(qgis.utils, "iface", None)
    qgis.utils.iface = IsolatedIface()
    try:
        yield app, canvas
    finally:
        from autocarto.tools.system._shared import output_manager
        output_manager.clear_session_context()
        QgsProject.instance().clear()
        qgis.utils.iface = previous_iface
        canvas.close()
        app.exitQgis()
