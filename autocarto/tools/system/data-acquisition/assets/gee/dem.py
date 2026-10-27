collection = available(ee.ImageCollection(params["dataset"]).filterBounds(roi).select(params["bands"]))
save_image(collection.mosaic(), collection.first())
