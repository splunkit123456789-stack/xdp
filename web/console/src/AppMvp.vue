<template>
  <main v-if="screen === 'login'" data-testid="login-page" data-theme="ops-login" class="login-shell">
    <div class="page-grid" aria-hidden="true"></div>
    <header class="topbar">
      <div class="brand"><span class="brand-mark">X</span><span>XDP&gt;Console</span></div>
      <span class="pill">AUTH GATEWAY</span>
      <span class="pill muted">MVP ACCESS</span>
    </header>

    <section class="login-layout">
      <section class="hero-card" aria-label="XDP 登录入口">
        <p class="eyebrow">SECURE DATA PLATFORM</p>
        <h1><span class="gradient-text">XDP</span><strong>可信数据入口</strong></h1>
        <p class="lede">采集、解析、索引与搜索统一入口，登录后进入 XDP 控制台。</p>
        <div class="chip-row"><span>Syslog Ingest</span><span>props.conf Parser</span><span>SPL Search</span></div>
      </section>

      <section class="login-card">
        <div class="card-head">
          <div><p class="eyebrow">SIGN IN</p><h2>登录控制台</h2></div>
          <span class="status-dot" aria-label="服务可用"></span>
        </div>
        <form class="login-form" @submit.prevent="submitLogin">
          <label>用户名<input v-model="credentials.username" name="username" autocomplete="username" placeholder="请输入用户名" required /></label>
          <label>密码<input v-model="credentials.password" name="password" autocomplete="current-password" placeholder="请输入密码" type="password" required /></label>
          <button type="submit">登录</button>
        </form>
        <p v-if="loginError" data-testid="login-error" class="error-box">{{ loginError }}</p>
        <button v-if="auth.enabled === false" class="btn ghost" type="button" @click="enterConsole">开发模式进入</button>
      </section>
    </section>
    <footer>© 2026 XDP Console</footer>
  </main>

  <main v-else data-testid="console-shell" data-theme="ops-console" class="console-shell">
    <div class="page-grid" aria-hidden="true"></div>
    <section class="console-page">
      <header class="topbar console-topbar">
        <div class="brand"><span class="brand-mark console-brand-mark">X</span><span>XDP&gt;Console</span></div>
        <nav data-testid="main-nav" class="topbar-nav" aria-label="主模块导航">
          <button v-for="item in modules" :key="item.key" :class="{ active: currentModule === item.key }" :data-testid="`nav-${item.key}`" type="button" @click="selectModule(item.key)">{{ item.label }}</button>
        </nav>
        <div class="user">Administrator</div>
        <button data-testid="logout" class="logout" type="button" @click="logout">退出</button>
      </header>

      <section class="workspace">
        <section class="main-panel">
          <section v-if="currentModule === 'collect'" data-testid="collect-page" class="tab-panel">
            <div class="panel-header"><h2><span class="page-icon page-icon-collect">IN</span>采集配置</h2><div class="panel-header-actions"><span class="badge">Syslog / Kafka</span><button data-testid="show-input-form" class="btn" type="button" @click="openInputForm">新增采集</button></div></div>
            <div class="content-grid" :class="{ 'list-first': !showInputForm }">
              <article v-if="showInputForm" data-testid="input-form-card" class="card config-drawer" aria-label="采集配置表单">
                <div class="card-head"><span>{{ editingInputId ? "修改采集" : "新增采集" }}</span><button class="btn ghost" type="button" @click="clearInputForm">清空</button></div>
                <form class="form-grid" @submit.prevent="saveInput">
                  <label>设备名称<input v-model="inputForm.name" class="field" required placeholder="请输入设备名称" /><span v-if="inputNameError" data-testid="input-name-error" class="field-error">{{ inputNameError }}</span></label>
                  <label>状态<select v-model="inputForm.status" class="select" required><option>active</option><option>disabled</option></select></label>
                  <div class="plugin-grid">
                    <button data-testid="input-plugin-syslog" :class="{ active: inputForm.plugin === 'Syslog' }" class="plugin-card" type="button" @click="inputForm.plugin = 'Syslog'"><span class="plugin-icon icon-syslog">SYS</span>Syslog</button>
                    <button data-testid="input-plugin-kafka" :class="{ active: inputForm.plugin === 'Kafka' }" class="plugin-card" type="button" @click="inputForm.plugin = 'Kafka'"><span class="plugin-icon icon-kafka">K</span>Kafka</button>
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
                    <div data-testid="kafka-runtime-disabled" class="note">Kafka 采集插件为 P1 能力，当前仅展示配置形态，暂不支持保存运行时采集。</div>
                    <label>传输层协议<select v-model="inputForm.transportProtocolKafka" class="select"><option>TCP</option></select></label>
                    <label>Broker 地址<input v-model="inputForm.brokers" class="field" placeholder="10.0.0.1:9092,10.0.0.2:9092" /></label>
                    <div class="two"><label>Topic<input v-model="inputForm.topic" class="field" placeholder="xdp-events" /></label><label>消费组<input v-model="inputForm.consumerGroup" class="field" placeholder="xdp-consumer" /></label></div>
                    <div class="two">
                      <label>通信协议<select v-model="inputForm.securityProtocol" class="select"><option>PLAINTEXT</option><option>SASL_PLAINTEXT</option><option>SASL_SSL</option><option>SSL</option></select></label>
                      <label>消费策略<select v-model="inputForm.consumeStrategy" class="select"><option>最早</option><option>最新</option></select></label>
                    </div>
                    <div class="two"><label>字符编码<select v-model="inputForm.encodingKafka" class="select"><option>UTF-8</option><option>GBK</option><option>ISO-8859-1</option></select></label><label>日志筛选<select v-model="inputForm.logFilterEnabledKafka" class="select"><option value="off">关闭</option><option value="on">开启</option></select></label></div>
                    <div v-if="inputForm.logFilterEnabledKafka === 'on'" class="conditional-panel"><label>正则筛选<input v-model="inputForm.logFilterRegexKafka" class="field" placeholder="^allow|^accept" /></label><div class="note">开启后，仅符合正则筛选条件的消息会进入解析与存储流程。</div></div>
                    <div class="actions"><button class="btn ghost" type="button">测试连通性</button></div>
                  </div>

                  <div class="form-hint">默认写入策略由系统内部处理，采集页不展示索引配置；解析配置仅通过采集源名称关联。</div>
                  <p v-if="inputFormError" data-testid="input-form-error" class="field-error form-error">{{ inputFormError }}</p>
                  <div class="actions"><button class="btn" type="submit">{{ editingInputId ? "保存修改" : "新增" }}</button><button data-testid="cancel-input-form" class="btn ghost" type="button" @click="resetInputForm">取消</button></div>
                </form>
              </article>

              <article class="card">
                <div class="card-head"><span>采集列表</span><span class="status-line">点击行查看运行详情</span></div>
                <div class="table-wrap">
                  <table>
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
                    <label class="page-size-select"><select v-model.number="collectPageSize" data-testid="collect-page-size" class="select compact-select" @change="reloadCollectFirstPage"><option v-for="size in listPageSizes" :key="size" :value="size">{{ size }} 条/页</option></select></label>
                  </div>
                </div>
              </article>
            </div>
          </section>

          <section v-if="currentModule === 'parse'" data-testid="parse-page" class="tab-panel">
            <div class="panel-header"><h2><span class="page-icon page-icon-parse">PX</span>解析配置</h2><div class="panel-header-actions"><span class="badge">props.conf / 解析插件</span><button data-testid="show-rule-form" class="btn" type="button" @click="openRuleForm">新增解析规则</button></div></div>
            <div class="content-grid" :class="{ 'list-first': !showRuleForm }">
              <article v-if="showRuleForm" data-testid="rule-form-card" class="card config-drawer" aria-label="解析配置表单">
                <div class="card-head"><span>{{ editingRuleId ? "修改规则" : "新增规则" }}</span><button class="btn ghost" type="button" @click="clearRuleForm">清空</button></div>
                <form class="form-grid" @submit.prevent="saveRule">
                  <label>规则名称<input v-model="ruleForm.name" data-testid="rule-name" class="field" required placeholder="请输入解析规则名称" /></label>
                  <div class="two">
                    <label>关联采集数据源名称<select v-model="ruleForm.dataSourceName" data-testid="rule-source" class="select" required @change="applyDataSourceRoute"><option value="">请选择采集数据源</option><option v-for="item in inputConfigs" :key="item.id" :value="item.name">{{ item.name }}</option></select></label>
                    <label>写入 index<select v-model="ruleForm.outputIndex" data-testid="rule-output-index" class="select" required><option v-for="item in businessIndexes" :key="item.id" :value="item.name">{{ item.name }}</option></select></label>
                  </div>
                  <label>优先级<input v-model.number="ruleForm.priority" data-testid="rule-priority" class="field" min="1" required type="number" placeholder="100" /></label>
                  <div class="note">新增规则默认不绑定采集数据源，需手动选择；写入 index 仅展示逻辑 index，物理表由服务端内部映射。</div>
	                  <label>解析方式</label>
	                  <div class="plugin-grid parser-plugin-grid">
	                    <button data-testid="parser-regex" :class="{ active: ruleForm.plugin === '正则解析插件' }" class="plugin-card" type="button" @click="selectParserPlugin('正则解析插件')"><span class="plugin-icon icon-regex">F(x)</span>正则</button>
	                  </div>
	                  <label>日志样例<textarea v-model="ruleForm.sampleLog" data-testid="sample-log" required placeholder="请输入日志样例"></textarea></label>
	                  <div v-if="ruleForm.plugin === '正则解析插件'" class="param-panel"><label>正则表达式<textarea v-model="ruleForm.regexPattern" data-testid="regex-pattern" required placeholder="src=(?<src_ip>\\S+)\\s+dst=(?<dst_ip>\\S+)\\s+action=(?<action>\\S+)"></textarea></label></div>
                  <div class="actions"><button data-testid="preview-parse" class="btn" type="button" @click="previewParse">预览解析结果</button></div>
                  <div data-testid="parse-preview" class="preview-box table-wrap"><table><thead><tr><th>序号</th><th>字段</th><th>值</th><th>字段类型</th></tr></thead><tbody><tr v-if="!previewRows.length"><td colspan="4">暂无解析结果</td></tr><tr v-for="(row, index) in previewRows" :key="`${row.field}-${index}`"><td>{{ index + 1 }}</td><td><code>{{ row.field }}</code></td><td>{{ row.value }}</td><td>{{ row.type }}</td></tr></tbody></table></div>
                  <details class="advanced-panel" open><summary>高级配置 / props.conf</summary><label>最终 props.conf 配置<textarea v-model="ruleForm.propsConf" data-testid="props-conf" class="props-editor" required placeholder="[source::firewall]&#10;TIME_PREFIX = event_time="></textarea></label></details>
                  <div class="form-hint">规则仅在 <code>ingest</code> 阶段生效。页面不单独展示事件时间字段；时间识别请在高级配置中使用 props.conf 配置项。</div>
                  <p v-if="ruleFormError" data-testid="parse-form-error" class="field-error form-error">{{ ruleFormError }}</p>
                  <div class="actions"><button class="btn" type="submit">{{ editingRuleId ? "保存修改" : "新增" }}</button><button data-testid="cancel-rule-form" class="btn ghost" type="button" @click="resetRuleForm">取消</button></div>
                </form>
              </article>
              <article class="card"><div class="card-head"><span>规则列表</span><span class="status-line">查询 / 修改 / 删除</span></div><div class="table-wrap"><table><thead><tr><th>名称</th><th>解析插件</th><th>采集数据源</th><th>写入 index</th><th>优先级</th><th>props.conf</th><th>操作</th></tr></thead><tbody><tr v-for="item in parseRules" :key="item.id"><td>{{ item.name }}</td><td>{{ item.plugin }}</td><td><code>{{ item.dataSourceName || "未选择" }}</code></td><td><code>{{ item.outputIndex || "app" }}</code></td><td><code>{{ item.priority || 100 }}</code></td><td><code class="multiline-code">{{ item.propsConf }}</code></td><td><div class="row-actions"><button class="link-btn" type="button" @click="editRule(item)">修改</button><button class="link-btn delete" type="button" @click="deleteRule(item.id)">删除</button></div></td></tr></tbody></table></div><div data-testid="parse-pagination" class="pagination-bar"><div class="pagination-controls"><button data-testid="parse-prev" class="pager-arrow" type="button" :disabled="parsePagination.page <= 1" aria-label="上一页" @click="goParsePage(parsePagination.page - 1)">‹</button><template v-for="item in visibleParsePages" :key="item.key"><span v-if="item.ellipsis" class="pager-ellipsis">...</span><button v-else :data-testid="`parse-page-${item.page}`" class="pager-page" :class="{ active: item.page === parsePagination.page }" type="button" @click="goParsePage(item.page)">{{ item.label }}</button></template><button data-testid="parse-next" class="pager-arrow" type="button" :disabled="parsePagination.page >= totalParsePages" aria-label="下一页" @click="goParsePage(parsePagination.page + 1)">›</button><label class="page-size-select"><select v-model.number="parsePageSize" data-testid="parse-page-size" class="select compact-select" @change="reloadParseFirstPage"><option v-for="size in listPageSizes" :key="size" :value="size">{{ size }} 条/页</option></select></label></div></div></article>
            </div>
          </section>

          <section v-if="currentModule === 'index'" data-testid="index-page" class="tab-panel">
            <div class="panel-header"><h2><span class="page-icon page-icon-index">IX</span>索引配置</h2><div class="panel-header-actions"><span class="badge">ClickHouse 物理分表</span><button data-testid="show-index-form" class="btn" type="button" @click="openIndexForm">新增 index</button></div></div>
            <div class="content-grid" :class="{ 'list-first': !showIndexForm }">
                  <article v-if="showIndexForm" data-testid="index-form-card" class="card config-drawer" aria-label="索引配置表单"><div class="card-head"><span>{{ editingIndexId ? "修改索引" : "新增索引" }}</span><button class="btn ghost" type="button" @click="clearIndexForm">清空</button></div><form class="form-grid" @submit.prevent="saveIndex"><label>index 名称<input v-model="indexForm.name" data-testid="index-name" class="field" required placeholder="请输入index名称" /></label><div class="two"><label>TTL 天数<input v-model="indexForm.ttl" data-testid="index-ttl" class="field" min="1" required type="number" /></label><label>状态<select v-model="indexForm.status" data-testid="index-status" class="select" required><option>active</option><option>disabled</option></select></label></div><p v-if="indexFormError" data-testid="index-form-error" class="field-error form-error">{{ indexFormError }}</p><div class="actions"><button class="btn" type="submit">{{ editingIndexId ? "保存修改" : "新增" }}</button><button data-testid="cancel-index-form" class="btn ghost" type="button" @click="resetIndexForm">取消</button></div></form></article>
              <article class="card"><div class="card-head"><span>索引列表</span><span class="status-line">查询 / 修改 / 删除</span></div><div class="table-wrap"><table><thead><tr><th>index</th><th>物理表</th><th>TTL</th><th>数据量</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="item in indexes" :key="item.id"><td><code>{{ item.name }}</code></td><td><code>events_{{ item.name }}</code></td><td>{{ item.ttl }}d</td><td>{{ item.rows.toLocaleString() }}</td><td>{{ item.status }}</td><td><div class="row-actions"><button class="link-btn" type="button" @click="editIndex(item)">修改</button><button class="link-btn delete" type="button" @click="deleteIndex(item.id)">删除</button></div></td></tr></tbody></table></div><div data-testid="index-pagination" class="pagination-bar"><div class="pagination-controls"><button data-testid="index-prev" class="pager-arrow" type="button" :disabled="indexPagination.page <= 1" aria-label="上一页" @click="goIndexPage(indexPagination.page - 1)">‹</button><template v-for="item in visibleIndexPages" :key="item.key"><span v-if="item.ellipsis" class="pager-ellipsis">...</span><button v-else :data-testid="`index-page-${item.page}`" class="pager-page" :class="{ active: item.page === indexPagination.page }" type="button" @click="goIndexPage(item.page)">{{ item.label }}</button></template><button data-testid="index-next" class="pager-arrow" type="button" :disabled="indexPagination.page >= totalIndexPages" aria-label="下一页" @click="goIndexPage(indexPagination.page + 1)">›</button><label class="page-size-select"><select v-model.number="indexPageSize" data-testid="index-page-size" class="select compact-select" @change="reloadIndexFirstPage"><option v-for="size in listPageSizes" :key="size" :value="size">{{ size }} 条/页</option></select></label></div></div></article>
            </div>
          </section>

          <section v-if="currentModule === 'search'" data-testid="search-page" class="tab-panel">
            <div class="panel-header"><h2><span class="page-icon page-icon-search">SP</span>搜索页</h2><span class="badge">SPL / 时间筛选</span></div>
            <div class="search-layout">
              <div class="search-row"><textarea v-model="searchQuery" class="search-box" rows="1" aria-label="SPL 搜索语句" placeholder="请输入 SPL语句"></textarea><select v-model="searchTime" data-testid="search-time" class="select"><option v-for="option in timeOptions" :key="option">{{ option }}</option></select><button data-testid="search-button" class="btn" type="button" @click="runSearchFirstPage">查询</button><button class="btn ghost" type="button" @click="saveSearch">保存</button></div>
              <div data-testid="time-help" class="time-help">高级时间：<code>@d</code> 表示当天 0 点，<code>earliest=@d latest=now</code> 表示今天 0 点到当前时间，<code>-7d@d</code> 表示 7 天前 0 点。</div>
              <div data-testid="timeline-chart" class="timeline timeline-shell" :class="{ empty: !timelineBars.length }"><div v-if="timelineBars.length" data-testid="timeline-y-axis" class="timeline-y-axis"><span v-for="label in timelineYAxisLabels" :key="label">{{ label }}</span></div><div class="timeline-plot"><div v-if="!timelineBars.length" class="timeline-empty">{{ timelineStatus }}</div><div v-else class="timeline-bars"><div v-for="bucket in timelineBars" :key="bucket.start" data-testid="timeline-bar" class="bar" :title="timelineTooltip(bucket)" :style="{ height: `${bucket.height}%` }"></div></div><div v-if="timelineBars.length" data-testid="timeline-x-axis" class="timeline-x-axis"><span v-for="tick in timelineTicks" :key="tick.key">{{ tick.label }}</span></div></div></div>
              <div class="search-toolbar"><div data-testid="saved-summary" class="saved-summary"><strong>保存搜索</strong><span>仅占一行，点击查看</span><span class="count">{{ savedSearches.length }}</span></div><button data-testid="toggle-saved-searches" class="btn ghost" type="button" @click="toggleSavedSearches">{{ savedOpen ? "收起保存搜索" : "查看保存搜索" }}</button></div>
              <div v-if="savedSearchError" data-testid="saved-search-error" class="field-error">{{ savedSearchError }}</div>
              <div v-if="savedOpen" class="saved-drawer"><div class="drawer-head"><span>已保存搜索</span><span class="status-line">查询 / 删除 / 回填</span></div><div class="table-wrap"><table><thead><tr><th>SPL</th><th>时间</th><th>操作</th></tr></thead><tbody><tr v-for="item in savedSearches" :key="item.id" :data-testid="`saved-search-row-${item.id}`"><td><code>{{ item.query }}</code></td><td>{{ item.time }}</td><td><div class="row-actions"><button class="link-btn" type="button" @click="useSearch(item)">回填</button><button class="link-btn delete" type="button" :data-testid="`delete-saved-search-${item.id}`" @click="deleteSavedSearch(item.id)">删除</button></div></td></tr></tbody></table></div></div>
              <article class="card"><div class="card-head result-head"><div><span>搜索结果</span><div class="result-meta">{{ resultStatus }}</div></div><span data-testid="result-mode" class="mode-pill">{{ resultMode === "stats" ? "统计视图" : "事件视图" }}</span></div><div data-testid="search-results" class="table-wrap"><table class="result-table"><thead><tr v-if="resultMode === 'stats'"><th v-for="field in statsFields" :key="field">{{ field }}</th></tr><tr v-else><th class="expand-col"></th><th>时间</th><th>事件</th></tr></thead><tbody><tr v-if="!searchResults.length"><td :colspan="resultMode === 'stats' ? Math.max(statsFields.length, 1) : 3">暂无匹配结果</td></tr><template v-for="(item, rowIndex) in searchResults" :key="item.id || item.group || rowIndex"><tr><template v-if="resultMode === 'stats'"><td v-for="field in statsFields" :key="field"><code>{{ formatStatsCell(field, item[field]) }}</code></td></template><template v-else><td><button :data-testid="`expand-event-${eventRowKey(item, rowIndex)}`" class="expand-toggle" type="button" @click="toggleEventDetail(item, rowIndex)">{{ isEventExpanded(item, rowIndex) ? "▼" : "▶" }}</button></td><td>{{ item.time }}</td><td><code class="multiline-code">{{ item.event }}</code></td></template></tr><tr v-if="resultMode !== 'stats' && isEventExpanded(item, rowIndex)" class="event-detail-row"><td></td><td colspan="2"><div class="event-detail"><div class="detail-raw"><span>raw</span><code class="multiline-code">{{ item.raw }}</code></div><table><thead><tr><th>字段</th><th>值</th></tr></thead><tbody><tr v-for="(row, detailIndex) in item.detailRows" :key="`${row.name}-${detailIndex}`"><td><code>{{ row.name }}</code></td><td><code>{{ formatDetailValue(row.value) }}</code></td></tr></tbody></table></div></td></tr></template></tbody></table></div><div data-testid="search-pagination" class="pagination-bar"><div data-testid="search-pagination-right" class="pagination-controls"><button data-testid="search-prev" class="pager-arrow" type="button" :disabled="searchPagination.page <= 1 || isSearchLoading" aria-label="上一页" @click="goSearchPage(searchPagination.page - 1)">‹</button><template v-for="item in visibleSearchPages" :key="item.key"><span v-if="item.ellipsis" class="pager-ellipsis">...</span><button v-else :data-testid="`search-page-${item.page}`" class="pager-page" :class="{ active: item.page === searchPagination.page }" type="button" :disabled="isSearchLoading" @click="goSearchPage(item.page)">{{ item.label }}</button></template><button data-testid="search-next" class="pager-arrow" type="button" :disabled="searchPagination.page >= totalSearchPages || isSearchLoading" aria-label="下一页" @click="goSearchPage(searchPagination.page + 1)">›</button><label class="page-size-select"><select v-model.number="searchPageSize" data-testid="search-page-size" class="select compact-select" @change="runSearchFirstPage"><option v-for="size in searchPageSizes" :key="size" :value="size">{{ size }} 条/页</option></select></label></div></div></article>
            </div>
          </section>

          <section v-if="currentModule === 'plugins'" data-testid="plugins-page" class="tab-panel">
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
                <div class="plugin-upload-panel">
                  <label>插件包上传<input data-testid="plugin-upload-file" class="field" type="file" accept=".zip,application/zip" @change="onPluginFileChange" /></label>
                  <button data-testid="plugin-upload-button" class="btn" type="button" @click="uploadPluginPackage">导入插件包</button>
                  <span data-testid="plugin-upload-status" class="status-line">{{ pluginUploadStatus }}</span>
                </div>
                <p v-if="pluginUploadError" class="field-error form-error">{{ pluginUploadError }}</p>
              </article>

              <article class="card">
                <div class="card-head"><span>{{ currentPluginTabLabel }}</span><span class="status-line">{{ filteredPlugins.length }} 个插件</span></div>
                <div class="table-wrap">
                  <table>
                    <thead><tr><th>插件名称</th><th>编码</th><th>版本</th><th>运行时</th><th>状态</th><th>校验值</th></tr></thead>
                    <tbody>
                      <tr v-if="!filteredPlugins.length"><td colspan="6">暂无插件</td></tr>
                      <tr v-for="item in filteredPlugins" :key="`${item.plugin_type}-${item.plugin_code}-${item.plugin_version}`">
                        <td>{{ item.name || item.plugin_code }}</td>
                        <td><code>{{ item.plugin_code }}</code></td>
                        <td>{{ item.plugin_version || "1.0.0" }}</td>
                        <td>{{ item.runtime || "go_builtin" }}</td>
                        <td><span class="status-pill" :class="item.status === 'active' ? 'runtime-running' : 'runtime-stopped'">{{ pluginStatusLabel(item.status) }}</span></td>
                        <td><code class="muted-code">{{ item.checksum || "builtin" }}</code></td>
                      </tr>
                    </tbody>
                  </table>
                </div>
              </article>
            </div>
          </section>
        </section>
      </section>
    </section>
  </main>
