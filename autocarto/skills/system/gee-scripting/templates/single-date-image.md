# 单日期影像筛选模板

本地 `single_date_image.py` 将 GeoCode 的“按日期平均云量预选 → 最多10个候选 → 同日云掩膜拼接 → 覆盖检查 → 达标早停”流程封装为函数，便于复用。它不是全任务的参数默认配置。

适用于单日期真实观测。年度/季节代表产品使用 `../references/image-composite.md` 的时间合成流程。先准备真实、有效的研究区；不要因为日期范围未给而让用户填写完整卫星参数表。

`gee_run_script` 中按 skill 返回的绝对 base directory 读取并调用：

```python
from pathlib import Path
import json
import runpy

# 这些变量由本次任务确定；路径来自 skill 工具和工程，日期不是通用默认值。
# skill_base_dir: skill 工具返回的绝对 base directory
# boundary_path: 已验证的单要素研究区文件
# start_date / end_date: 有界检索范围；end_date不包含在范围内
# cloud_limit / export_crs: 按任务和数据目录确定

template = runpy.run_path(
    str(Path(skill_base_dir) / "templates" / "single_date_image.py"),
    init_globals={"ee": ee, "heartbeat": heartbeat, "check_coverage": check_coverage},
)
roi = load_region(boundary_path).geometry()
collection = (ee.ImageCollection("COPERNICUS/S2_SR_HARMONIZED")
              .filterBounds(roi)
              .filterDate(start_date, end_date)
              .filter(ee.Filter.lt("CLOUDY_PIXEL_PERCENTAGE", cloud_limit)))
image, report = template["select_single_date"](
    collection, roi, bands=["B4", "B3", "B2"], coverage_scale=30,
)
report.update({
    "dataset": "COPERNICUS/S2_SR_HARMONIZED",
    "search_start": start_date,
    "search_end_exclusive": end_date,
    "cloud_filter_lt": cloud_limit,
})
print(json.dumps(report, ensure_ascii=False), flush=True)
Path(output_dir, "selection_report.json").write_text(
    json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8"
)
if not report["meets_target"]:
    # 沿用上游最佳候选交付逻辑；有有效像元可以下载，但必须披露未达目标。
    print(f"最佳单日期 {report['date']} 的有效覆盖为 {report['coverage']:.2%}，"
          f"未达 {report['target_coverage']:.0%} 目标；下载将保留真实缺口。", flush=True)
path = download_image(image, f"s2_{report['date']}.tif", region=roi,
                      scale=10, crs=export_crs)
print(path)
```

上面的10米RGB和S2集合只用于S2示例。其他传感器需要对应的云属性、掩膜函数、波段和像元大小；`select_single_date` 支持显式 `mask_fn` 和 `cloud_property` 参数。用户要求原始未掩膜影像时不能将去云产品当作原始数据交付，应调整流程并描述结果。

函数返回 `(image, report)`。`report.coverage` 是 float，范围0到1；`meets_target` 明确表示是否达到99%有效覆盖目标。`checked_candidates` 保留实际检查记录，`image_ids` 最多记录256个场景且附 `scene_ids_truncated`，不无界读取元数据。没有候选、全无效或覆盖返回异常时抛错。最佳候选有正有效覆盖时，上面的调用示例会下载并说明不足99%；用户另有明确最低覆盖要求时应遵从该要求，不能将部分覆盖宣称为完整影像。

下载结果以原研究区 ROI 检查有效像元，不能把输出栅格投影外接框中 ROI 外的空白计入缺口。已有相同数据、日期、ROI、波段、分辨率和处理方法的已验证成果时直接复用；复制或加载已有文件不需要重新下载。保留完整 ROI，让 `download_image` 内部分块；遇到超时先检查具体原因和已有成果，不另写南中北条带、不同矩形或并行大下载来绕过。

相对上游模板的必要修正：上游帮助函数实际返回float，模板却按字典索引，本实现使用float；日期分组保留ImageCollection类型并在服务器完成排序和截断；云量降序再拼接以保证低云优先；目标波段共同有效才计入覆盖；增加空结果和无效数值检查。完整来源及许可见 `../UPSTREAM.md`。
