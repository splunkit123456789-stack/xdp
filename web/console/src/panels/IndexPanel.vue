<template>
  <section data-testid="index-page" class="tab-panel">
    <div class="panel-header"><h2><span class="page-icon page-icon-index">IX</span>索引配置</h2><div class="panel-header-actions"><span class="badge">ClickHouse 物理分表</span><button data-testid="show-index-form" class="btn" type="button" @click="openIndexForm">新增 index</button></div></div>
    <div class="content-grid" :class="{ 'list-first': !showIndexForm }">
      <article data-testid="writer-runtime-panel" class="card writer-runtime-card"><div class="card-head"><span>Writer 入库状态</span><button class="btn ghost" type="button" @click="loadWriterRuntime(true)">刷新</button></div><div v-if="writerRuntimeLoading" class="status-line">Writer 状态加载中...</div><p v-else-if="writerRuntimeError" class="field-error">{{ writerRuntimeError }}</p><div v-else class="writer-runtime-grid"><div><span>状态</span><strong>{{ writerRuntime.status || "unknown" }}</strong><small>{{ writerRuntime.output_topic || "未连接" }}</small></div><div><span>吞吐</span><strong>{{ formatWriterEPS(writerRuntime.eps) }} EPS</strong><small>累计 {{ formatNumber(writerRuntime.total_events || 0) }} 条</small></div><div><span>P95 入库延迟</span><strong>P95 {{ formatNumber(writerRuntime.p95_ingest_latency_ms || 0) }}ms</strong><small>最近批次 {{ formatNumber(writerRuntime.last_duration_ms || 0) }}ms</small></div><div><span>失败 / Deadletter</span><strong>{{ formatPercent(writerRuntime.failure_rate || 0) }}</strong><small>失败 {{ formatNumber(writerRuntime.failed_events || 0) }} 条 · DLQ {{ formatNumber(writerRuntime.deadletter_events || 0) }} 条</small></div><div><span>批量策略</span><strong>{{ formatNumber(writerRuntime.batch_size || 0) }} 条/批</strong><small>批次 {{ formatNumber(writerRuntime.total_batches || 0) }} · 重试 {{ formatNumber(writerRuntime.last_retry_count || 0) }}</small></div></div></article>
      <article v-if="showIndexForm" data-testid="index-form-card" class="card config-drawer" aria-label="索引配置表单"><div class="card-head"><span>{{ editingIndexId ? "修改索引" : "新增索引" }}</span><button class="btn ghost" type="button" @click="clearIndexForm">清空</button></div><form class="form-grid" @submit.prevent="saveIndex"><label>index 名称<input v-model="indexForm.name" data-testid="index-name" class="field" required :disabled="Boolean(editingIndexId)" placeholder="请输入index名称" /></label><div class="two"><label>TTL 天数<input v-model="indexForm.ttl" data-testid="index-ttl" class="field" min="1" required type="number" /></label><label>状态<select v-model="indexForm.status" data-testid="index-status" class="select" required><option>active</option><option>disabled</option></select></label></div><p v-if="indexFormError" data-testid="index-form-error" class="field-error form-error">{{ indexFormError }}</p><div class="actions"><button class="btn" type="submit">{{ editingIndexId ? "保存修改" : "新增" }}</button><button data-testid="cancel-index-form" class="btn ghost" type="button" @click="resetIndexForm">取消</button></div></form></article>
      <article class="card">
        <div class="card-head"><span>索引列表</span><span class="status-line">查询 / 修改 / 删除 / 趋势</span></div>
        <div class="table-wrap">
          <table>
            <thead><tr><th>index</th><th>物理表</th><th>TTL</th><th>数据量</th><th>存储大小</th><th>最近写入</th><th>状态</th><th>操作</th></tr></thead>
            <tbody>
              <template v-for="item in indexes" :key="item.id">
                <tr>
                  <td><code>{{ item.name }}</code></td>
                  <td><code>{{ item.tableName || `events_${item.name}` }}</code></td>
                  <td><span>{{ item.ttl }}d</span><br /><small v-if="item.physicalTtl" class="status-line">物理 {{ item.physicalTtl }}d</small></td>
                  <td>{{ formatNumber(item.rows) }}</td>
                  <td>{{ formatBytes(item.storageBytes) }}</td>
                  <td>{{ formatIndexDateTime(item.latestEventTime) }}</td>
                  <td>{{ item.status }}</td>
                  <td>
                    <div class="row-actions">
                      <button class="link-btn" type="button" @click="loadIndexTrend(item)">{{ item.trendOpen ? "收起趋势" : "趋势" }}</button>
                      <button class="link-btn" type="button" @click="editIndex(item)">修改</button>
                      <button class="link-btn delete" type="button" @click="deleteIndex(item.id)">删除</button>
                    </div>
                  </td>
                </tr>
                <tr v-if="item.trendOpen" class="index-trend-row">
                  <td colspan="8">
                    <div data-testid="index-trend-panel" class="index-trend-panel">
                      <p v-if="item.trendLoading" class="status-line">趋势加载中...</p>
                      <p v-else-if="item.trendError" class="field-error">{{ item.trendError }}</p>
                      <div v-else>
                        <div class="index-trend-summary">
                          <span>近 7 天净增 {{ formatNumber(item.trend?.rows_growth_7d || 0) }} 条</span>
                          <span>存储变化 {{ formatBytes(Math.abs(item.trend?.storage_growth_bytes_7d || 0)) }}</span>
                          <span>当前 {{ formatNumber(item.trend?.current_rows || item.rows) }} 条 / {{ formatBytes(item.trend?.current_storage_bytes || item.storageBytes) }}</span>
                          <span>{{ item.trend?.source === "snapshot" ? "采样数据" : "实时数据" }}</span>
                          <span v-if="item.trend?.snapshot_retention_days">保留 {{ item.trend.snapshot_retention_days }} 天</span>
                        </div>
                        <div class="index-trend-chart">
                          <div class="index-trend-plot">
                            <div data-testid="index-trend-y-axis" class="index-trend-y-axis">
                              <span v-for="tick in indexTrendYTicks(item)" :key="tick.key">{{ tick.label }}</span>
                            </div>
                            <div class="index-trend-main">
                              <div class="index-trend-bars">
                                <span v-for="(point, pointIndex) in (item.trend?.points || [])" :key="`${point.date || point.captured_at || pointIndex}-${pointIndex}`" :title="`${indexTrendPointLabel(point)}: ${formatNumber(point.rows)} 条`" :style="{ height: `${indexTrendBarHeight(item, point)}%` }"></span>
                              </div>
                              <div data-testid="index-trend-x-axis" class="index-trend-x-axis">
                                <span v-for="tick in indexTrendTicks(item)" :key="tick.key">{{ tick.label }}</span>
                              </div>
                            </div>
                          </div>
                        </div>
                      </div>
                    </div>
                  </td>
                </tr>
              </template>
            </tbody>
          </table>
        </div>
        <div data-testid="index-pagination" class="pagination-bar">
          <div class="pagination-controls">
            <button data-testid="index-prev" class="pager-arrow" type="button" :disabled="indexPagination.page <= 1" aria-label="上一页" @click="goIndexPage(indexPagination.page - 1)">‹</button>
            <template v-for="item in visibleIndexPages" :key="item.key">
              <span v-if="item.ellipsis" class="pager-ellipsis">...</span>
              <button v-else :data-testid="`index-page-${item.page}`" class="pager-page" :class="{ active: item.page === indexPagination.page }" type="button" @click="goIndexPage(item.page)">{{ item.label }}</button>
            </template>
            <button data-testid="index-next" class="pager-arrow" type="button" :disabled="indexPagination.page >= totalIndexPages" aria-label="下一页" @click="goIndexPage(indexPagination.page + 1)">›</button>
            <label class="page-size-select"><select :value="indexPageSize" data-testid="index-page-size" class="select compact-select" @change="setIndexPageSize($event.target.value); reloadIndexFirstPage()"><option v-for="size in listPageSizes" :key="size" :value="size">{{ size }} 条/页</option></select></label>
          </div>
        </div>
      </article>
    </div>
  </section>
</template>

<script>
import { panelPropNames } from "./panel-props.js";

export default {
  name: "IndexPanel",
  props: panelPropNames
};
</script>
