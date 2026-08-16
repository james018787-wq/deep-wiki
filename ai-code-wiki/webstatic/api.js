// 通用 API 封装：自动携带登录 Bearer token（用户）与 X-Api-Secret（可选），
// 解析后端统一返回结构 {code,msg,data}；401（登录失效）自动跳转登录页。
async function apiRequest(path, options) {
  const cfg = window.APP_CONFIG || { apiBase: '/api/v1', apiSecret: '' };
  options = options || {};
  const headers = Object.assign({ 'Content-Type': 'application/json' }, options.headers || {});
  const token = (typeof authToken === 'function') ? authToken() : '';
  if (token) {
    headers['Authorization'] = 'Bearer ' + token;
  }
  if (cfg.apiSecret) {
    headers['X-Api-Secret'] = cfg.apiSecret;
  }
  const resp = await fetch(cfg.apiBase + path, Object.assign({}, options, { headers }));
  let body = {};
  try {
    body = await resp.json();
  } catch (e) {
    /* 非 JSON 响应，交由下方统一报错 */
  }
  if (resp.status === 401 && typeof clearAuth === 'function' && !location.pathname.endsWith('login.html')) {
    clearAuth();
    location.replace('login.html');
    throw new Error('登录已失效，请重新登录');
  }
  if (!resp.ok || body.code !== 0) {
    throw new Error((body && body.msg) || ('HTTP ' + resp.status));
  }
  return body.data;
}