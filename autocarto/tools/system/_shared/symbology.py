"""Parameter driven symbols and independent QGIS renderer/label readback."""
import math
from qgis.PyQt.QtCore import Qt
from qgis.PyQt.QtGui import QColor, QFont, QFontInfo
from qgis.core import (
    Qgis, QgsFillSymbol, QgsLineSymbol, QgsMarkerSymbol, QgsPalLayerSettings,
    QgsSimpleFillSymbolLayer, QgsSimpleLineSymbolLayer, QgsSimpleMarkerSymbolLayer,
    QgsSingleSymbolRenderer, QgsTextBufferSettings, QgsTextFormat, QgsVectorLayer,
    QgsVectorLayerSimpleLabeling,
)
from autocarto.tools.system._shared.style_common import find_style_layer, _wrap_main_thread
from autocarto.tools.system._shared.style_labeling import select_label_font

PEN = {'solid': Qt.SolidLine, 'dash': Qt.DashLine, 'dot': Qt.DotLine,
       'dash_dot': Qt.DashDotLine, 'dash_dot_dot': Qt.DashDotDotLine, 'none': Qt.NoPen}
CAP = {'flat': Qt.FlatCap, 'square': Qt.SquareCap, 'round': Qt.RoundCap}
PLACEMENT = {'horizontal': Qgis.LabelPlacement.Horizontal, 'line': Qgis.LabelPlacement.Line,
             'around_point': Qgis.LabelPlacement.AroundPoint}
MM = Qgis.RenderUnit.Millimeters


def _number(value, name, minimum=0):
    value = float(value)
    if not math.isfinite(value) or (minimum is not None and value < minimum):
        raise ValueError(name + ' 数值无效')
    return value


def _rgba(value, opacity=1):
    color = QColor('transparent' if value is None else str(value))
    if not color.isValid():
        raise ValueError('颜色无效：' + str(value))
    opacity = _number(opacity, 'opacity')
    if opacity > 1:
        raise ValueError('opacity 必须在 0–1 之间')
    alpha = color.alphaF() * opacity
    return (color.name() if alpha else None), alpha


def _qcolor(color, opacity):
    value = QColor(color or 'transparent')
    value.setAlphaF(opacity)
    return value


def _mm(value, unit):
    factors = {MM: 1.0, Qgis.RenderUnit.Points: 25.4 / 72, Qgis.RenderUnit.Inches: 25.4}
    return value * factors[unit] if unit in factors else None


def read_symbology(layer):
    """Read live getters, never reuse the request or symbol creation properties."""
    renderer = layer.renderer()
    symbol = renderer.symbol() if renderer is not None and hasattr(renderer, 'symbol') else None
    state = {'renderer': type(renderer).__name__ if renderer else '', 'labels': {'enabled': layer.labelsEnabled()}}
    if symbol is not None:
        layers = []
        kind = {0: 'marker', 1: 'line', 2: 'fill'}.get(int(symbol.type()), 'other')
        for index in range(symbol.symbolLayerCount()):
            sl = symbol.symbolLayer(index)
            if isinstance(sl, (QgsSimpleFillSymbolLayer, QgsSimpleMarkerSymbolLayer)):
                color = sl.color()
                value = {'fill': color.name() if color.alpha() else None, 'opacity': color.alphaF(),
                         'outline': sl.strokeColor().name(), 'outline_opacity': sl.strokeColor().alphaF(),
                         'outline_width_mm': _mm(sl.strokeWidth(), sl.strokeWidthUnit()),
                         'outline_style': next((k for k, v in PEN.items() if v == sl.strokeStyle()), 'unknown')}
                if isinstance(sl, QgsSimpleMarkerSymbolLayer):
                    value.update(shape=sl.encodeShape(sl.shape()), size_mm=_mm(sl.size(), sl.sizeUnit()))
                elif sl.brushStyle() == Qt.NoBrush:
                    value.update(fill=None, opacity=0.0)
            elif isinstance(sl, QgsSimpleLineSymbolLayer):
                value = {'color': sl.color().name(), 'opacity': sl.color().alphaF(),
                         'width_mm': _mm(sl.width(), sl.widthUnit()),
                         'style': next((k for k, v in PEN.items() if v == sl.penStyle()), 'unknown'),
                         'dash_mm': [_mm(v, sl.customDashPatternUnit()) for v in sl.customDashVector()] if sl.useCustomDashPattern() else [],
                         'offset_mm': _mm(sl.offset(), sl.offsetUnit()),
                         'cap': next((k for k, v in CAP.items() if v == sl.penCapStyle()), 'unknown')}
            else:
                value = {'layer_type': sl.layerType(), 'properties': sl.properties()}
            layers.append(value)
        state['symbol'] = {'type': kind, 'layers': layers}
    labeling = layer.labeling()
    if labeling is not None and hasattr(labeling, 'settings'):
        settings = labeling.settings()
        fmt = settings.format()
        buffer = fmt.buffer()
        state['labels'].update(field=settings.fieldName, size_mm=_mm(fmt.size(), fmt.sizeUnit()),
            color=fmt.color().name(), font=QFontInfo(fmt.font()).family(),
            bold=fmt.font().bold(), obstacle=settings.obstacle,
            placement=next((k for k, v in PLACEMENT.items() if v == settings.placement), str(int(settings.placement))),
            merge_lines=settings.mergeLines, repeat_mm=_mm(settings.repeatDistance, settings.repeatDistanceUnit),
            display_all=settings.displayAll,
            buffer={'enabled': buffer.enabled(), 'color': buffer.color().name(), 'size_mm': _mm(buffer.size(), buffer.sizeUnit())})
    return state


