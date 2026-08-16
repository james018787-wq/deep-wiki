<template>
  <div class="login-wrap">
    <div class="login-card">
      <div class="brand-line">
        <div class="logo">AI·CODE WIKI</div>
        <div class="tagline">代码知识库 · 智能问答 · 迭代影响分析</div>
      </div>
      <h1>系统登录</h1>
      <div class="sub">AUTHORIZED <span>ACCESS</span> ONLY</div>
      <form @submit.prevent="doLogin">
        <div class="field">
          <label>用户名 USERNAME</label>
          <input name="username" autocomplete="username" v-model="form.username" placeholder="admin">
        </div>
        <div class="field">
          <label>密码 PASSWORD</label>
          <input type="password" name="password" autocomplete="current-password" v-model="form.password" placeholder="••••••••">
        </div>
        <button type="submit" class="login-btn" :disabled="logging">
          {{ logging ? '验证中...' : '登 录' }}
        </button>
      </form>
      <div class="msg err" v-if="error">{{ error }}</div>
      <div class="login-tip">默认管理员 admin / admin123（生产请修改）</div>
    </div>
  </div>
</template>

<script setup>
import { reactive, ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { login } from '../store/auth'

const router = useRouter()
const route = useRoute()
const form = reactive({ username: '', password: '' })
const logging = ref(false)
const error = ref('')

async function doLogin() {
  // 兜底：浏览器自动填充可能不触发 v-model，直接读 DOM 值
  let username = form.username
  let password = form.password
  if (!username) {
    const el = document.querySelector('input[name="username"]')
    if (el) username = el.value || ''
  }
  if (!password) {
    const el = document.querySelector('input[name="password"]')
    if (el) password = el.value || ''
  }
  username = (username || '').trim()
  password = (password || '').trim()
  if (!username || !password) { error.value = '请输入用户名和密码'; return }
  logging.value = true
  error.value = ''
  try {
    await login(username, password)
    const redirect = route.query.redirect || '/docs'
    router.replace(redirect)
  } catch (e) {
    error.value = '登录失败: ' + e.message
  } finally {
    logging.value = false
  }
}
</script>

<style scoped>
.login-wrap { position: relative; z-index: 1; width: 380px; }
.login-card {
  background: rgba(18, 26, 44, 0.8);
  border: 1px solid rgba(56, 189, 248, 0.25);
  border-radius: 16px;
  padding: 36px 34px;
  backdrop-filter: blur(14px);
  box-shadow: 0 24px 70px rgba(2, 8, 20, 0.7), 0 0 40px rgba(34, 211, 238, 0.12);
}
.login-card h1 {
  margin: 0 0 6px; font-size: 20px; letter-spacing: 2px; color: var(--text-bright);
}
.login-card .sub { color: var(--text-dim); font-size: 13px; margin-bottom: 26px; }
.login-card .sub span { color: var(--cyan); }
.field { margin-bottom: 16px; }
.field label { display: block; color: var(--text-dim); font-size: 13px; margin-bottom: 6px; letter-spacing: 1px; }
.field input { width: 100%; padding: 11px 14px; font-size: 14px; }
.login-btn {
  width: 100%; margin-top: 8px; padding: 12px; font-size: 15px; letter-spacing: 4px;
}
.login-tip { margin-top: 16px; text-align: center; color: #51698c; font-size: 12px; }
.brand-line { text-align: center; margin-bottom: 22px; }
.brand-line .logo {
  font-family: var(--mono); font-weight: 800; font-size: 26px; letter-spacing: 4px;
  background: linear-gradient(90deg, var(--cyan), var(--violet));
  -webkit-background-clip: text; background-clip: text; color: transparent;
  text-shadow: 0 0 30px rgba(34, 211, 238, 0.4);
}
.brand-line .tagline { color: #51698c; font-size: 12px; letter-spacing: 2px; margin-top: 6px; }
.msg { margin-top: 12px; }
</style>