</template>

<script setup>
import { ref, reactive, computed, onMounted, watch } from "vue";

const tokenKey = "xdp_api_token";
const currentModuleKey = "xdp_current_module";
const defaultModuleKey = "collect";
const screen = ref("login");
const auth = reactive({ enabled: true, authenticated: false });
const credentials = reactive({ username: "admin", password: "" });
const loginError = ref("");
const lastProtectedPayload = ref("");
const modules = [{ key: "collect", label: "采集配置" }, { key: "parse", label: "解析配置" }, { key: "index", label: "索引配置" }, { key: "search", label: "搜索页" }, { key: "plugins", label: "插件管理" }];
const currentModule = ref(defaultModuleKey);

const inputConfigs = ref([
  { id: "in-1", name: "Firewall Syslog", plugin: "Syslog", status: "active", internalRawTopic: "raw.ds_firewall_syslog", collectorPort: "5514", transportProtocol: "TCP", encoding: "UTF-8", logFilterEnabled: "on", logFilterRegex: "action=(allow|deny)" },
  { id: "in-2", name: "Kafka Stream", plugin: "Kafka", status: "disabled", internalRawTopic: "raw.ds_kafka_stream", brokers: "10.0.0.1:9092,10.0.0.2:9092", topic: "xdp-events", consumerGroup: "xdp-consumer", securityProtocol: "PLAINTEXT", consumeStrategy: "最早", logFilterEnabledKafka: "off" }
]);
const parseRules = ref([{ id: "rule-regex", name: "Firewall Regex", plugin: "正则解析插件", dataSourceName: "Firewall Syslog", inputRoute: "raw.ds_firewall_syslog", outputIndex: "firewall", propsConf: "[source::firewall]\nEXTRACT-custom = src=(?<src_ip>\\S+)" }]);
const parserPlugins = ref([]);
const parseConfigLoaded = ref(false);
const indexConfigLoaded = ref(false);
const indexes = ref([{ id: "idx-1", name: "app", ttl: 30, rows: 179497, status: "active" }, { id: "idx-2", name: "firewall", ttl: 30, rows: 42013, status: "active" }, { id: "idx-3", name: "audit", ttl: 7, rows: 0, status: "active" }]);
const inputForm = reactive(defaultInputForm());
const ruleForm = reactive(defaultRuleForm());
const generatedPropsConf = ref("");
const indexForm = reactive(defaultIndexForm());
const editingInputId = ref("");
const editingRuleId = ref("");
const editingIndexId = ref("");
const showInputForm = ref(false);
const showRuleForm = ref(false);
const showIndexForm = ref(false);
const inputPortError = ref("");
const inputNameError = ref("");
const inputFormError = ref("");
const ruleFormError = ref("");
const indexFormError = ref("");
const selectedRuntimeId = ref("");
const runtimeDetail = ref(null);
const runtimeLoading = ref(false);
const runtimeError = ref("");
const previewRows = ref([]);
const timeOptions = ["近 1 天", "昨天", "近 7 天", "近一个月", "近一年", "所有时间", "自定义时间", "高级时间表达式"];
const timelineBuckets = ref([]);
const timelineStatus = ref("执行搜索后展示时间分布");
const timelineIntervalLabel = ref("auto");
const searchQuery = ref("");
const searchTime = ref("近 1 天");
const searchResults = ref([]);
const statsFields = ref([]);
const resultMode = ref("events");
const resultStatus = ref("等待执行搜索");
const isSearchLoading = ref(false);
const searchPage = ref(1);
const listPageSizes = [10, 50, 100, 1000];
const collectPageSize = ref(10);
const parsePageSize = ref(10);
const indexPageSize = ref(10);
const collectPagination = ref(defaultListPagination());
const parsePagination = ref(defaultListPagination());
const indexPagination = ref(defaultListPagination());
const searchPageSizes = [20, 50, 100, 1000];
const searchPageSize = ref(20);
const searchPagination = ref({ limit: 20, offset: 0, page: 1, returned: 0, hasMore: false, total: 0 });
const searchTimeRangeText = ref("");
const expandedEvents = ref(new Set());
const savedOpen = ref(false);
const savedSearchError = ref("");
const savedSearches = ref([]);
const savedSearchesLoaded = ref(false);
const pluginTabs = [
  { key: "input", label: "采集插件" },
  { key: "parser", label: "解析插件" },
  { key: "search_command", label: "搜索命令插件" }
];
const currentPluginTab = ref("input");
const pluginCatalog = ref([]);
const pluginsLoaded = ref(false);
const pluginUploadFile = ref(null);
const pluginUploadStatus = ref("");
const pluginUploadError = ref("");
const catalog = [
  { id: "evt-1", time: timeAgo(0, 0, 12), index: "app", source: "syslog-default", sourcetype: "app-regex", host: "api-01", service: "api", action: "allow", bytes: 1024, event: 'service=api level=info msg="login ok" bytes=1024' },
  { id: "evt-2", time: timeAgo(0, 0, 36), index: "app", source: "syslog-default", sourcetype: "app-regex", host: "web-01", service: "api", action: "allow", bytes: 3840, event: 'service=api level=warn msg="slow request" bytes=3840' },
  { id: "evt-3", time: timeAgo(1, 3, 0), index: "app", source: "syslog-default", sourcetype: "app-regex", host: "pay-01", service: "checkout", action: "allow", bytes: 2048, event: 'service=checkout level=info msg="payment ok" bytes=2048' },
  { id: "evt-4", time: timeAgo(1, 4, 0), index: "firewall", source: "syslog-udp", sourcetype: "firewall", host: "edge-01", service: "firewall", action: "deny", bytes: 2048, event: "src=10.0.1.8 dst=172.16.0.4 action=deny bytes=2048" }
];
const counts = computed(() => ({
  collect: Number(collectPagination.value.total || inputConfigs.value.length),
  parse: Number(parsePagination.value.total || parseRules.value.length),
  index: Number(indexPagination.value.total || indexes.value.length),
  search: savedSearches.value.length,
  plugins: pluginCatalog.value.length
}));
const businessIndexes = computed(() => indexes.value.filter((item) => !isSystemIndex(item)));
const selectedRuntimeName = computed(() => inputConfigs.value.find((item) => item.id === selectedRuntimeId.value)?.name || "");
const runtimeDetailSummary = computed(() => {
  if (runtimeDetail.value) return collectRuntimeSummary(runtimeDetail.value);
  const item = inputConfigs.value.find((current) => current.id === selectedRuntimeId.value);
  return collectRuntimeSummary(item || {});
});
const totalSearchPages = computed(() => {
  const limit = Math.max(1, Number(searchPagination.value.limit || searchPageSize.value || 20));
  const total = Math.max(0, Number(searchPagination.value.total || 0));
  const current = Math.max(1, Number(searchPagination.value.page || 1));
  const exactPages = Math.max(1, Math.ceil(total / limit));
  const minimumPages = searchPagination.value.hasMore ? current + 1 : current;
  return Math.max(exactPages, minimumPages);
});
const totalCollectPages = computed(() => totalListPages(collectPagination.value, collectPageSize.value));
const totalParsePages = computed(() => totalListPages(parsePagination.value, parsePageSize.value));
const totalIndexPages = computed(() => totalListPages(indexPagination.value, indexPageSize.value));
const visibleCollectPages = computed(() => visiblePageTokens(totalCollectPages.value, collectPagination.value.page));
const visibleParsePages = computed(() => visiblePageTokens(totalParsePages.value, parsePagination.value.page));
const visibleIndexPages = computed(() => visiblePageTokens(totalIndexPages.value, indexPagination.value.page));
const visibleSearchPages = computed(() => {
  const total = totalSearchPages.value;
  const current = Math.min(Math.max(1, Number(searchPagination.value.page || 1)), total);
  const pageSet = new Set([1, total, current, current - 1, current + 1].filter((page) => page >= 1 && page <= total));
  const pages = total <= 5 ? Array.from({ length: total }, (_, index) => index + 1) : Array.from(pageSet).sort((a, b) => a - b);
  const tokens = [];
  pages.forEach((page, index) => {
    const previous = pages[index - 1];
    if (previous && page - previous > 1) {
      tokens.push({ key: `ellipsis-${previous}-${page}`, ellipsis: true });
    }
    tokens.push({ key: `page-${page}`, page, label: String(page), ellipsis: false });
  });
  return tokens;
});
onMounted(loadAuthStatus);
watch(() => [
  ruleForm.name,
  ruleForm.plugin,
  ruleForm.regexPattern,
  ruleForm.kvPairDelimiter,
  ruleForm.kvDelimiter,
  ruleForm.kvQuote,
  ruleForm.delimitedDelimiter,
  ruleForm.delimitedQuote,
  ruleForm.delimitedFields
], () => syncPropsConf(), { immediate: true });
watch(currentModule, async (module) => {
  persistCurrentModule(module);
  if (module === "parse") {
    await loadIndexConfig();
    await loadParseConfig();
  }
  if (module === "index") await loadIndexConfig();
  if (module === "search" && !savedSearchesLoaded.value) await loadSavedSearches();
  if (module === "plugins" && !pluginsLoaded.value) await loadPlugins();
});

