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
          <template v-for="item in modules" :key="item.key">
            <RouterLink v-if="router" :to="{ name: item.key }" custom v-slot="{ navigate, isActive }">
              <button :class="{ active: isActive || currentModule === item.key }" :data-testid="`nav-${item.key}`" type="button" @click="selectModule(item.key, navigate)">{{ item.label }}</button>
            </RouterLink>
            <button v-else :class="{ active: currentModule === item.key }" :data-testid="`nav-${item.key}`" type="button" @click="selectModule(item.key)">{{ item.label }}</button>
          </template>
        </nav>
        <div class="user">{{ currentUserLabel }}</div>
        <button data-testid="logout" class="logout" type="button" @click="logout">退出</button>
      </header>

      <section class="workspace">
        <section class="main-panel">
          <section v-if="isModuleForbidden" data-testid="module-forbidden" class="tab-panel forbidden-panel route-forbidden-alert">
            <article class="card">
              <div class="card-head"><span>403 权限不足</span><span class="status-line">{{ forbiddenModuleLabel }}</span></div>
              <p class="status-line">当前账号没有访问该模块的权限，请联系管理员调整角色或 Token scope。</p>
            </article>
          </section>
          <NotFoundPanel v-if="currentModule === 'not-found'" />

          <CollectPanel v-else-if="currentModule === 'collect'" v-bind="panelBindings" />

          <ParsePanel v-else-if="currentModule === 'parse'" v-bind="panelBindings" />

          <IndexPanel v-else-if="currentModule === 'index'" v-bind="panelBindings" />

          <SearchPanel v-else-if="currentModule === 'search'" v-bind="panelBindings" />

          <PluginsPanel v-else-if="currentModule === 'plugins'" v-bind="panelBindings" />

          <RbacPanel v-else-if="currentModule === 'rbac'" v-bind="panelBindings" />
        </section>
      </section>
    </section>
  </main>
</template>

<script setup>
import { ref, reactive, computed, inject, onBeforeUnmount, onMounted, watch } from "vue";
import { RouterLink, routeLocationKey, routerKey } from "vue-router";
import { createAuthContext, authContextKey } from "./auth-context.js";
import { requestJSON } from "./http-client.js";
import CollectPanel from "./panels/CollectPanel.vue";
import IndexPanel from "./panels/IndexPanel.vue";
import NotFoundPanel from "./panels/NotFoundPanel.vue";
import ParsePanel from "./panels/ParsePanel.vue";
import PluginsPanel from "./panels/PluginsPanel.vue";
import RbacPanel from "./panels/RbacPanel.vue";
import SearchPanel from "./panels/SearchPanel.vue";

const currentModuleKey = "xdp_current_module";
const defaultModuleKey = "collect";
const screen = ref("login");
const appReady = ref(false);
const credentials = reactive({ username: "admin", password: "" });
const loginError = ref("");
const lastProtectedPayload = ref("");
const baseModules = [{ key: "collect", label: "采集配置" }, { key: "parse", label: "解析配置" }, { key: "index", label: "索引配置" }, { key: "search", label: "搜索页" }, { key: "plugins", label: "插件管理" }, { key: "rbac", label: "用户与权限" }];
const fallbackModule = ref(defaultModuleKey);
const router = inject(routerKey, null);
const route = inject(routeLocationKey, null);
const authContext = inject(authContextKey, null) || createAuthContext();
const auth = authContext.state.auth;
const routeModule = computed(() => routeModuleFromRouteName(route?.name));
const currentModule = computed(() => routeModule.value || fallbackModule.value);
const currentUser = authContext.state.currentUser;

const inputConfigs = ref([]);
const parseRules = ref([]);
const parserPlugins = ref([]);
const parseConfigLoaded = ref(false);
const indexConfigLoaded = ref(false);
const indexes = ref([]);
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
const inputFormNotice = ref("");
const kafkaConnectivityStatus = ref("");
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
const writerRuntime = ref({});
const writerRuntimeLoading = ref(false);
const writerRuntimeError = ref("");
const searchPageSizes = [20, 50, 100, 1000];
const searchPageSize = ref(20);
const searchPagination = ref({ limit: 20, offset: 0, page: 1, returned: 0, hasMore: false, total: 0 });
const searchTimeRangeText = ref("");
const expandedEvents = ref(new Set());
const savedOpen = ref(false);
const savedSearchError = ref("");
const savedSearches = ref([]);
const savedSearchesLoaded = ref(false);
const basePluginTabs = [
  { key: "input", label: "采集插件" },
  { key: "parser", label: "解析插件" },
  { key: "search_command", label: "搜索命令插件" }
];
const p2AssignablePermissionCodes = ["datasource:read", "parse_rule:read", "index:read", "index:manage", "search:execute", "rbac:manage"];
const currentPluginTab = ref("input");
const pluginCatalog = ref([]);
const pluginCatalogLoaded = reactive({ input: false, parser: false, search_command: false });
const pluginCatalogLoading = reactive({ input: false, parser: false, search_command: false });
const pluginCatalogErrors = reactive({ input: "", parser: "", search_command: "" });
const pluginManagementItems = reactive({ input: [], parser: [], search_command: [] });
const pluginManagementLoaded = reactive({ input: false, parser: false, search_command: false });
const pluginManagementSupportsPagination = reactive({ input: false, parser: false, search_command: false });
const pluginPaginationByType = reactive({
  input: defaultListPagination(),
  parser: defaultListPagination(),
  search_command: defaultListPagination()
});
const pluginPageSizeByType = reactive({ input: 10, parser: 10, search_command: 10 });
const pluginTypeCounts = reactive({ input: 0, parser: 0, search_command: 0 });
const pluginUploadFile = ref(null);
const pluginUploadStatus = ref("");
const pluginUploadError = ref("");
const selectedPlugin = ref(null);
const pluginSchema = ref(null);
const pluginActionStatus = ref("");
const pluginActionError = ref("");
const pluginExecutionAudits = ref([]);
const pluginExecutionAuditError = ref("");
const pluginExecutionAuditLoading = ref(false);
const rbacUsers = ref([]);
const rbacRoles = ref([]);
const rbacPermissions = ref([]);
const rbacLoaded = ref(false);
const rbacError = ref("");
const rbacNotice = ref("");
const rbacUserError = ref("");
const rbacRoleError = ref("");
const rbacUserPagination = ref(defaultListPagination(20));
const rbacUserForm = reactive(defaultRBACUserForm());
const rbacRoleForm = reactive(defaultRBACRoleForm());
const editingRBACUserId = ref("");
const editingRBACRoleId = ref("");
const catalog = [
  { id: "evt-1", time: timeAgo(0, 0, 12), index: "app", source: "syslog-default", sourcetype: "app-regex", host: "api-01", service: "api", action: "allow", bytes: 1024, event: 'service=api level=info msg="login ok" bytes=1024' },
  { id: "evt-2", time: timeAgo(0, 0, 36), index: "app", source: "syslog-default", sourcetype: "app-regex", host: "web-01", service: "api", action: "allow", bytes: 3840, event: 'service=api level=warn msg="slow request" bytes=3840' },
  { id: "evt-3", time: timeAgo(1, 3, 0), index: "app", source: "syslog-default", sourcetype: "app-regex", host: "pay-01", service: "checkout", action: "allow", bytes: 2048, event: 'service=checkout level=info msg="payment ok" bytes=2048' },
  { id: "evt-4", time: timeAgo(1, 4, 0), index: "firewall", source: "syslog-udp", sourcetype: "firewall", host: "edge-01", service: "firewall", action: "deny", bytes: 2048, event: "src=10.0.1.8 dst=172.16.0.4 action=deny bytes=2048" }
];
const modules = computed(() => baseModules.filter((item) => canAccessModule(item.key)));
const assignableRBACPermissions = computed(() => {
  const byCode = new Map((rbacPermissions.value || []).map((item) => [item.permission_code, item]));
  return p2AssignablePermissionCodes.map((code) => byCode.get(code) || { permission_code: code, display_name: p2PermissionDisplayName(code) });
});
const currentUserLabel = computed(() => currentUser.displayName || currentUser.username || "Administrator");
const currentModuleLabel = computed(() => baseModules.find((item) => item.key === currentModule.value)?.label || currentModule.value || "未知模块");
const routePath = computed(() => route?.fullPath || route?.path || "");
const forbiddenModule = computed(() => {
  const value = route?.query?.forbidden;
  if (typeof value === "string" && value) return value;
  if (Array.isArray(value) && value[0]) return value[0];
  if (!router && isKnownModule(currentModule.value) && !canAccessModule(currentModule.value)) return currentModule.value;
  return "";
});
const forbiddenModuleLabel = computed(() => baseModules.find((item) => item.key === forbiddenModule.value)?.label || forbiddenModule.value || currentModuleLabel.value);
const isModuleForbidden = computed(() => Boolean(forbiddenModule.value));
const pluginTabs = computed(() => basePluginTabs.filter((tab) => canManagePluginType(tab.key)));
const canManageCurrentPluginTab = computed(() => canManagePluginType(currentPluginTab.value));
const counts = computed(() => ({
  collect: Number(collectPagination.value.total || inputConfigs.value.length),
  parse: Number(parsePagination.value.total || parseRules.value.length),
  index: Number(indexPagination.value.total || indexes.value.length),
  search: savedSearches.value.length,
  plugins: Object.values(pluginTypeCounts).reduce((sum, value) => sum + Number(value || 0), 0),
  rbac: Number(rbacUserPagination.value.total || rbacUsers.value.length) + rbacRoles.value.length
}));
const businessIndexes = computed(() => indexes.value.filter((item) => !isSystemIndex(item)));
const selectedRuntimeName = computed(() => inputConfigs.value.find((item) => item.id === selectedRuntimeId.value)?.name || "");
const canUseSyslogInput = computed(() => canUsePlugin("input", "syslog"));
const kafkaInputPlugin = computed(() => pluginCatalog.value.find((item) => item.plugin_type === "input" && item.plugin_code === "kafka" && isPluginEnabled(item.status)) || null);
const parserPluginOptions = computed(() => {
  const items = pluginCatalog.value.filter((item) => item.plugin_type === "parser" && isPluginEnabled(item.status));
  const regex = items.find((item) => item.plugin_code === "regex") || {
    plugin_code: "regex",
    plugin_type: "parser",
    plugin_version: "1.0.0",
    name: "Regex Parser",
    status: "enabled",
    checksum: "builtin"
  };
  const base = canUsePlugin("parser", "regex") ? [regex] : [];
  return dedupePlugins([...base, ...items.filter((item) => item.plugin_code !== "regex")]);
});
const inputPluginBadge = computed(() => kafkaInputPlugin.value ? "Syslog / Kafka" : "Syslog / 导入插件");
const kafkaSchemaProperties = computed(() => kafkaInputPlugin.value?.config_schema?.properties || {});
const kafkaSchemaOrder = computed(() => {
  const order = kafkaInputPlugin.value?.ui_schema?.order;
  return Array.isArray(order) && order.length ? order : ["brokers", "topic", "consumer_group", "start_offset", "security_protocol", "encoding", "log_filter_enabled", "log_filter_regex"];
});
const kafkaFormFields = computed(() => kafkaSchemaOrder.value.map(kafkaFieldFromSchema).filter(Boolean));
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
const currentPluginPagination = computed(() => pluginPaginationByType[currentPluginTab.value] || defaultListPagination());
const currentPluginPageSize = computed({
  get: () => pluginPageSizeByType[currentPluginTab.value] || 10,
  set: (value) => {
    pluginPageSizeByType[currentPluginTab.value] = Number(value) || 10;
  }
});
const totalPluginPages = computed(() => totalListPages(currentPluginPagination.value, currentPluginPageSize.value));
const visiblePluginPages = computed(() => visiblePageTokens(totalPluginPages.value, currentPluginPagination.value.page));
const pluginUploadFileName = computed(() => pluginUploadFile.value?.name || "未选择文件");
onMounted(() => {
  document.addEventListener("pointerdown", handleConfigDrawerOutsidePointerDown, true);
  loadAuthStatus();
});
onBeforeUnmount(() => {
  document.removeEventListener("pointerdown", handleConfigDrawerOutsidePointerDown, true);
});
watch(() => [
  ruleForm.name,
  ruleForm.pluginCode,
  ruleForm.regexPattern,
  ruleForm.jsonArrayMode,
  ruleForm.jsonInvalidPolicy,
  ruleForm.kvPairDelimiter,
  ruleForm.kvDelimiter,
  ruleForm.kvQuote,
  ruleForm.delimitedDelimiter,
  ruleForm.delimitedQuote,
  ruleForm.delimitedFields
], () => syncPropsConf(), { immediate: true });
watch(() => [screen.value, currentModule.value], async ([currentScreen, module]) => {
  if (currentScreen !== "app" || !appReady.value || !isKnownModule(module)) return;
  if (!canAccessModule(module)) {
    if (!router) await redirectForbiddenModule(module);
    return;
  }
  if (!router) persistCurrentModule(module);
  await refreshCurrentModule(module, true);
});

