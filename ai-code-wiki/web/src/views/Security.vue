<template>
  <div class="container">
    <div class="panel">
      <h4>代码安全扫描</h4>
      <div class="row" style="margin-bottom:10px;">
        <label>仓库：</label>
        <select v-model="repo_id">
          <option v-for="r in enabledRepos2" :key="r.id" :value="r.id">{{ r.repo_name }}</option>
        </select>
        <button :disabled="scanning" @click="scan">开始扫描</button>
        <button class="btn-ghost" :disabled="loading" @click="load(1)">刷新</button>
      </div>
      <div class="scan-summary" v-if="summary">
        <span>扫描文件：<b>{{ summary.scanned_files }}</b></span>
        <span class="risk high">高危 <b>{{ summary.high }}</b></span>
        <span class="risk medium">中危 <b>{{ summary.medium }}</b></span>
        <span class="risk low">低危 <b>{{ summary.low }}</b></span>
        <span class="risk total">发现总数 <b>{{ summary.total }}</b></span>
      </div>
      <div class="msg err" v-if="error">{{ error }}</div>
    </div>

    <div class="panel">
      <h4>安全发现列表</h4>
      <div class="row" style="margin-bottom:10px;">
        <label>状态：</label>
        <select v-model="filter.status" @change="load(1)">
          <option value="">全部</option>
          <option value="open">待处理</option>
          <option value="fixed">已修复</option>
          <option value="false_positive">误报</option>
        </select>
        <label>风险：</label>
        <select v-model="filter.risk" @change="load(1)">
          <option value="">全部</option>
          <option value="high">高危</option>
          <option value="medium">中危</option>
          <option value="low">低危</option>
        </select>
        <span class="hint">共 {{ total }} 条</span>
      </div>
      <table>
        <thead>
          <tr>
            <th>文件:行</th>
            <th>类型</th>
            <th>风险</th>
            <th>命中值</th>
            <th>上下文</th>
            <th>状态</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="f in list" :key="f.id">
            <td><b>{{ f.file_path }}</b><span class="line" v-if="f.line">:{{ f.line }}</span></td>
            <td>{{ typeLabel(f.secret_type) }}</td>
            <td><span class="status" :class="riskClass(f.risk_level)">{{ riskLabel(f.risk_level) }}</span></td>
            <td><code class="secret">{{ f.secret_value }}</code></td>
            <td><code class="snip">{{ f.snippet || '-' }}</code></td>
            <td><span class="status" :class="statusClass(f.status)">{{ statusLabel(f.status) }}</span></td>
            <td>
              <button v-if="f.status === 'open'" class="btn-ok" :disabled="busyId === f.id" @click="setStatus(f, 'fixed')">标记已修复</button>
              <button v-if="f.status === 'open'" class="btn-ghost" :disabled="busyId === f.id" @click="setStatus(f, 'false_positive')" style="margin-left:4px;">误报</button>
              <button v-else class="btn-ghost" :disabled="busyId === f.id" @click="setStatus(f, 'open')">重新打开</button>
            </td>
          </tr>
          <tr v-if="!loading && list.length === 0">
            <td colspan="7" class="empty">暂无安全发现</td>
          </tr>
        </tbody>
      </table>
      <div class="pager" v-if="total > pageSize">
        <button :disabled="page <= 1" @click="load(page - 1)">上一页</button>
        <span>{{ page }} / {{ pages }}</span>
        <button :disabled="page >= pages" @click="load(page + 1)">下一页</button>
      </div>
      <template v-for="f in list" :key="'r'+f.id">
        <div class="msg ok" v-if="f.recommendation && showRecommend">
          <b>{{ typeLabel(f.secret_type) }} 修复建议：</b>{{ f.recommendation }}
        </div>
      </template>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { apiRequest } from '../api'
import { fetchRepos, enabledRepos, defaultRepoId } from '../store/repo'

const repos = ref([])
const repo_id = ref(0)
const scanning = ref(false)
const summary = ref(null)
const list = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const loading = ref(false)
const error = ref('')
const busyId = ref(0)
const showRecommend = ref(false)
const filter = reactive({ status: '', risk: '' })

const enabledRepos2 = computed(() => enabledRepos(repos.value))
const pages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))

function typeLabel(t) { return ({ aws_access_key: 'AWS Key', github_token: 'GitHub Token', gitlab_token: 'GitLab Token', openai_key: 'AI 密钥', private_key: '私钥', conn_string: '连接串', password: '密码', secret: '密钥', api_key: 'API Key', jwt_token: 'JWT', cookie_session: '会话密钥' })[t] || t }
function riskLabel(r) { return ({ high: '高危', medium: '中危', low: '低危' })[r] || r }
function statusLabel(s) { return ({ open: '待处理', fixed: '已修复', false_positive: '误报' })[s] || s }
function riskClass(r) { return ({ high: 'err', medium: 'warn', low: 'ok' })[r] || '' }
function statusClass(s) { return ({ open: 'warn', fixed: 'ok', false_positive: 'ai' })[s] || '' }

async function initRepos() {
  try {
    repos.value = await fetchRepos()
    repo_id.value = defaultRepoId(enabledRepos2.value)
    if (repo_id.value) load(1)
  } catch (e) { error.value = '加载仓库失败: ' + e.message }
}

async function scan() {
  if (!repo_id.value) { error.value = '请先选择仓库'; return }
  scanning.value = true
  error.value = ''
  try {
    summary.value = await apiRequest('/security/scan', { method: 'POST', body: JSON.stringify({ repo_id: repo_id.value }) })
    showRecommend.value = true
    load(1)
  } catch (e) { error.value = '扫描失败: ' + e.message }
  finally { scanning.value = false }
}

async function load(p) {
  if (!repo_id.value) return
  page.value = p
  loading.value = true
  error.value = ''
  try {
    const d = await apiRequest('/security/list?repo_id=' + repo_id.value +
      (filter.status ? '&status=' + filter.status : '') +
      (filter.risk ? '&risk=' + filter.risk : '') +
      '&page=' + page.value + '&page_size=' + pageSize)
    list.value = d.list || []
    total.value = d.total || 0
  } catch (e) { error.value = '加载安全发现失败: ' + e.message }
  finally { loading.value = false }
}

async function setStatus(f, status) {
  busyId.value = f.id
  error.value = ''
  try {
    await apiRequest('/security/' + f.id + '/status', { method: 'PUT', body: JSON.stringify({ status }) })
    f.status = status
  } catch (e) { error.value = '操作失败: ' + e.message }
  finally { busyId.value = 0 }
}

watch(repo_id, (v) => { if (v) load(1) })
onMounted(initRepos)
</script>

<style scoped>
.scan-summary { display: flex; gap: 18px; align-items: center; font-size: 13px; color: var(--text-dim); margin-bottom: 6px; flex-wrap: wrap; }
.risk b { font-size: 15px; }
.risk.high b { color: var(--red); }
.risk.medium b { color: var(--amber); }
.risk.low b { color: var(--green); }
.risk.total b { color: var(--cyan); }
.line { color: var(--amber); }
.secret { font-family: var(--mono); font-size: 12px; color: var(--red); background: rgba(248,113,113,.08); padding: 1px 6px; border-radius: 4px; }
.snip { font-family: var(--mono); font-size: 12px; color: #93a4c3; background: rgba(255,255,255,.04); padding: 1px 6px; border-radius: 4px; max-width: 360px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; display: inline-block; }
.pager { display: flex; gap: 12px; align-items: center; margin-top: 10px; font-size: 13px; color: var(--text-dim); }
.hint { margin-left: auto; color: #51698c; }
</style>