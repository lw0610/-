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
from qgis.core import QgsApplication, QgsProject, Qgis
from qgis.PyQt.QtCore import QTimer

app = QgsApplication([], False)
app.initQgis()
# Desktop QGIS adds its built-in processing plugin path during startup.
sys.path.insert(0, str(Path(QgsApplication.pkgDataPath()) / "python" / "plugins"))
from processing.core.Processing import Processing
Processing.initialize()

project = QgsProject.instance()
project_path = home / "work" / "project.qgz"
project_path.parent.mkdir(parents=True, exist_ok=True)
project.setFileName(str(project_path))
if not project.write():
    raise RuntimeError("cannot create isolated QGIS project")

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
    app.exitQgis()
