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

describe("XDP collection config API integration", () => {
  it("shows collector listener ports in the collect datasource list", async () => {
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
          runtime_status: "running",
          listener_status: "listening",
          listener_endpoint: "udp://0.0.0.0:5514",
          plugin_code: "syslog",
          plugin_config: { collector_port: 5514, transport_protocol: "UDP", encoding: "UTF-8" }
        }],
        pagination: { page: 1, page_size: 10, total: 1, total_pages: 1 }
      }))
      .mockResolvedValueOnce(jsonResponse({ indexes: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }));

    const wrapper = mount(App);
    await flushPromises();

    const collectPage = wrapper.get('[data-testid="collect-page"]');
    expect(collectPage.text()).toContain("监听端口");
    expect(wrapper.get('[data-testid="collect-row-firewall-syslog"]').text()).toContain("UDP:5514");
  });

  it("keeps the collect create form hidden until add is clicked and closes on cancel", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse({ indexes: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }));

    const wrapper = mount(App);
    await flushPromises();

    expect(wrapper.find('[data-testid="input-form-card"]').exists()).toBe(false);
    expect(wrapper.get('[data-testid="collect-page"]').text()).toContain("采集列表");
    await wrapper.get('[data-testid="show-input-form"]').trigger("click");
    await flushPromises();

    expect(wrapper.get('[data-testid="input-form-card"]').text()).toContain("新增采集");
    await wrapper.get('[data-testid="cancel-input-form"]').trigger("click");
    await flushPromises();

    expect(wrapper.find('[data-testid="input-form-card"]').exists()).toBe(false);
  });

  it("does not offer TCP for P0 Syslog runtime configs", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse({ indexes: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }));

    const wrapper = mount(App);
    await flushPromises();
    await wrapper.get('[data-testid="show-input-form"]').trigger("click");
    await flushPromises();
    const syslogOptions = Array.from(wrapper.findAll('select').find((select) => select.text().includes("UDP"))?.findAll("option") || []).map((option) => option.text());
    expect(syslogOptions).toContain("UDP");
    expect(syslogOptions).not.toContain("TCP");
  });

  it("blocks Kafka runtime collection configs in P0 before saving", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse({ indexes: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }));

    const wrapper = mount(App);
    await flushPromises();
    await wrapper.get('[data-testid="show-input-form"]').trigger("click");
    await wrapper.get('[data-testid="input-plugin-kafka"]').trigger("click");
    await flushPromises();

    expect(wrapper.get('[data-testid="kafka-runtime-disabled"]').text()).toContain("P1");
    await wrapper.get('input[placeholder="请输入设备名称"]').setValue("Kafka Stream P1");
    await wrapper.get('[data-testid="collect-page"] form').trigger("submit");
    await flushPromises();

    const saveCall = global.fetch.mock.calls.find(([url, options]) => url === "/api/v1/datasources" && options?.method === "POST");
    expect(saveCall).toBeFalsy();
    expect(wrapper.get('[data-testid="input-form-error"]').text()).toContain("Kafka 采集插件运行时能力未启用");
  });

  it("blocks incomplete Syslog required fields before checking port or saving", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse({ indexes: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }))
      .mockResolvedValueOnce(jsonResponse({ available: true, collector_port: 5514, transport_protocol: "UDP" }))
      .mockResolvedValueOnce(jsonResponse({ id: "bad-syslog", name: "Bad Syslog", plugin_code: "syslog", status: "active", plugin_config: {} }));

    const wrapper = mount(App);
    await flushPromises();
    await wrapper.get('[data-testid="show-input-form"]').trigger("click");
    await flushPromises();
    await wrapper.get('input[placeholder="请输入设备名称"]').setValue("Bad Syslog");
    await wrapper.get('input[placeholder="5514"]').setValue("");
    await wrapper.get('[data-testid="collect-page"] form').trigger("submit");
    await flushPromises();

    const portCheckCall = global.fetch.mock.calls.find(([url, options]) => url === "/api/v1/datasources/port-check" && options?.method === "POST");
    const saveCall = global.fetch.mock.calls.find(([url, options]) => url === "/api/v1/datasources" && options?.method === "POST");
    expect(portCheckCall).toBeFalsy();
    expect(saveCall).toBeFalsy();
    expect(wrapper.get('[data-testid="input-form-error"]').text()).toContain("监听端口为必填项");
  });

  it("submits Syslog configs through /api/v1/datasources without index/parser/raw_topic fields", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse({ indexes: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }))
      .mockResolvedValueOnce(jsonResponse({ available: true, collector_port: 5515, transport_protocol: "UDP" }))
      .mockResolvedValueOnce(jsonResponse({
        id: "firewall-syslog-p0",
        code: "firewall-syslog-p0",
        type: "syslog",
        name: "Firewall Syslog P0",
        status: "active",
        plugin_code: "syslog",
        internal_raw_topic: "raw.ds_firewall_syslog_p0",
        pipeline_id: "pipe_firewall_syslog_p0",
        plugin_config: {
          collector_port: 5515,
          transport_protocol: "UDP",
          encoding: "UTF-8",
          log_filter_enabled: false
        }
      }));

    const wrapper = mount(App);
    await flushPromises();
    await wrapper.get('[data-testid="show-input-form"]').trigger("click");
    await flushPromises();
    expect(wrapper.get('input[placeholder="请输入设备名称"]').exists()).toBe(true);
    expect(wrapper.get('[data-testid="collect-page"]').text()).not.toContain("IP 白名单");
    expect(wrapper.get('[data-testid="collect-page"]').text()).not.toContain("单条数据大小");
    expect(wrapper.get('[data-testid="collect-page"]').text()).not.toContain("保留未命中解析");
    await wrapper.get('input[placeholder="请输入设备名称"]').setValue("Firewall Syslog P0");
    await wrapper.get('input[placeholder="5514"]').setValue("5515");
    await wrapper.get('[data-testid="collect-page"] form').trigger("submit");
    await flushPromises();

    const postCall = global.fetch.mock.calls.find(([url, options]) => url === "/api/v1/datasources" && options?.method === "POST");
    expect(postCall).toBeTruthy();
    expect(postCall[1].headers.Authorization).toBe("Bearer test-token");
    const body = JSON.parse(postCall[1].body);
    expect(body).toMatchObject({ name: "Firewall Syslog P0", plugin_code: "syslog", status: "active" });
    expect(body.plugin_config).toEqual({
      collector_port: 5515,
      transport_protocol: "UDP",
      encoding: "UTF-8",
      log_filter_enabled: false,
      log_filter_regex: ""
    });
    expect(body.plugin_config).not.toHaveProperty("ip_acl");
    expect(body.plugin_config).not.toHaveProperty("keep_unparsed_raw");
    expect(body.plugin_config).not.toHaveProperty("max_event_size_m");
    expect(body).not.toHaveProperty("default_index");
    expect(body).not.toHaveProperty("parser");
    expect(body).not.toHaveProperty("time_field");
    expect(body).not.toHaveProperty("source");
    expect(body).not.toHaveProperty("sourcetype");
    expect(body).not.toHaveProperty("raw_topic");
    expect(body).not.toHaveProperty("internal_raw_topic");
    expect(wrapper.get('[data-testid="collect-page"]').text()).not.toContain("内部路由");
    expect(wrapper.get('[data-testid="collect-page"]').text()).not.toContain("raw.ds_firewall_syslog_p0");
  });

  it("checks Syslog listener port before saving and blocks occupied ports on the page", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse({ indexes: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }))
      .mockResolvedValueOnce(jsonResponse({ error: { code: "LISTENER_PORT_UNAVAILABLE", message: "端口不可用" } }, 409));

    const wrapper = mount(App);
    await flushPromises();
    await wrapper.get('[data-testid="show-input-form"]').trigger("click");
    await flushPromises();
    await wrapper.get('input[placeholder="请输入设备名称"]').setValue("Firewall Syslog P0");
    await wrapper.get('input[placeholder="5514"]').setValue("5515");
    await wrapper.get('[data-testid="collect-page"] form').trigger("submit");
    await flushPromises();

    const portCheckCall = global.fetch.mock.calls.find(([url, options]) => url === "/api/v1/datasources/port-check" && options?.method === "POST");
    expect(portCheckCall).toBeTruthy();
    expect(JSON.parse(portCheckCall[1].body)).toMatchObject({ plugin_code: "syslog", collector_port: 5515, transport_protocol: "UDP" });
    const saveCall = global.fetch.mock.calls.find(([url, options]) => url === "/api/v1/datasources" && options?.method === "POST");
    expect(saveCall).toBeFalsy();
    expect(wrapper.get('[data-testid="collector-port-error"]').text()).toContain("端口不可用");
  });

  it("blocks duplicate datasource names before creating Syslog configs", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({
        datasources: [{
          id: "firewall-syslog-p0",
          type: "syslog",
          name: "Firewall Syslog P0",
          status: "active",
          plugin_code: "syslog",
          plugin_config: { collector_port: 5514, transport_protocol: "UDP", encoding: "UTF-8" }
        }]
      }))
      .mockResolvedValueOnce(jsonResponse({ indexes: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }));

    const wrapper = mount(App);
    await flushPromises();
    await wrapper.get('[data-testid="show-input-form"]').trigger("click");
    await flushPromises();
    await wrapper.get('input[placeholder="请输入设备名称"]').setValue(" Firewall Syslog P0 ");
    await wrapper.get('input[placeholder="5514"]').setValue("5515");
    await wrapper.get('[data-testid="collect-page"] form').trigger("submit");
    await flushPromises();

    const portCheckCall = global.fetch.mock.calls.find(([url, options]) => url === "/api/v1/datasources/port-check" && options?.method === "POST");
    const saveCall = global.fetch.mock.calls.find(([url, options]) => url === "/api/v1/datasources" && options?.method === "POST");
    expect(portCheckCall).toBeFalsy();
    expect(saveCall).toBeFalsy();
    expect(wrapper.get('[data-testid="input-name-error"]').text()).toContain("设备名称已存在");
  });

  it("allows reusing a datasource name after deleting the old config", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({
        datasources: [{
          id: "reusable-syslog",
          type: "syslog",
          name: "Reusable Syslog",
          status: "disabled",
          plugin_code: "syslog",
          plugin_config: { collector_port: 5515, transport_protocol: "UDP", encoding: "UTF-8" }
        }]
      }))
      .mockResolvedValueOnce(jsonResponse({ indexes: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }))
      .mockResolvedValueOnce(jsonResponse({ status: "deleted", id: "reusable-syslog" }))
      .mockResolvedValueOnce(jsonResponse({ available: true, collector_port: 5516, transport_protocol: "UDP" }))
      .mockResolvedValueOnce(jsonResponse({
        id: "reusable-syslog",
        type: "syslog",
        name: "Reusable Syslog",
        status: "disabled",
        plugin_code: "syslog",
        plugin_config: { collector_port: 5516, transport_protocol: "UDP", encoding: "UTF-8" }
      }));

    const wrapper = mount(App);
    await flushPromises();
    await wrapper.findAll(".link-btn.delete")[0].trigger("click");
    await flushPromises();
    await wrapper.get('[data-testid="show-input-form"]').trigger("click");
    await flushPromises();
    await wrapper.get('input[placeholder="请输入设备名称"]').setValue(" Reusable Syslog ");
    await wrapper.get('input[placeholder="5514"]').setValue("5516");
    await wrapper.get('[data-testid="collect-page"] form').trigger("submit");
    await flushPromises();

    const saveCall = global.fetch.mock.calls.find(([url, options]) => url === "/api/v1/datasources" && options?.method === "POST");
    expect(saveCall).toBeTruthy();
    expect(JSON.parse(saveCall[1].body)).toMatchObject({ name: " Reusable Syslog ", plugin_code: "syslog" });
    expect(wrapper.get('[data-testid="collect-page"]').text()).toContain("Reusable Syslog");
    expect(wrapper.find('[data-testid="input-name-error"]').exists()).toBe(false);
  });

  it("shows only runtime status in datasource list and loads runtime details on demand", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({
        datasources: [
          {
            id: "running-syslog",
            code: "running-syslog",
            type: "syslog",
            name: "Running Syslog",
            status: "active",
            runtime_status: "running",
            listener_status: "listening",
            listener_endpoint: "udp://0.0.0.0:5514",
            plugin_code: "syslog",
            plugin_config: { collector_port: 5514, transport_protocol: "UDP", encoding: "UTF-8" }
          },
          {
            id: "stopped-syslog",
            code: "stopped-syslog",
            type: "syslog",
            name: "Stopped Syslog",
            status: "disabled",
            runtime_status: "stopped",
            listener_status: "stopped",
            listener_endpoint: "udp://0.0.0.0:5515",
            plugin_code: "syslog",
            plugin_config: { collector_port: 5515, transport_protocol: "UDP", encoding: "UTF-8" }
          },
          {
            id: "failed-syslog",
            code: "failed-syslog",
            type: "syslog",
            name: "Failed Syslog",
            status: "active",
            runtime_status: "failed",
            listener_status: "error",
            listener_endpoint: "udp://0.0.0.0:5516",
            plugin_code: "syslog",
            plugin_config: { collector_port: 5516, transport_protocol: "UDP", encoding: "UTF-8" }
          }
        ]
      }))
      .mockResolvedValueOnce(jsonResponse({ indexes: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }))
      .mockResolvedValueOnce(jsonResponse({
        id: "running-syslog",
        name: "Running Syslog",
        plugin_code: "syslog",
        desired_status: "active",
        runtime_status: "running",
        listener_status: "listening",
        listener_endpoint: "udp://0.0.0.0:5514",
        endpoint: "udp://0.0.0.0:5514",
        protocol: "udp",
        port: 5514,
        agent_id: "local-agent",
        pipeline_id: "pipe_running_syslog",
        config_version: 3,
        last_loaded_at: "2026-06-30T08:00:00Z",
        last_heartbeat_at: "2026-06-30T08:01:00Z",
        last_received_at: "2026-06-30T08:02:00Z",
        received_events_total: 42,
        received_bytes_total: 20480,
        last_error_code: "",
        last_error: "",
        parse_rule_name: "Firewall Regex",
        sourcetype: "Firewall Regex",
        output_index: "audit"
      }));

    const wrapper = mount(App);
    await flushPromises();

    expect(global.fetch.mock.calls.some(([url]) => url === "/api/v1/datasources/running-syslog/runtime")).toBe(false);
    expect(wrapper.find('[data-testid="collect-runtime-detail"]').exists()).toBe(false);

    const runningRow = wrapper.get('[data-testid="collect-row-running-syslog"]');
    expect(wrapper.get('[data-testid="collect-expand-running-syslog"]').text()).toBe("▶");
    expect(runningRow.text()).toContain("运行中");
    expect(runningRow.text()).toContain("停止");
    expect(runningRow.text()).not.toContain("启动");
    expect(runningRow.text()).not.toContain("active");
    expect(runningRow.text()).not.toContain("listening");

    const stoppedRow = wrapper.get('[data-testid="collect-row-stopped-syslog"]');
    expect(stoppedRow.text()).toContain("已停止");
    expect(wrapper.get('[data-testid="toggle-input-stopped-syslog"]').text()).toBe("启动");
    expect(stoppedRow.text()).not.toContain("disabled");

    const failedRow = wrapper.get('[data-testid="collect-row-failed-syslog"]');
    expect(failedRow.text()).toContain("异常");
    expect(failedRow.text()).toContain("查看状态");
    expect(failedRow.text()).toContain("重试");
    expect(failedRow.text()).not.toContain("启动");
    expect(failedRow.text()).not.toContain("停止");

    await wrapper.get('[data-testid="collect-expand-running-syslog"]').trigger("click");
    await flushPromises();

    const runtimeCall = global.fetch.mock.calls.find(([url]) => url === "/api/v1/datasources/running-syslog/runtime");
    expect(runtimeCall).toBeTruthy();
    expect(wrapper.get('[data-testid="collect-expand-running-syslog"]').text()).toBe("▼");
    const detail = wrapper.get('[data-testid="collect-runtime-detail-running-syslog"]');
    expect(detail.text()).toContain("Agent 心跳");
    expect(detail.text()).toContain("local-agent");
    expect(detail.text()).toContain("listener 状态");
    expect(detail.text()).toContain("累计接收事件数");
    expect(detail.text()).toContain("42");
    expect(detail.text()).toContain("累计字节数");
    expect(detail.text()).toContain("20,480");
    expect(detail.text()).toContain("配置版本");
    expect(detail.text()).toContain("3");
    expect(detail.text()).toContain("链路拓扑");
    expect(detail.text()).toContain("local-agent -> udp://0.0.0.0:5514 -> Firewall Regex -> audit");
    expect(detail.text()).not.toContain("pipe_running_syslog");

    await runningRow.trigger("click");
    await flushPromises();
    expect(wrapper.get('[data-testid="collect-expand-running-syslog"]').text()).toBe("▶");
    expect(wrapper.find('[data-testid="collect-runtime-detail-running-syslog"]').exists()).toBe(false);
  });

  it("falls back to loaded parse rules when runtime topology fields are missing", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({
        datasources: [{
          id: "firewall-syslog",
          code: "firewall-syslog",
          type: "syslog",
          name: "Firewall Syslog",
          status: "active",
          runtime_status: "running",
          listener_status: "listening",
          listener_endpoint: "udp://0.0.0.0:5514",
          plugin_code: "syslog",
          plugin_config: { collector_port: 5514, transport_protocol: "UDP", encoding: "UTF-8" }
        }]
      }))
      .mockResolvedValueOnce(jsonResponse({ indexes: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({
        parse_rules: [{
          id: "rule-firewall",
          name: "Firewall Regex",
          status: "active",
          parser_plugin: "regex",
          data_source_name: "Firewall Syslog",
          output_index: "audit",
          props_conf: "[source::firewall]"
        }]
      }))
      .mockResolvedValueOnce(jsonResponse({
        id: "firewall-syslog",
        name: "Firewall Syslog",
        plugin_code: "syslog",
        desired_status: "active",
        runtime_status: "running",
        listener_status: "listening",
        endpoint: "udp://0.0.0.0:5514",
        agent_id: "local-agent",
        pipeline_id: "pipe_firewall_syslog",
        config_version: 1
      }));

    const wrapper = mount(App);
    await flushPromises();
    await wrapper.get('[data-testid="collect-row-firewall-syslog"]').trigger("click");
    await flushPromises();

    const detail = wrapper.get('[data-testid="collect-runtime-detail"]');
    expect(detail.text()).toContain("local-agent -> udp://0.0.0.0:5514 -> Firewall Regex -> audit");
    expect(detail.text()).not.toContain("未绑定解析规则");
    expect(detail.text()).not.toContain("未指定 index");
    expect(detail.text()).not.toContain("pipe_firewall_syslog");
  });

  it("shows unspecified index when runtime topology is unbound even if backend returns legacy app", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({
        datasources: [{
          id: "unbound-syslog",
          code: "unbound-syslog",
          type: "syslog",
          name: "Unbound Syslog",
          status: "active",
          runtime_status: "running",
          listener_status: "listening",
          listener_endpoint: "udp://0.0.0.0:5517",
          plugin_code: "syslog",
          plugin_config: { collector_port: 5517, transport_protocol: "UDP", encoding: "UTF-8" }
        }]
      }))
      .mockResolvedValueOnce(jsonResponse({ indexes: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }))
      .mockResolvedValueOnce(jsonResponse({
        id: "unbound-syslog",
        name: "Unbound Syslog",
        plugin_code: "syslog",
        runtime_status: "running",
        listener_status: "listening",
        endpoint: "udp://0.0.0.0:5517",
        agent_id: "local-agent",
        parse_rule_name: "未绑定解析规则",
        sourcetype: "未绑定解析规则",
        output_index: "app"
      }));

    const wrapper = mount(App);
    await flushPromises();
    await wrapper.get('[data-testid="collect-expand-unbound-syslog"]').trigger("click");
    await flushPromises();

    const detail = wrapper.get('[data-testid="collect-runtime-detail-unbound-syslog"]');
    expect(detail.text()).toContain("local-agent -> udp://0.0.0.0:5517 -> 未绑定解析规则 -> 未指定 index");
    expect(detail.text()).not.toContain("-> app");
  });

  it("toggles Syslog datasource runtime through mutually exclusive actions", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({
        datasources: [{
          id: "firewall-syslog-p0",
          code: "firewall-syslog-p0",
          type: "syslog",
          name: "Firewall Syslog P0",
          status: "active",
          runtime_status: "running",
          listener_status: "listening",
          listener_endpoint: "udp://0.0.0.0:5514",
          plugin_code: "syslog",
          plugin_config: { collector_port: 5514, transport_protocol: "UDP", encoding: "UTF-8" }
        }]
      }))
      .mockResolvedValueOnce(jsonResponse({ indexes: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }))
      .mockResolvedValueOnce(jsonResponse({
        id: "firewall-syslog-p0",
        type: "syslog",
        name: "Firewall Syslog P0",
        status: "disabled",
        runtime_status: "stopped",
        listener_status: "stopped",
        listener_endpoint: "udp://0.0.0.0:5514",
        plugin_code: "syslog",
        plugin_config: { collector_port: 5514, transport_protocol: "UDP", encoding: "UTF-8" }
      }));

    const wrapper = mount(App);
    await flushPromises();

    const row = wrapper.get('[data-testid="collect-row-firewall-syslog-p0"]');
    expect(row.text()).toContain("运行中");
    expect(row.text()).toContain("停止");
    expect(row.text()).not.toContain("启动");
    await wrapper.get('[data-testid="toggle-input-firewall-syslog-p0"]').trigger("click");
    await flushPromises();

    const patchCall = global.fetch.mock.calls.find(([url, options]) => url === "/api/v1/datasources/firewall-syslog-p0/status" && options?.method === "PATCH");
    expect(patchCall).toBeTruthy();
    expect(patchCall[1].headers.Authorization).toBe("Bearer test-token");
    expect(JSON.parse(patchCall[1].body)).toEqual({ status: "disabled" });
    const updatedRow = wrapper.get('[data-testid="collect-row-firewall-syslog-p0"]');
    expect(updatedRow.text()).toContain("已停止");
    expect(wrapper.get('[data-testid="toggle-input-firewall-syslog-p0"]').text()).toBe("启动");
  });

  it("paginates the collect datasource list through API page params", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({
        datasources: [{
          id: "syslog-01",
          type: "syslog",
          name: "Syslog 01",
          status: "active",
          runtime_status: "running",
          listener_status: "listening",
          plugin_code: "syslog",
          plugin_config: { collector_port: 5514, transport_protocol: "UDP", encoding: "UTF-8" }
        }],
        pagination: { page: 1, page_size: 10, total: 42, total_pages: 5 }
      }))
      .mockResolvedValueOnce(jsonResponse({ indexes: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }))
      .mockResolvedValueOnce(jsonResponse({
        datasources: [{
          id: "syslog-11",
          type: "syslog",
          name: "Syslog 11",
          status: "active",
          runtime_status: "running",
          listener_status: "listening",
          plugin_code: "syslog",
          plugin_config: { collector_port: 5534, transport_protocol: "UDP", encoding: "UTF-8" }
        }],
        pagination: { page: 2, page_size: 10, total: 42, total_pages: 5 }
      }));

    const wrapper = mount(App);
    await flushPromises();

    expect(wrapper.get('[data-testid="collect-pagination"]').text()).toContain("1");
    expect(wrapper.get('[data-testid="collect-pagination"]').text()).toContain("3");
    expect(wrapper.get('[data-testid="collect-page-size"]').element.value).toBe("10");

    await wrapper.get('[data-testid="collect-next"]').trigger("click");
    await flushPromises();

    const pageCall = global.fetch.mock.calls.find(([url]) => String(url).startsWith("/api/v1/datasources?") && String(url).includes("page=2") && String(url).includes("page_size=10"));
    expect(pageCall).toBeTruthy();
    expect(wrapper.get('[data-testid="collect-page"]').text()).toContain("Syslog 11");
  });
});
