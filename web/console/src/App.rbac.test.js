import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import App from "./AppMvp.vue";

function jsonResponse(payload, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: status === 200 ? "OK" : "Error",
    text: async () => JSON.stringify(payload)
  };
}

function authStatus(authenticated = true) {
  return {
    enabled: true,
    login_required: true,
    authenticated,
    auth_type: "password_token",
    rbac_enabled: true,
    token_type: "Bearer",
    token_header: "Authorization",
    public_paths: ["/", "/healthz", "/readyz", "/api/v1/auth", "/api/v1/login"]
  };
}

function mePayload(overrides = {}) {
  return {
    user: { id: "u-admin", username: "admin", display_name: "Administrator", ...(overrides.user || {}) },
    roles: overrides.roles || [{ role_code: "platform_admin", role_name: "平台管理员" }],
    permissions: overrides.permissions || [
      "datasource:read", "datasource:create", "datasource:update", "datasource:delete", "datasource:start", "datasource:stop",
      "parse_rule:read", "parse_rule:create", "parse_rule:update", "parse_rule:delete", "parse_rule:test",
      "index:read", "index:manage", "index:create", "index:update", "index:delete", "index:trend",
      "search:execute", "search:fields", "search:timeline", "search:saved_search",
      "rbac:manage",
      "user:read", "user:create", "user:update", "user:delete", "user:reset_password",
      "role:read", "role:create", "role:update", "role:delete",
      "token:read", "token:create", "token:revoke",
      "audit:read"
    ],
    scopes: overrides.scopes || {
      plugins: {
        use: [
          { plugin_type: "input", plugin_code: "*" },
          { plugin_type: "parser", plugin_code: "*" },
          { plugin_type: "search_command", plugin_code: "*" }
        ],
        manage: [
          { plugin_type: "input", plugin_code: "*" },
          { plugin_type: "parser", plugin_code: "*" },
          { plugin_type: "search_command", plugin_code: "*" }
        ]
      }
    },
    token: { id: "tok-admin", name: "default", source: "env_seed" }
  };
}

function baseFetchWithMe(me = mePayload(), extra = {}) {
  return vi.fn(async (url) => {
    const path = String(url);
    if (path === "/api/v1/auth") return jsonResponse(authStatus(true));
    if (path === "/api/v1/me") return jsonResponse(me);
    if (path.startsWith("/api/v1/datasources")) return jsonResponse(extra.datasources || { datasources: [], pagination: { page: 1, page_size: 10, total: 0, total_pages: 1 } });
    if (path.startsWith("/api/v1/indexes")) return jsonResponse(extra.indexes || { indexes: [], pagination: { page: 1, page_size: 10, total: 0, total_pages: 1 } });
    if (path === "/api/v1/parser-plugins") return jsonResponse(extra.parserPlugins || { plugins: [] });
    if (path.startsWith("/api/v1/parse-rules")) return jsonResponse(extra.parseRules || { parse_rules: [], pagination: { page: 1, page_size: 10, total: 0, total_pages: 1 } });
    if (path.startsWith("/api/v1/search/favorites")) return jsonResponse(extra.savedSearches || { saved_searches: [] });
    if (path.startsWith("/api/v1/plugins?")) return jsonResponse(extra.plugins || { plugins: [], pagination: { page: 1, page_size: 10, total: 0, total_pages: 1 }, type_counts: { input: 0, parser: 0, search_command: 0 } });
    if (path.startsWith("/api/v1/plugins/catalog")) return jsonResponse(extra.pluginCatalog || { plugins: [] });
    if (path === "/api/v1/writer/runtime") return jsonResponse(extra.writerRuntime || { status: "ok" });
    if (path.startsWith("/api/v1/users")) return jsonResponse(extra.users || { users: [], pagination: { page: 1, page_size: 20, total: 0, total_pages: 1 } });
    if (path.startsWith("/api/v1/roles")) return jsonResponse(extra.rolesPayload || { roles: [] });
    if (path.startsWith("/api/v1/permissions")) return jsonResponse(extra.permissionsPayload || { permissions: [] });
    if (path.startsWith("/api/v1/tokens")) return jsonResponse(extra.tokens || { tokens: [] });
    if (path.startsWith("/api/v1/audit-logs")) return jsonResponse(extra.auditLogs || { audit_logs: [], pagination: { page: 1, page_size: 20, total: 0, total_pages: 1 } });
    return jsonResponse({});
  });
}

beforeEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("XDP RBAC console behavior", () => {
  it("loads current user roles and module menu permissions after login", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = baseFetchWithMe(mePayload({
      user: { username: "platform-admin", display_name: "平台管理员" }
    }));

    const wrapper = mount(App);
    await flushPromises();

    expect(global.fetch).toHaveBeenCalledWith("/api/v1/me", expect.any(Object));
    expect(wrapper.get(".user").text()).toContain("平台管理员");
    expect(wrapper.get('[data-testid="main-nav"]').text()).toContain("采集配置");
    expect(wrapper.get('[data-testid="main-nav"]').text()).toContain("解析配置");
    expect(wrapper.get('[data-testid="main-nav"]').text()).toContain("索引配置");
    expect(wrapper.get('[data-testid="main-nav"]').text()).toContain("搜索页");
    expect(wrapper.get('[data-testid="main-nav"]').text()).toContain("插件管理");
  });

  it("shows only modules allowed by main permissions and plugin manage scope", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = baseFetchWithMe(mePayload({
      permissions: ["search:execute"],
      scopes: {
        plugins: {
          use: [{ plugin_type: "search_command", plugin_code: "stats" }],
          manage: []
        }
      }
    }));

    const wrapper = mount(App);
    await flushPromises();

    const navText = wrapper.get('[data-testid="main-nav"]').text();
    expect(navText).not.toContain("采集配置");
    expect(navText).not.toContain("解析配置");
    expect(navText).not.toContain("索引配置");
    expect(navText).toContain("搜索页");
    expect(navText).not.toContain("插件管理");
    expect(wrapper.get('[data-testid="search-page"]').exists()).toBe(true);
  });

  it("filters plugin management tabs by manage scope", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    localStorage.setItem("xdp_current_module", "plugins");
    global.fetch = baseFetchWithMe(mePayload({
      permissions: [],
      scopes: {
        plugins: {
          use: [{ plugin_type: "parser", plugin_code: "regex" }],
          manage: [{ plugin_type: "parser", plugin_code: "*" }]
        }
      }
    }), {
      plugins: {
        plugins: [{ plugin_code: "json-parser", plugin_type: "parser", plugin_version: "1.0.0", name: "JSON Parser", status: "disabled", checksum: "sha256:json" }],
        pagination: { page: 1, page_size: 10, total: 1, total_pages: 1 },
        type_counts: { input: 0, parser: 1, search_command: 0 }
      }
    });

    const wrapper = mount(App);
    await flushPromises();

    expect(wrapper.get('[data-testid="main-nav"]').text()).toContain("插件管理");
    expect(wrapper.find('[data-testid="plugin-tab-input"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="plugin-tab-parser"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="plugin-tab-search_command"]').exists()).toBe(false);
    expect(wrapper.get('[data-testid="plugin-upload-button"]').exists()).toBe(true);
    expect(wrapper.get('[data-testid="plugins-page"]').text()).toContain("JSON Parser");
  });

  it("shows a 403 state when direct access lacks permission", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    localStorage.setItem("xdp_current_module", "plugins");
    global.fetch = baseFetchWithMe(mePayload({
      permissions: ["search:execute", "datasource:read", "parse_rule:read", "index:read"],
      scopes: {
        plugins: {
          use: [{ plugin_type: "search_command", plugin_code: "stats" }],
          manage: []
        }
      }
    }));

    const wrapper = mount(App);
    await flushPromises();

    expect(wrapper.get('[data-testid="module-forbidden"]').text()).toContain("403");
    expect(wrapper.get('[data-testid="module-forbidden"]').text()).toContain("权限不足");
  });

  it("shows user and role management when rbac manage permission is granted", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    localStorage.setItem("xdp_current_module", "rbac");
    global.fetch = baseFetchWithMe(mePayload({
      permissions: ["rbac:manage"],
      scopes: { plugins: { use: [], manage: [] } }
    }), {
      users: {
        users: [
          { id: "u-admin", username: "admin", display_name: "Administrator", status: "active", last_login_at: "2026-07-19T08:00:00Z", roles: [{ id: "role-admin", role_code: "platform_admin", role_name: "平台管理员" }] },
          { id: "u-analyst", username: "analyst01", display_name: "安全分析师", status: "active", last_login_at: "2026-07-18T11:02:03Z", roles: [{ id: "role-analyst", role_code: "analyst", role_name: "分析师" }] }
        ],
        pagination: { page: 1, page_size: 20, total: 2, total_pages: 1 }
      },
      rolesPayload: {
        roles: [
          { id: "role-admin", role_code: "platform_admin", role_name: "平台管理员", status: "active", builtin: true, permission_codes: ["rbac:manage", "search:execute", "index:read", "index:manage"], index_scopes: { read: ["*"], search: ["*"], manage: ["*"] }, plugin_scopes: { use: [{ plugin_type: "input", plugin_code: "*" }], manage: [{ plugin_type: "input", plugin_code: "*" }] } },
          { id: "role-analyst", role_code: "analyst", role_name: "分析师", status: "active", permission_codes: ["search:execute", "index:read"], index_scopes: { search: ["audit_*"] }, plugin_scopes: { use: [{ plugin_type: "search_command", plugin_code: "stats" }] } }
        ]
      },
      indexes: {
        indexes: [
          { index_name: "audit_prod", table_name: "events_audit_prod", ttl_days: 30, status: "active" },
          { index_name: "json_prod", table_name: "events_json_prod", ttl_days: 30, status: "active" }
        ],
        pagination: { page: 1, page_size: 10, total: 2, total_pages: 1 }
      },
      permissionsPayload: {
        permissions: [
          { permission_code: "search:execute", display_name: "执行搜索" },
          { permission_code: "datasource:read", display_name: "采集配置入口" },
          { permission_code: "index:read", display_name: "索引配置入口" },
          { permission_code: "index:manage", display_name: "索引管理能力" },
          { permission_code: "user:delete", display_name: "删除用户" }
        ]
      }
    });

    const wrapper = mount(App);
    await flushPromises();

    expect(wrapper.get('[data-testid="main-nav"]').text()).toContain("用户与权限");
    expect(wrapper.get('[data-testid="rbac-tab-users"]').classes()).toContain("active");
    expect(wrapper.find('[data-testid="rbac-users-table"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="rbac-roles-table"]').exists()).toBe(false);
    expect(wrapper.get('[data-testid="rbac-users-table"]').text()).toContain("最近登录时间");
    expect(wrapper.get('[data-testid="rbac-users-table"]').text()).toContain("2026-07-18");
    expect(wrapper.find('[data-testid="delete-user-u-admin"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="admin-delete-protected-u-admin"]').exists()).toBe(false);
    expect(wrapper.get('[data-testid="rbac-users-table"]').text()).not.toContain("admin 不可删除");
    expect(wrapper.find('[data-testid="delete-user-u-analyst"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="create-user"]').exists()).toBe(false);
    await wrapper.get('[data-testid="show-user-modal"]').trigger("click");
    await flushPromises();
    expect(wrapper.get('[data-testid="user-modal"]').text()).toContain("新建用户");
    expect(wrapper.get('[data-testid="user-modal"]').classes()).toContain("config-drawer");
    expect(wrapper.find('[data-testid="user-username"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="user-display-name"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="user-password"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="user-confirm-password"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="user-force-password-change"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="user-create-role"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="user-role-transfer"]').exists()).toBe(true);
    expect(wrapper.findAll('[data-testid="user-role-transfer"] .rbac-transfer-col')).toHaveLength(2);
    expect(wrapper.find('[data-testid="user-modal"] .rbac-modal-footer').exists()).toBe(true);
    expect(wrapper.find('[data-testid="user-modal"] .rbac-modal-body > .rbac-modal-footer').exists()).toBe(true);
    document.body.dispatchEvent(new MouseEvent("pointerdown", { bubbles: true }));
    await flushPromises();
    expect(wrapper.find('[data-testid="user-modal"]').exists()).toBe(false);
    await wrapper.get('[data-testid="show-user-modal"]').trigger("click");
    await flushPromises();
    await wrapper.get('[data-testid="cancel-user-modal"]').trigger("click");
    await flushPromises();
    expect(wrapper.find('[data-testid="user-modal"]').exists()).toBe(false);
    expect(wrapper.get('[data-testid="rbac-page"]').text()).toContain("analyst01");
    expect(wrapper.get('[data-testid="rbac-page"]').text()).toContain("分析师");

    await wrapper.get('[data-testid="rbac-tab-roles"]').trigger("click");
    await flushPromises();

    expect(wrapper.get('[data-testid="rbac-tab-roles"]').classes()).toContain("active");
    expect(wrapper.find('[data-testid="rbac-users-table"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="rbac-roles-table"]').exists()).toBe(true);
    const rolesTableText = wrapper.get('[data-testid="rbac-roles-table"]').text();
    expect(rolesTableText).toContain("索引");
    expect(rolesTableText).not.toContain("Index Scope");
    expect(rolesTableText).not.toContain("内置角色不可删除");
    expect(rolesTableText).toContain("*");
    expect(rolesTableText).not.toContain("read:*");
    expect(rolesTableText).not.toContain("manage:*");
    expect(wrapper.find('[data-testid="create-role"]').exists()).toBe(false);
    await wrapper.get('[data-testid="show-role-modal"]').trigger("click");
    await flushPromises();
    expect(wrapper.get('[data-testid="role-modal"]').text()).toContain("新建角色");
    expect(wrapper.get('[data-testid="role-modal"]').classes()).toContain("config-drawer");
    expect(wrapper.find('[data-testid="role-modal-tab-inherit"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="role-modal-tab-menu"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="role-modal-tab-plugin"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="role-modal-tab-index"]').exists()).toBe(true);
    expect(wrapper.get('[data-testid="role-modal-tab-inherit"]').text()).toContain("1");
    expect(wrapper.get('[data-testid="role-modal-tab-menu"]').text()).toContain("2");
    expect(wrapper.get('[data-testid="role-modal-tab-plugin"]').text()).toContain("3");
    expect(wrapper.get('[data-testid="role-modal-tab-index"]').text()).toContain("4");
    expect(wrapper.find('[data-testid="role-modal"] .rbac-modal-footer').exists()).toBe(true);
    expect(wrapper.find('[data-testid="role-modal"] .rbac-modal-body > .rbac-modal-footer').exists()).toBe(true);
    expect(wrapper.find('[data-testid="role-tab-frame"]').exists()).toBe(true);
    await wrapper.get('[data-testid="role-modal-tab-menu"]').trigger("click");
    await flushPromises();
    expect(wrapper.find('[data-testid="role-permissions"]').classes()).toContain("rbac-role-tab-panel");
    expect(wrapper.get('[data-testid="role-permissions"]').text()).toContain("采集配置");
    expect(wrapper.get('[data-testid="role-permissions"]').text()).toContain("索引配置");
    expect(wrapper.get('[data-testid="role-permissions"]').text()).toContain("搜索页");
    expect(wrapper.get('[data-testid="role-permissions"]').text()).not.toContain("允许访问采集配置菜单");
    expect(wrapper.get('[data-testid="role-permissions"]').text()).not.toContain("菜单权限只决定");
    expect(wrapper.get('[data-testid="role-permissions"]').text()).not.toContain("datasource:read");
    expect(wrapper.get('[data-testid="role-permissions"]').text()).not.toContain("user:delete");
    await wrapper.get('[data-testid="role-modal-tab-index"]').trigger("click");
    await flushPromises();
    expect(wrapper.find('[data-testid="role-index-permissions"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="role-index-scopes"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="role-index-list"]').classes()).toContain("checkbox-panel");
    expect(wrapper.get('[data-testid="role-index-list"]').text()).toContain("audit_prod");
    expect(wrapper.get('[data-testid="role-index-list"]').text()).toContain("json_prod");
    await wrapper.get('[data-testid="role-modal-tab-plugin"]').trigger("click");
    await flushPromises();
    expect(wrapper.get('[data-testid="role-modal"]').text()).toContain("use / manage");
    expect(wrapper.find('[data-testid="role-plugin-scopes"]').exists()).toBe(true);
    await wrapper.get('[data-testid="cancel-role-modal"]').trigger("click");
    await flushPromises();
    expect(wrapper.find('[data-testid="role-modal"]').exists()).toBe(false);
    expect(wrapper.get('[data-testid="rbac-page"]').text()).toContain("菜单：搜索页");
    expect(wrapper.get('[data-testid="rbac-page"]').text()).not.toContain("索引：索引配置入口");

    await wrapper.get('[data-testid="show-role-modal"]').trigger("click");
    await flushPromises();
    document.body.dispatchEvent(new MouseEvent("pointerdown", { bubbles: true }));
    await flushPromises();
    expect(wrapper.find('[data-testid="role-modal"]').exists()).toBe(false);
  });

  it("submits user creation and role creation with permissions and scopes", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    localStorage.setItem("xdp_current_module", "rbac");
    const requests = [];
    global.fetch = vi.fn(async (url, options = {}) => {
      const path = String(url);
      requests.push({ path, options });
      if (path === "/api/v1/auth") return jsonResponse(authStatus(true));
      if (path === "/api/v1/me") return jsonResponse(mePayload({
        permissions: ["rbac:manage"],
        scopes: { plugins: { use: [], manage: [] } }
      }));
      if (path.startsWith("/api/v1/users") && options.method === "POST") {
        return jsonResponse({ id: "u-new", username: "newuser", display_name: "新用户", status: "active", roles: [] }, 201);
      }
      if (path.startsWith("/api/v1/roles") && options.method === "POST") {
        return jsonResponse({ id: "role-new", role_code: "search_limited", role_name: "受限搜索", status: "active", permission_codes: ["search:execute"] }, 201);
      }
      if (path.startsWith("/api/v1/users")) return jsonResponse({ users: [], pagination: { page: 1, page_size: 20, total: 0, total_pages: 1 } });
      if (path.startsWith("/api/v1/roles")) return jsonResponse({ roles: [{ id: "role-analyst", role_code: "analyst", role_name: "分析师", status: "active" }] });
      if (path.startsWith("/api/v1/permissions")) return jsonResponse({ permissions: [{ permission_code: "search:execute", display_name: "执行搜索" }] });
      if (path.startsWith("/api/v1/indexes")) return jsonResponse({
        indexes: [{ index_name: "audit_prod", table_name: "events_audit_prod", ttl_days: 30, status: "active" }],
        pagination: { page: 1, page_size: 10, total: 1, total_pages: 1 }
      });
      return jsonResponse({});
    });

    const wrapper = mount(App);
    await flushPromises();

    await wrapper.get('[data-testid="show-user-modal"]').trigger("click");
    await flushPromises();
    await wrapper.get('[data-testid="user-username"]').setValue("newuser");
    await wrapper.get('[data-testid="user-display-name"]').setValue("新用户");
    await wrapper.get('[data-testid="user-password"]').setValue("ChangeMe_123");
    await wrapper.get('[data-testid="user-confirm-password"]').setValue("ChangeMe_123");
    await wrapper.get('[data-testid="user-role-option-role-analyst"]').trigger("click");
    await wrapper.get('[data-testid="create-user"]').trigger("submit");
    await flushPromises();

    await wrapper.get('[data-testid="rbac-tab-roles"]').trigger("click");
    await flushPromises();

    await wrapper.get('[data-testid="show-role-modal"]').trigger("click");
    await flushPromises();
    await wrapper.get('[data-testid="role-code"]').setValue("search_limited");
    await wrapper.get('[data-testid="role-name"]').setValue("受限搜索");
    await wrapper.get('[data-testid="role-modal-tab-menu"]').trigger("click");
    await wrapper.get('[data-testid="role-permission-search-execute"]').setValue(true);
    await wrapper.get('[data-testid="role-modal-tab-index"]').trigger("click");
    await wrapper.get('[data-testid="role-index-item-audit_prod"]').setValue(true);
    await wrapper.get('[data-testid="role-modal-tab-plugin"]').trigger("click");
    await wrapper.get('[data-testid="role-plugin-scopes"]').setValue("use:search_command/stats");
    await wrapper.get('[data-testid="create-role"]').trigger("submit");
    await flushPromises();

    const createUser = requests.find((item) => item.path === "/api/v1/users" && item.options.method === "POST");
    expect(JSON.parse(createUser.options.body)).toMatchObject({
      username: "newuser",
      display_name: "新用户",
      password: "ChangeMe_123",
      role_ids: ["role-analyst"]
    });
    const createRole = requests.find((item) => item.path === "/api/v1/roles" && item.options.method === "POST");
    expect(JSON.parse(createRole.options.body)).toMatchObject({
      role_code: "search_limited",
      role_name: "受限搜索",
      permission_codes: ["search:execute"],
      index_scopes: { search: ["audit_prod"] },
      plugin_scopes: { use: [{ plugin_type: "search_command", plugin_code: "stats" }] }
    });
  });

  it("updates and deletes users and roles from the RBAC page", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    localStorage.setItem("xdp_current_module", "rbac");
    vi.spyOn(window, "prompt").mockReturnValue("NewPassword_123");
    const requests = [];
    global.fetch = vi.fn(async (url, options = {}) => {
      const path = String(url);
      requests.push({ path, options });
      if (path === "/api/v1/auth") return jsonResponse(authStatus(true));
      if (path === "/api/v1/me") return jsonResponse(mePayload({
        permissions: ["rbac:manage"],
        scopes: { plugins: { use: [], manage: [] } }
      }));
      if (path === "/api/v1/users/u-analyst" && options.method === "PUT") return jsonResponse({ id: "u-analyst", username: "analyst01", display_name: "安全分析师", status: "disabled", roles: [] });
      if (path === "/api/v1/users/u-analyst/roles" && options.method === "PUT") return jsonResponse({ id: "u-analyst", username: "analyst01", display_name: "安全分析师", status: "disabled", roles: [] });
      if (path === "/api/v1/users/u-analyst/password" && options.method === "PUT") return jsonResponse({ updated: true });
      if (path === "/api/v1/users/u-analyst" && options.method === "DELETE") return jsonResponse({ deleted: true });
      if (path === "/api/v1/roles/role-analyst" && options.method === "PUT") return jsonResponse({ id: "role-analyst", role_code: "analyst", role_name: "安全分析师角色", status: "active", permission_codes: ["search:execute"] });
      if (path === "/api/v1/roles/role-analyst" && options.method === "DELETE") return jsonResponse({ deleted: true });
      if (path.startsWith("/api/v1/users")) return jsonResponse({
        users: [{ id: "u-analyst", username: "analyst01", display_name: "安全分析师", status: "active", roles: [{ id: "role-analyst", role_code: "analyst", role_name: "分析师" }] }],
        pagination: { page: 1, page_size: 20, total: 1, total_pages: 1 }
      });
      if (path.startsWith("/api/v1/roles")) return jsonResponse({ roles: [{ id: "role-analyst", role_code: "analyst", role_name: "分析师", status: "active", permission_codes: ["search:execute"] }] });
      if (path.startsWith("/api/v1/permissions")) return jsonResponse({ permissions: [{ permission_code: "search:execute", display_name: "执行搜索" }] });
      return jsonResponse({});
    });

    const wrapper = mount(App);
    await flushPromises();

    await wrapper.get('[data-testid="toggle-user-u-analyst"]').trigger("click");
    await wrapper.get('[data-testid="reset-password-u-analyst"]').trigger("click");
    await wrapper.get('[data-testid="delete-user-u-analyst"]').trigger("click");

    await wrapper.get('[data-testid="rbac-tab-roles"]').trigger("click");
    await flushPromises();

    await wrapper.get('[data-testid="edit-role-role-analyst"]').trigger("click");
    await flushPromises();
    await wrapper.get('[data-testid="role-name"]').setValue("安全分析师角色");
    await wrapper.get('[data-testid="create-role"]').trigger("submit");
    await wrapper.get('[data-testid="delete-role-role-analyst"]').trigger("click");
    await flushPromises();

    expect(requests).toEqual(expect.arrayContaining([
      expect.objectContaining({ path: "/api/v1/users/u-analyst", options: expect.objectContaining({ method: "PUT" }) }),
      expect.objectContaining({ path: "/api/v1/users/u-analyst/password", options: expect.objectContaining({ method: "PUT" }) }),
      expect.objectContaining({ path: "/api/v1/users/u-analyst", options: expect.objectContaining({ method: "DELETE" }) }),
      expect.objectContaining({ path: "/api/v1/roles/role-analyst", options: expect.objectContaining({ method: "PUT" }) }),
      expect.objectContaining({ path: "/api/v1/roles/role-analyst", options: expect.objectContaining({ method: "DELETE" }) })
    ]));
  });
});
