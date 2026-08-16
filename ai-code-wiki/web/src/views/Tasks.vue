<template>
  <div class="container">
    <div class="panel">
      <h4>触发索引任务</h4>
      <div class="row" style="margin-bottom:10px;">
        <label>仓库：</label>
        <select v-model="trigger.repo_id">
          <option v-for="r in enabledRepos2" :key="r.id" :value="r.id">{{ r.repo_name }}</option>
        </select>
        <input v-model="trigger.task_id" placeholder="task_id（外部系统任务标识）" style="flex:1;min-width:160px;">
        <input v-model="trigger.branch" placeholder="branch（如 main）" style="flex:1;min-width:120px;">
        <button :disabled="triggering" @click="doTrigger">触发</button>
      </div>
      <div class="msg ok" v-if="triggerMsg">{{ triggerMsg }}</div>
    </div>

    <div class="panel">
      <h4>查询任务状态</h4>
      <div class="row">
        <input v-model="statusTaskId" placeholder="输入 task_id 查询状态" style="flex:1;min-width:200px;">
        <button :disabled="querying" @click="queryStatus">查询</button>
      </div>
      <div class="msg info" v-if="statusInfo">{{ statusInfo }}</div>
    </div>

    <div class="panel">
      <h4>任务列表</h4>
      <div class="row" style="margin-bottom:10px;">
        <label>仓库筛选：</label>
        <select v-model="filterRepoId" @change="load(1)">
          <option value="0">全部仓库</option>
          <option v-for="r in repos" :key="r.id" :value="r.id">{{ r.repo_name }}</option>
        </select>
      </div>
      <table>
        <thead>
          <tr>
            <th>task_id</th>
            <th>仓库</th>
            <th>分支</th>
            <th>状态</th>
            <th>错误信息</th>
            <th>创建时间</th>
            <th>完成时间</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="t in list" :key="t.id">
            <td>{{ t.task_id }}</td>
            <td>{{ repoName(repos, t.repo_id) }}</td>
            <td>{{ t.branch }}</td>
            <td><span class="status" :class="statusClass(t.status)">{{ statusText(t.status) }}</span></td>
            <td>{{ t.err_msg || '-' }}</td>
            <td>{{ formatTime(t.create_time) }}</td>
            <td>{{ formatTime(t.finish_time) }}</td>
          </tr>
          <tr v-if="!loading && list.length === 0">
            <td colspan="7" class="empty">暂无任务</td>
          </tr>
        </tbody>
      </table>
      <div class="pager">
        <button :disabled="page <= 1" @click="load(page - 1)">上一页</button>
        <span>第 {{ page }} / {{ totalPages }} 页，共 {{ total }} 条</span>
        <button :disabled="page >= totalPages" @click="load(page + 1)">下一页</button>
        <button @click="load(1)">刷新</button>
      </div>
    </div>

    <div class="msg err" v-if="error">{{ error }}</div>
    <div class="msg info" v-if="loading">加载中...</div>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { apiRequest } from '../api'
import { fetchRepos, enabledRepos, defaultRepoId, repoName } from '../store/repo'

const repos = ref([])
const trigger = reactive({ repo_id: 0, task_id: '', branch: '' })
const triggering = ref(false)
const triggerMsg = ref('')
const statusTaskId = ref('')
const querying = ref(false)
const statusInfo = ref('')
const filterRepoId = ref(0)
const list = ref([])
const page = ref(1)
const pageSize = 20
const total = ref(0)
const totalPages = ref(0)
const loading = ref(false)
const error = ref('')

const enabledRepos2 = computed(() => enabledRepos(repos.value))

async function initRepos() {
  try {
    repos.value = await fetchRepos()
    trigger.repo_id = defaultRepoId(enabledRepos2.value)
  } catch (e) { error.value = '加载仓库失败: ' + e.message }
}

async function doTrigger() {
  if (!trigger.repo_id) { error.value = '请先选择仓库'; return }
  if (!trigger.task_id.trim() || !trigger.branch.trim()) { error.value = 'task_id 与 branch 不能为空'; return }
  triggering.value = true
  error.value = ''
  triggerMsg.value = ''
  try {
    const data = await apiRequest('/task/trigger', { method: 'POST', body: JSON.stringify(trigger) })
    triggerMsg.value = '触发成功，task_id=' + data.task_id + ' 状态=' + statusText(data.status)
    load(1)
  } catch (e) { error.value = '触发失败: ' + e.message }
  finally { triggering.value = false }
}

async function queryStatus() {
  const tid = statusTaskId.value.trim()
  if (!tid) { error.value = '请输入 task_id'; return }
  querying.value = true
  error.value = ''
  statusInfo.value = ''
  try {
    const s = await apiRequest('/task/status?task_id=' + encodeURIComponent(tid))
    statusInfo.value = 'task_id=' + s.task_id + ' 仓库=' + repoName(repos.value, s.repo_id) + ' 状态=' + statusText(s.status) + (s.err_msg ? '，错误：' + s.err_msg : '')
  } catch (e) { error.value = '查询失败: ' + e.message }
  finally { querying.value = false }
}

async function load(p) {
  page.value = p
  loading.value = true
  error.value = ''
  try {
    const params = new URLSearchParams({ page: page.value, page_size: String(pageSize) })
    if (filterRepoId.value > 0) params.set('repo_id', filterRepoId.value)
    const data = await apiRequest('/task/list?' + params.toString())
    list.value = data.list || []
    total.value = data.total || 0
    totalPages.value = data.page_size ? Math.max(1, Math.ceil(total.value / data.page_size)) : 1
  } catch (e) { error.value = '加载任务列表失败: ' + e.message }
  finally { loading.value = false }
}

function statusText(s) {
  return { 0: '待执行', 1: '执行中', 2: '成功', 3: '失败' }[s] || '未知'
}
function statusClass(s) {
  return { 0: 'ai', 1: 'ai', 2: 'ok', 3: 'err' }[s] || 'ai'
}
function formatTime(t) {
  return t ? String(t).replace('T', ' ').slice(0, 19) : '-'
}

onMounted(async () => {
  await initRepos()
  load(1)
})
</script>