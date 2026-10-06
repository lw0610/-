import logging
from typing import Any, Dict, List, Optional

from autocarto.core.tool_engine.project_metadata import resolve_project_identity

logger = logging.getLogger(__name__)


class QGISContext:
    def __init__(self):
        self._qgis_available = False
        self._init_qgis()

    def _init_qgis(self) -> None:
        try:
            from qgis.core import QgsProject

            self._qgis_available = True
            logger.info("QGIS 模块加载成功")
        except ImportError:
            logger.warning("QGIS 模块未找到，上下文功能受限")

    @property
    def is_available(self) -> bool:
        return self._qgis_available

    def get_project_info(self) -> Dict[str, Any]:
        if not self._qgis_available:
            return {"error": "QGIS 不可用"}

        try:
            from qgis.core import QgsProject

            project = QgsProject.instance()
            project_identity = resolve_project_identity(
                project.title(), project.fileName()
            )

            return {
                "title": project_identity["title"],
                "file_path": project_identity["file_path"] or None,
                "project_name": project_identity["project_name"],
                "display_name": project_identity["display_name"],
                "title_source": project_identity["title_source"],
                "crs": project.crs().authid() if project.crs().isValid() else None,
                "layer_count": len(project.mapLayers()),
                "is_dirty": project.isDirty(),
            }
        except Exception as e:
            logger.error(f"获取项目信息失败: {e}")
            return {"error": str(e)}

    def get_layers(self) -> List[Dict[str, Any]]:
        if not self._qgis_available:
            return []

        try:
            from qgis.core import QgsProject, QgsRasterLayer, QgsVectorLayer

            project = QgsProject.instance()
            layers = []

            for layer_id, layer in project.mapLayers().items():
                layer_info = {
                    "id": layer_id,
                    "name": layer.name(),
                    "type": "unknown",
                    "crs": layer.crs().authid() if layer.crs().isValid() else None,
                    "is_valid": layer.isValid(),
                }

                if isinstance(layer, QgsVectorLayer):
                    geometry_type = layer.geometryType()
                    try:
                        geometry_type = int(geometry_type)
                    except Exception:
                        geometry_type = str(geometry_type)

                    layer_info.update(
                        {
                            "type": "vector",
                            "geometry_type": geometry_type,
                            "feature_count": int(layer.featureCount()),
                        }
                    )
                elif isinstance(layer, QgsRasterLayer):
                    layer_info.update(
                        {
                            "type": "raster",
                            "width": layer.width(),
                            "height": layer.height(),
                        }
                    )

                layers.append(layer_info)

            return layers
        except Exception as e:
            logger.error(f"获取图层列表失败: {e}")
            return []

    def get_active_layer(self) -> Optional[Dict[str, Any]]:
        if not self._qgis_available:
            return None

        try:
            from qgis.utils import iface

            if not iface:
                return None

            layer = iface.activeLayer()
            if not layer:
                return None

            layer_type = layer.type()
            try:
                layer_type_name = layer_type.name
            except Exception:
                try:
                    layer_type_name = str(int(layer_type))
                except Exception:
                    layer_type_name = str(layer_type)

            return {
                "id": layer.id(),
                "name": layer.name(),
                "type": layer_type_name,
            }
        except Exception as e:
            logger.error(f"获取活动图层失败: {e}")
            return None

    def get_selected_features(self, layer_name: Optional[str] = None) -> Dict[str, Any]:
        if not self._qgis_available:
            return {"error": "QGIS 不可用"}

        try:
            from qgis.core import QgsProject, QgsVectorLayer
            from qgis.utils import iface

            if layer_name:
                project = QgsProject.instance()
                layers = project.mapLayersByName(layer_name)
                layer = layers[0] if layers else None
            else:
                layer = iface.activeLayer() if iface else None

            if not layer or not isinstance(layer, QgsVectorLayer):
                return {"error": "未找到有效的矢量图层"}

            selected = layer.selectedFeatures()
            return {
                "layer_name": layer.name(),
                "count": len(selected),
                "feature_ids": [f.id() for f in selected[:100]],
            }
        except Exception as e:
            logger.error(f"获取选中要素失败: {e}")
            return {"error": str(e)}

    def get_map_extent(self) -> Dict[str, Any]:
        if not self._qgis_available:
            return {"error": "QGIS 不可用"}

        try:
            from qgis.utils import iface

            if not iface:
                return {"error": "iface 不可用"}

            canvas = iface.mapCanvas()
            extent = canvas.extent()

            return {
                "xmin": extent.xMinimum(),
                "ymin": extent.yMinimum(),
                "xmax": extent.xMaximum(),
                "ymax": extent.yMaximum(),
                "crs": canvas.mapSettings().destinationCrs().authid(),
            }
        except Exception as e:
            logger.error(f"获取地图范围失败: {e}")
            return {"error": str(e)}

    def get_full_context(self) -> Dict[str, Any]:
        return {
            "project": self.get_project_info(),
            "layers": self.get_layers(),
            "active_layer": self.get_active_layer(),
            "map_extent": self.get_map_extent(),
        }
