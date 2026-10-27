import importlib
import io
from pathlib import Path
import tarfile
import zipfile

import pytest


def invoke(tmp_path, monkeypatch, archive, **limits):
    monkeypatch.setenv('AUTOCARTO_HOME', str(tmp_path / 'home'))
    module = importlib.import_module('autocarto.tools.system.data-acquisition.handlers.archive')
    return module.extract_handler(dict(input=str(archive), output=str(tmp_path / 'home/data/acquired/temp/extracted'), **limits))


@pytest.mark.parametrize('member', ['../outside', '/absolute', 'C:/absolute', r'..\outside'])
def test_reject_escaping_archives(tmp_path, monkeypatch, member):
    path = tmp_path / 'bad.zip'
    with zipfile.ZipFile(path, 'w') as archive:
        archive.writestr(member, 'x')
    assert not invoke(tmp_path, monkeypatch, path)['success']
    assert not (tmp_path/'home/data/acquired/temp/extracted').exists()


def test_limits_links_and_valid_archive(tmp_path, monkeypatch):
    path = tmp_path/'files.zip'
    with zipfile.ZipFile(path, 'w') as archive:
        archive.writestr('nested/file.txt', 'hello')
    assert not invoke(tmp_path, monkeypatch, path, max_bytes=4)['success']
    result = invoke(tmp_path, monkeypatch, path)
    assert result['success'], result
    assert Path(result['data']['files'][0]).read_text() == 'hello'
    assert not invoke(tmp_path, monkeypatch, path)['success']
    path = tmp_path/'link.tar.gz'
    with tarfile.open(path, 'w:gz') as archive:
        entry = tarfile.TarInfo('link'); entry.type = tarfile.SYMTYPE; entry.linkname = '/tmp'
        archive.addfile(entry)
    other = tmp_path/'other'; other.mkdir()
    assert not invoke(other, monkeypatch, path)['success']
