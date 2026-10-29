"""Offscreen production reverse-WS worker for the opt-in Go flow integration test.

No tool is mocked or installed into a desktop profile. All files live in the
explicit isolated runtime home supplied by the test process.
"""
import json
import os
from pathlib import Path
import signal
import sys

root = Path(os.environ["AUTOCARTO_PROJECT_ROOT"]).resolve()
home = Path(os.environ["AUTOCARTO_HOME"]).resolve()
sys.path.insert(0, str(root))
# QGIS reads this root at import/initialization time, before the constructor's
# profile argument takes effect on macOS.
os.environ["QGIS_CUSTOM_CONFIG_PATH"] = str(home / "qgis-config")
# The bundled Python launcher does not inherit the desktop app's GDAL data
# directory. OGR needs osmconf.ini to open actual OSM PBF downloads.
for parent in Path(sys.executable).resolve().parents:
    if parent.name == "Contents":
        gdal_data = parent / "Resources" / "qgis" / "gdal"
        if (gdal_data / "osmconf.ini").is_file():
            os.environ.setdefault("GDAL_DATA", str(gdal_data))
        break
from qgis.core import QgsApplication, QgsProject, QgsCoordinateReferenceSystem, Qgis
from qgis.PyQt.QtCore import QTimer

profile = home / "qgis-config" / "profiles" / "default"
profile.mkdir(parents=True, exist_ok=True)
# Exercise the real installed route renderer from an isolated QGIS profile.
# The production tool resolves this dependency through qgisSettingsDirPath().
if os.environ.get("AUTOCARTO_ROUTE_RENDERER_DIR"):
    renderer = Path(os.environ["AUTOCARTO_ROUTE_RENDERER_DIR"]).resolve(strict=True)
    plugins = profile / "python" / "plugins"
    plugins.mkdir(parents=True, exist_ok=True)
    link = plugins / renderer.name
    if not link.exists():
        link.symlink_to(renderer, target_is_directory=True)
app = QgsApplication([], False, str(profile))
app.initQgis()
# Desktop QGIS adds its built-in processing plugin path during startup.
sys.path.insert(0, str(Path(QgsApplication.pkgDataPath()) / "python" / "plugins"))
from processing.core.Processing import Processing
Processing.initialize()
from qgis.gui import QgsMapCanvas
import qgis.utils

canvas = QgsMapCanvas()

class IsolatedIface:
    def mapCanvas(self):
        return canvas

    def activeLayer(self):
        return None

qgis.utils.iface = IsolatedIface()

project = QgsProject.instance()
project_path = Path(os.environ.get("AUTOCARTO_REPLAY_PROJECT_PATH", str(home / "work" / "project.qgz"))).resolve()
project_path.parent.mkdir(parents=True, exist_ok=True)
if project_path.is_file():
    if not project.read(str(project_path)):
        raise RuntimeError("cannot reopen isolated QGIS project")
else:
    project.setFileName(str(project_path))
    if not project.write():
        raise RuntimeError("cannot create isolated QGIS project")
if not project.crs().isValid():
    project.setCrs(QgsCoordinateReferenceSystem("EPSG:4326"))
canvas.setDestinationCrs(project.crs())
project.crsChanged.connect(lambda: canvas.setDestinationCrs(project.crs()))

from autocarto.core.instance_ws_client import InstanceWSClient
from autocarto.core.tool_engine.engine import QGISToolEngine

class EmptyConfig:
    def get(self, key, default=None):
        return default

engine = QGISToolEngine()
client = InstanceWSClient(
    EmptyConfig(), engine, instance_id="t53-production-qgis",
    qgis_version=Qgis.QGIS_VERSION, skills_dir=str(root / "autocarto" / "skills"),
)
client.connection_changed.connect(
    lambda value: print(json.dumps({"connected": value, "qgis_version": Qgis.QGIS_VERSION,
                                   "project_path": str(project_path)}), flush=True)
)
client.start()
heartbeat = QTimer()
def report_ready():
    if (home / "worker-ready.json").exists():
        return
    # Observe actual installed execution bindings, without making Python a
    # public discovery authority or replacing any execution handler.
    names = [item.name for item in engine._registry.get_all_tools()]
    if all(name in names for name in ("data_load_vector", "data_export_vector", "get_layers_info")):
        (home / "worker-ready.json").write_text(json.dumps({"tools": names, "qgis_version": Qgis.QGIS_VERSION}), encoding="utf-8")
heartbeat.timeout.connect(report_ready)
heartbeat.start(200)

def stop(*_):
    project.write()
    client.stop()
    app.quit()

signal.signal(signal.SIGTERM, stop)
signal.signal(signal.SIGINT, stop)
try:
    app.exec_()
finally:
    client.stop()
    project.clear()
    canvas.close()
    app.exitQgis()
