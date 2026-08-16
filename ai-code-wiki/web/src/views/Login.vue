<template>
  <div class="cosmic-bg" aria-hidden="true">
    <div class="galaxy"></div>
    <div class="nebula n1"></div>
    <div class="nebula n2"></div>
    <div class="nebula n3"></div>
    <div class="planet p1"></div>
    <div class="planet p2"></div>
    <div class="planet p3"></div>
    <div class="planet p4"></div>
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
        <div class="brand-row">
          <img class="logo-mark" src="/logo.svg" alt="AI·CODE WIKI">
          <div class="logo">AI·CODE WIKI</div>
        </div>
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
    radial-gradient(1400px 900px at 16% -10%, rgba(34, 211, 238, 0.16), transparent 60%),
    radial-gradient(1200px 820px at 84% 110%, rgba(139, 92, 246, 0.2), transparent 60%),
    radial-gradient(1000px 700px at 50% 55%, rgba(10, 16, 36, 0.85), transparent 70%),
    linear-gradient(180deg, #030512 0%, #070c22 45%, #00020a 100%);
}

/* 星系核心（深邃感：上方一处明亮光核 + 柔和光晕 + 缓慢脉动） */
.galaxy {
  position: absolute; left: 50%; top: 4%; width: 680px; height: 520px;
  transform: translateX(-50%);
  background:
    radial-gradient(circle at 50% 50%, rgba(255,255,255,.95) 0%, rgba(199,210,254,.55) 7%, transparent 20%),
    radial-gradient(circle at 50% 50%, rgba(139,92,246,.5) 0%, transparent 58%),
    radial-gradient(circle at 50% 50%, rgba(34,211,238,.38) 0%, transparent 72%);
  filter: blur(16px);
  animation: galaxyPulse 9s ease-in-out infinite alternate;
}
@keyframes galaxyPulse {
  from { opacity: .72; transform: translateX(-50%) scale(1); }
  to   { opacity: 1;   transform: translateX(-50%) scale(1.07); }
}

/* 星云（大范围模糊色团，缓慢漂移） */
.nebula { position: absolute; border-radius: 50%; filter: blur(72px); opacity: .55; mix-blend-mode: screen; }
.nebula.n1 { width: 540px; height: 440px; left: -8%; top: -8%; background: radial-gradient(circle, rgba(139,92,246,.6), transparent 70%); animation: drift1 26s ease-in-out infinite alternate; }
.nebula.n2 { width: 620px; height: 480px; right: -12%; bottom: -10%; background: radial-gradient(circle, rgba(34,211,238,.5), transparent 70%); animation: drift2 34s ease-in-out infinite alternate; }
.nebula.n3 { width: 400px; height: 320px; left: 52%; top: 58%; background: radial-gradient(circle, rgba(236,72,153,.38), transparent 70%); animation: drift3 40s ease-in-out infinite alternate; }

@keyframes drift1 { from { transform: translate(0,0) scale(1); } to { transform: translate(60px, 40px) scale(1.12); } }
@keyframes drift2 { from { transform: translate(0,0) scale(1); } to { transform: translate(-70px, -50px) scale(1.15); } }
@keyframes drift3 { from { transform: translate(0,0) scale(1); } to { transform: translate(40px, -60px) scale(1.1); } }

