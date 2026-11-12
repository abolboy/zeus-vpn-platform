const configuredBase = (import.meta.env.VITE_ZEUS_API_BASE_URL || '').trim().replace(/\/+$/, '')

export async function getControlPlaneHealth() {
  if (!configuredBase) return { state: 'unconfigured', message: 'VITE_ZEUS_API_BASE_URL تنظیم نشده است' }
  const controller = new AbortController()
  const timeout = window.setTimeout(() => controller.abort(), 5000)
  try {
    const response = await fetch(configuredBase + '/healthz', {
      method: 'GET',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      signal: controller.signal,
    })
    if (!response.ok) return { state: 'offline', message: 'پاسخ HTTP ' + response.status }
    const body = await response.json()
    if (body?.status !== 'ok') return { state: 'offline', message: 'پاسخ سلامت API معتبر نیست' }
    return { state: 'online', message: 'سرویس Control Plane پاسخ سلامت معتبر داد' }
  } catch (error) {
    const message = error?.name === 'AbortError' ? 'مهلت پاسخ API تمام شد' : 'اتصال API برقرار نشد؛ آدرس، شبکه و CORS را بررسی کنید'
    return { state: 'offline', message }
  } finally {
    window.clearTimeout(timeout)
  }
}
