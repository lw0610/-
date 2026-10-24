"""表格行读取与旧参数转换。"""
import csv
import posixpath
import re
import xml.etree.ElementTree as ET
from zipfile import ZipFile
from typing import (
    Any,
    Dict,
    List,
    Optional,
)
from pathlib import Path


def _to_bool(v: Any) -> bool:
    if isinstance(v, bool):
        return v
    s = str(v or "").strip().lower()
    return s in {"1", "true", "yes", "y", "on"}



def _read_csv_rows(csv_path: Path) -> List[Dict[str, Any]]:
    with csv_path.open("r", encoding="utf-8-sig", newline="") as f:
        return list(csv.DictReader(f))



def _find_col(row: Dict[str, Any], aliases: List[str]) -> Optional[str]:
    key_map = {str(k).strip().lower(): k for k in row.keys()}
    for a in aliases:
        if a.lower() in key_map:
            return key_map[a.lower()]
    return None



def _get_col_value(row: Dict[str, Any], aliases: List[str], required: bool = False, default: Any = None) -> Any:
    col = _find_col(row, aliases)
    if col is None:
        if required:
            raise ValueError(f"缺少列: {aliases[0]}")
        return default
    return row.get(col, default)


def read_xlsx_rows(path, sheet_index=0, *, header_only=False):
    """按工作簿顺序读取一张表，支持共享字符串、内联文本和稀疏单元格。"""
    ns = {"s": "http://schemas.openxmlformats.org/spreadsheetml/2006/main"}
    relationship_ns = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
    package_ns = "http://schemas.openxmlformats.org/package/2006/relationships"
    with ZipFile(path) as archive:
        workbook = ET.fromstring(archive.read("xl/workbook.xml"))
        sheets = workbook.findall("s:sheets/s:sheet", ns)
        if not sheets:
            raise ValueError("工作簿没有工作表")
        if sheet_index < 0 or sheet_index >= len(sheets):
            raise IndexError("工作表序号超出范围")
        sheet = sheets[sheet_index]
        relation_id = sheet.get("{" + relationship_ns + "}id")
        relations = ET.fromstring(archive.read("xl/_rels/workbook.xml.rels"))
        targets = [item for item in relations.findall("{" + package_ns + "}Relationship")
                   if item.get("Id") == relation_id and item.get("TargetMode") != "External"]
        if len(targets) != 1:
            raise ValueError("工作表的文件引用无效")
        target = targets[0].get("Target", "")
        member = target.lstrip("/") if target.startswith("/") else posixpath.normpath(posixpath.join("xl", target))
        shared = []
        if "xl/sharedStrings.xml" in archive.namelist():
            root = ET.fromstring(archive.read("xl/sharedStrings.xml"))
            shared = ["".join(part.text or "" for part in item.iter("{" + ns["s"] + "}t"))
                      for item in root.findall("s:si", ns)]
        worksheet = ET.fromstring(archive.read(member))
        rows = []
        for row in worksheet.findall("s:sheetData/s:row", ns):
            values = []
            for cell in row.findall("s:c", ns):
                reference = cell.get("r", "")
                match = re.fullmatch(r"([A-Z]+)[0-9]+", reference)
                if not match:
                    raise ValueError("工作表包含无效的单元格引用")
                column = 0
                for char in match.group(1):
                    column = column * 26 + ord(char) - ord("A") + 1
                while len(values) < column:
                    values.append("")
                kind = cell.get("t", "")
                value = cell.findtext("s:v", default="", namespaces=ns)
                if kind == "s":
                    index = int(value)
                    if index < 0 or index >= len(shared):
                        raise ValueError("工作表共享字符串索引无效")
                    value = shared[index]
                elif kind == "inlineStr":
                    inline = cell.find("s:is", ns)
                    value = "".join(part.text or "" for part in inline.iter("{" + ns["s"] + "}t")) if inline is not None else ""
                elif kind == "e":
                    raise ValueError("工作表单元格含错误值")
                elif cell.find("s:f", ns) is not None and not value:
                    raise ValueError("工作表公式缺少缓存值，请先保存计算结果")
                values[column - 1] = value
            rows.append(values)
            if header_only and any(str(value).strip() for value in values):
                break
        return rows


def read_table_rows(path, required=(), aliases=None):
    """读取 CSV，或首张表头包含全部必填列的 XLSX 工作表。"""
    path = Path(path)
    if path.suffix.lower() == ".xlsx":
        if not required:
            return read_xlsx_rows(path)
        canonical = lambda value: str(value).strip().lstrip("\ufeff").strip().lower()
        names = {canonical(key): canonical(value) for key, value in (aliases or {}).items()}
        required = {canonical(value) for value in required}
        index = 0
        while True:
            try:
                rows = read_xlsx_rows(path, index, header_only=True)
            except IndexError:
                break
            header = next((row for row in rows if any(str(value).strip() for value in row)), [])
            columns = {names.get(canonical(value), canonical(value)) for value in header}
            if required.issubset(columns):
                # 选中之后才校验数据行；此表的错误不允许被下一张表掩盖。
                return read_xlsx_rows(path, index)
            index += 1
        raise ValueError("工作簿中没有包含必填列 " + "、".join(sorted(required)) + " 的工作表")
    if path.suffix.lower() == ".csv":
        with path.open(encoding="utf-8-sig", newline="") as stream:
            return list(csv.reader(stream))
    raise ValueError("对照表仅支持 UTF-8 CSV 或 XLSX")
