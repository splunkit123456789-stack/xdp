<template>
  <section data-testid="plugins-page" class="tab-panel">
    <div class="panel-header"><h2><span class="page-icon page-icon-plugins">PL</span>插件管理</h2><span class="badge">采集 / 解析 / 搜索插件</span></div>
    <div class="search-layout">
      <article class="card">
        <div class="card-head"><span>插件类型</span><span class="status-line">上传插件包后默认禁用，需后续启用后才参与运行</span></div>
        <div class="plugin-type-tabs" role="tablist" aria-label="插件类型">
          <button v-for="tab in pluginTabs" :key="tab.key" :data-testid="`plugin-tab-${tab.key}`" :class="{ active: currentPluginTab === tab.key }" type="button" @click="selectPluginTab(tab.key)">
            <span>{{ tab.label }}</span>
            <small>{{ pluginTypeCount(tab.key) }}</small>
          </button>
        </div>
        <div v-if="canManageCurrentPluginTab" class="plugin-upload-panel">
          <label class="plugin-file-field"><span>插件包上传</span><span class="plugin-file-control"><input data-testid="plugin-upload-file" class="plugin-file-input" type="file" accept=".zip,application/zip" @change="onPluginFileChange" /><span class="plugin-file-button">选择插件包</span><span data-testid="plugin-upload-filename" class="plugin-file-name">{{ pluginUploadFileName }}</span></span></label>
          <button data-testid="plugin-upload-button" class="btn" type="button" @click="uploadPluginPackage">导入插件包</button>
          <span data-testid="plugin-upload-status" class="status-line">{{ pluginUploadStatus }}</span>
        </div>
        <p v-else data-testid="plugin-manage-forbidden" class="status-line">当前账号没有该插件类型的管理权限。</p>
        <p v-if="pluginUploadError" data-testid="plugin-upload-error" class="field-error form-error">{{ pluginUploadError }}</p>
      </article>

      <article class="card">
        <div class="card-head"><span>{{ currentPluginTabLabel }}</span><span class="status-line">{{ currentPluginPagination.total }} 个插件</span></div>
        <div class="table-wrap">
          <table>
            <thead><tr><th>插件名称</th><th>编码</th><th>版本</th><th>运行时</th><th>状态</th><th>校验值</th><th>操作</th></tr></thead>
            <tbody>
              <tr v-if="!filteredPlugins.length"><td colspan="7">暂无插件</td></tr>
              <template v-for="item in filteredPlugins" :key="`${item.plugin_type}-${item.plugin_code}-${item.plugin_version}`">
                <tr :data-testid="`plugin-row-${item.plugin_code}-${item.plugin_version || '1.0.0'}`">
                  <td>{{ item.name || item.plugin_code }}</td>
                  <td><code>{{ item.plugin_code }}</code></td>
                  <td>{{ item.plugin_version || "1.0.0" }}</td>
                  <td>{{ item.runtime || "go_builtin" }}</td>
                  <td><span class="status-pill" :class="isPluginEnabled(item.status) ? 'runtime-running' : 'runtime-stopped'">{{ pluginStatusLabel(item.status) }}</span></td>
                  <td><code class="muted-code">{{ item.checksum || "builtin" }}</code></td>
                  <td>
                    <span v-if="isBuiltInPlugin(item)" class="status-line">内置基础能力</span>
                    <div v-else class="row-actions plugin-row-actions">
                      <button :data-testid="`plugin-detail-${item.plugin_code}`" class="plugin-action plugin-action-detail" type="button" @click="loadPluginDetail(item)">{{ isPluginDetailOpen(item) ? "收起详情" : "查看详情" }}</button>
                      <button v-if="!isPluginEnabled(item.status)" :data-testid="`plugin-enable-${item.plugin_code}`" class="plugin-action plugin-action-enable" type="button" @click="setPluginStatus(item, 'enable')">启用</button>
                      <button v-if="isPluginEnabled(item.status)" :data-testid="`plugin-disable-${item.plugin_code}`" class="plugin-action plugin-action-disable" type="button" @click="setPluginStatus(item, 'disable')">停用</button>
                      <button v-if="!isPluginEnabled(item.status)" :data-testid="`plugin-delete-${item.plugin_code}`" class="plugin-action plugin-action-delete" type="button" @click="deletePlugin(item)">删除</button>
                    </div>
                  </td>
                </tr>
                <tr v-if="isPluginDetailOpen(item)" :data-testid="`plugin-detail-row-${item.plugin_code}`" class="plugin-detail-inline-row">
                  <td colspan="7">
                    <article data-testid="plugin-detail-panel" class="plugin-detail-inline">
                      <div class="card-head">
                        <span>{{ selectedPlugin.name || selectedPlugin.plugin_code }}</span>
                        <span class="status-line">引用 {{ pluginReferenceCount }} 处</span>
                      </div>
                      <div class="runtime-detail-grid plugin-detail-grid">
                        <div><span>插件编码</span><strong>{{ selectedPlugin.plugin_code }}</strong></div>
                        <div><span>插件类型</span><strong>{{ selectedPlugin.plugin_type }}</strong></div>
                        <div><span>当前版本</span><strong>{{ selectedPlugin.plugin_version }}</strong></div>
                        <div><span>运行时</span><strong>{{ selectedPlugin.runtime || "go_builtin" }}</strong></div>
                        <div><span>状态</span><strong>{{ pluginStatusLabel(selectedPlugin.status) }}</strong></div>
                        <div><span>Checksum</span><strong>{{ selectedPlugin.checksum || "builtin" }}</strong></div>
                        <div v-if="selectedPlugin.entrypoint" class="topology"><span>Entrypoint</span><small>{{ selectedPlugin.entrypoint }}</small></div>
                        <div v-if="pluginEffectiveRuntimeConfig" class="topology"><span>执行限制</span><small>{{ pluginEffectiveRuntimeText }}</small></div>
                        <div v-if="selectedPlugin.description" class="topology"><span>说明</span><small>{{ selectedPlugin.description }}</small></div>
                        <div class="topology"><span>Config Schema</span><small>{{ pluginSchemaText }}</small></div>
                        <div class="topology"><span>UI Schema</span><small>{{ pluginUISchemaText }}</small></div>
                      </div>
                      <div v-if="selectedPlugin.plugin_type !== 'search_command'" class="table-wrap plugin-reference-table">
                        <table>
                          <thead><tr><th>引用类型</th><th>引用对象</th><th>状态</th></tr></thead>
                          <tbody>
                            <tr v-if="!pluginReferenceItems.length"><td colspan="3">暂无引用</td></tr>
                            <tr v-for="ref in pluginReferenceItems" :key="`${ref.type}-${ref.id || ref.name}`">
                              <td>{{ referenceTypeLabel(ref.type) }}</td>
                              <td>{{ ref.name || ref.id || "-" }}</td>
                              <td>{{ ref.status || "-" }}</td>
                            </tr>
                          </tbody>
                        </table>
                      </div>
                      <div v-if="showPluginExecutionAudits" data-testid="plugin-execution-audits" class="table-wrap plugin-reference-table">
                        <div class="card-head"><span>最近执行记录</span><span class="status-line">{{ pluginExecutionAudits.length }} 条</span></div>
                        <table>
                          <thead><tr><th>执行时间</th><th>命令</th><th>耗时</th><th>输入事件数</th><th>输出事件数</th><th>状态</th><th>错误码</th></tr></thead>
                          <tbody>
                            <tr v-if="pluginExecutionAuditLoading"><td colspan="7">执行审计加载中...</td></tr>
                            <tr v-else-if="!pluginExecutionAudits.length"><td colspan="7">暂无执行记录</td></tr>
                            <tr v-for="audit in pluginExecutionAudits" :key="`${audit.request_id}-${audit.command_name}-${audit.created_at}`">
                              <td>{{ formatFullTime(audit.created_at) }}</td>
                              <td><code>{{ audit.command_name || "-" }}</code></td>
                              <td>{{ audit.elapsed_ms ?? 0 }}ms</td>
                              <td>{{ formatRuntimeNumber(audit.input_rows) }} 条</td>
                              <td>{{ formatRuntimeNumber(audit.output_rows) }} 条</td>
                              <td><span class="status-pill" :class="audit.success ? 'runtime-running' : 'runtime-stopped'">{{ audit.success ? "成功" : "失败" }}</span></td>
                              <td><code class="muted-code">{{ audit.error_code || "-" }}</code></td>
                            </tr>
                          </tbody>
                        </table>
                        <p v-if="pluginExecutionAuditError" class="field-error form-error">{{ pluginExecutionAuditError }}</p>
                      </div>
                      <div class="plugin-current-actions">
                        <span v-if="isPluginReferenced(selectedPlugin)" class="status-line">被引用，不能停用或删除</span>
                        <span v-else class="status-line">启停与删除操作可直接在插件行操作列执行</span>
                      </div>
                      <p v-if="pluginActionStatus" class="status-line">{{ pluginActionStatus }}</p>
                      <p v-if="pluginActionError" class="field-error form-error">{{ pluginActionError }}</p>
                    </article>
                  </td>
                </tr>
              </template>
            </tbody>
          </table>
        </div>
        <div data-testid="plugin-pagination" class="pagination-bar">
          <div class="pagination-controls">
            <button data-testid="plugin-prev" class="pager-arrow" type="button" :disabled="currentPluginPagination.page <= 1" aria-label="上一页" @click="goPluginPage(currentPluginPagination.page - 1)">‹</button>
            <template v-for="item in visiblePluginPages" :key="item.key">
              <span v-if="item.ellipsis" class="pager-ellipsis">...</span>
              <button v-else :data-testid="`plugin-page-${item.page}`" class="pager-page" :class="{ active: item.page === currentPluginPagination.page }" type="button" @click="goPluginPage(item.page)">{{ item.label }}</button>
            </template>
            <button data-testid="plugin-next" class="pager-arrow" type="button" :disabled="currentPluginPagination.page >= totalPluginPages" aria-label="下一页" @click="goPluginPage(currentPluginPagination.page + 1)">›</button>
            <label class="page-size-select"><select :value="currentPluginPageSize" data-testid="plugin-page-size" class="select compact-select" @change="setCurrentPluginPageSize($event.target.value); reloadPluginFirstPage()"><option v-for="size in listPageSizes" :key="size" :value="size">{{ size }} 条/页</option></select></label>
          </div>
        </div>
      </article>
    </div>
  </section>
</template>

<script>
import { panelPropNames } from "./panel-props.js";

export default {
  name: "PluginsPanel",
  props: panelPropNames
};
</script>
