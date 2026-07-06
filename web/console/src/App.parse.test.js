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

function authStatus() {
  return {
    enabled: true,
    login_required: true,
    authenticated: true,
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

describe("XDP parse config API integration", () => {
  it("keeps the parse create form hidden until add is clicked and closes on cancel", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse({ indexes: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }));

    const wrapper = mount(App);
    await flushPromises();
    await wrapper.get('[data-testid="nav-parse"]').trigger("click");
    await flushPromises();

    expect(wrapper.find('[data-testid="rule-form-card"]').exists()).toBe(false);
    expect(wrapper.get('[data-testid="parse-page"]').text()).toContain("规则列表");
    await wrapper.get('[data-testid="show-rule-form"]').trigger("click");
    await flushPromises();

    expect(wrapper.get('[data-testid="rule-form-card"]').text()).toContain("新增规则");
    await wrapper.get('[data-testid="cancel-rule-form"]').trigger("click");
    await flushPromises();

    expect(wrapper.find('[data-testid="rule-form-card"]').exists()).toBe(false);
  });

  it("blocks incomplete regex rules before saving parse config", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({
        datasources: [{
          id: "firewall-syslog",
          type: "syslog",
          name: "Firewall Syslog",
          status: "active",
          plugin_code: "syslog",
          internal_raw_topic: "raw.ds_firewall_syslog",
          plugin_config: { collector_port: 5514, transport_protocol: "UDP", encoding: "UTF-8" }
        }]
      }))
      .mockResolvedValueOnce(jsonResponse({
        indexes: [{ index_name: "audit", name: "audit", ttl_days: 30, rows: 0, status: "active" }]
      }))
      .mockResolvedValueOnce(jsonResponse({
        plugins: [{ plugin_code: "regex", display_name: "正则解析插件", runtime_capabilities: { preview: true } }]
      }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }))
      .mockResolvedValueOnce(jsonResponse({
        id: "bad-rule",
        name: "Bad Rule",
        parser_plugin: "regex",
        data_source_name: "",
        output_index: "audit",
        props_conf: ""
      }));

    const wrapper = mount(App);
    await flushPromises();
    await wrapper.get('[data-testid="nav-parse"]').trigger("click");
    await flushPromises();
    await wrapper.get('[data-testid="show-rule-form"]').trigger("click");
    await flushPromises();

    await wrapper.get('[data-testid="rule-name"]').setValue("Bad Rule");
    await wrapper.get('[data-testid="rule-output-index"]').setValue("audit");
    await wrapper.get('[data-testid="sample-log"]').setValue("src=1.1.1.1");
    await wrapper.get('[data-testid="regex-pattern"]').setValue("src=(?<src_ip>\\S+)");
    await wrapper.get('[data-testid="parse-page"] form').trigger("submit");
    await flushPromises();

    const postCall = global.fetch.mock.calls.find(([url, options]) => url === "/api/v1/parse-rules" && options?.method === "POST");
    expect(postCall).toBeFalsy();
    expect(wrapper.get('[data-testid="parse-form-error"]').text()).toContain("关联采集数据源名称为必填项");
  });

  it("loads parser plugins, previews regex parsing, and saves props.conf through parse-rules API", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({
        datasources: [{
          id: "firewall-syslog",
          type: "syslog",
          name: "Firewall Syslog",
          status: "active",
          plugin_code: "syslog",
          internal_raw_topic: "raw.ds_firewall_syslog",
          plugin_config: { collector_port: 5514, transport_protocol: "UDP", encoding: "UTF-8" }
        }]
      }))
      .mockResolvedValueOnce(jsonResponse({
        indexes: [
          { index_name: "app", name: "app", ttl_days: 30, rows: 0, status: "active" },
          { index_name: "audit", name: "audit", ttl_days: 30, rows: 0, status: "active" },
          { index_name: "_unparsed", name: "_unparsed", ttl_days: 30, rows: 0, status: "active", system: true, index_type: "system" }
        ]
      }))
      .mockResolvedValueOnce(jsonResponse({
        plugins: [
          { plugin_code: "json", display_name: "JSON 解析插件", runtime_capabilities: { preview: true } },
          { plugin_code: "kv", display_name: "KV 解析插件", runtime_capabilities: { preview: true } },
          { plugin_code: "delimited", display_name: "分隔符解析插件", runtime_capabilities: { preview: true } },
          {
            plugin_code: "regex",
            plugin_type: "parser",
            display_name: "正则解析插件",
            version: "1.0.0",
            runtime: "go_builtin",
            status: "active",
            schema_summary: { required: ["sample_event", "regex_pattern", "props_conf"] },
            runtime_capabilities: { preview: true, ingest_parse: true, hot_reload: true, props_conf_generate: true }
          }
        ]
      }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({
        success: true,
        fields: [
          { field: "src_ip", value: "1.1.1.1", type: "string" },
          { field: "bytes", value: "1024", type: "number" }
        ]
      }))
      .mockResolvedValueOnce(jsonResponse({
        id: "pr_firewall_regex",
        name: "Firewall Regex",
        parser_plugin: "regex",
        data_source_name: "Firewall Syslog",
        input_route: "raw.ds_firewall_syslog",
        stage: "ingest",
        status: "active",
        output_index: "audit",
        priority: 20,
        plugin_config: { regex_pattern: "src=(?<src_ip>\\S+) bytes=(?<bytes>\\d+)" },
        props_conf: "[source::firewall-regex]\nEXTRACT-custom = src=(?<src_ip>\\S+) bytes=(?<bytes>\\d+)",
        hot_fields: [
          { name: "src_ip", type: "string", searchable: true, aggregatable: true, aliases: ["src"] },
          { name: "bytes", type: "uint64", searchable: false, aggregatable: true, aliases: [] }
        ]
      }));

    const wrapper = mount(App);
    await flushPromises();
    await wrapper.get('[data-testid="nav-parse"]').trigger("click");
    await flushPromises();

    expect(global.fetch).toHaveBeenCalledWith("/api/v1/parser-plugins", expect.objectContaining({ headers: expect.objectContaining({ Authorization: "Bearer test-token" }) }));
    expect(wrapper.get('[data-testid="parse-page"]').text()).toContain("正则");
    expect(wrapper.get('[data-testid="parse-page"]').text()).toContain("Firewall Syslog");
    expect(wrapper.get('[data-testid="parse-page"]').text()).not.toContain("raw.ds_firewall_syslog");
    expect(wrapper.find('[data-testid="rule-form-card"]').exists()).toBe(false);
    await wrapper.get('[data-testid="show-rule-form"]').trigger("click");
    await flushPromises();

    expect(wrapper.get('[data-testid="rule-name"]').attributes("placeholder")).toBe("请输入解析规则名称");
    expect(wrapper.get('[data-testid="rule-source"] option').text()).toBe("请选择采集数据源");
    expect(wrapper.get('[data-testid="parser-regex"]').classes()).toContain("active");
    expect(wrapper.find('[data-testid="sync-props"]').exists()).toBe(false);
    expect(wrapper.get('[data-testid="rule-output-index"]').text()).toContain("audit");
    expect(wrapper.get('[data-testid="rule-output-index"]').text()).not.toContain("_unparsed");
    expect(wrapper.get('[data-testid="rule-output-index"]').text()).not.toContain("events_audit");
    expect(wrapper.get('[data-testid="rule-priority"]').element.value).toBe("100");

    await wrapper.get('[data-testid="rule-name"]').setValue("Firewall Regex");
    await wrapper.get('[data-testid="rule-source"]').setValue("Firewall Syslog");
    await wrapper.get('[data-testid="rule-output-index"]').setValue("audit");
    await wrapper.get('[data-testid="rule-priority"]').setValue("20");
    await wrapper.get('[data-testid="sample-log"]').setValue("src=1.1.1.1 bytes=1024");
    await wrapper.get('[data-testid="regex-pattern"]').setValue("src=(?<src_ip>\\S+) bytes=(?<bytes>\\d+)");
    expect(wrapper.get('[data-testid="props-conf"]').element.value).toContain("EXTRACT-custom = src=(?<src_ip>\\S+) bytes=(?<bytes>\\d+)");
    await wrapper.get('[data-testid="preview-parse"]').trigger("click");
    await flushPromises();

    expect(wrapper.get('[data-testid="parse-preview"]').text()).toContain("src_ip");
    expect(wrapper.find('[data-testid="hot-fields-panel"]').exists()).toBe(false);
    expect(wrapper.get('[data-testid="parse-page"]').text()).not.toContain("字段优化");
    expect(wrapper.get('[data-testid="parse-page"]').text()).not.toContain("后台自动");
    expect(wrapper.get('[data-testid="parse-page"]').text()).not.toContain("可检索");
    expect(wrapper.get('[data-testid="parse-page"]').text()).not.toContain("可统计");
    const previewCall = global.fetch.mock.calls.find(([url]) => url === "/api/v1/parse-rules/preview/test");
    expect(previewCall).toBeTruthy();

    await wrapper.get('[data-testid="parse-page"] form').trigger("submit");
    await flushPromises();

    const postCall = global.fetch.mock.calls.find(([url, options]) => url === "/api/v1/parse-rules" && options?.method === "POST");
    expect(postCall).toBeTruthy();
    expect(postCall[1].headers.Authorization).toBe("Bearer test-token");
    const body = JSON.parse(postCall[1].body);
    expect(body).toMatchObject({
      name: "Firewall Regex",
      parser_plugin: "regex",
      input_route: "raw.ds_firewall_syslog",
      output_index: "audit",
      priority: 20,
      stage: "ingest",
      status: "active"
    });
    expect(body.plugin_config.regex_pattern).toBe("src=(?<src_ip>\\S+) bytes=(?<bytes>\\d+)");
    expect(body.plugin_config).toMatchObject({
      source_field: "raw",
      target: "fields",
      on_no_match: "continue"
    });
    expect(body).not.toHaveProperty("hot_fields");
    expect(body).not.toHaveProperty("source");
    expect(body).not.toHaveProperty("sourcetype");
    expect(body.props_conf).toContain("EXTRACT-custom");
    expect(body.props_conf).not.toContain("XDP_INPUT_ROUTE");
    expect(body).not.toHaveProperty("time_field");
    expect(wrapper.get('[data-testid="parse-page"]').text()).toContain("Firewall Regex");
    expect(wrapper.get('[data-testid="parse-page"]').text()).toContain("audit");
    expect(wrapper.get('[data-testid="parse-page"]').text()).toContain("20");
    expect(wrapper.get('[data-testid="parse-page"]').text()).not.toContain("events_audit");
    expect(wrapper.get('[data-testid="parse-page"]').text()).toContain("EXTRACT-custom");
  });

  it("paginates the parse rules list through API page params", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse({
        indexes: [{ index_name: "audit", name: "audit", ttl_days: 30, rows: 0, status: "active" }]
      }))
      .mockResolvedValueOnce(jsonResponse({
        plugins: [{ plugin_code: "regex", display_name: "正则解析插件", runtime_capabilities: { preview: true } }]
      }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({
        parse_rules: [{
          id: "rule-01",
          name: "Rule 01",
          parser_plugin: "regex",
          data_source_name: "Syslog 01",
          output_index: "audit",
          priority: 10,
          props_conf: "[source::rule-01]"
        }],
        pagination: { page: 1, page_size: 10, total: 41, total_pages: 5 }
      }))
      .mockResolvedValueOnce(jsonResponse({
        parse_rules: [{
          id: "rule-11",
          name: "Rule 11",
          parser_plugin: "regex",
          data_source_name: "Syslog 11",
          output_index: "audit",
          priority: 20,
          props_conf: "[source::rule-21]"
        }],
        pagination: { page: 2, page_size: 10, total: 41, total_pages: 5 }
      }));

    const wrapper = mount(App);
    await flushPromises();
    await wrapper.get('[data-testid="nav-parse"]').trigger("click");
    await flushPromises();

    expect(wrapper.get('[data-testid="parse-pagination"]').text()).toContain("3");
    await wrapper.get('[data-testid="parse-next"]').trigger("click");
    await flushPromises();

    const pageCall = global.fetch.mock.calls.find(([url]) => String(url).startsWith("/api/v1/parse-rules?") && String(url).includes("page=2") && String(url).includes("page_size=10"));
    expect(pageCall).toBeTruthy();
    expect(wrapper.get('[data-testid="parse-page"]').text()).toContain("Rule 11");
  });
});
