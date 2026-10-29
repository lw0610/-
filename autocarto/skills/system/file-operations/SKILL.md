---
name: file-operations
description: 目录、下载、打包与文件输出工具集合
kind: category
runtime: qgis
enabled: true
tags:
- file
- operations
tools:
- file_read
- file_write
- shell_execute
- create_directory
- file_downloader
- package_layers
- save_vector_features_to_file
- split_vector_layer
display_name: 文件操作
domain: 数据
system: false
hidden: false
auto_mount: ''
---

# 文件操作工具集

本工具包包含文件读取、文本写入、本机命令执行，以及目录、打包和文件输出操作。

`file_read` 读取当前会话、已授权工程目录、工程绑定图层文件，以及用户明确提供的文件或目录。用户提供目录时，先列举目录，再读取其中需要的文件；授权不包含父目录、相邻目录或指向目录外的链接。相对路径以会话 output 为基准。

UTF-8 文本（如 TXT、Markdown、CSV）直接读取；DOCX 自动提取正文中的段落和表格文字；XLSX 自动读取各工作表的单元格值，保留工作表名与行号，不计算公式，仅使用文件已保存的缓存值。文本支持 offset/limit 分页，注意 truncated/next_offset，不能把第一页当作全文。DOCX 中的图片、页眉页脚与其他嵌入附件不属于正文解析范围。

图片和 PDF 默认以真实媒体附件交给当前模型，能否理解取决于模型支持。PDF 也可以用 `as_text=true` 调用本机 `pdftotext` 提取并分页读取；未安装解析器、没有文本的扫描件或解析失败都会明确报错，需要相应媒体/OCR 能力，不能凭任务要求、文件名或模型记忆补造原文。其他二进制格式使用本机实际可用的解析工具，不能直接当文本读取。

`file_write` 将文本原样保存到当前会话 output，适用于脚本、CSV 和来源说明。它不把任意文字自动变成 Excel：需要 XLSX 时使用真实可用的写表库，不得只改扩展名。默认覆盖同一路径，返回实际文件路径、大小和 SHA256；需要保留既有文件时指定 overwrite=false。

`shell_execute` 在本机执行命令，默认工作目录是当前会话 output，可调用实际安装的 Python 或文档处理程序。这是本机执行，不是隔离沙箱；只处理本任务所需文件，不修改用户原件、不读取凭据。先核实命令和依赖是否存在，不假定安装了 OCR、PDF 或 Excel 库。命令失败要根据退出码与输出修正；超时或取消会终止进程，长输出明确标记截断。来源图片或 PDF 的提取结果保留原件并核验，不凭模型记忆补造数据。
