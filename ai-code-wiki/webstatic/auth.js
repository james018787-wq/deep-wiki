// ============ 登录鉴权（前端侧） ============
// 依赖 api.js（apiRequest）。令牌与用户信息存 localStorage。
const AUTH_KEY = 'ai-code-wiki-token';
const AUTH_USER_KEY = 'ai-code-wiki-user';

function authToken() {
  return localStorage.getItem(AUTH_KEY) || '';
}

function authUser() {
  try { return JSON.parse(localStorage.getItem(AUTH_USER_KEY) || '{}'); }
  catch (e) { return {}; }
}

function saveAuth(token, user) {
  localStorage.setItem(AUTH_KEY, token);
  localStorage.setItem(AUTH_USER_KEY, JSON.stringify(user || {}));
}

function clearAuth() {
  localStorage.removeItem(AUTH_KEY);
  localStorage.removeItem(AUTH_USER_KEY);
}

// requireAuth 页面守卫：未登录跳转登录页；已登录则渲染导航用户信息。
function requireAuth() {
  if (!authToken()) {
    location.replace('login.html');
    return false;
  }
  initAuthUI();
  return true;
}

// initAuthUI 将当前用户名渲染到导航 [data-auth-user] 元素。
function initAuthUI() {
  const u = authUser();
  const name = u.nickname || u.username || '';
  document.querySelectorAll('[data-auth-user]').forEach(el => { el.textContent = name; });
}

// logout 登出：调用后端失效令牌后清本地并回登录页。
async function logout() {
  try { await apiRequest('/auth/logout', { method: 'POST' }); } catch (e) { /* 忽略 */ }
  clearAuth();
  location.replace('login.html');
}

// apiRequestWithAuth 带 Bearer token 的请求（供登录页外的页面使用；api.js 已内置该逻辑）。
// 若后端返回 401（登录失效）自动跳登录页。
function apiRequestWithAuth(path, options) {
  return apiRequest(path, options);
}