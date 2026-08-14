// ai-code-wiki 极简前端全局配置
// apiBase：API 请求根路径（与后端同源，无需跨域）
// apiSecret：对应后端环境变量 API_SECRET_KEY；后端未配置密钥时留空即可，
//            配置后所有请求自动携带 X-Api-Secret 请求头。
window.APP_CONFIG = {
  apiBase: '/api/v1',
  apiSecret: ''
};