function hasPermission(permission) {
  return authContext.hasPermission(permission);
}
function canUsePlugin(pluginType, pluginCode = "") {
  return authContext.canUsePlugin(pluginType, pluginCode);
}
function canManagePluginType(pluginType) {
  return authContext.canManagePluginType(pluginType);
}
function hasAnyManagePluginScope() {
  return authContext.hasAnyManagePluginScope();
}
function canAccessModule(moduleKey) {
  return authContext.canAccessModule(moduleKey);
}
function routeModuleFromRouteName(name) {
  const routeName = String(name || "");
  if (routeName === "not-found") return "not-found";
  return isKnownModule(routeName) ? routeName : "";
}
async function navigateToModule(moduleKey, options = {}) {
  if (!isKnownModule(moduleKey)) return;
  if (!router) {
    fallbackModule.value = moduleKey;
    persistCurrentModule(moduleKey);
    return;
  }
  const target = { name: moduleKey, query: options.query || {} };
  try {
    if (options.replace) {
      await router.replace(target);
    } else if (route?.name !== moduleKey || route?.query?.forbidden) {
      await router.push(target);
    }
  } catch {
    // Ignore duplicated navigation and keep local module fallback in sync.
  }
}
async function redirectForbiddenModule(moduleKey) {
  if (!router || !isKnownModule(moduleKey)) return;
  const fallback = modules.value.find((item) => item.key !== moduleKey)?.key || defaultModuleKey;
  if (!isKnownModule(fallback)) return;
  try {
    await router.replace({ name: fallback, query: { forbidden: moduleKey } });
  } catch {
    fallbackModule.value = fallback;
  }
}
function navCount(key) { return counts.value[key] || 0; }
async function loadAuthStatus() {
  loginError.value = "";
  appReady.value = false;
  const response = await authContext.ensureAuthReady({ force: true });
  if (!response.loginRequired || response.authenticated) {
    enterConsole();
    ensureAccessibleDefaultModule();
    ensureAccessiblePluginTab();
    await loadProtectedData();
    appReady.value = true;
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
    await authContext.setLoginToken(response.token);
    enterConsole();
    ensureAccessibleDefaultModule();
    ensureAccessiblePluginTab();
    await loadProtectedData();
    appReady.value = true;
  } catch (error) {
    authContext.clearAuth();
    loginError.value = error.message;
    screen.value = "login";
  }
}
function enterConsole() {
  if (!router) fallbackModule.value = readStoredModule();
  screen.value = "app";
}
async function loadCurrentUser() {
  await authContext.reloadCurrentUser();
  ensureAccessibleDefaultModule();
  ensureAccessiblePluginTab();
}
function logout() {
  appReady.value = false;
  authContext.clearAuth();
  lastProtectedPayload.value = "";
  pluginCatalog.value = [];
  for (const type of Object.keys(pluginCatalogLoaded)) {
    pluginCatalogLoaded[type] = false;
    pluginCatalogLoading[type] = false;
    pluginCatalogErrors[type] = "";
  }
  if (router) {
    router.replace({ name: defaultModuleKey }).catch(() => {});
  } else {
    fallbackModule.value = defaultModuleKey;
  }
  screen.value = "login";
}
async function selectModule(moduleKey, navigate) {
  if (!isValidModule(moduleKey) || !canAccessModule(moduleKey)) return;
  if (currentModule.value === moduleKey) {
    refreshCurrentModule(moduleKey, true);
    return;
  }
  if (typeof navigate === "function") {
    await navigate();
    return;
  }
  await navigateToModule(moduleKey);
}
async function refreshCurrentModule(module, force = false) {
  if (!canAccessModule(module)) return;
  if (module === "collect") {
    await Promise.all([loadCollectConfig(collectPagination.value.page || 1), loadPluginCatalog("input", force)]);
  }
  if (module === "parse") {
    await Promise.all([loadIndexConfig(force), loadParseConfig(force), loadPluginCatalog("parser", force)]);
  }
  if (module === "index") {
    await Promise.all([loadIndexConfig(force), loadWriterRuntime(force)]);
  }
  if (module === "search") {
    await loadSavedSearches(force);
  }
  if (module === "plugins") {
    ensureAccessiblePluginTab();
    if (!canAccessModule("plugins")) return;
    await loadPlugins(force);
    if (pluginManagementSupportsPagination[currentPluginTab.value]) await loadPluginCatalog(currentPluginTab.value, force);
  }
  if (module === "rbac") {
    await loadRBACConfig(force);
  }
}
function readStoredModule() {
  const stored = localStorage.getItem(currentModuleKey);
  return isKnownModule(stored) ? stored : defaultModuleKey;
}
function persistCurrentModule(moduleKey) {
  localStorage.setItem(currentModuleKey, isKnownModule(moduleKey) ? moduleKey : defaultModuleKey);
}
function isValidModule(moduleKey) {
  return isKnownModule(moduleKey);
}
function isKnownModule(moduleKey) {
  return baseModules.some((item) => item.key === moduleKey);
}
function ensureAccessibleDefaultModule() {
  const stored = localStorage.getItem(currentModuleKey);
  if (canAccessModule(currentModule.value)) {
    if (!router && !isKnownModule(stored)) persistCurrentModule(currentModule.value);
    return;
  }
  if (router && isKnownModule(currentModule.value)) {
    redirectForbiddenModule(currentModule.value);
    return;
  }
  if (isKnownModule(stored)) return;
  const first = modules.value[0]?.key;
  if (first) {
    navigateToModule(first, { replace: true });
  }
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
    if (!currentUser.rbacEnabled) {
      await Promise.all([loadCollectConfig(), loadIndexConfig(true), loadParseConfig(true), loadSavedSearches()]);
      await loadPluginCatalog("input");
      return;
    }
    const tasks = [];
    if (canAccessModule("collect")) tasks.push(loadCollectConfig(), loadPluginCatalog("input"));
    if (canAccessModule("index")) tasks.push(loadIndexConfig(true));
    if (canAccessModule("parse")) tasks.push(loadParseConfig(true), loadPluginCatalog("parser"));
    if (canAccessModule("search")) tasks.push(loadSavedSearches());
    if (canAccessModule("plugins")) tasks.push(loadPlugins(true));
    if (canAccessModule("rbac")) tasks.push(loadRBACConfig(true));
    await Promise.all(tasks);
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
    if (Array.isArray(payload.indexes)) {
      const previousByName = new Map(indexes.value.map((item) => [item.name, item]));
      indexes.value = payload.indexes.map((index) => {
        const item = apiIndexToForm(index);
        const previous = previousByName.get(item.name);
        if (!previous) return item;
        return { ...item, trend: previous.trend, trendOpen: previous.trendOpen, trendLoading: previous.trendLoading, trendError: previous.trendError };
      });
    }
    indexPagination.value = normalizeListPagination(payload.pagination, indexes.value.length, page, indexPageSize.value);
    indexConfigLoaded.value = true;
  } catch {
    indexConfigLoaded.value = false;
  }
}
async function loadWriterRuntime(force = false) {
  if (writerRuntimeLoading.value && !force) return;
  writerRuntimeLoading.value = true;
  writerRuntimeError.value = "";
  try {
    writerRuntime.value = await requestJSON("/api/v1/writer/runtime", { auth: true });
  } catch (error) {
    writerRuntime.value = { status: "unknown" };
    writerRuntimeError.value = error.message || "Writer 状态加载失败";
  } finally {
    writerRuntimeLoading.value = false;
  }
}
async function loadRBACConfig(force = false) {
  if (rbacLoaded.value && !force) return;
  rbacError.value = "";
  try {
    const tasks = [];
    if (hasPermission("rbac:manage")) tasks.push(loadRBACUsers(), loadRBACRoles(), loadRBACPermissions(), loadIndexConfig(true).catch(() => {}));
    await Promise.all(tasks);
    rbacLoaded.value = true;
  } catch (error) {
    rbacLoaded.value = false;
    rbacError.value = error.message || "用户与权限加载失败";
  }
}
async function loadRBACUsers(page = rbacUserPagination.value.page || 1) {
  const payload = await requestJSON(listURL("/api/v1/users", page, 20), { auth: true });
  rbacUsers.value = Array.isArray(payload.users) ? payload.users : [];
  rbacUserPagination.value = normalizeListPagination(payload.pagination, rbacUsers.value.length, page, 20);
}
async function loadRBACRoles() {
  const payload = await requestJSON("/api/v1/roles", { auth: true });
  rbacRoles.value = Array.isArray(payload.roles) ? payload.roles : [];
}
async function loadRBACPermissions() {
  const payload = await requestJSON("/api/v1/permissions", { auth: true });
  rbacPermissions.value = Array.isArray(payload.permissions) ? payload.permissions : [];
}
async function saveRBACUser() {
  rbacUserError.value = "";
  rbacNotice.value = "";
  const validationError = validateRBACUserForm(rbacUserForm);
  if (validationError) {
    rbacUserError.value = validationError;
    return;
  }
  try {
    const isEdit = Boolean(editingRBACUserId.value);
    const payload = {
      username: String(rbacUserForm.username || "").trim(),
      display_name: String(rbacUserForm.displayName || "").trim(),
      status: rbacUserForm.status || "active",
      role_ids: [...rbacUserForm.roleIds]
    };
    if (!isEdit) payload.password = rbacUserForm.password;
    const saved = await requestJSON(isEdit ? `/api/v1/users/${encodeURIComponent(editingRBACUserId.value)}` : "/api/v1/users", {
      auth: true,
      method: isEdit ? "PUT" : "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload)
    });
    let next = saved;
    if (isEdit) {
      next = await requestJSON(`/api/v1/users/${encodeURIComponent(editingRBACUserId.value)}/roles`, {
        auth: true,
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ role_ids: [...rbacUserForm.roleIds] })
      });
      if (String(rbacUserForm.password || "").trim()) {
        await setRBACUserPassword(editingRBACUserId.value, rbacUserForm.password);
      }
    } else {
      adjustListPaginationTotal(rbacUserPagination, 1, 20);
    }
    rbacUsers.value = isEdit ? rbacUsers.value.map((item) => item.id === editingRBACUserId.value ? next : item) : [next, ...rbacUsers.value.filter((item) => item.id !== next.id)];
    resetRBACUserForm();
    rbacNotice.value = isEdit ? "用户已保存" : "用户已创建";
  } catch (error) {
    rbacUserError.value = error.message || "用户保存失败";
  }
}
async function saveRBACRole() {
  rbacRoleError.value = "";
  rbacNotice.value = "";
  const validationError = validateRBACRoleForm(rbacRoleForm);
  if (validationError) {
    rbacRoleError.value = validationError;
    return;
  }
  try {
    const isEdit = Boolean(editingRBACRoleId.value);
    const permissionCodes = mergeUnique([...rbacRoleForm.permissionCodes]);
    const saved = await requestJSON(isEdit ? `/api/v1/roles/${encodeURIComponent(editingRBACRoleId.value)}` : "/api/v1/roles", {
      auth: true,
      method: isEdit ? "PUT" : "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        role_code: String(rbacRoleForm.roleCode || "").trim(),
        role_name: String(rbacRoleForm.roleName || "").trim(),
        description: String(rbacRoleForm.description || "").trim(),
        status: rbacRoleForm.status || "active",
        permission_codes: permissionCodes,
        index_scopes: parseIndexScopesText(rbacRoleForm.indexScopesText),
        plugin_scopes: parsePluginScopesText(rbacRoleForm.pluginScopesText)
      })
    });
    rbacRoles.value = isEdit ? rbacRoles.value.map((item) => item.id === editingRBACRoleId.value ? saved : item) : [saved, ...rbacRoles.value.filter((item) => item.id !== saved.id)];
    resetRBACRoleForm();
    rbacNotice.value = isEdit ? "角色已保存" : "角色已创建";
  } catch (error) {
    rbacRoleError.value = error.message || "角色保存失败";
  }
}
function validateRBACUserForm(form) {
  if (!String(form.username || "").trim()) return "用户名为必填项";
  if (!String(form.displayName || "").trim()) return "全称为必填项";
  const password = String(form.password || "");
  const confirmPassword = String(form.confirmPassword || "");
  if (!editingRBACUserId.value && !password.trim()) return "设置密码为必填项";
  if ((!editingRBACUserId.value || password.trim() || confirmPassword.trim()) && password !== confirmPassword) return "两次输入的密码不一致";
  if (!String(form.status || "").trim()) return "状态为必填项";
  return "";
}
function validateRBACRoleForm(form) {
  if (!String(form.roleCode || "").trim()) return "角色编码为必填项";
  if (!String(form.roleName || "").trim()) return "角色名称为必填项";
  if (!String(form.status || "").trim()) return "状态为必填项";
  return "";
}
function splitRBACLines(value) {
  return String(value || "").split(/[\n,]+/).map((item) => item.trim()).filter(Boolean);
}
function mergeUnique(items) {
  return Array.from(new Set(items.map((item) => String(item || "").trim()).filter(Boolean)));
}
function parseIndexScopesText(value) {
  const out = {};
  String(value || "").split("\n").map((line) => line.trim()).filter(Boolean).forEach((line) => {
    const [action, patterns] = splitScopeLine(line);
    if (!action || !patterns) return;
    out[action] = mergeUnique([...(out[action] || []), ...splitRBACLines(patterns)]);
  });
  return out;
}
function parsePluginScopesText(value) {
  const out = {};
  String(value || "").split("\n").map((line) => line.trim()).filter(Boolean).forEach((line) => {
    const [action, bindings] = splitScopeLine(line);
    if (!action || !bindings) return;
    out[action] = [
      ...(out[action] || []),
      ...splitRBACLines(bindings).map(parsePluginScopeBinding).filter(Boolean)
    ];
  });
  return out;
}
function splitScopeLine(line) {
  const index = String(line || "").indexOf(":");
  if (index < 0) return ["", ""];
  return [line.slice(0, index).trim(), line.slice(index + 1).trim()];
}
function parsePluginScopeBinding(value) {
  const [pluginType, pluginCode] = String(value || "").split("/");
  if (!pluginType || !pluginCode) return null;
  return { plugin_type: normalizePluginType(pluginType), plugin_code: pluginCode.trim() };
}
function roleNames(roles = []) {
  return Array.isArray(roles) && roles.length ? roles.map((role) => role.role_name || role.role_code).join("、") : "-";
}
function compactList(items = []) {
  return Array.isArray(items) && items.length ? items.join("\n") : "-";
}
function formatIndexScopes(scopes = {}) {
  const lines = Object.entries(scopes || {}).flatMap(([action, patterns]) => (Array.isArray(patterns) ? patterns : []).map((pattern) => `${action}:${pattern}`));
  return lines.length ? lines.join("\n") : "-";
}
function formatPluginScopes(scopes = {}) {
  const lines = Object.entries(scopes || {}).flatMap(([action, items]) => (Array.isArray(items) ? items : []).map((item) => `${action}:${item.plugin_type}/${item.plugin_code}`));
  return lines.length ? lines.join("\n") : "-";
}
function permissionTestId(code) {
  return String(code || "").replace(/[^a-zA-Z0-9_-]+/g, "-");
}
function p2PermissionDisplayName(code) {
  return {
    "datasource:read": "采集配置入口",
    "parse_rule:read": "解析配置入口",
    "index:read": "索引配置入口",
    "index:manage": "索引管理能力",
    "search:execute": "搜索页入口",
    "rbac:manage": "用户与权限管理"
  }[code] || code;
}
function resetRBACUserForm() {
  editingRBACUserId.value = "";
  assignReactive(rbacUserForm, defaultRBACUserForm());
}
function editRBACUser(user) {
  editingRBACUserId.value = user.id;
  assignReactive(rbacUserForm, {
    username: user.username || "",
    displayName: user.display_name || user.displayName || "",
    password: "",
    confirmPassword: "",
    status: user.status || "active",
    roleIds: userRoleIds(user.roles),
    createRoleForUser: false,
    forcePasswordChange: false
  });
}
function userRoleIds(roles = []) {
  return Array.isArray(roles) ? roles.map((role) => role.id || role.role_id || role.role_code).filter(Boolean) : [];
}
async function toggleRBACUserStatus(user) {
  rbacUserError.value = "";
  const nextStatus = user.status === "active" ? "disabled" : "active";
  try {
    const saved = await requestJSON(`/api/v1/users/${encodeURIComponent(user.id)}`, {
      auth: true,
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ display_name: user.display_name || user.displayName || "", status: nextStatus })
    });
    rbacUsers.value = rbacUsers.value.map((item) => item.id === user.id ? { ...item, ...saved, status: nextStatus } : item);
    rbacNotice.value = nextStatus === "active" ? "用户已启用" : "用户已禁用";
  } catch (error) {
    rbacUserError.value = error.message || "用户状态更新失败";
  }
}
async function resetRBACUserPassword(user) {
  const password = window.prompt("请输入新密码");
  if (!String(password || "").trim()) return;
  rbacUserError.value = "";
  try {
    await setRBACUserPassword(user.id, password);
    rbacNotice.value = "用户密码已重置";
  } catch (error) {
    rbacUserError.value = error.message || "密码重置失败";
  }
}
async function setRBACUserPassword(userID, password) {
  return requestJSON(`/api/v1/users/${encodeURIComponent(userID)}/password`, {
    auth: true,
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ password })
  });
}
async function deleteRBACUser(user) {
  rbacUserError.value = "";
  try {
    await requestJSON(`/api/v1/users/${encodeURIComponent(user.id)}`, { auth: true, method: "DELETE" });
    rbacUsers.value = rbacUsers.value.filter((item) => item.id !== user.id);
    adjustListPaginationTotal(rbacUserPagination, -1, 20);
    if (editingRBACUserId.value === user.id) resetRBACUserForm();
    rbacNotice.value = "用户已删除";
  } catch (error) {
    rbacUserError.value = error.message || "用户删除失败";
  }
}
function resetRBACRoleForm() {
  editingRBACRoleId.value = "";
  assignReactive(rbacRoleForm, defaultRBACRoleForm());
}
function editRBACRole(role) {
  editingRBACRoleId.value = role.id;
  assignReactive(rbacRoleForm, {
    roleCode: role.role_code || "",
    roleName: role.role_name || "",
    description: role.description || "",
    status: role.status || "active",
    permissionCodes: Array.isArray(role.permission_codes) ? role.permission_codes.filter((code) => p2AssignablePermissionCodes.includes(code)) : [],
    permissionsText: compactList(role.permission_codes) === "-" ? "" : compactList(role.permission_codes),
    indexScopesText: formatIndexScopes(role.index_scopes) === "-" ? "" : formatIndexScopes(role.index_scopes),
    pluginScopesText: formatPluginScopes(role.plugin_scopes) === "-" ? "" : formatPluginScopes(role.plugin_scopes)
  });
}
async function deleteRBACRole(role) {
  rbacRoleError.value = "";
  try {
    await requestJSON(`/api/v1/roles/${encodeURIComponent(role.id)}`, { auth: true, method: "DELETE" });
    rbacRoles.value = rbacRoles.value.filter((item) => item.id !== role.id);
    if (editingRBACRoleId.value === role.id) resetRBACRoleForm();
    rbacNotice.value = "角色已删除";
  } catch (error) {
    rbacRoleError.value = error.message || "角色删除失败";
  }
}
async function loadPlugins(force = false, type = currentPluginTab.value, page = pluginPaginationByType[type]?.page || 1) {
  ensureAccessiblePluginTab();
  type = normalizePluginType(type || currentPluginTab.value);
  if (!canManagePluginType(type)) {
    pluginManagementItems[type] = [];
    pluginPaginationByType[type] = normalizeListPagination({}, 0, 1, pluginPageSizeByType[type] || 10);
    pluginManagementLoaded[type] = true;
    return;
  }
  if (pluginManagementLoaded[type] && !force && pluginPaginationByType[type]?.page === page) return;
  try {
    const pageSize = pluginPageSizeByType[type] || 10;
    const params = new URLSearchParams({
      plugin_type: type,
      page: String(Math.max(1, Number(page) || 1)),
      page_size: String(pageSize)
    });
    const payload = await requestJSON(`/api/v1/plugins?${params.toString()}`, { auth: true });
    const rawItems = Array.isArray(payload.plugins) ? payload.plugins : (Array.isArray(payload) ? payload : []);
    const items = dedupePlugins(rawItems.map(apiPluginToForm).filter(isProductVisiblePlugin));
    const paginated = Boolean(payload.pagination);
    if (!paginated && new Set(items.map((item) => item.plugin_type)).size > 1) {
      for (const tab of pluginTabs.value) {
        const typedItems = items.filter((item) => item.plugin_type === tab.key);
        pluginManagementItems[tab.key] = typedItems;
        pluginPaginationByType[tab.key] = normalizeListPagination({}, typedItems.length, 1, pageSize);
        pluginTypeCounts[tab.key] = typedItems.length;
        pluginManagementLoaded[tab.key] = true;
        pluginManagementSupportsPagination[tab.key] = false;
      }
    } else {
      pluginManagementItems[type] = items.filter((item) => item.plugin_type === type);
      pluginPaginationByType[type] = normalizeListPagination(payload.pagination, pluginManagementItems[type].length, page, pageSize);
      Object.assign(pluginTypeCounts, payload.type_counts || {});
      if (!payload.type_counts) pluginTypeCounts[type] = pluginPaginationByType[type].total;
      pluginManagementLoaded[type] = true;
      pluginManagementSupportsPagination[type] = paginated;
    }
    if (!paginated) {
      const enabledInputs = items.filter((item) => item.plugin_type === "input" && isPluginEnabled(item.status));
      if (enabledInputs.length) {
        pluginCatalog.value = dedupePlugins([
          ...pluginCatalog.value.filter((item) => item.plugin_type !== "input"),
          ...enabledInputs
        ]);
        pluginCatalogLoaded.input = true;
      }
    }
  } catch (error) {
    pluginUploadError.value = error.message || "插件列表加载失败";
    pluginManagementLoaded[type] = false;
  }
}
async function loadPluginCatalog(type = "input", force = false) {
  type = normalizePluginType(type);
  if (pluginCatalogLoaded[type] && !force) return;
  pluginCatalogLoading[type] = true;
  pluginCatalogErrors[type] = "";
  try {
    const payload = await requestJSON(`/api/v1/plugins/catalog?plugin_type=${encodeURIComponent(type)}&status=enabled`, { auth: true });
    const items = Array.isArray(payload.plugins) ? payload.plugins.map(apiPluginToForm).filter(isProductVisiblePlugin) : [];
    pluginCatalog.value = dedupePlugins([
      ...pluginCatalog.value.filter((item) => item.plugin_type !== type),
      ...items
    ]);
    pluginCatalogLoaded[type] = true;
  } catch (error) {
    pluginCatalogLoaded[type] = false;
    pluginCatalogErrors[type] = error.message || `${pluginTypeLabel(type)}目录加载失败`;
  } finally {
    pluginCatalogLoading[type] = false;
  }
}
async function retryInputPluginCatalog() {
  await loadPluginCatalog("input", true);
}
async function retryParserPluginCatalog() {
  await loadPluginCatalog("parser", true);
}
function defaultRBACUserForm() { return { username: "", displayName: "", password: "", confirmPassword: "", status: "active", roleIds: [], createRoleForUser: false, forcePasswordChange: true }; }
function defaultRBACRoleForm() { return { roleCode: "", roleName: "", description: "", status: "active", permissionCodes: [], permissionsText: "", indexScopesText: "", pluginScopesText: "", inheritedRoleIds: [] }; }
function defaultInputForm() { return { name: "", status: "active", plugin: "Syslog", collectorPort: "5514", transportProtocol: "UDP", encoding: "UTF-8", logFilterEnabled: "off", logFilterRegex: "", brokers: "", topic: "", consumerGroup: "", securityProtocol: "PLAINTEXT", startOffset: "earliest", encodingKafka: "UTF-8", logFilterEnabledKafka: "off", logFilterRegexKafka: "" }; }
function defaultRuleForm() { return { name: "", plugin: "正则解析插件", pluginCode: "regex", pluginVersion: "1.0.0", dataSourceName: "", inputRoute: "internal_raw_topic", outputIndex: "app", priority: 100, sampleLog: "", regexPattern: "", jsonArrayMode: "json_string", jsonInvalidPolicy: "continue", kvPairDelimiter: "空格", kvDelimiter: "=", kvQuote: '"', delimitedDelimiter: ",", delimitedQuote: '"', delimitedFields: "field1,field2,field3", propsConf: "" }; }
function defaultIndexForm() { return { name: "", ttl: 30, status: "active" }; }
function assignReactive(target, source) { Object.keys(target).forEach((key) => delete target[key]); Object.assign(target, source); }
const currentPluginTabLabel = computed(() => basePluginTabs.find((item) => item.key === currentPluginTab.value)?.label || "插件列表");
const filteredPlugins = computed(() => pluginManagementItems[currentPluginTab.value] || []);
const pluginReferenceCount = computed(() => Number(selectedPlugin.value?.references?.count || 0));
const pluginReferenceItems = computed(() => Array.isArray(selectedPlugin.value?.references?.items) ? selectedPlugin.value.references.items : []);
const pluginSchemaText = computed(() => JSON.stringify(pluginSchema.value?.config_schema || selectedPlugin.value?.config_schema || {}, null, 2));
const pluginUISchemaText = computed(() => JSON.stringify(pluginSchema.value?.ui_schema || selectedPlugin.value?.ui_schema || {}, null, 2));
const pluginEffectiveRuntimeConfig = computed(() => selectedPlugin.value?.effective_runtime_config || pluginSchema.value?.effective_runtime_config || null);
const pluginEffectiveRuntimeText = computed(() => {
  const config = pluginEffectiveRuntimeConfig.value || {};
  return [
    `interpreter=${config.interpreter || "-"}`,
    `timeout_ms=${config.timeout_ms ?? "-"}`,
    `max_input_rows=${config.max_input_rows ?? "-"}`,
    `max_output_bytes=${config.max_output_bytes ?? "-"}`
  ].join(" / ");
});
const showPluginExecutionAudits = computed(() => selectedPlugin.value?.plugin_type === "search_command" && !isBuiltInPlugin(selectedPlugin.value || {}));
async function selectPluginTab(tab) {
  if (!canManagePluginType(tab)) return;
  currentPluginTab.value = tab;
  pluginUploadStatus.value = "";
  pluginUploadError.value = "";
  clearPluginDetail();
  await loadPlugins(false, tab, pluginPaginationByType[tab]?.page || 1);
}
function ensureAccessiblePluginTab() {
  if (canManagePluginType(currentPluginTab.value)) return;
  const first = pluginTabs.value[0]?.key;
  if (first) currentPluginTab.value = first;
}
function pluginTypeCount(type) {
  return Number(pluginTypeCounts[type] || 0);
}
async function goPluginPage(page) {
  if (page < 1 || page > totalPluginPages.value) return;
  await loadPlugins(true, currentPluginTab.value, page);
}
async function reloadPluginFirstPage() {
  const type = currentPluginTab.value;
  pluginPaginationByType[type] = { ...pluginPaginationByType[type], page: 1, page_size: currentPluginPageSize.value };
  pluginManagementLoaded[type] = false;
  await loadPlugins(true, type, 1);
}
function isPluginDetailOpen(item) {
  return Boolean(selectedPlugin.value) && pluginIdentity(selectedPlugin.value) === pluginIdentity(item);
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
  const buildFormData = () => {
    const formData = new FormData();
    formData.append("file", pluginUploadFile.value);
    return formData;
  };
  try {
    const imported = await requestJSON("/api/v1/plugins/import", {
      auth: true,
      method: "POST",
      body: buildFormData()
    });
    const item = apiPluginToForm(imported);
    await applyImportedPlugin(item, imported);
  } catch (error) {
    if (String(error.message || "").includes("PLUGIN_ALREADY_EXISTS") && window.confirm("插件已存在，是否覆盖已有插件包？")) {
      try {
        const imported = await requestJSON("/api/v1/plugins/import?overwrite=true", {
          auth: true,
          method: "POST",
          body: buildFormData()
        });
        const item = apiPluginToForm(imported);
        await applyImportedPlugin(item, imported);
        pluginUploadStatus.value = `已覆盖：${item.name || item.plugin_code}`;
        return;
      } catch (overwriteError) {
        pluginUploadError.value = overwriteError.message || "插件覆盖失败";
        return;
      }
    }
    pluginUploadError.value = error.message || "插件导入失败";
  }
}
async function applyImportedPlugin(item, imported = {}) {
  const type = normalizePluginType(item.plugin_type);
  if (!type) return;
  if (currentPluginTab.value !== type) currentPluginTab.value = type;
  const exists = (pluginManagementItems[type] || []).some((current) => pluginIdentity(current) === pluginIdentity(item));
  pluginManagementItems[type] = dedupePlugins(upsertPlugin(pluginManagementItems[type] || [], item));
  pluginTypeCounts[type] = exists ? Math.max(Number(pluginTypeCounts[type] || 0), pluginManagementItems[type].length) : Math.max(Number(pluginTypeCounts[type] || 0) + 1, pluginManagementItems[type].length);
  pluginPaginationByType[type] = {
    ...pluginPaginationByType[type],
    total: pluginTypeCounts[type],
    total_pages: Math.max(1, Math.ceil(pluginTypeCounts[type] / (pluginPageSizeByType[type] || 10)))
  };
  pluginManagementLoaded[type] = true;
  selectedPlugin.value = item;
  pluginSchema.value = { config_schema: imported.config_schema || {}, ui_schema: imported.ui_schema || {} };
  pluginUploadStatus.value = `导入成功：${item.name || item.plugin_code}`;
  await loadPluginCatalog(type, true);
}
async function loadPluginDetail(item) {
  pluginActionStatus.value = "";
  pluginActionError.value = "";
  pluginExecutionAuditError.value = "";
  pluginExecutionAudits.value = [];
  if (isPluginDetailOpen(item)) {
    clearPluginDetail();
    return;
  }
  const type = encodeURIComponent(item.plugin_type);
  const code = encodeURIComponent(item.plugin_code);
  try {
    const detail = await requestJSON(`/api/v1/plugins/${code}?plugin_type=${type}`, { auth: true });
    const schema = await requestJSON(`/api/v1/plugins/${code}/schema?plugin_type=${type}`, { auth: true });
    selectedPlugin.value = { ...apiPluginToForm(detail), references: detail.references || { count: 0 }, config_schema: detail.config_schema || {}, ui_schema: detail.ui_schema || {} };
    pluginSchema.value = schema || {};
    if (showPluginExecutionAudits.value) await loadPluginExecutionAudits(selectedPlugin.value);
  } catch (error) {
    pluginActionError.value = error.message || "插件详情加载失败";
  }
}
async function loadPluginExecutionAudits(plugin) {
  pluginExecutionAuditLoading.value = true;
  pluginExecutionAuditError.value = "";
  pluginExecutionAudits.value = [];
  const type = encodeURIComponent(plugin.plugin_type);
  const code = encodeURIComponent(plugin.plugin_code);
  try {
    const payload = await requestJSON(`/api/v1/plugins/${code}/execution-audits?plugin_type=${type}&limit=20`, { auth: true });
    pluginExecutionAudits.value = Array.isArray(payload.audits) ? payload.audits : [];
  } catch (error) {
    pluginExecutionAuditError.value = error.message || "执行审计加载失败";
  } finally {
    pluginExecutionAuditLoading.value = false;
  }
}
async function setPluginStatus(plugin, action) {
  pluginActionStatus.value = "";
  pluginActionError.value = "";
  const type = encodeURIComponent(plugin.plugin_type);
  const code = encodeURIComponent(plugin.plugin_code);
  try {
    const updated = apiPluginToForm(await requestJSON(`/api/v1/plugins/${code}/${action}?plugin_type=${type}`, { auth: true, method: "POST" }));
    const pluginType = normalizePluginType(updated.plugin_type);
    pluginManagementItems[pluginType] = dedupePlugins(upsertPlugin(pluginManagementItems[pluginType] || [], updated));
    syncPluginCatalogStatus(updated);
    if (selectedPlugin.value?.plugin_code === updated.plugin_code) selectedPlugin.value = { ...selectedPlugin.value, ...updated };
    pluginActionStatus.value = `${pluginStatusLabel(updated.status)}：${updated.name || updated.plugin_code}`;
    await loadPluginCatalog(pluginType, true);
  } catch (error) {
    pluginActionError.value = error.message || "插件状态更新失败";
  }
}
async function deletePlugin(plugin) {
  pluginActionStatus.value = "";
  pluginActionError.value = "";
  const type = encodeURIComponent(plugin.plugin_type);
  const code = encodeURIComponent(plugin.plugin_code);
  try {
    await requestJSON(`/api/v1/plugins/${code}?plugin_type=${type}`, { auth: true, method: "DELETE" });
    const pluginType = normalizePluginType(plugin.plugin_type);
    pluginManagementItems[pluginType] = (pluginManagementItems[pluginType] || []).filter((item) => pluginIdentity(item) !== pluginIdentity(plugin));
    pluginTypeCounts[pluginType] = Math.max(0, Number(pluginTypeCounts[pluginType] || 0) - 1);
    pluginPaginationByType[pluginType] = {
      ...pluginPaginationByType[pluginType],
      total: pluginTypeCounts[pluginType],
      total_pages: Math.max(1, Math.ceil(pluginTypeCounts[pluginType] / (pluginPageSizeByType[pluginType] || 10)))
    };
    clearPluginDetail();
    pluginActionStatus.value = "插件版本已删除";
    await loadPluginCatalog(pluginType, true);
  } catch (error) {
    pluginActionError.value = error.message || "插件版本删除失败";
  }
}
function syncPluginCatalogStatus(item) {
  const type = normalizePluginType(item.plugin_type);
  const withoutVersion = pluginCatalog.value.filter((current) => pluginIdentity(current) !== pluginIdentity(item));
  pluginCatalog.value = isPluginEnabled(item.status) ? dedupePlugins([...withoutVersion, item]) : withoutVersion;
  if (type in pluginCatalogLoaded) pluginCatalogLoaded[type] = true;
}
function clearPluginDetail() {
  selectedPlugin.value = null;
  pluginSchema.value = null;
  pluginActionStatus.value = "";
  pluginActionError.value = "";
  pluginExecutionAudits.value = [];
  pluginExecutionAuditError.value = "";
  pluginExecutionAuditLoading.value = false;
}
function apiPluginToForm(item = {}) {
  return {
    plugin_code: String(item.plugin_code || item.code || "").trim(),
    plugin_type: normalizePluginType(item.plugin_type || item.type || ""),
    plugin_version: String(item.plugin_version || item.version || "1.0.0").trim() || "1.0.0",
    name: item.name || String(item.plugin_code || item.code || "").trim(),
    runtime: item.runtime || "go_builtin",
    entrypoint: item.entrypoint || "",
    description: item.description || "",
    status: item.status || "disabled",
    checksum: item.checksum || "builtin",
    built_in: Boolean(item.built_in),
    references: item.references || { count: 0 },
    config_schema: item.config_schema || {},
    ui_schema: item.ui_schema || {},
    runtime_config: item.runtime_config || {},
    effective_runtime_config: item.effective_runtime_config || null
  };
}
function isBuiltInPlugin(item = {}) {
  const type = normalizePluginType(item.plugin_type);
  const code = String(item.plugin_code || "").trim();
  return item.built_in === true ||
    type === "input" && code === "syslog" ||
    type === "parser" && code === "regex" ||
    type === "search_command" && code === "stats";
}
function isPluginReferenced(plugin = {}) {
  if (!selectedPlugin.value) return false;
  return selectedPlugin.value.plugin_code === plugin.plugin_code &&
    normalizePluginType(selectedPlugin.value.plugin_type) === normalizePluginType(plugin.plugin_type) &&
    pluginReferenceCount.value > 0;
}
function referenceTypeLabel(type) {
  const value = String(type || "").trim();
  if (value === "datasource") return "采集源";
  if (value === "parse_rule") return "解析规则";
  if (value === "saved_search") return "保存搜索";
  return value || "-";
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
    type === "parser" && code === "props-conf-parser";
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
  return items.some((current) => pluginIdentity(current) === pluginIdentity(item)) ? items.map((current) => pluginIdentity(current) === pluginIdentity(item) ? { ...current, ...item } : current) : [item, ...items];
}
function dedupePlugins(items = []) {
  const seen = new Set();
  const result = [];
  for (const raw of items) {
    const item = apiPluginToForm(raw);
    const key = pluginIdentity(item);
    if (seen.has(key)) continue;
    seen.add(key);
    result.push(item);
  }
  return result;
}
function pluginIdentity(item = {}) {
  return `${normalizePluginType(item.plugin_type)}/${String(item.plugin_code || "").trim()}`;
}
function pluginStatusLabel(status) {
  return isPluginEnabled(status) ? "已启用" : "未启用";
}
function isPluginEnabled(status) {
  return status === "active" || status === "enabled";
}
async function selectInputPlugin(plugin) {
  if (editingInputId.value) return;
  inputForm.plugin = plugin;
  if (plugin === "Kafka") {
    if (!kafkaInputPlugin.value) {
      inputForm.plugin = "Syslog";
      inputFormError.value = "请先在插件管理导入并启用 Kafka 插件";
      return;
    }
    applyKafkaSchemaDefaults();
  }
}
function kafkaFieldFromSchema(name) {
  const property = kafkaSchemaProperties.value?.[name] || {};
  const model = kafkaFieldModel(name);
  if (!model) return null;
  const enumValues = Array.isArray(property.enum) && property.enum.length ? property.enum : kafkaFieldFallbackEnum(name);
  return {
    name,
    model,
    label: property.title || kafkaFieldLabel(name),
    testid: `kafka-${name.replaceAll("_", "-")}`,
    kind: property.type === "boolean" || enumValues.length ? "select" : "input",
    options: property.type === "boolean" ? [{ value: "off", label: "关闭" }, { value: "on", label: "开启" }] : enumValues.map((value) => ({ value, label: value })),
    placeholder: kafkaFieldPlaceholder(name)
  };
}
function kafkaFieldModel(name) {
  return {
    brokers: "brokers",
    topic: "topic",
    consumer_group: "consumerGroup",
    start_offset: "startOffset",
    security_protocol: "securityProtocol",
    encoding: "encodingKafka",
    log_filter_enabled: "logFilterEnabledKafka",
    log_filter_regex: "logFilterRegexKafka"
  }[name] || "";
}
function kafkaFieldLabel(name) {
  return {
    brokers: "Broker 地址",
    topic: "Topic",
    consumer_group: "消费组",
    start_offset: "消费策略",
    security_protocol: "通信协议",
    encoding: "字符编码",
    log_filter_enabled: "日志筛选",
    log_filter_regex: "正则筛选"
  }[name] || name;
}
function kafkaFieldPlaceholder(name) {
  return {
    brokers: "10.0.0.1:9092,10.0.0.2:9092",
    topic: "xdp-events",
    consumer_group: "xdp-consumer",
    log_filter_regex: "^allow|^accept"
  }[name] || "";
}
function kafkaFieldFallbackEnum(name) {
  return {
    start_offset: ["earliest", "latest"],
    security_protocol: ["PLAINTEXT", "SASL_PLAINTEXT", "SASL_SSL", "SSL"],
    encoding: ["UTF-8", "GBK", "ISO-8859-1"]
  }[name] || [];
}
function applyKafkaSchemaDefaults() {
  for (const field of kafkaFormFields.value) {
    if (field.kind !== "select" || !field.options.length) continue;
    const current = String(inputForm[field.model] || "");
    if (!field.options.some((option) => String(option.value) === current)) {
      inputForm[field.model] = field.options[0].value;
    }
  }
}
function isKafkaFieldVisible(field) {
  return field.name !== "log_filter_regex" || inputForm.logFilterEnabledKafka === "on";
}
async function saveInput() {
  inputPortError.value = "";
  inputNameError.value = "";
  inputFormError.value = "";
  inputFormNotice.value = "";
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
  const notice = request.plugin_code === "kafka" ? "Kafka 配置已保存，运行时消费将按状态热加载" : "";
  resetInputForm();
  inputFormNotice.value = notice;
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
async function checkKafkaConnectivity() {
  inputFormError.value = "";
  kafkaConnectivityStatus.value = "";
  const validationMessage = validateKafkaConnectivityForm(inputForm);
  if (validationMessage) {
    kafkaConnectivityStatus.value = validationMessage;
    return;
  }
  try {
    const response = await requestJSON("/api/v1/datasources/connectivity-check", {
      auth: true,
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        plugin_code: "kafka",
        plugin_config: inputFormToAPI({ ...inputForm, plugin: "Kafka" }).plugin_config
      })
    });
    kafkaConnectivityStatus.value = response.message || "Kafka 连通性正常";
  } catch (error) {
    kafkaConnectivityStatus.value = error.message || "Kafka 连通性失败";
  }
}
function openInputForm() {
  clearInputForm();
}
function clearInputForm() {
  inputPortError.value = "";
  inputNameError.value = "";
  inputFormError.value = "";
  inputFormNotice.value = "";
  kafkaConnectivityStatus.value = "";
  editingInputId.value = "";
  assignReactive(inputForm, defaultInputForm());
  showInputForm.value = true;
}
function editInput(item) {
  inputPortError.value = "";
  inputNameError.value = "";
  inputFormError.value = "";
  inputFormNotice.value = "";
  kafkaConnectivityStatus.value = "";
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
  if (runtimeStatus === "running" && listenerStatus === "consuming") return { state: "running", label: "运行中" };
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
  if (String(item.plugin || "").toLowerCase() === "kafka") {
    return item.listenerEndpoint || "-";
  }
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
function formatFullTime(value) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  return `${date.getFullYear()}-${pad2(date.getMonth() + 1)}-${pad2(date.getDate())} ${pad2(date.getHours())}:${pad2(date.getMinutes())}:${pad2(date.getSeconds())}`;
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
  if (form.plugin === "Kafka") {
    if (!String(form.brokers || "").trim()) return "Broker 地址为必填项";
    if (!String(form.topic || "").trim()) return "Topic 为必填项";
    if (!String(form.consumerGroup || "").trim()) return "消费组为必填项";
    if (!String(form.startOffset || "").trim()) return "消费策略为必填项";
    if (!String(form.securityProtocol || "").trim()) return "通信协议为必填项";
    if (!String(form.encodingKafka || "").trim()) return "字符编码为必填项";
    if (!String(form.logFilterEnabledKafka || "").trim()) return "日志筛选为必填项";
    if (form.logFilterEnabledKafka === "on" && !String(form.logFilterRegexKafka || "").trim()) return "正则筛选为必填项";
  }
  return "";
}
function validateKafkaConnectivityForm(form) {
  if (!String(form.brokers || "").trim()) return "Broker 地址为必填项";
  if (!String(form.topic || "").trim()) return "Topic 为必填项";
  if (!String(form.consumerGroup || "").trim()) return "消费组为必填项";
  if (!String(form.startOffset || "").trim()) return "消费策略为必填项";
  if (!String(form.securityProtocol || "").trim()) return "通信协议为必填项";
  if (!String(form.encodingKafka || "").trim()) return "字符编码为必填项";
  if (!String(form.logFilterEnabledKafka || "").trim()) return "日志筛选为必填项";
  if (form.logFilterEnabledKafka === "on" && !String(form.logFilterRegexKafka || "").trim()) return "正则筛选为必填项";
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
    brokers: splitCSV(form.brokers),
    topic: form.topic,
    consumer_group: form.consumerGroup,
    start_offset: form.startOffset || "earliest",
    security_protocol: form.securityProtocol,
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
    return { ...defaultInputForm(), id: source.id, name: source.name, plugin: "Kafka", status: source.status, runtimeStatus: source.runtime_status || "", listenerStatus: source.listener_status || "", listenerEndpoint: source.listener_endpoint || "", internalRawTopic: source.internal_raw_topic || makeInputRoute(source.name), brokers: Array.isArray(config.brokers) ? config.brokers.join(",") : config.brokers || "", topic: config.topic || "", consumerGroup: config.consumer_group || "", securityProtocol: config.security_protocol || "PLAINTEXT", startOffset: config.start_offset || "earliest", encodingKafka: config.encoding || "UTF-8", logFilterEnabledKafka: config.log_filter_enabled ? "on" : "off", logFilterRegexKafka: config.log_filter_regex || "" };
  }
  return { ...defaultInputForm(), id: source.id, name: source.name, plugin: "Syslog", status: source.status, runtimeStatus: source.runtime_status || "", listenerStatus: source.listener_status || "", listenerEndpoint: source.listener_endpoint || "", internalRawTopic: source.internal_raw_topic || makeInputRoute(source.name), collectorPort: String(config.collector_port || parsePort(source.addr) || "5514"), transportProtocol: String(config.transport_protocol || source.protocol || "UDP").toUpperCase(), encoding: config.encoding || "UTF-8", logFilterEnabled: config.log_filter_enabled ? "on" : "off", logFilterRegex: config.log_filter_regex || "" };
}
function parsePort(value) { return Number(String(value || "").replace(/^:/, "")) || ""; }
function parsePortFromEndpoint(value) {
  const match = String(value || "").match(/:(\d+)(?:\/)?$/);
  return match ? match[1] : "";
}
function toNumber(value, fallback) { const parsed = Number(value); return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback; }
function splitCSV(value) { return String(value || "").split(",").map((item) => item.trim()).filter(Boolean); }

