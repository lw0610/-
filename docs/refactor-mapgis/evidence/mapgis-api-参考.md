# MapGIS 接口参考

> 写 C# 宿主工具时，按以下优先级取证：
>
> 1. **本文 §2**：第一版移植在真机上用过、确认可行的调用。文件路径相对 `/home/workspace/Projects/enio-mapgis/autocarto_mapgis/src/`。
> 2. **M02 导出的接口清单**（`evidence/mapgis-api/`）与本机 SDK 帮助。
> 3. **本文 §3**：中地数码公开的 MapGIS 移动端 SDK 文档。它与桌面版共享对象模型，只用来印证类名与方法名，签名以 1、2 为准。
>
> 公开网络上没有 MapGIS 10 .NET 桌面开发的完整 API 参考；司马云（smaryun.com）的开发包需要登录下载。

## 1. 程序集与命名空间

csproj 引用的 9 个程序集都位于 `$(MapGISDesktopDir)`，均为 `Private=false`：

| 程序集 | 主要命名空间 | 用途 |
|---|---|---|
| `MapGIS.PluginEngine.dll` | `MapGIS.PluginEngine` | `IPlugin`、`IConnect`、`IApplication` |
| `MapGIS.WorkSpaceEngine.dll` | — | 工作空间 |
| `MapGIS.GeoMap.dll` | `MapGIS.GeoMap` | `Document`、`Map`、`Maps`、`MapLayer`、`VectorLayer`、`RasterLayer`、`Layout`、版面元素、标注、专题 |
| `MapGIS.GeoDataBase.dll` | `MapGIS.GeoDataBase`、`.Convert`、`.GeoRaster` | `Server`、`DataBase`、`SFeatureCls`、`AnnotationCls`、`DataConvert`、`RasterDataSet` |
| `MapGIS.GeoObjects.dll` | `MapGIS.GeoObjects`、`.Geometry`、`.Info`、`.Att`、`.SpatialRef` | 几何、`LinInfo`/`RegInfo`/`PntInfo`/`TextAnnInfo`、`Fields`/`Record`、`SRefData` |
| `plugin\MapGIS.WorkSpace.Plugin.dll` | `MapGIS.WorkSpace` | `WorkSpacePanel` |
| `MapGIS.Desktop.UI.Controls.dll` | `MapGIS.Desktop.UI.Controls` | `WorkSpaceTree` |
| `MapGIS.UI.Controls.dll` | `MapGIS.UI.Controls` | `MapLayerHelp` 等 |
| `MapGIS.GISControl.dll` | `MapGIS.GISControl` | `MapControl` |

第一版源码中的 `using` 统计：`MapGIS.GeoMap` 56 处、`.GeoObjects.Geometry` 26、`.GeoObjects.Info` 23、`.GeoDataBase` 22、`.GeoObjects` 13、`.GeoObjects.Att` 8、`.PluginEngine` 5、`.GeoObjects.SpatialRef` 5。

## 2. 第一版已验证的调用

### 2.1 插件生命周期与线程

| 用法 | 位置 | 说明 |
|---|---|---|
| `class AutoCartoPlugin : IPlugin, IConnect`，`OnConnection(IApplication)`、`OnDisconnection()` | `PluginEntry.cs` | `OnConnection` 运行在宿主 UI 线程 |
| `IApplication.ApplicationLoadedEvent` 加上 `System.Windows.Forms.Application.Idle` | `AutoCartoRuntime.cs` | 宿主初始化完成后才连接后端，避免与默认文档初始化并行 |
| `WindowsFormsSynchronizationContext` 的 `Send` | `Support/UiDispatcher.cs` | 工具在 UI 线程同步执行 |
| `[HandleProcessCorruptedStateExceptions]` | `UiDispatcher.cs`、`Handlers/Support/NativeGuard.cs` | 拦截原生访问冲突。没有这个属性时，异常逃出消息循环，MapGIS 直接退出（实测，见源码注释） |

