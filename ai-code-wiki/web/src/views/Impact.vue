<template>
  <div class="container">
    <div class="panel">
      <h4>迭代影响分析</h4>
      <div class="row" style="margin-bottom:10px;">
        <label>仓库：</label>
        <select v-model="form.repo_id">
          <option v-for="r in enabledRepos2" :key="r.id" :value="r.id">{{ r.repo_name }}</option>
        </select>
        <label>方式：</label>
        <select v-model="form.mode">
          <option value="nl">自然语言描述变更</option>
          <option value="branch">分支（自动 diff）</option>
        </select>
        <label>方向：</label>
        <select v-model="form.direction">
          <option value="both">上游+下游</option>
          <option value="upstream">仅上游（谁调用我）</option>
          <option value="downstream">仅下游（我调用谁）</option>
        </select>
        <label>深度：</label>
        <select v-model="form.max_depth">
          <option :value="1">1 层</option>
          <option :value="2">2 层</option>
          <option :value="3">3 层</option>
        </select>
      </div>
      <div class="row" style="margin-bottom:10px;">
        <input v-if="form.mode === 'branch'" v-model="form.branch" placeholder="分支名，如 feature/impact-demo（对比默认分支）" style="flex:1;min-width:200px;">
        <textarea v-else v-model="form.query" placeholder="用自然语言描述本次迭代要改什么，例如：我要改支付回调的签名校验逻辑（支持多轮追问，会累计上下文）" style="height:500px;flex:1;min-width:0;resize:vertical;"></textarea>
      </div>
      <div class="row">
        <input v-model="form.version" placeholder="迭代版本号（可选，写入变更日志）" style="flex:1;min-width:160px;">
        <button :disabled="analyzing" @click="analyze">开始分析</button>
        <button class="btn-ghost" :disabled="analyzing" @click="newSession">清空会话（新话题）</button>
      </div>
      <div class="msg ok" v-if="sessionId && analyzed">会话：{{ sessionId }}（多轮追问将累计变更函数）</div>
      <div class="msg err" v-if="error">{{ error }}</div>
    </div>

    <div class="panel" v-if="result">
      <h4>影响分析结果</h4>
      <div class="impact-grid">
        <div class="col rev">
          <h5>上游调用方 <span class="tag">受改动波及</span></h5>
          <div v-for="f in result.reverse_impact" :key="'r'+f.module+'.'+f.func" class="func">
            <div class="name rev">{{ f.module }}.{{ f.func }}<span class="depth">深度{{ f.depth }}</span></div>
            <div class="edge">{{ f.edge }}</div>
            <div class="sum">{{ f.summary || '（暂无文档摘要）' }}</div>
          </div>
          <div class="empty" v-if="!result.reverse_impact || result.reverse_impact.length === 0">无上游调用方受影响</div>
        </div>
        <div class="col chg">
          <h5>直接修改 <span class="tag">本次迭代</span></h5>
          <div v-for="f in result.changed" :key="'c'+f.module+'.'+f.func" class="func">
            <div class="name chg">{{ f.module }}.{{ f.func }}</div>
            <div class="edge">{{ f.file }}</div>
            <div class="sum">{{ f.summary || '（暂无文档摘要）' }}</div>
          </div>
          <div class="empty" v-if="!result.changed || result.changed.length === 0">无直接修改</div>
        </div>
        <div class="col fwd">
          <h5>下游被调用 <span class="tag">改动牵连</span></h5>
          <div v-for="f in result.forward_impact" :key="'f'+f.module+'.'+f.func" class="func">
            <div class="name fwd">{{ f.module }}.{{ f.func }}<span class="depth">深度{{ f.depth }}</span></div>
            <div class="edge">{{ f.edge }}</div>
            <div class="sum">{{ f.summary || '（暂无文档摘要）' }}</div>
          </div>
          <div class="empty" v-if="!result.forward_impact || result.forward_impact.length === 0">无下游被调用受影响</div>
        </div>
      </div>
    </div>

    <div class="panel design" v-if="result && result.design_doc">
      <h4>开发设计文档初稿</h4>
      <div class="sec">
        <b>变更摘要</b>
        <p>{{ result.design_doc.change_summary }}</p>
      </div>
      <div class="sec">
        <b>业务影响范围</b>
        <p>{{ result.design_doc.business_impact }}</p>
      </div>
      <div class="sec">
        <b>上线注意事项与回归建议</b>
        <p>{{ result.design_doc.attention }}</p>
      </div>
      <div v-if="result.func_changes && result.func_changes.length" class="sec">
        <b>各函数个性化变更记录</b>
        <div v-for="fc in result.func_changes" :key="'fc'+fc.module+'.'+fc.func" class="func">
          <div class="name chg">{{ fc.module }}.{{ fc.func }}</div>
          <div class="sum"><b>改动：</b>{{ fc.change_summary }}</div>
          <div class="sum"><b>影响：</b>{{ fc.business_impact }}</div>
          <div class="sum"><b>注意：</b>{{ fc.attention }}</div>
        </div>
      </div>
      <div class="meta" v-if="result.used_model">模型：{{ result.used_model }} ｜ 本次估算成本：￥{{ result.cost != null ? result.cost.toFixed(5) : '-' }}</div>
    </div>

    <div class="msg info" v-if="analyzing">分析中（RAG 定位 → 调用图传播 → LLM 合成设计文档）...</div>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { apiRequest } from '../api'
import { fetchRepos, enabledRepos, defaultRepoId } from '../store/repo'

const SESSION_KEY = 'ai-code-wiki-impact-session'
function genSessionId() {
  return 'sess-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 8)
}

const repos = ref([])
const form = reactive({ repo_id: 0, mode: 'nl', direction: 'both', max_depth: 2, branch: '', query: '', version: '' })
const sessionId = ref(localStorage.getItem(SESSION_KEY) || genSessionId())
const analyzing = ref(false)
const analyzed = ref(false)
const result = ref(null)
const error = ref('')

const enabledRepos2 = computed(() => enabledRepos(repos.value))

async function initRepos() {
  try {
    repos.value = await fetchRepos()
    form.repo_id = defaultRepoId(enabledRepos2.value)
  } catch (e) { error.value = '加载仓库失败: ' + e.message }
}

async function analyze() {
  if (!form.repo_id) { error.value = '请先选择仓库'; return }
  const payload = { repo_id: form.repo_id, max_depth: Number(form.max_depth) || 2, direction: form.direction }
  if (form.mode === 'branch') {
    if (!form.branch.trim()) { error.value = '请输入分支名'; return }
    payload.branch = form.branch.trim()
  } else {
    if (!form.query.trim()) { error.value = '请描述本次迭代要改什么'; return }
    payload.query = form.query.trim()
  }
  if (form.version.trim()) payload.version = form.version.trim()
  payload.session_id = sessionId.value
  analyzing.value = true
  error.value = ''
  result.value = null
  try {
    const data = await apiRequest('/impact/analyze', { method: 'POST', body: JSON.stringify(payload) })
    result.value = data
    analyzed.value = true
    localStorage.setItem(SESSION_KEY, sessionId.value)
  } catch (e) { error.value = '分析失败: ' + e.message }
  finally { analyzing.value = false }
}

function newSession() {
  sessionId.value = genSessionId()
  result.value = null
  analyzed.value = false
  error.value = ''
  localStorage.setItem(SESSION_KEY, sessionId.value)
}

onMounted(initRepos)
</script>