function navCount(key) { return counts.value[key] || 0; }
async function loadAuthStatus() {
  loginError.value = "";
  const response = await requestJSON("/api/v1/auth", { auth: true });
  Object.assign(auth, response);
  if (!response.enabled || response.authenticated) {
    enterConsole();
    await loadProtectedData();
    return;
  }
  screen.value = "login";
}
async function submitLogin() {
  loginError.value = "";
  const username = credentials.username.trim();
  const password = credentials.password;
  if (!username || !password.trim()) { loginError.value = "请输入用户名和密码"; return; }
  try {
    const response = await requestJSON("/api/v1/login", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ username, password }) });
    localStorage.setItem(tokenKey, response.token);
    enterConsole();
    await loadProtectedData();
  } catch (error) {
    localStorage.removeItem(tokenKey);
    loginError.value = error.message;
    screen.value = "login";
  }
}
function enterConsole() {
  currentModule.value = readStoredModule();
  persistCurrentModule(currentModule.value);
  screen.value = "app";
}
function logout() {
  localStorage.removeItem(tokenKey);
  localStorage.removeItem(currentModuleKey);
  lastProtectedPayload.value = "";
  screen.value = "login";
}
function selectModule(moduleKey) {
  if (!isValidModule(moduleKey)) return;
  currentModule.value = moduleKey;
}
function readStoredModule() {
  const stored = localStorage.getItem(currentModuleKey);
  return isValidModule(stored) ? stored : defaultModuleKey;
}
function persistCurrentModule(moduleKey) {
  localStorage.setItem(currentModuleKey, isValidModule(moduleKey) ? moduleKey : defaultModuleKey);
}
function isValidModule(moduleKey) {
  return modules.some((item) => item.key === moduleKey);
}
function defaultListPagination(pageSize = 10) {
  return { page: 1, page_size: pageSize, total: 0, total_pages: 1 };
}
function listURL(path, page, pageSize) {
  const params = new URLSearchParams({
    page: String(Math.max(1, Number(page) || 1)),
    page_size: String(Math.max(1, Number(pageSize) || 10))
  });
  return `${path}?${params.toString()}`;
}
function normalizeListPagination(pagination = {}, returned = 0, requestedPage = 1, pageSize = 10) {
  const size = Math.max(1, Number(pagination.page_size || pagination.pageSize || pageSize || 10));
  const page = Math.max(1, Number(pagination.page || requestedPage || 1));
  const total = Math.max(0, Number(pagination.total ?? returned ?? 0));
  const totalPages = Math.max(1, Number(pagination.total_pages || pagination.totalPages || Math.ceil(total / size) || 1));
  return { page, page_size: size, total, total_pages: totalPages };
}
function totalListPages(pagination, pageSize = 10) {
  const total = Math.max(0, Number(pagination?.total || 0));
  const size = Math.max(1, Number(pagination?.page_size || pageSize || 10));
  return Math.max(1, Number(pagination?.total_pages || Math.ceil(total / size) || 1));
}
function visiblePageTokens(total, current) {
  const safeTotal = Math.max(1, Number(total || 1));
  const safeCurrent = Math.min(Math.max(1, Number(current || 1)), safeTotal);
  const pageSet = new Set([1, safeTotal, safeCurrent, safeCurrent - 1, safeCurrent + 1].filter((page) => page >= 1 && page <= safeTotal));
  const pages = safeTotal <= 5 ? Array.from({ length: safeTotal }, (_, index) => index + 1) : Array.from(pageSet).sort((a, b) => a - b);
  const tokens = [];
  pages.forEach((page, index) => {
    const previous = pages[index - 1];
    if (previous && page - previous > 1) {
      tokens.push({ key: `ellipsis-${previous}-${page}`, ellipsis: true });
    }
    tokens.push({ key: `page-${page}`, page, label: String(page), ellipsis: false });
  });
  return tokens;
}
function adjustListPaginationTotal(paginationRef, delta, pageSize = 10) {
  const total = Math.max(0, Number(paginationRef.value.total || 0) + delta);
  const size = Math.max(1, Number(paginationRef.value.page_size || pageSize || 10));
  paginationRef.value = {
    ...paginationRef.value,
    page_size: size,
    total,
    total_pages: Math.max(1, Math.ceil(total / size) || 1)
  };
}
async function goCollectPage(page) {
  if (page < 1 || page > totalCollectPages.value) return;
  await loadCollectConfig(page);
}
async function reloadCollectFirstPage() {
  collectPagination.value = { ...collectPagination.value, page: 1, page_size: collectPageSize.value };
  await loadCollectConfig(1);
}
async function goParsePage(page) {
  if (page < 1 || page > totalParsePages.value) return;
  await loadParseConfig(true, page);
}
async function reloadParseFirstPage() {
  parsePagination.value = { ...parsePagination.value, page: 1, page_size: parsePageSize.value };
  await loadParseConfig(true, 1);
}
async function goIndexPage(page) {
  if (page < 1 || page > totalIndexPages.value) return;
  await loadIndexConfig(true, page);
}
async function reloadIndexFirstPage() {
  indexPagination.value = { ...indexPagination.value, page: 1, page_size: indexPageSize.value };
  await loadIndexConfig(true, 1);
}
async function loadProtectedData() {
  try {
    await Promise.all([loadCollectConfig(), loadIndexConfig(true), loadParseConfig(true), loadSavedSearches()]);
  } catch {
    lastProtectedPayload.value = "";
  }
}
async function loadCollectConfig(page = collectPagination.value.page || 1) {
  try {
    const payload = await requestJSON(listURL("/api/v1/datasources", page, collectPageSize.value), { auth: true });
    lastProtectedPayload.value = JSON.stringify(payload, null, 2);
    if (Array.isArray(payload.datasources)) {
      inputConfigs.value = payload.datasources.filter(isCollectSourcePayload).map(apiSourceToInput);
    }
    collectPagination.value = normalizeListPagination(payload.pagination, inputConfigs.value.length, page, collectPageSize.value);
  } catch {
    lastProtectedPayload.value = "";
  }
}
async function loadParseConfig(force = false, page = parsePagination.value.page || 1) {
  if (parseConfigLoaded.value && !force) return;
  try {
    if (!parserPlugins.value.length) {
      const pluginsPayload = await requestJSON("/api/v1/parser-plugins", { auth: true });
      if (Array.isArray(pluginsPayload.plugins)) parserPlugins.value = pluginsPayload.plugins;
    }
    const rulesPayload = await requestJSON(listURL("/api/v1/parse-rules", page, parsePageSize.value), { auth: true });
    if (Array.isArray(rulesPayload.parse_rules)) parseRules.value = rulesPayload.parse_rules.map(apiRuleToForm);
    parsePagination.value = normalizeListPagination(rulesPayload.pagination, parseRules.value.length, page, parsePageSize.value);
    parseConfigLoaded.value = true;
  } catch {
    parseConfigLoaded.value = false;
  }
}
async function loadIndexConfig(force = false, page = indexPagination.value.page || 1) {
  if (indexConfigLoaded.value && !force) return;
  try {
    const payload = await requestJSON(listURL("/api/v1/indexes", page, indexPageSize.value), { auth: true });
    if (Array.isArray(payload.indexes)) indexes.value = payload.indexes.map(apiIndexToForm);
    indexPagination.value = normalizeListPagination(payload.pagination, indexes.value.length, page, indexPageSize.value);
    indexConfigLoaded.value = true;
  } catch {
    indexConfigLoaded.value = false;
  }
}
async function loadPlugins(force = false) {
  if (pluginsLoaded.value && !force) return;
  try {
    const payload = await requestJSON("/api/v1/plugins", { auth: true });
    const items = Array.isArray(payload.plugins) ? payload.plugins : (Array.isArray(payload) ? payload : []);
    pluginCatalog.value = items.map(apiPluginToForm).filter(isProductVisiblePlugin);
    pluginsLoaded.value = true;
  } catch (error) {
    pluginUploadError.value = error.message || "插件列表加载失败";
    pluginsLoaded.value = false;
  }
}
async function requestJSON(url, options = {}) {
  const headers = { ...(options.headers || {}) };
  if (options.auth) {
    const token = localStorage.getItem(tokenKey);
    if (token) headers.Authorization = `Bearer ${token}`;
  }
  const response = await fetch(url, { ...options, headers });
  const text = await response.text();
  const payload = text ? JSON.parse(text) : {};
  if (!response.ok) throw new Error(errorMessage(payload, response.statusText));
  return payload;
}
function errorMessage(payload, fallback) {
  if (payload?.error?.message) return payload.error.message;
  if (typeof payload.error === "string") return payload.error;
  return fallback || "请求失败";
}
function defaultInputForm() { return { name: "", status: "active", plugin: "Syslog", collectorPort: "5514", transportProtocol: "UDP", encoding: "UTF-8", logFilterEnabled: "off", logFilterRegex: "", transportProtocolKafka: "TCP", brokers: "", topic: "", consumerGroup: "", securityProtocol: "PLAINTEXT", consumeStrategy: "最早", encodingKafka: "UTF-8", logFilterEnabledKafka: "off", logFilterRegexKafka: "" }; }
function defaultRuleForm() { return { name: "", plugin: "正则解析插件", dataSourceName: "", inputRoute: "internal_raw_topic", outputIndex: "app", priority: 100, sampleLog: "", regexPattern: "", kvPairDelimiter: "空格", kvDelimiter: "=", kvQuote: '"', delimitedDelimiter: ",", delimitedQuote: '"', delimitedFields: "field1,field2,field3", propsConf: "" }; }
function defaultIndexForm() { return { name: "", ttl: 30, status: "active" }; }
function assignReactive(target, source) { Object.keys(target).forEach((key) => delete target[key]); Object.assign(target, source); }
const currentPluginTabLabel = computed(() => pluginTabs.find((item) => item.key === currentPluginTab.value)?.label || "插件列表");
const filteredPlugins = computed(() => pluginCatalog.value.filter((item) => item.plugin_type === currentPluginTab.value));
function selectPluginTab(tab) {
  currentPluginTab.value = tab;
  pluginUploadStatus.value = "";
  pluginUploadError.value = "";
}
function pluginTypeCount(type) {
  return pluginCatalog.value.filter((item) => item.plugin_type === type).length;
}
function onPluginFileChange(event) {
  pluginUploadFile.value = event.target.files?.[0] || null;
  pluginUploadStatus.value = "";
  pluginUploadError.value = "";
}
async function uploadPluginPackage() {
  pluginUploadError.value = "";
  if (!pluginUploadFile.value) {
    pluginUploadStatus.value = "请选择插件包";
    return;
  }
  const formData = new FormData();
  formData.append("file", pluginUploadFile.value);
  formData.append("plugin_type", currentPluginTab.value);
  try {
    const imported = await requestJSON(`/api/v1/plugins/import?plugin_type=${encodeURIComponent(currentPluginTab.value)}`, {
      auth: true,
      method: "POST",
      body: formData
    });
    const item = apiPluginToForm(imported);
    pluginCatalog.value = upsertPlugin(pluginCatalog.value, item);
    pluginsLoaded.value = true;
    pluginUploadStatus.value = `导入成功：${item.name || item.plugin_code}`;
  } catch (error) {
    pluginUploadError.value = error.message || "插件导入失败";
  }
}
function apiPluginToForm(item = {}) {
  return {
    plugin_code: item.plugin_code || item.code || "",
    plugin_type: normalizePluginType(item.plugin_type || item.type || ""),
    plugin_version: item.plugin_version || item.version || "1.0.0",
    name: item.name || item.plugin_code || item.code || "",
    runtime: item.runtime || "go_builtin",
    status: item.status || "disabled",
    checksum: item.checksum || "builtin"
  };
}
function isProductVisiblePlugin(item = {}) {
  const type = normalizePluginType(item.plugin_type);
  const code = String(item.plugin_code || "").trim();
  if (isHiddenProductPlugin(type, code)) return false;
  if (type === "input") return code === "syslog" || isImportedPlugin(item);
  if (type === "parser") return code === "regex" || isImportedPlugin(item);
  if (type === "search_command") return code === "stats" || isImportedPlugin(item);
  return false;
}
function isHiddenProductPlugin(type, code) {
  return type === "input" && code === "http-input" ||
    type === "parser" && (code === "json-parser" || code === "props-conf-parser");
}
function isImportedPlugin(item = {}) {
  const checksum = String(item.checksum || "").trim();
  return checksum !== "" && checksum !== "builtin";
}
function normalizePluginType(type) {
  const value = String(type || "").trim().toLowerCase();
  if (value === "search") return "search_command";
  if (value === "collect" || value === "collector") return "input";
  if (value === "parse") return "parser";
  return value;
}
function upsertPlugin(items, item) {
  const key = (current) => `${current.plugin_type}/${current.plugin_code}@${current.plugin_version}`;
  return items.some((current) => key(current) === key(item)) ? items.map((current) => key(current) === key(item) ? item : current) : [item, ...items];
}
function pluginStatusLabel(status) {
  return status === "active" ? "已启用" : "未启用";
}
async function saveInput() {
  inputPortError.value = "";
  inputNameError.value = "";
  inputFormError.value = "";
  const inputValidationError = validateInputForm(inputForm);
  if (inputValidationError) {
    inputFormError.value = inputValidationError;
    return;
  }
  const request = inputFormToAPI(inputForm);
  if (collectDataSourceNameExists(request.name, editingInputId.value)) {
    inputNameError.value = "设备名称已存在";
    return;
  }
  if (!editingInputId.value && request.plugin_code === "syslog") {
    const ok = await checkInputListenerPort(request);
    if (!ok) return;
  }
  const url = editingInputId.value ? `/api/v1/datasources/${encodeURIComponent(editingInputId.value)}` : "/api/v1/datasources";
  const method = editingInputId.value ? "PUT" : "POST";
  const saved = await requestJSON(url, { auth: true, method, headers: { "Content-Type": "application/json" }, body: JSON.stringify(request) });
  const item = apiSourceToInput(saved);
  if (!editingInputId.value) adjustListPaginationTotal(collectPagination, 1, collectPageSize.value);
  inputConfigs.value = editingInputId.value ? inputConfigs.value.map((current) => current.id === editingInputId.value ? item : current) : [item, ...inputConfigs.value];
  resetInputForm();
}
async function checkInputListenerPort(request) {
  try {
    const config = request.plugin_config || {};
    const response = await requestJSON("/api/v1/datasources/port-check", {
      auth: true,
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        plugin_code: request.plugin_code,
        collector_port: config.collector_port,
        transport_protocol: config.transport_protocol || "UDP"
      })
    });
    if (response.available === false) {
      inputPortError.value = response.message || "端口不可用";
      return false;
    }
    return true;
  } catch (error) {
    inputPortError.value = error.message || "端口不可用";
    return false;
  }
}
function openInputForm() {
  clearInputForm();
}
function clearInputForm() {
  inputPortError.value = "";
  inputNameError.value = "";
  inputFormError.value = "";
  editingInputId.value = "";
  assignReactive(inputForm, defaultInputForm());
  showInputForm.value = true;
}
function editInput(item) {
  inputPortError.value = "";
  inputNameError.value = "";
  inputFormError.value = "";
  editingInputId.value = item.id;
  assignReactive(inputForm, { ...defaultInputForm(), ...item });
  showInputForm.value = true;
}
async function deleteInput(id) {
  await requestJSON(`/api/v1/datasources/${encodeURIComponent(id)}`, { auth: true, method: "DELETE" });
  inputConfigs.value = inputConfigs.value.filter((item) => item.id !== id);
  adjustListPaginationTotal(collectPagination, -1, collectPageSize.value);
  if (selectedRuntimeId.value === id) clearRuntimeDetail();
}
async function toggleInputStatus(item) {
  const nextStatus = collectCanStop(item) ? "disabled" : "active";
  const saved = await requestJSON(`/api/v1/datasources/${encodeURIComponent(item.id)}/status`, { auth: true, method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ status: nextStatus }) });
  const next = apiSourceToInput(saved);
  inputConfigs.value = inputConfigs.value.map((current) => current.id === item.id ? next : current);
  if (selectedRuntimeId.value === item.id) {
    await loadRuntimeDetail(next);
  }
}
function collectRuntimeSummary(item) {
  const runtimeStatus = String(item?.runtimeStatus || item?.runtime_status || "").toLowerCase();
  const listenerStatus = String(item?.listenerStatus || item?.listener_status || "").toLowerCase();
  if (runtimeStatus === "running" && listenerStatus === "listening") return { state: "running", label: "运行中" };
  if (runtimeStatus === "stopped" && listenerStatus === "stopped") return { state: "stopped", label: "已停止" };
  return { state: "error", label: "异常" };
}
function collectCanStop(item) {
  return collectRuntimeSummary(item).state === "running";
}
function collectCanStart(item) {
  return collectRuntimeSummary(item).state === "stopped";
}
function collectListenerPortLabel(item) {
  const port = String(item.collectorPort || parsePortFromEndpoint(item.listenerEndpoint) || "").trim();
  if (!port) return "-";
  const protocol = String(item.transportProtocol || item.transportProtocolKafka || "UDP").toUpperCase();
  return `${protocol}:${port}`;
}
async function selectRuntimeSource(item) {
  if (selectedRuntimeId.value === item.id && runtimeDetail.value) {
    clearRuntimeDetail();
    return;
  }
  await loadRuntimeDetail(item);
}
async function loadRuntimeDetail(item) {
  selectedRuntimeId.value = item.id;
  runtimeDetail.value = null;
  runtimeError.value = "";
  runtimeLoading.value = true;
  try {
    runtimeDetail.value = await requestJSON(`/api/v1/datasources/${encodeURIComponent(item.id)}/runtime`, { auth: true });
  } catch (error) {
    runtimeError.value = error.message || "运行状态加载失败";
  } finally {
    runtimeLoading.value = false;
  }
}
async function retryRuntimeSource(item) {
  await loadRuntimeDetail(item);
}
function clearRuntimeDetail() {
  selectedRuntimeId.value = "";
  runtimeDetail.value = null;
  runtimeError.value = "";
  runtimeLoading.value = false;
}
function formatRuntimeNumber(value) {
  return Number(value || 0).toLocaleString();
}
function formatRuntimeValue(value) {
  const text = String(value || "").trim();
  return text || "暂无";
}
function runtimeTopology(detail) {
  const fallbackRule = runtimeParseRuleFallback(detail);
  const parseRule = detail.parse_rule_name || detail.sourcetype || fallbackRule?.name || "未绑定解析规则";
  const outputIndex = runtimeTopologyOutputIndex(detail, fallbackRule, parseRule);
  return `${detail.agent_id || "local-agent"} -> ${detail.endpoint || "未监听"} -> ${parseRule} -> ${outputIndex}`;
}
function runtimeTopologyOutputIndex(detail, fallbackRule, parseRule) {
  const explicitIndex = String(detail?.output_index || "").trim();
  const fallbackIndex = String(fallbackRule?.outputIndex || "").trim();
  if (parseRule === "未绑定解析规则" && (!fallbackIndex || explicitIndex === "app")) {
    return "未指定 index";
  }
  return explicitIndex || fallbackIndex || "未指定 index";
}
function runtimeParseRuleFallback(detail) {
  const sourceName = String(detail?.name || selectedRuntimeName.value || "").trim();
  if (!sourceName) return null;
  return parseRules.value.find((rule) => String(rule.dataSourceName || "").trim() === sourceName) || null;
}
function resetInputForm() {
  clearInputForm();
  showInputForm.value = false;
}
function collectDataSourceNameExists(name, selfID = "") {
  const normalized = String(name || "").trim();
  if (!normalized) return false;
  return inputConfigs.value.some((item) => item.id !== selfID && String(item.name || "").trim() === normalized);
}
function validateInputForm(form) {
  if (!String(form.name || "").trim()) return "设备名称为必填项";
  if (!String(form.status || "").trim()) return "状态为必填项";
  if (!String(form.plugin || "").trim()) return "采集数据源类型为必填项";
  if (form.plugin === "Syslog") {
    if (!String(form.collectorPort || "").trim()) return "监听端口为必填项";
    if (!String(form.logFilterEnabled || "").trim()) return "日志筛选为必填项";
    if (!String(form.transportProtocol || "").trim()) return "传输层协议为必填项";
    if (!String(form.encoding || "").trim()) return "字符编码为必填项";
    if (form.logFilterEnabled === "on" && !String(form.logFilterRegex || "").trim()) return "正则筛选为必填项";
  }
  if (form.plugin === "Kafka") return "Kafka 采集插件运行时能力未启用，P1 支持";
  return "";
}
function isCollectSourcePayload(source) { const code = source.plugin_code || source.type; return code === "syslog" || code === "kafka"; }
function inputFormToAPI(form) {
  const isSyslog = form.plugin === "Syslog";
  const config = isSyslog ? {
    collector_port: toNumber(form.collectorPort, 5514),
    transport_protocol: String(form.transportProtocol || "UDP").toUpperCase(),
    encoding: form.encoding || "UTF-8",
    log_filter_enabled: form.logFilterEnabled === "on",
    log_filter_regex: form.logFilterEnabled === "on" ? form.logFilterRegex : ""
  } : {
    brokers: form.brokers,
    topic: form.topic,
    consumer_group: form.consumerGroup,
    transport_protocol: String(form.transportProtocolKafka || "TCP").toUpperCase(),
    security_protocol: form.securityProtocol,
    consume_strategy: form.consumeStrategy,
    encoding: form.encodingKafka || "UTF-8",
    log_filter_enabled: form.logFilterEnabledKafka === "on",
    log_filter_regex: form.logFilterEnabledKafka === "on" ? form.logFilterRegexKafka : ""
  };
  return { name: form.name, plugin_code: isSyslog ? "syslog" : "kafka", status: form.status, plugin_config: config };
}
function apiSourceToInput(source) {
  const config = source.plugin_config || {};
  const pluginCode = source.plugin_code || source.type || "syslog";
  if (pluginCode === "kafka") {
    return { ...defaultInputForm(), id: source.id, name: source.name, plugin: "Kafka", status: source.status, runtimeStatus: source.runtime_status || "", listenerStatus: source.listener_status || "", listenerEndpoint: source.listener_endpoint || "", internalRawTopic: source.internal_raw_topic || makeInputRoute(source.name), brokers: config.brokers || "", topic: config.topic || "", consumerGroup: config.consumer_group || "", transportProtocolKafka: config.transport_protocol || "TCP", securityProtocol: config.security_protocol || "PLAINTEXT", consumeStrategy: config.consume_strategy || "最早", encodingKafka: config.encoding || "UTF-8", logFilterEnabledKafka: config.log_filter_enabled ? "on" : "off", logFilterRegexKafka: config.log_filter_regex || "" };
  }
  return { ...defaultInputForm(), id: source.id, name: source.name, plugin: "Syslog", status: source.status, runtimeStatus: source.runtime_status || "", listenerStatus: source.listener_status || "", listenerEndpoint: source.listener_endpoint || "", internalRawTopic: source.internal_raw_topic || makeInputRoute(source.name), collectorPort: String(config.collector_port || parsePort(source.addr) || "5514"), transportProtocol: String(config.transport_protocol || source.protocol || "UDP").toUpperCase(), encoding: config.encoding || "UTF-8", logFilterEnabled: config.log_filter_enabled ? "on" : "off", logFilterRegex: config.log_filter_regex || "" };
}
function parsePort(value) { return Number(String(value || "").replace(/^:/, "")) || ""; }
function parsePortFromEndpoint(value) {
  const match = String(value || "").match(/:(\d+)(?:\/)?$/);
  return match ? match[1] : "";
}
function toNumber(value, fallback) { const parsed = Number(value); return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback; }

