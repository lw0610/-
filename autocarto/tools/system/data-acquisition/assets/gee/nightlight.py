collection = available(ee.ImageCollection(params["dataset"]).filterBounds(roi).filterDate(params["start"], params["end"]))
def valid_observations(image):
    return image.updateMask(image.select("cf_cvg").gt(0)).select(params["bands"])
save_image(collection.map(valid_observations).mean(), collection.first().select(params["bands"]))
