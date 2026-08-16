<template>
  <div class="container">
    <div class="chat-layout">
      <div class="side">
        <h4>会话</h4>
        <button @click="newSession">＋ 新建会话</button>
        <div v-for="s in sessions" :key="s.session_id" class="sess" :class="{active: s.session_id === sessionId}" @click="openSession(s)">
          <div class="t">{{ s.title || '未命名会话' }}</div>
          <div class="m">{{ fmtTime(s.updated_at) }} ｜ {{ s.message_count }} 条</div>
          <button class="del" :disabled="s.deleting" @click.stop="removeSession(s)">删除</button>
        </div>
        <div class="empty" v-if="!sessions.length" style="padding:10px 0;">暂无会话</div>
      </div>

      <div class="main">
        <div class="chat-head">
          <select v-model="form.repo_id">
            <option v-for="r in enabledRepos2" :key="r.id" :value="r.id">{{ r.repo_name }}</option>
          </select>
          <select v-model="form.mode">
            <option value="qa">智能问答（多轮追问）</option>
            <option value="req">需求分析（粘贴产品修订/需求文档）</option>
          </select>
          <span class="hint">模式：{{ form.mode === 'req' ? '需求→开发设计建议' : '基于代码 Wiki 的 RAG 问答' }}</span>
        </div>
        <div class="chat-body" ref="body">
          <div class="empty" v-if="!bubbles.length">
            {{ form.mode === 'req' ? '粘贴产品修订/需求文档，获取相关模块、风险点与开发设计建议。' : '可以直接问我，例如："下单模块的详细逻辑是什么？"，然后继续追问，我会记住对话上下文。' }}
          </div>
          <div v-for="(b, i) in bubbles" :key="i" class="bubble" :class="b.role">
            <div class="who">{{ b.role === 'user' ? '我' : 'AI' }}</div>
            <div class="text">{{ b.content }}</div>
            <div class="refs" v-if="b.refs && b.refs.length">
              <template v-for="(r, j) in b.refs" :key="j">
                <a v-if="r.doc_id"
                   :href="'/doc-source/' + r.doc_id + (r.func_line ? '#L' + r.func_line : '')"
                   :title="(r.module_name ? r.module_name + '.' + r.func_name : r.func_name) + '（' + r.file_path + ':' + (r.func_line || '?') + '）'"
                   target="_blank" @click.stop>{{ r.module_name ? r.module_name + '.' + r.func_name : r.func_name }}</a>
                <span v-else>{{ r.module_name ? r.module_name + '.' + r.func_name : (r.func_name + (r.file_path ? '（' + r.file_path + '）' : '')) }}</span>
              </template>
            </div>
            <div class="meta" v-if="b.meta">{{ b.meta }}</div>
          </div>
          <div class="msg info" v-if="sending" style="font-size:13px;">思考中...</div>
        </div>
        <div class="chat-input">
          <textarea v-model="form.query" rows="2" placeholder="输入问题，Enter 发送 / Shift+Enter 换行" @keydown.enter.exact.prevent="send"></textarea>
          <button :disabled="sending || !form.query.trim()" @click="send">发送</button>
        </div>
        <div class="msg err" v-if="error">{{ error }}</div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import { apiRequest } from '../api'
import { fetchRepos, enabledRepos, defaultRepoId } from '../store/repo'

const SESSION_KEY = 'ai-code-wiki-chat-session'
const repos = ref([])
const sessions = ref([])
const sessionId = ref(localStorage.getItem(SESSION_KEY) || '')
const form = reactive({ repo_id: 0, mode: 'qa', query: '' })
const bubbles = ref([])
const sending = ref(false)
const error = ref('')
const body = ref(null)

const enabledRepos2 = computed(() => enabledRepos(repos.value))

function fmtTime(ts) {
  if (!ts) return '-'
  const d = new Date(ts * 1000)
  const p = n => String(n).padStart(2, '0')
  return d.getFullYear() + '-' + p(d.getMonth() + 1) + '-' + p(d.getDate()) + ' ' + p(d.getHours()) + ':' + p(d.getMinutes())
}

