"""Execute every shipped GEE template with a local SDK boundary double."""
from pathlib import Path
from types import SimpleNamespace
import pytest

ROOT = Path(__file__).resolve().parents[1] / 'tools/system/data-acquisition/assets/gee'


@pytest.mark.parametrize('template', ['dem','landcover','optical','population','nightlight'])
def test_system_template_parameters_and_native_projection(template, tmp_path):
    calls=[]
    class Chain:
        def __init__(self, result=None): self.result=result
        def getInfo(self): return self.result
        def size(self): return Chain(2)
        def first(self): return self
        def projection(self): return self
        def crs(self): return Chain('EPSG:32650')
        def map(self, fn): calls.append(('map',));fn(self);return self
        def __getattr__(self, name):
            def call(*args):calls.append((name,*args));return self
            return call
    image=Chain()
    fake=SimpleNamespace(ImageCollection=lambda dataset:(calls.append(('dataset',dataset)) or image),
        Geometry=SimpleNamespace(Rectangle=lambda bbox,*args:('roi',bbox)),
        Filter=SimpleNamespace(lte=lambda *args:('lte',*args),eq=lambda *args:('eq',*args)))
    outputs=[]
    def download(image, filename, **kwargs):
        outputs.append(kwargs);return str(tmp_path/filename)
    params=dict(dataset='verified/dataset',bands=['B4','B3','B2'],scale=20,bbox=[110,30,111,31],start='2025-01-01',end='2026-01-01',cloud_max=10,year=2020)
    scope=dict(ee=fake,params=params,download_image=download)
    exec(compile((ROOT/'common.py').read_text()+'\n'+(ROOT/(template+'.py')).read_text(),template,'exec'),scope)
    assert outputs==[dict(region=('roi',params['bbox']),scale=20,crs='EPSG:32650')]
    assert ('dataset','verified/dataset') in calls
    if template in ('optical','nightlight'): assert ('filterDate','2025-01-01','2026-01-01') in calls
    if template=='optical': assert ('select','SCL') in calls and ('filter',('lte','CLOUDY_PIXEL_PERCENTAGE',10)) in calls
    if template=='nightlight': assert ('select','cf_cvg') in calls
    if template=='population': assert ('filter',('eq','year',2020)) in calls
