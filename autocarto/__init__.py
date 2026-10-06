"""
AutoCarto AI - QGIS插件
基于多智能体系统的GIS智能助手
"""

def classFactory(iface):
    """QGIS插件入口"""
    from .autocarto_plugin import AutoCartoPlugin
    return AutoCartoPlugin(iface)
