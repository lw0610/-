import importlib


def refresh_canvas(logger=None):
    try:
        iface = getattr(importlib.import_module("qgis.utils"), "iface", None)
        qtimer = getattr(importlib.import_module("qgis.PyQt.QtCore"), "QTimer")

        if iface and iface.mapCanvas():
            canvas = iface.mapCanvas()
            qtimer.singleShot(0, canvas.refresh)
    except Exception as exc:
        if logger is not None:
            try:
                logger.debug("刷新画布失败: %s", exc)
            except Exception:
                pass
