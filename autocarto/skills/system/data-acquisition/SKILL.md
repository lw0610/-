---
name: data-acquisition
display_name: 数据获取
description: 系统按已选数据需求执行安全解压、规范化与派生。
kind: category
domain: 系统
hidden: true
system: false
tools:
  - archive_extract
  - data_normalize
  - data_derive
  - data_load_tiles
---

# 数据获取

本工具包由统一数据服务的系统获取执行器使用。来源和参数必须来自已校验的获取计划；不执行下载文件中的代码，不更改用户原件。所有派生文件写入数据仓临时目录，核验后才可用于后续制图。

来源目录及类别词表位于此工具域的 assets 下，由 Go 端统一解析。来源记录须保留实际地址、许可、获取时间与摘要；不得记录密钥值。
