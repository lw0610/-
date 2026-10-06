import type { QGISInstance } from '@/types/instance'

function getCreatedAtSortValue(instance: Pick<QGISInstance, 'created_at'>) {
  const timestamp = new Date(instance.created_at || '').getTime()
  if (Number.isNaN(timestamp)) {
    return Number.POSITIVE_INFINITY
  }
  return timestamp
}

export function sortInstancesByCreatedAt(list: QGISInstance[]) {
  return [...list].sort((left, right) => {
    const leftCreatedAt = getCreatedAtSortValue(left)
    const rightCreatedAt = getCreatedAtSortValue(right)

    if (leftCreatedAt !== rightCreatedAt) {
      return leftCreatedAt - rightCreatedAt
    }

    return left.id.localeCompare(right.id)
  })
}
