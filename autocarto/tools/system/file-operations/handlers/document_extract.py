"""Read actual documents into CSV and retain OCR evidence; no generated data."""
import csv
import hashlib
from html.parser import HTMLParser
import io
import json
from pathlib import Path
import statistics
import re
import subprocess
import sys


class TableParser(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.tables, self.rows, self.pending = [], [], {}
        self.row, self.cell, self.span, self.depth = None, None, (1, 1), 0

    def handle_starttag(self, tag, attrs):
        if tag == 'table':
            self.depth += 1
            if self.depth == 1:
                self.rows, self.pending = [], {}
        if self.depth != 1:
            return
        if tag == 'tr':
            self.row = {}
            for column, (value, remaining) in list(self.pending.items()):
                self.row[column] = value
                if remaining == 1:
                    del self.pending[column]
                else:
                    self.pending[column] = (value, remaining - 1)
        elif tag in ('td', 'th') and self.row is not None:
            attrs = dict(attrs)
            self.cell = []
            self.span = (max(1, int(attrs.get('rowspan', 1))), max(1, int(attrs.get('colspan', 1))))
        elif tag == 'br' and self.cell is not None:
            self.cell.append(' ')

    def handle_data(self, data):
        if self.cell is not None:
            self.cell.append(data)

    def handle_endtag(self, tag):
        if self.depth == 1:
            if tag in ('td', 'th') and self.cell is not None:
                column = 0
                while column in self.row:
                    column += 1
                text = ' '.join(''.join(self.cell).split())
                for index in range(column, column + self.span[1]):
                    self.row[index] = text
                    if self.span[0] > 1:
                        self.pending[index] = (text, self.span[0] - 1)
                self.cell = None
            elif tag == 'tr' and self.row is not None:
                if self.row:
                    self.rows.append([self.row.get(i, '') for i in range(max(self.row) + 1)])
                self.row = None
            elif tag == 'table' and self.rows:
                self.tables.append(self.rows)
        if tag == 'table':
            self.depth -= 1


def ocr_cells(path, options):
    engine = options.get('ocr_engine', 'rapidocr')
    if engine == 'rapidocr':
        from rapidocr_onnxruntime import RapidOCR
        values, _ = RapidOCR()(str(path))
        return [{'text': text, 'confidence': float(confidence),
                 'x': min(p[0] for p in box), 'y': min(p[1] for p in box),
                 'width': max(p[0] for p in box) - min(p[0] for p in box),
                 'height': max(p[1] for p in box) - min(p[1] for p in box)}
                for box, text, confidence in (values or [])]
    command = [options.get('ocr_path') or 'tesseract', str(path), 'stdout', '-l',
               options.get('ocr_languages') or 'chi_sim+eng', '--psm', '6', 'tsv']
    output = subprocess.run(command, check=True, capture_output=True, text=True, timeout=180).stdout
    return [{'text': c['text'].strip(), 'confidence': float(c['conf']) / 100,
             'x': float(c['left']), 'y': float(c['top']), 'width': float(c['width']), 'height': float(c['height'])}
            for c in csv.DictReader(io.StringIO(output), delimiter='\t') if c['text'].strip() and float(c['conf']) >= 0]


def cells_to_rows(cells):
    """Cluster OCR boxes by baselines; retain blank columns using shared x positions."""
    if not cells:
        return []
    height = statistics.median(c['height'] for c in cells)
    lines = []
    for cell in sorted(cells, key=lambda c: c['y'] + c['height']/2):
        center = cell['y'] + cell['height']/2
        if not lines or abs(center - statistics.mean(c['y']+c['height']/2 for c in lines[-1])) > height*.65:
            lines.append([])
        lines[-1].append(cell)
    for line in lines:
        line.sort(key=lambda c: c['x'])
    numeric = lambda cell: bool(re.fullmatch(r"[-+]?\d+(?:[.,]\d+)?", cell['text']))
    counts = [sum(numeric(c) for c in line) for line in lines]
    width = max(counts)
    if width >= 2:
        complete = [[c for c in line if numeric(c)] for line, count in zip(lines, counts) if count == width]
        anchors = [statistics.median(row[i]['x'] + row[i]['width']/2 for row in complete) for i in range(width)]
        table = []
        for line, count in zip(lines, counts):
            if count < width / 2:
                continue
            row = [''] * (width + 1)
            for cell in line:
                if not numeric(cell):
                    row[0] += cell['text']
                    continue
                center = cell['x'] + cell['width']/2
                column = min(range(width), key=lambda i: abs(center - anchors[i])) + 1
                row[column] = (row[column] + ' ' + cell['text']).strip()
            table.append(row)
        return table
    return [[c['text'] for c in line] for line in lines]


def extract_document(path, output_dir, options=None):
    options = options or {}
    path, output_dir = Path(path), Path(output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)
    raw = path.read_bytes()
    result = {'source': str(path), 'source_sha256': hashlib.sha256(raw).hexdigest(),
              'method': '', 'tables': [], 'text': '', 'cells': [], 'uncertain_cells': [], 'images': []}
    tables = []
    suffix = path.suffix.lower()
    if raw.startswith(b'%PDF-'):
        suffix = '.pdf'
    elif b'<html' in raw[:1024].lower() or b'<table' in raw[:1024].lower():
        suffix = '.html'
    elif raw.startswith(b'\x89PNG'):
        suffix = '.png'
    elif raw.startswith(b'\xff\xd8\xff'):
        suffix = '.jpg'
    if suffix in ('.html', '.htm'):
        parser = TableParser()
        parser.feed(raw.decode('utf-8-sig'))
        tables = parser.tables
        result['method'] = 'html'
    elif suffix == '.pdf':
        import pdfplumber
        with pdfplumber.open(path) as pdf:
            selected_pages = set()
            for part in options.get('pages', '').split(','):
                if not part.strip():
                    continue
                numbers = [int(value.strip()) for value in part.split('-')]
                if len(numbers) > 2 or min(numbers) < 1 or max(numbers) > len(pdf.pages) or numbers[-1] < numbers[0]:
                    raise ValueError('PDF页码须在1到%d之间，范围起点不大于终点' % len(pdf.pages))
                selected_pages.update(range(numbers[0], numbers[-1] + 1))
            for index, page in enumerate(pdf.pages):
                if selected_pages and index + 1 not in selected_pages:
                    continue
                text = page.extract_text() or ''
                result['text'] += text + '\n'
                if text.strip():
                    for table in page.extract_tables():
                        tables.append([[cell or '' for cell in row] for row in table])
                else:
                    image = output_dir / ('page-%d.png' % (index + 1))
                    page.to_image(resolution=200).save(image)
                    result['images'].append(str(image))
        result['method'] = 'pdf_text'
    elif suffix in ('.png', '.jpg', '.jpeg', '.tif', '.tiff', '.webp'):
        result['images'].append(str(path))
    elif suffix in ('.csv', '.tsv'):
        tables = [list(csv.reader(io.StringIO(raw.decode('utf-8-sig')), delimiter='\t' if suffix == '.tsv' else ','))]
        result['method'] = 'table'
    else:
        raise ValueError('支持 HTML、PDF、图片、CSV、TSV；XLSX 可用 file_read')
    if result['images']:
        if options.get('vision'):
            result['method'] = 'vision'
            result['_agent_media'] = [{'type': 'image', 'path': p, 'mime_type': 'image/png' if Path(p).suffix == '.png' else 'image/jpeg', 'label': Path(p).name} for p in result['images']]
        else:
            result['method'] = options.get('ocr_engine', 'rapidocr')
            for page, image in enumerate(result['images'], 1):
                cells = ocr_cells(image, options)
                for cell in cells:
                    cell['page'] = page
                result['cells'].extend(cells)
                tables.append(cells_to_rows(cells))
            result['uncertain_cells'] = [c for c in result['cells'] if c['confidence'] < .9]
    for index, rows in enumerate(tables, 1):
        if not rows:
            continue
        target = output_dir / ('table-%d.csv' % index)
        with target.open('w', encoding='utf-8-sig', newline='') as stream:
            csv.writer(stream).writerows(rows)
        hint = options.get('table_hint', '').strip()
        result['tables'].append({'path': str(target), 'rows': len(rows), 'columns': max(map(len, rows)),
                                 'hint_match': bool(hint and any(hint in str(cell) for row in rows for cell in row))})
    result['success'] = True
    if not tables and not result['images'] and not result['text'].strip():
        raise ValueError('原件中没有提取到表格、文字或图片')
    return result


if __name__ == '__main__':
    request = json.load(sys.stdin)
    try:
        print(json.dumps(extract_document(request['path'], request['output_dir'], request), ensure_ascii=False))
    except (ImportError, OSError, ValueError, subprocess.SubprocessError) as exc:
        print(json.dumps({'success': False, 'error': str(exc), 'installation': '安装依赖：python -m pip install pdfplumber rapidocr-onnxruntime；或安装 Tesseract 与 chi_sim/eng 语言模型'}, ensure_ascii=False))
        sys.exit(1)