### 2.2 文档与地图

| 用法 | 位置 | 说明 |
|---|---|---|
| `IApplication.Document` | `Handlers/Support/DocumentSession.cs` | 属性只读，但可以对它调用 `Open`、`SaveAs`；插件不得 `Close`/`Dispose` |
| `document.GetFilePath()`、`document.IsNew`、`document.Title`、`document.GetMaps()`、`document.GetScenes()` | `DocumentSession.cs`、`CreateMapxTool.cs` | 打开、新建期间路径可能短暂为空 |
| `document.Open(path) != 1` 视为失败 | `OpenMapxTool.cs:39` | 返回值为整数 |
| `document.SaveAs(path)` 返回 bool；`document.Save()` 返回 bool | `CreateMapxTool.cs`、`SaveMapxTool.cs` | 保存后不要立即回读原生文档（源码注释：可能崩溃） |
| `document.ClosedDocument`、`CreatedDocument`、`OpenedDocument` 事件 | `DocumentSession.cs` | 用于判断关闭与新建 |
| `maps.GetMap(i)`、`maps.Append(map)` | `AddMapTool.cs:59`、`GetLayersInfoTool.cs` | 新建地图 |

### 2.3 视图

| 用法 | 位置 |
|---|---|
| `container.CreateContentsView(...)`、`MapControl.ActiveMap`、`MapControl.Restore()`、`control.GetMapScale()`、`tree.RefreshDocumentItem(...)` | `Handlers/Support/MapViewSync.cs` |

视图键复用 `"{地图名}$MapView"`。全部步骤尽力而为，失败只记日志（第一版 README §8.4）。

### 2.4 本地库与数据加载

| 用法 | 位置 | 说明 |
|---|---|---|
| `new Server()`、`server.Connect("MapGISLocalPlus", "", "")`、`server.OpenGDB("database")` | `DataLoadVectorTool.cs:51-80` | 共享连接建立后不关闭；图层依赖它 |
| `gdb.GetXclses(XClsType.SFCls, 0)`、`gdb.GetXclsName(XClsType.SFCls, id)` | `DataLoadVectorTool.cs` 中的 `QuerySfClsNames` | 列出要素类 |
| `new DataConvert()`、`OpenDestination(库URL)`、`OpenSource(文件)`、`Convert()`、`Close()`、`Dispose()` | `DataLoadVectorTool.cs` 中的 `TryConvertToGdb` | 返回值大于 0 为成功；不要预建空类，也不要对 GeoJSON 调 `PreCheck`（源码注释） |
| `new SFeatureCls(gdb)`、`cls.Open(name, 0)`、`cls.URL`、`cls.Count`、`cls.GeomType`、`cls.Fields` | `DataLoadVectorTool.cs` 中的 `TryAttachLayer` | 新建类的 URL 可能为空，按“库 URL + `/sfcls/` + 类名”拼接 |
| `MapLayerHelp.CreateMapLayerByUrl(classUrl, false)`，`layer.Name = …`，`map.Append(layer) >= 0` | 同上 | 第二个参数 `false`：不弹 HDF 子集对话框 |
| `new RasterDataSet()`、`dataSet.Open(path, RasAccessType.RasAccessType_ReadOnly)`、`new RasterLayer()`、`rasterLayer.AttachData(dataSet)` | `DataLoadRasterTool.cs:76-104` | 栅格不入库 |
| `cls.Select(QueryDef)`、`cls.GetInfo(oid)`、`cls.UpdateInfo(oid, info)` | `LayerSetGraduatedFillTool.cs`、`Support/LayerToolSupport.cs:1099,1285` | 写要素图形参数（本轮改为专题，只在探查不通过时使用） |
| `AnnotationCls`、`Record`、`SaveEdit()` | `RouteDrawTool.cs:882-1170` | `SaveEdit` 返回未文档化的整数，须回读内容确认（源码注释） |
| 已有的类只清空不删除 | `RouteDrawTool.cs:1489` | 删除会触发原生访问冲突 |
| 字符串字段要设字符宽度，并在添加后回读字段长度 | `RouteDrawTool.cs:1423-1445` | 只设 `FieldLength` 时 `MskLength` 为 0，SDK 会拒绝添加 |

