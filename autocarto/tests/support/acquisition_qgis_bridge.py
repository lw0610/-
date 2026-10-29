"""JSON bridge for opt-in offline Go→QGIS acquisition acceptance.

All task data and QGIS profiles are temporary. Synthetic data is never presented
as a downloaded real-world dataset; builtin province polygons remain read-only.
"""
import importlib
import json
import os
from pathlib import Path
import sys

from autocarto.tests.golden.qgis_bootstrap import isolated_qgis


def main():
    request = json.load(sys.stdin)
    name, args = request['name'], request['args']
    with isolated_qgis(Path(os.environ['AUTOCARTO_HOME']) / 'qgis-bridge'):
        if name == 'admin_boundary_extract':
            from autocarto.tools.system._shared.admin_boundary_impl import extract_boundary_handler
            result = extract_boundary_handler(args)
        elif name in {'data_normalize', 'data_derive'}:
            module = 'normalize' if name == 'data_normalize' else 'derive'
            handler = getattr(importlib.import_module('autocarto.tools.system.data-acquisition.handlers.' + module), module + '_handler')
            result = handler(args)
        elif name == 'verify_basemap':
            from autocarto.tools.system._shared.data_loading_impl import load_list_handler
            from autocarto.tools.system.cartography.handlers.style import load_styles_handler
            from qgis.core import QgsProject
            loaded = load_list_handler({'items': args['items'], 'strict': True, 'reuse_existing': True})
            if not loaded['success']:
                result = loaded
            else:
                layers = list(QgsProject.instance().mapLayers().values())
                styled = load_styles_handler({'items': args['styles']})
                reused = load_list_handler({'items': args['items'], 'strict': True, 'reuse_existing': True})
                result = {'success': styled['success'] and reused['success'], 'load': loaded, 'style': styled, 'reuse': reused,
                          'layers': {layer.name(): {'source': layer.source(), 'fields': layer.fields().names(), 'count': layer.featureCount(), 'crs': layer.crs().authid()} for layer in layers}}
        else:
            raise ValueError('unsupported fixture operation')
        print('V4_QGIS_RESULT=' + json.dumps(result, ensure_ascii=False))


if __name__ == '__main__':
    main()
