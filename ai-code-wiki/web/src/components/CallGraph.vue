<template>
  <div class="call-graph">
    <div v-if="!graph || !graph.nodes || !graph.nodes.length" class="empty">暂无调用关系</div>
    <svg ref="svg" :style="{ width: '100%', height: height + 'px' }"></svg>
    <div class="legend">
      <span><i class="dot" style="background:#f59e0b;"></i>直接修改</span>
      <span><i class="dot" style="background:#ef4444;"></i>上游调用方</span>
      <span><i class="dot" style="background:#3b82f6;"></i>下游被调用</span>
      <span><i class="dot" style="background:#22d3ee;"></i>自身</span>
      <span class="hint">拖拽节点 / 滚轮缩放 / 点击节点跳转源码</span>
    </div>
  </div>
</template>

<script setup>
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import * as d3 from 'd3'

const props = defineProps({
  graph: { type: Object, default: null },
  height: { type: Number, default: 420 }
})

const svg = ref(null)
let sim = null

const KIND_COLOR = {
  changed: '#f59e0b',
  reverse: '#ef4444',
  forward: '#3b82f6',
  self: '#22d3ee',
  caller: '#ef4444',
  callee: '#3b82f6'
}

const KIND_LABEL = {
  changed: '修改', reverse: '上游', forward: '下游', self: '自身', caller: '调用方', callee: '被调'
}

async function render() {
  if (!props.graph || !props.graph.nodes || !props.graph.nodes.length) return
  await nextTick()
  if (!svg.value) return
  const el = svg.value
  el.innerHTML = ''

  const width = el.clientWidth || 800
  const height = props.height
  const nodes = props.graph.nodes.map(n => ({ ...n }))
  const links = (props.graph.links || []).map(l => ({ source: l.source, target: l.target }))

  sim = d3.forceSimulation(nodes)
    .force('link', d3.forceLink(links).id(d => d.id).distance(90))
    .force('charge', d3.forceManyBody().strength(-280))
    .force('center', d3.forceCenter(width / 2, height / 2))
    .force('collide', d3.forceCollide(38))

  const zoom = d3.zoom().scaleExtent([0.3, 4]).on('zoom', (ev) => {
    g.attr('transform', ev.transform)
  })

  const svgEl = d3.select(el)
  svgEl.call(zoom)
  const g = svgEl.append('g')

  const defs = svgEl.append('defs')
  defs.append('marker')
    .attr('id', 'arrow')
    .attr('viewBox', '0 -5 10 10')
    .attr('refX', 24).attr('refY', 0)
    .attr('markerWidth', 7).attr('markerHeight', 7)
    .attr('orient', 'auto')
    .append('path')
    .attr('d', 'M0,-5L10,0L0,5')
    .attr('fill', '#64748b')

  const link = g.append('g').selectAll('line')
    .data(links).join('line')
    .attr('stroke', '#64748b').attr('stroke-opacity', 0.6).attr('stroke-width', 1.2)
    .attr('marker-end', 'url(#arrow)')

  const node = g.append('g').selectAll('g')
    .data(nodes).join('g')
    .call(d3.drag()
      .on('start', (ev, d) => { if (!ev.active) sim.alphaTarget(0.3).restart(); d.fx = d.x; d.fy = d.y })
      .on('drag', (ev, d) => { d.fx = ev.x; d.fy = ev.y })
      .on('end', (ev, d) => { if (!ev.active) sim.alphaTarget(0); d.fx = null; d.fy = null })
    )

  node.append('circle')
    .attr('r', d => (d.kind === 'changed' || d.kind === 'self') ? 14 : 11)
    .attr('fill', d => KIND_COLOR[d.kind] || '#94a3b8')
    .attr('fill-opacity', 0.25)
    .attr('stroke', d => KIND_COLOR[d.kind] || '#94a3b8')
    .attr('stroke-width', 1.5)

  node.append('text')
    .attr('dy', 4)
    .attr('text-anchor', 'middle')
    .attr('font-size', 11)
    .attr('font-weight', d => (d.kind === 'changed' || d.kind === 'self') ? 700 : 500)
    .attr('fill', '#e2e8f0')
    .text(d => d.func.length > 14 ? d.func.slice(0, 13) + '…' : d.func)

  node.append('title').text(d => d.module + '.' + d.func + (d.file ? '\n' + d.file : ''))

  // 节点点击跳转源码（有 doc_id 时）
  node.on('click', (ev, d) => {
    if (d.doc_id) {
      const line = ''
      window.open('/doc-source/' + d.doc_id, '_blank')
    }
  })

  sim.on('tick', () => {
    link.attr('x1', d => d.source.x).attr('y1', d => d.source.y)
      .attr('x2', d => d.target.x).attr('y2', d => d.target.y)
    node.attr('transform', d => 'translate(' + d.x + ',' + d.y + ')')
  })
}

watch(() => props.graph, render, { deep: true })
render()

onBeforeUnmount(() => { if (sim) sim.stop() })
</script>

<style scoped>
.call-graph { border: 1px solid var(--line); border-radius: 10px; background: #0a0f1c; overflow: hidden; }
.call-graph .empty { padding: 24px; text-align: center; color: var(--text-dim); font-size: 13px; }
.legend { display: flex; gap: 14px; align-items: center; padding: 8px 12px; font-size: 12px; color: var(--text-dim); border-top: 1px solid var(--line); flex-wrap: wrap; }
.legend .dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; margin-right: 4px; }
.legend .hint { margin-left: auto; color: #51698c; }
</style>