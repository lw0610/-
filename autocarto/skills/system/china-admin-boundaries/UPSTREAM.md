# GeoCode 中国行政区边界来源与本地适配

来源为 [GeoCode-Release](https://github.com/zzhonglei/GeoCode-Release)，固定提交 `6e3534f7af3675b19bd3c5d54c94022959dccdd2`。采用其 `contributions/china-admin-boundaries` 完整技能正文及省、市、县三个完整数据文件，未按当前武汉测试裁剪源资产。上游 manifest 版本为 `0.1.0`，许可声明 MIT；仓库 LICENSE 保留于本目录，技能及 manifest 原文保留于 `upstream/`。

所有原始文件的地址、字节数、SHA-256 和 Git blob SHA-1 都记录在 `upstream-manifest.json`。执行模板读取数据时校验完整字节 SHA-256。

| 原始数据 | 要素数 | 字节数 | SHA-256 |
| --- | ---: | ---: | --- |
| `data/china_province.geojson` | 34 | 1,973,055 | `672b321924533dc2d4eb1429de322e3b612a5c9de860f518828a25e75ba6f262` |
| `data/china_city.geojson` | 375 | 4,989,113 | `a3038b09823597c5b03ea2563455cebcc90559e83764f5d49d38a1403ad8683a` |
| `data/china_county.geojson` | 2891 | 11,856,930 | `22db6c54b9cb0118ecdcc8a117bf2da4fc24a2229cdba7e6e55dbb5dd56e8246` |

上游正文声明数据来自天地图，三个 GeoJSON 实际均声明 `urn:ogc:def:crs:OGC:1.3:CRS84`，即经度/纬度顺序的 WGS84。这里记录该可核对来源与数据声明；没有独立认证所有坐标、数据时效或制图法律合规性。未引入 DataV、GCJ-02、OSM 或另一套默认行政区来源。

武汉检查使用实际数据按 `adcode=420100` 选取，仅有一个 `city` 要素，属性名称为武汉市，类型为有效 MultiPolygon，共369个环顶点。按声明坐标系统转换到 EPSG:32650，面积约8588.17平方公里；这用于验证测试对象，不是模板中写死的城市、代码、范围或面积条件。

本地改动仅为：添加 AutoCarto 可读 `workflow` 元数据和调用说明；将上游“按唯一 adcode 选取”转为 `templates/select_boundary.py` 可执行本地模板；名称查询返回候选，只有唯一完整身份才能导出；保留完整原始要素和坐标，并把单要素 GeoJSON、来源和几何核验记录写入当前 `output_dir`。模板不联网、不索取用户文件、不隐式修复/简化/偏移、不下载遥感影像。它由 GEE 专家已有的 `gee_run_script` 读取执行，不新增在线服务工具。

此包补齐“本地没有边界文件时如何准备研究区”的数据环节。GeoCode 的一般 OSM 取数能力属于另一条独立链：主 GeoAgent 委派 `osmfetch`，使用 `OsmSearch → OsmGetObjects → ReadGeoData`；这里没有把该专用 OSM 工具链误称为已移植。中国行政区默认来源遵循本上游边界技能，而 GEE 仍用 `load_region` 读取已经准备好的本地文件。
