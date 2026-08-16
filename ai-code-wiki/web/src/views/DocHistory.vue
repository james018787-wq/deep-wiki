<template>
  <div class="container">
    <div class="toolbar">
      <button @click="goEdit">返回编辑</button>
      <button @click="load">刷新</button>
    </div>

    <div class="panel">
      <h4>历史修改记录（doc_id={{ docId }}）</h4>
      <table>
        <thead>
          <tr>
            <th>log_id</th>
            <th>操作</th>
            <th>操作人</th>
            <th>修改时间</th>
            <th>备注</th>
            <th>快照</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in list" :key="item.log_id" class="log-row"
              :class="{ active: detail && detail.log_id === item.log_id }"
              @click="viewSnapshot(item.log_id)">
            <td>{{ item.log_id }}</td>
            <td><span class="tag" :class="operateClass(item.operate_type)">{{ item.operate_name }}</span></td>
            <td>{{ item.operator }}</td>
            <td>{{ formatTime(item.operate_time) }}</td>
            <td>{{ item.remark || '-' }}</td>
            <td><a class="link" href="javascript:void(0)" @click.stop="viewSnapshot(item.log_id)">查看</a></td>
          </tr>
          <tr v-if="!loading && list.length === 0">
            <td colspan="6" class="empty">暂无历史修改记录</td>
          </tr>
        </tbody>
      </table>

      <div class="msg info" v-if="loading">加载中...</div>
      <div class="msg err" v-if="error">{{ error }}</div>
    </div>

    <div class="panel" v-if="detail">
      <h4>快照详情 #{{ detail.log_id }}
        <span class="tag" :class="operateClass(detail.operate_type)">{{ detail.operate_name }}</span>
        <span class="meta" style="margin-left:8px;display:inline-block;">{{ detail.operator }} · {{ formatTime(detail.operate_time) }}</span>
      </h4>
      <div class="msg info" style="margin-top:0;margin-bottom:12px;font-size:12px;" v-if="detail.remark">
        备注：{{ detail.remark }}
      </div>
      <div class="snapshot">
        <details open>
          <summary>修改前快照 before_content（原始 JSON）</summary>
          <pre>{{ prettyJSON(detail.before) }}</pre>
        </details>
        <details>
          <summary>修改后快照 after_content（原始 JSON）</summary>
          <pre>{{ prettyJSON(detail.after) }}</pre>
        </details>
      </div>
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { apiRequest } from '../api'

const route = useRoute()
const router = useRouter()
const docId = ref(0)
const list = ref([])
const detail = ref(null)
const loading = ref(false)
const error = ref('')

function goEdit() {
  router.push('/doc-edit/' + docId.value)
}

function load() {
  error.value = ''
  loading.value = true
  apiRequest('/doc/' + docId.value + '/history')
    .then(l => { list.value = l || [] })
    .catch(e => { error.value = '加载历史记录失败: ' + e.message })
    .finally(() => { loading.value = false })
}

function viewSnapshot(logId) {
  if (detail.value && detail.value.log_id === logId) {
    detail.value = null
    return
  }
  error.value = ''
  loading.value = true
  apiRequest('/doc/' + docId.value + '/history/' + logId)
    .then(d => { detail.value = d })
    .catch(e => { error.value = '加载快照详情失败: ' + e.message })
    .finally(() => { loading.value = false })
}

function formatTime(t) {
  return t ? String(t).replace('T', ' ').slice(0, 19) : '-'
}
function operateClass(type) {
  return type === 2 ? 'reset' : 'edit'
}
function prettyJSON(obj) {
  return JSON.stringify(obj || {}, null, 2)
}

onMounted(() => {
  docId.value = parseInt(route.params.id, 10)
  if (!docId.value) {
    error.value = '缺少 doc_id 参数'
    return
  }
  load()
})
</script>