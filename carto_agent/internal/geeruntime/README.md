# Earth Engine runtime

This package runs user-requested Earth Engine Python in a separate interpreter
from QGIS. It does not install packages, create environments, run OAuth, or read
credential contents. `ee.Initialize(project=...)` uses the credentials already
configured for that Python/user account.

`Handles(name)` accepts `gee_check_environment`, `gee_run_script`,
`gee_task_status`, and `gee_cancel_task`. `Invoke(ctx, name, args, Options)` returns
a `*Result`; keep this result even when an error is also returned, because it
contains diagnostic logs, retained script paths and any submitted cloud task IDs.

- `gee_check_environment`: no arguments; allowed while disabled. A completed
  diagnostic has `success=true`; check `environment.ready` and `status` to decide
  whether the environment can execute. Missing configuration/dependencies/auth
  include setup guidance, without making any changes automatically.
- `gee_run_script`: `code` must be a nonempty Python string (at most 1 MiB).
- `gee_task_status`: `task_id` is a bare ID or `projects/P/operations/ID`.
  A cloud state of `FAILED`, `CANCELLED`, or `UNKNOWN` has `success=false`.
- `gee_cancel_task`: same ID parameter, explicitly requests cancellation and
  returns `CANCEL_REQUESTED`. Query the status to confirm cancellation.

## Script interface

Scripts have these globals, after Earth Engine initialization succeeds:

```python
# ee: the full official Earth Engine API module
# output_dir: pathlib.Path for this run's unique output directory
image = ee.Image.constant(1)
region = ee.Geometry.Rectangle([116.3, 39.9, 116.31, 39.91])
path = download_image(image, "sample.tif", region=region, scale=30)
print(path)

# For a larger image: returns a real task ID and current state, not a local file.
task = export_image_to_drive(
    image, "example_export", region=region, scale=30, folder="EarthEngineExports"
)
print(task["id"], task["state"])
```

`load_region(local_path)` returns an EE FeatureCollection for one local polygon
feature. As upstream, GeoPandas is required for CRS and topology validation,
including GeoJSON, Shapefile and GeoPackage. Missing dependencies are reported
through `environment.region_ready` and a concrete error, never bypassed with
a weaker check. Coordinates are transformed to EPSG:4326; no invented boundary,
silent geometry repair or implicit installation is used.

`check_coverage(image, roi, band=None, scale=30, *, bands=None)` returns a float
between 0 and 1. Its denominator is an independent constant image over the whole
ROI, never the source image's footprint. `bands` checks the intersection of band
masks. This measures valid coverage at the check scale; it is not a guarantee of
cloud-free data or per-output-pixel quality.

`with heartbeat("stage"):` logs stage progress and elapsed time and publishes
`workflow_progress` events. Its stdout activity resets the script idle timeout, as in GeoCode. These helpers follow GeoCode v0.9.6's workflow; its accompanying
single-date template is adapted under `autocarto/skills/system/gee-scripting/`.
The template selects a bounded set of candidate dates rather than downloading
an unbounded image collection. There is no fixed satellite/date default.

`download_image(image, filename, region=None, scale=None, crs=None,
dimensions=None, *, dtype=None, max_requests=4, max_cpus=1, max_tile_size=16, reuse=True)`
uses pinned geedim 2.0.0 to download and assemble a complete local GeoTIFF.
When `region` is supplied, the helper clips the image to that geometry before
export and cache lookup. The GeoTIFF still has a rectangular pixel grid, but
pixels outside the geometry are NoData; existing source masks and valid zero
values are preserved. A region-less request keeps the image's existing mask.
The 32 MiB EE request limit applies to individual tiles, not the whole file.
Source precision is preserved unless explicitly converted. New files inside `output_dir` are published after all tiles and metadata checks
succeed. Within one session, an exact serialized request can reuse an unchanged
completed file from an earlier run; always use the returned path. Image, region,
scale, CRS, dimensions and dtype participate in identity. Cross-process locks
prevent simultaneous identical downloads; parameter changes and failed files
are not reused. `reuse=False` explicitly refreshes data. This is a local
reliability addition, not a feature copied from GeoCode. It does not resume
partial tiles, migrate old unindexed outputs, or share files across sessions. Failure
is retained even when a user script catches the exception. There is no automatic
Drive fallback. Proxy-aware sessions and byte progress use narrow version-pinned
adapters to the geedim writer.

`export_image_to_drive(image, description, **kwargs)` accepts official
`ee.batch.Export.image.toDrive` arguments, submits once, journals the ID, and reads
the current state. It neither waits for completion nor downloads Drive files.
Direct `ee` export calls bypass that journal; scripts should persist their IDs.

## Execution and artifacts

Each invocation creates a unique `run-*` directory under `Options.OutputDir`
(default `outputs/gee`). A script, bounded stdout/stderr logs, cloud task journal
and produced files are retained; `artifact_paths` lists up to 200 regular files.
Stdout/stderr each retain at most 64 KiB and indicate truncation. For scripts,
the configured timeout (default 300 seconds, up to 3600) measures continuous
stdout/stderr inactivity, including after log truncation. Download helpers emit
progress and periodic heartbeats. Diagnostic/status/cancel operations still use
a total timeout. Context cancellation or
timeout terminates the local process tree. **Already submitted cloud tasks keep
running**; use their journaled IDs to query/cancel explicitly before resubmitting.

Independent process execution is not a security sandbox: Python scripts can use
the current user's filesystem and network permissions. QGIS-specific
`PYTHONHOME`/`PYTHONPATH` are removed to avoid contaminating the configured GEE
interpreter. An explicitly configured HTTP(S) proxy applies only to its child
process; otherwise inherited proxy environment variables remain available.

## Offline validation

`go test ./internal/geeruntime` exercises real child processes against a mock EE
module, including failure/protocol detection, bounded output, cancellation,
process-tree termination, timeout recovery of task IDs, and absolute artifact
paths. It also runs `worker_test.py`, which checks mocked Earth Engine methods,
downloads, path limits, task states, disabled execution and setup diagnostics.
No tests contact Google or require Earth Engine credentials. Python-dependent
tests are skipped if Python is unavailable; the POSIX interpreter fixture is
skipped on Windows.

Official API references (checked 2026-10-06):

- https://developers.google.com/earth-engine/guides/auth
- https://developers.google.com/earth-engine/apidocs/ee-image-getdownloadurl
- https://developers.google.com/earth-engine/apidocs/export-image-todrive
- https://github.com/google/earthengine-api/blob/master/python/ee/data.py
