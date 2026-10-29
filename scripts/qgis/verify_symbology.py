#!/usr/bin/env python3
"""Write actual QGIS D14 requests/readback and old/A1/A4 rendered comparisons."""
import argparse
import json
import tempfile
from pathlib import Path
from autocarto.tests.golden.qgis_bootstrap import isolated_qgis

ROOT = Path(__file__).resolve().parents[2]


def verify(output):
    from qgis.core import QgsFeature, QgsGeometry, QgsVectorLayer, QgsProject, QgsMapSettings, QgsMapRendererParallelJob, QgsRectangle
    from qgis.PyQt.QtCore import QSize, QRect
    from qgis.PyQt.QtGui import QColor, QImage, QPainter, QFont
    from autocarto.tools.system._shared.symbology import set_symbology_handler, read_symbology
    from autocarto.tools.system._shared.style_labeling import select_label_font
    cases = json.loads((ROOT / 'autocarto/tests/fixtures/basemap_symbology.json').read_text())
    panel = QImage(1040, 80 + len(cases) * 180, QImage.Format_ARGB32)
    panel.fill(QColor('white'))
    painter = QPainter(panel)
    painter.setFont(QFont(select_label_font()[0], 12))
    painter.setPen(QColor('#1f2937'))
    for x, title in [(0, '图层'), (150, '已存 QML（未修正）'), (440, '参数 A1'), (730, '参数 A4 ×0.35')]:
        painter.drawText(QRect(x+10, 10, 290, 50), 0, title)
    results = []
    for row, (name, geometry, spec) in enumerate(cases):
        layers = []
        for column in range(3):
            layer = QgsVectorLayer(geometry + '?crs=EPSG:4326&field=NAME:string&field=name:string', name + str(column), 'memory')
            feature = QgsFeature(layer.fields()); feature.setAttributes([name, name])
            feature.setGeometry(QgsGeometry.fromWkt({'Polygon': 'POLYGON((0 0,4 0,4 3,0 3,0 0))', 'LineString': 'LINESTRING(0 0,2 1.5,4 3)', 'Point': 'POINT(2 1.5)'}[geometry]))
            assert layer.dataProvider().addFeatures([feature])[0]; layer.updateExtents()
            QgsProject.instance().addMapLayer(layer)
            layers.append(layer)
        assert layers[0].loadNamedStyle(str(ROOT / 'autocarto/resources/system/styles' / name / (name + '.qml')))[1]
        painter.drawText(QRect(10, 85 + row*180, 130, 160), 0, name)
        record = {'name': name, 'saved_qml': read_symbology(layers[0]), 'calls': []}
        for column in (1, 2):
            request = {'items': [dict(spec, layer=layers[column].id())], 'scale': 1 if column == 1 else .35}
            response = set_symbology_handler(request)
            assert response['success'] and not response['data']['items'][0]['differences'], response
            record['calls'].append({'tool': 'layer_set_symbology', 'arguments': request, 'result': response})
        for column, layer in enumerate(layers):
            settings = QgsMapSettings(); settings.setLayers([layer]); settings.setDestinationCrs(layer.crs())
            settings.setExtent(QgsRectangle(-1, -1, 5, 4)); settings.setOutputSize(QSize(280, 160)); settings.setOutputDpi(96)
            settings.setBackgroundColor(QColor('#f3f6f9'))
            job = QgsMapRendererParallelJob(settings); job.start(); job.waitForFinished()
            painter.drawImage(150 + column*290, 75 + row*180, job.renderedImage())
        results.append(record)
    painter.end()
    output.mkdir(parents=True, exist_ok=True)
    assert panel.save(str(output / 'D14-symbols-A1-A4.png'))
    (output / 'D14-readback.json').write_text(json.dumps(results, ensure_ascii=False, indent=2)+'\n')
    QgsProject.instance().clear()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix='d14-qgis-') as temporary:
        with isolated_qgis(Path(temporary)):
            verify(args.output.resolve())


if __name__ == '__main__':
    main()
