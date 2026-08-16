<template>
  <div class="cosmic-bg" aria-hidden="true">
    <div class="nebula n1"></div>
    <div class="nebula n2"></div>
    <div class="nebula n3"></div>
    <div class="planet"></div>
    <div class="stars s-sm"></div>
    <div class="stars s-md"></div>
    <div class="stars s-lg"></div>
    <div class="shoot sh1"></div>
    <div class="shoot sh2"></div>
    <div class="shoot sh3"></div>
    <div class="vignette"></div>
  </div>
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
/* ============ 宇宙空间背景 ============ */
.cosmic-bg {
  position: fixed; inset: 0; z-index: 0; overflow: hidden;
  background:
    radial-gradient(1200px 800px at 50% -12%, #1b2a6b 0%, transparent 60%),
    radial-gradient(1000px 700px at 88% 112%, #14204a 0%, transparent 55%),
    radial-gradient(700px 500px at 0% 100%, #16204a 0%, transparent 50%),
    linear-gradient(180deg, #04070f 0%, #080d24 45%, #01040a 100%);
}

/* 星云（大范围模糊色团，缓慢漂移） */
.nebula { position: absolute; border-radius: 50%; filter: blur(72px); opacity: .55; mix-blend-mode: screen; }
.nebula.n1 { width: 540px; height: 440px; left: -8%; top: -8%; background: radial-gradient(circle, rgba(139,92,246,.6), transparent 70%); animation: drift1 26s ease-in-out infinite alternate; }
.nebula.n2 { width: 620px; height: 480px; right: -12%; bottom: -10%; background: radial-gradient(circle, rgba(34,211,238,.5), transparent 70%); animation: drift2 34s ease-in-out infinite alternate; }
.nebula.n3 { width: 400px; height: 320px; left: 52%; top: 58%; background: radial-gradient(circle, rgba(236,72,153,.38), transparent 70%); animation: drift3 40s ease-in-out infinite alternate; }

@keyframes drift1 { from { transform: translate(0,0) scale(1); } to { transform: translate(60px, 40px) scale(1.12); } }
@keyframes drift2 { from { transform: translate(0,0) scale(1); } to { transform: translate(-70px, -50px) scale(1.15); } }
@keyframes drift3 { from { transform: translate(0,0) scale(1); } to { transform: translate(40px, -60px) scale(1.1); } }

/* 发光星球 */
.planet {
  position: absolute; right: 7%; bottom: 10%; width: 230px; height: 230px; border-radius: 50%;
  background: radial-gradient(circle at 34% 32%, #ffe0b3, #f5a97f 42%, #7c5cff 88%, #5a3fd6 100%);
  box-shadow: 0 0 90px 26px rgba(139, 92, 246, 0.42), inset -36px -40px 90px rgba(0, 0, 0, 0.55);
  animation: floaty 13s ease-in-out infinite alternate;
}
@keyframes floaty { from { transform: translateY(0); } to { transform: translateY(-26px); } }

/* 星星（三层不同大小/密度，repeating 瓦片铺满） */
.stars { position: absolute; inset: -60px; pointer-events: none; }
.stars.s-sm {
  background-image:
    radial-gradient(1px 1px at 22px 30px, rgba(255,255,255,.85), transparent 55%),
    radial-gradient(1px 1px at 96px 128px, rgba(255,255,255,.6), transparent 55%),
    radial-gradient(1px 1px at 150px 60px, rgba(186,230,253,.8), transparent 55%),
    radial-gradient(1px 1px at 60px 190px, rgba(255,255,255,.5), transparent 55%),
    radial-gradient(1px 1px at 190px 220px, rgba(186,230,253,.7), transparent 55%),
    radial-gradient(1px 1px at 210px 100px, rgba(255,255,255,.65), transparent 55%);
  background-size: 240px 240px;
  animation: twinkle 5s ease-in-out infinite;
}
.stars.s-md {
  background-image:
    radial-gradient(1.4px 1.4px at 40px 45px, rgba(255,255,255,.95), transparent 55%),
    radial-gradient(1.4px 1.4px at 130px 160px, rgba(199,210,254,.9), transparent 55%),
    radial-gradient(1.4px 1.4px at 205px 40px, rgba(255,255,255,.85), transparent 55%),
    radial-gradient(1.4px 1.4px at 80px 230px, rgba(199,210,254,.8), transparent 55%),
    radial-gradient(1.4px 1.4px at 230px 180px, rgba(255,255,255,.9), transparent 55%);
  background-size: 280px 280px;
  animation: twinkle 7s ease-in-out -2s infinite;
}
.stars.s-lg {
  background-image:
    radial-gradient(2px 2px at 70px 90px, #fff, transparent 60%),
    radial-gradient(2px 2px at 190px 210px, #fff, transparent 60%),
    radial-gradient(2px 2px at 250px 70px, rgba(186,230,253,1), transparent 60%),
    radial-gradient(2px 2px at 30px 260px, #fff, transparent 60%);
  background-size: 320px 320px;
  animation: twinkle 9s ease-in-out -4s infinite;
}
@keyframes twinkle { 0%, 100% { opacity: .55; } 50% { opacity: 1; } }

/* 流星 */
.shoot {
  position: absolute; width: 2px; height: 2px; border-radius: 50%;
  background: #fff; filter: drop-shadow(0 0 6px rgba(255,255,255,.9));
}
.shoot::after {
  content: ""; position: absolute; top: 0; left: 0;
  width: 120px; height: 1px;
  background: linear-gradient(270deg, rgba(255,255,255,.85), transparent);
  transform-origin: left center;
}
.shoot.sh1 { top: 14%; left: 68%; animation: shoot 7s linear infinite; }
.shoot.sh2 { top: 38%; left: 18%; animation: shoot 11s linear infinite 3.2s; }
.shoot.sh3 { top: 8%; left: 40%; animation: shoot 13s linear infinite 6.5s; }
@keyframes shoot {
  0% { transform: translate(0, 0); opacity: 0; }
  4% { opacity: 1; }
  12% { transform: translate(-240px, 150px); opacity: 0; }
  100% { transform: translate(-240px, 150px); opacity: 0; }
}

/* 暗角：保证登录卡片可读 */
.vignette {
  position: absolute; inset: 0;
  background: radial-gradient(1100px 700px at 50% 45%, transparent 55%, rgba(1, 3, 10, 0.55) 100%);
}

/* ============ 登录卡片 ============ */
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