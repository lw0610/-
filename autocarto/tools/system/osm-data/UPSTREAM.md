# OSM 工具来源

参考 GeoCode-Release 发行包 GeoCode-source-v0.9.6（2026-06-25）。该源码包 LICENSE 为 MIT、Copyright (c) 2025 opencode；当前提供的源码包未含独立 GeoCode NOTICE，未虚构其版权文本。保留原 LICENSE。

本地以 Go 重写行为，不引入 TypeScript 运行时。几何拼接按 OSM 节点/线/关系结构实现；闭合线判面表来自 osm2geojson-lite 2.0.1 的 dist/polytags.json。2026-10-25 核实 npm 官方注册表 https://registry.npmjs.org/osm2geojson-lite/2.0.1 元数据与发行包 LICENSE 均为 MIT，Copyright (c) 2025 Xihua Duan，保留声明。

上游源码 SHA-256：

- `packages/opencode/src/tool/geo/osm-search.ts`: `42a239e7d76113b0cfc2d52357ebf130975bab38cb0afa77ee99fd940b897433`
- `packages/opencode/src/tool/geo/osm-probe.ts`: `04d11406d4ba7d8e3389228c34fc295f76a43b6e66d669d6c573d314c78449b5`
- `packages/opencode/src/tool/geo/osm-get-objects.ts`: `6c741673e0348891aa7e57a5d11ee7aa4bf673f2b6c20fa76fe81f207e06a09d`
- `packages/opencode/src/tool/geo/osm-get-features.ts`: `5e37a50b77141a2e87a010045eba390e2decdd85c48be1769a45ca3fb1adb819`
- `packages/opencode/src/tool/geo/osm/client.ts`: `c380dcfa7399bb4c2103b481e743f1ef3e80687e6c4dc170a757eae353624264`
- `packages/opencode/src/tool/geo/osm/query.ts`: `0f643a810c32c04ef0a9a2527430f15085acf473123ebf49ab6187d21afe8a3a`
- `packages/opencode/src/tool/geo/osm/extent.ts`: `e8c0f7ac6cfa0f4189575085e02633f83dfa607b1b57ad581a409366b29e6a06`
- `packages/opencode/src/tool/geo/osm/convert.ts`: `d97383c8bdfcfd1a57107c2cdb650928a923442b7570f463133274971ddf46e5`
- `packages/opencode/src/tool/geo/osm/centroid.ts`: `730aea76a60ac15c1f0658a3ddb45213e342ff04d1511f2d345435c25a3dd9b8`
- `packages/opencode/src/tool/geo/osm/output.ts`: `d1fcd6fe4fb19f8f2c712b9268834c4dcd3fe610d7904cd4ca46073f4026025c`
- `packages/opencode/test/tool/osm-query.test.ts`: `8c37f77b86d8964f616c5b8e6e35c85f1d0cc368736fcae31d0b7a801aebe7ab`
- `packages/opencode/test/tool/osm-extent.test.ts`: `6ae7fa995d978012a04633d3f3450be34d583bef87c9cf3562b1bad8962ca316`
- `packages/opencode/test/tool/osm-convert.test.ts`: `7d67356606b619440b25ab88d249ec70d070e7ebc15bc5058b596c87dbcb97c1`
- `packages/opencode/src/agent/prompt/osm-download.txt`: `9dc5446b8655254cd25dc35095418ae37adebd757b857c6f7e8f8d43c326a360`

第三方包：https://registry.npmjs.org/osm2geojson-lite/-/osm2geojson-lite-2.0.1.tgz

SHA-256: `1593c753bc84eb69a540c6209d963aa54e0c8b0bca86b2745953227268951417`。

本地修改：独立扩展服务配置、由 OSM 服务总开关控制下载、输出限定当前会话/工程、默认不覆盖、逐文件来源JSON、失败范围与几何报告、中文说明及中国行政边界提示、会话文件登记。OSM数据许可为ODbL 1.0，与工具代码MIT许可不同。

2026-10-27 同时核对上游 `osm-download.txt` 的模型使用逻辑：名称与地区消歧、批量请求先统计并按需检查标签分布、少量 ID 直接下载、标签并集、POI 包含 node/way/relation、AOI 保留面、按几何族分别核验、保留/丢弃字段及部分结果报告，现体现在本地工具包说明中。`osm_probe` 并非每次对象 ID 下载的必经步骤。

保留的本地补强包括：已有中国行政边界优先复用；单一文件范围下载后通过 QGIS 精确裁剪并返回真实状态；响应空闲超时与总时限分开计算；限制切换节点次数并记录下载进度。不会为了对齐上游而回退这些已实现行为。上游的 `file:` 查询本身只保证范围候选，不能把范围查询等同于精裁。

上游提供独立 `osmfetch` 专家，本项目由人工配置了 `osm-data` 工具包的既有角色执行，未复制上游整个角色架构。以上为源码与本地契约核对，不代表所有区域和标签组合都已完成在线下载验证。
