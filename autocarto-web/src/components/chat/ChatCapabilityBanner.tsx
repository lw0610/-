'use client'

import AlertPanel from '@/components/settings/AlertPanel'

type ChatCapabilityBannerProps = {
  activeInstanceId: string | null
  connected: boolean
  hasProjectPath: boolean
  projectConnected: boolean
  runningCount: number
  stale: boolean
}

function buildCapabilityBanner(props: ChatCapabilityBannerProps) {
  const { activeInstanceId, connected, hasProjectPath, projectConnected, runningCount, stale } = props

  if (!connected) {
    return {
      testId: 'backend-status-banner',
      tone: 'error' as const,
      title: '后端连接中断',
      message: '历史会话与运行状态暂时保持只读，新的消息发送与项目操作已暂停。',
    }
  }

  if (projectConnected) {
    return null
  }

  if (stale) {
    return {
      testId: 'project-capability-banner',
      tone: 'warning' as const,
      title: '运行时同步中',
      message: '后端已连接，正在等待 QGIS 运行态恢复同步。历史会话、任务和工作流仍可查看，新的地图执行请求暂时不可发送。',
    }
  }

  if (runningCount <= 0) {
    return {
      testId: 'project-capability-banner',
      tone: 'warning' as const,
      title: 'GIS 运行态不可用',
      message: '后端在线，但当前没有可用的运行中 GIS 实例。可继续查看历史会话，新的地图执行请求已暂停。',
    }
  }

  if (!activeInstanceId) {
    return {
      testId: 'project-capability-banner',
      tone: 'warning' as const,
      title: '未选择 QGIS 实例',
      message: '后端在线，但尚未选定可用实例。可继续查看历史会话，请先选择实例并连接项目后再发送新消息。',
    }
  }

  if (!hasProjectPath) {
    return {
      testId: 'project-capability-banner',
      tone: 'warning' as const,
      title: 'QGIS 项目能力暂不可用',
      message: '当前实例尚未打开项目。可继续查看历史会话，请先打开或重新连接 QGIS 项目后再发送新消息。',
    }
  }

  return {
    testId: 'project-capability-banner',
    tone: 'warning' as const,
    title: 'QGIS 项目能力暂不可用',
    message: '后端在线，但项目连接尚未恢复。历史会话、任务和工作流仍可查看，新的地图执行请求暂时不可发送。',
  }
}

export default function ChatCapabilityBanner(props: ChatCapabilityBannerProps) {
  const banner = buildCapabilityBanner(props)
  if (!banner) {
    return null
  }

  return (
    <div className="px-3 pt-3">
      <div data-testid={banner.testId}>
        <AlertPanel compact tone={banner.tone} title={banner.title} message={banner.message} />
      </div>
    </div>
  )
}
