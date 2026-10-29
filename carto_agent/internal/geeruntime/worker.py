"""Earth Engine worker. Uses user-managed credentials and never installs or authenticates.

Scripts execute with the user's permissions, in a fresh process. This is not a sandbox.
Workflow helpers follow GeoCode v0.9.6; attribution and MIT license are in
autocarto/skills/system/gee-remote-sensing/UPSTREAM.md and LICENSE.
API references: developers.google.com/earth-engine/guides/auth,
apidocs/ee-image-getdownloadurl and apidocs/export-image-todrive.
"""

import contextlib
import hashlib
import importlib
import importlib.metadata
import json
import math
import os
from pathlib import Path
import re
import shutil
import sys
import tempfile
import threading
import time
import traceback
import urllib.parse
import urllib.request

GEEDIM_VERSION = "2.0.0"
DOWNLOAD_LOCK = threading.RLock()
PROGRESS_LOCK = threading.Lock()
TASK_RE = re.compile(r"(?:[A-Za-z0-9_-]+|projects/[A-Za-z0-9._:-]+/operations/[A-Za-z0-9_-]+)\Z")
FAILED_STATES = {"FAILED", "CANCELLED", "UNKNOWN"}


class RuntimeFailure(Exception):
    def __init__(self, code, message):
        super().__init__(message)
        self.code = code


def auth_steps():
    return {
        "install_command": [sys.executable, "-m", "pip", "install", "earthengine-api"],
        "download_install_command": [sys.executable, "-m", "pip", "install", "geedim==" + GEEDIM_VERSION],
        "region_install_command": [sys.executable, "-m", "pip", "install", "geopandas"],
        "authenticate_command": [sys.executable, "-c", "import ee; ee.Authenticate()"],
        "project_step": "Configure a Google Cloud project registered for Earth Engine, with the Earth Engine API enabled and access granted to your account.",
        "credentials_note": "Complete authentication yourself in a terminal/browser. Do not paste tokens or credential file contents into the chat.",
    }


def initialize(project):
    try:
        ee = importlib.import_module("ee")
    except ImportError as exc:
        raise RuntimeFailure("dependency_missing", "The configured Python cannot import earthengine-api. Install it into that interpreter explicitly; see environment setup commands.") from exc
    if not project:
        raise RuntimeFailure("project_missing", "Configure the Google Cloud project ID for Earth Engine before running scripts.")
    try:
        # 'persistent' is the official default: existing EE credentials / application default credentials.
        ee.Initialize(project=project)
    except Exception as exc:
        text = str(exc)
        lowered = text.lower()
        auth_error = any(x in lowered for x in ("credential", "authenticate", "invalid_grant", "refresh token", "unauthorized"))
        error_code = "authentication_required" if auth_error else "initialization_failed"
        raise RuntimeFailure(error_code, "Earth Engine initialization failed: " + text + ". Check existing authentication, project registration/API access, and proxy connectivity. No automatic authentication was attempted.") from exc
    return ee


def operation_name(task_id, project):
    if not isinstance(task_id, str) or not TASK_RE.fullmatch(task_id):
        raise RuntimeFailure("invalid_arguments", "Invalid Earth Engine task ID or operation name")
    return task_id if task_id.startswith("projects/") else "projects/{}/operations/{}".format(project, task_id)


def task_status(ee, name):
    operation = ee.data.getOperation(name)
    metadata = operation.get("metadata", {})
    state = metadata.get("state", "UNKNOWN")
    state = {"PENDING": "READY", "SUCCEEDED": "COMPLETED", "CANCELLING": "CANCEL_REQUESTED"}.get(state, state)
    if state not in {"READY", "RUNNING", "COMPLETED", "FAILED", "CANCEL_REQUESTED", "CANCELLED"}:
        state = "UNKNOWN"
    if operation.get("error"):
        state = "CANCELLED" if state == "CANCELLED" else "FAILED"
    # A terminal response without a known state is still not evidence of success.
    result = {"id": name.rsplit("/", 1)[-1], "name": name, "state": state, "done": bool(operation.get("done", False))}
    if operation.get("error"):
        result["error_message"] = operation["error"].get("message", "Earth Engine task failed")
    for field in ("description", "destinationUris", "createTime", "updateTime"):
        if field in metadata:
            result[field] = metadata[field]
    return result


def safe_output_path(root, filename):
    if not isinstance(filename, (str, os.PathLike)) or not str(filename).strip():
        raise RuntimeFailure("invalid_output_path", "Provide a relative output filename inside output_dir")
    root = Path(root).resolve()
    target = (root / filename).resolve()
    if target == root or root not in target.parents:
        raise RuntimeFailure("invalid_output_path", "Output must stay within this run's output_dir")
    if target.exists():
        raise RuntimeFailure("output_exists", "Output already exists; choose another filename: " + str(target))
    return target


