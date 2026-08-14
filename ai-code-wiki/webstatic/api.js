// 通用 API 封装：统一携带 X-Api-Secret 鉴权头，解析后端统一返回结构 {code,msg,data}
async function apiRequest(path, options) {
  const cfg = window.APP_CONFIG || { apiBase: '/api/v1', apiSecret: '' };
  options = options || {};
  const headers = Object.assign({ 'Content-Type': 'application/json' }, options.headers || {});
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
  if (!resp.ok || body.code !== 0) {
    throw new Error((body && body.msg) || ('HTTP ' + resp.status));
  }
  return body.data;
}
