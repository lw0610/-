"""Earth Engine worker. Uses user-managed credentials and never installs or authenticates.

Scripts execute with the user's permissions, in a fresh process. This is not a sandbox.
API references: developers.google.com/earth-engine/guides/auth,
apidocs/ee-image-getdownloadurl and apidocs/export-image-todrive.
"""

import importlib
import json
import os
from pathlib import Path
import re
import sys
import traceback
import urllib.request

MAX_DOWNLOAD_BYTES = 32 * 1024 * 1024
TASK_RE = re.compile(r"(?:[A-Za-z0-9_-]+|projects/[A-Za-z0-9._:-]+/operations/[A-Za-z0-9_-]+)\Z")
FAILED_STATES = {"FAILED", "CANCELLED", "UNKNOWN"}


class RuntimeFailure(Exception):
    def __init__(self, code, message):
        super().__init__(message)
        self.code = code


def auth_steps():
    return {
        "install_command": [sys.executable, "-m", "pip", "install", "earthengine-api"],
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


def make_helpers(ee, root, tasks):
    root = Path(root).resolve()

    def download_image(image, filename, region=None, scale=None, crs=None, dimensions=None):
        """Download one small GeoTIFF (official API limit: 32 MB, each dimension <=10000).

        Use export_image_to_drive for large images. This helper does not tile or
        silently coarsen the resolution. Existing destination files are never replaced.
        """
        target = safe_output_path(root, filename)
        if target.suffix.lower() not in (".tif", ".tiff"):
            raise RuntimeFailure("invalid_output_path", "download_image requires a .tif or .tiff filename")
        if dimensions is not None:
            if isinstance(dimensions, int) and not isinstance(dimensions, bool):
                dims = [dimensions]
            elif isinstance(dimensions, str) and re.fullmatch(r"\d+(?:x\d+)?", dimensions):
                dims = [int(v) for v in dimensions.split("x")]
            elif isinstance(dimensions, (tuple, list)):
                dims = list(dimensions)
            else:
                raise RuntimeFailure("download_limit", "dimensions must be an integer, WIDTHxHEIGHT, or [width, height]")
            if not 1 <= len(dims) <= 2 or any(not isinstance(v, int) or isinstance(v, bool) or v <= 0 or v > 10000 for v in dims):
                raise RuntimeFailure("download_limit", "Each getDownloadURL grid dimension must be between 1 and 10000")
        params = {"format": "GEO_TIFF", "filePerBand": False}
        for key, value in (("region", region), ("scale", scale), ("crs", crs), ("dimensions", dimensions)):
            if value is not None:
                params[key] = value
        try:
            download_url = image.getDownloadURL(params)
        except Exception as exc:
            raise RuntimeFailure("download_failed", "Earth Engine download request failed (small-image limit: 32 MB / 10000 pixels per dimension). Use a smaller area or export_image_to_drive. " + str(exc)) from exc
        # The signed URL stays in this process; it is never printed or persisted.
        target.parent.mkdir(parents=True, exist_ok=True)
        temporary = target.with_name(target.name + ".part")
        try:
            with urllib.request.urlopen(download_url, timeout=60) as response, temporary.open("xb") as output:
                total = 0
                signature = b""
                while True:
                    chunk = response.read(65536)
                    if not chunk:
                        break
                    total += len(chunk)
                    if len(signature) < 4:
                        signature = (signature + chunk)[:4]
                    if total > MAX_DOWNLOAD_BYTES:
                        raise RuntimeFailure("download_limit", "Download exceeded 32 MB; use export_image_to_drive for larger images")
                    output.write(chunk)
            if total == 0:
                raise RuntimeFailure("download_failed", "Earth Engine returned an empty download")
            if signature not in (b"II*\x00", b"MM\x00*", b"II+\x00", b"MM\x00+"):
                raise RuntimeFailure("download_failed", "Earth Engine response was not a TIFF; no output artifact was published")
            # Link creates the final name exclusively, without replacing a concurrently created file.
            os.link(temporary, target)
            temporary.unlink()
        except BaseException:
            temporary.unlink(missing_ok=True)
            raise
        return str(target)

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


def execute(payload):
    operation = payload["operation"]
    tasks = []
    response = {"success": False, "status": "failed", "tasks": tasks}
    environment = {"python": sys.executable, "project": payload.get("project", ""), "ready": False, **auth_steps()}
    response["environment"] = environment
    try:
        if operation != "gee_check_environment" and not payload.get("enabled"):
            raise RuntimeFailure("disabled", "Earth Engine execution is disabled")
        ee = initialize(payload.get("project", ""))
        environment.update({"ready": True, "earthengine_api_version": getattr(ee, "__version__", "unknown")})
        if operation == "gee_check_environment":
            response.update(success=True, status="ready", message="Earth Engine initialized using existing user credentials. Execution remains disabled." if not payload.get("enabled") else "Earth Engine initialized using existing user credentials.")
        elif operation == "gee_run_script":
            output_dir = Path(payload["output_dir"]).resolve()
            download, export = make_helpers(ee, output_dir, tasks)
            script = Path(payload["script_path"])
            scope = {"__name__": "__main__", "__file__": str(script), "ee": ee, "output_dir": output_dir, "download_image": download, "export_image_to_drive": export}
            exec(compile(script.read_text(encoding="utf-8"), str(script), "exec"), scope)
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
        response.update(success=False, status="failed", error_code=exc.code if isinstance(exc, RuntimeFailure) else "script_failed", message=str(exc) or type(exc).__name__)
        if operation == "gee_check_environment":
            response.update(success=True, status=response["error_code"])
        else:
            traceback.print_exc(file=sys.stderr)
    return response


def main():
    payload = json.load(sys.stdin)
    response = execute(payload)
    destination = Path(payload["result_path"])
    destination.write_text(json.dumps(response, ensure_ascii=False), encoding="utf-8")
    return 0 if response["success"] else 1


if __name__ == "__main__":
    sys.exit(main())