/* 星球（多个不同大小/位置/远近，营造纵深） */
.planet {
  position: absolute; border-radius: 50%;
  background: radial-gradient(circle at 34% 32%, #ffe0b3, #f5a97f 42%, #7c5cff 88%, #5a3fd6 100%);
}
/* 右下主星球：放远变小、光晕减淡 */
.planet.p1 {
  right: 6%; bottom: 8%; width: 150px; height: 150px;
  box-shadow: 0 0 60px 16px rgba(139, 92, 246, 0.28), inset -26px -30px 70px rgba(0, 0, 0, 0.55);
  animation: floaty 15s ease-in-out infinite alternate;
}
/* 左上：中号，青色偏冷 */
.planet.p2 {
  left: 10%; top: 14%; width: 110px; height: 110px;
  background: radial-gradient(circle at 35% 33%, #c7f0ff, #7fd4ff 45%, #4a6cf7 90%);
  box-shadow: 0 0 46px 12px rgba(56, 189, 248, 0.26), inset -20px -22px 55px rgba(0, 0, 0, 0.5);
  animation: floaty 12s ease-in-out -3s infinite alternate;
}
/* 右上：小号，暖橙 */
.planet.p3 {
  right: 22%; top: 20%; width: 68px; height: 68px;
  background: radial-gradient(circle at 36% 34%, #ffe6c2, #ffb86b 50%, #e06b3a 92%);
  box-shadow: 0 0 30px 8px rgba(255, 184, 107, 0.25), inset -12px -14px 34px rgba(0, 0, 0, 0.5);
  animation: floaty 10s ease-in-out -6s infinite alternate;
}
/* 左下：迷你，暗紫 */
.planet.p4 {
  left: 26%; bottom: 16%; width: 44px; height: 44px;
  background: radial-gradient(circle at 36% 34%, #e4d7ff, #a78bfa 55%, #6d4fd6 95%);
  box-shadow: 0 0 20px 5px rgba(167, 139, 250, 0.22), inset -8px -9px 22px rgba(0, 0, 0, 0.5);
  animation: floaty 9s ease-in-out -1s infinite alternate;
}
@keyframes floaty { from { transform: translateY(0); } to { transform: translateY(-24px); } }

/* 星星（三层不同大小/密度，远近分层 + 视差漂移：远层慢、近层快） */
.stars { position: absolute; inset: -60px; pointer-events: none; }
.stars.s-sm {
  background-image:
    radial-gradient(2px 2px at 22px 30px, rgba(255,255,255,.9), transparent 55%),
    radial-gradient(2px 2px at 96px 128px, rgba(255,255,255,.7), transparent 55%),
    radial-gradient(2px 2px at 150px 60px, rgba(186,230,253,.85), transparent 55%),
    radial-gradient(2px 2px at 60px 190px, rgba(255,255,255,.65), transparent 55%),
    radial-gradient(2px 2px at 190px 220px, rgba(186,230,253,.8), transparent 55%),
    radial-gradient(2px 2px at 210px 100px, rgba(255,255,255,.75), transparent 55%);
  background-size: 240px 240px;
  animation: twinkle 5s ease-in-out infinite, driftStar-sm 110s linear infinite;
}
.stars.s-md {
  background-image:
    radial-gradient(3px 3px at 40px 45px, #fff, transparent 58%),
    radial-gradient(3px 3px at 130px 160px, rgba(199,210,254,1), transparent 58%),
    radial-gradient(3px 3px at 205px 40px, rgba(255,255,255,.95), transparent 58%),
    radial-gradient(3px 3px at 80px 230px, rgba(199,210,254,.9), transparent 58%),
    radial-gradient(3px 3px at 230px 180px, #fff, transparent 58%);
  background-size: 280px 280px;
  animation: twinkle 7s ease-in-out -2s infinite, driftStar-md 75s linear infinite;
}
.stars.s-lg {
  background-image:
    radial-gradient(4px 4px at 70px 90px, #fff, transparent 62%),
    radial-gradient(4px 4px at 190px 210px, #fff, transparent 62%),
    radial-gradient(4px 4px at 250px 70px, rgba(186,230,253,1), transparent 62%),
    radial-gradient(4px 4px at 30px 260px, #fff, transparent 62%);
  background-size: 320px 320px;
  filter: drop-shadow(0 0 3px rgba(255,255,255,.85));
  animation: twinkle 9s ease-in-out -4s infinite, driftStar-lg 48s linear infinite;
}
@keyframes twinkle { 0%, 100% { opacity: .7; } 50% { opacity: 1; } }
/* 视差漂移：背景位移动一个瓦片尺寸即无缝循环；远层慢、近层快 */
@keyframes driftStar-sm { from { background-position: 0 0; } to { background-position: 240px 160px; } }
@keyframes driftStar-md { from { background-position: 0 0; } to { background-position: 280px 190px; } }
@keyframes driftStar-lg { from { background-position: 0 0; } to { background-position: 320px 220px; } }

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

/* 暗角 + 深空纵深：中央明亮、四周压入黑暗 */
.vignette {
  position: absolute; inset: 0;
  background:
    radial-gradient(1200px 800px at 50% 42%, transparent 42%, rgba(1, 3, 10, 0.78) 100%),
    linear-gradient(180deg, rgba(1, 3, 10, 0.4) 0%, transparent 22%, transparent 78%, rgba(1, 3, 10, 0.55) 100%);
}

/* ============ 登录卡片 ============ */
.login-wrap { position: relative; z-index: 1; width: 380px; }
/* 卡片四周光晕（扩散 + 呼吸脉动） */
.login-wrap::before {
  content: ""; position: absolute; inset: -34px; z-index: -1; border-radius: 30px;
  background: radial-gradient(circle at 50% 50%,
    rgba(34, 211, 238, 0.26) 0%,
    rgba(139, 92, 246, 0.16) 45%,
    transparent 72%);
  filter: blur(26px);
  animation: cardGlow 5.5s ease-in-out infinite alternate;
}
@keyframes cardGlow {
  from { opacity: .72; transform: scale(.98); }
  to   { opacity: 1.1; transform: scale(1.03); }
}
.login-card {
  background: rgba(18, 26, 44, 0.8);
  border: 1px solid rgba(56, 189, 248, 0.35);
  border-radius: 16px;
  padding: 36px 34px;
  backdrop-filter: blur(14px);
  box-shadow:
    0 24px 70px rgba(2, 8, 20, 0.7),
    0 0 0 1px rgba(34, 211, 238, 0.14),
    0 0 42px rgba(34, 211, 238, 0.34),
    0 0 96px rgba(139, 92, 246, 0.22);
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
.brand-row { display: flex; align-items: center; justify-content: center; gap: 12px; }
.brand-row .logo-mark {
  display: inline-block;
  width: 42px; height: 42px;
  filter: drop-shadow(0 0 5px rgba(34, 211, 238, 0.95))
          drop-shadow(0 0 16px rgba(34, 211, 238, 0.6))
          drop-shadow(0 0 30px rgba(139, 92, 246, 0.45));
}
.brand-line .logo {
  font-family: var(--mono); font-weight: 800; font-size: 26px; letter-spacing: 4px;
  background: linear-gradient(90deg, var(--cyan), var(--violet));
  -webkit-background-clip: text; background-clip: text; color: transparent;
  text-shadow: 0 0 30px rgba(34, 211, 238, 0.4);
}
.brand-line .tagline { color: #51698c; font-size: 12px; letter-spacing: 2px; margin-top: 6px; }
.msg { margin-top: 12px; }
</style>