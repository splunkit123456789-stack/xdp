import { reactive } from "vue";
import { requestJSON as defaultRequestJSON, tokenKey } from "./http-client.js";
import { defaultModuleKey, moduleRouteNames } from "./router.js";

export const authContextKey = Symbol("xdp-auth-context");

export const modulePermissionMap = {
  collect: "datasource:read",
  parse: "parse_rule:read",
  index: "index:read",
  search: "search:execute",
  rbac: "rbac:manage"
};

export function allFrontendPermissions() {
  return [
    "datasource:read", "datasource:create", "datasource:update", "datasource:delete", "datasource:start", "datasource:stop",
    "parse_rule:read", "parse_rule:create", "parse_rule:update", "parse_rule:delete", "parse_rule:test",
    "index:read", "index:manage", "index:create", "index:update", "index:delete", "index:trend",
    "search:execute", "search:fields", "search:timeline", "search:saved_search",
    "rbac:manage",
    "user:read", "user:create", "user:update", "user:delete", "user:reset_password",
    "role:read", "role:create", "role:update", "role:delete",
    "token:read", "token:create", "token:revoke",
    "audit:read"
  ];
}

export function allPluginScopeBindings() {
  return [
    { plugin_type: "input", plugin_code: "*" },
    { plugin_type: "parser", plugin_code: "*" },
    { plugin_type: "search_command", plugin_code: "*" }
  ];
}

export function defaultCurrentUser() {
  return {
    loaded: false,
    rbacEnabled: false,
    username: "Administrator",
    displayName: "Administrator",
    permissions: allFrontendPermissions(),
    scopes: {
      plugins: {
        use: allPluginScopeBindings(),
        manage: allPluginScopeBindings()
      }
    }
  };
}

