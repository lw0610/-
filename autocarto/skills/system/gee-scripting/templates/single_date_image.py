"""GeoCode single-date workflow adapted for AutoCarto's initialized GEE runner.

Inject ee, heartbeat and check_coverage through runpy.run_path(init_globals=...).
Copyright (c) 2025 opencode; Copyright (c) 2026 zzhonglei.
Distributed under the MIT license in ../LICENSE.
"""

import math


def mask_s2_clouds_scl(image):
    """Keep valid S2 SR pixels excluding cloud, cirrus, shadow and defects."""
    scl = image.select("SCL")
    keep = scl.neq(0).And(scl.neq(1))
    for value in (3, 8, 9, 10):
        keep = keep.And(scl.neq(value))
    return image.updateMask(keep)


def select_single_date(
    collection,
    roi,
    *,
    bands,
    coverage_scale=30,
    min_coverage=0.99,
    max_candidates=10,
    cloud_property="CLOUDY_PIXEL_PERCENTAGE",
    mask_fn=mask_s2_clouds_scl,
):
    """Return the best checked same-day image and an explicit quality report.

    The caller supplies a spatially and temporally bounded collection and chooses
    whether a below-target result is acceptable. This function never downloads,
    expands a search window, merges different dates or changes output resolution.
    """
    if not bands or isinstance(bands, str):
        raise ValueError("bands must contain the output band names")
    bands = list(bands)
    if not all(isinstance(band, str) and band for band in bands):
        raise ValueError("bands must contain nonempty band names")
    if not math.isfinite(float(coverage_scale)) or coverage_scale <= 0:
        raise ValueError("coverage_scale must be positive and finite")
    if not math.isfinite(float(min_coverage)) or not 0 < min_coverage <= 1:
        raise ValueError("min_coverage must be in (0, 1]")
    if isinstance(max_candidates, bool) or not isinstance(max_candidates, int) or not 1 <= max_candidates <= 10:
        raise ValueError("max_candidates must be an integer from 1 to 10")

    # Keep ImageCollection.map image-valued. Only the top date summaries cross
    # the client boundary; grouping, sorting and truncation occur on the server.
    dated = collection.filter(ee.Filter.notNull([cloud_property, "system:time_start"]))
    dated = dated.map(lambda image: image.set("_selection_date", image.date().format("YYYY-MM-dd")))
    with heartbeat("按日期和平均云量筛选候选影像"):
        grouped = dated.reduceColumns(
            reducer=ee.Reducer.mean().group(groupField=0, groupName="date"),
            selectors=["_selection_date", cloud_property],
        ).get("groups", ee.List([]))
        ranked = ee.FeatureCollection(ee.List(grouped).map(lambda group: ee.Feature(None, ee.Dictionary(group))))
        candidates = (ranked.sort("mean").limit(max_candidates).toList(max_candidates)
                      .map(lambda feature: ee.Feature(feature).toDictionary(["date", "mean"]))
                      .getInfo())
    if not candidates:
        raise ValueError("指定研究区、日期和云量条件下没有候选影像；没有修改原始条件")

    print(f"检查最多 {len(candidates)} 个单日期候选", flush=True)
    best_image = None
    best = None
    checked = []
    for index, candidate in enumerate(candidates, 1):
        date = candidate["date"]
        day = dated.filterDate(date, ee.Date(date).advance(1, "day"))
        # mosaic uses the last valid pixel: place lower-cloud scenes last.
        masked = day.sort(cloud_property, False).map(mask_fn)
        image = masked.mosaic().select(bands).clip(roi)
        with heartbeat(f"检查候选 {index}/{len(candidates)}：{date}"):
            coverage = float(check_coverage(image, roi, scale=coverage_scale, bands=bands))
        if not math.isfinite(coverage) or not 0 <= coverage <= 1:
            raise ValueError(f"覆盖检查返回无效比例：{coverage}")
        record = {"date": date, "mean_cloud": float(candidate["mean"]), "coverage": coverage}
        checked.append(record)
        print(f"{date}：平均整景云量 {record['mean_cloud']:.1f}%，研究区有效覆盖 {coverage:.2%}", flush=True)
        if best is None or coverage > best["coverage"]:
            best, best_image = record, image
        if coverage >= min_coverage:
            break

    if best is None or best["coverage"] <= 0:
        raise ValueError("已检查候选在完整研究区内均无有效目标波段像元，停止下载")
    day = dated.filterDate(best["date"], ee.Date(best["date"]).advance(1, "day"))
    with heartbeat("记录所选观测场景"):
        provenance = ee.Dictionary({
            "scene_count": day.size(),
            "image_ids": day.limit(256).aggregate_array("system:id"),
        }).getInfo()
    report = dict(best)
    report.update({
        "bands": bands,
        "coverage_scale": coverage_scale,
        "target_coverage": min_coverage,
        "meets_target": best["coverage"] >= min_coverage,
        "candidate_count": len(candidates),
        "checked_candidates": checked,
        "scene_count": int(provenance["scene_count"]),
        "image_ids": provenance["image_ids"],
        "scene_ids_truncated": int(provenance["scene_count"]) > 256,
        "method": "same_day_cloud_masked_mosaic",
    })
    return best_image, report