def safe_diagnostic(value):
    # Network exceptions may include signed download URLs. Keep the endpoint,
    # never its query parameters, in journals and parent-visible diagnostics.
    return re.sub(r"https?://[^\s\"'<>]+", lambda m: m.group(0).split("?", 1)[0] + ("?[redacted]" if "?" in m.group(0) else ""), str(value))[:4000]


def append_progress(root, event):
    # Workflow heartbeats and downloader callbacks may run on separate threads.
    with PROGRESS_LOCK:
        with (Path(root) / "progress.jsonl").open("a", encoding="utf-8") as stream:
            stream.write(json.dumps(event, ensure_ascii=False) + "\n")
            stream.flush()


class WorkflowHeartbeat:
    """GeoCode-style periodic status that keeps the idle watchdog alive."""

    def __init__(self, root, stage_id, message, interval=30):
        if isinstance(interval, bool) or not isinstance(interval, (int, float)) or not math.isfinite(interval) or interval <= 0:
            raise RuntimeFailure("invalid_arguments", "heartbeat interval must be a positive number of seconds")
        self.root, self.stage_id = root, stage_id
        self.message = safe_diagnostic(message).replace("\n", " ")[:160]
        self.interval = interval
        self.started = None
        self.stopped = threading.Event()
        self.thread = None

    def elapsed(self):
        return max(0, int(time.monotonic() - self.started)) if self.started is not None else 0

    def emit(self, phase):
        elapsed = self.elapsed()
        append_progress(self.root, {"type": "workflow_progress", "stage_id": self.stage_id, "stage": self.message, "phase": phase, "elapsed_seconds": elapsed})
        if phase == "running":
            print("[{}s] {}".format(elapsed, self.message), flush=True)

    def run(self):
        while not self.stopped.wait(self.interval):
            self.emit("running")

    def __enter__(self):
        if self.started is not None:
            raise RuntimeFailure("invalid_arguments", "Create a new heartbeat context for each operation")
        self.started = time.monotonic()
        self.emit("running")
        self.thread = threading.Thread(target=self.run, daemon=True)
        self.thread.start()
        return self

    def __exit__(self, exception_type, *_):
        self.stopped.set()
        self.thread.join(timeout=1)
        self.emit("failed" if exception_type else "completed")


def validate_region_geometry(geometry, *, geographic=False):
    """Additional polygon/coordinate checks after GeoPandas topology validation."""
    if not isinstance(geometry, dict) or geometry.get("type") not in {"Polygon", "MultiPolygon"}:
        raise RuntimeFailure("invalid_region", "Region must be a non-empty Polygon or MultiPolygon")
    coordinates = geometry.get("coordinates")
    polygons = [coordinates] if geometry["type"] == "Polygon" else coordinates
    if not isinstance(polygons, list) or not polygons:
        raise RuntimeFailure("invalid_region", "Region geometry is empty")
    for polygon in polygons:
        if not isinstance(polygon, list) or not polygon:
            raise RuntimeFailure("invalid_region", "Region polygon has no rings")
        for ring in polygon:
            if not isinstance(ring, list) or len(ring) < 4:
                raise RuntimeFailure("invalid_region", "Each region ring requires at least four coordinates")
            for point in ring:
                if not isinstance(point, (list, tuple)) or len(point) < 2 or any(isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) for value in point):
                    raise RuntimeFailure("invalid_region", "Region coordinates must be finite numeric positions")
                if geographic and (not -180 <= point[0] <= 180 or not -90 <= point[1] <= 90):
                    raise RuntimeFailure("invalid_region", "WGS84 region coordinates exceed longitude/latitude bounds; check the source CRS")
            if ring[0][:2] != ring[-1][:2]:
                raise RuntimeFailure("invalid_region", "Region rings must be closed; repair the geometry in QGIS first")


def load_region_backend():
    try:
        return importlib.import_module("geopandas")
    except ImportError:
        raise RuntimeFailure("region_dependency_missing", "load_region requires geopandas and its geometry/projection dependencies in the configured Python. See environment.region_install_command; nothing was installed automatically.") from None


def read_region_geometry(file_path):
    path = Path(file_path).expanduser().resolve()
    if not path.is_file():
        raise RuntimeFailure("region_not_found", "Region file not found: " + str(path))
    gpd = load_region_backend()
    frame = gpd.read_file(path)
    if frame.crs is None:
        raise RuntimeFailure("invalid_region", "Region has no CRS metadata; assign its actual source CRS in QGIS first")
    if len(frame) != 1:
        raise RuntimeFailure("invalid_region", "load_region requires exactly one feature; dissolve the boundary in QGIS first (got {})".format(len(frame)))
    geometry = frame.geometry.iloc[0]
    if geometry is None or geometry.is_empty or not geometry.is_valid:
        raise RuntimeFailure("invalid_region", "Region geometry is empty or invalid; repair it in QGIS first")
    try:
        frame = frame.to_crs(4326)
    except Exception as exc:
        raise RuntimeFailure("invalid_region", "Could not reproject region to EPSG:4326; export it as WGS84 in QGIS first: " + safe_diagnostic(exc)) from None
    geometry = frame.geometry.iloc[0]
    if geometry is None or geometry.is_empty or not geometry.is_valid:
        raise RuntimeFailure("invalid_region", "Region geometry became invalid after reprojection; export a valid WGS84 boundary in QGIS first")
    # As upstream, discard attributes (dates etc. need not be serializable).
    geojson = json.loads(frame[[frame.geometry.name]].to_json())
    geometry = geojson["features"][0]["geometry"]
    validate_region_geometry(geometry, geographic=True)
    return geometry


