<template>
  <div class="container">
    <div class="stats" v-if="stats">
      <div class="stat"><b>{{ stats.total_doc_count }}</b><span>总文档</span></div>
      <div class="stat"><b>{{ stats.module_count }}</b><span>业务模块</span></div>
      <div class="stat"><b>{{ stats.auto_doc_count }}</b><span>AI 自动生成</span></div>
      <div class="stat"><b>{{ stats.manual_doc_count }}</b><span>人工校正</span></div>
      <div class="stat warn" v-if="stats.pending_review_count > 0"><b>{{ stats.pending_review_count }}</b><span>待复核</span></div>
    </div>

    <div class="toolbar">
      <label>仓库：</label>
      <select v-model="repoId" @change="onRepoChange">
        <option v-for="r in enabledRepos2" :key="r.id" :value="r.id">{{ r.repo_name }}</option>
      </select>
      <label>模块筛选：</label>
      <select v-model="module" @change="load(1)">
        <option value="">全部模块</option>
        <option v-for="m in modules" :key="m.id" :value="m.module_name">{{ m.module_name }}</option>
      </select>
      <button @click="load(1)">刷新</button>
    </div>

    <table>
      <thead>
        <tr>
          <th>ID</th>
          <th>模块</th>
          <th>函数</th>
          <th>文件路径</th>
          <th>摘要</th>
          <th>来源</th>
          <th>更新时间</th>
          <th>操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="d in list" :key="d.id" class="doc-row" @click="goDetail(d.id)">
          <td>{{ d.id }}</td>
          <td>{{ d.module_name }}</td>
          <td>{{ d.func_name }}</td>
          <td>{{ d.file_path }}</td>
          <td>{{ d.summary }}</td>
          <td><span class="tag" :class="d.content_source === 2 ? 'manual' : 'ai'">{{ d.content_source === 2 ? '人工校正' : 'AI生成' }}</span></td>
          <td>{{ formatTime(d.update_time) }}</td>
          <td><a class="link" href="javascript:void(0)" @click.stop="viewSource(d.id)">查看源码</a></td>
        </tr>
        <tr v-if="!loading && list.length === 0">
          <td colspan="8" class="empty">暂无文档</td>
        </tr>
      </tbody>
    </table>

    <div class="pager">
      <button :disabled="page <= 1" @click="load(page - 1)">上一页</button>
      <span>第 {{ page }} / {{ totalPages }} 页，共 {{ total }} 条</span>
      <button :disabled="page >= totalPages" @click="load(page + 1)">下一页</button>
    </div>

    <div class="msg err" v-if="error">{{ error }}</div>
    <div class="msg info" v-if="loading">加载中...</div>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { apiRequest } from '../api'
import { fetchRepos, enabledRepos, defaultRepoId, setRepoId } from '../store/repo'

const router = useRouter()
const repos = ref([])
const repoId = ref(0)
const modules = ref([])
const module = ref('')
const list = ref([])
const page = ref(1)
const pageSize = 20
const total = ref(0)
const totalPages = ref(1)
const loading = ref(false)
const error = ref('')
const stats = ref(null)

const enabledRepos2 = computed(() => enabledRepos(repos.value))

async function initRepos() {
  try {
    repos.value = await fetchRepos()
    repoId.value = defaultRepoId(enabledRepos2.value)
  } catch (e) { error.value = '加载仓库失败: ' + e.message }
}

function onRepoChange() {
  setRepoId(repoId.value)
  module.value = ''
  loadModules()
  load(1)
}

async function loadModules() {
  try {
    modules.value = await apiRequest('/doc/module/list?repo_id=' + repoId.value)
  } catch (e) { error.value = '加载模块失败: ' + e.message }
}

async function load(p) {
  page.value = p
  loading.value = true
  error.value = ''
  try {
    const params = new URLSearchParams({ repo_id: repoId.value, page: page.value, page_size: String(pageSize) })
    if (module.value) params.set('module', module.value)
    const data = await apiRequest('/doc/list?' + params.toString())
    list.value = data.list || []
    total.value = data.total || 0
    totalPages.value = data.page_size ? Math.max(1, Math.ceil(total.value / data.page_size)) : 1
  } catch (e) { error.value = '加载文档失败: ' + e.message }
  finally { loading.value = false }
}

function goDetail(id) {
  router.push('/doc-edit/' + id)
}

function viewSource(id) {
  window.open('/doc-source/' + id, '_blank')
}

function formatTime(t) {
  return t ? String(t).replace('T', ' ').slice(0, 19) : '-'
}

async function loadStats() {
  try {
    stats.value = await apiRequest('/report/basic')
  } catch (e) { /* 统计失败不阻塞列表 */ }
}

onMounted(async () => {
  loadStats()
  await initRepos()
  loadModules()
  load(1)
})
</script>