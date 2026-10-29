#!/usr/bin/env python3
"""Measure pixel differences, without assigning an acceptance threshold."""
import argparse
import hashlib
import json
import tempfile
from pathlib import Path
import numpy as np
from PIL import Image
from autocarto.tests.golden.qgis_bootstrap import isolated_qgis
from autocarto.tests.golden.compare_golden import perceptual_hash

ROOT = Path(__file__).resolve().parents[2]


def pixels(image):
    # Compare visible RGB over white; hidden RGB under transparency is irrelevant.
    if isinstance(image, Path):
        with Image.open(image) as source:
            source = source.convert('RGBA')
            return np.asarray(Image.alpha_composite(Image.new('RGBA', source.size, 'white'), source).convert('RGB')).copy()
    converted = image.convertToFormat(image.Format_RGBA8888)
    value = np.frombuffer(converted.constBits().asstring(converted.sizeInBytes()), dtype=np.uint8).reshape(converted.height(), converted.width(), 4).astype(float)
    alpha = value[:, :, 3:4] / 255
    return np.rint(value[:, :, :3] * alpha + 255 * (1 - alpha)).astype(np.uint8)


def metrics(left, right):
    if left.shape != right.shape:
        return {'comparable': False, 'reason': 'dimensions differ', 'expected_shape': list(left.shape), 'actual_shape': list(right.shape)}
    delta = np.abs(left.astype(np.int16) - right.astype(np.int16))
    changed = np.any(delta != 0, axis=2)
    painted = np.any(left != 255, axis=2) | np.any(right != 255, axis=2)
    locations = np.argwhere(changed)
    return {'comparable': True, 'width': left.shape[1], 'height': left.shape[0], 'total_pixels': int(changed.size),
            'changed_pixels': int(changed.sum()), 'changed_fraction': float(changed.mean()),
            'rgb_mae_0_255': float(delta.mean()), 'rgb_rmse_0_255': float(np.sqrt(np.mean(delta.astype(float)**2))),
            'max_channel_difference': int(delta.max()),
            'nonwhite_union_pixels': int(painted.sum()),
            'changed_fraction_of_nonwhite_union': float(changed.sum()/painted.sum()) if painted.any() else 0,
            'difference_bbox_xyxy': [int(locations[:,1].min()),int(locations[:,0].min()),int(locations[:,1].max()),int(locations[:,0].max())] if locations.size else None}


def isolated_layers():
    from qgis.core import QgsFeature, QgsGeometry, QgsVectorLayer, QgsProject, QgsMapSettings, QgsMapRendererParallelJob, QgsRectangle
    from qgis.PyQt.QtCore import QSize
    from qgis.PyQt.QtGui import QColor
    from autocarto.tools.system._shared.symbology import set_symbology_handler
    from autocarto.tools.system._shared.style_labeling import restore_qml_font_families
    cases = json.loads((ROOT / 'autocarto/tests/fixtures/basemap_symbology.json').read_text())
    def render(layer):
        settings = QgsMapSettings(); settings.setLayers([layer]); settings.setDestinationCrs(layer.crs())
        settings.setExtent(QgsRectangle(-1, -1, 5, 4)); settings.setOutputSize(QSize(800, 600)); settings.setOutputDpi(96)
        settings.setBackgroundColor(QColor('white'))
        job = QgsMapRendererParallelJob(settings); job.start(); job.waitForFinished()
        return pixels(job.renderedImage())
    results = []
    for name, geometry, spec in cases:
        layers = []
        for index in range(2):
            layer = QgsVectorLayer(geometry + '?crs=EPSG:4326&field=NAME:string&field=name:string', name+str(index), 'memory')
            feature = QgsFeature(layer.fields()); feature.setAttributes([name, name])
            feature.setGeometry(QgsGeometry.fromWkt({'Polygon': 'POLYGON((0 0,4 0,4 3,0 3,0 0))', 'LineString': 'LINESTRING(0 0,2 1.5,4 3)', 'Point': 'POINT(2 1.5)'}[geometry]))
            assert layer.dataProvider().addFeatures([feature])[0]; layer.updateExtents()
            QgsProject.instance().addMapLayer(layer); layers.append(layer)
        old, new = layers
        assert old.loadNamedStyle(str(ROOT / 'autocarto/resources/system/styles' / name / (name + '.qml')))[1]
        result = set_symbology_handler({'items': [dict(spec, layer=new.id())], 'scale': 1})
        assert result['success'] and not result['data']['items'][0]['differences'], result
        row = {'name': name, 'unmodified_qml_vs_parameter_a1': metrics(render(old), render(new))}
        # Explicitly remove intended font/river-area label differences in memory.
        # No production QML, layer data or original golden PNG is changed.
        restore_qml_font_families(old)
        if spec['labels'] is None:
            old.setLabelsEnabled(False)
        row['same_font_and_label_policy'] = metrics(render(old), render(new))
        old.setLabelsEnabled(False); new.setLabelsEnabled(False)
        row['symbols_only'] = metrics(render(old), render(new))
        row['repeat_current_render'] = metrics(render(new), render(new))
        results.append(row)
        QgsProject.instance().clear()
    return results


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--reference-root', type=Path, default=Path('/tmp/enio2-v6-acceptance-routes-20261028'))
    parser.add_argument('--candidate-root', type=Path, default=Path('/tmp/autocarto-v7-d14-acceptance-routes-20261029'))
    args = parser.parse_args()
    old_root = args.reference_root
    new_root = args.candidate_root
    baseline = ROOT / 'autocarto/tests/golden/baseline/route/preview.png'
    full = []
    for name in ('route-no-review', 'route-with-review'):
        old, new = old_root/name/'preview.png', new_root/name/'preview.png'
        hashes = [perceptual_hash(path) for path in (old, new)]
        record = {'scenario': name, 'reference': str(old), 'candidate': str(new),
                  'reference_sha256': hashlib.sha256(old.read_bytes()).hexdigest(),
                  'candidate_sha256': hashlib.sha256(new.read_bytes()).hexdigest(),
                  'dhash64_distance': (int(hashes[0],16)^int(hashes[1],16)).bit_count(),
                  'pixels': metrics(pixels(old),pixels(new)),
                  'checked_in_golden_vs_v6': metrics(pixels(baseline), pixels(old)),
                  'checked_in_golden_vs_candidate': metrics(pixels(baseline), pixels(new))}
        full.append(record)
    with tempfile.TemporaryDirectory(prefix='d14-pixels-') as temporary:
        with isolated_qgis(Path(temporary)):
            layers = isolated_layers()
    report = {'acceptance': 'not_determined', 'predefined_pixel_threshold': None,
              'reason': 'Existing golden comparator explicitly treats pixel/dHash differences as diagnostic only; no prior pixel threshold exists.',
              'method': 'All visible RGB channels, over white, at original resolution; no resize/registration/mask of full map.',
              'checked_in_golden': str(baseline), 'checked_in_golden_sha256': hashlib.sha256(baseline.read_bytes()).hexdigest(),
              'full_a1_maps': full, 'controlled_layer_comparison': layers}
    args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
    print(json.dumps({'full_map_changed_fractions': [r['pixels']['changed_fraction'] for r in full],
                      'symbol_changed_pixels': {r['name']:r['symbols_only']['changed_pixels'] for r in layers},
                      'same_font_changed_pixels': {r['name']:r['same_font_and_label_policy']['changed_pixels'] for r in layers}}, ensure_ascii=False))


if __name__ == '__main__':
    main()