function makeInputRoute(name) { return `raw.ds_${String(name || "source").toLowerCase().replace(/[^a-z0-9]+/g, "_").replace(/^_+|_+$/g, "") || "source"}`; }
function applyDataSourceRoute() { const item = inputConfigs.value.find((input) => input.name === ruleForm.dataSourceName); ruleForm.inputRoute = item ? item.internalRawTopic : "internal_raw_topic"; }
function selectParserPlugin(plugin) { ruleForm.plugin = plugin; syncPropsConf(); }
async function previewParse() {
  const request = ruleFormToAPI(ruleForm);
  try {
    const id = editingRuleId.value || "preview";
    const response = await requestJSON(`/api/v1/parse-rules/${encodeURIComponent(id)}/test`, { auth: true, method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(request) });
    previewRows.value = response.fields || [];
    return;
	  } catch {
	    const sample = ruleForm.sampleLog || "";
	    previewRows.value = previewRegex(sample, ruleForm.regexPattern);
	  }
	}
function syncPropsConf() {
  const nextGenerated = buildPropsConf(ruleForm);
  const manualLines = preserveManualPropsConf(ruleForm.propsConf, generatedPropsConf.value, nextGenerated);
  ruleForm.propsConf = manualLines ? `${nextGenerated}\n${manualLines}` : nextGenerated;
  generatedPropsConf.value = nextGenerated;
}
function preserveManualPropsConf(current, previousGenerated, nextGenerated) {
  const currentText = String(current || "").trim();
  if (!currentText || currentText === String(previousGenerated || "").trim() || currentText === nextGenerated.trim()) return "";
  const generatedLines = new Set([
    ...String(previousGenerated || "").split("\n").map((line) => line.trim()).filter(Boolean),
    ...nextGenerated.split("\n").map((line) => line.trim()).filter(Boolean)
  ]);
  return currentText
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line && !generatedLines.has(line))
    .join("\n");
}
async function saveRule() {
  ruleFormError.value = "";
  const ruleValidationError = validateRuleForm(ruleForm);
  if (ruleValidationError) {
    ruleFormError.value = ruleValidationError;
    return;
  }
  const request = ruleFormToAPI(ruleForm);
  const url = editingRuleId.value ? `/api/v1/parse-rules/${encodeURIComponent(editingRuleId.value)}` : "/api/v1/parse-rules";
  const method = editingRuleId.value ? "PUT" : "POST";
  const saved = await requestJSON(url, { auth: true, method, headers: { "Content-Type": "application/json" }, body: JSON.stringify(request) });
  const item = apiRuleToForm(saved);
  if (!editingRuleId.value) adjustListPaginationTotal(parsePagination, 1, parsePageSize.value);
  parseRules.value = editingRuleId.value ? parseRules.value.map((current) => current.id === editingRuleId.value ? item : current) : [item, ...parseRules.value];
  resetRuleForm();
}
function openRuleForm() {
  clearRuleForm();
}
function clearRuleForm() {
  editingRuleId.value = "";
  ruleFormError.value = "";
  previewRows.value = [];
  assignReactive(ruleForm, defaultRuleForm());
  generatedPropsConf.value = buildPropsConf(ruleForm);
  ruleForm.propsConf = generatedPropsConf.value;
  showRuleForm.value = true;
}
function editRule(item) {
  ruleFormError.value = "";
  editingRuleId.value = item.id;
  assignReactive(ruleForm, { ...defaultRuleForm(), ...item });
  generatedPropsConf.value = buildPropsConf(ruleForm);
  showRuleForm.value = true;
}
async function deleteRule(id) {
  await requestJSON(`/api/v1/parse-rules/${encodeURIComponent(id)}`, { auth: true, method: "DELETE" });
  parseRules.value = parseRules.value.filter((item) => item.id !== id);
  adjustListPaginationTotal(parsePagination, -1, parsePageSize.value);
}
function resetRuleForm() {
  clearRuleForm();
  showRuleForm.value = false;
}
function validateRuleForm(form) {
  if (!String(form.name || "").trim()) return "规则名称为必填项";
  if (!String(form.dataSourceName || "").trim()) return "关联采集数据源名称为必填项";
  if (!String(form.outputIndex || "").trim()) return "写入 index 为必填项";
  if (!Number(form.priority || 0)) return "优先级为必填项";
  if (!String(form.plugin || "").trim()) return "解析方式为必填项";
  if (!String(form.sampleLog || "").trim()) return "日志样例为必填项";
  if (form.plugin === "正则解析插件" && !String(form.regexPattern || "").trim()) return "正则表达式为必填项";
  if (!String(form.propsConf || "").trim()) return "最终 props.conf 配置项为必填项";
  return "";
}
	function buildPropsConf(data) {
	  const sourceName = String(data.name || "custom").trim().toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "") || "custom";
	  return `[source::${sourceName}]\nEXTRACT-custom = ${(data.regexPattern || "field=(?<field>\\S+)").replace(/\?P</g, "?<")}`;
	}
