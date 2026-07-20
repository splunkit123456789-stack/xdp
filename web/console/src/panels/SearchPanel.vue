<template>
  <section data-testid="search-page" class="tab-panel">
    <div class="panel-header"><h2><span class="page-icon page-icon-search">SP</span>搜索页</h2><span class="badge">SPL / 时间筛选</span></div>
    <div class="search-layout">
      <div class="search-row">
        <textarea :value="searchQuery" data-testid="search-query" class="search-box" rows="1" aria-label="SPL 搜索语句" placeholder="请输入 SPL语句" @input="setSearchQuery($event.target.value)"></textarea>
        <select :value="searchTime" data-testid="search-time" class="select" @change="setSearchTime($event.target.value)"><option v-for="option in timeOptions" :key="option">{{ option }}</option></select>
        <button data-testid="search-button" class="btn" type="button" @click="runSearchFirstPage">查询</button>
        <button data-testid="save-search" class="btn ghost" type="button" @click="saveSearch">保存</button>
      </div>
      <div data-testid="time-help" class="time-help">高级时间：<code>@d</code> 表示当天 0 点，<code>earliest=@d latest=now</code> 表示今天 0 点到当前时间，<code>-7d@d</code> 表示 7 天前 0 点。</div>
      <div data-testid="timeline-chart" class="timeline timeline-shell" :class="{ empty: !timelineBars.length }">
        <div v-if="timelineBars.length" data-testid="timeline-y-axis" class="timeline-y-axis"><span v-for="label in timelineYAxisLabels" :key="label">{{ label }}</span></div>
        <div class="timeline-plot">
          <div v-if="!timelineBars.length" class="timeline-empty">{{ timelineStatus }}</div>
          <div v-else class="timeline-bars"><div v-for="bucket in timelineBars" :key="bucket.start" data-testid="timeline-bar" class="bar" :title="timelineTooltip(bucket)" :style="{ height: `${bucket.height}%` }"></div></div>
          <div v-if="timelineBars.length" data-testid="timeline-x-axis" class="timeline-x-axis"><span v-for="tick in timelineTicks" :key="tick.key">{{ tick.label }}</span></div>
        </div>
      </div>
      <div class="search-toolbar"><div data-testid="saved-summary" class="saved-summary"><strong>保存搜索</strong><span>仅占一行，点击查看</span><span class="count">{{ savedSearches.length }}</span></div><button data-testid="toggle-saved-searches" class="btn ghost" type="button" @click="toggleSavedSearches">{{ savedOpen ? "收起保存搜索" : "查看保存搜索" }}</button></div>
      <div v-if="savedSearchError" data-testid="saved-search-error" class="field-error">{{ savedSearchError }}</div>
      <div v-if="savedOpen" class="saved-drawer">
        <div class="drawer-head"><span>已保存搜索</span><span class="status-line">查询 / 删除 / 回填</span></div>
        <div class="table-wrap"><table><thead><tr><th>SPL</th><th>时间</th><th>操作</th></tr></thead><tbody><tr v-for="item in savedSearches" :key="item.id" :data-testid="`saved-search-row-${item.id}`"><td><code>{{ item.query }}</code></td><td>{{ item.time }}</td><td><div class="row-actions"><button class="link-btn" type="button" @click="useSearch(item)">回填</button><button class="link-btn delete" type="button" :data-testid="`delete-saved-search-${item.id}`" @click="deleteSavedSearch(item.id)">删除</button></div></td></tr></tbody></table></div>
      </div>
      <article class="card">
        <div class="card-head result-head"><div><span>搜索结果</span><div class="result-meta">{{ resultStatus }}</div></div><span data-testid="result-mode" class="mode-pill">{{ resultMode === "stats" ? "统计视图" : (resultMode === "table" ? "表格视图" : "事件视图") }}</span></div>
        <div data-testid="search-results" class="table-wrap">
          <table class="result-table align-left-table">
            <thead><tr v-if="resultMode === 'stats' || resultMode === 'table'"><th v-for="field in statsFields" :key="field">{{ field }}</th></tr><tr v-else><th class="expand-col"></th><th>时间</th><th>事件</th></tr></thead>
            <tbody>
              <tr v-if="!searchResults.length"><td :colspan="resultMode === 'stats' || resultMode === 'table' ? Math.max(statsFields.length, 1) : 3">暂无匹配结果</td></tr>
              <template v-for="(item, rowIndex) in searchResults" :key="item.id || item.group || rowIndex">
                <tr>
                  <template v-if="resultMode === 'stats' || resultMode === 'table'"><td v-for="field in statsFields" :key="field"><code>{{ formatStatsCell(field, item[field]) }}</code></td></template>
                  <template v-else><td><button :data-testid="`expand-event-${eventRowKey(item, rowIndex)}`" class="expand-toggle" type="button" @click="toggleEventDetail(item, rowIndex)">{{ isEventExpanded(item, rowIndex) ? "▼" : "▶" }}</button></td><td>{{ item.time }}</td><td><code class="multiline-code">{{ item.event }}</code></td></template>
                </tr>
                <tr v-if="resultMode === 'events' && isEventExpanded(item, rowIndex)" class="event-detail-row"><td></td><td colspan="2"><div class="event-detail"><div class="detail-raw"><span>raw</span><code class="multiline-code">{{ item.raw }}</code></div><table><thead><tr><th>字段</th><th>值</th></tr></thead><tbody><tr v-for="(row, detailIndex) in item.detailRows" :key="`${row.name}-${detailIndex}`"><td><code>{{ row.name }}</code></td><td><code>{{ formatDetailValue(row.value) }}</code></td></tr></tbody></table></div></td></tr>
              </template>
            </tbody>
          </table>
        </div>
        <div data-testid="search-pagination" class="pagination-bar"><div data-testid="search-pagination-right" class="pagination-controls"><button data-testid="search-prev" class="pager-arrow" type="button" :disabled="searchPagination.page <= 1 || isSearchLoading" aria-label="上一页" @click="goSearchPage(searchPagination.page - 1)">‹</button><template v-for="item in visibleSearchPages" :key="item.key"><span v-if="item.ellipsis" class="pager-ellipsis">...</span><button v-else :data-testid="`search-page-${item.page}`" class="pager-page" :class="{ active: item.page === searchPagination.page }" type="button" :disabled="isSearchLoading" @click="goSearchPage(item.page)">{{ item.label }}</button></template><button data-testid="search-next" class="pager-arrow" type="button" :disabled="searchPagination.page >= totalSearchPages || isSearchLoading" aria-label="下一页" @click="goSearchPage(searchPagination.page + 1)">›</button><label class="page-size-select"><select :value="searchPageSize" data-testid="search-page-size" class="select compact-select" @change="setSearchPageSize($event.target.value); runSearchFirstPage()"><option v-for="size in searchPageSizes" :key="size" :value="size">{{ size }} 条/页</option></select></label></div></div>
      </article>
    </div>
  </section>
</template>

<script>
import { panelPropNames } from "./panel-props.js";

export default {
  name: "SearchPanel",
  props: panelPropNames
};
</script>
