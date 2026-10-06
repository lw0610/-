from __future__ import annotations

import logging
import os
from typing import Any, Callable, Dict, Optional, Tuple, Type

from qgis.PyQt.QtCore import (
    QCoreApplication,
    QEventLoop,
    QMetaObject,
    QObject,
    QThread,
    Qt,
    QTimer,
    pyqtSignal,
    pyqtSlot,
)
from qgis.core import QgsLayoutPoint, QgsLayoutSize, QgsUnitTypes

logger = logging.getLogger(__name__)

_MAIN_THREAD_TIMEOUT_MS = max(
    0, int(os.environ.get("AUTOCARTO_MAIN_THREAD_TIMEOUT_MS", "300000"))
)


class _MainThreadInvoker(QObject):
    finished = pyqtSignal()

    def __init__(self, fn: Callable[[], Any]):
        super().__init__()
        self._fn = fn
        self.result: Any = None
        self.error: Optional[BaseException] = None

    @pyqtSlot()
    def invoke(self):
        try:
            app = QCoreApplication.instance()
            if app is not None and QThread.currentThread() != app.thread():
                raise RuntimeError("run_in_main_thread dispatch invariant broken")
            self.result = self._fn()
        except BaseException as exc:
            self.error = exc
        finally:
            self.finished.emit()


def run_in_main_thread(fn: Callable[[], Any]) -> Any:
    app = QCoreApplication.instance()
    if app is None or QThread.currentThread() == app.thread():
        return fn()

    loop = QEventLoop()
    invoker = _MainThreadInvoker(fn)
    invoker.moveToThread(app.thread())
    invoker.finished.connect(loop.quit, Qt.QueuedConnection)

    timer = None
    timed_out = {"value": False}
    if _MAIN_THREAD_TIMEOUT_MS > 0:
        timer = QTimer()
        timer.setSingleShot(True)

        def _on_timeout():
            timed_out["value"] = True
            loop.quit()

        timer.timeout.connect(_on_timeout)
        timer.start(_MAIN_THREAD_TIMEOUT_MS)

    invoked = QMetaObject.invokeMethod(invoker, "invoke", Qt.QueuedConnection)
    if invoked is False:
        if timer is not None and timer.isActive():
            timer.stop()
        raise RuntimeError("failed to queue invoke into Qt main thread")
    loop.exec()

    if timer is not None and timer.isActive():
        timer.stop()

    if timed_out["value"]:
        raise TimeoutError(f"主线程工具调用超时 ({_MAIN_THREAD_TIMEOUT_MS}ms)")

    if invoker.error is not None:
        raise invoker.error
    return invoker.result


def _layout_page_size_mm(layout) -> Optional[Tuple[float, float]]:
    page_collection = layout.pageCollection()
    if page_collection.pageCount() <= 0:
        return None
    page = page_collection.page(0)
    size = page.pageSize()
    return float(size.width()), float(size.height())


def get_item_rect_mm(item) -> Dict[str, float]:
    width = float(item.rect().width())
    height = float(item.rect().height())

    if hasattr(item, "pagePositionWithUnits"):
        pos = None
        try:
            pos = item.pagePositionWithUnits(QgsUnitTypes.LayoutMillimeters)
        except TypeError:
            try:
                pos = item.pagePositionWithUnits()
            except Exception:
                pos = None
        except Exception:
            pos = None

        if pos is not None:
            x = float(pos.x())
            y = float(pos.y())
        else:
            pos = item.pos()
            x = float(pos.x())
            y = float(pos.y())
    else:
        pos = item.pos()
        x = float(pos.x())
        y = float(pos.y())

    return {
        "x": round(x, 3),
        "y": round(y, 3),
        "width": round(width, 3),
        "height": round(height, 3),
    }


def ensure_item_visible_in_page(layout, item) -> Tuple[Dict[str, float], bool]:
    page_size = _layout_page_size_mm(layout)
    if not page_size:
        return get_item_rect_mm(item), False

    page_width, page_height = page_size
    rect = get_item_rect_mm(item)
    x, y = rect["x"], rect["y"]
    width, height = rect["width"], rect["height"]

    new_width = min(max(width, 1.0), max(page_width, 1.0))
    new_height = min(max(height, 1.0), max(page_height, 1.0))

    max_x = max(page_width - new_width, 0.0)
    max_y = max(page_height - new_height, 0.0)
    new_x = min(max(x, 0.0), max_x)
    new_y = min(max(y, 0.0), max_y)

    auto_adjusted = (
        abs(new_x - x) > 1e-6
        or abs(new_y - y) > 1e-6
        or abs(new_width - width) > 1e-6
        or abs(new_height - height) > 1e-6
    )

    if auto_adjusted:
        if hasattr(item, "attemptResize"):
            item.attemptResize(
                QgsLayoutSize(new_width, new_height, QgsUnitTypes.LayoutMillimeters)
            )
        if hasattr(item, "attemptMove"):
            item.attemptMove(
                QgsLayoutPoint(new_x, new_y, QgsUnitTypes.LayoutMillimeters)
            )

    return get_item_rect_mm(item), auto_adjusted


def validate_layout_item(
    layout,
    layout_name: str,
    item_id: str,
    expected_type: Optional[Type[Any]],
    item_type: str,
    extra_validator: Optional[Callable[[Any], Optional[str]]] = None,
    auto_adjust: bool = True,
) -> Tuple[Optional[Any], Optional[Dict[str, Any]], Optional[str]]:
    item = layout.itemById(item_id)
    if item is None:
        return None, None, f"错误：布局元素 '{item_id}' 未创建成功（itemById 不存在）"

    if expected_type is not None and not isinstance(item, expected_type):
        return (
            None,
            None,
            f"错误：布局元素 '{item_id}' 类型不匹配，期望 {expected_type.__name__}，实际 {type(item).__name__}",
        )

    if extra_validator:
        error = extra_validator(item)
        if error:
            return None, None, error

    if auto_adjust:
        rect_mm, auto_adjusted = ensure_item_visible_in_page(layout, item)
    else:
        rect_mm = get_item_rect_mm(item)
        auto_adjusted = False
    data = {
        "layout": layout_name,
        "item_id": item_id,
        "item_type": item_type,
        "rect_mm": rect_mm,
        "auto_adjusted": auto_adjusted,
    }
    return item, data, None


def success_with_layout_data(
    output: Any, info: str, data: Dict[str, Any]
) -> Dict[str, Any]:
    return {
        "success": True,
        "output": output,
        "info": info,
        "data": data,
    }