### 2.5 参照系

| 用法 | 位置 |
|---|---|
| `var srs = new SRefData(); SRefData.EPSGToSRS(code, ref srs) > 0` | `Handlers/Support/Plot/RouteCrs.cs:196-199`、`RouteDrawTool.cs:1614` |
| `SRefData.WKTToSRS(wkt, false, ref srs)` | `RouteCrs.cs:211-215` |

EPSG 号不是数据库的 SrID：先解析参数，再注册到当前数据库（`RouteDrawTool.cs:1613` 注释）。

### 2.6 系统库、颜色、字体、符号

| 用法 | 位置 |
|---|---|
| `SystemLibrarys.GetSystemLibrarys().GetDefaultSystemLibrary()`、`new SystemLibrarys().GetSystemLibrary(0)` | `Support/LayerToolSupport.cs:393-402` |
| `sysLibrary.GetColorLibrary()`、`GetFontLibrary()`、`GetSymbolLibrary()` | `LayerToolSupport.cs` |
| `library.GetColorByNo(no)`、`color.GetRGB()`、`library.AddColor(color)`、`library.RemoveLastColor()`、`library.Save()` | `LayerToolSupport.cs:654` 一带 |
| `fontLibrary.GetFontNo(name)`、`GetFontName(no)` | `LayerToolSupport.cs` |
| `symbolLibrary.GetSymbol(...)`、`GetDefaultSymbol(...)` | `LayerToolSupport.cs` |
| `styleLib.Open(sysLibrary.Guid, filter)` | `LayoutAddNorthArrowTool.cs:280`、`LayoutAddDecorativeBorderTool.cs:230`、`LayoutSetNorthArrowStyleTool.cs:287` |
| 按文件指纹安装线符号与点符号 XML | `Support/LineSymbolLibrary.cs`、`RoutePointSymbolLibrary.cs` |
| `VectorLayer.SymbolShow`、`SetScaleRangeOfSymbolShow(1e100, 0)`、`GetScaleRangeOfSymbolShow`、`ShowRegBorder`、`RegBorderSymbolShow`、`RegBorderFollowZoom`、`FillReg`、`Themes` | `LayerToolSupport.cs:1488-1660`、`LayerSetGraduatedFillTool.cs:608-627` |
| `new UniqueThemeInfo()` | `LayerToolSupport.cs:1965` |

### 2.7 标注

| 用法 | 位置 |
|---|---|
| `GeneralVectorLabel label = hidden ? new UniqueLabel() : new SimpleLabel()`；`label.LabelExpression`、`HighlightClr`、`HighlightLabelUnPlaced`、`Priority`、`Visible`、`LabelGeomType`、`LabelUnPlaced`、`LabelClientOutSide` | `LayerToolSupport.cs:1878-1977` |
| `new LabelInfo { AnnInfo, Caption, IsVisible, MinMapScale, MaxMapScale }` | 同上 |
| `RegionPlaceInfo { DuplicateType, LimitLabelSmallRegion, SmallRegionMaxArea, TryLabelOutside, Type = CurvedPlace / HorizationPlace }`、`LinePlaceInfo { RepeatType }`、`PointPlaceInfo` | 同上 |
| `layer.Label = label`；`layer.LabelRendererVisible` | `LayerToolSupport.cs:1977`、`LayerHideLabelsTool.cs` |
| `TextAnnInfo { Color, Height, Width, Ovprnt, Bold, Ifnt（中文字体号）, Chnt（西文字体号）, Italic, ItalicType, Ifnx, FontAngle, BackClr（描边色）, BackExp（描边宽，毫米）}` | `LayerToolSupport.cs`、`LayoutAddLabelTool.cs:330` |