def make_workflow_helpers(ee, root):
    stage_id = 0
    stage_lock = threading.Lock()

    def heartbeat(message, interval=30):
        nonlocal stage_id
        with stage_lock:
            stage_id += 1
            return WorkflowHeartbeat(root, stage_id, message, interval)

    def load_region(file_path):
        """Return a single local polygon boundary as a WGS84 ee.FeatureCollection.

        GeoPandas follows the upstream reprojection/topology checks. The input
        must contain one valid polygon; no geometries are silently fixed.
        """
        with heartbeat("Loading region: " + Path(file_path).name):
            geometry = read_region_geometry(file_path)
            return ee.FeatureCollection([{"type": "Feature", "geometry": geometry, "properties": {}}])

    def check_coverage(image, roi, band=None, scale=30, *, bands=None):
        """Return valid pixel fraction in [0, 1], using the entire ROI denominator.

        The GeoCode band/scale API and float result are preserved. Supplying
        bands checks their common valid mask, for example all three RGB bands.
        This is a scan at scale metres, not a full-resolution quality guarantee.
        """
        if isinstance(scale, bool) or not isinstance(scale, (int, float)) or not math.isfinite(scale) or scale <= 0:
            raise RuntimeFailure("invalid_arguments", "coverage scale must be a positive number of metres")
        if band is not None and bands is not None:
            raise RuntimeFailure("invalid_arguments", "Specify band or bands, not both")
        if band is not None and (not isinstance(band, str) or not band):
            raise RuntimeFailure("invalid_arguments", "band must be a non-empty band name")
        if bands is not None and (not isinstance(bands, (list, tuple)) or not bands or any(not isinstance(value, str) or not value for value in bands)):
            raise RuntimeFailure("invalid_arguments", "bands must be a non-empty list of band names")
        requested = list(bands) if bands is not None else [band] if band is not None else None
        with heartbeat("Checking coverage"):
            if requested is not None:
                available = image.bandNames().getInfo()
                missing = [value for value in requested if value not in available]
                if missing:
                    raise RuntimeFailure("invalid_arguments", "Coverage bands not found: {}. Available: {}".format(", ".join(missing), ", ".join(available)))
            selected = image.select(requested if requested is not None else [0])
            projection = selected.select([0]).projection()
            valid = selected.mask().reduce(ee.Reducer.min()).unmask(0, sameFootprint=False).gt(0).rename("valid")
            # The denominator has the full ROI footprint, even when a single
            # scene covers only part of it. Both bands share the sampling grid.
            whole = ee.Image.constant(1).rename("all").reproject(projection)
            stats = valid.addBands(whole).reduceRegion(reducer=ee.Reducer.sum(), geometry=roi, scale=scale, maxPixels=1e13).getInfo()
            valid_pixels = float(stats.get("valid") or 0)
            total_pixels = float(stats.get("all") or 0)
            if not math.isfinite(valid_pixels) or not math.isfinite(total_pixels) or valid_pixels < 0 or total_pixels < 0 or valid_pixels > total_pixels:
                raise RuntimeFailure("coverage_invalid", "Earth Engine returned inconsistent valid/total pixel counts")
            coverage = valid_pixels / total_pixels if total_pixels else 0.0
        print("Coverage: {:.1%} | valid: {}/{}".format(coverage, valid_pixels, total_pixels), flush=True)
        return round(coverage, 4)

    return {"load_region": load_region, "check_coverage": check_coverage, "heartbeat": heartbeat}


def load_download_backend():
    try:
        version = importlib.metadata.version("geedim")
        if version != GEEDIM_VERSION:
            raise RuntimeFailure("download_dependency_version", "Local raster downloads require geedim=={}; found {}. Install the pinned version in the configured interpreter.".format(GEEDIM_VERSION, version))
        importlib.import_module("geedim")  # registers the ee.Image.gd accessor
        return importlib.import_module("geedim.image"), importlib.import_module("rasterio")
    except RuntimeFailure:
        raise
    except (ImportError, importlib.metadata.PackageNotFoundError) as exc:
        raise RuntimeFailure("download_dependency_missing", "Local raster downloads require geedim=={} and its rasterio dependencies in the configured interpreter. Use environment.download_install_command; nothing was installed automatically.".format(GEEDIM_VERSION)) from None


