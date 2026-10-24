"""Deterministic executable sidecar for build-pipeline tests (no real deployment)."""
import sys
from pathlib import Path

SIDECAR = '''#!{python}
import argparse, json
from http.server import BaseHTTPRequestHandler, HTTPServer
parser=argparse.ArgumentParser()
parser.add_argument('-port',type=int,required=True)
args=parser.parse_args()
class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header('Content-Type','application/json')
        self.end_headers()
        self.wfile.write(b'{{"success":true,"data":{{}}}}')
    def log_message(self,*args): pass
HTTPServer(('127.0.0.1',args.port),Handler).serve_forever()
'''

def install_fake_build_platform_and_go(fake_bin: Path) -> None:
    # These fixtures exercise Linux release layout independently of host OS.
    platform = fake_bin / "uname"
    platform.write_text('#!/bin/sh\nif [ "$1" = "-m" ]; then echo x86_64; else echo Linux; fi\n')
    platform.chmod(0o755)
    binary = SIDECAR.format(python=sys.executable)
    go = fake_bin / "go"
    go.write_text(f'''#!{sys.executable}
import os, sys
from pathlib import Path
args=sys.argv[1:]
if args==['version']:
    print('go version go1.24.11 linux/amd64')
    raise SystemExit(0)
with open(os.environ['AUTOCARTO_FAKE_LOG'],'a') as log:
    log.write('go '+' '.join(args)+'\\n')
if '-o' in args:
    path=Path(args[args.index('-o')+1])
    path.parent.mkdir(parents=True,exist_ok=True)
    path.write_text({binary!r})
    path.chmod(0o755)
''')
    go.chmod(0o755)