### 2.8 版面

| 用法 | 位置 |
|---|---|
| `document.GetLayout()` | 全部 `Layout*Tool.cs` |
| `layout.GetLayoutSize()`、`SetLayoutSize(...)`、`Clear()`、`Append(element)`、`Remove(element)`、`GetElement(i)` | `Support/RouteLayoutSupport.cs`、`LayoutAddLegendTool.cs` |
| `new TextElement { Text, Geometry = new Dot(x, y), TextInfo = textAnnInfo }`，追加后再赋一次 `Name` | `LayoutAddLabelTool.cs:336-349` |
| `MapElement.SetMap/GetMap`、`SetViewRange/GetViewRange`、`SetRange` | `RouteLayoutSupport.cs`、`Support/LayoutSupport.cs:175-205` |
| `mapElement.GetGrid()`、`SetGrid(...)`；`GridInfo.SetPosX/SetPosY/SetCoord`，`coordInfo.SetInfo` | `LayoutAddGridTool.cs` |
| `PolygonElement`、`LineElement` 的 `SetGeometry`、`SetRegInfo`、`SetLinInfo` | `LayoutAddLegendTool.cs`、`LayoutAddScalebarTool.cs` |
| `LegendElement.GetLayerItem`、`SetGeometryDot`、`GetGeometryDot`；`new LegendItem(library.Guid, LegendType.Item) { LabelText }`；`new LegendLayer()` | `LayoutAddLegendTool.cs`、`LayoutAddClassifiedLegendTool.cs:837-845` |
| `new NorthArrowElement()` 加样式库 | `LayoutAddNorthArrowTool.cs` |
| 图片元素：`RouteLayoutPictures.CreateElement`（已验证 PNG、BMP） | `Support/RouteLayoutPictures.cs` |
| 元素分组：`group.Append(...)` | `RouteLayoutSupport.cs` |
| `layout.OutputToPDFFile(path, width, height, 72, FileMode.RGB / FileMode.CMYK)` | `LayoutExportPdfTool.cs:141-146` |
| `layout.OutputToStream(width, height, type, false, backgroundColor, true, out bytes)` | `Support/LayoutImageExportSupport.cs:35` |

版面坐标在 MapGIS 内部是左下角原点、毫米（第一版 README §3.4），对外须换算为 QGIS 约定（`01` §7.3）。

### 2.9 第一版总结的禁忌

1. 原生调用一律包 `NativeGuard.Try`：参数语义不对时，MapGIS 抛 `AccessViolationException`，普通 `catch` 拦不住。
2. Newtonsoft.Json 钉在 13.0.3（宿主自带版本）。
3. 宿主运行时不要改插件目录里的文件：DLL 被锁，构建报 MSB3021/MSB3027。
4. 符号 XML 是 gb2312 编码。颜色写色号；线符号周期 10000 设计单位；纯白（色号 9）会被当作背景色；不用 `<Circle>`。
5. 要素类同名可能触发原生崩溃：转换前比对类名集合。
6. 删除库内容会触发原生访问冲突：一律清空后复用。
7. 点符号尺寸受“跟随缩放”和基准比例尺影响，要显式关闭或对齐（`RouteDrawTool.cs:586` 注释）。
8. 层级 `SimpleLabel/K10` 在路线注记路径上无法显示文字，路线名称改用文字注记对象（`RouteDrawTool.cs:881` 注释）。

## 3. 公开对象模型（MapGIS 移动端 SDK）

来源：npm 包 `@mapgis/mobile-react-native@1.1.4` 的文档（unpkg 托管）。移动端与桌面端共享 MapGIS 核心对象模型；下表只用于印证类名，桌面 .NET 签名以 M02 的导出为准。

