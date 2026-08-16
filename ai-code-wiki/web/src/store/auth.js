import { apiRequest, getToken, clearAuth } from '../api'

const USER_KEY = 'ai-code-wiki-user'

export function getUser() {
  try {
    return JSON.parse(localStorage.getItem(USER_KEY) || 'null')
  } catch (e) { return null }
}

export function saveAuth(token, user) {
  localStorage.setItem('ai-code-wiki-token', token)
  localStorage.setItem(USER_KEY, JSON.stringify(user || {}))
}

export function isAuthed() {
  return !!getToken()
}

export async function login(username, password) {
  const data = await apiRequest('/auth/login', {
    method: 'POST',
    body: JSON.stringify({ username, password })
  })
  saveAuth(data.token, {
    username: data.username,
    nickname: data.nickname,
    is_admin: data.is_admin
  })
  return data
}

export async function logout() {
  try {
    await apiRequest('/auth/logout', { method: 'POST' })
  } catch (e) { /* ignore */ }
  clearAuth()
}

export { isAuthed as hasToken }