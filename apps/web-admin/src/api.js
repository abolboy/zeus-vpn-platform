const configuredBase = (import.meta.env.VITE_ZEUS_API_BASE_URL || '').trim().replace(/\/+$/, '')

export const isControlPlaneConfigured = Boolean(configuredBase)

export function createIdempotencyKey() {
  const bytes = new Uint8Array(16)
  if (!globalThis.crypto?.getRandomValues) {
    throw new Error('مرورگر امکان تولید کلید امن درخواست را ندارد')
  }
  globalThis.crypto.getRandomValues(bytes)
  return Array.from(bytes, value => value.toString(16).padStart(2, '0')).join('')
}

async function requestJSON(path, { method = 'GET', token, body, timeoutMs = 8000, idempotencyKey } = {}) {
  if (!configuredBase) throw new Error('نشانی Control Plane تنظیم نشده است')
  const controller = new AbortController()
  const timeout = window.setTimeout(() => controller.abort(), timeoutMs)
  const headers = { Accept: 'application/json' }
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if (token) headers.Authorization = 'Bearer ' + token
  if (idempotencyKey) headers['Idempotency-Key'] = idempotencyKey
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
    if (error instanceof TypeError) throw new Error('ارتباط با Control Plane برقرار نشد؛ شبکه، نشانی API و CORS را بررسی کنید')
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

export function createVPNUser(token, input, idempotencyKey = createIdempotencyKey()) {
  return requestJSON('/v1/users', {
    method: 'POST',
    token,
    body: input,
    idempotencyKey,
  })
}

export function updateVPNUser(token, userID, input, idempotencyKey = createIdempotencyKey()) {
  return requestJSON('/v1/users/' + encodeURIComponent(userID), {
    method: 'PATCH',
    token,
    body: input,
    idempotencyKey,
  })
}

export function listVPNDevices(token, userID) {
  return requestJSON('/v1/users/' + encodeURIComponent(userID) + '/devices', { token })
}

export function revokeVPNDevice(token, userID, deviceID, idempotencyKey = createIdempotencyKey()) {
  return requestJSON(
    '/v1/users/' + encodeURIComponent(userID) + '/devices/' + encodeURIComponent(deviceID) + '/revoke',
    { method: 'POST', token, idempotencyKey },
  )
}

export function createDeviceEnrollmentCode(token, userID) {
  return requestJSON(
    '/v1/users/' + encodeURIComponent(userID) + '/enrollment-code',
    { method: 'POST', token },
  )
}
