"""Trusted acquisition template prelude; parameters are JSON supplied by Go."""
import json

if params.get("region_path"):
    with open(params["region_path"], encoding="utf-8") as source:
        region_document = json.load(source)
    declared_crs = region_document.get("crs")
    if declared_crs and "4326" not in json.dumps(declared_crs) and "CRS84" not in json.dumps(declared_crs):
        raise ValueError("获取边界须为 WGS84 GeoJSON")
    roi = ee.FeatureCollection(region_document).geometry()
else:
    roi = ee.Geometry.Rectangle(params["bbox"], "EPSG:4326", False)


def available(collection):
    if collection.size().getInfo() < 1:
        raise ValueError("所选时段和范围没有可用影像")
    return collection


def save_image(image, reference):
    image = image.setDefaultProjection(reference.select([0]).projection())
    projection = reference.select([0]).projection().crs().getInfo()
    path = download_image(image, "acquired.tif", region=roi, scale=params["scale"], crs=projection)
    print("已导出获取结果: " + path, flush=True)
