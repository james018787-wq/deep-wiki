<template>
  <div class="container">
    <div class="panel">
      <h4>模型配置</h4>
      <div class="msg info" v-if="modelsError">{{ modelsError }}</div>
      <table>
        <thead>
          <tr>
            <th>模型</th>
            <th>供应商</th>
            <th>状态</th>
            <th>运行状态</th>
            <th>输入价格(元/1k)</th>
            <th>输出价格(元/1k)</th>
            <th>上下文</th>
            <th>RPM/TPM</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="m in models" :key="m.name">
            <td><b>{{ m.name }}</b></td>
            <td>{{ m.provider }}</td>
            <td><span class="status" :class="m.enable ? 'ok' : 'muted'">{{ m.enable ? '启用' : '停用' }}</span></td>
            <td>
              <span v-if="statusOf(m.name).circuit_open" class="status warn">熔断中 {{ statusOf(m.name).circuit_ttl }}s</span>
              <span v-else-if="!m.enable" class="status muted">停用</span>
              <span v-else-if="!statusOf(m.name).key_ready" class="status muted">密钥未配置</span>
              <span v-else class="status ok">正常</span>
              <div class="hint" v-if="statusOf(m.name).degrade_count > 0">累计降级 {{ statusOf(m.name).degrade_count }} 次</div>
            </td>
            <td>{{ m.input_price }}</td>
            <td>{{ m.output_price }}</td>
            <td>{{ m.max_context > 0 ? m.max_context.toLocaleString() : '-' }}</td>
            <td>{{ m.rpm }} / {{ m.tpm }}</td>
          </tr>
          <tr v-if="models.length === 0">
            <td colspan="8" class="empty">暂无模型配置（model_pool.yaml 为空或 AI 服务不可用）</td>
          </tr>
        </tbody>
      </table>
      <div class="hint" v-if="global && Object.keys(global).length">调度参数：最大降级切换 {{ global.max_retry_switch }} 次 · 熔断阈值 {{ global.circuit_failure_threshold }} 次/{{ global.circuit_ttl_sec }}s · 高配阈值 ¥{{ global.high_quality_price_threshold }}/1k</div>
    </div>

    <div class="panel">
      <h4>模型消耗</h4>
      <div class="row" style="margin-bottom:10px;">
        <label>时间：</label>
        <select v-model="range" @change="loadUsage">
          <option value="1">近 1 天</option>
          <option value="7">近 7 天</option>
          <option value="30">近 30 天</option>
        </select>
        <label>场景：</label>
        <select v-model="scenario" @change="loadUsage">
          <option value="">全部</option>
          <option value="doc">文档生成</option>
          <option value="search">智能问答</option>
          <option value="chat">多轮对话</option>
          <option value="requirement">需求分析</option>
          <option value="impact">迭代影响</option>
          <option value="func_change">函数变更记录</option>
          <option value="rollup">会话摘要</option>
        </select>
        <label>维度：</label>
        <select v-model="groupBy" @change="loadUsage">
          <option value="model">按模型</option>
          <option value="day">按天</option>
          <option value="scenario">按场景</option>
        </select>
        <button class="btn-ghost" :disabled="loading" @click="loadUsage">刷新</button>
      </div>

      <div class="cards" v-if="total">
        <div class="card"><span class="c-label">调用次数</span><b class="c-val">{{ total.calls }}</b></div>
        <div class="card"><span class="c-label">输入 tokens</span><b class="c-val">{{ fmt(total.input_tokens) }}</b></div>
        <div class="card"><span class="c-label">输出 tokens</span><b class="c-val">{{ fmt(total.output_tokens) }}</b></div>
        <div class="card"><span class="c-label">总 tokens</span><b class="c-val">{{ fmt(total.total_tokens) }}</b></div>
        <div class="card"><span class="c-label">预估成本</span><b class="c-val">¥{{ total.cost.toFixed(4) }}</b></div>
      </div>
      <div class="msg err" v-if="usageError">{{ usageError }}</div>

      <table>
        <thead>
          <tr>
            <th>{{ groupBy === 'model' ? '模型' : (groupBy === 'day' ? '日期' : '场景') }}</th>
            <th>调用次数</th>
            <th>输入 tokens</th>
            <th>输出 tokens</th>
            <th>总 tokens</th>
            <th>成本(元)</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in rows" :key="r.group_key">
            <td><b>{{ r.group_key || '-' }}</b></td>
            <td>{{ r.calls }}</td>
            <td>{{ fmt(r.input_tokens) }}</td>
            <td>{{ fmt(r.output_tokens) }}</td>
            <td>{{ fmt(r.total_tokens) }}</td>
            <td>¥{{ r.cost.toFixed(4) }}</td>
          </tr>
          <tr v-if="rows.length === 0">
            <td colspan="6" class="empty">暂无消耗数据</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { apiRequest } from '../api'

const models = ref([])
const global = ref({})
const modelsError = ref('')
const statusMap = ref({})
const rows = ref([])
const total = ref(null)
const loading = ref(false)
const usageError = ref('')
const range = ref('7')
const scenario = ref('')
const groupBy = ref('model')

function statusOf(name) {
  return statusMap.value[name] || {}
}

function fmt(n) {
  return Number(n || 0).toLocaleString('zh-CN')
}

async function loadModels() {
  try {
    const data = await apiRequest('/model/list')
    models.value = data.models || []
    global.value = data.global || {}
  } catch (e) {
    modelsError.value = '模型配置加载失败: ' + e.message
  }
}

async function loadStatus() {
  try {
    const data = await apiRequest('/model/status')
    const map = {}
    for (const m of (data.models || [])) map[m.name] = m
    statusMap.value = map
  } catch (e) {
    /* 状态接口失败不阻塞页面 */
  }
}

async function loadUsage() {
  loading.value = true
  usageError.value = ''
  try {
    const data = await apiRequest('/model/usage?days=' + range.value + '&scenario=' + encodeURIComponent(scenario.value) + '&group_by=' + groupBy.value)
    rows.value = data.rows || []
    total.value = data.total || null
  } catch (e) {
    usageError.value = '消耗统计加载失败: ' + e.message
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  loadModels()
  loadStatus()
  loadUsage()
})
</script>

<style scoped>
.cards {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-bottom: 14px;
}
.card {
  border: 1px solid #e3e8ef;
  border-radius: 8px;
  padding: 10px 16px;
  min-width: 120px;
  background: #f8fafc;
}
.c-label {
  display: block;
  font-size: 12px;
  color: #667085;
}
.c-val {
  font-size: 18px;
  color: #1d2939;
}
.status.ok { color: #067647; }
.status.muted { color: #667085; }
.status.warn { color: #b54708; background: #fef0c7; padding: 2px 6px; border-radius: 4px; }
</style>