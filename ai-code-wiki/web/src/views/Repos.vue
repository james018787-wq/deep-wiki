<template>
  <div class="container">
    <div class="panel">
      <h4>注册代码仓库</h4>
      <div class="row">
        <input v-model="form.repo_name" placeholder="仓库名（全局唯一）" style="flex:1;min-width:150px;">
        <input v-model="form.repo_url" placeholder="克隆地址（https/ssh/本地路径）" style="flex:2;min-width:260px;">
        <input v-model="form.default_branch" placeholder="默认分支（默认 main）" style="width:140px;">
        <button :disabled="submitting" @click="register">注册</button>
      </div>
      <div class="msg ok" v-if="regMsg">{{ regMsg }}</div>
    </div>

    <div class="panel">
      <h4>仓库列表</h4>
      <table>
        <thead>
          <tr>
            <th>ID</th>
            <th>仓库名</th>
            <th>克隆地址</th>
            <th>默认分支</th>
            <th>状态</th>
            <th>注册时间</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in list" :key="r.id">
            <td>{{ r.id }}</td>
            <td>{{ r.repo_name }}</td>
            <td>{{ r.repo_url }}</td>
            <td>{{ r.default_branch }}</td>
            <td><span class="status" :class="r.status === 1 ? 'ok' : 'err'">{{ r.status === 1 ? '启用' : '停用' }}</span></td>
            <td>{{ formatTime(r.create_time) }}</td>
            <td>
              <button v-if="r.status === 1" class="btn-danger" :disabled="busyId === r.id" @click="setStatus(r, 2)">停用</button>
              <button v-else class="btn-ok" :disabled="busyId === r.id" @click="setStatus(r, 1)">启用</button>
            </td>
          </tr>
          <tr v-if="!loading && list.length === 0">
            <td colspan="7" class="empty">暂无仓库，请先注册</td>
          </tr>
        </tbody>
      </table>
      <div class="msg err" v-if="error">{{ error }}</div>
    </div>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { apiRequest } from '../api'
import { fetchRepos } from '../store/repo'

const form = reactive({ repo_name: '', repo_url: '', default_branch: 'main' })
const submitting = ref(false)
const regMsg = ref('')
const busyId = ref(0)
const list = ref([])
const loading = ref(false)
const error = ref('')

async function register() {
  if (!form.repo_name.trim() || !form.repo_url.trim()) { error.value = '仓库名与克隆地址不能为空'; return }
  submitting.value = true
  error.value = ''
  regMsg.value = ''
  try {
    await apiRequest('/repo/register', { method: 'POST', body: JSON.stringify(form) })
    regMsg.value = '注册成功'
    load()
  } catch (e) { error.value = '注册失败: ' + e.message }
  finally { submitting.value = false }
}

async function setStatus(r, status) {
  busyId.value = r.id
  error.value = ''
  try {
    await apiRequest('/repo/' + r.id + '/status', { method: 'PUT', body: JSON.stringify({ status: status }) })
    r.status = status
  } catch (e) { error.value = '操作失败: ' + e.message }
  finally { busyId.value = 0 }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    list.value = await fetchRepos()
  } catch (e) { error.value = '加载仓库列表失败: ' + e.message }
  finally { loading.value = false }
}

function formatTime(t) {
  return t ? String(t).replace('T', ' ').slice(0, 19) : '-'
}

onMounted(load)
</script>