@contextlib.contextmanager
def proxy_aware_download_session(gd_image):
    # geedim 2.0.0's runner creates aiohttp with trust_env=False. Give only this
    # serialized download a session which honours the same environment/system
    # proxies as EE authentication. Restore the runner even when a tile fails.
    # This narrow adapter is version-pinned along with ReportingTiler below.
    runner = gd_image.utils.AsyncRunner()
    previous = runner._session

    async def create_session():
        aiohttp = gd_image.utils.aiohttp
        timeout = aiohttp.ClientTimeout(total=300, sock_connect=30, ceil_threshold=5)
        return aiohttp.ClientSession(raise_for_status=True, timeout=timeout, trust_env=True)

    session = runner.run(create_session())
    runner._session = session
    try:
        yield
    finally:
        runner._session = previous
        runner.run(session.close())


class DownloadProgress:
    def __init__(self, root, record):
        self.path = root / "progress.jsonl"
        self.record = record
        self.lock = threading.Lock()
        self.completed = 0
        self.total = None
        self.last_write = 0.0
        self.last_phase = None

    def emit(self, phase, percent=None, **extra):
        with self.lock:
            now = time.monotonic()
            if phase == self.last_phase == "downloading" and now - self.last_write < 1.0:
                return
            event = {"type": "download_progress", "download_id": self.record["download_id"], "filename": self.record["filename"], "phase": phase, "percent": percent}
            if self.total is not None:
                event.update(completed_bytes=self.completed, total_bytes=self.total)
            event.update(extra)
            append_progress(self.path.parent, event)
            print("GEE {}: {}{}".format(phase, self.record["filename"], " ({:.1f}%)".format(percent) if percent is not None else ""), flush=True)
            self.last_write, self.last_phase = now, phase

    def wrote(self, size):
        with self.lock:
            self.completed += int(size)
            # Completion is only published after the GeoTIFF closes and validates.
            percent = min(99.0, 100.0 * self.completed / self.total) if self.total else None
        self.emit("downloading", percent)

    @contextlib.contextmanager
    def heartbeat(self):
        stopped = threading.Event()

        def report():
            while not stopped.wait(30):
                print("GEE download active: " + self.record["filename"], flush=True)

        thread = threading.Thread(target=report, daemon=True)
        thread.start()
        try:
            yield
        finally:
            stopped.set()
            thread.join(timeout=1)


def download_fingerprint(image, region, scale, crs, dimensions, dtype):
    """Exact serialized request identity, never a guess based on filename/date."""
    def expression(value):
        if hasattr(value, "serialize"):
            return json.loads(value.serialize())
        return value

    if not callable(getattr(image, "serialize", None)):
        return None
    try:
        request = {"version": 1, "geedim": GEEDIM_VERSION, "image": expression(image),
                   "region": expression(region), "scale": scale, "crs": crs,
                   "dimensions": dimensions, "dtype": dtype}
        encoded = json.dumps(request, sort_keys=True, separators=(",", ":"), allow_nan=False)
        return hashlib.sha256(encoded.encode("utf-8")).hexdigest()
    except (TypeError, ValueError, AttributeError):
        # An unrepresentable request can still download, but cannot be reused.
        return None


@contextlib.contextmanager
def completed_download_lock(directory, key):
    """OS-owned lock: competing processes share one product; crashes release it."""
    directory.mkdir(parents=True, exist_ok=True)
    with (directory / (key + ".lock")).open("a+b") as stream:
        if os.name == "nt":
            import msvcrt
            stream.seek(0)
            stream.write(b"\0")
            stream.flush()
            while True:
                try:
                    stream.seek(0)
                    msvcrt.locking(stream.fileno(), msvcrt.LK_NBLCK, 1)
                    break
                except OSError:
                    time.sleep(0.2)
            try:
                yield
            finally:
                stream.seek(0)
                msvcrt.locking(stream.fileno(), msvcrt.LK_UNLCK, 1)
        else:
            import fcntl
            fcntl.flock(stream.fileno(), fcntl.LOCK_EX)
            try:
                yield
            finally:
                fcntl.flock(stream.fileno(), fcntl.LOCK_UN)


def reusable_download(manifest, download_root):
    try:
        if manifest.stat().st_size > 65536:
            return None
        saved = json.loads(manifest.read_text(encoding="utf-8"))
        record = saved["record"]
        path = Path(record["path"])
        resolved = path.resolve(strict=True)
        if path.is_symlink() or download_root not in resolved.parents or not resolved.is_file():
            return None
        stat = resolved.stat()
        signature = {"size": stat.st_size, "mtime_ns": stat.st_mtime_ns,
                     "ctime_ns": stat.st_ctime_ns, "device": stat.st_dev, "inode": stat.st_ino}
        if record["status"] != "completed" or any(saved.get(key) != value for key, value in signature.items()):
            return None
        return record
    except (OSError, ValueError, KeyError, TypeError):
        return None


