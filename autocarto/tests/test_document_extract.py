import importlib.util
from pathlib import Path
import csv
import pytest

MODULE = Path(__file__).parents[1] / 'tools/system/file-operations/handlers/document_extract.py'
spec = importlib.util.spec_from_file_location('document_extract', MODULE)
extract = importlib.util.module_from_spec(spec)
spec.loader.exec_module(extract)


def test_html_spans(tmp_path):
    path = tmp_path / 'source.html'
    path.write_text('<table><tr><th rowspan="2">地区</th><th colspan="2">人口</th></tr><tr><th>2022</th><th>2023</th></tr><tr><td>郑州</td><td>1283</td><td>1301</td></tr></table>')
    result = extract.extract_document(path, tmp_path / 'out')
    rows = list(csv.reader(Path(result['tables'][0]['path']).open(encoding='utf-8-sig')))
    assert rows == [['地区', '人口', '人口'], ['地区', '2022', '2023'], ['郑州', '1283', '1301']]


def test_pdf_text_table(tmp_path):
    pytest.importorskip('pdfplumber')
    pytest.importorskip('reportlab')
    from reportlab.platypus import SimpleDocTemplate, Table, TableStyle
    from reportlab.lib import colors
    path = tmp_path / 'source.pdf'
    table = Table([['City', '2023'], ['Zhengzhou', '1301'], ['Jiyuan', '73']])
    table.setStyle(TableStyle([('GRID', (0, 0), (-1, -1), 1, colors.black)]))
    SimpleDocTemplate(str(path)).build([table])
    result = extract.extract_document(path, tmp_path / 'out')
    rows = list(csv.reader(Path(result['tables'][0]['path']).open(encoding='utf-8-sig')))
    assert rows == [['City', '2023'], ['Zhengzhou', '1301'], ['Jiyuan', '73']]


def test_image_vision_preserves_original(tmp_path):
    image = Path(__file__).parent / 'fixtures/documents/henan-2024-table-3-3.jpg'
    result = extract.extract_document(image, tmp_path, {'vision': True})
    assert result['method'] == 'vision'
    assert result['_agent_media'][0]['path'] == str(image)
    assert result['tables'] == []


def test_image_automatically_uses_ocr_without_vision(tmp_path, monkeypatch):
    image = Path(__file__).parent / 'fixtures/documents/henan-2024-table-3-3.jpg'
    monkeypatch.setattr(extract, 'ocr_cells', lambda path, opts: [{'text': '1301', 'confidence': .82, 'x': 10, 'y': 10, 'width': 30, 'height': 20}])
    result = extract.extract_document(image, tmp_path, {'vision': False, 'ocr_engine': 'rapidocr'})
    assert result['method'] == 'rapidocr'
    assert len(result['uncertain_cells']) == 1
    assert result['tables']


def test_real_henan_image_2023_when_ocr_enabled(tmp_path):
    import os
    import json
    if os.getenv('AUTOCARTO_TEST_REAL_OCR') != '1':
        pytest.skip('opt in to local OCR; no model/network used')
    pytest.importorskip('rapidocr_onnxruntime')
    fixtures = Path(__file__).parent / 'fixtures/documents'
    expected = json.loads((fixtures / 'henan-2024-table-3-3.expected.json').read_text())
    result = extract.extract_document(fixtures / 'henan-2024-table-3-3.jpg', tmp_path, {'ocr_engine': 'rapidocr'})
    rows = list(csv.reader(Path(result['tables'][0]['path']).open(encoding='utf-8-sig')))
    assert len(rows) == 20
    assert [row[-1] for row in rows[1:]] == [str(row[-1]) for row in expected['rows']]


def test_scanned_pdf_renders_real_page_for_vision(tmp_path):
    pytest.importorskip('pdfplumber')
    pytest.importorskip('reportlab')
    from reportlab.pdfgen.canvas import Canvas
    image = Path(__file__).parent / 'fixtures/documents/henan-2024-table-3-3.jpg'
    pdf = tmp_path / 'scan.pdf'
    canvas = Canvas(str(pdf), pagesize=(600, 500))
    canvas.drawImage(str(image), 0, 0, width=600, height=500)
    canvas.save()
    result = extract.extract_document(pdf, tmp_path / 'out', {'vision': True})
    assert result['method'] == 'vision'
    assert len(result['images']) == 1
    assert Path(result['images'][0]).is_file()
    assert result['tables'] == []


def test_pdf_page_range_and_table_hint(tmp_path):
    pytest.importorskip('pdfplumber')
    pytest.importorskip('reportlab')
    from reportlab.platypus import SimpleDocTemplate, Table, TableStyle, PageBreak
    from reportlab.lib import colors
    tables = [Table([['City', '2023'], [name, value]]) for name, value in [('First', '1'), ('Second', '2')]]
    for table in tables:
        table.setStyle(TableStyle([('GRID', (0, 0), (-1, -1), 1, colors.black)]))
    source = tmp_path / 'download.bin'
    SimpleDocTemplate(str(source)).build([tables[0], PageBreak(), tables[1]])
    result = extract.extract_document(source, tmp_path / 'out', {'pages': '2', 'table_hint': 'Second'})
    assert len(result['tables']) == 1
    assert result['tables'][0]['hint_match']
    rows = list(csv.reader(Path(result['tables'][0]['path']).open(encoding='utf-8-sig')))
    assert rows == [['City', '2023'], ['Second', '2']]
    with pytest.raises(ValueError, match='页码'):
        extract.extract_document(source, tmp_path / 'invalid', {'pages': '3'})
