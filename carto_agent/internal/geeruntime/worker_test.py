"""Offline tests: no Earth Engine account, SDK, or Google network calls required."""
import contextlib
import asyncio
import importlib.util
import io
import json
import os
import subprocess
import sys
from pathlib import Path
import tempfile
import threading
import time
import types
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("gee_worker", Path(__file__).with_name("worker.py"))
worker = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(worker)


class RasterFixture:
    """Simulate the downloader boundary without an EE account or large allocation."""
    def __init__(self, root, shape=(2200, 2200), dtype="float64", count=3):
        self.root, self.shape, self.dtype, self.count = root, shape, dtype, count
        self.crs = "EPSG:32650"
        self.transform = [10, 0, 250000, 0, -10, 3300000]
        self.prepare_calls, self.export_calls, self.download_calls = [], [], []
        self.failure = None
        self.omit_last = False
        self.invalid_dataset = None
        self.original_tiler = type("FakeTiler", (), {"map_tiles": self.map_tiles})
        self.module = types.SimpleNamespace(Tiler=self.original_tiler)
        self.image = types.SimpleNamespace(gd=self, prepare_for_export=self.prepare_for_export)
        self.rio = types.SimpleNamespace(open=self.open, Affine=lambda *args: tuple(args), crs=types.SimpleNamespace(CRS=types.SimpleNamespace(from_user_input=lambda value: value)))

    @property
    def size(self):
        return self.shape[0] * self.shape[1] * self.count * {"float64": 8, "float32": 4, "uint16": 2}[self.dtype]

    def prepareForExport(self, **kwargs):
        self.prepare_calls.append(kwargs)
        if kwargs.get("dtype"):
            self.dtype = kwargs["dtype"]
        return self.image

    def prepare_for_export(self, params):
        self.export_calls.append(params)
        return self.image, {}

    def map_tiles(self, func, masked=False):
        # Each array is a complete written tile; total exceeds the former 32 MiB
        # whole-file cap and is computed from actual 64-bit source precision.
        chunks = [self.size // 2, self.size - self.size // 2]
        for index, size in enumerate(chunks):
            if index == 1 and self.failure:
                raise self.failure
            if index == 1 and self.omit_last:
                return
            func(index, types.SimpleNamespace(nbytes=size))

    def toGeoTIFF(self, path, **kwargs):
        self.download_calls.append(kwargs)
        assert not (self.root / "whole.tif").exists(), "published before completion"
        Path(path).write_bytes(b"II*\x00synthetic raster")
        self.module.Tiler().map_tiles(lambda tile, array: None)

    @contextlib.contextmanager
    def open(self, path):
        dataset = types.SimpleNamespace(shape=self.shape, count=self.count, dtypes=[self.dtype] * self.count, crs=self.crs, transform=types.SimpleNamespace(almost_equals=lambda value: value == tuple(self.transform)))
        if self.invalid_dataset:
            setattr(dataset, *self.invalid_dataset)
        yield dataset


class WorkerTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.ee = types.SimpleNamespace(Initialize=lambda **kwargs: None, __version__="mock")
        self.payload = {"operation": "gee_run_script", "enabled": True, "project": "test-project", "output_dir": str(self.root), "script_path": str(self.root / "script.py")}
        self.mock_import = patch.object(worker.importlib, "import_module", return_value=self.ee)
        self.mock_import.start()
        self.addCleanup(self.mock_import.stop)

    def run_script(self, source):
        Path(self.payload["script_path"]).write_text(source, encoding="utf-8")
        with contextlib.redirect_stderr(io.StringIO()), contextlib.redirect_stdout(io.StringIO()):
            return worker.execute(self.payload)

    def mock_raster(self, **kwargs):
        fixture = RasterFixture(self.root, **kwargs)
        for mock in (patch.object(worker, "load_download_backend", return_value=(fixture.module, fixture.rio)), patch.object(worker, "proxy_aware_download_session", side_effect=lambda _: contextlib.nullcontext())):
            mock.start()
            self.addCleanup(mock.stop)
        self.ee.Image = lambda: fixture.image
        return fixture

    def test_initialized_script_and_artifact(self):
        calls = []
        self.ee.Initialize = lambda **kwargs: calls.append(kwargs)
        result = self.run_script("(output_dir / 'table.csv').write_text('value\\n1\\n')")
        self.assertTrue(result["success"])
        self.assertEqual(calls, [{"project": "test-project"}])
        self.assertTrue((self.root / "table.csv").is_file())

    def test_workflow_helpers_are_available_to_scripts(self):
        result = self.run_script("assert callable(load_region) and callable(check_coverage)\nwith heartbeat('Preparing imagery'):\n    (output_dir / 'helper.txt').write_text('ready')")
        self.assertTrue(result["success"], result)
        events = [json.loads(line) for line in (self.root / "progress.jsonl").read_text().splitlines()]
        self.assertEqual([event["phase"] for event in events], ["running", "completed"])

    def test_failure_and_system_exit_are_not_success(self):
        for script in ("raise ValueError('bad geometry')", "raise SystemExit(0)"):
            with self.subTest(script=script):
                result = self.run_script(script)
                self.assertFalse(result["success"])
                self.assertEqual(result["error_code"], "script_failed")

    def test_disabled_execution_never_initializes(self):
        self.payload["enabled"] = False
        self.ee.Initialize = lambda **kwargs: self.fail("disabled execution initialized EE")
        result = self.run_script("raise AssertionError('must not run')")
        self.assertEqual(result["error_code"], "disabled")

    def test_disabled_diagnostic_reports_dependency_and_auth_steps(self):
        self.payload.update(operation="gee_check_environment", enabled=False)
        with patch.object(worker.importlib, "import_module", side_effect=ImportError("missing ee")):
            result = worker.execute(self.payload)
        self.assertTrue(result["success"])
        self.assertFalse(result["environment"]["ready"])
        self.assertEqual(result["status"], "dependency_missing")
        self.assertIn("ee.Authenticate()", result["environment"]["authenticate_command"][-1])

    def test_environment_distinguishes_query_readiness_from_local_downloads(self):
        self.payload.update(operation="gee_check_environment")
        with patch.object(worker, "load_download_backend", side_effect=worker.RuntimeFailure("download_dependency_missing", "geedim==2.0.0 is missing")):
            result = worker.execute(self.payload)
        self.assertTrue(result["success"])
        self.assertTrue(result["environment"]["ready"])
        self.assertFalse(result["environment"]["download_ready"])
        self.assertEqual(result["status"], "ready_without_local_download")
        self.assertEqual(result["environment"]["download_dependency_error_code"], "download_dependency_missing")
        with patch.object(worker, "load_download_backend", return_value=(object(), object())):
            result = worker.execute(self.payload)
        self.assertTrue(result["environment"]["download_ready"])
        self.assertEqual(result["environment"]["geedim_version"], "2.0.0")

    def test_environment_reports_missing_region_dependency_separately(self):
        self.payload.update(operation="gee_check_environment")
        with patch.object(worker, "load_download_backend", return_value=(object(), object())), patch.object(worker, "load_region_backend", side_effect=worker.RuntimeFailure("region_dependency_missing", "geopandas missing")):
            result = worker.execute(self.payload)
        self.assertTrue(result["success"])
        self.assertTrue(result["environment"]["ready"])
        self.assertTrue(result["environment"]["download_ready"])
        self.assertFalse(result["environment"]["region_ready"])
        self.assertEqual(result["status"], "ready_without_region_loading")
        self.assertEqual(result["environment"]["region_install_command"][-1], "geopandas")

    def test_authentication_is_never_started(self):
        def bad_init(**kwargs):
            raise ValueError("Please authenticate; credentials missing")
        self.ee.Initialize = bad_init
        self.ee.Authenticate = lambda: self.fail("must not authenticate automatically")
        result = self.run_script("print('not reached')")
        self.assertEqual(result["error_code"], "authentication_required")

    def test_project_missing(self):
        self.payload["project"] = ""
        result = self.run_script("pass")
        self.assertFalse(result["success"])
        self.assertEqual(result["error_code"], "project_missing")

    def test_download_rejects_escape_existing_and_symlink(self):
        download, _ = worker.make_helpers(self.ee, self.root, [])
        image = types.SimpleNamespace(getDownloadURL=lambda _: self.fail("invalid path made a network request"))
        for filename in ("../escape.tif", "/outside.tif", "wrong.csv"):
            with self.subTest(filename=filename), self.assertRaises(worker.RuntimeFailure):
                download(image, filename)
        (self.root / "existing.tif").write_bytes(b"existing")
        with self.assertRaises(worker.RuntimeFailure):
            download(image, "existing.tif")
        with tempfile.TemporaryDirectory() as outside:
            try:
                (self.root / "link").symlink_to(outside, target_is_directory=True)
            except OSError:
                return
            with self.assertRaises(worker.RuntimeFailure):
                download(image, "link/escape.tif")

    def test_large_float64_raster_keeps_scale_precision_and_reports_completion(self):
        fixture = self.mock_raster()
        result = self.run_script("download_image(ee.Image(), 'whole.tif', region={'type': 'Polygon'}, scale=10, crs='EPSG:32650')")
        self.assertTrue(result["success"], result)
        self.assertGreater(fixture.size, 32 * 1024 * 1024)
        self.assertEqual(fixture.prepare_calls[0], {"region": {"type": "Polygon"}, "scale": 10, "crs": "EPSG:32650", "dtype": None, "scale_offset": False})
        self.assertEqual(fixture.download_calls, [{"overwrite": False, "max_tile_size": 16, "max_requests": 4, "max_cpus": 1}])
        self.assertEqual(result["downloads"][0]["dtype"], "float64")
        self.assertEqual(result["downloads"][0]["status"], "completed")
        self.assertTrue((self.root / "whole.tif").is_file())
        self.assertEqual(list(self.root.glob("_gee_download_*")), [])
        self.assertIs(fixture.module.Tiler, fixture.original_tiler)
        events = [json.loads(line) for line in (self.root / "progress.jsonl").read_text().splitlines()]
        self.assertEqual([event["phase"] for event in events], ["preparing", "downloading", "validating", "completed"])
        self.assertEqual(events[-1]["completed_bytes"], fixture.size)
        self.assertEqual(events[-1]["percent"], 100)

    def test_dimensions_keep_earth_engine_semantics_and_explicit_dtype(self):
        fixture = self.mock_raster(shape=(16, 32))
        download, _ = worker.make_helpers(self.ee, self.root, [])
        for index, dimensions in enumerate((32, "32x16", [32, 16])):
            download(fixture.image, "small{}.tif".format(index), dimensions=dimensions, dtype="float32", max_requests=2)
            self.assertEqual(fixture.export_calls[-1], {"dimensions": dimensions})
            self.assertEqual(fixture.prepare_calls[-1], {"dtype": "float32", "scale_offset": False})
        self.assertEqual(fixture.dtype, "float32")
        for kwargs in ({"dimensions": 10001}, {"dimensions": True}, {"scale": 0}, {"scale": float("nan")}, {"max_requests": 0}, {"max_cpus": 5}, {"max_tile_size": 33}, {"dtype": "uint4"}):
            with self.subTest(kwargs=kwargs), self.assertRaises(worker.RuntimeFailure) as caught:
                download(fixture.image, "invalid.tif", **kwargs)
            self.assertEqual(caught.exception.code, "invalid_arguments")
        self.assertEqual(len(fixture.download_calls), 3)

    def cached_helper(self, name, records):
        root = self.root / name
        root.mkdir()
        download, _ = worker.make_helpers(self.ee, root, [], records, download_root=self.root)
        return download

    def test_identical_completed_download_is_reused_without_copying(self):
        fixture = self.mock_raster()
        fixture.image.serialize = lambda: '{"image":"same-scene","mask":"same-mask"}'
        first_records, second_records = [], []
        first = self.cached_helper("run-first", first_records)
        second = self.cached_helper("run-second", second_records)
        args = dict(region={"type": "Polygon", "coordinates": [1, 2]}, scale=10, crs="EPSG:32650")
        path = first(fixture.image, "north.tif", **args)
        reused = second(fixture.image, "renamed-north.tif", **args)
        self.assertEqual(reused, path)
        self.assertEqual(len(fixture.download_calls), 1)
        self.assertFalse((self.root / "run-second" / "renamed-north.tif").exists())
        self.assertTrue(second_records[0]["reused"])
        self.assertEqual(second_records[0]["status"], "completed")
        self.assertEqual(second_records[0]["filename"], "renamed-north.tif")
        self.assertEqual(json.loads((self.root / "run-second" / "downloads.jsonl").read_text().splitlines()[-1])["path"], path)
        with self.assertRaises(worker.RuntimeFailure):
            second(fixture.image, "invalid.tif", max_requests=0, **args)

    def test_different_mask_region_resolution_or_dtype_is_not_reused(self):
        fixture = self.mock_raster()
        expression = {"scene": "same", "mask": "cloud"}
        fixture.image.serialize = lambda: json.dumps(expression)
        requests = [dict(scale=10), dict(scale=20), dict(scale=10, region={"type": "Polygon"}), dict(scale=10, dtype="float32")]
        for index, kwargs in enumerate(requests):
            self.cached_helper("run-" + str(index), [])(fixture.image, "image.tif", **kwargs)
        expression["mask"] = "unmasked"
        self.cached_helper("run-mask", [])(fixture.image, "image.tif", scale=10)
        self.assertEqual(len(fixture.download_calls), 5)

    def test_missing_changed_or_explicitly_refreshed_file_is_not_reused(self):
        fixture = self.mock_raster()
        fixture.image.serialize = lambda: '{"image":"same"}'
        first = self.cached_helper("run-first", [])(fixture.image, "image.tif", scale=10)
        Path(first).write_bytes(b"modified")
        second = self.cached_helper("run-changed", [])(fixture.image, "image.tif", scale=10)
        self.assertNotEqual(first, second)
        Path(second).unlink()
        third = self.cached_helper("run-missing", [])(fixture.image, "image.tif", scale=10)
        refreshed = self.cached_helper("run-refresh", [])(fixture.image, "image.tif", scale=10, reuse=False)
        self.assertNotEqual(third, refreshed)
        self.assertEqual(len(fixture.download_calls), 4)

    def test_replaced_content_with_preserved_size_and_mtime_is_not_reused(self):
        fixture = self.mock_raster()
        fixture.image.serialize = lambda: '{"image":"same"}'
        first = Path(self.cached_helper("run-first", [])(fixture.image, "image.tif", scale=10))
        previous = first.stat()
        time.sleep(0.01)
        first.write_bytes(b"x" * previous.st_size)
        os.utime(first, ns=(previous.st_atime_ns, previous.st_mtime_ns))
        records = []
        second = self.cached_helper("run-second", records)(fixture.image, "image.tif", scale=10)
        self.assertNotEqual(str(first), second)
        self.assertNotIn("reused", records[0])
        self.assertEqual(len(fixture.download_calls), 2)

    def test_reuse_index_failure_does_not_fail_completed_delivery(self):
        fixture = self.mock_raster()
        fixture.image.serialize = lambda: '{"image":"same"}'
        records = []
        with patch.object(Path, "replace", side_effect=OSError("index unavailable")):
            path = self.cached_helper("run-first", records)(fixture.image, "image.tif", scale=10)
        self.assertTrue(Path(path).is_file())
        self.assertEqual(records[0]["status"], "completed")
        self.assertIn("index unavailable", records[0]["reuse_warning"])

    def test_failed_download_is_not_reused(self):
        fixture = self.mock_raster()
        fixture.image.serialize = lambda: '{"image":"same"}'
        fixture.failure = RuntimeError("failed tile")
        with self.assertRaises(worker.RuntimeFailure):
            self.cached_helper("run-failed", [])(fixture.image, "image.tif", scale=10)
        self.assertEqual(list((self.root / ".gee-downloads").glob("*.json")), [])
        fixture.failure = None
        records = []
        self.cached_helper("run-retry", records)(fixture.image, "image.tif", scale=10)
        self.assertNotIn("reused", records[0])
        self.assertEqual(len(fixture.download_calls), 2)

    def test_simultaneous_processes_download_identical_request_only_once(self):
        # Two independent Python workers, the same boundary as concurrent tools.
        # The substitute writes tiny local bytes and never contacts Earth Engine.
        code = '''
import importlib.util,json,pathlib,sys,time,types
spec=importlib.util.spec_from_file_location("worker",sys.argv[1])
w=importlib.util.module_from_spec(spec);spec.loader.exec_module(w)
root=pathlib.Path(sys.argv[2]); run=root/sys.argv[3];run.mkdir()
def download(image,target,region,scale,crs,dimensions,dtype,max_requests,max_cpus,max_tile_size,record,progress):
    with (root/"calls.txt").open("a") as out: out.write("download\\n")
    time.sleep(0.3)
    target.write_bytes(b"complete-test-raster")
    record.update(status="completed",path=str(target),bytes=target.stat().st_size)
    return str(target)
w.download_raster=download
helper,_=w.make_helpers(object(),run,[],download_root=root)
image=types.SimpleNamespace(serialize=lambda:'{"image":"same"}')
path=helper(image,"image.tif",scale=10)
(run/"path.txt").write_text(path)
'''
        commands = [[sys.executable, "-c", code, str(Path(worker.__file__).resolve()), str(self.root), name] for name in ("run-a", "run-b")]
        processes = [subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE) for command in commands]
        try:
            for process in processes:
                stdout, stderr = process.communicate(timeout=10)
                self.assertEqual(process.returncode, 0, (stdout, stderr))
        finally:
            for process in processes:
                if process.poll() is None:
                    process.kill()
                    process.communicate()
        self.assertEqual((self.root / "calls.txt").read_text().splitlines(), ["download"])
        self.assertEqual((self.root / "run-a/path.txt").read_text(), (self.root / "run-b/path.txt").read_text())

    def test_caught_tile_failure_still_fails_delivery_and_cleans_partial_files(self):
        fixture = self.mock_raster()
        fixture.failure = RuntimeError("request https://example.test/tile?token=private-secret failed")
        result = self.run_script("try:\n    download_image(ee.Image(), 'whole.tif', scale=10, crs='EPSG:32650')\nexcept Exception:\n    pass")
        self.assertFalse(result["success"], result)
        self.assertEqual(result["status"], "incomplete")
        self.assertEqual(result["error_code"], "download_incomplete")
        self.assertEqual(result["downloads"][0]["status"], "failed")
        self.assertEqual(result["tasks"], [])
        self.assertFalse((self.root / "whole.tif").exists())
        self.assertEqual(list(self.root.glob("_gee_download_*")), [])
        self.assertIs(fixture.module.Tiler, fixture.original_tiler)
        journal = (self.root / "progress.jsonl").read_text()
        self.assertNotIn("private-secret", json.dumps(result) + journal)
        self.assertEqual(json.loads(journal.splitlines()[-1])["phase"], "failed")

    def test_unreported_missing_tiles_and_invalid_raster_are_not_published(self):
        fixture = self.mock_raster()
        for change in (None, ("shape", (1, 1)), ("dtypes", ["uint16"] * 3), ("crs", "EPSG:4326"), ("transform", types.SimpleNamespace(almost_equals=lambda _: False))):
            fixture.omit_last = change is None
            fixture.invalid_dataset = change
            with self.subTest(change=change):
                result = self.run_script("download_image(ee.Image(), 'whole.tif', scale=10, crs='EPSG:32650')")
                self.assertEqual(result["error_code"], "download_incomplete")
                self.assertFalse((self.root / "whole.tif").exists())
                self.assertEqual(list(self.root.glob("_gee_download_*")), [])

    def test_interrupted_downloader_cleans_and_restores_adapter(self):
        fixture = self.mock_raster()
        fixture.failure = KeyboardInterrupt("cancelled")
        result = self.run_script("download_image(ee.Image(), 'whole.tif')")
        self.assertFalse(result["success"])
        self.assertEqual(list(self.root.glob("_gee_download_*")), [])
        self.assertFalse((self.root / "whole.tif").exists())
        self.assertIs(fixture.module.Tiler, fixture.original_tiler)

    def test_missing_or_wrong_download_dependency_has_explicit_setup_diagnostic(self):
        for version, expected in ((None, "download_dependency_missing"), ("1.9.0", "download_dependency_version")):
            with self.subTest(version=version), patch.object(worker.importlib.metadata, "version", side_effect=worker.importlib.metadata.PackageNotFoundError("geedim") if version is None else None, return_value=version):
                result = self.run_script("download_image(object(), 'whole.tif')")
            self.assertEqual(result["error_code"], expected)
            self.assertIn("geedim==2.0.0", result["message"])
            self.assertIn("geedim==2.0.0", result["environment"]["download_install_command"])
            self.assertEqual(result["tasks"], [])

    def test_proxy_session_honours_environment_and_restores_runner_after_failure(self):
        loop = asyncio.new_event_loop()
        self.addCleanup(loop.close)
        old_session = object()
        runner = types.SimpleNamespace(_session=old_session, run=loop.run_until_complete)
        sessions = []

        def create(**kwargs):
            session = types.SimpleNamespace(options=kwargs, closed=False)

            async def close():
                session.closed = True
            session.close = close
            sessions.append(session)
            return session

        aiohttp = types.SimpleNamespace(ClientSession=create, ClientTimeout=lambda **kwargs: kwargs)
        gd_image = types.SimpleNamespace(utils=types.SimpleNamespace(AsyncRunner=lambda: runner, aiohttp=aiohttp))
        with self.assertRaisesRegex(RuntimeError, "tile failed"):
            with worker.proxy_aware_download_session(gd_image):
                self.assertTrue(runner._session.options["trust_env"])
                self.assertIsNot(runner._session, old_session)
                raise RuntimeError("tile failed")
        self.assertIs(runner._session, old_session)
        self.assertTrue(sessions[0].closed)

    @unittest.skipUnless(os.environ.get("AUTOCARTO_GEE_TEST_GEEDIM") == "1", "opt-in local HTTP integration with installed geedim==2.0.0")
    def test_real_geedim_tiles_and_rasterio_writer_against_local_http(self):
        # Exercise the pinned tiler, aiohttp, writer and assembled pixel content.
        # Only the Earth Engine URL factory is simulated; no Google API is used.
        self.mock_import.stop()
        from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
        import numpy as np

        gd_image, rio = worker.load_download_backend()
        source = np.arange(3 * 64 * 96, dtype="float64").reshape(3, 64, 96) + 0.125
        transform = rio.Affine(10, 0, 250000, 0, -10, 3300000)
        buffers, requests, lock = {}, [], threading.Lock()

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_GET(self):
                with lock:
                    data = buffers[self.path]
                self.send_response(200)
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)

        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()

        def shutdown():
            server.shutdown()
            server.server_close()
            thread.join(timeout=2)
        self.addCleanup(shutdown)

        class ImageSlice:
            def __init__(self, start=0, stop=3):
                self.start, self.stop = start, stop

            def slice(self, start, stop):
                return ImageSlice(start, stop)

            def getDownloadURL(self, params):
                width, height = params["dimensions"]
                tile_transform = rio.Affine(*params["crs_transform"])
                col = round((tile_transform.c - transform.c) / transform.a)
                row = round((tile_transform.f - transform.f) / transform.e)
                values = source[self.start:self.stop, row:row + height, col:col + width]
                with rio.MemoryFile() as memory:
                    with memory.open(driver="GTiff", width=width, height=height, count=self.stop - self.start, dtype="float64", crs=params["crs"], transform=tile_transform) as raster:
                        raster.write(values)
                    data = memory.read()
                with lock:
                    key = "/tile-{}".format(len(buffers))
                    buffers[key] = data
                    requests.append(values.nbytes)
                return "http://127.0.0.1:{}{}".format(server.server_port, key)

        accessor = types.SimpleNamespace(dtype="float64", shape=source.shape[1:], count=3, size=source.nbytes, crs="EPSG:32650", transform=list(transform)[:6], index="synthetic", id="synthetic", _ee_image=ImageSlice(), _raise_cannot_export=lambda: None, _write_metadata=lambda _: None)
        accessor.profile = dict(width=96, height=64, count=3, dtype="float64", crs="EPSG:32650", transform=transform)
        accessor.toGeoTIFF = types.MethodType(gd_image.ImageAccessor.toGeoTIFF, accessor)
        image = types.SimpleNamespace(gd=accessor)
        accessor.prepareForExport = lambda **kwargs: image
        downloads = []
        download, _ = worker.make_helpers(self.ee, self.root, [], downloads)
        with patch.dict(os.environ, {"NO_PROXY": "127.0.0.1", "no_proxy": "127.0.0.1"}):
            destination = download(image, "whole.tif", scale=10, crs="EPSG:32650", max_tile_size=0.02)
        self.assertGreater(len(requests), 1)
        self.assertEqual(sum(requests), source.nbytes)
        with rio.open(destination) as raster:
            np.testing.assert_array_equal(raster.read(), source)
            self.assertEqual(raster.transform, transform)
            self.assertEqual(raster.dtypes, ("float64",) * 3)
        self.assertEqual(downloads[0]["status"], "completed")
        self.assertEqual(list(self.root.glob("_gee_download_*")), [])

    def test_export_is_submitted_not_completed_and_records_task_before_status(self):
        task = types.SimpleNamespace(id="real-task-id", name="projects/test-project/operations/real-task-id", start=lambda: None, status=lambda: {"state": "RUNNING"})
        self.ee.batch = types.SimpleNamespace(Export=types.SimpleNamespace(image=types.SimpleNamespace(toDrive=lambda **kwargs: task)))
        result = self.run_script("export_image_to_drive(object(), 'sample', scale=10)")
        self.assertTrue(result["success"])
        self.assertEqual(result["status"], "exports_submitted")
        self.assertEqual(result["tasks"][0]["state"], "RUNNING")
        record = json.loads((self.root / "tasks.jsonl").read_text())
        self.assertEqual(record["id"], "real-task-id")
        task.status = lambda: {"state": "FAILED", "error_message": "quota"}
        result = self.run_script("export_image_to_drive(object(), 'sample')")
        self.assertFalse(result["success"])
        self.assertEqual(result["error_code"], "export_failed")
        result = self.run_script("try:\n    export_image_to_drive(object(), 'sample')\nexcept Exception:\n    pass")
        self.assertFalse(result["success"])

    def test_task_status_failure_and_explicit_cancel(self):
        cancels = []
        self.ee.data = types.SimpleNamespace(getOperation=lambda name: {"name": name, "done": True, "metadata": {"state": "FAILED"}, "error": {"message": "quota"}}, cancelOperation=lambda name: cancels.append(name))
        self.payload.update(operation="gee_task_status", task_id="id123")
        result = worker.execute(self.payload)
        self.assertFalse(result["success"])
        self.assertEqual(result["task"]["state"], "FAILED")
        self.assertEqual(cancels, [])
        self.payload["operation"] = "gee_cancel_task"
        result = worker.execute(self.payload)
        self.assertTrue(result["success"])
        self.assertEqual(result["status"], "CANCEL_REQUESTED")
        self.assertEqual(cancels, ["projects/test-project/operations/id123"])

    def test_task_pending_is_not_complete(self):
        self.ee.data = types.SimpleNamespace(getOperation=lambda name: {"metadata": {"state": "PENDING"}, "done": False})
        status = worker.task_status(self.ee, "projects/p/operations/id")
        self.assertEqual(status["state"], "READY")
        self.assertFalse(status["done"])


