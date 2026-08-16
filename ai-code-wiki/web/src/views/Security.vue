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
              <div class="actions">
                <button class="btn-ghost act-btn" @click="openDetail(f)">详情</button>
                <button v-if="f.status === 'open'" class="btn-ok act-btn" :disabled="busyId === f.id" @click="setStatus(f, 'fixed')">标记已修复</button>
                <button v-if="f.status === 'open'" class="btn-ghost act-btn" :disabled="busyId === f.id" @click="setStatus(f, 'false_positive')">误报</button>
                <button v-else class="btn-ghost act-btn" :disabled="busyId === f.id" @click="setStatus(f, 'open')">重新打开</button>
              </div>
            </td>
          </tr>
          <tr v-if="!loading && list.length === 0">
            <td colspan="7" class="empty">暂无安全发现</td>
          </tr>
        </tbody>
      </table>
      <div class="pager" v-if="list.length">
        <button :disabled="page <= 1" @click="load(page - 1)">上一页</button>
        <span>{{ page }} / {{ pages }}</span>
        <button :disabled="page >= pages" @click="load(page + 1)">下一页</button>
      </div>
    </div>

    <div class="modal-mask" v-if="detailModal.show" @click.self="closeDetail">
      <div class="modal modal-wide">
        <h4>安全发现详情</h4>
        <div class="detail-grid">
          <div class="d-row"><span>文件:行</span><b>{{ detailModal.f.file_path }}:{{ detailModal.f.line }}</b></div>
          <div class="d-row"><span>类型</span><b>{{ typeLabel(detailModal.f.secret_type) }}</b></div>
          <div class="d-row"><span>风险</span><span class="status" :class="riskClass(detailModal.f.risk_level)">{{ riskLabel(detailModal.f.risk_level) }}</span></div>
          <div class="d-row"><span>命中值</span><code class="secret">{{ detailModal.f.secret_value }}</code></div>
          <div class="d-row"><span>状态</span><span class="status" :class="statusClass(detailModal.f.status)">{{ statusLabel(detailModal.f.status) }}</span></div>
          <div class="d-row"><span>上下文</span><code class="snip">{{ detailModal.f.snippet || '-' }}</code></div>
        </div>
        <div class="detail-rec" v-if="detailModal.f.recommendation">
          <b>修复建议</b>
          <p>{{ detailModal.f.recommendation }}</p>
        </div>
        <div class="modal-actions">
          <button class="btn-ghost" @click="closeDetail">关闭</button>
        </div>
      </div>
    </div>

    <div class="modal-mask" v-if="scanModal.show" @click.self="closeScanModal">
      <div class="modal">
        <h4>扫描完成 <span class="hint">{{ repoName }}</span></h4>
        <div class="scan-result">
          <div class="result-row"><span>扫描文件</span><b>{{ scanModal.summary.scanned_files }}</b></div>
          <div class="result-row risk-high"><span>高危</span><b>{{ scanModal.summary.high }}</b></div>
          <div class="result-row risk-medium"><span>中危</span><b>{{ scanModal.summary.medium }}</b></div>
          <div class="result-row risk-low"><span>低危</span><b>{{ scanModal.summary.low }}</b></div>
          <div class="result-row risk-total"><span>发现总数</span><b>{{ scanModal.summary.total }}</b></div>
        </div>
        <div class="msg info" v-if="scanModal.summary.total === 0">未发现敏感信息，扫描干净。</div>
        <div class="modal-actions">
          <button class="btn-ghost" @click="closeScanModal">关闭</button>
        </div>
      </div>
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
const scanModal = reactive({ show: false, summary: null })
const detailModal = reactive({ show: false, f: {} })
const list = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const loading = ref(false)
const error = ref('')
const busyId = ref(0)
const filter = reactive({ status: '', risk: '' })

const enabledRepos2 = computed(() => enabledRepos(repos.value))
const pages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
const repoName = computed(() => {
  const r = repos.value.find(x => x.id === repo_id.value)
  return r ? r.repo_name : ''
})

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
    const s = await apiRequest('/security/scan', { method: 'POST', body: JSON.stringify({ repo_id: repo_id.value }) })
    scanModal.summary = s
    scanModal.show = true
    load(1)
  } catch (e) { error.value = '扫描失败: ' + e.message }
  finally { scanning.value = false }
}

function openDetail(f) {
  detailModal.f = f
  detailModal.show = true
}

function closeDetail() {
  detailModal.show = false
}

function closeScanModal() {
  scanModal.show = false
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
.actions { display: flex; gap: 6px; align-items: center; flex-wrap: nowrap; }
.act-btn { min-width: 90px; height: 32px; padding: 0 8px; white-space: nowrap; box-sizing: border-box; }
td:last-child { white-space: nowrap; min-width: 196px; }
.scan-result { display: flex; flex-direction: column; gap: 8px; margin: 12px 0; }
.result-row {
  display: flex; justify-content: space-between; align-items: center;
  padding: 8px 12px; border: 1px solid var(--line); border-radius: 8px;
  font-size: 13px; color: var(--text-dim); background: rgba(255,255,255,.03);
}
.result-row b { font-size: 16px; color: var(--text-bright); }
.result-row.risk-high b { color: var(--red); }
.result-row.risk-medium b { color: var(--amber); }
.result-row.risk-low b { color: var(--green); }
.result-row.risk-total b { color: var(--cyan); }
.detail-grid { display: flex; flex-direction: column; gap: 8px; margin: 12px 0; }
.d-row {
  display: flex; align-items: center; gap: 10px;
  padding: 7px 12px; border: 1px solid var(--line); border-radius: 8px;
  font-size: 13px; color: var(--text-dim); background: rgba(255,255,255,.03);
}
.d-row span:first-child { min-width: 64px; color: var(--text-dim); }
.d-row b { color: var(--text-bright); word-break: break-all; }
.detail-rec {
  border: 1px solid rgba(96, 165, 250, 0.3); border-radius: 8px;
  background: rgba(96, 165, 250, 0.08); padding: 10px 12px; margin-top: 10px;
}
.detail-rec b { color: var(--blue); font-size: 13px; }
.detail-rec p { margin: 6px 0 0; color: var(--text); font-size: 13px; line-height: 1.6; }
.line { color: var(--amber); }
.secret { font-family: var(--mono); font-size: 12px; color: var(--red); background: rgba(248,113,113,.08); padding: 1px 6px; border-radius: 4px; }
.snip { font-family: var(--mono); font-size: 12px; color: #93a4c3; background: rgba(255,255,255,.04); padding: 1px 6px; border-radius: 4px; max-width: 360px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; display: inline-block; }
.pager { display: flex; gap: 12px; align-items: center; margin-top: 10px; font-size: 13px; color: var(--text-dim); }
.hint { margin-left: auto; color: #51698c; }
</style>