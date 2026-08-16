const BASE = '/api/v1'
const TOKEN_KEY = 'ai-code-wiki-token'

export function getToken() {
  return localStorage.getItem(TOKEN_KEY) || ''
}

export function clearAuth() {
  localStorage.removeItem('ai-code-wiki-token')
  localStorage.removeItem('ai-code-wiki-user')
}

export async function apiRequest(path, options = {}) {
  const headers = { 'Content-Type': 'application/json', ...(options.headers || {}) }
  const t = getToken()
  if (t) headers['Authorization'] = 'Bearer ' + t
  let resp
  try {
    resp = await fetch(BASE + path, { ...options, headers })
  } catch (e) {
    throw new Error('网络请求失败: ' + e.message)
  }
  let body = {}
  try { body = await resp.json() } catch (e) { /* ignore */ }
  if (resp.status === 401) {
    if (location.pathname !== '/login') {
      clearAuth()
      location.href = '/login'
      throw new Error('登录已失效，请重新登录')
    }
    throw new Error((body && body.msg) || '未登录或登录已失效')
  }
  if (!resp.ok || (body && typeof body.code === 'number' && body.code !== 0)) {
    throw new Error((body && body.msg) || ('请求失败: HTTP ' + resp.status))
  }
  return body.data
}