def journal_download(root, record):
    with (root / "downloads.jsonl").open("a", encoding="utf-8") as stream:
        stream.write(json.dumps(record, ensure_ascii=False) + "\n")
        stream.flush()


def validate_dimensions(dimensions):
    if dimensions is None:
        return
    if isinstance(dimensions, int) and not isinstance(dimensions, bool):
        dims = [dimensions]
    elif isinstance(dimensions, str) and re.fullmatch(r"\d+(?:x\d+)?", dimensions):
        dims = [int(v) for v in dimensions.split("x")]
    elif isinstance(dimensions, (tuple, list)):
        dims = list(dimensions)
    else:
        raise RuntimeFailure("invalid_arguments", "dimensions must be an integer, WIDTHxHEIGHT, or [width, height]")
    if not 1 <= len(dims) <= 2 or any(not isinstance(v, int) or isinstance(v, bool) or v <= 0 or v > 10000 for v in dims):
        raise RuntimeFailure("invalid_arguments", "Explicit dimensions retain the 1..10000 limit per dimension; use region and scale for larger automatically tiled downloads")


def validate_download_arguments(scale, dimensions, dtype, max_requests, max_cpus, max_tile_size):
    if dtype is not None and dtype not in {"uint8", "int8", "uint16", "int16", "uint32", "int32", "float32", "float64"}:
        raise RuntimeFailure("invalid_arguments", "Unsupported explicit raster dtype")
    for value, limit, name in ((max_requests, 16, "max_requests"), (max_cpus, 4, "max_cpus")):
        if isinstance(value, bool) or not isinstance(value, int) or not 1 <= value <= limit:
            raise RuntimeFailure("invalid_arguments", "{} must be an integer between 1 and {}".format(name, limit))
    if isinstance(max_tile_size, bool) or not isinstance(max_tile_size, (int, float)) or not math.isfinite(max_tile_size) or not 0 < max_tile_size <= 32:
        raise RuntimeFailure("invalid_arguments", "max_tile_size must be greater than 0 and at most 32 MiB")
    if scale is not None and (isinstance(scale, bool) or not isinstance(scale, (int, float)) or not math.isfinite(scale) or scale <= 0):
        raise RuntimeFailure("invalid_arguments", "scale must be a positive number of metres")
    validate_dimensions(dimensions)


def download_raster(image, target, region, scale, crs, dimensions, dtype, max_requests, max_cpus, max_tile_size, record, progress):
    """Use the pinned geedim export grid, type-aware tiler and concurrent writer."""
    gd_image, rio = load_download_backend()
    validate_download_arguments(scale, dimensions, dtype, max_requests, max_cpus, max_tile_size)
    if dimensions is not None:
        # Preserve Earth Engine's scalar / WIDTHxHEIGHT semantics, including
        # aspect ratio for a scalar, rather than silently making a square grid.
        params = {k: v for k, v in {"region": region, "scale": scale, "crs": crs, "dimensions": dimensions}.items() if v is not None}
        image, _ = image.prepare_for_export(params)
        prepared = image.gd.prepareForExport(dtype=dtype, scale_offset=False)
    else:
        prepared = image.gd.prepareForExport(region=region, scale=scale, crs=crs, dtype=dtype, scale_offset=False)
    accessor = prepared.gd
    height, width = accessor.shape
    record.update(dtype=accessor.dtype, width=int(width), height=int(height), bands=int(accessor.count), crs=str(accessor.crs))
    progress.total = int(accessor.size)
    progress.emit("downloading", 0.0)
    target.parent.mkdir(parents=True, exist_ok=True)
    temporary_dir = Path(tempfile.mkdtemp(prefix="_gee_download_", dir=target.parent))
    temporary = temporary_dir / (target.name + ".part")
    original_tiler = gd_image.Tiler

    class ReportingTiler(original_tiler):
        def map_tiles(self, func, masked=False):
            def write_and_report(tile, array):
                func(tile, array)
                progress.wrote(array.nbytes)
            return super().map_tiles(write_and_report, masked=masked)

    try:
        # Pinned 2.0.0 calls this documented Tiler.map_tiles callback after each
        # complete tile. Only wrap reporting; geedim owns splitting/retries/writes.
        gd_image.Tiler = ReportingTiler
        with proxy_aware_download_session(gd_image):
            accessor.toGeoTIFF(temporary, overwrite=False, max_tile_size=max_tile_size, max_requests=max_requests, max_cpus=max_cpus)
        progress.emit("validating", 99.0)
        with rio.open(temporary) as dataset:
            if dataset.shape != (height, width) or dataset.count != accessor.count:
                raise RuntimeFailure("download_incomplete", "Downloaded raster dimensions or band count do not match the requested grid")
            if any(value != accessor.dtype for value in dataset.dtypes):
                raise RuntimeFailure("download_incomplete", "Downloaded raster dtype does not match the prepared image")
            if dataset.crs != rio.crs.CRS.from_user_input(accessor.crs):
                raise RuntimeFailure("download_incomplete", "Downloaded raster CRS does not match the prepared image")
            expected_transform = rio.Affine(*accessor.transform)
            if not dataset.transform.almost_equals(expected_transform):
                raise RuntimeFailure("download_incomplete", "Downloaded raster transform does not match the requested grid")
        if progress.completed != progress.total:
            raise RuntimeFailure("download_incomplete", "Not all raster bytes were written by the tiled downloader")
        record["bytes"] = temporary.stat().st_size
        # Publish exclusively after every tile and metadata check succeeds.
        os.link(temporary, target)
        record.update(status="completed", path=str(target))
        progress.emit("completed", 100.0)
        return str(target)
    finally:
        gd_image.Tiler = original_tiler
        shutil.rmtree(temporary_dir, ignore_errors=True)


