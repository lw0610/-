"""Resolve project layers by identity, keeping names as an unambiguous fallback."""
from typing import Any

from qgis.core import QgsProject


def resolve_project_layer(reference: Any):
    if isinstance(reference, dict):
        reference = (reference.get("layer_id") or reference.get("id")
                     or reference.get("name") or reference.get("layer_name"))
    ref = str(reference or "").strip()
    if not ref:
        return None
    project = QgsProject.instance()
    exact = project.mapLayer(ref)
    if exact is not None:
        return exact
    matches = project.mapLayersByName(ref)
    if len(matches) > 1:
        ids = ", ".join(layer.id() for layer in matches)
        raise ValueError(f"图层名称 '{ref}' 不唯一，请使用图层 ID：{ids}")
    return matches[0] if matches else None