def scale_loaded_style(layer, scale):
    """Scale physical dimensions in the loaded renderer without replacing its style."""
    expected = read_symbology(layer)
    warnings = []
    if scale == 1:
        return expected, warnings
    renderer = layer.renderer()
    symbol = renderer.symbol() if renderer is not None and hasattr(renderer, 'symbol') else None
    if symbol is not None:
        for index, value in enumerate(expected['symbol']['layers']):
            sl = symbol.symbolLayer(index)
            if isinstance(sl, QgsSimpleLineSymbolLayer):
                attributes = [('width_mm', sl.setWidth, sl.setWidthUnit, .1),
                              ('offset_mm', sl.setOffset, sl.setOffsetUnit, None)]
                if sl.useCustomDashPattern() and all(v is not None for v in value['dash_mm']):
                    value['dash_mm'] = [v * scale for v in value['dash_mm']]
                    sl.setCustomDashVector(value['dash_mm']); sl.setCustomDashPatternUnit(MM)
            elif isinstance(sl, (QgsSimpleFillSymbolLayer, QgsSimpleMarkerSymbolLayer)):
                attributes = [('outline_width_mm', sl.setStrokeWidth, sl.setStrokeWidthUnit, .1)]
                if isinstance(sl, QgsSimpleMarkerSymbolLayer):
                    attributes.append(('size_mm', sl.setSize, sl.setSizeUnit, None))
            else:
                warnings.append('符号层 ' + sl.layerType() + ' 不支持纸张尺寸缩放，保留原设置')
                continue
            for key, setter, unit_setter, floor in attributes:
                if value[key] is None:
                    warnings.append(key + ' 使用非物理单位，保留原设置')
                    continue
                value[key] *= scale
                if floor is not None:
                    value[key] = max(floor, value[key])
                setter(value[key]); unit_setter(MM)
    else:
        warnings.append('当前样式不是单一符号渲染器，保留符号尺寸')
    labeling = layer.labeling()
    if labeling is not None and hasattr(labeling, 'settings'):
        settings = labeling.settings()
        fmt = settings.format(); buffer = fmt.buffer()
        values = expected['labels']
        if values['size_mm'] is not None:
            values['size_mm'] = max(1.8, values['size_mm'] * scale)
            fmt.setSize(values['size_mm']); fmt.setSizeUnit(MM)
        if values['buffer']['size_mm'] is not None:
            values['buffer']['size_mm'] *= scale
            buffer.setSize(values['buffer']['size_mm']); buffer.setSizeUnit(MM); fmt.setBuffer(buffer)
        if values['repeat_mm'] is not None:
            values['repeat_mm'] *= scale
            settings.repeatDistance = values['repeat_mm']; settings.repeatDistanceUnit = MM
        settings.setFormat(fmt)
        layer.setLabeling(QgsVectorLayerSimpleLabeling(settings))
    return expected, warnings


def symbology_differences(expected, actual, path=''):
    differences = []
    if isinstance(expected, dict):
        for key, value in expected.items():
            differences.extend(symbology_differences(value, actual.get(key) if isinstance(actual, dict) else None, path + '.' + key if path else key))
    elif isinstance(expected, list):
        if not isinstance(actual, list) or len(actual) != len(expected):
            differences.append({'path': path, 'expected': expected, 'actual': actual})
        else:
            for index, value in enumerate(expected):
                differences.extend(symbology_differences(value, actual[index], f'{path}[{index}]'))
    elif isinstance(expected, (int, float)) and not isinstance(expected, bool) and isinstance(actual, (int, float)):
        if abs(expected - actual) > (0.01 if '_mm' in path else 0.0001):
            differences.append({'path': path, 'expected': expected, 'actual': actual})
    elif expected != actual:
        differences.append({'path': path, 'expected': expected, 'actual': actual})
    return differences