def make_helpers(ee, root, tasks, downloads=None, *, download_root=None):
    root = Path(root).resolve()
    downloads = [] if downloads is None else downloads
    download_root = Path(download_root).resolve() if download_root else None
    if download_root is not None and download_root != root.parent:
        raise RuntimeFailure("invalid_output_path", "Download reuse is restricted to this session's GEE output directory")

    def download_image(image, filename, region=None, scale=None, crs=None, dimensions=None, *, dtype=None, max_requests=4, max_cpus=1, max_tile_size=16, reuse=True):
        """Download one complete local GeoTIFF with geedim 2.0.0's tiled writer.

        Region masks pixels outside the requested geometry; source masks and zero
        values are preserved. Scale and source precision are preserved unless
        dtype is explicitly given.
        dimensions keeps the legacy <=10000-per-dimension contract; use region and
        scale for larger rasters. Failures remain recorded even if scripts catch
        their exceptions. No Drive export or automatic installation is attempted.
        """
        with DOWNLOAD_LOCK:
            record = {"download_id": len(downloads) + 1, "filename": str(filename), "status": "preparing"}
            downloads.append(record)
            progress = DownloadProgress(root, record)
            try:
                target = safe_output_path(root, filename)
                if target.suffix.lower() not in (".tif", ".tiff"):
                    raise RuntimeFailure("invalid_output_path", "download_image requires a .tif or .tiff filename")
                validate_download_arguments(scale, dimensions, dtype, max_requests, max_cpus, max_tile_size)
                if not isinstance(reuse, bool):
                    raise RuntimeFailure("invalid_arguments", "reuse must be a boolean")
                progress.emit("preparing")
                journal_download(root, record)
                if region is not None:
                    image = image.clip(region)
                key = download_fingerprint(image, region, scale, crs, dimensions, dtype) if download_root else None
                cache = download_root / ".gee-downloads" if key else None
                with progress.heartbeat(), completed_download_lock(cache, key) if key else contextlib.nullcontext():
                    manifest = cache / (key + ".json") if key else None
                    saved = reusable_download(manifest, download_root) if key and reuse else None
                    if saved:
                        identity = {"download_id": record["download_id"], "filename": record["filename"]}
                        record.update(saved)
                        record.update(identity, reused=True)
                        print("Reusing completed GEE file: " + record["path"], flush=True)
                        journal_download(root, record)
                        progress.emit("completed", 100.0, reused=True)
                        return record["path"]
                    path = download_raster(image, target, region, scale, crs, dimensions, dtype, max_requests, max_cpus, max_tile_size, record, progress)
                    journal_download(root, record)
                    if manifest:
                        try:
                            stat = Path(path).stat()
                            saved = {"record": dict(record), "size": stat.st_size, "mtime_ns": stat.st_mtime_ns,
                                     "ctime_ns": stat.st_ctime_ns, "device": stat.st_dev, "inode": stat.st_ino}
                            temporary = manifest.with_suffix(".tmp")
                            temporary.write_text(json.dumps(saved, ensure_ascii=False), encoding="utf-8")
                            temporary.replace(manifest)
                        except OSError as exc:
                            # The raster is already complete. Optional reuse metadata
                            # failure must not turn delivery into another download.
                            record["reuse_warning"] = safe_diagnostic(exc)
                            print("Completed GEE file retained; reuse index unavailable: " + safe_diagnostic(exc), flush=True)
                    return path
            except BaseException as exc:
                code = exc.code if isinstance(exc, RuntimeFailure) else "download_failed"
                message = safe_diagnostic(exc)
                record.update(status="failed", error_code=code, message=message)
                journal_download(root, record)
                progress.emit("failed", message=message)
                raise RuntimeFailure(code, message) from None

    def export_image_to_drive(image, description, **kwargs):
        """Submit a Drive export; return the real task ID/state, never imply completion.

        kwargs are the official ee.batch.Export.image.toDrive parameters (region,
        scale, folder, crs, maxPixels, etc.). Poll with gee_task_status. Local process
        cancellation does not cancel the cloud task.
        """
        task = ee.batch.Export.image.toDrive(image=image, description=description, **kwargs)
        task.start()
        if not task.id:
            raise RuntimeFailure("export_submission_unknown", "Export call returned without a task ID. Check Earth Engine tasks before retrying to avoid duplicate exports.")
        record = {"id": task.id, "name": getattr(task, "name", None), "state": "SUBMITTED", "description": description}
        tasks.append(record)
        # Journal immediately: status may block or the worker may be cancelled afterwards.
        with (root / "tasks.jsonl").open("a", encoding="utf-8") as output:
            output.write(json.dumps(record) + "\n")
            output.flush()
            os.fsync(output.fileno())
        record.update(task.status())
        record["id"] = task.id
        print("Earth Engine export task {}: {} (cloud export; no local file yet)".format(task.id, record.get("state", "UNKNOWN")), flush=True)
        if record.get("state", "UNKNOWN") in FAILED_STATES:
            raise RuntimeFailure("export_failed", "Export task {} is {}: {}".format(task.id, record.get("state"), record.get("error_message", "")))
        return dict(record)

    return download_image, export_image_to_drive