export function createAuthContext({ requestJSON = defaultRequestJSON, storage = localStorage } = {}) {
  const state = reactive({
    ready: false,
    loading: false,
    auth: {
      enabled: true,
      authenticated: false,
      loginRequired: true,
      rbacEnabled: false
    },
    currentUser: defaultCurrentUser()
  });

  function permissionSet() {
    return new Set(state.currentUser.permissions || []);
  }

  function assignCurrentUser(user) {
    Object.keys(state.currentUser).forEach((key) => delete state.currentUser[key]);
    Object.assign(state.currentUser, user);
  }

  function assignAuth(payload = {}) {
    const enabled = payload.enabled !== false;
    const authenticated = !enabled || Boolean(payload.authenticated);
    const loginRequired = enabled && payload.login_required !== false && payload.loginRequired !== false;
    const rbacEnabled = Boolean(payload.rbac_enabled || payload.rbacEnabled);
    Object.assign(state.auth, {
      enabled,
      authenticated,
      loginRequired,
      rbacEnabled,
      login_required: loginRequired,
      rbac_enabled: rbacEnabled
    });
  }

  async function ensureAuthReady({ force = false } = {}) {
    if (state.ready && !force) return state.auth;
    state.loading = true;
    try {
      const payload = await requestJSON("/api/v1/auth", { auth: true });
      assignAuth(payload);
      if (state.auth.authenticated) {
        await reloadCurrentUser();
      } else {
        assignCurrentUser(defaultCurrentUser());
      }
      state.ready = true;
      return state.auth;
    } finally {
      state.loading = false;
    }
  }

  async function reloadCurrentUser() {
    if (!state.auth.rbacEnabled) {
      const user = defaultCurrentUser();
      user.loaded = true;
      user.rbacEnabled = false;
      assignCurrentUser(user);
      return state.currentUser;
    }
    try {
      const payload = await requestJSON("/api/v1/me", { auth: true });
      assignCurrentUser({
        loaded: true,
        rbacEnabled: true,
        username: payload.user?.username || "unknown",
        displayName: payload.user?.display_name || payload.user?.username || "unknown",
        permissions: Array.isArray(payload.permissions) ? payload.permissions : [],
        scopes: payload.scopes || { plugins: {} }
      });
    } catch {
      assignCurrentUser({
        loaded: true,
        rbacEnabled: true,
        username: "unknown",
        displayName: "unknown",
        permissions: [],
        scopes: { plugins: {} }
      });
    }
    return state.currentUser;
  }

  async function setLoginToken(token) {
    storage.setItem(tokenKey, token);
    state.ready = false;
    state.auth.authenticated = true;
    await ensureAuthReady({ force: true });
  }

  function clearAuth() {
    storage.removeItem(tokenKey);
    storage.removeItem("xdp_current_module");
    state.ready = false;
    Object.assign(state.auth, {
      enabled: true,
      authenticated: false,
      loginRequired: true,
      rbacEnabled: false,
      login_required: true,
      rbac_enabled: false
    });
    assignCurrentUser(defaultCurrentUser());
  }

  function hasPermission(permission) {
    return permissionSet().has(permission);
  }

  function pluginScopeItems(action) {
    const scopes = state.currentUser.scopes?.plugins || state.currentUser.plugin_scopes || {};
    return Array.isArray(scopes[action]) ? scopes[action] : [];
  }

  function hasPluginScope(action, pluginType, pluginCode = "") {
    const normalizedType = normalizePluginType(pluginType);
    const normalizedCode = String(pluginCode || "").trim().toLowerCase();
    return pluginScopeItems(action).some((item) => {
      const itemType = normalizePluginType(item.plugin_type || item.pluginType);
      const itemCode = String(item.plugin_code || item.pluginCode || "").trim().toLowerCase();
      return itemType === normalizedType && (itemCode === "*" || !normalizedCode || itemCode === normalizedCode);
    });
  }

  function canUsePlugin(pluginType, pluginCode = "") {
    if (isBuiltInPlugin(pluginType, pluginCode)) return true;
    return hasPluginScope("use", pluginType, pluginCode) || hasPluginScope("manage", pluginType, pluginCode);
  }

  function canManagePluginType(pluginType) {
    return hasPluginScope("manage", pluginType, "");
  }

  function hasAnyManagePluginScope() {
    return pluginScopeItems("manage").length > 0;
  }

  function canAccessModule(moduleKey) {
    if (!state.currentUser.rbacEnabled) return moduleKey !== "rbac";
    if (moduleKey === "plugins") return hasAnyManagePluginScope();
    const permission = modulePermissionMap[moduleKey];
    return Boolean(permission && hasPermission(permission));
  }

  function firstAccessibleModule() {
    return moduleRouteNames.find((moduleKey) => canAccessModule(moduleKey)) || defaultModuleKey;
  }

  function resolveRouteAccess(routeName) {
    const moduleKey = String(routeName || "");
    if (!moduleRouteNames.includes(moduleKey) || canAccessModule(moduleKey)) {
      return { allowed: true, redirectName: "", forbidden: "" };
    }
    return { allowed: false, redirectName: firstAccessibleModule(), forbidden: moduleKey };
  }

  return {
    state,
    ensureAuthReady,
    reloadCurrentUser,
    setLoginToken,
    clearAuth,
    hasPermission,
    hasPluginScope,
    canUsePlugin,
    canManagePluginType,
    hasAnyManagePluginScope,
    canAccessModule,
    firstAccessibleModule,
    resolveRouteAccess
  };
}

function normalizePluginType(pluginType) {
  const value = String(pluginType || "").trim().toLowerCase();
  if (value === "search-command") return "search_command";
  return value;
}

function isBuiltInPlugin(pluginType, pluginCode) {
  const type = normalizePluginType(pluginType);
  const code = String(pluginCode || "").trim().toLowerCase();
  return (type === "input" && code === "syslog")
    || (type === "parser" && code === "regex")
    || (type === "search_command" && code === "stats");
}
