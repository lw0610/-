collection = available(ee.ImageCollection(params["dataset"]).filterBounds(roi).filterDate(params["start"], params["end"]).filter(ee.Filter.lte("CLOUDY_PIXEL_PERCENTAGE", params["cloud_max"])))
def cloud_mask(image):
    # SCL excludes saturated pixels, cloud shadow, medium/high cloud and cirrus.
    scl = image.select("SCL")
    mask = scl.neq(0).And(scl.neq(1)).And(scl.neq(3)).And(scl.neq(8)).And(scl.neq(9)).And(scl.neq(10))
    return image.updateMask(mask).select(params["bands"])
image = collection.map(cloud_mask).median()
save_image(image, collection.first().select(params["bands"]))
