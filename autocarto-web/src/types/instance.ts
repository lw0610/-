export interface QGISInstance {
  id: string
  machine_fingerprint?: string
  start_time?: string
  pid: number
  qgis_version: string
  project_path: string
  status: 'running' | 'stopped' | 'dead'
  status_reason?: string
  status_detail?: string
  services: Record<string, ServiceInfo>
  created_at: string
  updated_at: string
  last_heartbeat: string
  source: 'register' | 'scan' | 'reverse_ws'
}

export interface ServiceInfo {
  port: number
  status: string
  last_heartbeat: string
}

export interface InstanceEvent {
  type: 'added' | 'removed' | 'changed'
  instance: QGISInstance
}
