export function formatClientIssue(error: unknown, fallback: string): string {
  if (error instanceof Error) {
    const message = error.message.trim()
    if (message) {
      return message
    }
  }

  if (typeof error === 'string') {
    const message = error.trim()
    if (message) {
      return message
    }
  }

  return fallback
}

export function reportClientIssue(
  context: string,
  error: unknown,
  fallback: string,
): string {
  const detail = formatClientIssue(error, fallback)
  console.warn(`${context}: ${detail}`, error)
  return detail
}