async function scrollBottom() {
  await nextTick()
  if (body.value) body.value.scrollTop = body.value.scrollHeight
}

async function initRepos() {
  try {
    repos.value = await fetchRepos()
    form.repo_id = defaultRepoId(enabledRepos2.value)
  } catch (e) { error.value = '加载仓库失败: ' + e.message }
}

async function loadSessions() {
  if (!form.repo_id) return
  try {
    sessions.value = (await apiRequest('/chat/sessions?repo_id=' + form.repo_id)) || []
  } catch (e) { /* 静默 */ }
}

async function openSession(s) {
  sessionId.value = s.session_id
  localStorage.setItem(SESSION_KEY, sessionId.value)
  error.value = ''
  try {
    const msgs = (await apiRequest('/chat/history?session_id=' + sessionId.value)) || []
    bubbles.value = msgs.map(m => ({ role: m.role, content: m.content }))
    scrollBottom()
  } catch (e) { error.value = '加载历史失败: ' + e.message }
}

function newSession() {
  sessionId.value = ''
  bubbles.value = []
  error.value = ''
  localStorage.removeItem(SESSION_KEY)
}

async function removeSession(s) {
  if (!confirm('确认删除会话「' + (s.title || '未命名会话') + '」？')) return
  s.deleting = true
  error.value = ''
  try {
    await apiRequest('/chat/session?session_id=' + encodeURIComponent(s.session_id), { method: 'DELETE' })
    sessions.value = sessions.value.filter(x => x.session_id !== s.session_id)
    if (sessionId.value === s.session_id) newSession()
  } catch (e) { error.value = '删除会话失败: ' + e.message }
  finally { s.deleting = false }
}

async function send() {
  const query = form.query.trim()
  if (!query || sending.value) return
  if (!form.repo_id) { error.value = '请先选择仓库'; return }
  bubbles.value.push({ role: 'user', content: query })
  form.query = ''
  sending.value = true
  error.value = ''
  scrollBottom()
  try {
    if (form.mode === 'req') await sendRequirement(query)
    else await sendQA(query)
  } catch (e) { error.value = '发送失败: ' + e.message }
  finally {
    sending.value = false
    scrollBottom()
  }
}

async function sendQA(query) {
  const payload = { repo_id: form.repo_id, query: query }
  if (sessionId.value) payload.session_id = sessionId.value
  const data = await apiRequest('/chat/ask', { method: 'POST', body: JSON.stringify(payload) })
  sessionId.value = data.session_id
  localStorage.setItem(SESSION_KEY, sessionId.value)
  bubbles.value.push({
    role: 'assistant',
    content: data.answer,
    refs: data.reference_list || [],
    meta: (data.used_model ? ('模型: ' + data.used_model) : '') + (data.cost != null ? (' ｜ 成本: ￥' + data.cost.toFixed(5)) : '')
  })
  loadSessions()
}

async function sendRequirement(query) {
  const payload = { repo_id: form.repo_id, user_requirement: query }
  const data = await apiRequest('/requirement/analyze', { method: 'POST', body: JSON.stringify(payload) })
  let text = '【相关模块】' + (data.related_modules || []).join('、') + '\n\n'
  text += '【需求分析】\n' + data.analysis + '\n\n'
  text += '【风险点】\n' + (data.risk_points || []).map(x => '· ' + x).join('\n') + '\n\n'
  text += '【开发设计建议】\n' + data.suggestion
  bubbles.value.push({
    role: 'assistant',
    content: text,
    refs: (data.related_functions || []).map(f => ({ func_name: f.func_name || '', file_path: f.file_path || '' })),
    meta: (data.used_model ? ('模型: ' + data.used_model) : '') + (data.cost != null ? (' ｜ 成本: ￥' + data.cost.toFixed(5)) : '')
  })
}

onMounted(async () => {
  await initRepos()
  await loadSessions()
  if (sessionId.value) await openSession({ session_id: sessionId.value })
})
</script>