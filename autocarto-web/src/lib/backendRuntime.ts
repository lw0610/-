import { buildControlContractHeaders } from "@/lib/api";

export function normalizeBackendPort(raw: unknown) {
  const parsed = Number(raw)
  if (!Number.isInteger(parsed) || parsed <= 0 || parsed > 65535) {
    return null
  }
  return parsed
}

export function buildBackendRuntime(port: unknown) {
  const safePort = normalizeBackendPort(port)
  if (!safePort) {
    return {
      port: 0,
      backendUrl: "",
      wsUrl: "",
    }
  }
  return {
    port: safePort,
    backendUrl: `http://127.0.0.1:${safePort}`,
    wsUrl: `ws://127.0.0.1:${safePort}/ws`,
  }
}

export interface ServicePortInfo {
  host: string
  port: number
  protocol: string
  pid: number
  status: string
}

export type ServicePortsMap = Record<string, ServicePortInfo>

export async function fetchServicePorts(backendUrl: string): Promise<ServicePortsMap> {
  const res = await fetch(`${backendUrl}/api/services/ports`, {
    headers: buildControlContractHeaders(),
    signal: AbortSignal.timeout(3000),
  })
  if (!res.ok) {
    throw new Error(`获取服务端口失败: ${res.status}`)
  }
  const json = await res.json()
  return (json.data ?? {}) as ServicePortsMap
}
