import inspect
print('===== download_image source =====')
try:
    print(inspect.getsource(download_image))
except Exception as e:
    print('ERR', e)
print('===== export_image_to_drive source =====')
try:
    print(inspect.getsource(export_image_to_drive))
except Exception as e:
    print('ERR', e)