def _normalize(item, layer, scale, warnings):
    expected = {}
    if 'symbol' in item:
        symbol = item['symbol']
        kind = symbol['type']
        if kind not in ('fill', 'line', 'marker') or layer.geometryType() != {'fill': 2, 'line': 1, 'marker': 0}[kind]:
            raise ValueError('符号类型与图层几何类型不一致')
        if not isinstance(symbol.get('layers'), list) or not symbol['layers']:
            raise ValueError('symbol.layers 不能为空')
        normalized = []
        for raw in symbol['layers']:
            if kind == 'line':
                color, opacity = _rgba(raw.get('color', '#000000'), raw.get('opacity', 1))
                style = raw.get('style', 'solid')
                cap = raw.get('cap', 'flat')
                if style not in PEN or cap not in CAP:
                    raise ValueError('线型或端点样式无效')
                dash = [_number(v, 'dash') * scale for v in raw.get('dash', [])]
                if dash and (len(dash) % 2 or any(v == 0 for v in dash)):
                    raise ValueError('dash 必须为正数的线段/间隔成对列表')
                normalized.append({'color': color or '#000000', 'opacity': opacity,
                    'width_mm': max(.1, _number(raw.get('width', .3), 'width') * scale),
                    'style': style, 'dash_mm': dash,
                    'offset_mm': _number(raw.get('offset', 0), 'offset', None) * scale, 'cap': cap})
            else:
                fill, opacity = _rgba(raw.get('fill', '#ffffff'), raw.get('opacity', 1))
                outline, outline_opacity = _rgba(raw.get('outline', '#000000'))
                style = raw.get('outline_style', 'solid')
                if style not in PEN:
                    raise ValueError('边线线型无效：' + str(style))
                value = {'fill': fill, 'opacity': opacity, 'outline': outline or '#000000',
                    'outline_opacity': outline_opacity,
                    'outline_width_mm': max(.1, _number(raw.get('outline_width', .1), 'outline_width') * scale),
                    'outline_style': style}
                if kind == 'marker':
                    shape = raw.get('shape', 'circle')
                    if shape not in ('circle', 'square', 'triangle', 'star'):
                        raise ValueError('点符号形状无效：' + str(shape))
                    value.update(shape=shape, size_mm=_number(raw.get('size', 2), 'size') * scale)
                normalized.append(value)
        expected['symbol'] = {'type': kind, 'layers': normalized}
    if 'labels' in item:
        raw = item['labels']
        if raw is None:
            expected['labels'] = {'enabled': False}
        else:
            fields = layer.fields().names()
            field = raw.get('field', 'auto')
            if field == 'auto':
                preferred = str(layer.customProperty('autocarto/label_field', ''))
                field = next((name for name in (preferred, 'NAME', 'name', 'name:zh', '名称') if name and name != '无' and name in fields), '')
            if field not in fields:
                warnings.append('标注字段不存在，已关闭标注：' + (field or '没有可用名称字段'))
            unit = raw.get('unit', 'mm')
            if unit not in ('mm', 'pt'):
                raise ValueError('标注单位仅支持 mm 或 pt')
            placement = raw.get('placement', {0: 'around_point', 1: 'line', 2: 'horizontal'}[layer.geometryType()])
            if placement not in PLACEMENT:
                raise ValueError('标注放置方式无效')
            font, font_warnings = select_label_font(raw.get('font', 'auto'))
            warnings.extend(font_warnings)
            color, _ = _rgba(raw.get('color', '#000000'))
            buffer = raw.get('buffer')
            normalized_buffer = {'enabled': buffer is not None}
            if buffer is not None:
                buffer_color, _ = _rgba(buffer.get('color', '#ffffff'))
                normalized_buffer.update(color=buffer_color, size_mm=_number(buffer.get('size', .5), 'buffer.size') * scale)
            expected['labels'] = {'enabled': True, 'field': field,
                'size_mm': max(1.8, _number(raw.get('size', 3), 'labels.size') * (25.4 / 72 if unit == 'pt' else 1) * scale),
                'color': color, 'font': font, 'placement': placement,
                'bold': bool(raw.get('bold', False)), 'obstacle': bool(raw.get('obstacle', True)),
                'merge_lines': bool(raw.get('merge_lines', False)),
                'repeat_mm': _number(raw.get('repeat', 0), 'labels.repeat') * scale,
                'display_all': bool(raw.get('display_all', False)), 'buffer': normalized_buffer}
    if not expected:
        raise ValueError('至少指定 symbol 或 labels')
    return expected