| 类别 | 类 |
|---|---|
| 地图与图层 | `Document`、`Map`、`Maps`、`MapLayer`、`VectorLayer`、`GroupLayer`、`ServerLayer`、`TileMapServer`、`VectorTileMapServer`、`OGCWMTSMapServer` |
| 专题 | `Theme`、`Themes`、`ThemeInfo`、`SimpleTheme`、`UniqueTheme`、`UniqueThemeInfo`、`RangeTheme`、`RangeThemeInfo`、`MultiClassTheme` |
| 符号参数 | `LinInfo`、`RegInfo`、`PntInfo`、`TextAnnInfo`、`LineStyle`、`PointStyle`、`FillStyle`、`TextStyle` |
| 标注 | `Label`、`LabelInfo`、`SimpleLabel`、`VectorLabel`、`GeneralVectorLabel`、`LinePlaceInfo`、`PointPlaceInfo`、`RegionPlaceInfo` |
| 空间分析 | `SpaAnalysis`、`SpaCalculator`、`SpaRelation` |
| 地理数据库 | `DataBase`、`SFeatureCls`、`AnnotationCls`、`Field`、`Fields`、`Record`、`RecordSet`、`QueryDef`、`FeatureQuery` |
| 投影 | `SRefData`、`SpaProjection`、`CoordinateConvert`、`ElpTransformation`、`ElpTransParam` |

几个与本方案相关的类：

- **`SpaAnalysis`**：
  - 缓冲：`buffer(geom, leftDis, rightDis, sRefSrc)` 和 `bufferWithEndCapStyle(…, endCapStyle)`，端点样式 0 为圆、1 为垂直、2 为平；
  - 叠加：`intersection`、`union`、`difference`、`symmetricDifference`；
  - 裁剪与合并：`merge`、`mergeGeoPolygons`、`clip`、`clipWithType`（0 内裁、1 外裁）；
  - 其他：`boundary`、`convexHull`、`split`、`setTolerance`。

  全部是**几何级**接口，经纬度数据要先 `setTolerance(1e-9)`。这是“分析类工具放到 geo 运行时”的依据之一（README §3.2）。
- **`SpaProjection`**：`setSourcePara(SRefData)`、`setDestinationPara(SRefData)`、`projTrans(Dot)`；椭球不同时需要 `setEllipTransId` 或 `setEllipTransParam`。
- **`RangeTheme`**：`setExpression(字段表达式)`、`append(RangeThemeInfo)`、`setItem`、`getItem`、`setDefaultInfo(ThemeInfo)`、`setAllOtherDataItemInfoSource`、`clear`、`getCount`。
- **`Themes`**：`append(theme)`、`remove(index)`、`clear()`、`getCount()`、`getThemeByIndex`、`getThemeByName`。第一版桌面代码中用到 `layer.Themes`，说明专题挂在图层上（M02 第 3 项确认）。

## 4. 待 M02 确认的接口

见 `03` §6 的 19 项。M02 的结论写入 `evidence/M02-probes.md`，并回填本文 §2，标注“M02 验证”。

## 5. 参考链接

- MapGIS 移动端 SDK 文档：<https://unpkg.com/@mapgis/mobile-react-native@1.1.4/doc/index.html>（`SpaAnalysis.html`、`SpaProjection.html`、`RangeTheme.html`、`Themes.html`、`UniqueTheme.html`）
- MapGIS 10 Objects 开发入门手册目录（只有章节标题，没有接口）：<https://malagis.com/mapgis-10-objects-dev-handbook.html>
- MapGIS Desktop Java 开源版（插件结构参考，不含接口文档）：<https://github.com/MapGIS/MapGIS-Desktop-Java>
- 第一版移植 README：`/home/workspace/Projects/enio-mapgis/autocarto_mapgis/README.md`（工具清单、协议、符号 XML 规格、编译加载与排错）
