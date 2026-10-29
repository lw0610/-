#!/usr/bin/env python3
"""Build published administrative layers with QGIS; never edit upstream data."""
from pathlib import Path
import json
import os
import sys
import tempfile
from datetime import datetime, timezone

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT))
os.environ['AUTOCARTO_ASSETS_ROOT'] = str(ROOT / 'autocarto')
from autocarto.tests.golden.qgis_bootstrap import isolated_qgis


def build(output, selected=None):
    import yaml
    from qgis.core import QgsVectorLayer, QgsVectorFileWriter, QgsFeature, QgsProject, QgsGeometry
    from autocarto.tools.system._shared.builtin_layers import data_digest, render_layer
    from autocarto.tests.support.resource_handles import resolve_handle
    upstream = ROOT / 'autocarto/tools/system/admin-boundaries/assets/中国行政区边界/data'
    output.mkdir(parents=True, exist_ok=True)
    style_handles = {}
    for key, short, label, aliases in [('province','省','省域面',['省域','省级行政区']),('city','市','市域面',['市域','地级行政区']),('county','县','县域面',['县域','县级行政区'])]:
        path = upstream / ('china_' + key + '.geojson')
        source = QgsVectorLayer(str(path), label, 'ogr')
        if not source.isValid(): raise RuntimeError(str(path))
        variants = [('area',label,'polygon','admin.division',label,path,source,aliases)] if not selected or key+'-area' in selected else []
        for kind,name,geometry,category,style in [('boundary',short+'界','line','admin.boundary_line','省界'),('seat',('地级' if key=='city' else '县级')+'城市驻地（近似）','point','settlement.prefecture_seat' if key=='city' else 'settlement.county_seat','地级城市驻地' if key=='city' else '县级城市驻地')]:
            if key=='province' and kind=='seat': continue
            if selected and key+'-'+kind not in selected: continue
            folder=output/(key+'-'+kind);folder.mkdir(parents=True,exist_ok=True)
            target=folder/'data.gpkg'
            if target.exists(): target.unlink()
            derived=QgsVectorLayer(('MultiLineString' if kind=='boundary' else 'Point')+'?crs=EPSG:4326',name,'memory')
            provider=derived.dataProvider();provider.addAttributes(source.fields());derived.updateFields()
            features=[]
            for feature in source.getFeatures():
                converted=QgsFeature(derived.fields());converted.setAttributes(feature.attributes())
                geom=QgsGeometry(feature.geometry().constGet().boundary()) if kind=='boundary' else feature.geometry().pointOnSurface()
                if geom.isEmpty():
                    raise RuntimeError(f'{name}生成空几何：{feature["name"]}')
                converted.setGeometry(geom);features.append(converted)
            provider.addFeatures(features);derived.updateExtents()
            options=QgsVectorFileWriter.SaveVectorOptions();options.driverName='GPKG';options.layerName=kind
            result=QgsVectorFileWriter.writeAsVectorFormatV3(derived,str(target),QgsProject.instance().transformContext(),options)
            if result[0] != QgsVectorFileWriter.NoError: raise RuntimeError(result)
            boundary_aliases={'province':['地区界线','线状省界'],'city':['地级界线'],'county':['县级界线']}
            variants.append((kind,name,geometry,category,style,target,derived,boundary_aliases[key] if kind=='boundary' else []))
        for kind,name,geometry,category,style,target,layer,alias in variants:
            folder=output/(key+'-'+kind);folder.mkdir(parents=True,exist_ok=True)
            box=layer.extent()
            manifest={'apiVersion':'layer/v1','id':'layer.system.china-admin.'+key+'-'+kind,'name':name,'aliases':alias,'pack':'全国行政区（系统自带）','category':category,'params':{'level':short} if kind!='seat' else {},'geometry':geometry,'file':os.path.relpath(target,folder),'sublayer':kind if kind!='area' else '', 'declared_crs':'EPSG:4326','crs':'EPSG:4326','coverage':{'bbox':[box.xMinimum(),box.yMinimum(),box.xMaximum(),box.yMaximum()],'regions':[f['name'] for f in source.getFeatures()] if key=='province' else ['中国']},'scale':{},'fields':{'name':'name','code':'adcode'},'label_field':'name' if kind!='boundary' else '', 'default_style':style,'feature_count':layer.featureCount(),'source':{'title':'GeoCode 中国行政区边界','license':'上游数据许可待核实','attribution':'上游声明来自天地图','url':'https://github.com/zzhonglei/GeoCode-Release','version':'6e3534f7af3675b19bd3c5d54c94022959dccdd2'},'compliance':'南海诸岛、钓鱼岛等岛屿完整性未核实；行政区外围线不代表国界。'+('面内代表点，并非真实城市驻地。' if kind=='seat' else ''),'digest':data_digest(target),'preview':'preview.png','created_at':datetime.now(timezone.utc).isoformat()}
            (folder/'layer.yaml').write_text(yaml.safe_dump(manifest,allow_unicode=True,sort_keys=False))
            if style not in style_handles:
                style_handles[style] = resolve_handle(ROOT/'autocarto/resources/system/styles'/style/(style+'.qml'), 'style')
            render_layer(target,folder/'preview.png',{'name':name,'sublayer':manifest['sublayer'],'style':style_handles[style],'label_field':manifest['label_field'],'crs':'EPSG:4326'})
            print(name,manifest['feature_count'],flush=True)

if __name__=='__main__':
    import argparse
    parser=argparse.ArgumentParser();parser.add_argument('--output',type=Path,default=ROOT/'autocarto/data/layers/system/china-admin')
    parser.add_argument('--only',nargs='+',choices=['province-area','province-boundary','city-area','city-boundary','city-seat','county-area','county-boundary','county-seat'])
    args=parser.parse_args()
    with tempfile.TemporaryDirectory(prefix='autocarto-layer-build-') as tmp:
        with isolated_qgis(Path(tmp)):
            build(args.output,args.only)
