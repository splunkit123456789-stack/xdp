<template>
  <section data-testid="collect-page" class="tab-panel">
    <div class="panel-header"><h2><span class="page-icon page-icon-collect">IN</span>采集配置</h2><div class="panel-header-actions"><span class="badge">{{ inputPluginBadge }}</span><button data-testid="show-input-form" class="btn" type="button" @click="openInputForm">新增采集</button></div></div>
    <p v-if="inputFormNotice" data-testid="input-form-notice" class="status-line">{{ inputFormNotice }}</p>
    <div v-if="pluginCatalogErrors.input" data-testid="input-plugin-catalog-error" class="catalog-load-error">
      <span>{{ pluginCatalogErrors.input }}</span>
      <button data-testid="retry-input-plugin-catalog" class="btn ghost" type="button" :disabled="pluginCatalogLoading.input" @click="retryInputPluginCatalog">
        {{ pluginCatalogLoading.input ? "加载中" : "重试" }}
      </button>
    </div>
    <div class="content-grid" :class="{ 'list-first': !showInputForm }">
      <article v-if="showInputForm" data-testid="input-form-card" class="card config-drawer" aria-label="采集配置表单">
        <div class="card-head"><span>{{ editingInputId ? "修改采集" : "新增采集" }}</span><button class="btn ghost" type="button" @click="clearInputForm">清空</button></div>
        <form class="form-grid" @submit.prevent="saveInput">
          <label>设备名称<input v-model="inputForm.name" class="field" required placeholder="请输入设备名称" /><span v-if="inputNameError" data-testid="input-name-error" class="field-error">{{ inputNameError }}</span></label>
          <label>状态<select v-model="inputForm.status" class="select" required><option>active</option><option>disabled</option></select></label>
          <div class="plugin-grid">
            <button v-if="canUseSyslogInput" data-testid="input-plugin-syslog" :disabled="Boolean(editingInputId)" :class="{ active: inputForm.plugin === 'Syslog', locked: Boolean(editingInputId) }" class="plugin-card" type="button" @click="selectInputPlugin('Syslog')"><span class="plugin-icon icon-syslog">SYS</span>Syslog</button>
            <button v-if="kafkaInputPlugin" data-testid="input-plugin-kafka" :disabled="Boolean(editingInputId)" :class="{ active: inputForm.plugin === 'Kafka', locked: Boolean(editingInputId) }" class="plugin-card" type="button" @click="selectInputPlugin('Kafka')"><span class="plugin-icon icon-kafka">K</span>Kafka</button>
          </div>

          <div v-if="inputForm.plugin === 'Syslog'" class="param-panel">
            <div class="two">
              <label>监听端口<input v-model="inputForm.collectorPort" class="field" required placeholder="5514" /><span v-if="inputPortError" data-testid="collector-port-error" class="field-error">{{ inputPortError }}</span></label>
              <label>日志筛选<select v-model="inputForm.logFilterEnabled" class="select" required><option value="off">关闭</option><option value="on">开启</option></select></label>
            </div>
            <div class="two">
              <label>传输层协议<select v-model="inputForm.transportProtocol" class="select" required><option>UDP</option></select></label>
              <label>字符编码<select v-model="inputForm.encoding" class="select" required><option>UTF-8</option><option>GBK</option><option>ISO-8859-1</option></select></label>
            </div>
            <div v-if="inputForm.logFilterEnabled === 'on'" class="conditional-panel">
              <label>正则筛选<input v-model="inputForm.logFilterRegex" class="field" required placeholder="^allow|^accept" /></label>
              <div class="note">开启后，仅符合正则筛选条件的日志会进入解析与存储流程。</div>
            </div>
          </div>

          <div v-else class="param-panel">
            <div data-testid="kafka-runtime-enabled" class="note">Kafka 采集插件为 P1 运行时能力，保存后由 Agent 按数据源状态热加载消费。</div>
            <div class="two">
              <template v-for="field in kafkaFormFields" :key="field.name">
                <label v-if="isKafkaFieldVisible(field)">{{ field.label }}
                  <select v-if="field.kind === 'select'" v-model="inputForm[field.model]" :data-testid="field.testid" class="select" required>
                    <option v-for="option in field.options" :key="option.value" :value="option.value">{{ option.label }}</option>
                  </select>
                  <input v-else v-model="inputForm[field.model]" :data-testid="field.testid" class="field" required :placeholder="field.placeholder" />
                </label>
              </template>
            </div>
            <div v-if="inputForm.logFilterEnabledKafka === 'on'" class="note">开启后，仅符合正则筛选条件的消息会进入解析与存储流程。</div>
            <div class="actions"><button data-testid="kafka-connectivity-check" class="btn ghost" type="button" @click="checkKafkaConnectivity">测试连通性</button></div>
            <p v-if="kafkaConnectivityStatus" data-testid="kafka-connectivity-status" class="status-line">{{ kafkaConnectivityStatus }}</p>
          </div>

          <div class="form-hint">默认写入策略由系统内部处理，采集页不展示索引配置；解析配置仅通过采集源名称关联。</div>
          <p v-if="inputFormError" data-testid="input-form-error" class="field-error form-error">{{ inputFormError }}</p>
          <div class="actions"><button class="btn" type="submit">{{ editingInputId ? "保存修改" : "新增" }}</button><button data-testid="cancel-input-form" class="btn ghost" type="button" @click="resetInputForm">取消</button></div>
        </form>
      </article>

      <article class="card">
        <div class="card-head"><span>采集列表</span><span class="status-line">点击行查看运行详情</span></div>
        <div class="table-wrap">
          <table class="align-left-table collect-table">
            <thead><tr><th></th><th>名称</th><th>采集插件</th><th>监听端口</th><th>运行状态</th><th>操作</th></tr></thead>
            <tbody>
              <template v-for="item in inputConfigs" :key="item.id">
                <tr
                  :data-testid="`collect-row-${item.id}`"
                  class="collect-runtime-row"
                  :class="{ selected: selectedRuntimeId === item.id, abnormal: collectRuntimeSummary(item).state === 'error' }"
                  @click="selectRuntimeSource(item)"
                >
                  <td class="expand-cell"><button class="expand-toggle" :data-testid="`collect-expand-${item.id}`" type="button" :aria-expanded="selectedRuntimeId === item.id" @click.stop="selectRuntimeSource(item)">{{ selectedRuntimeId === item.id ? "▼" : "▶" }}</button></td>
                  <td>{{ item.name }}</td>
                  <td>{{ item.plugin }}</td>
                  <td>{{ collectListenerPortLabel(item) }}</td>
                  <td><span class="status-pill" :class="`runtime-${collectRuntimeSummary(item).state}`">{{ collectRuntimeSummary(item).label }}</span></td>
                  <td>
                    <div class="row-actions">
                      <button v-if="collectCanStop(item)" class="link-btn" :data-testid="`toggle-input-${item.id}`" type="button" @click.stop="toggleInputStatus(item)">停止</button>
                      <button v-else-if="collectCanStart(item)" class="link-btn" :data-testid="`toggle-input-${item.id}`" type="button" @click.stop="toggleInputStatus(item)">启动</button>
                      <template v-else>
                        <button class="link-btn" :data-testid="`view-runtime-${item.id}`" type="button" @click.stop="loadRuntimeDetail(item)">查看状态</button>
                        <button class="link-btn" :data-testid="`retry-runtime-${item.id}`" type="button" @click.stop="retryRuntimeSource(item)">重试</button>
                      </template>
                      <button class="link-btn" type="button" @click.stop="editInput(item)">修改</button>
                      <button class="link-btn delete" type="button" @click.stop="deleteInput(item.id)">删除</button>
                    </div>
                  </td>
                </tr>
                <tr v-if="selectedRuntimeId === item.id" class="collect-runtime-detail-row">
                  <td colspan="6">
                    <section data-testid="collect-runtime-detail" class="runtime-detail-card">
                      <div :data-testid="`collect-runtime-detail-${item.id}`">
                        <div class="runtime-detail-head">
                          <div><strong>运行详情</strong><span>{{ runtimeDetail?.name || selectedRuntimeName }}</span></div>
                          <span class="status-pill" :class="`runtime-${runtimeDetailSummary.state}`">{{ runtimeDetailSummary.label }}</span>
                        </div>
                        <p v-if="runtimeLoading" class="status-line">正在加载运行状态...</p>
                        <p v-if="runtimeError" data-testid="collect-runtime-error" class="field-error form-error">{{ runtimeError }}</p>
                        <div v-if="runtimeDetail" class="runtime-detail-grid">
                          <div><span>Agent 心跳</span><strong>{{ runtimeDetail.agent_id || "local-agent" }}</strong><small>{{ formatRuntimeValue(runtimeDetail.last_heartbeat_at) }}</small></div>
                          <div><span>listener 状态</span><strong>{{ runtimeDetail.runtime_status || "unknown" }} / {{ runtimeDetail.listener_status || "unknown" }}</strong><small>{{ runtimeDetail.endpoint || "未监听" }}</small></div>
                          <div><span>累计接收事件数</span><strong>{{ formatRuntimeNumber(runtimeDetail.received_events_total) }}</strong><small>最近接收时间 {{ formatRuntimeValue(runtimeDetail.last_received_at) }}</small></div>
                          <div><span>累计字节数</span><strong>{{ formatRuntimeNumber(runtimeDetail.received_bytes_total) }}</strong><small>原始日志接收字节</small></div>
                          <div><span>最近加载时间</span><strong>{{ formatRuntimeValue(runtimeDetail.last_loaded_at) }}</strong><small>配置版本 {{ runtimeDetail.config_version || 1 }}</small></div>
                          <div><span>最近错误</span><strong>{{ runtimeDetail.last_error_code || "无" }}</strong><small>{{ runtimeDetail.last_error || "暂无错误" }}</small></div>
                          <div class="topology"><span>链路拓扑</span><strong>{{ runtimeTopology(runtimeDetail) }}</strong><small>Agent -> Listener -> 解析规则 -> Index</small></div>
                        </div>
                      </div>
                    </section>
                  </td>
                </tr>
              </template>
            </tbody>
          </table>
        </div>
        <div data-testid="collect-pagination" class="pagination-bar">
          <div class="pagination-controls">
            <button data-testid="collect-prev" class="pager-arrow" type="button" :disabled="collectPagination.page <= 1" aria-label="上一页" @click="goCollectPage(collectPagination.page - 1)">‹</button>
            <template v-for="item in visibleCollectPages" :key="item.key">
              <span v-if="item.ellipsis" class="pager-ellipsis">...</span>
              <button v-else :data-testid="`collect-page-${item.page}`" class="pager-page" :class="{ active: item.page === collectPagination.page }" type="button" @click="goCollectPage(item.page)">{{ item.label }}</button>
            </template>
            <button data-testid="collect-next" class="pager-arrow" type="button" :disabled="collectPagination.page >= totalCollectPages" aria-label="下一页" @click="goCollectPage(collectPagination.page + 1)">›</button>
            <label class="page-size-select"><select :value="collectPageSize" data-testid="collect-page-size" class="select compact-select" @change="setCollectPageSize($event.target.value); reloadCollectFirstPage()"><option v-for="size in listPageSizes" :key="size" :value="size">{{ size }} 条/页</option></select></label>
          </div>
        </div>
      </article>
    </div>
  </section>
</template>

<script>
import { panelPropNames } from "./panel-props.js";

export default {
  name: "CollectPanel",
  props: panelPropNames
};
</script>
