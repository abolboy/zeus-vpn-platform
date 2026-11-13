const configuredBase = (import.meta.env.VITE_ZEUS_API_BASE_URL || '').trim().replace(/\/+$/, '')

export const isControlPlaneConfigured = Boolean(configuredBase)

async function requestJSON(path, { method = 'GET', token, body, timeoutMs = 8000 } = {}) {
  if (!configuredBase) throw new Error('نشانی Control Plane تنظیم نشده است')
  const controller = new AbortController()
  const timeout = window.setTimeout(() => controller.abort(), timeoutMs)
  const headers = { Accept: 'application/json' }
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if (token) headers.Authorization = 'Bearer ' + token
  try {
    const response = await fetch(configuredBase + path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      cache: 'no-store',
      signal: controller.signal,
    })
    let payload = null
    try { payload = await response.json() } catch { /* Error response may be empty or non-JSON. */ }
    if (!response.ok) {
      const error = new Error(payload?.error?.message || ('پاسخ HTTP ' + response.status))
      error.status = response.status
      error.code = payload?.error?.code || 'http_error'
      throw error
    }
    return payload
  } catch (error) {
    if (error?.name === 'AbortError') throw new Error('مهلت پاسخ Control Plane تمام شد')
    throw error
  } finally {
    window.clearTimeout(timeout)
  }
}

export async function getControlPlaneHealth() {
  if (!configuredBase) return { state: 'unconfigured', message: 'VITE_ZEUS_API_BASE_URL تنظیم نشده است' }
  try {
    const body = await requestJSON('/healthz', { timeoutMs: 5000 })
    if (body?.status !== 'ok') return { state: 'offline', message: 'پاسخ سلامت API معتبر نیست' }
    return { state: 'online', message: 'سرویس Control Plane پاسخ سلامت معتبر داد' }
  } catch (error) {
    return { state: 'offline', message: error.message || 'اتصال API برقرار نشد؛ نشانی، شبکه و CORS را بررسی کنید' }
  }
}

export function loginOperator(login, password) {
  return requestJSON('/v1/auth/login', { method: 'POST', body: { login, password } })
}

export function getCurrentOperator(token) {
  return requestJSON('/v1/auth/me', { token })
}

export function logoutOperator(token) {
  return requestJSON('/v1/auth/logout', { method: 'POST', token })
}

export function listVPNUsers(token, { limit = 50, cursor = '' } = {}) {
  const params = new URLSearchParams({ limit: String(limit) })
  if (cursor) params.set('cursor', cursor)
  return requestJSON('/v1/users?' + params.toString(), { token })
}