def make_feature_downloader(root, downloads):
    root = Path(root).resolve()

    def safe_download_url(address):
        parsed = urllib.parse.urlparse(address)
        hostname = (parsed.hostname or "").lower()
        allowed = ("googleapis.com", "googleusercontent.com", "earthengine.google.com")
        if parsed.scheme != "https" or parsed.username or parsed.password or not any(hostname == domain or hostname.endswith("." + domain) for domain in allowed):
            raise RuntimeFailure("invalid_download_url", "Feature export URL is outside Earth Engine download services")
        return address

    class GoogleRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, req, fp, code, msg, headers, newurl):
            safe_download_url(newurl)
            return super().redirect_request(req, fp, code, msg, headers, newurl)

    def download_features(collection, filename, *, max_features=5000, max_bytes=25 * 1024 * 1024):
        if isinstance(max_features, bool) or not isinstance(max_features, int) or not 1 <= max_features <= 50000:
            raise RuntimeFailure("invalid_arguments", "max_features must be 1..50000")
        if isinstance(max_bytes, bool) or not isinstance(max_bytes, int) or not 1 <= max_bytes <= 100 * 1024 * 1024:
            raise RuntimeFailure("invalid_arguments", "max_bytes must be 1..100 MiB")
        target = safe_output_path(root, filename)
        if target.suffix.lower() != ".geojson":
            raise RuntimeFailure("invalid_output_path", "download_features requires a .geojson filename")
        record = {"download_id": len(downloads) + 1, "filename": str(filename), "status": "preparing", "kind": "vector"}
        downloads.append(record)
        temporary = None
        try:
            count = collection.limit(max_features + 1).size().getInfo()
            if not isinstance(count, int) or count < 1 or count > max_features:
                raise RuntimeFailure("feature_limit", "Feature count is empty or exceeds the allowed bound")
            address = safe_download_url(collection.getDownloadURL(filetype="geojson", filename=target.stem))
            target.parent.mkdir(parents=True, exist_ok=True)
            descriptor, temporary = tempfile.mkstemp(prefix=".gee-features-", suffix=".geojson", dir=target.parent)
            size = 0
            digest = hashlib.sha256()
            progress = DownloadProgress(root, record)
            opener = urllib.request.build_opener(GoogleRedirect())
            with os.fdopen(descriptor, "wb") as output, progress.heartbeat(), opener.open(address, timeout=120) as response:
                safe_download_url(response.geturl())
                while True:
                    block = response.read(min(1024 * 1024, max_bytes - size + 1))
                    if not block:
                        break
                    size += len(block)
                    if size > max_bytes:
                        raise RuntimeFailure("feature_size_limit", "Feature export exceeds the byte limit; reduce extent")
                    output.write(block)
                    digest.update(block)
            document = json.loads(Path(temporary).read_text(encoding="utf-8"))
            if document.get("type") != "FeatureCollection" or len(document.get("features", [])) != count:
                raise RuntimeFailure("feature_export_incomplete", "Feature export count does not match the requested collection")
            os.link(temporary, target)
            record.update(status="completed", path=str(target), feature_count=count, bytes=size, sha256=digest.hexdigest())
            journal_download(root, record)
            progress.emit("completed", 100.0)
            return str(target)
        except Exception as exc:
            code = exc.code if isinstance(exc, RuntimeFailure) else "feature_download_failed"
            message = safe_diagnostic(exc)
            record.update(status="failed", error_code=code, message=message)
            journal_download(root, record)
            raise RuntimeFailure(code, message) from None
        finally:
            if temporary:
                Path(temporary).unlink(missing_ok=True)

    return download_features