class CoverageImage:
    """Small pixel grid including None outside the image's footprint."""

    def __init__(self, fixture, bands):
        self.fixture, self.bands = fixture, bands

    def bandNames(self):
        return types.SimpleNamespace(getInfo=lambda: list(self.bands))

    def select(self, names):
        keys = list(self.bands)
        return CoverageImage(self.fixture, {keys[name] if isinstance(name, int) else name: self.bands[keys[name] if isinstance(name, int) else name] for name in names})

    def projection(self):
        return "EPSG:32650"

    def mask(self):
        return self

    def reduce(self, reducer):
        assert reducer == "min"
        values = list(self.bands.values())
        return CoverageImage(self.fixture, {"min": [min(row) if all(value is not None for value in row) else None for row in zip(*values)]})

    def unmask(self, value, sameFootprint=True):
        self.fixture.same_footprint = sameFootprint
        return CoverageImage(self.fixture, {name: [value if item is None else item for item in values] for name, values in self.bands.items()})

    def gt(self, threshold):
        return CoverageImage(self.fixture, {name: [int(value > threshold) for value in values] for name, values in self.bands.items()})

    def rename(self, name):
        assert len(self.bands) == 1
        return CoverageImage(self.fixture, {name: next(iter(self.bands.values()))})

    def reproject(self, projection):
        self.fixture.projections.append(projection)
        return self

    def addBands(self, image):
        return CoverageImage(self.fixture, {**self.bands, **image.bands})

    def reduceRegion(self, **kwargs):
        self.fixture.reduce_calls.append(kwargs)
        return types.SimpleNamespace(getInfo=lambda: {name: sum(value or 0 for value in values) for name, values in self.bands.items()})


class WorkflowTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.features = []
        self.ee = types.SimpleNamespace(FeatureCollection=lambda features: self.features.extend(features) or features)
        self.helpers = worker.make_workflow_helpers(self.ee, self.root)
        self.geometry = {"type": "Polygon", "coordinates": [[[114.0, 30.0], [114.1, 30.0], [114.1, 30.1], [114.0, 30.0]]]}

    def geojson(self, geometry=None, count=1, **extra):
        path = self.root / "region.geojson"
        path.write_text(json.dumps({"type": "FeatureCollection", "features": [{"type": "Feature", "geometry": self.geometry if geometry is None else geometry, "properties": {"date": "not passed to EE"}} for _ in range(count)], **extra}))
        return path

    def test_region_dependency_missing_does_not_bypass_geometry_validation(self):
        with patch.object(worker.importlib, "import_module", side_effect=ImportError), contextlib.redirect_stdout(io.StringIO()):
            with self.assertRaises(worker.RuntimeFailure) as caught:
                self.helpers["load_region"](self.geojson())
            self.assertEqual(caught.exception.code, "region_dependency_missing")
        self.assertEqual(self.features, [])

    def test_region_rejects_missing_crs_multiple_features_and_bad_topology(self):
        class Frame:
            def __init__(self, crs="EPSG:4326", count=1, geometry=None):
                self.crs, self.count = crs, count
                self.geometry = types.SimpleNamespace(iloc=[geometry])
            def __len__(self):
                return self.count
        valid = types.SimpleNamespace(is_empty=False, is_valid=True)
        frames = [Frame(crs=None, geometry=valid), Frame(count=2, geometry=valid), Frame(geometry=None), Frame(geometry=types.SimpleNamespace(is_empty=True, is_valid=True)), Frame(geometry=types.SimpleNamespace(is_empty=False, is_valid=False))]
        path = self.geojson()
        for frame in frames:
            with self.subTest(frame=frame), patch.object(worker, "load_region_backend", return_value=types.SimpleNamespace(read_file=lambda _: frame)), contextlib.redirect_stdout(io.StringIO()), self.assertRaises(worker.RuntimeFailure) as caught:
                self.helpers["load_region"](path)
            self.assertEqual(caught.exception.code, "invalid_region")
        self.assertEqual(self.features, [])

    @unittest.skipUnless(importlib.util.find_spec("geopandas"), "optional installed GeoPandas geometry/projection check")
    def test_real_geopandas_reprojects_utm_and_rejects_self_intersection(self):
        import geopandas as gpd
        from shapely.geometry import shape, Polygon, Point
        frame = gpd.GeoDataFrame({"date": ["2024-12-24"]}, geometry=[shape(self.geometry)], crs=4326)
        projected = self.root / "projected.gpkg"
        frame.to_crs(32650).to_file(projected, driver="GPKG")
        with contextlib.redirect_stdout(io.StringIO()):
            result = self.helpers["load_region"](projected)
        self.assertEqual(result[0]["properties"], {})
        for actual, expected in zip(result[0]["geometry"]["coordinates"][0], self.geometry["coordinates"][0]):
            self.assertAlmostEqual(actual[0], expected[0], places=8)
            self.assertAlmostEqual(actual[1], expected[1], places=8)
        invalid_geometries = [Polygon([(114, 30), (115, 31), (115, 30), (114, 31), (114, 30)]), Point(114, 30)]
        for index, geometry in enumerate(invalid_geometries):
            path = self.root / ("invalid{}.geojson".format(index))
            gpd.GeoDataFrame(geometry=[geometry], crs=4326).to_file(path, driver="GeoJSON")
            with self.subTest(geometry=geometry), contextlib.redirect_stdout(io.StringIO()), self.assertRaises(worker.RuntimeFailure) as caught:
                self.helpers["load_region"](path)
            self.assertEqual(caught.exception.code, "invalid_region")

    def coverage_fixture(self):
        fixture = types.SimpleNamespace(same_footprint=None, projections=[], reduce_calls=[])
        self.ee.Image = types.SimpleNamespace(constant=lambda value: CoverageImage(fixture, {"constant": [value] * 8}))
        self.ee.Reducer = types.SimpleNamespace(min=lambda: "min", sum=lambda: "sum")
        image = CoverageImage(fixture, {"B4": [1, 1, 1, 1, None, None, None, None], "B3": [1, 1, 0, 1, None, None, None, None], "B2": [1, 1, 1, 0, None, None, None, None]})
        return fixture, image

    def test_coverage_counts_full_roi_and_intersects_selected_band_masks(self):
        fixture, image = self.coverage_fixture()
        with contextlib.redirect_stdout(io.StringIO()):
            coverage = self.helpers["check_coverage"](image, "whole_roi", bands=["B4", "B3", "B2"], scale=20)
        # 4 of 8 pixels lie in the scene, but only 2 are valid in all RGB bands.
        self.assertEqual(coverage, 0.25)
        self.assertIsInstance(coverage, float)
        self.assertFalse(fixture.same_footprint)
        self.assertEqual(fixture.projections, ["EPSG:32650"])
        self.assertEqual(fixture.reduce_calls[0]["geometry"], "whole_roi")
        self.assertEqual(fixture.reduce_calls[0]["scale"], 20)
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(self.helpers["check_coverage"](image, "whole_roi"), 0.5)

    def test_coverage_rejects_missing_band_before_reduce_and_zero_data_is_zero(self):
        fixture, image = self.coverage_fixture()
        with contextlib.redirect_stdout(io.StringIO()):
            for kwargs in ({"band": "missing"}, {"band": "B4", "bands": ["B3"]}, {"bands": []}, {"scale": 0}):
                with self.subTest(kwargs=kwargs), self.assertRaises(worker.RuntimeFailure):
                    self.helpers["check_coverage"](image, "roi", **kwargs)
            self.assertEqual(fixture.reduce_calls, [])
            empty = CoverageImage(fixture, {"B4": [None] * 8})
            self.assertEqual(self.helpers["check_coverage"](empty, "roi"), 0.0)

    def test_heartbeat_reports_periodically_and_stops_on_failure(self):
        heartbeat = self.helpers["heartbeat"]("Checking candidates", interval=0.01)
        with contextlib.redirect_stdout(io.StringIO()), self.assertRaisesRegex(ValueError, "query failed"):
            with heartbeat:
                time.sleep(0.035)
                raise ValueError("query failed")
        self.assertFalse(heartbeat.thread.is_alive())
        events = [json.loads(line) for line in (self.root / "progress.jsonl").read_text().splitlines()]
        self.assertGreaterEqual(len(events), 3)
        self.assertEqual(events[0]["phase"], "running")
        self.assertEqual(events[-1]["phase"], "failed")
        self.assertTrue(all(event["stage_id"] == 1 and event["type"] == "workflow_progress" for event in events))
        with contextlib.redirect_stdout(io.StringIO()):
            with self.helpers["heartbeat"]("Next stage"):
                pass
        last = json.loads((self.root / "progress.jsonl").read_text().splitlines()[-1])
        self.assertEqual(last["stage_id"], 2)
        self.assertEqual(last["phase"], "completed")


if __name__ == "__main__":
    unittest.main()
