<template>
  <div class="container">
    <div class="toolbar" v-if="src">
      <span class="meta"><b>{{ src.repo_name }}</b> · {{ src.module_name }}.{{ src.func_name }} · {{ src.file_path }}<span class="lang-tag" v-if="lang">{{ lang }}</span></span>
      <span class="spacer"></span>
      <button @click="close">关闭窗口</button>
      <button @click="load">刷新</button>
    </div>

    <div class="code-panel">
      <div class="msg err" v-if="error">{{ error }}</div>
      <div class="msg info" v-if="loading">加载源码中...</div>
      <pre class="line-numbers code-block" v-if="src && src.content"><code ref="code" :class="'language-' + (lang || 'none')">{{ src.content }}</code></pre>
      <div class="empty" v-if="src && !src.content">（文件为空）</div>
    </div>
  </div>
</template>

<script setup>
import { computed, nextTick, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { apiRequest } from '../api'
import Prism from 'prismjs'
import 'prismjs/components/prism-go'
import 'prismjs/components/prism-java'
import 'prismjs/components/prism-python'
import 'prismjs/components/prism-javascript'
import 'prismjs/components/prism-typescript'
import 'prismjs/components/prism-sql'
import 'prismjs/components/prism-json'
import 'prismjs/components/prism-yaml'
import 'prismjs/components/prism-bash'
import 'prismjs/components/prism-markup'
import 'prismjs/components/prism-markdown'
import 'prismjs/components/prism-css'
import 'prismjs/components/prism-c'
import 'prismjs/components/prism-cpp'
import 'prismjs/components/prism-rust'
import 'prismjs/components/prism-ruby'
import 'prismjs/components/prism-markup-templating'
import 'prismjs/components/prism-php'
import 'prismjs/components/prism-kotlin'
import 'prismjs/plugins/line-numbers/prism-line-numbers'
import 'prismjs/themes/prism-tomorrow.css'
import 'prismjs/plugins/line-numbers/prism-line-numbers.css'

const EXT_MAP = {
  go: 'go', java: 'java', py: 'python', js: 'javascript', mjs: 'javascript',
  ts: 'typescript', tsx: 'typescript', sql: 'sql', json: 'json',
  yml: 'yaml', yaml: 'yaml', sh: 'bash', bash: 'bash', html: 'markup',
  vue: 'markup', xml: 'markup', md: 'markdown', markdown: 'markdown',
  css: 'css', scss: 'scss', c: 'c', h: 'c', cpp: 'cpp', cc: 'cpp', cxx: 'cpp',
  rs: 'rust', rb: 'ruby', php: 'php', kt: 'kotlin', kts: 'kotlin'
}

const route = useRoute()
const src = ref(null)
const loading = ref(false)
const error = ref('')
const code = ref(null)

const lang = computed(() => {
  const ext = (src.value?.file_path || '').split('.').pop().toLowerCase()
  return EXT_MAP[ext] || null
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    src.value = await apiRequest('/doc/' + route.params.id + '/source')
    await nextTick()
    if (code.value) Prism.highlightElement(code.value)
    scrollToHashLine()
  } catch (e) { error.value = '加载源码失败: ' + e.message }
  finally { loading.value = false }
}

// 支持 ?/#L{n} 定位到指定行（问答引用跳转：/doc-source/:id#L{行号}）
function scrollToHashLine() {
  const m = (window.location.hash || '').match(/^#L(\d+)$/)
  if (!m) return
  const ln = parseInt(m[1], 10)
  if (!ln || !src.value) return
  nextTick(() => {
    const panel = document.querySelector('.code-block')
    const rows = panel ? panel.querySelectorAll('.line-numbers-rows > span') : []
    if (!panel || !rows.length) return
    const target = rows[ln - 1]
    if (!target) return
    const panelTop = panel.getBoundingClientRect().top
    const targetTop = target.getBoundingClientRect().top
    panel.scrollTop += (targetTop - panelTop) - panel.clientHeight / 2
    target.classList.add('jump-line')
  })
}

function close() {
  window.close()
}

onMounted(load)
</script>

<style scoped>
.toolbar .meta { color: var(--text-dim); font-size: 13px; }
.toolbar .spacer { flex: 1; }
.toolbar .lang-tag {
  margin-left: 8px; padding: 1px 8px; border-radius: 6px;
  background: rgba(34, 211, 238, 0.12); color: var(--cyan);
  font-family: var(--mono); font-size: 11px;
}
.code-panel {
  background: #0a0f1c;
  border: 1px solid var(--line);
  border-radius: 12px;
  overflow: hidden;
}
.code-block {
  margin: 0 !important; padding: 14px 0;
  font-size: 13px; line-height: 1.55;
  max-height: calc(100vh - 120px);
  overflow: auto;
  background: #0a0f1c !important;
  text-shadow: none !important;
}
.line-numbers-rows > span.jump-line {
  background: rgba(250, 204, 21, 0.18);
  outline: 1px solid rgba(250, 204, 21, 0.35);
}
</style>