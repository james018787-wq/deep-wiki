<template>
  <div class="container">
    <div class="panel">
      <h3 style="margin-top:0;">文档编辑 / 详情</h3>

      <div class="meta">
        <div><b>ID:</b> {{ doc.id }} <b>模块:</b> {{ doc.module_name }} <b>函数:</b> {{ doc.func_name }}</div>
        <div><b>文件:</b> {{ doc.file_path }}</div>
        <div><b>来源:</b> {{ doc.content_source === 2 ? '人工校正' : 'AI自动生成' }}
             <b>待复核:</b> {{ doc.source_code_changed === 1 ? '是' : '否' }}
             <b>最近操作人:</b> {{ doc.last_edit_user || '-' }}</div>
      </div>

      <div class="form-item">
        <label>业务摘要 summary</label>
        <input v-model="form.summary" placeholder="一句话业务摘要">
      </div>
      <div class="form-item">
        <label>入参说明 input_desc</label>
        <textarea v-model="form.input_desc"></textarea>
      </div>
      <div class="form-item">
        <label>返回值说明 output_desc</label>
        <textarea v-model="form.output_desc"></textarea>
      </div>
      <div class="form-item">
        <label>业务执行流程 process_flow</label>
        <textarea v-model="form.process_flow"></textarea>
      </div>
      <div class="form-item">
        <label>业务风险点 risk_point</label>
        <textarea v-model="form.risk_point"></textarea>
      </div>
      <div class="form-item">
        <label>备注 remark</label>
        <input v-model="form.remark" placeholder="本次修改说明">
      </div>

      <div class="actions">
        <button class="btn-save" :disabled="saving" @click="save">保存校正</button>
        <button class="btn-reset" :disabled="saving" @click="reset">重置为原始AI版本</button>
        <button class="btn-back" @click="goSource">查看源码</button>
        <button class="btn-back" @click="goHistory">历史版本</button>
        <button class="btn-back" @click="goList">返回列表</button>
      </div>

      <div class="msg err" v-if="error">{{ error }}</div>
      <div class="msg ok" v-if="okMsg">{{ okMsg }}</div>

      <details v-if="doc.origin_auto_doc">
        <summary>查看原始 AI 自动生成文档（origin_auto_doc，只读）</summary>
        <pre>{{ doc.origin_auto_doc }}</pre>
      </details>
    </div>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { apiRequest } from '../api'

const route = useRoute()
const router = useRouter()
const docId = ref(0)
const doc = ref({})
const form = reactive({ summary: '', input_desc: '', output_desc: '', process_flow: '', risk_point: '', remark: '' })
const saving = ref(false)
const error = ref('')
const okMsg = ref('')

function docPath() {
  return '/doc/' + docId.value
}
function goHistory() {
  router.push('/doc-history/' + docId.value)
}
function goSource() {
  window.open('/doc-source/' + docId.value, '_blank')
}
function goList() {
  router.push('/docs')
}

async function load() {
  error.value = ''
  try {
    doc.value = await apiRequest(docPath())
    form.summary = doc.value.summary || ''
    form.input_desc = doc.value.input_desc || ''
    form.output_desc = doc.value.output_desc || ''
    form.process_flow = doc.value.process_flow || ''
    form.risk_point = doc.value.risk_point || ''
  } catch (e) {
    error.value = '加载文档失败: ' + e.message
  }
}

async function save() {
  saving.value = true
  error.value = ''
  okMsg.value = ''
  try {
    await apiRequest(docPath() + '/edit', {
      method: 'PUT',
      body: JSON.stringify(form)
    })
    okMsg.value = '保存成功，已写入操作日志并同步向量库'
    load()
  } catch (e) { error.value = '保存失败: ' + e.message }
  finally { saving.value = false }
}

async function reset() {
  if (!confirm('确认重置为原始 AI 自动生成版本？')) return
  saving.value = true
  error.value = ''
  okMsg.value = ''
  try {
    await apiRequest(docPath() + '/reset', {
      method: 'POST',
      body: JSON.stringify({ remark: form.remark })
    })
    okMsg.value = '已重置为原始 AI 版本'
    load()
  } catch (e) { error.value = '重置失败: ' + e.message }
  finally { saving.value = false }
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