---
name: quality-feedback
description: 使用已配置的千问多模态模型检查 QGIS 初版地图，输出有图像证据、可供制图智能体独立判断的排版与配色建议，并对 original/final
  进行终审
kind: category
runtime: cli
enabled: true
tools:
- prepare_quality_review
- load_review_images
display_name: 质量审查
domain: 质检
system: false
hidden: false
auto_mount: ''
---

# 质量审查使用说明

只审查已有导图并输出证据与建议，不生成候选图，也不调用 Image 2。

使用已配置的多模态模型读取真实导出图片与布局快照，检查用户任务、locked_content、所选版式及真实元素状态。审美偏好不能推翻用户要求或版式规则。只对能指出具体证据的问题提出最小修改，不凑满审查项，不改写数据、语义、正文或锁定样式。元素矩形与地图框相交不等于遮住信息，应结合图片识别实际被遮挡对象；具体保护对象和合法锚点由当前流程与版式提供。

快照中的 legal_anchors 描述版式允许的锚点位置，先结合它判断位置是否合法；综述正文来自调用者，不凭审美改写。审查图片 DPI 由工具自动选择。font_locked 元素默认拒绝字号改动；只有任务明确允许解锁且调用传 unlock_font=true 才可修改，不通过缩放外框绕过锁定。

## 八个维度

- overlap：压盖、遮挡、裁切和引线交叉，记录遮挡与被遮挡对象。
- layout：位置、页面平衡、主图中心和页面边界。
- alignment_spacing：对齐、间距、页边距与留白。
- visual_hierarchy：标题、主图、图例、说明和辅助元素的层级。
- typography：文字完整性、换行、裁切和可读性；遵从已锁定字体与字号。
- color：色彩协调和对比度，保留具有数据含义的颜色映射。
- map_readability：主要信息、线、点和标注与底图的视觉分离。
- style_consistency：图例、比例尺、指北针、边框和标注的风格一致性。

## 初审 initial_review

调用prepare_quality_review，传真实original_image_path、layout_snapshot_path、task_requirement和locked_content。工具返回受保护的original_image_path、review_manifest_path、reserved_final_image_path及真实图片与元素状态；本次准备结果有效时复用。快照缺失就报告缺口，不猜坐标、DPI或ID，不把快照手工改成工具参数。建议只是供实施者复核的候选方案。

仅输出一个JSON对象：stage=initial_review、review_manifest_path、original_image_path、reserved_final_image_path、overall_judgment=pass|needs_improvement、summary、dimension_results、suggestions。dimension_results以八个英文维度为键，每项含has_issue、finding、affected_item_ids、evidence，八项都保留，不打分。八项均无问题时pass且suggestions=[]；每个has_issue=true都有对应建议。

suggestions每项包含suggestion_id、priority(high|medium|low)、category、target、target_item_ids、problem、evidence、desired_result、adjustment、reason、executable、unavailable_reason。ID只能取自快照，组元素按工具真实契约保持完整。evidence含region、observation、obscured_content、confidence(0..1)。desired_result说明目标与验收；executable=true时adjustment={"tool_name":"真实工具名","parameters":{}}，否则adjustment=null并解释缺口。移动/缩放应检查完整矩形、容器、页面和其他元素；没有可靠目标位置仍报告问题，不编造可执行坐标。可用工具和参数边界以工具定义与校验器为准，不在说明里硬编码元素白名单。

## 终审 final_review

调用load_review_images(review_manifest_path,final_image_path,applied_suggestions)读取original+final两张真实图片及任务、锁定内容和状态。保留初审dimension_results及完整suggestions，按suggestion_id对照真实执行结果。参数执行不等于问题改善；超出页面/容器、新遮挡、锁定内容变化或可读性降低均属回归。不能仅用扩大文本框留下空白假装改善文字。

仅输出一个JSON对象：stage=final_review、final_passed、summary、dimension_results、suggestion_results、final_content_changes、remaining_issues。suggestion_results各项status为correctly_applied|partially_applied|not_applied|skipped|regression。只有原高/中优先级问题解决、没有新回归且锁定内容保持才final_passed=true。终审输出事实与剩余问题，后续安排由流程决定，不自行无限循环修改。
