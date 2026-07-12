import { flushPromises, mount } from "@vue/test-utils";
import { readFileSync } from "node:fs";
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
    token_type: "Bearer",
    token_header: "Authorization",
    public_paths: ["/", "/healthz", "/readyz", "/api/v1/auth", "/api/v1/login"]
  };
}

beforeEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("XDP Console MVP pages", () => {
  async function mountAuthenticatedApp() {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus(true)))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse({
        indexes: [
          { index_name: "app", ttl_days: 30, rows: 179497, status: "active" },
          { index_name: "firewall", ttl_days: 30, rows: 42013, status: "active" },
          { index_name: "audit", ttl_days: 7, rows: 0, status: "active" }
        ]
      }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({
        saved_searches: [
          { id: "s-1", name: "App stats", spl: "index=app | stats count as total by service", time_range_type: "近 1 天" },
          { id: "s-2", name: "Firewall deny", spl: "index=firewall action=deny", time_range_type: "近 7 天" }
        ]
      }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }));

    const wrapper = mount(App);
    await flushPromises();
    return wrapper;
  }

  it("renders the trusted console shell and collection plugin cards", async () => {
    const wrapper = await mountAuthenticatedApp();

    expect(wrapper.get('[data-testid="console-shell"]').text()).toContain("XDP>Console");
    expect(wrapper.get('[data-testid="collect-page"]').text()).toContain("Syslog / 导入插件");
    expect(wrapper.get('[data-testid="collect-page"]').text()).toContain("监听端口");
    expect(wrapper.find('[data-testid="input-form-card"]').exists()).toBe(false);

    await wrapper.get('[data-testid="show-input-form"]').trigger("click");

    expect(wrapper.get('[data-testid="collect-page"]').text()).toContain("Syslog");
    expect(wrapper.find('[data-testid="input-plugin-kafka"]').exists()).toBe(false);
  });

  it("uses an operations console theme after login instead of the login gradient theme", async () => {
    const wrapper = await mountAuthenticatedApp();

    const shell = wrapper.get('[data-testid="console-shell"]');
    expect(shell.attributes("data-theme")).toBe("ops-console");
    expect(wrapper.get(".console-shell .brand-mark").classes()).toContain("console-brand-mark");
    await wrapper.get('[data-testid="show-input-form"]').trigger("click");
    expect(wrapper.get('[data-testid="input-plugin-syslog"] .plugin-icon').classes()).toContain("icon-syslog");
    expect(wrapper.find('[data-testid="input-plugin-kafka"]').exists()).toBe(false);
  });

  it("pins collect, parse, and index add actions to the panel header right side", async () => {
    const wrapper = await mountAuthenticatedApp();

    expect(wrapper.get('[data-testid="show-input-form"]').element.closest(".panel-header-actions")).not.toBeNull();

    await wrapper.get('[data-testid="nav-parse"]').trigger("click");
    expect(wrapper.get('[data-testid="show-rule-form"]').element.closest(".panel-header-actions")).not.toBeNull();

    await wrapper.get('[data-testid="nav-index"]').trigger("click");
    expect(wrapper.get('[data-testid="show-index-form"]').element.closest(".panel-header-actions")).not.toBeNull();
  });

  it("opens collect, parse, and index forms in right side drawers", async () => {
    const wrapper = await mountAuthenticatedApp();

    await wrapper.get('[data-testid="show-input-form"]').trigger("click");
    expect(wrapper.get('[data-testid="input-form-card"]').classes()).toContain("config-drawer");
    expect(wrapper.get('[data-testid="input-form-card"]').attributes("aria-label")).toBe("采集配置表单");

    await wrapper.get('[data-testid="nav-parse"]').trigger("click");
    await wrapper.get('[data-testid="show-rule-form"]').trigger("click");
    expect(wrapper.get('[data-testid="rule-form-card"]').classes()).toContain("config-drawer");
    expect(wrapper.get('[data-testid="rule-form-card"]').attributes("aria-label")).toBe("解析配置表单");

    await wrapper.get('[data-testid="nav-index"]').trigger("click");
    await wrapper.get('[data-testid="show-index-form"]').trigger("click");
    expect(wrapper.get('[data-testid="index-form-card"]').classes()).toContain("config-drawer");
    expect(wrapper.get('[data-testid="index-form-card"]').attributes("aria-label")).toBe("索引配置表单");
  });

  it("uses the operations console visual theme on the login page without changing login content", async () => {
    global.fetch = vi.fn().mockResolvedValueOnce(jsonResponse(authStatus(false)));

    const wrapper = mount(App);
    await flushPromises();

    const loginPage = wrapper.get('[data-testid="login-page"]');
    expect(loginPage.attributes("data-theme")).toBe("ops-login");
    expect(loginPage.text()).toContain("XDP>Console");
    expect(loginPage.text()).toContain("AUTH GATEWAY");
    expect(loginPage.text()).toContain("MVP ACCESS");
    expect(loginPage.text()).toContain("SECURE DATA PLATFORM");
    expect(loginPage.text()).toContain("可信数据入口");
    expect(loginPage.text()).toContain("采集、解析、索引与搜索统一入口，登录后进入 XDP 控制台。");
    expect(loginPage.text()).toContain("Syslog Ingest");
    expect(loginPage.text()).toContain("props.conf Parser");
    expect(loginPage.text()).toContain("SPL Search");
    expect(loginPage.text()).toContain("SIGN IN");
    expect(loginPage.text()).toContain("登录控制台");
    expect(wrapper.get('input[name="username"]').attributes("placeholder")).toBe("请输入用户名");
    expect(wrapper.get('input[name="password"]').attributes("placeholder")).toBe("请输入密码");
    expect(wrapper.get(".login-form button").text()).toBe("登录");
    expect(loginPage.text()).toContain("© 2026 XDP Console");

    const source = readFileSync("src/AppMvp.vue", "utf8");
    expect(source).toContain('.login-shell[data-theme="ops-login"]');
    expect(source).toContain("linear-gradient(135deg,#eef4f7 0%,#f8fbfc 46%,#edf5f3 100%)");
    expect(source).toContain(
      '.login-shell[data-theme="ops-login"] .login-form input{border-color:#cfd9e3;color:#1c2c3d;background:#fff}'
    );
  });

  it("uses the top navigation as the only module switcher and expands the workspace", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus(true)))
      .mockResolvedValueOnce(jsonResponse({
        datasources: [
          { id: "ds-1", name: "Firewall Syslog", plugin_code: "syslog", status: "active", plugin_config: {} }
        ]
      }))
      .mockResolvedValueOnce(jsonResponse({
        indexes: [
          { index_name: "app", ttl_days: 30, rows: 100, status: "active" },
          { index_name: "audit", ttl_days: 7, rows: 2, status: "active" }
        ]
      }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({
        parse_rules: [
          { id: "r1", name: "JSON", parser_plugin: "json", output_index: "app", plugin_config: {}, props_conf: "" },
          { id: "r2", name: "Regex", parser_plugin: "regex", output_index: "audit", plugin_config: {}, props_conf: "" },
          { id: "r3", name: "KV", parser_plugin: "kv", output_index: "audit", plugin_config: {}, props_conf: "" },
          { id: "r4", name: "CSV", parser_plugin: "delimited", output_index: "audit", plugin_config: {}, props_conf: "" }
        ]
      }));

    const wrapper = mount(App);
    await flushPromises();

    const topNavButtons = wrapper.findAll('[data-testid="main-nav"] button');
    expect(topNavButtons.map((button) => button.text())).toEqual(["采集配置", "解析配置", "索引配置", "搜索页", "插件管理"]);
    expect(wrapper.find(".sidebar").exists()).toBe(false);
    expect(wrapper.find(".workspace .main-panel").exists()).toBe(true);

    const source = readFileSync("src/AppMvp.vue", "utf8");
    expect(source).toContain(".workspace{display:block;margin-top:28px}");
    expect(source).not.toContain("<aside class=\"sidebar\">");
    expect(source).not.toContain("grid-template-columns:220px minmax(0,1fr)");
  });

  it("renders plugin management and uploads plugin packages through the unified API", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus(true)))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse({ indexes: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({
        plugins: [
          { plugin_code: "syslog", plugin_type: "input", plugin_version: "1.0.0", name: "Syslog", status: "active", checksum: "builtin" },
          { plugin_code: "http-input", plugin_type: "input", plugin_version: "1.0.0", name: "HTTP Input", status: "active", checksum: "" },
          { plugin_code: "regex", plugin_type: "parser", plugin_version: "1.0.0", name: "Regex", status: "active", checksum: "builtin" },
          { plugin_code: "json-parser", plugin_type: "parser", plugin_version: "1.0.0", name: "JSON Parser", status: "active", checksum: "" },
          { plugin_code: "props-conf-parser", plugin_type: "parser", plugin_version: "1.0.0", name: "Props.conf Parser", status: "active", checksum: "" },
          { plugin_code: "clickhouse-output", plugin_type: "output", plugin_version: "1.0.0", name: "ClickHouse Output", status: "active", checksum: "" },
          { plugin_code: "table", plugin_type: "search_command", plugin_version: "1.0.0", name: "table", status: "disabled", checksum: "sha256:table" }
        ]
      }))
      .mockResolvedValueOnce(jsonResponse({
        plugin_code: "demo-kafka",
        plugin_type: "input",
        plugin_version: "1.0.0",
        name: "Demo Kafka",
        status: "disabled",
        checksum: "sha256:demo"
      }, 201));

    const wrapper = mount(App);
    await flushPromises();

    await wrapper.get('[data-testid="nav-plugins"]').trigger("click");
    await flushPromises();

    expect(wrapper.get('[data-testid="plugins-page"]').text()).toContain("插件管理");
    expect(wrapper.get('[data-testid="plugins-page"]').text()).toContain("采集插件");
    expect(wrapper.get('[data-testid="plugins-page"]').text()).toContain("解析插件");
    expect(wrapper.get('[data-testid="plugins-page"]').text()).toContain("搜索命令插件");
    expect(wrapper.get('[data-testid="plugins-page"]').text()).not.toContain("SPL 函数插件");
    expect(wrapper.get('[data-testid="plugins-page"]').text()).toContain("Syslog");
    expect(wrapper.get('[data-testid="plugins-page"]').text()).not.toContain("HTTP Input");
    expect(wrapper.get('[data-testid="plugins-page"]').text()).not.toContain("ClickHouse Output");

    await wrapper.get('[data-testid="plugin-tab-parser"]').trigger("click");
    expect(wrapper.get('[data-testid="plugins-page"]').text()).toContain("Regex");
    expect(wrapper.get('[data-testid="plugins-page"]').text()).not.toContain("JSON Parser");
    expect(wrapper.get('[data-testid="plugins-page"]').text()).not.toContain("Props.conf Parser");

    await wrapper.get('[data-testid="plugin-tab-search_command"]').trigger("click");
    expect(wrapper.get('[data-testid="plugins-page"]').text()).toContain("table");
    expect(wrapper.get('[data-testid="plugins-page"]').text()).toContain("sha256:table");

    await wrapper.get('[data-testid="plugin-tab-input"]').trigger("click");
    const file = new File(["plugin zip"], "demo-kafka.zip", { type: "application/zip" });
    Object.defineProperty(wrapper.get('[data-testid="plugin-upload-file"]').element, "files", {
      value: [file],
      configurable: true
    });
    await wrapper.get('[data-testid="plugin-upload-file"]').trigger("change");
    expect(wrapper.get('[data-testid="plugin-upload-filename"]').text()).toBe("demo-kafka.zip");
    await wrapper.get('[data-testid="plugin-upload-button"]').trigger("click");
    await flushPromises();

    const uploadCall = global.fetch.mock.calls.find(([url]) => url === "/api/v1/plugins/import");
    expect(uploadCall).toBeTruthy();
    expect(uploadCall[1].method).toBe("POST");
    expect(uploadCall[1].headers.Authorization).toBe("Bearer test-token");
    expect(uploadCall[1].body).toBeInstanceOf(FormData);
    expect(wrapper.get('[data-testid="plugins-page"]').text()).toContain("Demo Kafka");
    expect(wrapper.get('[data-testid="plugin-upload-status"]').text()).toContain("导入成功");
  });

  it("loads plugin management pages independently from the enabled configuration catalog", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi.fn(async (url) => {
      const path = String(url);
      if (path === "/api/v1/auth") return jsonResponse(authStatus(true));
      if (path.startsWith("/api/v1/datasources?")) return jsonResponse({ datasources: [], pagination: { page: 1, page_size: 10, total: 0, total_pages: 1 } });
      if (path.startsWith("/api/v1/indexes?")) return jsonResponse({ indexes: [], pagination: { page: 1, page_size: 10, total: 0, total_pages: 1 } });
      if (path === "/api/v1/parser-plugins") return jsonResponse({ plugins: [] });
      if (path.startsWith("/api/v1/parse-rules?")) return jsonResponse({ parse_rules: [], pagination: { page: 1, page_size: 10, total: 0, total_pages: 1 } });
      if (path === "/api/v1/search/favorites") return jsonResponse({ saved_searches: [] });
      if (path === "/api/v1/plugins/catalog?plugin_type=input&status=enabled") {
        return jsonResponse({
          plugins: [
            { plugin_code: "syslog", plugin_type: "input", plugin_version: "1.0.0", name: "Syslog", status: "enabled", checksum: "builtin" },
            { plugin_code: "kafka-page-2", plugin_type: "input", plugin_version: "1.0.0", name: "Kafka Page 2", status: "enabled", checksum: "sha256:kafka" }
          ]
        });
      }
      if (path === "/api/v1/plugins?plugin_type=input&page=1&page_size=10") {
        return jsonResponse({
          plugins: [{ plugin_code: "syslog", plugin_type: "input", plugin_version: "1.0.0", name: "Syslog", status: "enabled", checksum: "builtin" }],
          pagination: { page: 1, page_size: 10, total: 12, total_pages: 2 },
          type_counts: { input: 12, parser: 3, search_command: 5 }
        });
      }
      if (path === "/api/v1/plugins?plugin_type=input&page=2&page_size=10") {
        return jsonResponse({
          plugins: [{ plugin_code: "kafka-page-2", plugin_type: "input", plugin_version: "1.0.0", name: "Kafka Page 2", status: "enabled", checksum: "sha256:kafka" }],
          pagination: { page: 2, page_size: 10, total: 12, total_pages: 2 },
          type_counts: { input: 12, parser: 3, search_command: 5 }
        });
      }
      if (path === "/api/v1/plugins?plugin_type=parser&page=1&page_size=10") {
        return jsonResponse({
          plugins: [{ plugin_code: "regex", plugin_type: "parser", plugin_version: "1.0.0", name: "Regex", status: "enabled", checksum: "builtin" }],
          pagination: { page: 1, page_size: 10, total: 3, total_pages: 1 },
          type_counts: { input: 12, parser: 3, search_command: 5 }
        });
      }
      return jsonResponse({});
    });

    const wrapper = mount(App);
    await flushPromises();
    await wrapper.get('[data-testid="nav-plugins"]').trigger("click");
    await flushPromises();

    expect(global.fetch).toHaveBeenCalledWith("/api/v1/plugins/catalog?plugin_type=input&status=enabled", expect.any(Object));
    expect(global.fetch).toHaveBeenCalledWith("/api/v1/plugins?plugin_type=input&page=1&page_size=10", expect.any(Object));
    expect(wrapper.get('[data-testid="plugin-tab-input"]').text()).toContain("12");

    await wrapper.get('[data-testid="plugin-next"]').trigger("click");
    await flushPromises();
    expect(global.fetch).toHaveBeenCalledWith("/api/v1/plugins?plugin_type=input&page=2&page_size=10", expect.any(Object));
    expect(wrapper.get('[data-testid="plugins-page"]').text()).toContain("Kafka Page 2");

    await wrapper.get('[data-testid="plugin-tab-parser"]').trigger("click");
    await flushPromises();
    expect(global.fetch).toHaveBeenCalledWith("/api/v1/plugins?plugin_type=parser&page=1&page_size=10", expect.any(Object));
    expect(wrapper.get('[data-testid="plugin-tab-parser"]').text()).toContain("3");

    await wrapper.get('[data-testid="plugin-tab-input"]').trigger("click");
    await flushPromises();
    expect(wrapper.get('[data-testid="plugin-page-2"]').classes()).toContain("active");
  });

  it("loads plugin detail, versions, schema, and manages plugin version state", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus(true)))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse({ indexes: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({
        plugins: [
          { plugin_code: "demo-kafka", plugin_type: "input", plugin_version: "1.0.0", name: "Demo Kafka", runtime: "go_builtin", status: "disabled", checksum: "sha256:demo" }
        ]
      }))
      .mockResolvedValueOnce(jsonResponse({
        plugin_code: "demo-kafka",
        plugin_type: "input",
        plugin_version: "1.0.0",
        name: "Demo Kafka",
        runtime: "go_builtin",
        status: "disabled",
        checksum: "sha256:demo",
        references: { count: 0 }
      }))
      .mockResolvedValueOnce(jsonResponse({
        config_schema: { type: "object", properties: { brokers: { type: "string" } }, required: ["brokers"] },
        ui_schema: { order: ["brokers"] }
      }))
      .mockResolvedValueOnce(jsonResponse({
        plugin_code: "demo-kafka",
        plugin_type: "input",
        plugin_version: "1.0.0",
        name: "Demo Kafka",
        runtime: "go_builtin",
        status: "disabled",
        checksum: "sha256:demo",
        references: { count: 0 }
      }))
      .mockResolvedValueOnce(jsonResponse({
        config_schema: { type: "object", properties: { brokers: { type: "string" } }, required: ["brokers"] },
        ui_schema: { order: ["brokers"] }
      }))
      .mockResolvedValueOnce(jsonResponse({ plugin_code: "demo-kafka", plugin_type: "input", plugin_version: "1.0.0", name: "Demo Kafka", status: "enabled", checksum: "sha256:demo" }))
      .mockResolvedValueOnce(jsonResponse({
        plugins: [{ plugin_code: "demo-kafka", plugin_type: "input", plugin_version: "1.0.0", name: "Demo Kafka", status: "enabled", checksum: "sha256:demo" }]
      }))
      .mockResolvedValueOnce(jsonResponse({ plugin_code: "demo-kafka", plugin_type: "input", plugin_version: "1.0.0", name: "Demo Kafka", status: "disabled", checksum: "sha256:demo" }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({}, 204))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }));

    const wrapper = mount(App);
    await flushPromises();

    await wrapper.get('[data-testid="nav-plugins"]').trigger("click");
    await flushPromises();

    await wrapper.get('[data-testid="plugin-detail-demo-kafka"]').trigger("click");
    await flushPromises();

    expect(wrapper.get('[data-testid="plugin-detail-row-demo-kafka"]').text()).toContain("Demo Kafka");
    expect(wrapper.get('[data-testid="plugin-detail-panel"]').text()).toContain("sha256:demo");
    expect(wrapper.get('[data-testid="plugin-detail-panel"]').text()).toContain("brokers");
    expect(wrapper.get('[data-testid="plugin-detail-demo-kafka"]').classes()).toContain("plugin-action-detail");
    expect(wrapper.get('[data-testid="plugin-detail-demo-kafka"]').text()).toBe("收起详情");

    await wrapper.get('[data-testid="plugin-detail-demo-kafka"]').trigger("click");
    await flushPromises();
    expect(wrapper.find('[data-testid="plugin-detail-row-demo-kafka"]').exists()).toBe(false);

    await wrapper.get('[data-testid="plugin-detail-demo-kafka"]').trigger("click");
    await flushPromises();
    expect(wrapper.get('[data-testid="plugin-detail-row-demo-kafka"]').text()).toContain("Demo Kafka");

    expect(wrapper.find('[data-testid="plugin-version-row-demo-kafka-1.0.0"]').exists()).toBe(false);

    await wrapper.get('[data-testid="plugin-enable-demo-kafka"]').trigger("click");
    await flushPromises();
    expect(wrapper.get('[data-testid="plugins-page"]').text()).toContain("已启用");
    expect(wrapper.get('[data-testid="plugin-disable-demo-kafka"]').classes()).toContain("plugin-action-disable");

    await wrapper.get('[data-testid="plugin-disable-demo-kafka"]').trigger("click");
    await flushPromises();
    expect(wrapper.get('[data-testid="plugins-page"]').text()).toContain("未启用");
    expect(wrapper.get('[data-testid="plugin-delete-demo-kafka"]').classes()).toContain("plugin-action-delete");

    await wrapper.get('[data-testid="plugin-delete-demo-kafka"]').trigger("click");
    await flushPromises();
    expect(wrapper.get('[data-testid="plugins-page"]').text()).not.toContain("Demo Kafka");

    expect(global.fetch).toHaveBeenCalledWith("/api/v1/plugins/demo-kafka?plugin_type=input", expect.objectContaining({ headers: expect.objectContaining({ Authorization: "Bearer test-token" }) }));
    expect(global.fetch).toHaveBeenCalledWith("/api/v1/plugins/demo-kafka/schema?plugin_type=input", expect.objectContaining({ headers: expect.objectContaining({ Authorization: "Bearer test-token" }) }));
    expect(global.fetch).toHaveBeenCalledWith("/api/v1/plugins/demo-kafka/enable?plugin_type=input", expect.objectContaining({ method: "POST", headers: expect.objectContaining({ Authorization: "Bearer test-token" }) }));
    expect(global.fetch).toHaveBeenCalledWith("/api/v1/plugins/demo-kafka/disable?plugin_type=input", expect.objectContaining({ method: "POST", headers: expect.objectContaining({ Authorization: "Bearer test-token" }) }));
    expect(global.fetch).toHaveBeenCalledWith("/api/v1/plugins/demo-kafka?plugin_type=input", expect.objectContaining({ method: "DELETE", headers: expect.objectContaining({ Authorization: "Bearer test-token" }) }));
  });

  it("opens inline details for imported input, parser, and search command plugins only", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus(true)))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse({ indexes: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({
        plugins: [
          { plugin_code: "syslog", plugin_type: "input", plugin_version: "1.0.0", name: "Syslog", runtime: "go_builtin", status: "active", checksum: "builtin" },
          { plugin_code: "demo-kafka", plugin_type: "input", plugin_version: "1.0.0", name: "Demo Kafka", runtime: "go_builtin", status: "enabled", checksum: "sha256:kafka" },
          { plugin_code: "regex", plugin_type: "parser", plugin_version: "1.0.0", name: "Regex", runtime: "go_builtin", status: "active", checksum: "builtin" },
          { plugin_code: "vendor-fw", plugin_type: "parser", plugin_version: "2.0.0", name: "Vendor Firewall Parser", runtime: "go_plugin", status: "enabled", checksum: "sha256:parser" },
          { plugin_code: "stats", plugin_type: "search_command", plugin_version: "1.0.0", name: "stats", runtime: "go_builtin", status: "active", checksum: "builtin" },
          { plugin_code: "table", plugin_type: "search_command", plugin_version: "1.0.0", name: "Table Command", runtime: "go_plugin", status: "disabled", checksum: "sha256:table" }
        ]
      }))
      .mockResolvedValueOnce(jsonResponse({
        plugin_code: "demo-kafka",
        plugin_type: "input",
        plugin_version: "1.0.0",
        name: "Demo Kafka",
        runtime: "go_builtin",
	        status: "enabled",
	        checksum: "sha256:kafka",
	        references: {
	          count: 2,
	          items: [
	            { type: "datasource", id: "ds-kafka", name: "Kafka Source", status: "active" },
	            { type: "datasource", id: "ds-kafka-backup", name: "Kafka Backup", status: "disabled" }
	          ]
	        }
      }))
      .mockResolvedValueOnce(jsonResponse({
        config_schema: { type: "object", properties: { brokers: { type: "string" } } },
        ui_schema: { order: ["brokers"] }
      }))
      .mockResolvedValueOnce(jsonResponse({
        plugin_code: "vendor-fw",
        plugin_type: "parser",
        plugin_version: "2.0.0",
        name: "Vendor Firewall Parser",
        runtime: "go_plugin",
	        status: "enabled",
	        checksum: "sha256:parser",
	        references: {
	          count: 1,
	          items: [{ type: "parse_rule", id: "rule-fw", name: "Vendor FW", status: "active" }]
	        }
      }))
      .mockResolvedValueOnce(jsonResponse({
        config_schema: { type: "object", properties: { regex_pattern: { type: "string" } } },
        ui_schema: { order: ["regex_pattern"] }
      }))
      .mockResolvedValueOnce(jsonResponse({
        plugin_code: "table",
        plugin_type: "search_command",
        plugin_version: "1.0.0",
        name: "Table Command",
        runtime: "go_plugin",
	        status: "disabled",
	        checksum: "sha256:table",
	        references: { count: 0, items: [] }
      }))
      .mockResolvedValueOnce(jsonResponse({
        config_schema: { type: "object", properties: { fields: { type: "array" } } },
        ui_schema: { order: ["fields"] }
      }))
      .mockResolvedValueOnce(jsonResponse({
        audits: [
          {
            request_id: "req-1",
            plugin_code: "table",
            plugin_type: "search_command",
            plugin_version: "1.0.0",
            command_name: "table",
            runtime: "executable_search_command",
            elapsed_ms: 12,
            input_rows: 3,
            output_rows: 3,
            success: true,
            error_code: "",
            created_at: "2026-07-12T12:00:00+08:00"
          }
        ]
      }));

    const wrapper = mount(App);
    await flushPromises();

    await wrapper.get('[data-testid="nav-plugins"]').trigger("click");
    await flushPromises();

    expect(wrapper.find('[data-testid="plugin-detail-syslog"]').exists()).toBe(false);
    await wrapper.get('[data-testid="plugin-detail-demo-kafka"]').trigger("click");
    await flushPromises();
	    expect(wrapper.get('[data-testid="plugin-detail-row-demo-kafka"]').text()).toContain("引用 2 处");
	    expect(wrapper.get('[data-testid="plugin-detail-row-demo-kafka"]').text()).toContain("Kafka Source");
	    expect(wrapper.get('[data-testid="plugin-detail-row-demo-kafka"]').text()).toContain("采集源");
	    expect(wrapper.find('[data-testid="plugin-disable-demo-kafka"]').exists()).toBe(true);
	    expect(wrapper.find('[data-testid="plugin-delete-demo-kafka"]').exists()).toBe(false);
	    expect(wrapper.get('[data-testid="plugin-detail-row-demo-kafka"]').text()).toContain("被引用，不能停用或删除");
	    expect(wrapper.get('[data-testid="plugin-detail-panel"]').text()).toContain("brokers");

    await wrapper.get('[data-testid="plugin-tab-parser"]').trigger("click");
    await flushPromises();
    expect(wrapper.find('[data-testid="plugin-detail-regex"]').exists()).toBe(false);
    await wrapper.get('[data-testid="plugin-detail-vendor-fw"]').trigger("click");
    await flushPromises();
    expect(wrapper.find('[data-testid="plugin-detail-row-demo-kafka"]').exists()).toBe(false);
	    expect(wrapper.get('[data-testid="plugin-detail-row-vendor-fw"]').text()).toContain("Vendor Firewall Parser");
	    expect(wrapper.get('[data-testid="plugin-detail-row-vendor-fw"]').text()).toContain("Vendor FW");
	    expect(wrapper.get('[data-testid="plugin-detail-row-vendor-fw"]').text()).toContain("解析规则");
	    expect(wrapper.get('[data-testid="plugin-detail-panel"]').text()).toContain("regex_pattern");

    await wrapper.get('[data-testid="plugin-tab-search_command"]').trigger("click");
    await flushPromises();
    expect(wrapper.find('[data-testid="plugin-detail-stats"]').exists()).toBe(false);
    await wrapper.get('[data-testid="plugin-detail-table"]').trigger("click");
    await flushPromises();
	    expect(wrapper.find('[data-testid="plugin-detail-row-vendor-fw"]').exists()).toBe(false);
	    expect(wrapper.get('[data-testid="plugin-detail-row-table"]').text()).toContain("Table Command");
	    expect(wrapper.get('[data-testid="plugin-detail-row-table"]').text()).not.toContain("引用类型");
	    expect(wrapper.get('[data-testid="plugin-detail-row-table"]').text()).not.toContain("引用对象");
	    expect(wrapper.get('[data-testid="plugin-execution-audits"]').text()).toContain("最近执行记录");
	    expect(wrapper.get('[data-testid="plugin-execution-audits"]').text()).toContain("table");
	    expect(wrapper.get('[data-testid="plugin-execution-audits"]').text()).toContain("12ms");
	    expect(wrapper.get('[data-testid="plugin-execution-audits"]').text()).toContain("成功");
	    expect(wrapper.find('[data-testid="plugin-enable-table"]').exists()).toBe(true);
	    expect(wrapper.find('[data-testid="plugin-disable-table"]').exists()).toBe(false);
	    expect(wrapper.find('[data-testid="plugin-delete-table"]').exists()).toBe(true);
	    expect(wrapper.get('[data-testid="plugin-detail-panel"]').text()).toContain("fields");

    expect(global.fetch).toHaveBeenCalledWith("/api/v1/plugins/demo-kafka?plugin_type=input", expect.any(Object));
    expect(global.fetch).toHaveBeenCalledWith("/api/v1/plugins/vendor-fw?plugin_type=parser", expect.any(Object));
    expect(global.fetch).toHaveBeenCalledWith("/api/v1/plugins/table?plugin_type=search_command", expect.any(Object));
    expect(global.fetch).toHaveBeenCalledWith("/api/v1/plugins/table/execution-audits?plugin_type=search_command&limit=20", expect.any(Object));
  });

  it("shows plugin package and lifecycle error codes in the page", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus(true)))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse({ indexes: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({
        plugins: [
          { plugin_code: "demo-kafka", plugin_type: "input", plugin_version: "1.0.0", name: "Demo Kafka", runtime: "go_builtin", status: "enabled", checksum: "sha256:demo" }
        ]
      }))
      .mockResolvedValueOnce(jsonResponse({ error: { code: "PLUGIN_MANIFEST_MISSING", message: "manifest.json is required" } }, 400))
      .mockResolvedValueOnce(jsonResponse({ error: { code: "PLUGIN_ALREADY_EXISTS", message: "plugin already exists, confirm overwrite to replace the current plugin package" } }, 409))
      .mockResolvedValueOnce(jsonResponse({
        plugin_code: "demo-kafka",
        plugin_type: "input",
        plugin_version: "1.0.0",
        name: "Demo Kafka",
        runtime: "go_builtin",
        status: "enabled",
        checksum: "sha256:demo-overwrite"
      }, 201))
      .mockResolvedValueOnce(jsonResponse({
        plugins: [
          { plugin_code: "demo-kafka", plugin_type: "input", plugin_version: "1.0.0", name: "Demo Kafka", runtime: "go_builtin", status: "enabled", checksum: "sha256:demo-overwrite" }
        ]
      }))
      .mockResolvedValueOnce(jsonResponse({ error: { code: "PLUGIN_IN_USE", message: "plugin is in use" } }, 409));

    const wrapper = mount(App);
    await flushPromises();

    await wrapper.get('[data-testid="nav-plugins"]').trigger("click");
    await flushPromises();

    const file = new File(["bad plugin"], "bad.zip", { type: "application/zip" });
    Object.defineProperty(wrapper.get('[data-testid="plugin-upload-file"]').element, "files", {
      value: [file],
      configurable: true
    });
    await wrapper.get('[data-testid="plugin-upload-file"]').trigger("change");
    await wrapper.get('[data-testid="plugin-upload-button"]').trigger("click");
    await flushPromises();

    expect(wrapper.get('[data-testid="plugins-page"]').text()).toContain("PLUGIN_MANIFEST_MISSING");
    expect(wrapper.get('[data-testid="plugins-page"]').text()).toContain("manifest.json is required");
    expect(wrapper.get('[data-testid="plugin-upload-error"]').text()).toContain("PLUGIN_MANIFEST_MISSING");

    vi.spyOn(window, "confirm").mockReturnValueOnce(true);
    await wrapper.get('[data-testid="plugin-upload-button"]').trigger("click");
    await flushPromises();
    expect(wrapper.get('[data-testid="plugin-upload-status"]').text()).toContain("已覆盖");
    expect(global.fetch).toHaveBeenCalledWith("/api/v1/plugins/import?overwrite=true", expect.objectContaining({ method: "POST" }));

    await wrapper.get('[data-testid="plugin-disable-demo-kafka"]').trigger("click");
    await flushPromises();
    expect(wrapper.get('[data-testid="plugins-page"]').text()).toContain("PLUGIN_IN_USE");
    expect(wrapper.get('[data-testid="plugins-page"]').text()).toContain("plugin is in use");
  });

  it("shows built-in plugins without detail actions", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus(true)))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse({ indexes: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({
        plugins: [
          { plugin_code: "syslog", plugin_type: "input", plugin_version: "1.0.0", name: "Syslog", runtime: "go_builtin", status: "active", checksum: "builtin" },
          { plugin_code: "regex", plugin_type: "parser", plugin_version: "1.0.0", name: "Regex", runtime: "go_builtin", status: "active", checksum: "builtin" },
          { plugin_code: "stats", plugin_type: "search_command", plugin_version: "1.0.0", name: "stats", runtime: "go_builtin", status: "active", checksum: "builtin" },
          { plugin_code: "demo-kafka", plugin_type: "input", plugin_version: "1.0.0", name: "Demo Kafka", runtime: "go_builtin", status: "disabled", checksum: "sha256:demo" }
        ]
      }));

    const wrapper = mount(App);
    await flushPromises();

    await wrapper.get('[data-testid="nav-plugins"]').trigger("click");
    await flushPromises();

    expect(wrapper.get('[data-testid="plugins-page"]').text()).toContain("Syslog");
    expect(wrapper.find('[data-testid="plugin-detail-syslog"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="plugin-detail-demo-kafka"]').exists()).toBe(true);

    await wrapper.get('[data-testid="plugin-tab-parser"]').trigger("click");
    await flushPromises();
    expect(wrapper.find('[data-testid="plugin-detail-regex"]').exists()).toBe(false);

    await wrapper.get('[data-testid="plugin-tab-search_command"]').trigger("click");
    await flushPromises();
    expect(wrapper.find('[data-testid="plugin-detail-stats"]').exists()).toBe(false);
  });

  it("deduplicates display-equivalent built-in plugins in the list", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus(true)))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse({ indexes: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({
        plugins: [
          { plugin_code: "syslog", plugin_type: "input", plugin_version: "1.0.0", name: "Syslog", runtime: "go_builtin", status: "active", checksum: "builtin" },
          { plugin_code: " syslog ", plugin_type: "collect", plugin_version: "", name: "Syslog legacy", runtime: "go_builtin", status: "active", checksum: "legacy" }
        ]
      }));

    const wrapper = mount(App);
    await flushPromises();

    await wrapper.get('[data-testid="nav-plugins"]').trigger("click");
    await flushPromises();

    expect(wrapper.findAll('[data-testid="plugin-row-syslog-1.0.0"]')).toHaveLength(1);
    expect(wrapper.find('[data-testid="plugin-detail-syslog"]').exists()).toBe(false);
  });

  it("persists the current console module across refresh and clears it on logout", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValue(jsonResponse(authStatus(true)));

    const wrapper = mount(App);
    await flushPromises();

    await wrapper.get('[data-testid="nav-search"]').trigger("click");
    expect(localStorage.getItem("xdp_current_module")).toBe("search");
    expect(wrapper.find('[data-testid="search-page"]').exists()).toBe(true);
    wrapper.unmount();

    const refreshed = mount(App);
    await flushPromises();

    expect(refreshed.find('[data-testid="search-page"]').exists()).toBe(true);
    expect(refreshed.find('[data-testid="collect-page"]').exists()).toBe(false);

    localStorage.setItem("xdp_current_module", "unknown");
    refreshed.unmount();
    const invalidStored = mount(App);
    await flushPromises();

    expect(invalidStored.find('[data-testid="collect-page"]').exists()).toBe(true);
    expect(localStorage.getItem("xdp_current_module")).toBe("collect");

    await invalidStored.get('[data-testid="nav-parse"]').trigger("click");
    expect(localStorage.getItem("xdp_current_module")).toBe("parse");
    await invalidStored.get('[data-testid="logout"]').trigger("click");

    expect(localStorage.getItem("xdp_current_module")).toBeNull();
    expect(localStorage.getItem("xdp_api_token")).toBeNull();
  });

  it("supports parser preview and automatic props.conf synchronization", async () => {
    const wrapper = await mountAuthenticatedApp();

    await wrapper.get('[data-testid="nav-parse"]').trigger("click");
    await wrapper.get('[data-testid="show-rule-form"]').trigger("click");
    expect(wrapper.get('[data-testid="parser-regex"]').classes()).toContain("active");
    expect(wrapper.find('[data-testid="sync-props"]').exists()).toBe(false);
    await wrapper.get('[data-testid="sample-log"]').setValue("src=10.0.1.8 dst=172.16.0.4 action=deny");
    await wrapper.get('[data-testid="regex-pattern"]').setValue("src=(?<src_ip>\\S+)\\s+dst=(?<dst_ip>\\S+)\\s+action=(?<action>\\S+)");
    expect(wrapper.get('[data-testid="props-conf"]').element.value).toContain("EXTRACT-custom");
    await wrapper.get('[data-testid="preview-parse"]').trigger("click");

    expect(wrapper.get('[data-testid="parse-preview"]').text()).toContain("src_ip");
    expect(wrapper.get('[data-testid="parse-preview"]').text()).toContain("10.0.1.8");
  });

  it("renders index and search pages according to the MVP prototype", async () => {
    const wrapper = await mountAuthenticatedApp();

    await wrapper.get('[data-testid="nav-index"]').trigger("click");
    expect(wrapper.get('[data-testid="index-page"]').text()).toContain("ClickHouse 物理分表");
    expect(wrapper.get('[data-testid="index-page"]').text()).toContain("events_app");
    expect(wrapper.get('[data-testid="index-page"]').text()).not.toContain("显示名称");

    await wrapper.get('[data-testid="nav-search"]').trigger("click");
    const searchBox = wrapper.get(".search-box");
    expect(searchBox.element.value).toBe("");
    expect(searchBox.attributes("placeholder")).toBe("请输入 SPL语句");
    expect(wrapper.get('[data-testid="search-time"]').text()).toContain("近 1 天");
    expect(wrapper.get('[data-testid="search-time"]').text()).toContain("高级时间表达式");
    expect(wrapper.get('[data-testid="time-help"]').text()).toContain("@d");
    await searchBox.setValue("index=app | stats count as total by service");

    global.fetch.mockResolvedValueOnce(jsonResponse({
      mode: "stats",
      spl: "index=app | stats count as total by service",
      index: "app",
      elapsed_ms: 8,
      stats: {
        fields: ["service", "total"],
        rows: [{ service: "api", total: 2 }]
      }
    })).mockResolvedValueOnce(jsonResponse({
      interval: "hour",
      buckets: [
        { start: "2026-06-27T10:00:00+08:00", end: "2026-06-27T11:00:00+08:00", count: 1 },
        { start: "2026-06-27T11:00:00+08:00", end: "2026-06-27T12:00:00+08:00", count: 0 },
        { start: "2026-06-27T12:00:00+08:00", end: "2026-06-27T13:00:00+08:00", count: 3 }
      ]
    }));
    await wrapper.get('[data-testid="search-button"]').trigger("click");
    await flushPromises();
    const searchCall = global.fetch.mock.calls.find(([url]) => String(url).startsWith("/api/v1/search?"));
    expect(searchCall).toBeTruthy();
    expect(String(searchCall[0])).toContain("q=index%3Dapp");
    const timelineCall = global.fetch.mock.calls.find(([url]) => String(url).startsWith("/api/v1/search/timeline?"));
    expect(timelineCall).toBeTruthy();
    expect(String(timelineCall[0])).toContain("q=index%3Dapp");
    expect(String(timelineCall[0])).toContain("stats+count+as+total+by+service");
    expect(String(timelineCall[0])).toContain("interval=auto");
    expect(wrapper.get('[data-testid="timeline-y-axis"]').text()).toContain("3");
    expect(wrapper.get('[data-testid="timeline-x-axis"]').text()).toContain("06/27");
    const bars = wrapper.findAll('[data-testid="timeline-bar"]');
    expect(bars).toHaveLength(3);
    expect(bars.map((bar) => bar.text())).toEqual(["", "", ""]);
    expect(bars[0].attributes("style")).toContain("height: 33%");
    expect(bars[1].attributes("style")).toContain("height: 0%");
    expect(bars[2].attributes("style")).toContain("height: 100%");
    expect(wrapper.get('[data-testid="result-mode"]').text()).toContain("统计视图");
    const searchResults = wrapper.get('[data-testid="search-results"]');
    const headers = searchResults.findAll("thead th").map((cell) => cell.text());
    const cells = searchResults.findAll("tbody td").map((cell) => cell.text());
    expect(headers).toEqual(["service", "total"]);
    expect(cells).toEqual(["api", "2"]);
    expect(searchResults.text()).not.toContain("service=api");
    expect(searchResults.text()).not.toContain("total=2");
  });

  it("renders table search command rows as plain table values", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse({ indexes: [{ index_name: "app", name: "app", ttl_days: 30, rows: 2, status: "active" }] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }));

    const wrapper = mount(App);
    await flushPromises();
    await wrapper.get('[data-testid="nav-search"]').trigger("click");
    await wrapper.get('[data-testid="search-query"]').setValue("index=app | table service action bytes | sort - bytes | head 2");
    global.fetch.mockResolvedValueOnce(jsonResponse({
      mode: "table",
      spl: "index=app | table service action bytes | sort - bytes | head 2",
      index: "app",
      elapsed_ms: 12,
      search_command: { plugin_code: "table", plugin_type: "search_command", plugin_version: "1.0.0", runtime: "go_builtin", output_mode: "table" },
      table: {
        fields: ["service", "action", "bytes"],
        rows: [
          { service: "worker", action: "deny", bytes: 300 },
          { service: "api", action: "allow", bytes: 100 }
        ]
      },
      pagination: { limit: 20, offset: 0, page: 1, returned: 2, has_more: false, total: 2 }
    })).mockResolvedValueOnce(jsonResponse({ interval: "hour", buckets: [] }));

    await wrapper.get('[data-testid="search-button"]').trigger("click");
    await flushPromises();

    expect(wrapper.get('[data-testid="result-mode"]').text()).toContain("表格视图");
    const searchResults = wrapper.get('[data-testid="search-results"]');
    const headers = searchResults.findAll("thead th").map((cell) => cell.text());
    const cells = searchResults.findAll("tbody td").map((cell) => cell.text());
    expect(headers).toEqual(["service", "action", "bytes"]);
    expect(cells).toEqual(["worker", "deny", "300", "api", "allow", "100"]);
    expect(searchResults.text()).not.toContain("service=worker");
    expect(searchResults.text()).not.toContain("bytes=300");
  });

  it("does not render duplicate timeline y-axis labels when max count is one", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse({ indexes: [{ index_name: "audit_alt", name: "audit_alt", ttl_days: 30, rows: 1, status: "active" }] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }));

    const wrapper = mount(App);
    await flushPromises();
    await wrapper.get('[data-testid="nav-search"]').trigger("click");
    await flushPromises();

    await wrapper.get(".search-box").setValue("index=audit_alt");
    global.fetch
      .mockResolvedValueOnce(jsonResponse({
        mode: "events",
        spl: "index=audit_alt",
        index: "audit_alt",
        elapsed_ms: 47,
        events: [{ time: "07/02 23:52:51", event: "traffic src=10.0.1.8 dst=172.16.0.4 bytes=2048", raw: "traffic src=10.0.1.8 dst=172.16.0.4 bytes=2048" }],
        pagination: { limit: 20, offset: 0, page: 1, returned: 1, has_more: false, total: 1 }
      }))
      .mockResolvedValueOnce(jsonResponse({
        interval: "day",
        buckets: [
          { start: "2026-07-02T00:00:00+08:00", end: "2026-07-03T00:00:00+08:00", count: 1 }
        ]
      }));
    await wrapper.get('[data-testid="search-button"]').trigger("click");
    await flushPromises();

    const labels = wrapper.get('[data-testid="timeline-y-axis"]').findAll("span").map((label) => label.text()).filter(Boolean);
    expect(labels).toEqual(["1", "0"]);
  });

  it("loads and deletes a saved search through the server API", async () => {
    const wrapper = await mountAuthenticatedApp();

    await wrapper.get('[data-testid="nav-search"]').trigger("click");
    expect(wrapper.get('[data-testid="saved-summary"]').text()).toContain("2");

    global.fetch
      .mockResolvedValueOnce(jsonResponse({
        saved_searches: [
          { id: "s-1", name: "App stats", spl: "index=app | stats count as total by service", time_range_type: "近 1 天" },
          { id: "s-2", name: "Firewall deny", spl: "index=firewall action=deny", time_range_type: "近 7 天" }
        ]
      }))
      .mockResolvedValueOnce(jsonResponse({ deleted: true, id: "s-1" }));

    await wrapper.get('[data-testid="toggle-saved-searches"]').trigger("click");
    await flushPromises();
    expect(wrapper.get('[data-testid="saved-search-row-s-1"]').text()).toContain("index=app | stats count as total by service");

    await wrapper.get('[data-testid="delete-saved-search-s-1"]').trigger("click");
    await flushPromises();

    expect(wrapper.get('[data-testid="saved-summary"]').text()).toContain("1");
    expect(wrapper.find('[data-testid="saved-search-row-s-1"]').exists()).toBe(false);
    expect(wrapper.get(".saved-drawer").text()).not.toContain("index=app | stats count as total by service");
    const favoriteListCall = global.fetch.mock.calls.find(([url, options]) => (
      String(url).includes("/api/v1/search/favorites") && (!options?.method || options.method === "GET")
    ));
    expect(favoriteListCall).toBeTruthy();
    expect(favoriteListCall[1].headers.Authorization).toBe("Bearer test-token");
    const favoriteDeleteCall = global.fetch.mock.calls.find(([url, options]) => (
      String(url).includes("/api/v1/search/favorites") && options?.method === "DELETE"
    ));
    expect(favoriteDeleteCall).toBeTruthy();
    expect(String(favoriteDeleteCall[0])).toContain("/api/v1/search/favorites/s-1");
    expect(favoriteDeleteCall[1].headers.Authorization).toBe("Bearer test-token");
  });

  it("shows only time and event columns for normal event searches", async () => {
    const wrapper = await mountAuthenticatedApp();

    await wrapper.get('[data-testid="nav-search"]').trigger("click");
    await wrapper.get(".search-box").setValue("index=audit action=deny");
    global.fetch.mockResolvedValueOnce(jsonResponse({
      mode: "events",
      spl: "index=audit action=deny",
      elapsed_ms: 5,
      events: [{
        event_id: "evt-audit-1",
        event_time: "2026-06-28T10:30:00+08:00",
        raw: "src=10.0.1.8 dst=172.16.0.4 action=deny bytes=2048",
        display: {
          time: "2026-06-28T10:30:00+08:00",
          event: "src=10.0.1.8 dst=172.16.0.4 action=deny bytes=2048",
          expandable: true
        },
        detail: {
          raw: "src=10.0.1.8 dst=172.16.0.4 action=deny bytes=2048",
          field_rows: [
            { category: "metadata", name: "index", value: "audit", type: "string" },
            { category: "metadata", name: "source", value: "Firewall Syslog", type: "string" },
            { category: "metadata", name: "sourcetype", value: "Firewall Regex", type: "string" },
            { category: "metadata", name: "parse_status", value: "parsed", type: "string" },
            { category: "field", name: "src_ip", value: "10.0.1.8", type: "string" },
            { category: "field", name: "action", value: "deny", type: "string" },
            { category: "field", name: "bytes", value: "2048", type: "number" }
          ]
        },
        metadata: {
          index: "audit",
          source_name: "Firewall Syslog",
          sourcetype: "Firewall Regex",
          parse_status: "parsed",
          parse_rule_id: "pr_firewall_regex",
          parse_rule_name: "Firewall Regex",
          parse_error: "",
          parsed_at: "2026-06-28T10:30:00+08:00"
        }
      }],
      time_range: {
        start_time: "2026-06-28T00:00:00+08:00",
        end_time: "2026-06-28T23:59:59+08:00"
      },
      pagination: { limit: 20, offset: 0, page: 1, returned: 1, has_more: false, total: 179497 }
    })).mockResolvedValueOnce(jsonResponse({ interval: "hour", buckets: [] }));

    await wrapper.get('[data-testid="search-button"]').trigger("click");
    await flushPromises();

    const searchResults = wrapper.get('[data-testid="search-results"]');
    const headers = searchResults.findAll("thead th").map((cell) => cell.text());
    const cells = searchResults.findAll("tbody td").map((cell) => cell.text());
    expect(headers).toEqual(["", "时间", "事件"]);
    expect(cells).toEqual(["▶", "06/28 10:30:00", "src=10.0.1.8 dst=172.16.0.4 action=deny bytes=2048"]);
    expect(wrapper.get(".result-meta").text()).toContain("179,497 个事件");
    expect(wrapper.get(".result-meta").text()).toContain("2026-06-28 00:00:00 - 2026-06-28 23:59:59");
    expect(wrapper.get(".result-meta").text()).not.toContain("近 1 天");
    expect(searchResults.text()).not.toContain("Firewall Syslog");
    expect(searchResults.text()).not.toContain("Firewall Regex");
    expect(searchResults.text()).not.toContain("audit");
    expect(wrapper.vm.searchResults[0]).toMatchObject({
      source: "Firewall Syslog",
      sourcetype: "Firewall Regex",
      index: "audit",
      parseStatus: "parsed",
      parseRuleId: "pr_firewall_regex",
      parseRuleName: "Firewall Regex",
      parseError: ""
    });

    await wrapper.get('[data-testid="expand-event-evt-audit-1"]').trigger("click");
    await flushPromises();

    expect(searchResults.text()).toContain("Firewall Syslog");
    expect(searchResults.text()).toContain("Firewall Regex");
    expect(searchResults.text()).toContain("src_ip");
    expect(searchResults.text()).toContain("10.0.1.8");
    expect(searchResults.text()).not.toContain("src_ip=10.0.1.8");
    const detailHeaders = searchResults.find(".event-detail table").findAll("thead th").map((cell) => cell.text());
    expect(detailHeaders).toEqual(["字段", "值"]);
    expect(searchResults.text()).not.toContain("分类");
    expect(searchResults.text()).not.toContain("类型");

    await wrapper.get('[data-testid="expand-event-evt-audit-1"]').trigger("click");
    await flushPromises();

    expect(searchResults.text()).not.toContain("Firewall Syslog");
  });

  it("paginates search results and resets to the first page for a new query", async () => {
    const wrapper = await mountAuthenticatedApp();

    await wrapper.get('[data-testid="nav-search"]').trigger("click");
    await wrapper.get(".search-box").setValue("index=audit");
    global.fetch.mockResolvedValueOnce(jsonResponse({
      mode: "events",
      spl: "index=audit",
      elapsed_ms: 5,
      events: [{
        event_id: "evt-page-1",
        display: { time: "2026-06-28T10:30:00+08:00", event: "first event", expandable: true },
        detail: { raw: "first event", field_rows: [] },
        raw: "first event"
      }],
      time_range: {
        start_time: "2026-06-28T00:00:00+08:00",
        end_time: "2026-06-28T23:59:59+08:00"
      },
      pagination: { limit: 20, offset: 0, page: 1, returned: 1, has_more: true, total: 42 }
    })).mockResolvedValueOnce(jsonResponse({ interval: "hour", buckets: [] }));

    const pageSize = wrapper.get('[data-testid="search-page-size"]');
    expect(pageSize.element.value).toBe("20");
    expect(pageSize.findAll("option").map((option) => option.text())).toEqual(["20 条/页", "50 条/页", "100 条/页", "1000 条/页"]);
    expect(wrapper.find('[data-testid="search-pagination-right"]').exists()).toBe(true);

    await wrapper.get('[data-testid="search-button"]').trigger("click");
    await flushPromises();

    expect(wrapper.get('[data-testid="search-page-1"]').classes()).toContain("active");
    expect(wrapper.find('[data-testid="search-page-2"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="search-page-3"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="search-page-4"]').exists()).toBe(false);
    expect(wrapper.get('[data-testid="search-pagination"]').text()).not.toContain("本页");
    expect(wrapper.get('[data-testid="search-pagination"]').text()).not.toContain("显示 1-20");
    expect(wrapper.get(".result-meta").text()).toContain("42 个事件");
    expect(wrapper.get(".result-meta").text()).toContain("2026-06-28 00:00:00 - 2026-06-28 23:59:59");
    expect(wrapper.get('[data-testid="search-prev"]').attributes("disabled")).toBeDefined();
    expect(wrapper.get('[data-testid="search-next"]').attributes("disabled")).toBeUndefined();
    const firstSearchCall = global.fetch.mock.calls.find(([url]) => String(url).startsWith("/api/v1/search?"));
    expect(String(firstSearchCall[0])).toContain("limit=20");
    expect(String(firstSearchCall[0])).toContain("page=1");

    global.fetch.mockResolvedValueOnce(jsonResponse({
      mode: "events",
      spl: "index=audit",
      elapsed_ms: 6,
      events: [{
        event_id: "evt-page-2",
        display: { time: "2026-06-28T10:29:00+08:00", event: "second event", expandable: true },
        detail: { raw: "second event", field_rows: [] },
        raw: "second event"
      }],
      time_range: {
        start_time: "2026-06-28T00:00:00+08:00",
        end_time: "2026-06-28T23:59:59+08:00"
      },
      pagination: { limit: 20, offset: 20, page: 2, returned: 1, has_more: false, total: 42 }
    })).mockResolvedValueOnce(jsonResponse({ interval: "hour", buckets: [] }));

    await wrapper.get('[data-testid="search-next"]').trigger("click");
    await flushPromises();

    const searchCalls = global.fetch.mock.calls.filter(([url]) => String(url).startsWith("/api/v1/search?"));
    expect(String(searchCalls.at(-1)[0])).toContain("page=2");
    expect(wrapper.get('[data-testid="search-page-2"]').classes()).toContain("active");
    expect(wrapper.find('[data-testid="search-page-3"]').exists()).toBe(true);
    expect(wrapper.get('[data-testid="search-next"]').attributes("disabled")).toBeUndefined();

    global.fetch.mockResolvedValueOnce(jsonResponse({
      mode: "events",
      spl: "index=audit",
      elapsed_ms: 7,
      events: [{
        event_id: "evt-page-3",
        display: { time: "2026-06-28T10:28:00+08:00", event: "third event", expandable: true },
        detail: { raw: "third event", field_rows: [] },
        raw: "third event"
      }],
      time_range: {
        start_time: "2026-06-28T00:00:00+08:00",
        end_time: "2026-06-28T23:59:59+08:00"
      },
      pagination: { limit: 20, offset: 40, page: 3, returned: 1, has_more: false, total: 42 }
    })).mockResolvedValueOnce(jsonResponse({ interval: "hour", buckets: [] }));

    await wrapper.get('[data-testid="search-next"]').trigger("click");
    await flushPromises();

    expect(wrapper.get('[data-testid="search-page-3"]').classes()).toContain("active");
    expect(wrapper.get('[data-testid="search-next"]').attributes("disabled")).toBeDefined();

    await wrapper.get(".search-box").setValue("index=audit action=deny");
    global.fetch.mockResolvedValueOnce(jsonResponse({
      mode: "events",
      spl: "index=audit action=deny",
      elapsed_ms: 4,
      events: [],
      time_range: {
        start_time: "2026-06-28T00:00:00+08:00",
        end_time: "2026-06-28T23:59:59+08:00"
      },
      pagination: { limit: 20, offset: 0, page: 1, returned: 0, has_more: false, total: 0 }
    })).mockResolvedValueOnce(jsonResponse({ interval: "hour", buckets: [] }));

    await wrapper.get('[data-testid="search-button"]').trigger("click");
    await flushPromises();

    const finalSearchCalls = global.fetch.mock.calls.filter(([url]) => String(url).startsWith("/api/v1/search?"));
    expect(String(finalSearchCalls.at(-1)[0])).toContain("q=index%3Daudit+action%3Ddeny");
    expect(String(finalSearchCalls.at(-1)[0])).toContain("page=1");
    expect(wrapper.get('[data-testid="search-page-1"]').classes()).toContain("active");
    expect(wrapper.find('[data-testid="search-page-2"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="search-page-3"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="search-page-4"]').exists()).toBe(false);
  });

  it("collapses large search pagination ranges with ellipsis", async () => {
    const wrapper = await mountAuthenticatedApp();

    await wrapper.get('[data-testid="nav-search"]').trigger("click");
    await wrapper.get(".search-box").setValue("index=audit");
    global.fetch.mockResolvedValueOnce(jsonResponse({
      mode: "events",
      spl: "index=audit",
      elapsed_ms: 5,
      events: [{
        event_id: "evt-page-10",
        display: { time: "2026-06-28T10:30:00+08:00", event: "page 10 event", expandable: true },
        detail: { raw: "page 10 event", field_rows: [] },
        raw: "page 10 event"
      }],
      time_range: {
        start_time: "2026-06-28T00:00:00+08:00",
        end_time: "2026-06-28T23:59:59+08:00"
      },
      pagination: { limit: 20, offset: 180, page: 10, returned: 1, has_more: true, total: 400 }
    })).mockResolvedValueOnce(jsonResponse({ interval: "hour", buckets: [] }));

    await wrapper.get('[data-testid="search-button"]').trigger("click");
    await flushPromises();

    const pagination = wrapper.get('[data-testid="search-pagination"]');
    expect(wrapper.find('[data-testid="search-page-1"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="search-page-9"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="search-page-10"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="search-page-11"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="search-page-20"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="search-page-4"]').exists()).toBe(false);
    expect(pagination.text()).toContain("...");
  });
});