def execute(payload):
    operation = payload["operation"]
    tasks = []
    downloads = []
    response = {"success": False, "status": "failed", "tasks": tasks, "downloads": downloads}
    environment = {"python": sys.executable, "project": payload.get("project", ""), "ready": False, **auth_steps()}
    response["environment"] = environment
    try:
        if operation != "gee_check_environment" and not payload.get("enabled"):
            raise RuntimeFailure("disabled", "Earth Engine execution is disabled")
        ee = initialize(payload.get("project", ""))
        environment.update({"ready": True, "earthengine_api_version": getattr(ee, "__version__", "unknown")})
        if operation == "gee_check_environment":
            # EE queries do not need the raster downloader or region loader.
            # Report each capability without installing anything implicitly.
            environment.update(download_ready=False, geedim_required_version=GEEDIM_VERSION)
            try:
                load_download_backend()
                environment.update(download_ready=True, geedim_version=GEEDIM_VERSION)
            except Exception as exc:
                environment.update(download_dependency_error=safe_diagnostic(exc), download_dependency_error_code=exc.code if isinstance(exc, RuntimeFailure) else "download_dependency_unavailable")
            environment["region_ready"] = False
            try:
                backend = load_region_backend()
                environment.update(region_ready=True, geopandas_version=getattr(backend, "__version__", "unknown"))
            except Exception as exc:
                environment.update(region_dependency_error=safe_diagnostic(exc), region_dependency_error_code=exc.code if isinstance(exc, RuntimeFailure) else "region_dependency_unavailable")
            response.update(success=True, status="ready", message="Earth Engine initialized using existing user credentials. Execution remains disabled." if not payload.get("enabled") else "Earth Engine initialized using existing user credentials.")
            if not environment["download_ready"]:
                response.update(status="ready_without_local_download", message=response["message"] + " Earth Engine queries are ready, but local raster downloads require the configured geedim dependency; see environment.download_dependency_error.")
            if not environment["region_ready"]:
                response["message"] += " Loading local boundaries requires geopandas; see environment.region_dependency_error."
                if environment["download_ready"]:
                    response["status"] = "ready_without_region_loading"
        elif operation == "gee_run_script":
            output_dir = Path(payload["output_dir"]).resolve()
            download, export = make_helpers(ee, output_dir, tasks, downloads, download_root=payload.get("download_root"))
            script = Path(payload["script_path"])
            scope = {"__name__": "__main__", "__file__": str(script), "ee": ee, "output_dir": output_dir, "download_image": download, "export_image_to_drive": export}
            scope.update(make_workflow_helpers(ee, output_dir))
            scope["download_features"] = make_feature_downloader(output_dir, downloads)
            exec(compile(script.read_text(encoding="utf-8"), str(script), "exec"), scope)
            if any(item.get("status") != "completed" for item in downloads):
                raise RuntimeFailure("download_incomplete", "One or more requested local raster downloads failed. Caught exceptions or submitted cloud exports do not make local delivery complete; inspect downloads and retry failed outputs explicitly.")
            if any(task.get("state", "UNKNOWN") in FAILED_STATES for task in tasks):
                raise RuntimeFailure("export_failed", "At least one submitted export failed or has unknown state. Inspect tasks before retrying.")
            response.update(success=True, status="script_completed", message="Python script finished. Inspect outputs for scientific validity.")
            if tasks:
                response.update(status="exports_submitted", message="Script finished; see individual cloud task states. Submitted/running exports are not completed files; use gee_task_status.")
        elif operation in ("gee_task_status", "gee_cancel_task"):
            name = operation_name(payload.get("task_id"), payload["project"])
            if operation == "gee_cancel_task":
                ee.data.cancelOperation(name)
                response.update(success=True, status="CANCEL_REQUESTED", task={"id": name.rsplit("/", 1)[-1], "name": name, "state": "CANCEL_REQUESTED"}, message="Cancellation was requested from Earth Engine. Use gee_task_status to confirm the final state.")
            else:
                response["task"] = task_status(ee, name)
                state = response["task"]["state"]
                response.update(success=state not in FAILED_STATES, status=state, message="Earth Engine task state: " + state)
                if state in FAILED_STATES:
                    response.update(error_code="cloud_task_failed", message=response["task"].get("error_message", "Cloud task state: " + state))
        else:
            raise RuntimeFailure("unknown_tool", "Unknown Earth Engine operation")
    except BaseException as exc:
        code = exc.code if isinstance(exc, RuntimeFailure) else "script_failed"
        response.update(success=False, status="incomplete" if code == "download_incomplete" else "failed", error_code=code, message=safe_diagnostic(exc) or type(exc).__name__)
        if operation == "gee_check_environment":
            response.update(success=True, status=response["error_code"])
        else:
            print(safe_diagnostic("".join(traceback.format_exception(type(exc), exc, exc.__traceback__))), file=sys.stderr)
    return response


def main():
    payload = json.load(sys.stdin)
    response = execute(payload)
    destination = Path(payload["result_path"])
    destination.write_text(json.dumps(response, ensure_ascii=False), encoding="utf-8")
    return 0 if response["success"] else 1


if __name__ == "__main__":
    sys.exit(main())
