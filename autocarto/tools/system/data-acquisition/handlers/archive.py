"""Bounded archive extraction; publication happens only after complete validation."""
import os
from pathlib import Path, PurePosixPath, PureWindowsPath
import shutil
import stat
import tarfile
import tempfile
import zipfile

from autocarto.core.paths import get_autocarto_home, resolve_project_path


def _member(name):
    normalized = name.replace("\\", "/")
    p = PurePosixPath(normalized)
    if not normalized or "\x00" in normalized or p.is_absolute() or ".." in p.parts or PureWindowsPath(name).drive:
        raise ValueError("压缩包含越界路径")
    return p


def extract_handler(params):
    stage = None
    try:
        source = resolve_project_path(params.get("input") or "")
        output = resolve_project_path(params.get("output") or "")
        allowed = (get_autocarto_home() / "data" / "acquired").resolve()
        if output == allowed or allowed not in output.parents:
            raise ValueError("解压目录必须位于数据仓临时目录内")
        if output.exists() or output.is_symlink():
            raise ValueError("解压输出目录已存在")
        max_files = int(params.get("max_files") or 10000)
        max_bytes = int(params.get("max_bytes") or 2 * 1024 ** 3)
        if not 1 <= max_files <= 100000 or not 1 <= max_bytes <= 20 * 1024 ** 3:
            raise ValueError("解压限制无效")
        output.parent.mkdir(parents=True, exist_ok=True)
        stage = Path(tempfile.mkdtemp(prefix=".extract-", dir=output.parent))
        seen, total, files = set(), 0, []
        archive = zipfile.ZipFile(source) if zipfile.is_zipfile(source) else tarfile.open(source, "r:gz")
        with archive:
            entries = archive.infolist() if isinstance(archive, zipfile.ZipFile) else archive.getmembers()
            if len(entries) > max_files:
                raise ValueError("压缩包文件数量超过限制")
            for entry in entries:
                is_zip = isinstance(archive, zipfile.ZipFile)
                name = entry.filename if is_zip else entry.name
                path = _member(name)
                if str(path) in seen:
                    raise ValueError("压缩包存在重复路径")
                seen.add(str(path))
                directory = entry.is_dir() if is_zip else entry.isdir()
                mode = entry.external_attr >> 16 if is_zip else entry.mode
                if (is_zip and stat.S_IFMT(mode) not in (0, stat.S_IFREG, stat.S_IFDIR)) or (not is_zip and not (entry.isfile() or directory)):
                    raise ValueError("压缩包不允许符号链接或特殊文件")
                declared = entry.file_size if is_zip else entry.size
                total += declared
                if total > max_bytes:
                    raise ValueError("压缩包解压总大小超过限制")
                target = stage.joinpath(*path.parts)
                if directory:
                    target.mkdir(parents=True, exist_ok=True)
                    continue
                target.parent.mkdir(parents=True, exist_ok=True)
                stream = archive.open(entry) if is_zip else archive.extractfile(entry)
                actual = 0
                with stream, target.open("xb") as out:
                    while True:
                        block = stream.read(min(1024 * 1024, declared - actual + 1))
                        if not block:
                            break
                        actual += len(block)
                        if actual > declared:
                            raise ValueError("解压大小与声明不符")
                        out.write(block)
                if actual != declared:
                    raise ValueError("压缩包文件不完整")
                files.append(str(output.joinpath(*path.parts)))
        # Reserve the final name exclusively before publishing its contents.
        output.mkdir()
        try:
            for child in stage.iterdir():
                os.rename(child, output / child.name)
        except Exception:
            shutil.rmtree(output, ignore_errors=True)
            raise
        stage.rmdir()
        stage = None
        return {"success": True, "output": str(output), "data": {"files": files, "bytes": total}, "info": "安全解压完成"}
    except Exception as exc:
        return {"success": False, "error_type": "archive_extract_failed", "message": str(exc), "info": str(exc)}
    finally:
        if stage:
            shutil.rmtree(stage, ignore_errors=True)
