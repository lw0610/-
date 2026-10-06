"""Refresh stateless layout implementations when tool sources are updated.

Tool wrappers are executed from disk for every invocation, but ordinary Python
imports retain their implementation modules for the lifetime of QGIS. Limit
refreshing to these two function-only layout modules; session state, the Qt
dispatcher and the user's project are deliberately left untouched.
"""
import hashlib
import importlib.util
import logging
from pathlib import Path
import sys
import threading


logger = logging.getLogger(__name__)
_LOCK = threading.RLock()
_REVISION_ATTRIBUTE = "__autocarto_layout_source_revision__"
_ALLOWED_MODULES = {
    "autocarto.tools.system._shared.print_layout_items_impl",
    "autocarto.tools.system._shared.print_layout_advanced_impl",
}


def load_layout_handler(module_name, function_name, implementation_path):
    if module_name not in _ALLOWED_MODULES:
        raise ValueError(f"不允许刷新非布局实现模块: {module_name}")
    source_path = Path(implementation_path).resolve(strict=True)
    if source_path.name != module_name.rsplit(".", 1)[1] + ".py":
        raise ValueError("布局实现文件与模块名称不一致")
    with _LOCK:
        source = source_path.read_bytes()
        revision = (str(source_path), hashlib.sha256(source).hexdigest())
        module = sys.modules.get(module_name)
        if module is None or getattr(module, _REVISION_ATTRIBUTE, None) != revision:
            spec = importlib.util.spec_from_file_location(module_name, source_path)
            if spec is None or spec.loader is None:
                raise ImportError(f"无法加载布局实现: {source_path}")
            refreshed = importlib.util.module_from_spec(spec)
            # Compile the inspected bytes directly. A same-second deployment
            # with unchanged file length must not reuse an older .pyc file.
            exec(compile(source, str(source_path), "exec"), refreshed.__dict__)
            handler = getattr(refreshed, function_name)
            if not callable(handler):
                raise TypeError(f"布局 handler 不可调用: {module_name}.{function_name}")
            setattr(refreshed, _REVISION_ATTRIBUTE, revision)
            # Publish only a fully loaded implementation. Import failures keep
            # the previous cache usable and propagate the original exception.
            sys.modules[module_name] = refreshed
            parent_name, child_name = module_name.rsplit(".", 1)
            parent = sys.modules.get(parent_name)
            if parent is not None:
                setattr(parent, child_name, refreshed)
            module = refreshed
            logger.info("布局实现已刷新: module=%s sha256=%s", module_name, revision[1][:12])
        handler = getattr(module, function_name)
        if not callable(handler):
            raise TypeError(f"布局 handler 不可调用: {module_name}.{function_name}")
        return handler
