#!/usr/bin/env python3
"""Serve an existing Next export for opt-in A3 UI acceptance; owns only its listener."""
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import os
import sys

EXPORT = Path(os.environ['AUTOCARTO_REFACTOR_UI_EXPORT_DIR']) if os.environ.get('AUTOCARTO_REFACTOR_UI_EXPORT_DIR') else Path(__file__).resolve().parents[1] / 'out'

class ExportHandler(SimpleHTTPRequestHandler):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory=str(EXPORT), **kwargs)

    def translate_path(self, path):
        translated = Path(super().translate_path(path))
        if not translated.exists() and not translated.suffix:
            html = translated.with_suffix('.html')
            if html.is_file():
                return str(html)
        return str(translated)

if not (EXPORT / 'chat.html').is_file():
    raise SystemExit('Build the static export before starting UI acceptance.')
with ThreadingHTTPServer(('127.0.0.1', int(sys.argv[1])), ExportHandler) as server:
    server.serve_forever()