function ruleFormToAPI(form) {
  const plugin = parserPluginCode(form.plugin);
  return { name: form.name, status: "active", parser_plugin: plugin, parser_plugin_version: "1.0.0", data_source_name: form.dataSourceName, input_route: form.inputRoute || "internal_raw_topic", output_index: form.outputIndex || "app", priority: Number(form.priority || 100), stage: "ingest", sample_event: form.sampleLog, plugin_config: pluginConfigFromRuleForm(form, plugin), props_conf: form.propsConf || buildPropsConf(form) };
}
	function pluginConfigFromRuleForm(form, plugin) {
	  if (plugin === "regex") return { source_field: "raw", regex_pattern: form.regexPattern, target: "fields", field_types: {}, on_no_match: "continue" };
	  return {};
	}
function apiRuleToForm(rule) {
  const config = rule.plugin_config || {};
  const plugin = parserPluginLabel(rule.parser_plugin);
  return { ...defaultRuleForm(), id: rule.id, name: rule.name, plugin, dataSourceName: rule.data_source_name || "", inputRoute: rule.input_route || "internal_raw_topic", outputIndex: rule.output_index || "app", priority: Number(rule.priority || 100), sampleLog: rule.sample_event || "", regexPattern: config.regex_pattern || "", kvPairDelimiter: displayDelimiter(config.field_delimiter), kvDelimiter: config.kv_delimiter || "=", kvQuote: config.field_quote || '"', delimitedDelimiter: config.field_delimiter || ",", delimitedQuote: config.field_quote || '"', delimitedFields: Array.isArray(config.field_names) ? config.field_names.join(",") : (config.field_names || "field1,field2,field3"), propsConf: rule.props_conf || "" };
}

	function parserPluginCode(label) { return { "正则解析插件": "regex" }[label] || label; }
	function parserPluginLabel(code) { return { regex: "正则解析插件" }[code] || code; }
function normalizeFieldDelimiter(value) { return value === "空格" ? " " : (value || " "); }
function displayDelimiter(value) { return value === " " ? "空格" : (value || "空格"); }
function previewJson(sample) { try { return flattenJson(JSON.parse(sample)); } catch { return [{ field: "error", value: "JSON 样例无法解析", type: "error" }]; } }
function flattenJson(value, prefix = "") { if (value === null || typeof value !== "object" || Array.isArray(value)) return [{ field: prefix || "root", value: typeof value === "object" ? JSON.stringify(value) : value, type: Array.isArray(value) ? "array" : typeof value }]; return Object.entries(value).flatMap(([key, child]) => flattenJson(child, prefix ? `${prefix}.${key}` : key)); }
function previewRegex(sample, pattern) {
  try {
    const regex = new RegExp((pattern || "field=(?<field>\\S+)").replace(/\?P</g, "?<"), "g");
    return Array.from(sample.matchAll(regex)).flatMap((match, matchIndex) => {
      const named = Object.entries(match.groups || {}).map(([field, value]) => ({ field, value, type: valueType(value) }));
      return named.length ? named : match.slice(1).map((value, index) => ({ field: `group_${matchIndex + 1}_${index + 1}`, value, type: valueType(value) }));
    });
  } catch { return [{ field: "error", value: "正则表达式无效", type: "error" }]; }
}
function previewKV(sample, delimiter = "=") { const escaped = delimiter.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"); return Array.from(sample.matchAll(new RegExp(`([\\w.@-]+)\\s*${escaped}\\s*(".*?"|'.*?'|\\S+)`, "g"))).map((match) => ({ field: match[1], value: stripQuotes(match[2]), type: valueType(match[2]) })); }
function previewDelimited(sample, delimiter = ",", fields = "") { const values = sample.split(resolveDelimiter(delimiter)); const names = fields.split(",").map((item) => item.trim()).filter(Boolean); return values.map((value, index) => ({ field: names[index] || `field_${index + 1}`, value: stripQuotes(value.trim()), type: valueType(value) })); }
function resolveDelimiter(value) { return { "竖杠": "|", "斜杠": "/", "逗号": ",", "分号": ";", "空格": " ", "换行": "\n" }[value] || value || ","; }
function stripQuotes(value) { const text = String(value || "").trim(); return ((text.startsWith('"') && text.endsWith('"')) || (text.startsWith("'") && text.endsWith("'"))) ? text.slice(1, -1) : text; }
function valueType(value) { if (/^-?\d+(\.\d+)?$/.test(String(value))) return "number"; if (/^(true|false)$/i.test(String(value))) return "boolean"; return "string"; }
async function saveIndex() {
  const validationMessage = validateIndexForm(indexForm);
  if (validationMessage) {
    indexFormError.value = validationMessage;
    return;
  }
  indexFormError.value = "";
  const request = {
    index_name: String(indexForm.name).trim(),
    ttl_days: Number(indexForm.ttl),
    status: indexForm.status
  };
  const saved = await requestJSON("/api/v1/indexes", { auth: true, method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(request) });
  const item = apiIndexToForm(saved);
  indexes.value = upsertIndexForm(indexes.value, item);
  await loadIndexConfig(true);
  resetIndexForm();
}
function validateIndexForm(form) {
  if (!String(form.name || "").trim()) return "index 名称为必填项";
  if (form.ttl === "" || form.ttl === null || form.ttl === undefined) return "TTL 天数为必填项";
  const ttl = Number(form.ttl);
  if (!Number.isFinite(ttl) || ttl <= 0) return "TTL 天数必须大于 0";
  if (!String(form.status || "").trim()) return "状态为必填项";
  return "";
}
function openIndexForm() {
  clearIndexForm();
}
function clearIndexForm() {
  editingIndexId.value = "";
  indexFormError.value = "";
  assignReactive(indexForm, defaultIndexForm());
  showIndexForm.value = true;
}
function editIndex(item) {
  indexFormError.value = "";
  editingIndexId.value = item.id;
  assignReactive(indexForm, { name: item.name, ttl: item.ttl, status: item.status });
  showIndexForm.value = true;
}
async function deleteIndex(id) {
  const item = indexes.value.find((current) => current.id === id);
  if (!item) return;
  await requestJSON(`/api/v1/indexes?index=${encodeURIComponent(item.name)}&drop_storage=true`, { auth: true, method: "DELETE" });
  indexes.value = indexes.value.filter((current) => current.id !== id);
  await loadIndexConfig(true);
}
function resetIndexForm() {
  clearIndexForm();
  showIndexForm.value = false;
}
function apiIndexToForm(index) {
  const name = index.index_name || index.name || "";
  return { id: name || nextId("idx"), name, ttl: Number(index.ttl_days || index.ttl || 30), rows: Number(index.rows || 0), status: index.status || "active", system: Boolean(index.system), indexType: index.index_type || (String(name).startsWith("_") ? "system" : "business") };
}
function isSystemIndex(item) { return Boolean(item?.system || item?.indexType === "system" || String(item?.name || "").startsWith("_")); }
function upsertIndexForm(items, item) {
  const exists = items.some((current) => current.name === item.name);
  return exists ? items.map((current) => current.name === item.name ? item : current) : [item, ...items];
}
async function runSearch({ resetPage = false } = {}) {
  if (!searchQuery.value.trim()) {
    resultStatus.value = "请输入 SPL 语句";
    return;
  }
  if (resetPage) {
    searchPage.value = 1;
  }
  isSearchLoading.value = true;
  resultStatus.value = "搜索执行中...";
  try {
    const response = await requestJSON(`/api/v1/search?${buildSearchParams()}`, { auth: true });
    try {
      await loadTimeline();
    } catch (error) {
      timelineBuckets.value = [];
      timelineStatus.value = `时间柱状图加载失败：${error.message}`;
    }
    resultMode.value = response.mode === "stats" ? "stats" : "events";
    updateSearchPagination(response.pagination);
    if (resultMode.value === "stats") {
      statsFields.value = response.stats?.fields?.length ? response.stats.fields : inferFields(response.stats?.rows || []);
      searchResults.value = response.stats?.rows || [];
      expandedEvents.value = new Set();
      searchTimeRangeText.value = formatResponseTimeRange(response.time_range);
      resultStatus.value = `统计视图 · ${formatNumber(searchPagination.value.total || searchPagination.value.returned)} 组 · ${searchTimeRangeText.value || "未限定时间"} · ${response.elapsed_ms ?? 0}ms`;
      return;
    }
    statsFields.value = [];
    searchResults.value = (response.events || []).map(apiEventToSearchRow);
    expandedEvents.value = new Set();
    searchTimeRangeText.value = formatResponseTimeRange(response.time_range);
    resultStatus.value = `事件视图 · ${formatNumber(searchPagination.value.total)} 个事件 · ${searchTimeRangeText.value || "未限定时间"} · ${response.elapsed_ms ?? 0}ms`;
  } catch (error) {
    statsFields.value = [];
    searchResults.value = [];
    expandedEvents.value = new Set();
    resultMode.value = "events";
    timelineBuckets.value = [];
    timelineStatus.value = "执行搜索后展示时间分布";
    searchPagination.value = { limit: searchPageSize.value, offset: 0, page: searchPage.value, returned: 0, hasMore: false, total: 0 };
    searchTimeRangeText.value = "";
    resultStatus.value = `搜索失败：${error.message}`;
  } finally {
    isSearchLoading.value = false;
  }
}
async function loadTimeline() {
  const response = await requestJSON(`/api/v1/search/timeline?${buildTimelineParams()}`, { auth: true });
  timelineBuckets.value = Array.isArray(response.buckets) ? response.buckets : [];
  timelineIntervalLabel.value = response.interval || "auto";
  timelineStatus.value = timelineBuckets.value.length ? "" : "当前搜索无时间分布数据";
}
function buildSearchParams() {
  const params = new URLSearchParams({ q: searchQuery.value.trim(), limit: String(searchPageSize.value), page: String(searchPage.value) });
  const range = resolveSearchTimeRange(searchTime.value);
  if (range.start) params.set("start_time", range.start);
  if (range.end) params.set("end_time", range.end);
  return params.toString();
}
function buildTimelineParams() {
  const params = new URLSearchParams(buildSearchParams());
  params.delete("limit");
  params.delete("page");
  params.set("interval", "auto");
  return params.toString();
}
function updateSearchPagination(pagination = {}) {
  const limit = Number(pagination.limit || searchPageSize.value);
  const page = Number(pagination.page || searchPage.value || 1);
  const returned = Number(pagination.returned ?? searchResults.value.length);
  searchPage.value = page > 0 ? page : 1;
  searchPagination.value = {
    limit,
    offset: Number(pagination.offset || 0),
    page: searchPage.value,
    returned,
    hasMore: Boolean(pagination.has_more ?? pagination.hasMore),
    total: Number(pagination.total ?? pagination.Total ?? returned)
  };
}
function runSearchFirstPage() { return runSearch({ resetPage: true }); }
function goSearchPage(page) {
  if (page < 1 || isSearchLoading.value) return;
  searchPage.value = page;
  return runSearch();
}
const timelineMax = computed(() => Math.max(0, ...timelineBuckets.value.map((bucket) => Number(bucket.count) || 0)));
const timelineBars = computed(() => {
  const max = timelineMax.value;
  return timelineBuckets.value.map((bucket) => {
    const count = Number(bucket.count) || 0;
    return { start: bucket.start || "", end: bucket.end || "", count, height: count > 0 && max > 0 ? Math.max(4, Math.round((count / max) * 100)) : 0 };
  });
});
const timelineYAxisLabels = computed(() => {
  const max = timelineMax.value;
  if (max <= 0) return ["0"];
  const labels = [max];
  const mid = Math.floor(max / 2);
  if (mid > 0 && mid !== max) labels.push(mid);
  labels.push(0);
  return labels.map(String);
});
const timelineTicks = computed(() => {
  if (!timelineBars.value.length) return [];
  const last = timelineBars.value.length - 1;
  const indexes = Array.from(new Set([0, Math.floor(last / 2), last])).sort((a, b) => a - b);
  return indexes.map((index) => ({ key: `${index}-${timelineBars.value[index].start}`, label: formatTimelineTick(timelineBars.value[index].start) }));
});
function timelineTooltip(bucket) {
  const end = bucket.end ? ` - ${bucket.end}` : "";
  return `${bucket.start}${end} · ${bucket.count} 个事件 · ${timelineIntervalLabel.value}`;
}
function resolveSearchTimeRange(label) {
  const now = new Date();
  if (label === "所有时间") return {};
  if (label === "昨天") {
    const start = startOfLocalDay(addDays(now, -1));
    const end = startOfLocalDay(now);
    return { start: start.toISOString(), end: end.toISOString() };
  }
  if (label === "近 7 天") return { start: addDays(now, -7).toISOString(), end: now.toISOString() };
  if (label === "近一个月") return { start: addDays(now, -30).toISOString(), end: now.toISOString() };
  if (label === "近一年") return { start: addDays(now, -365).toISOString(), end: now.toISOString() };
  if (label === "高级时间表达式") return { start: startOfLocalDay(now).toISOString(), end: now.toISOString() };
  return { start: addDays(now, -1).toISOString(), end: now.toISOString() };
}
function addDays(date, days) { return new Date(date.getTime() + days * 24 * 60 * 60 * 1000); }
function startOfLocalDay(date) { return new Date(date.getFullYear(), date.getMonth(), date.getDate()); }
function apiEventToSearchRow(item) {
  const metadata = item.metadata || {};
  const display = item.display || {};
  const detail = item.detail || {};
  return {
    id: item.event_id,
    time: display.time ? formatTime(new Date(display.time)) : (item.event_time ? formatTime(new Date(item.event_time)) : ""),
    event: display.event || item.raw || JSON.stringify(item.fields || {}),
    raw: detail.raw || item.raw || "",
    detailRows: normalizeDetailRows(detail.field_rows, item, metadata),
    source: metadata.source_name || item.source_name || item.source?.name || item.source?.type || "",
    sourcetype: metadata.sourcetype || item.sourcetype || "",
    index: metadata.index || item.index || "",
    parseStatus: metadata.parse_status || item.parse_status || "",
    parseRuleId: metadata.parse_rule_id || item.parse_rule_id || "",
    parseRuleName: metadata.parse_rule_name || item.parse_rule_name || "",
    parseError: metadata.parse_error || item.parse_error || "",
    parsedAt: metadata.parsed_at || item.parsed_at || ""
  };
}
function normalizeDetailRows(rows, item, metadata) {
  if (Array.isArray(rows) && rows.length) return rows;
  const out = [
    { category: "metadata", name: "index", value: metadata.index || item.index || "", type: "string" },
    { category: "metadata", name: "source", value: metadata.source || metadata.source_name || item.source?.name || "", type: "string" },
    { category: "metadata", name: "sourcetype", value: metadata.sourcetype || "", type: "string" },
    { category: "metadata", name: "parse_status", value: metadata.parse_status || "", type: "string" }
  ];
  Object.entries(item.fields || {}).forEach(([name, value]) => out.push({ category: "field", name, value, type: detailValueType(value) }));
  return out;
}
function eventRowKey(item, index) { return item.id || `row-${index}`; }
function isEventExpanded(item, index) { return expandedEvents.value.has(eventRowKey(item, index)); }
function toggleEventDetail(item, index) {
  const key = eventRowKey(item, index);
  const next = new Set(expandedEvents.value);
  if (next.has(key)) next.delete(key);
  else next.add(key);
  expandedEvents.value = next;
}
function formatDetailValue(value) {
  if (value == null) return "";
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}
function detailValueType(value) {
  if (typeof value === "number") return "number";
  if (typeof value === "boolean") return "bool";
  if (value && typeof value === "object") return "json";
  return "string";
}
function inferFields(rows) { return Array.from(rows.reduce((set, row) => { Object.keys(row || {}).forEach((key) => set.add(key)); return set; }, new Set())); }
function formatStatsCell(_field, value) {
  if (value == null) return "";
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}
function matchesRecord(record, segment) {
  const cleaned = segment.replace(/\bearliest=[^\s]+/ig, "").replace(/\blatest=[^\s]+/ig, "").replace(/^search\s+/i, "").trim();
  if (!cleaned) return true;
  return (cleaned.match(/"[^"]+"|'[^']+'|\S+/g) || []).every((token) => {
    const clause = token.match(/^([\w.-]+)\s*(!=|=)\s*(.+)$/);
    if (clause) { const [, field, operator, rawValue] = clause; const actual = String(record[field] ?? "").toLowerCase(); const expected = stripQuotes(rawValue).toLowerCase(); return operator === "=" ? actual === expected : actual !== expected; }
    return Object.values(record).join(" ").toLowerCase().includes(stripQuotes(token).toLowerCase());
  });
}
function aggregateStats(records, statsSegment) {
  const byMatch = statsSegment.match(/\bby\b\s+(.+)$/i);
  const groupFields = byMatch ? byMatch[1].trim().split(/\s+/).filter(Boolean) : [];
  const metric = statsSegment.match(/count(?:\s+as\s+([\w.-]+))?/i)?.[1] || "count";
  const groups = new Map();
  records.forEach((record) => { const group = groupFields.length ? groupFields.map((field) => `${field}=${record[field] ?? ""}`).join(" ") : "all"; if (!groups.has(group)) groups.set(group, []); groups.get(group).push(record); });
  return Array.from(groups.entries()).map(([group, rows]) => ({ group, value: rows.length, metric, sample: rows[0]?.event || "—" }));
}
async function toggleSavedSearches() {
  savedOpen.value = !savedOpen.value;
  if (savedOpen.value) await loadSavedSearches();
}
async function loadSavedSearches() {
  savedSearchError.value = "";
  try {
    const payload = await requestJSON("/api/v1/search/favorites", { auth: true });
    if (Array.isArray(payload.saved_searches)) savedSearches.value = payload.saved_searches.map(apiSavedSearchToForm);
    savedSearchesLoaded.value = true;
  } catch (error) {
    savedSearchError.value = error.message || "保存搜索加载失败";
  }
}
async function saveSearch() {
  const spl = searchQuery.value.trim();
  if (!spl) return;
  savedSearchError.value = "";
  try {
    const saved = await requestJSON("/api/v1/search/favorites", {
      auth: true,
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        name: spl.slice(0, 80),
        spl,
        time_range_type: searchTime.value
      })
    });
    const item = apiSavedSearchToForm(saved);
    savedSearches.value = [item, ...savedSearches.value.filter((current) => current.id !== item.id && (current.query !== item.query || current.time !== item.time))];
  } catch (error) {
    savedSearchError.value = error.message || "保存搜索失败";
  }
}
function useSearch(item) { searchQuery.value = item.query; searchTime.value = item.time; runSearchFirstPage(); }
async function deleteSavedSearch(id) {
  savedSearchError.value = "";
  try {
    await requestJSON(`/api/v1/search/favorites/${encodeURIComponent(id)}`, { auth: true, method: "DELETE" });
    savedSearches.value = savedSearches.value.filter((item) => item.id !== id);
  } catch (error) {
    savedSearchError.value = error.message || "保存搜索删除失败";
  }
}
function apiSavedSearchToForm(item) {
  return {
    id: item.id,
    query: item.spl || item.query || "",
    time: item.time_range_type || item.time || "近 1 天"
  };
}
function nextId(prefix) { return `${prefix}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`; }
function timeAgo(days = 0, hours = 0, minutes = 0) { return new Date(Date.now() - (((days * 24 + hours) * 60 + minutes) * 60 * 1000)); }
function pad2(value) { return String(value).padStart(2, "0"); }
function formatTime(date) { return `${pad2(date.getMonth() + 1)}/${pad2(date.getDate())} ${pad2(date.getHours())}:${pad2(date.getMinutes())}:${pad2(date.getSeconds())}`; }
const searchDateTimeFormatter = new Intl.DateTimeFormat("zh-CN", {
  timeZone: "Asia/Shanghai",
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hourCycle: "h23"
});
function formatFullDateTime(value) {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const parts = Object.fromEntries(searchDateTimeFormatter.formatToParts(date).map((part) => [part.type, part.value]));
  return `${parts.year}-${parts.month}-${parts.day} ${parts.hour}:${parts.minute}:${parts.second}`;
}
function formatResponseTimeRange(range) {
  if (!range) return "";
  const start = range.start_time || range.start;
  const end = range.end_time || range.end;
  const startText = formatFullDateTime(start);
  const endText = formatFullDateTime(end);
  if (!startText || !endText) return "";
  return `${startText} - ${endText}`;
}
function formatNumber(value) { return Number(value || 0).toLocaleString(); }
function formatTimelineTick(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return String(value || "").slice(0, 16);
  return `${pad2(date.getMonth() + 1)}/${pad2(date.getDate())} ${pad2(date.getHours())}:${pad2(date.getMinutes())}`;
}
</script>

