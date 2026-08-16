<template>
  <div class="container">
    <div class="panel">
      <h4>迭代影响分析</h4>
      <div class="row" style="margin-bottom:10px;">
        <label>仓库：</label>
        <select v-model="form.repo_id">
          <option v-for="r in enabledRepos2" :key="r.id" :value="r.id">{{ r.repo_name }}</option>
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
        <input v-model="form.branch" placeholder="分支名，如 feature/order-refactor（自动 diff 对比默认分支，精准推导本次改动）" style="flex:1;min-width:200px;">
      </div>
      <div class="row">
        <input v-model="form.version" placeholder="迭代版本号（可选，写入变更日志）" style="flex:1;min-width:160px;">
        <button :disabled="analyzing" @click="analyze">开始分析</button>
      </div>
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

    <div class="panel" v-if="result && result.graph && result.graph.nodes && result.graph.nodes.length">
      <h4>调用关系图 <span class="tag">D3 可视化</span></h4>
      <CallGraph :graph="result.graph" :height="460" />
    </div>

    <div class="panel" v-if="result && result.api_schema && result.api_schema.length">
      <h4>接口 / API 变更 <span class="tag">破坏性变更检测</span></h4>
      <table>
        <thead>
          <tr><th>函数</th><th>类型</th><th>签名变化</th></tr>
        </thead>
        <tbody>
          <tr v-for="(a, i) in result.api_schema" :key="'api'+i">
            <td><b>{{ a.module }}.{{ a.func }}</b><div class="edge">{{ a.file }}</div></td>
            <td>
              <span class="status" :class="a.change_type === 'removed' ? 'err' : (a.change_type === 'modified' ? 'warn' : 'ok')">
                {{ {added:'新增', modified:'签名变更', removed:'删除'}[a.change_type] || a.change_type }}
              </span>
            </td>
            <td>
              <div v-if="a.change_type === 'modified'"><div class="old-sig">{{ a.old }}</div><div class="new-sig">→ {{ a.new }}</div></div>
              <div v-else-if="a.new">{{ a.new }}</div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="panel" v-if="result && result.db_schema_changes && result.db_schema_changes.length">
      <h4>数据库表结构变更 <span class="tag">Schema 影响</span></h4>
      <div v-for="(d, i) in result.db_schema_changes" :key="'db'+i" class="dbchg">
        <div class="name chg">
          <span class="status" :class="{ok:d.change_type==='create', err:d.change_type==='drop'}">
            {{ {create:'建表', alter:'改表', drop:'删表', rename:'重命名'}[d.change_type] || d.change_type }}
          </span>
          {{ d.tables.join('、') }}
          <span class="edge">{{ d.file }}</span>
        </div>
        <div class="sum" v-if="d.affected_modules && d.affected_modules.length">
          影响模块：{{ d.affected_modules.join('、') }}
        </div>
      </div>
    </div>

    <div class="panel" v-if="result && result.test_files && result.test_files.length">
      <h4>建议回归测试 <span class="tag">引用受影响函数</span></h4>
      <div v-for="(t, i) in result.test_files" :key="'tf'+i" class="dbchg">
        <div class="name chg">{{ t.file }}</div>
        <div class="sum">命中函数：{{ t.funcs.join('、') }}</div>
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

    <div class="msg info" v-if="analyzing">分析中（git diff 推导变更 → 调用图传播 → LLM 合成设计文档）...</div>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { apiRequest } from '../api'
import { fetchRepos, enabledRepos, defaultRepoId } from '../store/repo'
import CallGraph from '../components/CallGraph.vue'

const repos = ref([])
const form = reactive({ repo_id: 0, direction: 'both', max_depth: 2, branch: '', version: '' })
const analyzing = ref(false)
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
  if (!form.branch.trim()) { error.value = '请输入分支名'; return }
  const payload = {
    repo_id: form.repo_id,
    branch: form.branch.trim(),
    max_depth: Number(form.max_depth) || 2,
    direction: form.direction
  }
  if (form.version.trim()) payload.version = form.version.trim()
  analyzing.value = true
  error.value = ''
  result.value = null
  try {
    const data = await apiRequest('/impact/analyze', { method: 'POST', body: JSON.stringify(payload) })
    result.value = data
  } catch (e) { error.value = '分析失败: ' + e.message }
  finally { analyzing.value = false }
}

onMounted(initRepos)

</script>
