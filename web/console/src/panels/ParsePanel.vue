<template>
  <section data-testid="parse-page" class="tab-panel">
    <div class="panel-header"><h2><span class="page-icon page-icon-parse">PX</span>解析配置</h2><div class="panel-header-actions"><span class="badge">props.conf / 解析插件</span><button data-testid="show-rule-form" class="btn" type="button" @click="openRuleForm">新增解析规则</button></div></div>
    <div v-if="pluginCatalogErrors.parser" data-testid="parser-plugin-catalog-error" class="catalog-load-error">
      <span>{{ pluginCatalogErrors.parser }}</span>
      <button data-testid="retry-parser-plugin-catalog" class="btn ghost" type="button" :disabled="pluginCatalogLoading.parser" @click="retryParserPluginCatalog">
        {{ pluginCatalogLoading.parser ? "加载中" : "重试" }}
      </button>
    </div>
    <div class="content-grid list-first">
      <article v-if="showRuleForm" data-testid="rule-form-card" class="card config-drawer" aria-label="解析配置表单">
        <div class="card-head"><span>{{ editingRuleId ? "修改规则" : "新增规则" }}</span><button class="btn ghost" type="button" @click="clearRuleForm">清空</button></div>
        <form class="form-grid" @submit.prevent="saveRule">
          <label>规则名称<input v-model="ruleForm.name" data-testid="rule-name" class="field" required placeholder="请输入解析规则名称" /></label>
          <div class="two">
            <label>关联采集数据源名称<select v-model="ruleForm.dataSourceName" data-testid="rule-source" class="select" required @change="applyDataSourceRoute"><option value="">请选择采集数据源</option><option v-for="item in inputConfigs" :key="item.id" :value="item.name">{{ item.name }}</option></select></label>
            <label>写入 index<select v-model="ruleForm.outputIndex" data-testid="rule-output-index" class="select" required><option v-for="item in businessIndexes" :key="item.id" :value="item.name">{{ item.name }}</option></select></label>
          </div>
          <label>优先级<input v-model.number="ruleForm.priority" data-testid="rule-priority" class="field" min="1" required type="number" placeholder="100" /></label>
          <div>
            <span class="form-hint">解析方式</span>
            <div class="plugin-grid parser-plugin-grid">
              <button
                v-for="plugin in parserPluginOptions"
                :key="plugin.plugin_code"
                :data-testid="`parser-${plugin.plugin_code}`"
                :class="{ active: ruleForm.pluginCode === plugin.plugin_code }"
                class="plugin-card"
                type="button"
                @click="selectParserPlugin(plugin)"
              >
                <span class="plugin-icon" :class="plugin.iconClass">{{ plugin.icon }}</span>{{ plugin.label }}
              </button>
            </div>
          </div>
          <label>日志样例<textarea v-model="ruleForm.sampleLog" data-testid="sample-log" required placeholder="请输入日志样例"></textarea></label>
          <div v-if="ruleForm.pluginCode === 'regex'" class="param-panel"><label>正则表达式<textarea v-model="ruleForm.regexPattern" data-testid="regex-pattern" required placeholder="src=(?<src_ip>\\S+)\\s+dst=(?<dst_ip>\\S+)\\s+action=(?<action>\\S+)"></textarea></label></div>
          <div class="actions"><button data-testid="preview-parse" class="btn" type="button" @click="previewParse">预览解析结果</button></div>
          <div data-testid="parse-preview" class="preview-box table-wrap"><table><thead><tr><th>序号</th><th>字段</th><th>值</th><th>字段类型</th></tr></thead><tbody><tr v-if="!previewRows.length"><td colspan="4">暂无解析结果</td></tr><tr v-for="(row, index) in previewRows" :key="`${row.field}-${index}`"><td>{{ index + 1 }}</td><td><code>{{ row.field }}</code></td><td>{{ row.value }}</td><td>{{ row.type }}</td></tr></tbody></table></div>
          <details class="advanced-panel" open><summary>高级配置 / props.conf</summary><label>最终 props.conf 配置<textarea v-model="ruleForm.propsConf" data-testid="props-conf" class="props-editor" required placeholder="[source::firewall]&#10;TIME_PREFIX = event_time="></textarea></label></details>

          <p v-if="ruleFormError" data-testid="parse-form-error" class="field-error form-error">{{ ruleFormError }}</p>
          <div class="actions"><button class="btn" type="submit">{{ editingRuleId ? "保存修改" : "新增" }}</button><button data-testid="cancel-rule-form" class="btn ghost" type="button" @click="resetRuleForm">取消</button></div>
        </form>
      </article>
      <article class="card"><div class="card-head"><span>规则列表</span><span class="status-line">查询 / 修改 / 删除</span></div><div class="table-wrap"><table><thead><tr><th>名称</th><th>解析插件</th><th>采集数据源</th><th>写入 index</th><th>优先级</th><th>props.conf</th><th>操作</th></tr></thead><tbody><tr v-for="item in parseRules" :key="item.id"><td>{{ item.name }}</td><td>{{ item.plugin }}</td><td><code>{{ item.dataSourceName || "未选择" }}</code></td><td><code>{{ item.outputIndex || "app" }}</code></td><td><code>{{ item.priority || 100 }}</code></td><td><code class="multiline-code">{{ item.propsConf }}</code></td><td><div class="row-actions"><button class="link-btn" type="button" @click="editRule(item)">修改</button><button class="link-btn delete" type="button" @click="deleteRule(item.id)">删除</button></div></td></tr></tbody></table></div><div data-testid="parse-pagination" class="pagination-bar"><div class="pagination-controls"><button data-testid="parse-prev" class="pager-arrow" type="button" :disabled="parsePagination.page <= 1" aria-label="上一页" @click="goParsePage(parsePagination.page - 1)">‹</button><template v-for="item in visibleParsePages" :key="item.key"><span v-if="item.ellipsis" class="pager-ellipsis">...</span><button v-else :data-testid="`parse-page-${item.page}`" class="pager-page" :class="{ active: item.page === parsePagination.page }" type="button" @click="goParsePage(item.page)">{{ item.label }}</button></template><button data-testid="parse-next" class="pager-arrow" type="button" :disabled="parsePagination.page >= totalParsePages" aria-label="下一页" @click="goParsePage(parsePagination.page + 1)">›</button><label class="page-size-select"><select :value="parsePageSize" data-testid="parse-page-size" class="select compact-select" @change="setParsePageSize($event.target.value); reloadParseFirstPage()"><option v-for="size in listPageSizes" :key="size" :value="size">{{ size }} 条/页</option></select></label></div></div></article>
    </div>
  </section>
</template>

<script>
import { panelPropNames } from "./panel-props.js";

export default {
  name: "ParsePanel",
  props: panelPropNames
};
</script>