function makeInputRoute(name) { return `raw.ds_${String(name || "source").toLowerCase().replace(/[^a-z0-9]+/g, "_").replace(/^_+|_+$/g, "") || "source"}`; }
function applyDataSourceRoute() { const item = inputConfigs.value.find((input) => input.name === ruleForm.dataSourceName); ruleForm.inputRoute = item ? item.internalRawTopic : "internal_raw_topic"; }
function selectParserPlugin(plugin) {
  ruleForm.pluginCode = plugin.plugin_code;
  ruleForm.pluginVersion = plugin.plugin_version || "1.0.0";
  ruleForm.plugin = parserPluginLabel(plugin.plugin_code);
  syncPropsConf();
}
function parserPluginDisplayName(plugin) {
  if (plugin.plugin_code === "regex") return "正则";
  if (plugin.plugin_code === "json-parser") return "JSON";
  return plugin.name || plugin.plugin_code;
}
function parserPluginIcon(plugin) {
  if (plugin.plugin_code === "regex") return "REG";
  if (plugin.plugin_code === "json-parser") return "JSON";
  return String(plugin.plugin_code || "P").slice(0, 4).toUpperCase();
}
function parserPluginIconClass(plugin) {
  if (plugin.plugin_code === "regex") return "icon-regex";
  if (plugin.plugin_code === "json-parser") return "icon-json";
  return "icon-kv";
}
async function previewParse() {
  const request = ruleFormToAPI(ruleForm);
  try {
    const id = editingRuleId.value || "preview";
    const response = await requestJSON(`/api/v1/parse-rules/${encodeURIComponent(id)}/test`, { auth: true, method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(request) });
    previewRows.value = response.fields || [];
    return;
  } catch {
    const sample = ruleForm.sampleLog || "";
    previewRows.value = ruleForm.pluginCode === "json-parser" ? previewJson(sample) : previewRegex(sample, ruleForm.regexPattern);
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
  try {
    const saved = await requestJSON(url, { auth: true, method, headers: { "Content-Type": "application/json" }, body: JSON.stringify(request) });
    const item = apiRuleToForm(saved);
    if (!editingRuleId.value) adjustListPaginationTotal(parsePagination, 1, parsePageSize.value);
    parseRules.value = editingRuleId.value ? parseRules.value.map((current) => current.id === editingRuleId.value ? item : current) : [item, ...parseRules.value];
    resetRuleForm();
  } catch (error) {
    ruleFormError.value = error.message || "解析规则保存失败";
  }
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
  if (!String(form.pluginCode || "").trim()) return "解析方式为必填项";
  if (!String(form.sampleLog || "").trim()) return "日志样例为必填项";
  if (form.pluginCode === "regex" && !String(form.regexPattern || "").trim()) return "正则表达式为必填项";
  if (!String(form.propsConf || "").trim()) return "最终 props.conf 配置项为必填项";
  return "";
}
function buildPropsConf(data) {
  const sourceName = String(data.name || "custom").trim().toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "") || "custom";
  if (data.pluginCode === "json-parser") return `[source::${sourceName}]\nINDEXED_EXTRACTIONS = json\nKV_MODE = none`;
  return `[source::${sourceName}]\nEXTRACT-custom = ${(data.regexPattern || "field=(?<field>\\S+)").replace(/\?P</g, "?<")}`;
}
function ruleFormToAPI(form) {
  const plugin = form.pluginCode || parserPluginCode(form.plugin);
  return { name: form.name, status: "active", parser_plugin: plugin, parser_plugin_version: form.pluginVersion || "1.0.0", data_source_name: form.dataSourceName, input_route: form.inputRoute || "internal_raw_topic", output_index: form.outputIndex || "app", priority: Number(form.priority || 100), stage: "ingest", sample_event: form.sampleLog, plugin_config: pluginConfigFromRuleForm(form, plugin), props_conf: form.propsConf || buildPropsConf(form) };
}
function pluginConfigFromRuleForm(form, plugin) {
  if (plugin === "regex") return { source_field: "raw", regex_pattern: form.regexPattern, target: "fields", field_types: {}, on_no_match: "continue" };
  if (plugin === "json-parser") return { source_field: "raw", target: "fields", flatten_nested: true, flatten_separator: ".", array_mode: form.jsonArrayMode || "json_string", on_invalid_json: form.jsonInvalidPolicy || "continue" };
  return {};
}
function apiRuleToForm(rule) {
  const config = rule.plugin_config || {};
  const plugin = parserPluginLabel(rule.parser_plugin);
  return { ...defaultRuleForm(), id: rule.id, name: rule.name, plugin, pluginCode: rule.parser_plugin || "regex", pluginVersion: rule.parser_plugin_version || "1.0.0", dataSourceName: rule.data_source_name || "", inputRoute: rule.input_route || "internal_raw_topic", outputIndex: rule.output_index || "app", priority: Number(rule.priority || 100), sampleLog: rule.sample_event || "", regexPattern: config.regex_pattern || "", jsonArrayMode: config.array_mode || "json_string", jsonInvalidPolicy: config.on_invalid_json || "continue", kvPairDelimiter: displayDelimiter(config.field_delimiter), kvDelimiter: config.kv_delimiter || "=", kvQuote: config.field_quote || '"', delimitedDelimiter: config.field_delimiter || ",", delimitedQuote: config.field_quote || '"', delimitedFields: Array.isArray(config.field_names) ? config.field_names.join(",") : (config.field_names || "field1,field2,field3"), propsConf: rule.props_conf || "" };
}

