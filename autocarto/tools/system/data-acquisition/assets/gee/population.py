collection = available(ee.ImageCollection(params["dataset"]).filterBounds(roi).filter(ee.Filter.eq("year", params["year"])).select(params["bands"]))
save_image(collection.mosaic(), collection.first())
