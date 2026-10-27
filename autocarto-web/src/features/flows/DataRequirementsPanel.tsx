import type { FlowPlan } from './types'
import AcquisitionPlanPanel from '@/features/flow-run/AcquisitionPlanPanel'
export default function DataRequirementsPanel({
  plan,
}: {
  plan: FlowPlan | null
}) {
  if (!plan?.requirements?.length)
    return (
      <p className="text-xs text-zinc-500">
        此流程尚未声明数据需求，输入由用户提供。
      </p>
    )
  return (
    <div className="space-y-4 text-xs">
      {plan.requirements.map((requirement) => {
        const items =
          plan.acquisition_plan?.items.filter(
            (item) => item.requirement === requirement.name,
          ) || []
        return (
          <section
            key={requirement.name}
            className="space-y-2 rounded border border-zinc-200 p-3 dark:border-zinc-800"
          >
            <h3 className="font-medium">{requirement.name}</h3>
            <p>
              绑定：{requirement.list || items[0]?.input || requirement.name}
            </p>
            <p>
              范围：{requirement.extent || '运行时确定'} · 比例尺：
              {requirement.scale || '未指定'}
            </p>
            {requirement.category && (
              <p>
                类别：{requirement.category}
                {requirement.level ? ` · 层级：${requirement.level}` : ''}
              </p>
            )}
            {!!Object.keys(requirement.fields || {}).length && (
              <p>
                字段要求：
                {Object.entries(requirement.fields)
                  .map(([name, type]) => `${name}：${type}`)
                  .join('、')}
              </p>
            )}
            {items.map((item) => (
              <div
                key={item.key}
                className="rounded bg-zinc-50 p-2 dark:bg-zinc-900"
              >
                <strong>{item.entry || requirement.name}</strong>
                <p>
                  类别：{item.category || '未填写'} · 几何：
                  {item.geometry || '不限'} · {item.required ? '必需' : '可选'}
                </p>
                <p>
                  字段：
                  {Object.entries(item.fields || {})
                    .map(([name, type]) => `${name}：${type}`)
                    .join('、') || '未指定'}
                </p>
                {!item.category && (
                  <p>
                    该行只能由用户提供；填写类别后可自动获取。
                    <a href="#data-list-guide" className="text-blue-600">
                      查看清单编辑说明
                    </a>
                  </p>
                )}
                <p>
                  候选来源：
                  {[
                    ...(item.chosen ? [item.chosen] : []),
                    ...(item.alternatives || []),
                  ]
                    .map(
                      (source) =>
                        `${source.name}（${source.tier} 级，${source.license}）`,
                    )
                    .join('、') || '暂无可用来源'}
                </p>
                {item.excluded?.map((source) => (
                  <p key={source.source_id} className="text-zinc-500">
                    {source.name}：{source.reasons.join('；')}
                  </p>
                ))}
              </div>
            ))}
          {items.some(item=>item.acquirable)&&<AcquisitionPlanPanel items={items.filter(item=>item.acquirable)} />}
          </section>
        )
      })}
      <details id="data-list-guide" className="rounded border p-3">
        <summary className="cursor-pointer font-medium">清单编辑说明</summary>
        <p className="mt-2">
          在“资源”页找到本流程的数据清单，编辑清单文件后上传替换。保留名称、路径和原有字段；为需要自动获取的行增加
          类别（category）列，按需要填写字段（fields）列，例如 NAME=name;CODE=code；自动获取（acquire）列填“否”时仍由用户提供。没有类别的行继续使用用户提供的文件。数据类别及来源可在设置的“数据获取”页查看。
        </p>
        <p className="mt-2">
          保存流程后刷新数据需求。修改内置流程前请先“复制为我的流程”。
        </p>
      </details>
    </div>
  )
}