function parserPluginCode(label) { return { "正则解析插件": "regex", "JSON 解析插件": "json-parser" }[label] || label; }
function parserPluginLabel(code) { return { regex: "正则解析插件", "json-parser": "JSON 解析插件" }[code] || code; }
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
  const endpoint = editingIndexId.value ? `/api/v1/indexes/${encodeURIComponent(request.index_name)}` : "/api/v1/indexes";
  const method = editingIndexId.value ? "PUT" : "POST";
  const saved = await requestJSON(endpoint, { auth: true, method, headers: { "Content-Type": "application/json" }, body: JSON.stringify(request) });
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
function handleConfigDrawerOutsidePointerDown(event) {
  if (!showInputForm.value && !showRuleForm.value && !showIndexForm.value) return;
  const target = event.target;
  if (target?.closest?.(".config-drawer")) return;
  if (showInputForm.value) resetInputForm();
  if (showRuleForm.value) resetRuleForm();
  if (showIndexForm.value) resetIndexForm();
}
function apiIndexToForm(index) {
  const name = index.index_name || index.name || "";
  return {
    id: name || nextId("idx"),
    name,
    tableName: index.table_name || (name ? `events_${name}` : ""),
    ttl: Number(index.ttl_days || index.ttl || 30),
    physicalTtl: Number(index.physical_ttl_days || index.physicalTtl || 0),
    rows: Number(index.rows || 0),
    storageBytes: Number(index.storage_bytes || index.storageBytes || 0),
    latestEventTime: index.latest_event_time || index.latestEventTime || "",
    status: index.status || "active",
    system: Boolean(index.system),
    indexType: index.index_type || (String(name).startsWith("_") ? "system" : "business"),
    trend: index.trend || null,
    trendOpen: false,
    trendLoading: false,
    trendError: ""
  };
}
function isSystemIndex(item) { return Boolean(item?.system || item?.indexType === "system" || String(item?.name || "").startsWith("_")); }
function upsertIndexForm(items, item) {
  const exists = items.some((current) => current.name === item.name);
  return exists ? items.map((current) => current.name === item.name ? item : current) : [item, ...items];
}
async function loadIndexTrend(item) {
  if (item.trendOpen) {
    item.trendOpen = false;
    return;
  }
  item.trendOpen = true;
  item.trendLoading = true;
  item.trendError = "";
  try {
    item.trend = normalizeIndexTrend(await requestJSON(`/api/v1/indexes/${encodeURIComponent(item.name)}/trend?days=7`, { auth: true }));
  } catch (error) {
    item.trendError = error.message || "容量趋势加载失败";
  } finally {
    item.trendLoading = false;
  }
}
function indexTrendBarHeight(item, point) {
  const rows = Number(point?.rows || 0);
  const maxRows = Math.max(...(item.trend?.points || []).map((entry) => Number(entry.rows || 0)), 1);
  return rows <= 0 ? 4 : Math.max(10, Math.round((rows / maxRows) * 100));
}
function indexTrendTicks(item) {
  const points = Array.isArray(item?.trend?.points) ? item.trend.points : [];
  if (!points.length) return [];
  const labels = points.map((point, index) => ({ key: `${indexTrendPointLabel(point)}-${index}`, label: indexTrendPointLabel(point) }));
  if (points.length <= 3) {
    return labels;
  }
  const indexes = Array.from(new Set([0, Math.floor((points.length - 1) / 2), points.length - 1])).sort((a, b) => a - b);
  return indexes.map((index) => labels[index]);
}
function indexTrendYTicks(item) {
  const points = Array.isArray(item?.trend?.points) ? item.trend.points : [];
  const maxRows = Math.max(...points.map((entry) => Number(entry.rows || 0)), 0);
  const middle = Math.round(maxRows / 2);
  return [
    { key: "max", label: `${formatNumber(maxRows)} 条` },
    { key: "middle", label: `${formatNumber(middle)} 条` },
    { key: "zero", label: "0 条" }
  ];
}
function indexTrendPointLabel(point) {
  const raw = point?.captured_at || point?.capturedAt || point?.date || "";
  const text = String(raw);
  const date = new Date(text);
  if (!Number.isNaN(date.getTime())) {
    const month = String(date.getMonth() + 1).padStart(2, "0");
    const day = String(date.getDate()).padStart(2, "0");
    const hour = String(date.getHours()).padStart(2, "0");
    const minute = String(date.getMinutes()).padStart(2, "0");
    return `${month}-${day} ${hour}:${minute}`;
  }
  const compact = text.match(/^(\d{4})(\d{2})(\d{2})$/);
  if (compact) return `${compact[2]}-${compact[3]}`;
  const isoDate = text.match(/^(\d{4})-(\d{2})-(\d{2})/);
  if (isoDate) return `${isoDate[2]}-${isoDate[3]}`;
  return text || "-";
}
function normalizeIndexTrend(trend) {
  return {
    ...trend,
    rows_growth_7d: Number(trend?.rows_growth_7d ?? trend?.rowsGrowth7d ?? 0),
    storage_growth_bytes_7d: Number(trend?.storage_growth_bytes_7d ?? trend?.storageGrowthBytes7d ?? 0),
    current_rows: Number(trend?.current_rows ?? trend?.currentRows ?? 0),
    current_storage_bytes: Number(trend?.current_storage_bytes ?? trend?.currentStorageBytes ?? 0),
    points: Array.isArray(trend?.points) ? trend.points : [],
    snapshot_retention_days: Number(trend?.snapshot_retention_days ?? trend?.snapshotRetentionDays ?? 0)
  };
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
    resultMode.value = response.mode === "stats" ? "stats" : (response.mode === "table" ? "table" : "events");
    updateSearchPagination(response.pagination);
    if (resultMode.value === "stats") {
      statsFields.value = response.stats?.fields?.length ? response.stats.fields : inferFields(response.stats?.rows || []);
      searchResults.value = response.stats?.rows || [];
      expandedEvents.value = new Set();
      searchTimeRangeText.value = formatResponseTimeRange(response.time_range);
      resultStatus.value = `统计视图 · ${formatNumber(searchPagination.value.total || searchPagination.value.returned)} 组 · ${searchTimeRangeText.value || "未限定时间"} · ${response.elapsed_ms ?? 0}ms`;
      return;
    }
    if (resultMode.value === "table") {
      statsFields.value = response.table?.fields?.length ? response.table.fields : inferFields(response.table?.rows || []);
      searchResults.value = response.table?.rows || [];
      expandedEvents.value = new Set();
      searchTimeRangeText.value = formatResponseTimeRange(response.time_range);
      resultStatus.value = `表格视图 · ${formatNumber(searchPagination.value.total || searchPagination.value.returned)} 行 · ${searchTimeRangeText.value || "未限定时间"} · ${response.elapsed_ms ?? 0}ms`;
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
async function loadSavedSearches(force = false) {
  if (savedSearchesLoaded.value && !force) return;
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
function formatBytes(value) {
  const bytes = Number(value || 0);
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let size = bytes;
  let unitIndex = 0;
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex += 1;
  }
  const formatted = size >= 10 || unitIndex === 0 ? size.toFixed(0) : size.toFixed(1).replace(/\.0$/, "");
  return `${formatted} ${units[unitIndex]}`;
}
function formatWriterEPS(value) {
  const eps = Number(value || 0);
  if (!Number.isFinite(eps)) return "0";
  if (eps >= 10) return eps.toFixed(1);
  return eps.toFixed(2).replace(/\.00$/, "");
}
function formatPercent(value) {
  const ratio = Number(value || 0);
  if (!Number.isFinite(ratio)) return "0%";
  return `${(ratio * 100).toFixed(ratio > 0 && ratio < 0.01 ? 2 : 1)}%`;
}
function formatIndexDateTime(value) {
  const formatted = formatFullDateTime(value);
  return formatted || "—";
}
function formatTimelineTick(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return String(value || "").slice(0, 16);
  return `${pad2(date.getMonth() + 1)}/${pad2(date.getDate())} ${pad2(date.getHours())}:${pad2(date.getMinutes())}`;
}

function setPositivePageSize(target, value, fallback = 10) {
  const parsed = Number(value);
  target.value = Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}
function setCollectPageSize(value) { setPositivePageSize(collectPageSize, value, 10); }
function setParsePageSize(value) { setPositivePageSize(parsePageSize, value, 10); }
function setIndexPageSize(value) { setPositivePageSize(indexPageSize, value, 10); }
function setSearchPageSize(value) { setPositivePageSize(searchPageSize, value, 20); }
function setCurrentPluginPageSize(value) { currentPluginPageSize.value = Number(value) || 10; }
function setSearchQuery(value) { searchQuery.value = value; }
function setSearchTime(value) { searchTime.value = value; }

const panelBindings = computed(() => ({
  inputPluginBadge: inputPluginBadge.value,
  inputFormNotice: inputFormNotice.value,
  pluginCatalogErrors,
  pluginCatalogLoading,
  retryInputPluginCatalog,
  retryParserPluginCatalog,
  showInputForm: showInputForm.value,
  openInputForm,
  editingInputId: editingInputId.value,
  clearInputForm,
  saveInput,
  inputForm,
  inputNameError: inputNameError.value,
  canUseSyslogInput: canUseSyslogInput.value,
  kafkaInputPlugin: kafkaInputPlugin.value,
  selectInputPlugin,
  inputPortError: inputPortError.value,
  kafkaFormFields: kafkaFormFields.value,
  isKafkaFieldVisible,
  checkKafkaConnectivity,
  kafkaConnectivityStatus: kafkaConnectivityStatus.value,
  inputFormError: inputFormError.value,
  resetInputForm,
  inputConfigs: inputConfigs.value,
  selectedRuntimeId: selectedRuntimeId.value,
  collectRuntimeSummary,
  collectListenerPortLabel,
  collectCanStop,
  collectCanStart,
  selectRuntimeSource,
  toggleInputStatus,
  loadRuntimeDetail,
  retryRuntimeSource,
  editInput,
  deleteInput,
  runtimeDetail: runtimeDetail.value,
  selectedRuntimeName: selectedRuntimeName.value,
  runtimeLoading: runtimeLoading.value,
  runtimeError: runtimeError.value,
  runtimeDetailSummary: runtimeDetailSummary.value,
  formatRuntimeNumber,
  formatRuntimeValue,
  formatFullTime,
  runtimeTopology,
  collectPagination: collectPagination.value,
  visibleCollectPages: visibleCollectPages.value,
  totalCollectPages: totalCollectPages.value,
  goCollectPage,
  collectPageSize: collectPageSize.value,
  setCollectPageSize,
  reloadCollectFirstPage,
  showRuleForm: showRuleForm.value,
  openRuleForm,
  editingRuleId: editingRuleId.value,
  clearRuleForm,
  saveRule,
  ruleForm,
  applyDataSourceRoute,
  businessIndexes: businessIndexes.value,
  parserPluginOptions: parserPluginOptions.value.map((plugin) => ({
    ...plugin,
    label: parserPluginDisplayName(plugin),
    icon: parserPluginIcon(plugin),
    iconClass: parserPluginIconClass(plugin)
  })),
  selectParserPlugin,
  previewParse,
  previewRows: previewRows.value,
  ruleFormError: ruleFormError.value,
  resetRuleForm,
  parseRules: parseRules.value,
  editRule,
  deleteRule,
  parsePagination: parsePagination.value,
  visibleParsePages: visibleParsePages.value,
  totalParsePages: totalParsePages.value,
  goParsePage,
  parsePageSize: parsePageSize.value,
  setParsePageSize,
  reloadParseFirstPage,
  loadWriterRuntime,
  writerRuntimeLoading: writerRuntimeLoading.value,
  writerRuntimeError: writerRuntimeError.value,
  writerRuntime: writerRuntime.value,
  formatWriterEPS,
  formatNumber,
  formatBytes,
  formatPercent,
  showIndexForm: showIndexForm.value,
  openIndexForm,
  editingIndexId: editingIndexId.value,
  clearIndexForm,
  saveIndex,
  indexForm,
  indexFormError: indexFormError.value,
  resetIndexForm,
  indexes: indexes.value,
  loadIndexTrend,
  editIndex,
  deleteIndex,
  indexTrendYTicks,
  indexTrendPointLabel,
  indexTrendBarHeight,
  indexTrendTicks,
  formatIndexDateTime,
  indexPagination: indexPagination.value,
  visibleIndexPages: visibleIndexPages.value,
  totalIndexPages: totalIndexPages.value,
  goIndexPage,
  indexPageSize: indexPageSize.value,
  setIndexPageSize,
  reloadIndexFirstPage,
  searchQuery: searchQuery.value,
  setSearchQuery,
  searchTime: searchTime.value,
  setSearchTime,
  timeOptions,
  runSearchFirstPage,
  saveSearch,
  timelineBars: timelineBars.value,
  timelineYAxisLabels: timelineYAxisLabels.value,
  timelineStatus: timelineStatus.value,
  timelineTooltip,
  timelineTicks: timelineTicks.value,
  savedSearches: savedSearches.value,
  savedOpen: savedOpen.value,
  toggleSavedSearches,
  savedSearchError: savedSearchError.value,
  useSearch,
  deleteSavedSearch,
  resultStatus: resultStatus.value,
  resultMode: resultMode.value,
  statsFields: statsFields.value,
  searchResults: searchResults.value,
  formatStatsCell,
  eventRowKey,
  toggleEventDetail,
  isEventExpanded,
  formatDetailValue,
  searchPagination: searchPagination.value,
  isSearchLoading: isSearchLoading.value,
  totalSearchPages: totalSearchPages.value,
  visibleSearchPages: visibleSearchPages.value,
  goSearchPage,
  searchPageSize: searchPageSize.value,
  setSearchPageSize,
  searchPageSizes,
  pluginTabs: pluginTabs.value,
  currentPluginTab: currentPluginTab.value,
  selectPluginTab,
  pluginTypeCount,
  canManageCurrentPluginTab: canManageCurrentPluginTab.value,
  onPluginFileChange,
  pluginUploadFileName: pluginUploadFileName.value,
  uploadPluginPackage,
  pluginUploadStatus: pluginUploadStatus.value,
  pluginUploadError: pluginUploadError.value,
  currentPluginTabLabel: currentPluginTabLabel.value,
  filteredPlugins: filteredPlugins.value,
  isBuiltInPlugin,
  isPluginEnabled,
  pluginStatusLabel,
  isPluginDetailOpen,
  loadPluginDetail,
  setPluginStatus,
  deletePlugin,
  selectedPlugin: selectedPlugin.value,
  pluginReferenceCount: pluginReferenceCount.value,
  pluginEffectiveRuntimeConfig: pluginEffectiveRuntimeConfig.value,
  pluginEffectiveRuntimeText: pluginEffectiveRuntimeText.value,
  pluginSchemaText: pluginSchemaText.value,
  pluginUISchemaText: pluginUISchemaText.value,
  pluginReferenceItems: pluginReferenceItems.value,
  referenceTypeLabel,
  showPluginExecutionAudits: showPluginExecutionAudits.value,
  pluginExecutionAuditLoading: pluginExecutionAuditLoading.value,
  pluginExecutionAuditError: pluginExecutionAuditError.value,
  pluginExecutionAudits: pluginExecutionAudits.value,
  isPluginReferenced,
  pluginActionStatus: pluginActionStatus.value,
  pluginActionError: pluginActionError.value,
  currentPluginPagination: currentPluginPagination.value,
  visiblePluginPages: visiblePluginPages.value,
  totalPluginPages: totalPluginPages.value,
  goPluginPage,
  currentPluginPageSize: currentPluginPageSize.value,
  setCurrentPluginPageSize,
  reloadPluginFirstPage,
  rbacNotice: rbacNotice.value,
  rbacError: rbacError.value,
  hasPermission,
  saveRBACUser,
  rbacUserForm,
  rbacUserPagination: rbacUserPagination.value,
  editingRBACUserId: editingRBACUserId.value,
  rbacRoles: rbacRoles.value,
  rbacUserError: rbacUserError.value,
  resetRBACUserForm,
  rbacUsers: rbacUsers.value,
  roleNames,
  editRBACUser,
  toggleRBACUserStatus,
  resetRBACUserPassword,
  deleteRBACUser,
  saveRBACRole,
  rbacRoleForm,
  editingRBACRoleId: editingRBACRoleId.value,
  assignableRBACPermissions: assignableRBACPermissions.value,
  permissionTestId,
  rbacRoleError: rbacRoleError.value,
  resetRBACRoleForm,
  formatIndexScopes,
  formatPluginScopes,
  compactList,
  editRBACRole,
  deleteRBACRole,
  listPageSizes
}));
</script>

<style>
*{box-sizing:border-box}:root{--xdp-bg:#070925;--xdp-bg2:#12091f;--xdp-ink:#f8fbff;--xdp-muted:#a5b2d1;--xdp-line:rgba(151,173,255,.18);--xdp-glass:rgba(10,15,45,.78);--xdp-glass2:rgba(19,27,72,.82);--xdp-orange:#ffad00;--xdp-coral:#ff6848;--xdp-pink:#ff1f85;--xdp-cyan:#55dfff;--xdp-green:#67f28a;--xdp-danger:#ff5f61;--xdp-radius:22px;--xdp-shadow:0 24px 80px rgba(0,0,0,.42);--xdp-sans:"Avenir Next","PingFang SC","Microsoft YaHei",sans-serif;--xdp-mono:"SFMono-Regular","Menlo","Consolas",monospace}body{margin:0;min-height:100vh;font-family:var(--xdp-sans);background:#070925}button,input,select,textarea{font:inherit}button{cursor:pointer}.login-shell,.console-shell{min-height:100vh;position:relative;overflow-x:hidden;color:var(--xdp-ink);background:radial-gradient(circle at 74% 8%,rgba(255,31,133,.24),transparent 24rem),radial-gradient(circle at 10% 28%,rgba(85,223,255,.15),transparent 26rem),linear-gradient(115deg,#071348 0%,#080a2b 44%,#15061e 100%)}.page-grid{position:absolute;inset:0;background:repeating-linear-gradient(90deg,transparent 0 78px,rgba(255,115,49,.13) 79px,transparent 81px),repeating-linear-gradient(0deg,transparent 0 138px,rgba(85,223,255,.045) 139px,transparent 141px);opacity:.58;pointer-events:none}.login-shell{display:grid;grid-template-rows:auto 1fr auto;gap:38px;padding:22px clamp(18px,4vw,56px) 28px}.topbar,.login-layout,footer,.console-page{position:relative;z-index:1}.topbar{min-height:62px;display:flex;align-items:center;gap:14px;border:1px solid var(--xdp-line);border-radius:999px;padding:0 18px 0 24px;background:rgba(5,8,30,.66);backdrop-filter:blur(18px);box-shadow:0 18px 58px rgba(0,0,0,.3)}.login-shell>.topbar{width:min(1500px,100%);margin:0 auto}.brand{display:flex;align-items:center;gap:9px;margin-right:auto;color:#fff;font-size:22px;font-weight:500;letter-spacing:-.03em}.brand-mark{display:grid;place-items:center;width:32px;height:32px;border-radius:9px;background:linear-gradient(135deg,var(--xdp-orange),var(--xdp-pink));color:#12071c;font-weight:600;box-shadow:0 0 30px rgba(255,78,86,.38)}.pill{border:1px solid rgba(255,255,255,.13);border-radius:999px;padding:8px 12px;color:#e8eeff;font:700 12px var(--xdp-mono);letter-spacing:.08em}.muted,.status-line,.result-meta,.note,.form-hint{color:var(--xdp-muted)}.login-layout{width:min(1280px,100%);margin:auto;display:grid;grid-template-columns:minmax(0,1.08fr) minmax(380px,480px);gap:28px;align-items:stretch}.hero-card,.login-card,.card,.main-panel{border:1px solid var(--xdp-line);border-radius:var(--xdp-radius);background:linear-gradient(180deg,rgba(20,29,76,.82),rgba(6,10,35,.74));box-shadow:var(--xdp-shadow);backdrop-filter:blur(18px)}.hero-card{min-height:520px;position:relative;display:flex;flex-direction:column;justify-content:center;overflow:hidden;padding:clamp(30px,5vw,58px)}.hero-card:after{content:"";position:absolute;right:-92px;bottom:-76px;width:360px;height:260px;border:1px solid rgba(255,255,255,.08);border-radius:60px;background:linear-gradient(135deg,rgba(255,173,0,.12),rgba(255,31,133,.14));transform:rotate(18deg)}.eyebrow{margin:0;color:#c8d4f5;font:700 13px var(--xdp-mono);letter-spacing:.1em;text-transform:uppercase}.hero-card h1{position:relative;z-index:1;margin:18px 0 0;display:grid;gap:12px}.gradient-text{display:inline-block;width:max-content;padding-right:.18em;background:linear-gradient(90deg,var(--xdp-orange),var(--xdp-coral) 46%,var(--xdp-pink));-webkit-background-clip:text;background-clip:text;color:transparent;font-size:clamp(48px,6.4vw,84px);font-weight:700;line-height:1;letter-spacing:-.025em;text-shadow:0 18px 70px rgba(255,54,117,.26)}.hero-card strong{max-width:650px;color:#fff;font-size:clamp(24px,2.8vw,36px);font-weight:700;line-height:1.16;letter-spacing:-.05em}.lede{position:relative;z-index:1;max-width:560px;margin:24px 0 0;color:var(--xdp-muted);font-size:17px;line-height:1.7}.chip-row{position:relative;z-index:1;display:flex;flex-wrap:wrap;gap:10px;margin-top:34px}.chip-row span,.count,.badge,.mode-pill{border:1px solid rgba(85,223,255,.24);border-radius:999px;background:rgba(85,223,255,.12);color:#dffaff;padding:4px 9px;font-size:12px;font-weight:800}.chip-row span{border-color:rgba(255,255,255,.14);background:rgba(255,255,255,.07);color:#e8eeff;padding:8px 12px}.login-card{align-self:center;min-height:470px;padding:28px}.card-head,.result-head{display:flex;align-items:flex-start;justify-content:space-between;gap:18px;margin-bottom:16px;color:#fff;font-weight:800}.login-card h2{margin:8px 0 0;color:#fff;font-size:30px;line-height:1.1;letter-spacing:-.04em}.status-dot{width:14px;height:14px;margin-top:4px;border-radius:999px;background:var(--xdp-green);box-shadow:0 0 0 7px rgba(103,242,138,.1),0 0 28px rgba(103,242,138,.68)}.login-form,.form-grid{display:grid;gap:16px}.login-form label,.form-grid label{display:grid;gap:8px;color:#dce5fb;font-size:13px;font-weight:700}.login-form input,.field,.select,textarea,.search-box{width:100%;border:1px solid rgba(255,255,255,.12);border-radius:14px;outline:none;padding:0 16px;color:#fff;background:rgba(1,4,22,.52);transition:border-color .16s ease,box-shadow .16s ease,background .16s ease}.login-form input,.field,.select{height:44px}.login-form input{height:56px}textarea{min-height:106px;padding:12px 14px;resize:vertical;font-family:var(--xdp-mono)}.props-editor{min-height:150px}.login-form input:focus,.field:focus,.select:focus,textarea:focus,.search-box:focus{border-color:rgba(85,223,255,.78);background:rgba(3,9,34,.74);box-shadow:0 0 0 4px rgba(85,223,255,.1)}.login-form button,.btn,.logout,.topbar-nav button{border:0;cursor:pointer;font-weight:700}.login-form button{height:56px;margin-top:6px;border-radius:14px;background:linear-gradient(90deg,var(--xdp-orange),var(--xdp-coral) 46%,var(--xdp-pink));color:#14071c;font-size:18px;box-shadow:0 18px 42px rgba(255,57,116,.26)}.error-box{margin:16px 0 0;border:1px solid rgba(255,95,97,.35);border-radius:14px;padding:10px 12px;color:#ffcbc6;background:rgba(255,95,97,.1)}.btn.ghost,.logout{border:1px solid rgba(85,223,255,.28);color:#dffaff;background:rgba(85,223,255,.1)}footer{justify-self:center;color:#7f8fb7;font-size:13px}.console-page{width:min(1500px,calc(100vw - 44px));margin:0 auto;padding:22px 0 56px}.console-topbar{position:sticky;top:16px;z-index:20}.console-shell .brand{margin-right:0;flex:0 0 auto}.topbar-nav{display:flex;gap:6px;margin-left:4px;margin-right:auto}.topbar-nav button{color:#b7c2df;background:transparent}.topbar-nav button{border-radius:999px;padding:8px 12px;font-size:14px}.topbar-nav button.active,.topbar-nav button:hover{color:#fff;background:rgba(85,223,255,.12)}.user{color:#bdc9e8;font-size:13px}.logout{border-radius:999px;padding:8px 12px}.workspace{display:block;margin-top:28px}.main-panel{min-width:0;padding:22px}.panel-header{display:flex;align-items:center;justify-content:space-between;gap:16px;margin-bottom:18px}.panel-header h2{margin:0;font-size:30px;letter-spacing:-.04em}.content-grid{display:grid;grid-template-columns:minmax(360px,.9fr) minmax(460px,1.1fr);gap:18px}.card{min-width:0;padding:18px}.plugin-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}.plugin-card{min-height:80px;display:flex;align-items:center;gap:10px;border:1px solid rgba(255,255,255,.12);border-radius:18px;color:#dce5fb;background:rgba(255,255,255,.055);padding:14px;font-weight:800}.plugin-card.active{border-color:rgba(85,223,255,.78);color:#fff;background:rgba(85,223,255,.12);box-shadow:0 0 30px rgba(85,223,255,.1)}.plugin-icon{display:grid;place-items:center;width:38px;height:38px;border-radius:12px;color:#100b22;background:linear-gradient(135deg,var(--xdp-cyan),var(--xdp-green));font:800 12px var(--xdp-mono)}.param-panel,.conditional-panel,.advanced-panel,.preview-box,.saved-drawer{border:1px solid rgba(255,255,255,.1);border-radius:18px;background:rgba(255,255,255,.045);padding:14px}.two{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.unit-field{display:grid;grid-template-columns:1fr 46px;align-items:center}.unit-field .field{border-radius:14px 0 0 14px}.unit-field span{display:grid;place-items:center;height:44px;border:1px solid rgba(255,255,255,.12);border-left:0;border-radius:0 14px 14px 0;color:var(--xdp-muted);background:rgba(255,255,255,.06)}.actions{display:flex;flex-wrap:wrap;gap:10px}.btn{min-height:40px;border-radius:999px;padding:0 16px;color:#071127;background:linear-gradient(135deg,var(--xdp-cyan),var(--xdp-green));box-shadow:0 14px 34px rgba(85,223,255,.18)}.table-wrap{width:100%;overflow-x:auto}table{width:100%;border-collapse:collapse;color:#e7edff;font-size:13px}th,td{border-bottom:1px solid rgba(255,255,255,.08);padding:12px 10px;vertical-align:top;text-align:left}th{color:#c8d4f5;background:rgba(255,255,255,.05)}tr:hover td{background:rgba(85,223,255,.045)}code,.search-box{font-family:var(--xdp-mono)}.multiline-code{white-space:pre-wrap}.muted-code{color:var(--xdp-muted)}.row-actions{display:flex;gap:8px}.link-btn{border:0;padding:0;color:var(--xdp-cyan);background:transparent;font-weight:800}.link-btn.delete{color:#ff9ea0}.search-layout{display:grid;gap:16px}.search-row{display:grid;grid-template-columns:minmax(320px,1fr) 180px auto auto;gap:10px;align-items:center}.search-box{min-height:46px;resize:none;line-height:24px;padding:10px 16px;overflow:auto}.time-help{color:var(--xdp-muted)}.timeline{height:118px;display:flex;align-items:end;gap:8px;border:1px solid rgba(255,255,255,.09);border-radius:18px;padding:14px;background:repeating-linear-gradient(0deg,transparent 0 20px,rgba(255,255,255,.045) 21px,transparent 22px),rgba(255,255,255,.035)}.bar{flex:1;min-width:12px;border-radius:10px 10px 2px 2px;background:linear-gradient(180deg,var(--xdp-cyan),var(--xdp-pink))}.search-toolbar{display:flex;justify-content:space-between;align-items:center;gap:12px}.saved-summary{display:flex;align-items:center;gap:10px;border:1px solid rgba(255,255,255,.1);border-radius:999px;padding:8px 12px;background:rgba(255,255,255,.045);color:var(--xdp-muted)}.saved-summary strong,.result-head>div>span{color:#fff}.drawer-head{display:flex;justify-content:space-between;margin-bottom:10px;font-weight:800}@media (max-width:980px){.content-grid,.login-layout{grid-template-columns:1fr}.topbar-nav{flex-wrap:wrap;margin-left:0}.search-row{grid-template-columns:1fr}}@media (max-width:720px){.login-shell{gap:22px;padding:14px 14px 22px}.topbar{min-height:auto;align-items:flex-start;border-radius:24px;padding:14px}.pill,.user{display:none}.hero-card{min-height:auto;padding:24px}.gradient-text{font-size:clamp(44px,13vw,64px)}.hero-card strong{font-size:22px}.login-card{min-height:auto;padding:22px}.console-page{width:min(100% - 24px,1500px)}.two,.plugin-grid{grid-template-columns:1fr}}

.console-shell[data-theme="ops-console"]{--ops-bg:#f4f7fb;--ops-surface:#ffffff;--ops-surface-soft:#f8fbfd;--ops-ink:#1f2d3d;--ops-muted:#657589;--ops-line:#d9e2ea;--ops-topbar:#18212a;--ops-sidebar:#eef3f7;--ops-primary:#13bfb4;--ops-primary-dark:#0f8f89;--ops-blue:#2878b8;--ops-green:#28b76f;--ops-shadow:0 18px 48px rgba(29,49,70,.12);color:var(--ops-ink);background:linear-gradient(135deg,#eef4f7 0%,#f8fbfc 46%,#edf5f3 100%)}.console-shell[data-theme="ops-console"] .page-grid{background:repeating-linear-gradient(90deg,transparent 0 94px,rgba(23,129,138,.08) 95px,transparent 96px),repeating-linear-gradient(0deg,transparent 0 128px,rgba(40,120,184,.055) 129px,transparent 130px);opacity:.85}.console-shell[data-theme="ops-console"] .console-topbar{border-color:#24313d;background:var(--ops-topbar);box-shadow:0 12px 28px rgba(24,33,42,.22)}.console-shell[data-theme="ops-console"] .brand,.console-shell[data-theme="ops-console"] .user{color:#edf6fb}.console-shell[data-theme="ops-console"] .console-brand-mark{background:linear-gradient(135deg,var(--ops-primary),var(--ops-blue));color:#fff;box-shadow:0 0 0 1px rgba(255,255,255,.12),0 10px 24px rgba(19,191,180,.28)}.console-shell[data-theme="ops-console"] .topbar-nav button{color:#c8d4df}.console-shell[data-theme="ops-console"] .topbar-nav button.active,.console-shell[data-theme="ops-console"] .topbar-nav button:hover{color:#fff;background:rgba(19,191,180,.18)}.console-shell[data-theme="ops-console"] .logout{border-color:rgba(19,191,180,.38);color:#e7fffb;background:rgba(19,191,180,.12)}.console-shell[data-theme="ops-console"] .sidebar,.console-shell[data-theme="ops-console"] .main-panel,.console-shell[data-theme="ops-console"] .card{border-color:var(--ops-line);background:rgba(255,255,255,.94);box-shadow:var(--ops-shadow);backdrop-filter:none}.console-shell[data-theme="ops-console"] .sidebar-title{color:var(--ops-muted)}.console-shell[data-theme="ops-console"] .sidebar button{color:#435365}.console-shell[data-theme="ops-console"] .sidebar button.active,.console-shell[data-theme="ops-console"] .sidebar button:hover{color:#0d766f;background:#dff7f4}.console-shell[data-theme="ops-console"] .panel-header h2{display:flex;align-items:center;gap:10px;color:#162635}.console-shell[data-theme="ops-console"] .page-icon{display:grid;place-items:center;width:34px;height:34px;border-radius:10px;color:#fff;font:800 11px var(--xdp-mono);letter-spacing:.04em;box-shadow:0 10px 24px rgba(19,191,180,.2)}.console-shell[data-theme="ops-console"] .page-icon-collect{background:linear-gradient(135deg,#0fb7a9,#28b76f)}.console-shell[data-theme="ops-console"] .page-icon-parse{background:linear-gradient(135deg,#2878b8,#2ab7ca)}.console-shell[data-theme="ops-console"] .page-icon-index{background:linear-gradient(135deg,#1f6fa4,#4f86d9)}.console-shell[data-theme="ops-console"] .page-icon-search{background:linear-gradient(135deg,#0f8f89,#1f6fa4)}.console-shell[data-theme="ops-console"] .badge,.console-shell[data-theme="ops-console"] .count,.console-shell[data-theme="ops-console"] .mode-pill{border-color:#b9e8e4;background:#e4f8f5;color:#08776f}.console-shell[data-theme="ops-console"] .card-head,.console-shell[data-theme="ops-console"] .result-head{color:#1f2d3d}.console-shell[data-theme="ops-console"] .muted,.console-shell[data-theme="ops-console"] .status-line,.console-shell[data-theme="ops-console"] .result-meta,.console-shell[data-theme="ops-console"] .note,.console-shell[data-theme="ops-console"] .form-hint{color:var(--ops-muted)}.console-shell[data-theme="ops-console"] .form-grid label{color:#344558}.console-shell[data-theme="ops-console"] .field,.console-shell[data-theme="ops-console"] .select,.console-shell[data-theme="ops-console"] textarea,.console-shell[data-theme="ops-console"] .search-box{border-color:#cfd9e3;color:#1c2c3d;background:#fff}.console-shell[data-theme="ops-console"] .field:focus,.console-shell[data-theme="ops-console"] .select:focus,.console-shell[data-theme="ops-console"] textarea:focus,.console-shell[data-theme="ops-console"] .search-box:focus{border-color:var(--ops-primary);background:#fff;box-shadow:0 0 0 4px rgba(19,191,180,.14)}.console-shell[data-theme="ops-console"] .param-panel,.console-shell[data-theme="ops-console"] .conditional-panel,.console-shell[data-theme="ops-console"] .advanced-panel,.console-shell[data-theme="ops-console"] .preview-box,.console-shell[data-theme="ops-console"] .saved-drawer{border-color:#d9e4ec;background:#f8fbfd}.console-shell[data-theme="ops-console"] .plugin-card{border-color:#d6e1e9;color:#243447;background:#fff}.console-shell[data-theme="ops-console"] .plugin-card.active{border-color:var(--ops-primary);color:#0d4d4b;background:#e8fbf8;box-shadow:0 10px 28px rgba(19,191,180,.16)}.console-shell[data-theme="ops-console"] .plugin-icon{color:#fff;box-shadow:0 8px 20px rgba(40,120,184,.16)}.console-shell[data-theme="ops-console"] .icon-syslog{background:linear-gradient(135deg,#0fb7a9,#28b76f)}.console-shell[data-theme="ops-console"] .icon-kafka{background:linear-gradient(135deg,#2878b8,#38bdf8)}.console-shell[data-theme="ops-console"] .icon-regex{background:linear-gradient(135deg,#1f6fa4,#2ab7ca)}.console-shell[data-theme="ops-console"] .icon-json{background:linear-gradient(135deg,#13bfb4,#28b76f)}.console-shell[data-theme="ops-console"] .icon-delimited{background:linear-gradient(135deg,#4f86d9,#2ab7ca)}.console-shell[data-theme="ops-console"] .icon-kv{background:linear-gradient(135deg,#0f8f89,#4f86d9)}.console-shell[data-theme="ops-console"] .btn{color:#fff;background:linear-gradient(135deg,var(--ops-primary),var(--ops-green));box-shadow:0 12px 24px rgba(19,191,180,.22)}.console-shell[data-theme="ops-console"] .btn.ghost{border-color:#b8e5e1;color:#08776f;background:#eefbf9}.console-shell[data-theme="ops-console"] table{color:#223246}.console-shell[data-theme="ops-console"] th{color:#405168;background:#edf3f7}.console-shell[data-theme="ops-console"] td{border-color:#e5ebf1}.console-shell[data-theme="ops-console"] tr:hover td{background:#f1fbfa}.console-shell[data-theme="ops-console"] code{color:#0f6378}.console-shell[data-theme="ops-console"] .link-btn{color:#087eab}.console-shell[data-theme="ops-console"] .link-btn.delete{color:#c2410c}.console-shell[data-theme="ops-console"] .unit-field span{border-color:#cfd9e3;color:var(--ops-muted);background:#edf3f7}.console-shell[data-theme="ops-console"] .time-help{color:var(--ops-muted)}.console-shell[data-theme="ops-console"] .timeline{border-color:#d8e4ec;background:repeating-linear-gradient(0deg,transparent 0 20px,rgba(40,120,184,.08) 21px,transparent 22px),#fff}.console-shell[data-theme="ops-console"] .bar{background:linear-gradient(180deg,#2878b8,#13bfb4 62%,#28b76f)}.console-shell[data-theme="ops-console"] .saved-summary{border-color:#d9e4ec;background:#fff;color:var(--ops-muted)}.console-shell[data-theme="ops-console"] .saved-summary strong,.console-shell[data-theme="ops-console"] .result-head>div>span{color:#1f2d3d}
.login-shell[data-theme="ops-login"]{--ops-bg:#f4f7fb;--ops-surface:#ffffff;--ops-surface-soft:#f8fbfd;--ops-ink:#1f2d3d;--ops-muted:#657589;--ops-line:#d9e2ea;--ops-topbar:#18212a;--ops-primary:#13bfb4;--ops-primary-dark:#0f8f89;--ops-blue:#2878b8;--ops-green:#28b76f;--ops-shadow:0 18px 48px rgba(29,49,70,.12);color:var(--ops-ink);background:linear-gradient(135deg,#eef4f7 0%,#f8fbfc 46%,#edf5f3 100%)}.login-shell[data-theme="ops-login"] .page-grid{background:repeating-linear-gradient(90deg,transparent 0 94px,rgba(23,129,138,.08) 95px,transparent 96px),repeating-linear-gradient(0deg,transparent 0 128px,rgba(40,120,184,.055) 129px,transparent 130px);opacity:.85}.login-shell[data-theme="ops-login"] .topbar{border-color:#24313d;background:var(--ops-topbar);box-shadow:0 12px 28px rgba(24,33,42,.22);backdrop-filter:none}.login-shell[data-theme="ops-login"] .brand{color:#edf6fb}.login-shell[data-theme="ops-login"] .brand-mark{background:linear-gradient(135deg,var(--ops-primary),var(--ops-blue));color:#fff;box-shadow:0 0 0 1px rgba(255,255,255,.12),0 10px 24px rgba(19,191,180,.28)}.login-shell[data-theme="ops-login"] .pill{border-color:rgba(19,191,180,.38);color:#e7fffb;background:rgba(19,191,180,.12)}.login-shell[data-theme="ops-login"] .pill.muted{color:#c8d4df}.login-shell[data-theme="ops-login"] .hero-card,.login-shell[data-theme="ops-login"] .login-card{border-color:var(--ops-line);background:rgba(255,255,255,.94);box-shadow:var(--ops-shadow);backdrop-filter:none}.login-shell[data-theme="ops-login"] .hero-card:after{border-color:rgba(19,191,180,.18);background:linear-gradient(135deg,rgba(19,191,180,.18),rgba(40,120,184,.12))}.login-shell[data-theme="ops-login"] .eyebrow{color:var(--ops-muted)}.login-shell[data-theme="ops-login"] .gradient-text{background:linear-gradient(90deg,var(--ops-primary),var(--ops-blue) 58%,var(--ops-green));-webkit-background-clip:text;background-clip:text;color:transparent;text-shadow:0 18px 70px rgba(19,191,180,.16)}.login-shell[data-theme="ops-login"] .hero-card strong,.login-shell[data-theme="ops-login"] .login-card h2{color:#162635}.login-shell[data-theme="ops-login"] .lede,.login-shell[data-theme="ops-login"] footer{color:var(--ops-muted)}.login-shell[data-theme="ops-login"] .chip-row span{border-color:var(--ops-line);color:#315567;background:#fff}.login-shell[data-theme="ops-login"] .login-form label{color:#344558}.login-shell[data-theme="ops-login"] .login-form input{border-color:#cfd9e3;color:#1c2c3d;background:#fff}.login-shell[data-theme="ops-login"] .login-form input::placeholder{color:#9aa8b7}.login-shell[data-theme="ops-login"] .login-form input:focus{border-color:var(--ops-primary);background:#fff;box-shadow:0 0 0 4px rgba(19,191,180,.14)}.login-shell[data-theme="ops-login"] .login-form button{color:#fff;background:linear-gradient(135deg,var(--ops-primary),var(--ops-green));box-shadow:0 14px 28px rgba(19,191,180,.24)}.login-shell[data-theme="ops-login"] .login-form button:hover{box-shadow:0 18px 34px rgba(19,191,180,.3)}.login-shell[data-theme="ops-login"] .login-form button:focus-visible,.login-shell[data-theme="ops-login"] .btn.ghost:focus-visible{outline:3px solid rgba(19,191,180,.28);outline-offset:3px}.login-shell[data-theme="ops-login"] .status-dot{background:var(--ops-green);box-shadow:0 0 0 7px rgba(40,183,111,.12),0 0 28px rgba(40,183,111,.46)}.login-shell[data-theme="ops-login"] .error-box{border-color:rgba(220,91,75,.28);color:#b43f32;background:rgba(220,91,75,.08)}.login-shell[data-theme="ops-login"] .btn.ghost{border-color:#b8e5e1;color:#08776f;background:#eefbf9}.timeline.empty{align-items:center;justify-content:center}.timeline-empty{color:var(--xdp-muted);font-weight:800}.bar{display:flex;align-items:flex-start;justify-content:center;padding-top:5px;color:#f6fbff;font:800 11px var(--xdp-mono)}.bar span{opacity:.9}.console-shell[data-theme="ops-console"] .bar{color:#fff}
.timeline.timeline-shell{height:156px;display:grid;grid-template-columns:46px minmax(0,1fr);align-items:stretch;gap:10px;padding:12px 14px 10px}.timeline.timeline-shell.empty{display:grid;grid-template-columns:1fr}.timeline-y-axis{display:flex;flex-direction:column;justify-content:space-between;padding:3px 0 24px;text-align:right;color:var(--xdp-muted);font:800 11px var(--xdp-mono)}.timeline-plot{min-width:0;display:grid;grid-template-rows:1fr 22px;position:relative}.timeline-bars{display:flex;align-items:end;gap:3px;min-height:104px;border-left:1px solid rgba(255,255,255,.13);border-bottom:1px solid rgba(255,255,255,.16);padding:0 4px}.timeline-x-axis{display:flex;justify-content:space-between;gap:8px;padding-top:7px;color:var(--xdp-muted);font:800 11px var(--xdp-mono);white-space:nowrap}.timeline.timeline-shell .bar{flex:1;min-width:3px;padding-top:0;border-radius:3px 3px 0 0}.timeline.timeline-shell.empty .timeline-plot{display:grid;place-items:center}.console-shell[data-theme="ops-console"] .timeline-y-axis,.console-shell[data-theme="ops-console"] .timeline-x-axis{color:var(--ops-muted)}.console-shell[data-theme="ops-console"] .timeline-bars{border-left-color:#d4e0e8;border-bottom-color:#c9d8e3}
.expand-col{width:34px}.expand-toggle{border:0;background:transparent;color:var(--xdp-cyan);font:800 13px var(--xdp-mono);cursor:pointer}.event-detail-row:hover td{background:transparent}.event-detail{display:grid;gap:12px;border:1px solid rgba(255,255,255,.1);border-radius:16px;padding:12px;background:rgba(255,255,255,.04)}.detail-raw{display:grid;gap:6px}.detail-raw span{color:var(--xdp-muted);font-size:12px;font-weight:800;text-transform:uppercase}.console-shell[data-theme="ops-console"] .expand-toggle{color:#087eab}.console-shell[data-theme="ops-console"] .event-detail{border-color:#d9e4ec;background:#f8fbfd}.console-shell[data-theme="ops-console"] .detail-raw span{color:var(--ops-muted)}
.pagination-bar{display:flex;align-items:center;justify-content:flex-end;gap:16px;margin:0 -18px -18px;padding:18px 22px 20px;border-top:1px solid rgba(255,255,255,.1);border-radius:0 0 var(--xdp-radius) var(--xdp-radius);background:rgba(255,255,255,.035)}.pagination-controls{display:flex;align-items:center;justify-content:flex-end;gap:10px}.pager-arrow,.pager-page{display:grid;place-items:center;min-width:36px;height:36px;border:1px solid transparent;border-radius:6px;background:transparent;color:var(--xdp-ink);font-weight:800}.pager-arrow{font-size:24px}.pager-page{font-size:16px}.pager-ellipsis{display:grid;place-items:center;min-width:24px;height:36px;color:#7c8796;font-weight:800;letter-spacing:2px}.pager-arrow:not(:disabled):hover,.pager-page:hover{border-color:rgba(255,173,0,.45);background:rgba(255,173,0,.08);color:var(--xdp-orange)}.pager-page.active{border-color:var(--xdp-orange);background:rgba(255,173,0,.1);color:var(--xdp-orange);box-shadow:0 0 0 3px rgba(255,173,0,.08)}.pager-arrow:disabled{color:rgba(165,178,209,.45);cursor:not-allowed}.page-size-select .select{min-width:132px;height:40px;border-radius:8px;padding:0 14px}.console-shell[data-theme="ops-console"] .pagination-bar{border-top-color:#e5ebf1;background:#fff}.console-shell[data-theme="ops-console"] .pager-arrow,.console-shell[data-theme="ops-console"] .pager-page{color:#172638}.console-shell[data-theme="ops-console"] .pager-ellipsis{color:#8996a3}.console-shell[data-theme="ops-console"] .pager-arrow:not(:disabled):hover,.console-shell[data-theme="ops-console"] .pager-page:hover{border-color:rgba(255,122,26,.45);background:#fff5ec;color:#ff7a1a}.console-shell[data-theme="ops-console"] .pager-page.active{border-color:#ff7a1a;background:#fff;color:#ff7a1a;box-shadow:0 0 0 3px rgba(255,122,26,.08)}.console-shell[data-theme="ops-console"] .pager-arrow:disabled{color:#c3cbd2}.console-shell[data-theme="ops-console"] .page-size-select .select{border-color:#cfd9e3;background:#fff;color:#172638}@media (max-width:720px){.pagination-bar{align-items:flex-start;flex-direction:column}.pagination-controls{justify-content:flex-start;flex-wrap:wrap}}
.parser-plugin-grid{grid-template-columns:repeat(4,minmax(0,1fr))}
.index-trend-row:hover td{background:transparent}.index-trend-panel{display:grid;gap:12px;border:1px solid rgba(255,255,255,.1);border-radius:16px;padding:12px;background:rgba(255,255,255,.04)}.index-trend-summary{display:flex;flex-wrap:wrap;gap:10px;color:var(--xdp-muted);font-size:12px;font-weight:800}.index-trend-summary span{border:1px solid rgba(85,223,255,.2);border-radius:999px;padding:5px 9px;background:rgba(85,223,255,.08)}.index-trend-chart{display:grid;gap:6px}.index-trend-plot{display:grid;grid-template-columns:70px minmax(0,1fr);gap:10px;align-items:stretch}.index-trend-y-axis{height:110px;display:flex;flex-direction:column;justify-content:space-between;padding:0 0 22px;text-align:right;color:var(--xdp-muted);font:800 11px var(--xdp-mono);white-space:nowrap}.index-trend-main{min-width:0;display:grid;grid-template-rows:88px 22px}.index-trend-bars{display:flex;align-items:end;gap:6px;border-left:1px solid rgba(255,255,255,.13);border-bottom:1px solid rgba(255,255,255,.16);padding:0 6px}.index-trend-bars span{flex:1;min-width:8px;border-radius:5px 5px 0 0;background:linear-gradient(180deg,var(--xdp-cyan),var(--xdp-green))}.index-trend-x-axis{display:flex;justify-content:space-between;gap:8px;padding:7px 0 0 6px;color:var(--xdp-muted);font:800 11px var(--xdp-mono);white-space:nowrap}.console-shell[data-theme="ops-console"] .index-trend-panel{border-color:#d9e4ec;background:#f8fbfd}.console-shell[data-theme="ops-console"] .index-trend-summary,.console-shell[data-theme="ops-console"] .index-trend-x-axis,.console-shell[data-theme="ops-console"] .index-trend-y-axis{color:var(--ops-muted)}.console-shell[data-theme="ops-console"] .index-trend-summary span{border-color:#b9e8e4;background:#e4f8f5}.console-shell[data-theme="ops-console"] .index-trend-bars{border-left-color:#d4e0e8;border-bottom-color:#c9d8e3}.console-shell[data-theme="ops-console"] .index-trend-bars span{background:linear-gradient(180deg,#2878b8,#13bfb4 70%,#28b76f)}
.collect-runtime-row{cursor:pointer}.collect-runtime-row.selected td{background:rgba(85,223,255,.08)}.collect-runtime-row.abnormal td{box-shadow:inset 3px 0 0 var(--xdp-danger)}.status-pill{display:inline-flex;align-items:center;width:max-content;border:1px solid rgba(85,223,255,.24);border-radius:999px;padding:4px 9px;font-size:12px;font-weight:800;color:#dffaff;background:rgba(85,223,255,.12)}.status-pill.runtime-running{border-color:rgba(103,242,138,.34);color:#dfffe9;background:rgba(103,242,138,.12)}.status-pill.runtime-stopped{border-color:rgba(165,178,209,.28);color:#d8e0f5;background:rgba(165,178,209,.1)}.status-pill.runtime-error{border-color:rgba(255,95,97,.34);color:#ffd4d2;background:rgba(255,95,97,.12)}.runtime-detail-card{margin-top:14px;border:1px solid rgba(255,255,255,.1);border-radius:18px;padding:14px;background:rgba(255,255,255,.045)}.runtime-detail-head{display:flex;align-items:flex-start;justify-content:space-between;gap:12px;margin-bottom:12px}.runtime-detail-head div{display:grid;gap:4px}.runtime-detail-head strong{color:#fff;font-size:16px}.runtime-detail-head span:not(.status-pill){color:var(--xdp-muted);font-size:12px}.runtime-detail-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}.runtime-detail-grid>div{display:grid;gap:6px;border:1px solid rgba(255,255,255,.09);border-radius:14px;padding:12px;background:rgba(1,4,22,.22)}.runtime-detail-grid span{color:var(--xdp-muted);font-size:12px;font-weight:800}.runtime-detail-grid strong{color:#fff;word-break:break-all}.runtime-detail-grid small{color:var(--xdp-muted);word-break:break-all}.runtime-detail-grid .topology{grid-column:1/-1}
.console-shell[data-theme="ops-console"] .collect-runtime-row.selected td{background:#e9fbf8}.console-shell[data-theme="ops-console"] .collect-runtime-row.abnormal td{box-shadow:inset 3px 0 0 #dc5b4b}.console-shell[data-theme="ops-console"] .status-pill.runtime-running{border-color:#b8ead5;color:#0f7a50;background:#e6f8ef}.console-shell[data-theme="ops-console"] .status-pill.runtime-stopped{border-color:#d6e1e9;color:#526174;background:#f4f7fb}.console-shell[data-theme="ops-console"] .status-pill.runtime-error{border-color:#f0c4bd;color:#b43f32;background:#fff1ef}.console-shell[data-theme="ops-console"] .runtime-detail-card{border-color:#d9e4ec;background:#f8fbfd}.console-shell[data-theme="ops-console"] .runtime-detail-head strong,.console-shell[data-theme="ops-console"] .runtime-detail-grid strong{color:#1f2d3d}.console-shell[data-theme="ops-console"] .runtime-detail-head span:not(.status-pill),.console-shell[data-theme="ops-console"] .runtime-detail-grid span,.console-shell[data-theme="ops-console"] .runtime-detail-grid small{color:var(--ops-muted)}.console-shell[data-theme="ops-console"] .runtime-detail-grid>div{border-color:#d9e4ec;background:#fff}
@media (max-width:720px){.parser-plugin-grid{grid-template-columns:1fr}}
@media (max-width:720px){.runtime-detail-grid{grid-template-columns:1fr}}
.panel-header-actions{display:flex;align-items:center;justify-content:flex-end;gap:10px;margin-left:auto}
.plugin-type-tabs{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:10px;margin-bottom:16px}.plugin-type-tabs button{display:flex;align-items:center;justify-content:space-between;gap:10px;border:1px solid rgba(255,255,255,.12);border-radius:16px;padding:14px;color:#dce5fb;background:rgba(255,255,255,.055);font-weight:800}.plugin-type-tabs button.active{border-color:rgba(85,223,255,.78);color:#fff;background:rgba(85,223,255,.12);box-shadow:0 0 30px rgba(85,223,255,.1)}.plugin-type-tabs small{display:grid;place-items:center;min-width:28px;height:24px;border-radius:999px;background:rgba(85,223,255,.14);color:inherit}.plugin-upload-panel{display:grid;grid-template-columns:minmax(260px,1fr) auto minmax(180px,auto);align-items:end;gap:12px}.console-shell[data-theme="ops-console"] .page-icon-plugins{background:linear-gradient(135deg,#13bfb4,#4f86d9)}.console-shell[data-theme="ops-console"] .plugin-type-tabs button{border-color:#d6e1e9;color:#243447;background:#fff}.console-shell[data-theme="ops-console"] .plugin-type-tabs button.active{border-color:var(--ops-primary);color:#0d4d4b;background:#e8fbf8;box-shadow:0 10px 28px rgba(19,191,180,.16)}.console-shell[data-theme="ops-console"] .plugin-type-tabs small{background:#dff7f4;color:#08776f}
.plugin-card:disabled,.plugin-card.locked{cursor:not-allowed;opacity:.7;box-shadow:none}.plugin-file-field{display:grid;gap:8px;color:#344558;font-size:13px;font-weight:700}.plugin-file-control{position:relative;display:flex;align-items:center;min-height:44px;overflow:hidden;border:1px solid rgba(255,255,255,.12);border-radius:14px;background:rgba(1,4,22,.52)}.plugin-file-input{position:absolute;inset:0;width:100%;height:100%;opacity:0;cursor:pointer}.plugin-file-button{display:grid;place-items:center;align-self:stretch;min-width:116px;padding:0 16px;border-right:1px solid rgba(255,255,255,.12);color:#dffaff;background:rgba(85,223,255,.12);font-weight:800}.plugin-file-name{min-width:0;overflow:hidden;padding:0 14px;color:var(--xdp-muted);text-overflow:ellipsis;white-space:nowrap}.plugin-action{min-height:32px;border:1px solid transparent;border-radius:9px;padding:0 12px;font-weight:800;transition:transform .15s ease,box-shadow .15s ease,background .15s ease}.plugin-action:hover{transform:translateY(-1px)}.plugin-action:focus-visible{outline:3px solid rgba(85,223,255,.2);outline-offset:2px}.plugin-action-detail{border-color:rgba(85,223,255,.34);color:#dffaff;background:rgba(85,223,255,.1)}.plugin-action-enable{border-color:rgba(103,242,138,.34);color:#dfffe9;background:rgba(103,242,138,.12)}.plugin-action-disable{border-color:rgba(255,173,0,.34);color:#ffe2a6;background:rgba(255,173,0,.1)}.plugin-action-delete{border-color:rgba(255,95,97,.34);color:#ffd4d2;background:rgba(255,95,97,.1)}.console-shell[data-theme="ops-console"] .plugin-file-control{border-color:#cfd9e3;background:#fff}.console-shell[data-theme="ops-console"] .plugin-file-button{border-right-color:#cfd9e3;color:#08776f;background:#e8fbf8}.console-shell[data-theme="ops-console"] .plugin-file-name{color:var(--ops-muted)}.console-shell[data-theme="ops-console"] .plugin-action-detail{border-color:#9ddbd5;color:#08776f;background:#eefbf9}.console-shell[data-theme="ops-console"] .plugin-action-enable{border-color:#aee1c9;color:#0f7a50;background:#e6f8ef}.console-shell[data-theme="ops-console"] .plugin-action-disable{border-color:#f1cc99;color:#a85a05;background:#fff7e8}.console-shell[data-theme="ops-console"] .plugin-action-delete{border-color:#efb6ad;color:#b43f32;background:#fff1ef}.plugin-detail-inline-row:hover td{background:transparent}.plugin-detail-inline-row>td{padding:14px 10px 18px;background:rgba(85,223,255,.035)}.plugin-detail-inline{display:grid;gap:14px;border:1px solid rgba(85,223,255,.18);border-radius:18px;padding:16px;background:rgba(1,4,22,.22)}.plugin-detail-inline .card-head{margin-bottom:0}.plugin-version-table{border:1px solid rgba(255,255,255,.08);border-radius:14px}.console-shell[data-theme="ops-console"] .plugin-detail-inline-row>td{background:#f3fbfa}.console-shell[data-theme="ops-console"] .plugin-detail-inline{border-color:#cbe7e4;background:#fff}.console-shell[data-theme="ops-console"] .plugin-version-table{border-color:#d9e4ec}
.config-drawer{position:fixed;z-index:40;top:104px;right:max(22px,calc((100vw - 1500px)/2 + 22px));width:min(560px,calc(100vw - 44px));max-height:calc(100vh - 132px);overflow:auto;animation:drawerSlideIn .18s ease-out}.config-drawer:before{content:"";position:fixed;inset:0;z-index:-1;background:rgba(12,22,32,.18);pointer-events:none}.content-grid{grid-template-columns:1fr}@keyframes drawerSlideIn{from{transform:translateX(28px);opacity:.72}to{transform:translateX(0);opacity:1}}@media (max-width:720px){.config-drawer{top:0;right:0;bottom:0;width:100vw;max-height:none;border-radius:0;overflow:auto}}
.content-grid.list-first{grid-template-columns:1fr}
.writer-runtime-card{display:grid;gap:14px}.writer-runtime-grid{display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:12px}.writer-runtime-grid>div{display:grid;gap:5px;border:1px solid rgba(255,255,255,.09);border-radius:15px;padding:12px;background:rgba(1,4,22,.22)}.writer-runtime-grid span{color:var(--xdp-muted);font-size:12px;font-weight:800}.writer-runtime-grid strong{color:#fff;font-size:16px;word-break:break-all}.writer-runtime-grid small{color:var(--xdp-muted);font-size:12px;word-break:break-all}.console-shell[data-theme="ops-console"] .writer-runtime-grid>div{border-color:#d9e4ec;background:#fff}.console-shell[data-theme="ops-console"] .writer-runtime-grid span,.console-shell[data-theme="ops-console"] .writer-runtime-grid small{color:var(--ops-muted)}.console-shell[data-theme="ops-console"] .writer-runtime-grid strong{color:#1f2d3d}@media (max-width:1100px){.writer-runtime-grid{grid-template-columns:repeat(2,minmax(0,1fr))}}@media (max-width:720px){.writer-runtime-grid{grid-template-columns:1fr}}
.catalog-load-error{display:flex;align-items:center;justify-content:space-between;gap:14px;margin:-4px 0 18px;border:1px solid rgba(255,95,97,.35);border-radius:14px;padding:10px 12px;color:#ffcbc6;background:rgba(255,95,97,.1)}.catalog-load-error .btn{min-height:34px;padding:0 13px;box-shadow:none}.catalog-load-error .btn:disabled{cursor:not-allowed;opacity:.55}.console-shell[data-theme="ops-console"] .catalog-load-error{border-color:#efc1b9;color:#a63b2f;background:#fff3f1}.console-shell[data-theme="ops-console"] .catalog-load-error .btn.ghost{border-color:#e5b7af;color:#a63b2f;background:#fff}
.rbac-tabs{margin-bottom:18px}.rbac-grid{grid-template-columns:1fr}.rbac-card-actions{display:flex;align-items:center;gap:12px;margin-left:auto}.rbac-card-actions .btn{min-height:38px;padding:0 16px}.compact-form{margin-bottom:18px}.checkbox-panel{display:grid;gap:8px;border:1px solid rgba(255,255,255,.1);border-radius:16px;padding:12px;background:rgba(255,255,255,.04)}.check-row{display:flex!important;grid-template-columns:none!important;align-items:center;gap:8px!important;color:inherit!important;font-size:13px!important;font-weight:700!important}.check-row input{width:16px;height:16px;accent-color:var(--xdp-cyan)}.rbac-modal-backdrop{position:fixed;z-index:80;inset:0;display:grid;place-items:start center;overflow:auto;padding:76px 22px;background:rgba(14,24,34,.28);backdrop-filter:blur(8px)}.rbac-modal{width:min(1040px,calc(100vw - 44px));display:grid;gap:0;overflow:hidden;border:1px solid rgba(255,255,255,.16);border-radius:22px;background:rgba(12,18,34,.96);box-shadow:0 26px 70px rgba(0,0,0,.32)}.rbac-role-modal{width:min(1120px,calc(100vw - 44px))}.rbac-modal-head{display:flex;align-items:center;justify-content:space-between;gap:18px;padding:20px 24px;border-bottom:1px solid rgba(255,255,255,.1)}.rbac-modal-head h3{margin:0;color:#fff;font-size:22px}.rbac-modal-close{display:grid;place-items:center;width:36px;height:36px;border:1px solid rgba(255,255,255,.14);border-radius:999px;color:#dffaff;background:rgba(255,255,255,.06);font-size:22px;line-height:1;cursor:pointer}.rbac-modal-body{display:grid;gap:16px;padding:22px 24px}.rbac-modal-footer{display:flex;align-items:center;justify-content:flex-end;gap:12px;padding:16px 24px;border-top:1px solid rgba(255,255,255,.1)}.rbac-option-panel{align-content:center}.rbac-transfer{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);align-items:stretch;gap:14px}.rbac-transfer-col{min-width:0;min-height:260px;display:grid;align-content:start;gap:8px;border:1px solid rgba(255,255,255,.1);border-radius:16px;padding:12px;background:rgba(255,255,255,.04)}.rbac-transfer-head{display:flex;align-items:center;justify-content:space-between;gap:10px;margin-bottom:4px}.rbac-transfer-head strong{color:#fff}.rbac-list-item{min-width:0;display:flex;align-items:center;justify-content:space-between;gap:12px;border:1px solid rgba(255,255,255,.1);border-radius:12px;padding:10px 12px;color:#eaf6ff;background:rgba(255,255,255,.05);text-align:left;cursor:pointer}.rbac-list-item span,.rbac-list-item code{min-width:0;overflow-wrap:anywhere}.rbac-list-item:hover{border-color:rgba(85,223,255,.45);background:rgba(85,223,255,.1)}.rbac-list-item.selected{border-color:rgba(103,242,138,.28);background:rgba(103,242,138,.08)}.rbac-modal-tabs{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:8px}.rbac-modal-tabs button{border:1px solid rgba(255,255,255,.12);border-radius:14px;padding:12px;color:#dce5fb;background:rgba(255,255,255,.055);font-weight:800}.rbac-modal-tabs .tab-step{display:inline-grid;place-items:center;width:20px;height:20px;margin-right:6px;border-radius:999px;background:rgba(85,223,255,.12);color:inherit;font-size:12px}.rbac-modal-tabs button.active{border-color:rgba(85,223,255,.78);color:#fff;background:rgba(85,223,255,.12)}.rbac-modal-section{display:grid;gap:10px}.console-shell[data-theme="ops-console"] .page-icon-rbac{background:linear-gradient(135deg,#0f8f89,#4f86d9)}.console-shell[data-theme="ops-console"] .checkbox-panel{border-color:#d9e4ec;background:#f8fbfd}.console-shell[data-theme="ops-console"] .check-row input{accent-color:var(--ops-primary)}.console-shell[data-theme="ops-console"] .rbac-modal-backdrop{background:rgba(17,31,43,.18);backdrop-filter:blur(6px)}.console-shell[data-theme="ops-console"] .rbac-modal{border-color:#d9e4ec;background:#fff;box-shadow:0 30px 80px rgba(29,49,70,.22)}.console-shell[data-theme="ops-console"] .rbac-modal-head,.console-shell[data-theme="ops-console"] .rbac-modal-footer{border-color:#e5ebf1}.console-shell[data-theme="ops-console"] .rbac-modal-head h3,.console-shell[data-theme="ops-console"] .rbac-transfer-head strong{color:#1f2d3d}.console-shell[data-theme="ops-console"] .rbac-modal-close{border-color:#cfd9e3;color:#08776f;background:#eefbf9}.console-shell[data-theme="ops-console"] .rbac-transfer-col{border-color:#d9e4ec;background:#f8fbfd}.console-shell[data-theme="ops-console"] .rbac-list-item{border-color:#d9e4ec;color:#223246;background:#fff}.console-shell[data-theme="ops-console"] .rbac-list-item:hover{border-color:#9ddbd5;background:#eefbf9}.console-shell[data-theme="ops-console"] .rbac-list-item.selected{border-color:#aee1c9;background:#e6f8ef}.console-shell[data-theme="ops-console"] .rbac-modal-tabs button{border-color:#d6e1e9;color:#243447;background:#fff}.console-shell[data-theme="ops-console"] .rbac-modal-tabs button.active{border-color:var(--ops-primary);color:#0d4d4b;background:#e8fbf8}@media (max-width:780px){.rbac-modal-backdrop{padding:20px 10px}.rbac-modal,.rbac-role-modal{width:calc(100vw - 20px)}.rbac-transfer,.rbac-modal-tabs{grid-template-columns:1fr}.rbac-card-actions{align-items:flex-end;flex-direction:column}}
.rbac-drawer{z-index:60;width:min(680px,calc(100vw - 44px));padding:0!important;display:flex;flex-direction:column;overflow:hidden}.rbac-role-drawer{width:min(760px,calc(100vw - 44px))}.rbac-drawer .rbac-modal,.console-shell[data-theme="ops-console"] .rbac-drawer .rbac-modal{width:auto;min-height:0;display:flex;flex-direction:column;overflow:hidden;border:0;border-radius:0;background:transparent;box-shadow:none}.rbac-drawer .rbac-modal-head{padding:18px 22px;border-bottom:1px solid rgba(255,255,255,.1)}.rbac-drawer .rbac-modal-body{min-height:0;overflow:auto;padding:20px 22px}.rbac-drawer .rbac-modal-footer{position:sticky;bottom:0;z-index:2;justify-content:center;padding:16px 22px;border-top:1px solid rgba(255,255,255,.1);background:rgba(12,18,34,.96)}.console-shell[data-theme="ops-console"] .rbac-drawer .rbac-modal-head,.console-shell[data-theme="ops-console"] .rbac-drawer .rbac-modal-footer{border-color:#e5ebf1}.console-shell[data-theme="ops-console"] .rbac-drawer .rbac-modal-footer{background:#fff}@media (max-width:720px){.rbac-drawer,.rbac-role-drawer{top:0;right:0;bottom:0;width:100vw;max-height:none;border-radius:0}.rbac-transfer{grid-template-columns:1fr}}
.rbac-transfer-col{height:320px;min-height:320px;overflow:auto}.rbac-drawer{bottom:auto;max-height:calc(100vh - 132px)}.rbac-drawer .rbac-modal,.console-shell[data-theme="ops-console"] .rbac-drawer .rbac-modal{height:auto}.rbac-drawer .rbac-modal-head{flex:0 0 auto}.rbac-drawer .rbac-modal-body{flex:0 1 auto;align-content:start;gap:14px}.rbac-role-drawer .rbac-modal-section,.rbac-role-drawer [data-testid="role-inheritance"],.rbac-role-drawer [data-testid="role-permissions"]{min-height:190px}.rbac-role-drawer [data-testid="role-index-list"]{min-height:160px}.rbac-drawer .rbac-modal-footer{flex:0 0 auto;position:relative;bottom:auto;justify-content:center;border-top:0;padding:8px 22px 20px;background:transparent}.console-shell[data-theme="ops-console"] .rbac-drawer .rbac-modal-footer{background:transparent}@media (max-width:720px){.rbac-transfer-col{height:auto;min-height:220px}.rbac-drawer,.rbac-role-drawer{bottom:0;max-height:none}.rbac-role-drawer .rbac-modal-section,.rbac-role-drawer [data-testid="role-inheritance"],.rbac-role-drawer [data-testid="role-permissions"],.rbac-role-drawer [data-testid="role-index-list"]{min-height:0}}
.rbac-role-drawer .rbac-modal-body{grid-template-rows:auto auto auto 292px auto auto;align-content:start}.rbac-role-tab-frame{height:292px;min-height:292px;display:grid;align-items:start;overflow:hidden}.rbac-role-tab-panel{grid-area:1/1;width:100%;height:100%;min-height:100%!important;overflow:auto;align-content:start}.rbac-role-tab-panel .props-editor{min-height:160px}.rbac-drawer .rbac-modal-footer{margin-top:2px;padding:0;background:transparent!important;border-top:0!important;box-shadow:none}.rbac-drawer .rbac-modal-body>.rbac-modal-footer{display:flex;align-items:center;justify-content:flex-start;gap:10px}.align-left-table th,.align-left-table td,.result-table th,.result-table td,.collect-table th,.collect-table td{text-align:left!important}.align-left-table th{vertical-align:middle}@media (max-width:720px){.rbac-role-drawer .rbac-modal-body{grid-template-rows:auto auto auto auto auto auto}.rbac-role-tab-frame{height:auto;min-height:0}.rbac-role-tab-panel{height:auto;min-height:0!important}}
</style>