def _apply(layer, expected):
    if 'symbol' in expected:
        spec = expected['symbol']
        layers = []
        for value in spec['layers']:
            if spec['type'] == 'line':
                sl = QgsSimpleLineSymbolLayer()
                sl.setColor(_qcolor(value['color'], value['opacity']))
                sl.setWidth(value['width_mm']); sl.setWidthUnit(MM)
                sl.setPenStyle(PEN[value['style']]); sl.setPenCapStyle(CAP[value['cap']])
                sl.setOffset(value['offset_mm']); sl.setOffsetUnit(MM)
                sl.setUseCustomDashPattern(bool(value['dash_mm']))
                if value['dash_mm']:
                    sl.setCustomDashVector(value['dash_mm']); sl.setCustomDashPatternUnit(MM)
            else:
                sl = QgsSimpleFillSymbolLayer() if spec['type'] == 'fill' else QgsSimpleMarkerSymbolLayer()
                sl.setColor(_qcolor(value['fill'], value['opacity']))
                sl.setStrokeColor(_qcolor(value['outline'], value['outline_opacity']))
                sl.setStrokeWidth(value['outline_width_mm']); sl.setStrokeWidthUnit(MM)
                sl.setStrokeStyle(PEN[value['outline_style']])
                if spec['type'] == 'marker':
                    sl.setShape(sl.decodeShape(value['shape'])[0])
                    sl.setSize(value['size_mm']); sl.setSizeUnit(MM)
            layers.append(sl)
        symbol = {'fill': QgsFillSymbol, 'line': QgsLineSymbol, 'marker': QgsMarkerSymbol}[spec['type']](layers)
        layer.setRenderer(QgsSingleSymbolRenderer(symbol))
    if 'labels' in expected:
        value = expected['labels']
        if not value['enabled']:
            layer.setLabelsEnabled(False)
            layer.setLabeling(None)
        else:
            settings = QgsPalLayerSettings()
            settings.fieldName = value['field']; settings.isExpression = False
            settings.placement = PLACEMENT[value['placement']]
            settings.mergeLines = value['merge_lines']; settings.displayAll = value['display_all']
            settings.obstacle = value['obstacle']
            settings.repeatDistance = value['repeat_mm']; settings.repeatDistanceUnit = MM
            fmt = QgsTextFormat()
            font = QFont(value['font']); font.setBold(value['bold'])
            fmt.setFont(font); fmt.setFamilies([value['font']])
            fmt.setSize(value['size_mm']); fmt.setSizeUnit(MM); fmt.setColor(QColor(value['color']))
            buffer = QgsTextBufferSettings(); buffer.setEnabled(value['buffer']['enabled'])
            if value['buffer']['enabled']:
                buffer.setColor(QColor(value['buffer']['color']))
                buffer.setSize(value['buffer']['size_mm']); buffer.setSizeUnit(MM)
            fmt.setBuffer(buffer); settings.setFormat(fmt)
            layer.setLabeling(QgsVectorLayerSimpleLabeling(settings))
            layer.setLabelsEnabled(value['field'] in layer.fields().names())
    layer.triggerRepaint()


@_wrap_main_thread
def set_symbology_handler(params):
    from qgis.core import QgsProject
    items = params.get('items')
    scale = _number(params.get('scale', 1), 'scale')
    if scale == 0 or not isinstance(items, list) or not items:
        return {'success': False, 'error_type': 'invalid_arguments', 'info': 'items 不能为空，scale 必须大于 0'}
    results = []
    for item in items:
        warnings = []
        try:
            layer = find_style_layer(QgsProject.instance(), item['layer'])
            if not isinstance(layer, QgsVectorLayer):
                raise ValueError('符号化仅支持矢量图层')
            expected = _normalize(item, layer, scale, warnings)
            _apply(layer, expected)
            actual = read_symbology(layer)
            differences = symbology_differences(expected, actual)
            results.append({'success': True, 'layer_id': layer.id(), 'layer_name': layer.name(),
                'request': item, 'requested': expected, 'actual': actual, 'warnings': warnings, 'differences': differences})
        except (KeyError, TypeError, ValueError) as exc:
            results.append({'success': False, 'request': item, 'info': str(exc), 'warnings': warnings})
    return {'success': all(item['success'] for item in results), 'info': '已设置符号并读回 QGIS 实际状态',
            'data': {'items': results, 'scale': scale}}
