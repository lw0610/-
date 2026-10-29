---
name: print-layout
description: 打印布局创建、编辑与页面编排工具集合
kind: category
runtime: qgis
enabled: true
tags:
- layout
- print
tools:
- resource_preview
- layout_add_callouts
- layout_info
- layout_create
- layout_add_legend
- layout_set_legend_style
- layout_add_label
- layout_set_label_style
- layout_add_grid
- layout_add_map
- layout_set_map_extent
- layout_set_map_layers
- layout_add_scalebar
- layout_set_scalebar_style
- layout_add_north_arrow
- layout_set_north_arrow_style
- layout_add_decorative_border
- layout_get_item_properties
- layout_get_items_properties
- layout_move_item
- layout_move_callout
- layout_resize_item
- layout_align_items
- layout_set_item_background
- layout_set_item_frame
- export_print_layout
- layout_export_svg
- layout_export_image
- export_layout_pdf
- layout_add_attribute_table
- layout_add_overview
- layout_add_picture
- layout_add_shape
- layout_delete
- layout_distribute_items
- layout_duplicate
- layout_get_item_corners
- layout_group_items
- layout_list
- layout_save_template
- layout_lock_item
- layout_remove_item
- layout_rename
- layout_set_item_rotation
- layout_set_item_z_order
- layout_set_map_rotation
- layout_set_map_scale
- layout_set_page_background
- layout_set_page_size
- layout_ungroup_items
- import_layout_template
- layout_compose
display_name: 版面编排
domain: 版面与输出
system: false
hidden: false
auto_mount: ''
---

# 版面编排使用说明

复用当前工程版面时，先用 layout_list 查看版面与地图项，再用 layout_save_template 指定 layout 与输出 path。多个地图项须明确 main_map_item（ID 或 UUID）；工具只将保存副本的主图命名为 main_map，不修改工程中的版面，返回识别到的文本占位符。

先核实实际图层、样式、输出纸张、方向、标题/说明内容和交付格式。layout_compose 的 spec 选择声明式版式资源的名称、编号或路径，一次建立地图、边框、标题、图例、比例尺和指北针；标题与综述正文由调用者提供，工具不内置专题名称。版式按纸张锁定字号并记录锚点，grid 可关闭，legend_symbols 选择符号资源，legend_extra_items 用 label 与 symbol_type 选取条目，可覆盖 size_mm；也支持显式 image_path。

先按流程规定从底到顶逐个加载最终图层，加载完成后再排版。`layout_compose` 默认直接沿用图层面板中已可见图层的自然顺序。独立插图等需要不同图层时，可通过 `map_layers` 按从上到下顺序指定本地图框的图层，已有地图框用 `layout_set_map_layers` 的 `layers`；这些设置仅作用于版面，不自动加载、隐藏、重排或清理工程图层。配置 QPT 主图而省略 `map_layers` 时同样沿用已可见图层；未提供地图参数时保留模板原图层与范围。核对地图框返回的 `layer_ids`/`map_layer_ids`。

调用后先读 warnings，核对遮挡、文本溢出、边界和地图范围。重复执行明确 replace_existing，避免无意创建重名元素。import_layout_template 用于 QPT：template 选择 QPT 版式资源的名称、编号或路径，name 指定新布局，texts 一遍字面替换标题、综述及自定义占位符，HTML 元素的替换值会转义。需要调整主图时传 map_item_id、map_layers 与 extent_layers/extent，可用 padding 留边，legend_layers 控制图例；只传 template/name 会保留模板原图层与范围。核查返回的 item_ids、rects_mm、warnings 和 unreplaced_placeholders，不能假定所有模板都有同一元素ID。

图例名称按本次数据含义生成。layout_compose 和 layout_set_legend_style 的 legend_labels 接受图层 ID 或唯一名称到显示文字的映射，只改变图例名称，直接复用真实图层符号。相同含义和画法的主线、分支可在 legend_layers 中选一个代表图层，其他路线继续保留在主图；不同含义的符号分别列出。不要为了改名寻找外部箭头图片或改工程内部图层名。更新条目或字号后，工具按实际尺寸保持图例原有锚点，左下锚点的图例底边不会因条目减少而上移。

单项编辑先用 layout_info/layout_get_items_properties 获取真实ID、毫米矩形、CRS和状态。地图范围坐标应与地图框CRS一致，锁定本次图层以免其他任务图层进入图例。位置与大小均检查整个矩形，字体遵从所选版式；图文标注按组移动并保持锚点，不逐个拆散。

导出前检查标题、图例、比例尺、方向、图层、文字完整性和纸张边界；检查工具实际状态及文件，不以文件存在单独证明排版正确。批量分幅方法：references/批量出图.md，用 reference_read 按需读取。

## 参考资料

使用 reference_read 传入“print-layout/references/文件.md”读取：

- 批量出图：references/批量出图.md