<style scoped>
:global(*){box-sizing:border-box}:global(:root){--xdp-bg:#070925;--xdp-bg2:#12091f;--xdp-ink:#f8fbff;--xdp-muted:#a5b2d1;--xdp-line:rgba(151,173,255,.18);--xdp-glass:rgba(10,15,45,.78);--xdp-glass2:rgba(19,27,72,.82);--xdp-orange:#ffad00;--xdp-coral:#ff6848;--xdp-pink:#ff1f85;--xdp-cyan:#55dfff;--xdp-green:#67f28a;--xdp-danger:#ff5f61;--xdp-radius:22px;--xdp-shadow:0 24px 80px rgba(0,0,0,.42);--xdp-sans:"Avenir Next","PingFang SC","Microsoft YaHei",sans-serif;--xdp-mono:"SFMono-Regular","Menlo","Consolas",monospace}:global(body){margin:0;min-height:100vh;font-family:var(--xdp-sans);background:#070925}button,input,select,textarea{font:inherit}button{cursor:pointer}.login-shell,.console-shell{min-height:100vh;position:relative;overflow-x:hidden;color:var(--xdp-ink);background:radial-gradient(circle at 74% 8%,rgba(255,31,133,.24),transparent 24rem),radial-gradient(circle at 10% 28%,rgba(85,223,255,.15),transparent 26rem),linear-gradient(115deg,#071348 0%,#080a2b 44%,#15061e 100%)}.page-grid{position:absolute;inset:0;background:repeating-linear-gradient(90deg,transparent 0 78px,rgba(255,115,49,.13) 79px,transparent 81px),repeating-linear-gradient(0deg,transparent 0 138px,rgba(85,223,255,.045) 139px,transparent 141px);opacity:.58;pointer-events:none}.login-shell{display:grid;grid-template-rows:auto 1fr auto;gap:38px;padding:22px clamp(18px,4vw,56px) 28px}.topbar,.login-layout,footer,.console-page{position:relative;z-index:1}.topbar{min-height:62px;display:flex;align-items:center;gap:14px;border:1px solid var(--xdp-line);border-radius:999px;padding:0 18px 0 24px;background:rgba(5,8,30,.66);backdrop-filter:blur(18px);box-shadow:0 18px 58px rgba(0,0,0,.3)}.login-shell>.topbar{width:min(1500px,100%);margin:0 auto}.brand{display:flex;align-items:center;gap:9px;margin-right:auto;color:#fff;font-size:22px;font-weight:500;letter-spacing:-.03em}.brand-mark{display:grid;place-items:center;width:32px;height:32px;border-radius:9px;background:linear-gradient(135deg,var(--xdp-orange),var(--xdp-pink));color:#12071c;font-weight:600;box-shadow:0 0 30px rgba(255,78,86,.38)}.pill{border:1px solid rgba(255,255,255,.13);border-radius:999px;padding:8px 12px;color:#e8eeff;font:700 12px var(--xdp-mono);letter-spacing:.08em}.muted,.status-line,.result-meta,.note,.form-hint{color:var(--xdp-muted)}.login-layout{width:min(1280px,100%);margin:auto;display:grid;grid-template-columns:minmax(0,1.08fr) minmax(380px,480px);gap:28px;align-items:stretch}.hero-card,.login-card,.card,.main-panel{border:1px solid var(--xdp-line);border-radius:var(--xdp-radius);background:linear-gradient(180deg,rgba(20,29,76,.82),rgba(6,10,35,.74));box-shadow:var(--xdp-shadow);backdrop-filter:blur(18px)}.hero-card{min-height:520px;position:relative;display:flex;flex-direction:column;justify-content:center;overflow:hidden;padding:clamp(30px,5vw,58px)}.hero-card:after{content:"";position:absolute;right:-92px;bottom:-76px;width:360px;height:260px;border:1px solid rgba(255,255,255,.08);border-radius:60px;background:linear-gradient(135deg,rgba(255,173,0,.12),rgba(255,31,133,.14));transform:rotate(18deg)}.eyebrow{margin:0;color:#c8d4f5;font:700 13px var(--xdp-mono);letter-spacing:.1em;text-transform:uppercase}.hero-card h1{position:relative;z-index:1;margin:18px 0 0;display:grid;gap:12px}.gradient-text{display:inline-block;width:max-content;padding-right:.18em;background:linear-gradient(90deg,var(--xdp-orange),var(--xdp-coral) 46%,var(--xdp-pink));-webkit-background-clip:text;background-clip:text;color:transparent;font-size:clamp(48px,6.4vw,84px);font-weight:700;line-height:1;letter-spacing:-.025em;text-shadow:0 18px 70px rgba(255,54,117,.26)}.hero-card strong{max-width:650px;color:#fff;font-size:clamp(24px,2.8vw,36px);font-weight:700;line-height:1.16;letter-spacing:-.05em}.lede{position:relative;z-index:1;max-width:560px;margin:24px 0 0;color:var(--xdp-muted);font-size:17px;line-height:1.7}.chip-row{position:relative;z-index:1;display:flex;flex-wrap:wrap;gap:10px;margin-top:34px}.chip-row span,.count,.badge,.mode-pill{border:1px solid rgba(85,223,255,.24);border-radius:999px;background:rgba(85,223,255,.12);color:#dffaff;padding:4px 9px;font-size:12px;font-weight:800}.chip-row span{border-color:rgba(255,255,255,.14);background:rgba(255,255,255,.07);color:#e8eeff;padding:8px 12px}.login-card{align-self:center;min-height:470px;padding:28px}.card-head,.result-head{display:flex;align-items:flex-start;justify-content:space-between;gap:18px;margin-bottom:16px;color:#fff;font-weight:800}.login-card h2{margin:8px 0 0;color:#fff;font-size:30px;line-height:1.1;letter-spacing:-.04em}.status-dot{width:14px;height:14px;margin-top:4px;border-radius:999px;background:var(--xdp-green);box-shadow:0 0 0 7px rgba(103,242,138,.1),0 0 28px rgba(103,242,138,.68)}.login-form,.form-grid{display:grid;gap:16px}.login-form label,.form-grid label{display:grid;gap:8px;color:#dce5fb;font-size:13px;font-weight:700}.login-form input,.field,.select,textarea,.search-box{width:100%;border:1px solid rgba(255,255,255,.12);border-radius:14px;outline:none;padding:0 16px;color:#fff;background:rgba(1,4,22,.52);transition:border-color .16s ease,box-shadow .16s ease,background .16s ease}.login-form input,.field,.select{height:44px}.login-form input{height:56px}textarea{min-height:106px;padding:12px 14px;resize:vertical;font-family:var(--xdp-mono)}.props-editor{min-height:150px}.login-form input:focus,.field:focus,.select:focus,textarea:focus,.search-box:focus{border-color:rgba(85,223,255,.78);background:rgba(3,9,34,.74);box-shadow:0 0 0 4px rgba(85,223,255,.1)}.login-form button,.btn,.logout,.topbar-nav button{border:0;cursor:pointer;font-weight:700}.login-form button{height:56px;margin-top:6px;border-radius:14px;background:linear-gradient(90deg,var(--xdp-orange),var(--xdp-coral) 46%,var(--xdp-pink));color:#14071c;font-size:18px;box-shadow:0 18px 42px rgba(255,57,116,.26)}.error-box{margin:16px 0 0;border:1px solid rgba(255,95,97,.35);border-radius:14px;padding:10px 12px;color:#ffcbc6;background:rgba(255,95,97,.1)}.btn.ghost,.logout{border:1px solid rgba(85,223,255,.28);color:#dffaff;background:rgba(85,223,255,.1)}footer{justify-self:center;color:#7f8fb7;font-size:13px}.console-page{width:min(1500px,calc(100vw - 44px));margin:0 auto;padding:22px 0 56px}.console-topbar{position:sticky;top:16px;z-index:20}.console-shell .brand{margin-right:0;flex:0 0 auto}.topbar-nav{display:flex;gap:6px;margin-left:4px;margin-right:auto}.topbar-nav button{color:#b7c2df;background:transparent}.topbar-nav button{border-radius:999px;padding:8px 12px;font-size:14px}.topbar-nav button.active,.topbar-nav button:hover{color:#fff;background:rgba(85,223,255,.12)}.user{color:#bdc9e8;font-size:13px}.logout{border-radius:999px;padding:8px 12px}.workspace{display:block;margin-top:28px}.main-panel{min-width:0;padding:22px}.panel-header{display:flex;align-items:center;justify-content:space-between;gap:16px;margin-bottom:18px}.panel-header h2{margin:0;font-size:30px;letter-spacing:-.04em}.content-grid{display:grid;grid-template-columns:minmax(360px,.9fr) minmax(460px,1.1fr);gap:18px}.card{min-width:0;padding:18px}.plugin-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}.plugin-card{min-height:80px;display:flex;align-items:center;gap:10px;border:1px solid rgba(255,255,255,.12);border-radius:18px;color:#dce5fb;background:rgba(255,255,255,.055);padding:14px;font-weight:800}.plugin-card.active{border-color:rgba(85,223,255,.78);color:#fff;background:rgba(85,223,255,.12);box-shadow:0 0 30px rgba(85,223,255,.1)}.plugin-icon{display:grid;place-items:center;width:38px;height:38px;border-radius:12px;color:#100b22;background:linear-gradient(135deg,var(--xdp-cyan),var(--xdp-green));font:800 12px var(--xdp-mono)}.param-panel,.conditional-panel,.advanced-panel,.preview-box,.saved-drawer{border:1px solid rgba(255,255,255,.1);border-radius:18px;background:rgba(255,255,255,.045);padding:14px}.two{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.unit-field{display:grid;grid-template-columns:1fr 46px;align-items:center}.unit-field .field{border-radius:14px 0 0 14px}.unit-field span{display:grid;place-items:center;height:44px;border:1px solid rgba(255,255,255,.12);border-left:0;border-radius:0 14px 14px 0;color:var(--xdp-muted);background:rgba(255,255,255,.06)}.actions{display:flex;flex-wrap:wrap;gap:10px}.btn{min-height:40px;border-radius:999px;padding:0 16px;color:#071127;background:linear-gradient(135deg,var(--xdp-cyan),var(--xdp-green));box-shadow:0 14px 34px rgba(85,223,255,.18)}.table-wrap{width:100%;overflow-x:auto}table{width:100%;border-collapse:collapse;color:#e7edff;font-size:13px}th,td{border-bottom:1px solid rgba(255,255,255,.08);padding:12px 10px;vertical-align:top;text-align:left}th{color:#c8d4f5;background:rgba(255,255,255,.05)}tr:hover td{background:rgba(85,223,255,.045)}code,.search-box{font-family:var(--xdp-mono)}.multiline-code{white-space:pre-wrap}.muted-code{color:var(--xdp-muted)}.row-actions{display:flex;gap:8px}.link-btn{border:0;padding:0;color:var(--xdp-cyan);background:transparent;font-weight:800}.link-btn.delete{color:#ff9ea0}.search-layout{display:grid;gap:16px}.search-row{display:grid;grid-template-columns:minmax(320px,1fr) 180px auto auto;gap:10px;align-items:center}.search-box{min-height:46px;resize:none;line-height:24px;padding:10px 16px;overflow:auto}.time-help{color:var(--xdp-muted)}.timeline{height:118px;display:flex;align-items:end;gap:8px;border:1px solid rgba(255,255,255,.09);border-radius:18px;padding:14px;background:repeating-linear-gradient(0deg,transparent 0 20px,rgba(255,255,255,.045) 21px,transparent 22px),rgba(255,255,255,.035)}.bar{flex:1;min-width:12px;border-radius:10px 10px 2px 2px;background:linear-gradient(180deg,var(--xdp-cyan),var(--xdp-pink))}.search-toolbar{display:flex;justify-content:space-between;align-items:center;gap:12px}.saved-summary{display:flex;align-items:center;gap:10px;border:1px solid rgba(255,255,255,.1);border-radius:999px;padding:8px 12px;background:rgba(255,255,255,.045);color:var(--xdp-muted)}.saved-summary strong,.result-head>div>span{color:#fff}.drawer-head{display:flex;justify-content:space-between;margin-bottom:10px;font-weight:800}@media (max-width:980px){.content-grid,.login-layout{grid-template-columns:1fr}.topbar-nav{flex-wrap:wrap;margin-left:0}.search-row{grid-template-columns:1fr}}@media (max-width:720px){.login-shell{gap:22px;padding:14px 14px 22px}.topbar{min-height:auto;align-items:flex-start;border-radius:24px;padding:14px}.pill,.user{display:none}.hero-card{min-height:auto;padding:24px}.gradient-text{font-size:clamp(44px,13vw,64px)}.hero-card strong{font-size:22px}.login-card{min-height:auto;padding:22px}.console-page{width:min(100% - 24px,1500px)}.two,.plugin-grid{grid-template-columns:1fr}}

.console-shell[data-theme="ops-console"]{--ops-bg:#f4f7fb;--ops-surface:#ffffff;--ops-surface-soft:#f8fbfd;--ops-ink:#1f2d3d;--ops-muted:#657589;--ops-line:#d9e2ea;--ops-topbar:#18212a;--ops-sidebar:#eef3f7;--ops-primary:#13bfb4;--ops-primary-dark:#0f8f89;--ops-blue:#2878b8;--ops-green:#28b76f;--ops-shadow:0 18px 48px rgba(29,49,70,.12);color:var(--ops-ink);background:linear-gradient(135deg,#eef4f7 0%,#f8fbfc 46%,#edf5f3 100%)}.console-shell[data-theme="ops-console"] .page-grid{background:repeating-linear-gradient(90deg,transparent 0 94px,rgba(23,129,138,.08) 95px,transparent 96px),repeating-linear-gradient(0deg,transparent 0 128px,rgba(40,120,184,.055) 129px,transparent 130px);opacity:.85}.console-shell[data-theme="ops-console"] .console-topbar{border-color:#24313d;background:var(--ops-topbar);box-shadow:0 12px 28px rgba(24,33,42,.22)}.console-shell[data-theme="ops-console"] .brand,.console-shell[data-theme="ops-console"] .user{color:#edf6fb}.console-shell[data-theme="ops-console"] .console-brand-mark{background:linear-gradient(135deg,var(--ops-primary),var(--ops-blue));color:#fff;box-shadow:0 0 0 1px rgba(255,255,255,.12),0 10px 24px rgba(19,191,180,.28)}.console-shell[data-theme="ops-console"] .topbar-nav button{color:#c8d4df}.console-shell[data-theme="ops-console"] .topbar-nav button.active,.console-shell[data-theme="ops-console"] .topbar-nav button:hover{color:#fff;background:rgba(19,191,180,.18)}.console-shell[data-theme="ops-console"] .logout{border-color:rgba(19,191,180,.38);color:#e7fffb;background:rgba(19,191,180,.12)}.console-shell[data-theme="ops-console"] .sidebar,.console-shell[data-theme="ops-console"] .main-panel,.console-shell[data-theme="ops-console"] .card{border-color:var(--ops-line);background:rgba(255,255,255,.94);box-shadow:var(--ops-shadow);backdrop-filter:none}.console-shell[data-theme="ops-console"] .sidebar-title{color:var(--ops-muted)}.console-shell[data-theme="ops-console"] .sidebar button{color:#435365}.console-shell[data-theme="ops-console"] .sidebar button.active,.console-shell[data-theme="ops-console"] .sidebar button:hover{color:#0d766f;background:#dff7f4}.console-shell[data-theme="ops-console"] .panel-header h2{display:flex;align-items:center;gap:10px;color:#162635}.console-shell[data-theme="ops-console"] .page-icon{display:grid;place-items:center;width:34px;height:34px;border-radius:10px;color:#fff;font:800 11px var(--xdp-mono);letter-spacing:.04em;box-shadow:0 10px 24px rgba(19,191,180,.2)}.console-shell[data-theme="ops-console"] .page-icon-collect{background:linear-gradient(135deg,#0fb7a9,#28b76f)}.console-shell[data-theme="ops-console"] .page-icon-parse{background:linear-gradient(135deg,#2878b8,#2ab7ca)}.console-shell[data-theme="ops-console"] .page-icon-index{background:linear-gradient(135deg,#1f6fa4,#4f86d9)}.console-shell[data-theme="ops-console"] .page-icon-search{background:linear-gradient(135deg,#0f8f89,#1f6fa4)}.console-shell[data-theme="ops-console"] .badge,.console-shell[data-theme="ops-console"] .count,.console-shell[data-theme="ops-console"] .mode-pill{border-color:#b9e8e4;background:#e4f8f5;color:#08776f}.console-shell[data-theme="ops-console"] .card-head,.console-shell[data-theme="ops-console"] .result-head{color:#1f2d3d}.console-shell[data-theme="ops-console"] .muted,.console-shell[data-theme="ops-console"] .status-line,.console-shell[data-theme="ops-console"] .result-meta,.console-shell[data-theme="ops-console"] .note,.console-shell[data-theme="ops-console"] .form-hint{color:var(--ops-muted)}.console-shell[data-theme="ops-console"] .form-grid label{color:#344558}.console-shell[data-theme="ops-console"] .field,.console-shell[data-theme="ops-console"] .select,.console-shell[data-theme="ops-console"] textarea,.console-shell[data-theme="ops-console"] .search-box{border-color:#cfd9e3;color:#1c2c3d;background:#fff}.console-shell[data-theme="ops-console"] .field:focus,.console-shell[data-theme="ops-console"] .select:focus,.console-shell[data-theme="ops-console"] textarea:focus,.console-shell[data-theme="ops-console"] .search-box:focus{border-color:var(--ops-primary);background:#fff;box-shadow:0 0 0 4px rgba(19,191,180,.14)}.console-shell[data-theme="ops-console"] .param-panel,.console-shell[data-theme="ops-console"] .conditional-panel,.console-shell[data-theme="ops-console"] .advanced-panel,.console-shell[data-theme="ops-console"] .preview-box,.console-shell[data-theme="ops-console"] .saved-drawer{border-color:#d9e4ec;background:#f8fbfd}.console-shell[data-theme="ops-console"] .plugin-card{border-color:#d6e1e9;color:#243447;background:#fff}.console-shell[data-theme="ops-console"] .plugin-card.active{border-color:var(--ops-primary);color:#0d4d4b;background:#e8fbf8;box-shadow:0 10px 28px rgba(19,191,180,.16)}.console-shell[data-theme="ops-console"] .plugin-icon{color:#fff;box-shadow:0 8px 20px rgba(40,120,184,.16)}.console-shell[data-theme="ops-console"] .icon-syslog{background:linear-gradient(135deg,#0fb7a9,#28b76f)}.console-shell[data-theme="ops-console"] .icon-kafka{background:linear-gradient(135deg,#2878b8,#38bdf8)}.console-shell[data-theme="ops-console"] .icon-regex{background:linear-gradient(135deg,#1f6fa4,#2ab7ca)}.console-shell[data-theme="ops-console"] .icon-json{background:linear-gradient(135deg,#13bfb4,#28b76f)}.console-shell[data-theme="ops-console"] .icon-delimited{background:linear-gradient(135deg,#4f86d9,#2ab7ca)}.console-shell[data-theme="ops-console"] .icon-kv{background:linear-gradient(135deg,#0f8f89,#4f86d9)}.console-shell[data-theme="ops-console"] .btn{color:#fff;background:linear-gradient(135deg,var(--ops-primary),var(--ops-green));box-shadow:0 12px 24px rgba(19,191,180,.22)}.console-shell[data-theme="ops-console"] .btn.ghost{border-color:#b8e5e1;color:#08776f;background:#eefbf9}.console-shell[data-theme="ops-console"] table{color:#223246}.console-shell[data-theme="ops-console"] th{color:#405168;background:#edf3f7}.console-shell[data-theme="ops-console"] td{border-color:#e5ebf1}.console-shell[data-theme="ops-console"] tr:hover td{background:#f1fbfa}.console-shell[data-theme="ops-console"] code{color:#0f6378}.console-shell[data-theme="ops-console"] .link-btn{color:#087eab}.console-shell[data-theme="ops-console"] .link-btn.delete{color:#c2410c}.console-shell[data-theme="ops-console"] .unit-field span{border-color:#cfd9e3;color:var(--ops-muted);background:#edf3f7}.console-shell[data-theme="ops-console"] .time-help{color:var(--ops-muted)}.console-shell[data-theme="ops-console"] .timeline{border-color:#d8e4ec;background:repeating-linear-gradient(0deg,transparent 0 20px,rgba(40,120,184,.08) 21px,transparent 22px),#fff}.console-shell[data-theme="ops-console"] .bar{background:linear-gradient(180deg,#2878b8,#13bfb4 62%,#28b76f)}.console-shell[data-theme="ops-console"] .saved-summary{border-color:#d9e4ec;background:#fff;color:var(--ops-muted)}.console-shell[data-theme="ops-console"] .saved-summary strong,.console-shell[data-theme="ops-console"] .result-head>div>span{color:#1f2d3d}
.login-shell[data-theme="ops-login"]{--ops-bg:#f4f7fb;--ops-surface:#ffffff;--ops-surface-soft:#f8fbfd;--ops-ink:#1f2d3d;--ops-muted:#657589;--ops-line:#d9e2ea;--ops-topbar:#18212a;--ops-primary:#13bfb4;--ops-primary-dark:#0f8f89;--ops-blue:#2878b8;--ops-green:#28b76f;--ops-shadow:0 18px 48px rgba(29,49,70,.12);color:var(--ops-ink);background:linear-gradient(135deg,#eef4f7 0%,#f8fbfc 46%,#edf5f3 100%)}.login-shell[data-theme="ops-login"] .page-grid{background:repeating-linear-gradient(90deg,transparent 0 94px,rgba(23,129,138,.08) 95px,transparent 96px),repeating-linear-gradient(0deg,transparent 0 128px,rgba(40,120,184,.055) 129px,transparent 130px);opacity:.85}.login-shell[data-theme="ops-login"] .topbar{border-color:#24313d;background:var(--ops-topbar);box-shadow:0 12px 28px rgba(24,33,42,.22);backdrop-filter:none}.login-shell[data-theme="ops-login"] .brand{color:#edf6fb}.login-shell[data-theme="ops-login"] .brand-mark{background:linear-gradient(135deg,var(--ops-primary),var(--ops-blue));color:#fff;box-shadow:0 0 0 1px rgba(255,255,255,.12),0 10px 24px rgba(19,191,180,.28)}.login-shell[data-theme="ops-login"] .pill{border-color:rgba(19,191,180,.38);color:#e7fffb;background:rgba(19,191,180,.12)}.login-shell[data-theme="ops-login"] .pill.muted{color:#c8d4df}.login-shell[data-theme="ops-login"] .hero-card,.login-shell[data-theme="ops-login"] .login-card{border-color:var(--ops-line);background:rgba(255,255,255,.94);box-shadow:var(--ops-shadow);backdrop-filter:none}.login-shell[data-theme="ops-login"] .hero-card:after{border-color:rgba(19,191,180,.18);background:linear-gradient(135deg,rgba(19,191,180,.18),rgba(40,120,184,.12))}.login-shell[data-theme="ops-login"] .eyebrow{color:var(--ops-muted)}.login-shell[data-theme="ops-login"] .gradient-text{background:linear-gradient(90deg,var(--ops-primary),var(--ops-blue) 58%,var(--ops-green));-webkit-background-clip:text;background-clip:text;color:transparent;text-shadow:0 18px 70px rgba(19,191,180,.16)}.login-shell[data-theme="ops-login"] .hero-card strong,.login-shell[data-theme="ops-login"] .login-card h2{color:#162635}.login-shell[data-theme="ops-login"] .lede,.login-shell[data-theme="ops-login"] footer{color:var(--ops-muted)}.login-shell[data-theme="ops-login"] .chip-row span{border-color:var(--ops-line);color:#315567;background:#fff}.login-shell[data-theme="ops-login"] .login-form label{color:#344558}.login-shell[data-theme="ops-login"] .login-form input{border-color:#cfd9e3;color:#1c2c3d;background:#fff}.login-shell[data-theme="ops-login"] .login-form input::placeholder{color:#9aa8b7}.login-shell[data-theme="ops-login"] .login-form input:focus{border-color:var(--ops-primary);background:#fff;box-shadow:0 0 0 4px rgba(19,191,180,.14)}.login-shell[data-theme="ops-login"] .login-form button{color:#fff;background:linear-gradient(135deg,var(--ops-primary),var(--ops-green));box-shadow:0 14px 28px rgba(19,191,180,.24)}.login-shell[data-theme="ops-login"] .login-form button:hover{box-shadow:0 18px 34px rgba(19,191,180,.3)}.login-shell[data-theme="ops-login"] .login-form button:focus-visible,.login-shell[data-theme="ops-login"] .btn.ghost:focus-visible{outline:3px solid rgba(19,191,180,.28);outline-offset:3px}.login-shell[data-theme="ops-login"] .status-dot{background:var(--ops-green);box-shadow:0 0 0 7px rgba(40,183,111,.12),0 0 28px rgba(40,183,111,.46)}.login-shell[data-theme="ops-login"] .error-box{border-color:rgba(220,91,75,.28);color:#b43f32;background:rgba(220,91,75,.08)}.login-shell[data-theme="ops-login"] .btn.ghost{border-color:#b8e5e1;color:#08776f;background:#eefbf9}.timeline.empty{align-items:center;justify-content:center}.timeline-empty{color:var(--xdp-muted);font-weight:800}.bar{display:flex;align-items:flex-start;justify-content:center;padding-top:5px;color:#f6fbff;font:800 11px var(--xdp-mono)}.bar span{opacity:.9}.console-shell[data-theme="ops-console"] .bar{color:#fff}
.timeline.timeline-shell{height:156px;display:grid;grid-template-columns:46px minmax(0,1fr);align-items:stretch;gap:10px;padding:12px 14px 10px}.timeline.timeline-shell.empty{display:grid;grid-template-columns:1fr}.timeline-y-axis{display:flex;flex-direction:column;justify-content:space-between;padding:3px 0 24px;text-align:right;color:var(--xdp-muted);font:800 11px var(--xdp-mono)}.timeline-plot{min-width:0;display:grid;grid-template-rows:1fr 22px;position:relative}.timeline-bars{display:flex;align-items:end;gap:3px;min-height:104px;border-left:1px solid rgba(255,255,255,.13);border-bottom:1px solid rgba(255,255,255,.16);padding:0 4px}.timeline-x-axis{display:flex;justify-content:space-between;gap:8px;padding-top:7px;color:var(--xdp-muted);font:800 11px var(--xdp-mono);white-space:nowrap}.timeline.timeline-shell .bar{flex:1;min-width:3px;padding-top:0;border-radius:3px 3px 0 0}.timeline.timeline-shell.empty .timeline-plot{display:grid;place-items:center}.console-shell[data-theme="ops-console"] .timeline-y-axis,.console-shell[data-theme="ops-console"] .timeline-x-axis{color:var(--ops-muted)}.console-shell[data-theme="ops-console"] .timeline-bars{border-left-color:#d4e0e8;border-bottom-color:#c9d8e3}
.expand-col{width:34px}.expand-toggle{border:0;background:transparent;color:var(--xdp-cyan);font:800 13px var(--xdp-mono);cursor:pointer}.event-detail-row:hover td{background:transparent}.event-detail{display:grid;gap:12px;border:1px solid rgba(255,255,255,.1);border-radius:16px;padding:12px;background:rgba(255,255,255,.04)}.detail-raw{display:grid;gap:6px}.detail-raw span{color:var(--xdp-muted);font-size:12px;font-weight:800;text-transform:uppercase}.console-shell[data-theme="ops-console"] .expand-toggle{color:#087eab}.console-shell[data-theme="ops-console"] .event-detail{border-color:#d9e4ec;background:#f8fbfd}.console-shell[data-theme="ops-console"] .detail-raw span{color:var(--ops-muted)}
.pagination-bar{display:flex;align-items:center;justify-content:flex-end;gap:16px;margin:0 -18px -18px;padding:18px 22px 20px;border-top:1px solid rgba(255,255,255,.1);border-radius:0 0 var(--xdp-radius) var(--xdp-radius);background:rgba(255,255,255,.035)}.pagination-controls{display:flex;align-items:center;justify-content:flex-end;gap:10px}.pager-arrow,.pager-page{display:grid;place-items:center;min-width:36px;height:36px;border:1px solid transparent;border-radius:6px;background:transparent;color:var(--xdp-ink);font-weight:800}.pager-arrow{font-size:24px}.pager-page{font-size:16px}.pager-ellipsis{display:grid;place-items:center;min-width:24px;height:36px;color:#7c8796;font-weight:800;letter-spacing:2px}.pager-arrow:not(:disabled):hover,.pager-page:hover{border-color:rgba(255,173,0,.45);background:rgba(255,173,0,.08);color:var(--xdp-orange)}.pager-page.active{border-color:var(--xdp-orange);background:rgba(255,173,0,.1);color:var(--xdp-orange);box-shadow:0 0 0 3px rgba(255,173,0,.08)}.pager-arrow:disabled{color:rgba(165,178,209,.45);cursor:not-allowed}.page-size-select .select{min-width:132px;height:40px;border-radius:8px;padding:0 14px}.console-shell[data-theme="ops-console"] .pagination-bar{border-top-color:#e5ebf1;background:#fff}.console-shell[data-theme="ops-console"] .pager-arrow,.console-shell[data-theme="ops-console"] .pager-page{color:#172638}.console-shell[data-theme="ops-console"] .pager-ellipsis{color:#8996a3}.console-shell[data-theme="ops-console"] .pager-arrow:not(:disabled):hover,.console-shell[data-theme="ops-console"] .pager-page:hover{border-color:rgba(255,122,26,.45);background:#fff5ec;color:#ff7a1a}.console-shell[data-theme="ops-console"] .pager-page.active{border-color:#ff7a1a;background:#fff;color:#ff7a1a;box-shadow:0 0 0 3px rgba(255,122,26,.08)}.console-shell[data-theme="ops-console"] .pager-arrow:disabled{color:#c3cbd2}.console-shell[data-theme="ops-console"] .page-size-select .select{border-color:#cfd9e3;background:#fff;color:#172638}@media (max-width:720px){.pagination-bar{align-items:flex-start;flex-direction:column}.pagination-controls{justify-content:flex-start;flex-wrap:wrap}}
.parser-plugin-grid{grid-template-columns:repeat(4,minmax(0,1fr))}
.collect-runtime-row{cursor:pointer}.collect-runtime-row.selected td{background:rgba(85,223,255,.08)}.collect-runtime-row.abnormal td{box-shadow:inset 3px 0 0 var(--xdp-danger)}.status-pill{display:inline-flex;align-items:center;width:max-content;border:1px solid rgba(85,223,255,.24);border-radius:999px;padding:4px 9px;font-size:12px;font-weight:800;color:#dffaff;background:rgba(85,223,255,.12)}.status-pill.runtime-running{border-color:rgba(103,242,138,.34);color:#dfffe9;background:rgba(103,242,138,.12)}.status-pill.runtime-stopped{border-color:rgba(165,178,209,.28);color:#d8e0f5;background:rgba(165,178,209,.1)}.status-pill.runtime-error{border-color:rgba(255,95,97,.34);color:#ffd4d2;background:rgba(255,95,97,.12)}.runtime-detail-card{margin-top:14px;border:1px solid rgba(255,255,255,.1);border-radius:18px;padding:14px;background:rgba(255,255,255,.045)}.runtime-detail-head{display:flex;align-items:flex-start;justify-content:space-between;gap:12px;margin-bottom:12px}.runtime-detail-head div{display:grid;gap:4px}.runtime-detail-head strong{color:#fff;font-size:16px}.runtime-detail-head span:not(.status-pill){color:var(--xdp-muted);font-size:12px}.runtime-detail-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}.runtime-detail-grid>div{display:grid;gap:6px;border:1px solid rgba(255,255,255,.09);border-radius:14px;padding:12px;background:rgba(1,4,22,.22)}.runtime-detail-grid span{color:var(--xdp-muted);font-size:12px;font-weight:800}.runtime-detail-grid strong{color:#fff;word-break:break-all}.runtime-detail-grid small{color:var(--xdp-muted);word-break:break-all}.runtime-detail-grid .topology{grid-column:1/-1}
.console-shell[data-theme="ops-console"] .collect-runtime-row.selected td{background:#e9fbf8}.console-shell[data-theme="ops-console"] .collect-runtime-row.abnormal td{box-shadow:inset 3px 0 0 #dc5b4b}.console-shell[data-theme="ops-console"] .status-pill.runtime-running{border-color:#b8ead5;color:#0f7a50;background:#e6f8ef}.console-shell[data-theme="ops-console"] .status-pill.runtime-stopped{border-color:#d6e1e9;color:#526174;background:#f4f7fb}.console-shell[data-theme="ops-console"] .status-pill.runtime-error{border-color:#f0c4bd;color:#b43f32;background:#fff1ef}.console-shell[data-theme="ops-console"] .runtime-detail-card{border-color:#d9e4ec;background:#f8fbfd}.console-shell[data-theme="ops-console"] .runtime-detail-head strong,.console-shell[data-theme="ops-console"] .runtime-detail-grid strong{color:#1f2d3d}.console-shell[data-theme="ops-console"] .runtime-detail-head span:not(.status-pill),.console-shell[data-theme="ops-console"] .runtime-detail-grid span,.console-shell[data-theme="ops-console"] .runtime-detail-grid small{color:var(--ops-muted)}.console-shell[data-theme="ops-console"] .runtime-detail-grid>div{border-color:#d9e4ec;background:#fff}
@media (max-width:720px){.parser-plugin-grid{grid-template-columns:1fr}}
@media (max-width:720px){.runtime-detail-grid{grid-template-columns:1fr}}
.panel-header-actions{display:flex;align-items:center;justify-content:flex-end;gap:10px;margin-left:auto}
.plugin-type-tabs{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:10px;margin-bottom:16px}.plugin-type-tabs button{display:flex;align-items:center;justify-content:space-between;gap:10px;border:1px solid rgba(255,255,255,.12);border-radius:16px;padding:14px;color:#dce5fb;background:rgba(255,255,255,.055);font-weight:800}.plugin-type-tabs button.active{border-color:rgba(85,223,255,.78);color:#fff;background:rgba(85,223,255,.12);box-shadow:0 0 30px rgba(85,223,255,.1)}.plugin-type-tabs small{display:grid;place-items:center;min-width:28px;height:24px;border-radius:999px;background:rgba(85,223,255,.14);color:inherit}.plugin-upload-panel{display:grid;grid-template-columns:minmax(260px,1fr) auto minmax(180px,auto);align-items:end;gap:12px}.console-shell[data-theme="ops-console"] .page-icon-plugins{background:linear-gradient(135deg,#13bfb4,#4f86d9)}.console-shell[data-theme="ops-console"] .plugin-type-tabs button{border-color:#d6e1e9;color:#243447;background:#fff}.console-shell[data-theme="ops-console"] .plugin-type-tabs button.active{border-color:var(--ops-primary);color:#0d4d4b;background:#e8fbf8;box-shadow:0 10px 28px rgba(19,191,180,.16)}.console-shell[data-theme="ops-console"] .plugin-type-tabs small{background:#dff7f4;color:#08776f}
.config-drawer{position:fixed;z-index:40;top:104px;right:max(22px,calc((100vw - 1500px)/2 + 22px));width:min(560px,calc(100vw - 44px));max-height:calc(100vh - 132px);overflow:auto;animation:drawerSlideIn .18s ease-out}.config-drawer:before{content:"";position:fixed;inset:0;z-index:-1;background:rgba(12,22,32,.18);pointer-events:none}.content-grid{grid-template-columns:1fr}@keyframes drawerSlideIn{from{transform:translateX(28px);opacity:.72}to{transform:translateX(0);opacity:1}}@media (max-width:720px){.config-drawer{top:0;right:0;bottom:0;width:100vw;max-height:none;border-radius:0;overflow:auto}}
.content-grid.list-first{grid-template-columns:1fr}
</style>
