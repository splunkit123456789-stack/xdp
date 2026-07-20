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
  it("loads the enabled input catalog when opening the collect page directly", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi.fn(async (url) => {
      if (url === "/api/v1/auth") return jsonResponse(authStatus());
      if (String(url).startsWith("/api/v1/datasources?")) return jsonResponse({ datasources: [], pagination: { page: 1, page_size: 10, total: 0, total_pages: 1 } });
      if (String(url).startsWith("/api/v1/indexes?")) return jsonResponse({ indexes: [], pagination: { page: 1, page_size: 10, total: 0, total_pages: 1 } });
      if (url === "/api/v1/parser-plugins") return jsonResponse({ plugins: [] });
      if (String(url).startsWith("/api/v1/parse-rules?")) return jsonResponse({ parse_rules: [], pagination: { page: 1, page_size: 10, total: 0, total_pages: 1 } });
      if (url === "/api/v1/search/favorites") return jsonResponse({ saved_searches: [] });
      if (url === "/api/v1/plugins/catalog?plugin_type=input&status=enabled") {
        return jsonResponse({
          plugins: [
            { plugin_code: "syslog", plugin_type: "input", plugin_version: "1.0.0", name: "Syslog", status: "enabled", checksum: "builtin" },
            { plugin_code: "kafka", plugin_type: "input", plugin_version: "1.0.0", name: "Kafka", status: "enabled", checksum: "sha256:kafka", config_schema: { type: "object", properties: {} } }
          ]
        });
      }
      return jsonResponse({});
    });

    const wrapper = mount(App);
    await flushPromises();
    await wrapper.get('[data-testid="show-input-form"]').trigger("click");
    await flushPromises();

    expect(global.fetch).toHaveBeenCalledWith(
      "/api/v1/plugins/catalog?plugin_type=input&status=enabled",
      expect.any(Object)
    );
    expect(global.fetch.mock.calls.some(([url]) => String(url).startsWith("/api/v1/plugins?"))).toBe(false);
    expect(wrapper.get('[data-testid="input-plugin-kafka"]').text()).toContain("Kafka");
  });

  it("retries the input plugin catalog after a direct-load failure", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    let catalogAttempts = 0;
    global.fetch = vi.fn(async (url) => {
      if (url === "/api/v1/auth") return jsonResponse(authStatus());
      if (String(url).startsWith("/api/v1/datasources?")) return jsonResponse({ datasources: [], pagination: { page: 1, page_size: 10, total: 0, total_pages: 1 } });
      if (String(url).startsWith("/api/v1/indexes?")) return jsonResponse({ indexes: [], pagination: { page: 1, page_size: 10, total: 0, total_pages: 1 } });
      if (url === "/api/v1/parser-plugins") return jsonResponse({ plugins: [] });
      if (String(url).startsWith("/api/v1/parse-rules?")) return jsonResponse({ parse_rules: [], pagination: { page: 1, page_size: 10, total: 0, total_pages: 1 } });
      if (url === "/api/v1/search/favorites") return jsonResponse({ saved_searches: [] });
      if (url === "/api/v1/plugins/catalog?plugin_type=input&status=enabled") {
        catalogAttempts += 1;
        if (catalogAttempts === 1) {
          return jsonResponse({ error: { code: "PLUGIN_CATALOG_UNAVAILABLE", message: "采集插件目录加载失败" } }, 503);
        }
        return jsonResponse({
          plugins: [
            { plugin_code: "syslog", plugin_type: "input", plugin_version: "1.0.0", name: "Syslog", status: "enabled", checksum: "builtin" },
            { plugin_code: "kafka", plugin_type: "input", plugin_version: "1.0.0", name: "Kafka", status: "enabled", checksum: "sha256:kafka", config_schema: { type: "object", properties: {} } }
          ]
        });
      }
      return jsonResponse({});
    });

    const wrapper = mount(App);
    await flushPromises();

    expect(wrapper.get('[data-testid="input-plugin-catalog-error"]').text()).toContain("采集插件目录加载失败");
    await wrapper.get('[data-testid="retry-input-plugin-catalog"]').trigger("click");
    await flushPromises();
    await wrapper.get('[data-testid="show-input-form"]').trigger("click");
    await flushPromises();

    expect(catalogAttempts).toBe(2);
    expect(wrapper.find('[data-testid="input-plugin-catalog-error"]').exists()).toBe(false);
    expect(wrapper.get('[data-testid="input-plugin-kafka"]').text()).toContain("Kafka");
  });

  it("locks the input plugin selection while editing an existing datasource", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi.fn(async (url) => {
      if (url === "/api/v1/auth") return jsonResponse(authStatus());
      if (String(url).startsWith("/api/v1/datasources?")) {
        return jsonResponse({
          datasources: [{
            id: "syslog-1",
            name: "Immutable Syslog",
            plugin_code: "syslog",
            status: "disabled",
            plugin_config: {
              collector_port: 5514,
              transport_protocol: "UDP",
              encoding: "UTF-8",
              log_filter_enabled: false
            }
          }],
          pagination: { page: 1, page_size: 10, total: 1, total_pages: 1 }
        });
      }
      if (String(url).startsWith("/api/v1/indexes?")) return jsonResponse({ indexes: [], pagination: { page: 1, page_size: 10, total: 0, total_pages: 1 } });
      if (url === "/api/v1/parser-plugins") return jsonResponse({ plugins: [] });
      if (String(url).startsWith("/api/v1/parse-rules?")) return jsonResponse({ parse_rules: [], pagination: { page: 1, page_size: 10, total: 0, total_pages: 1 } });
      if (url === "/api/v1/search/favorites") return jsonResponse({ saved_searches: [] });
      if (url === "/api/v1/plugins?plugin_type=input&page=1&page_size=10") {
        return jsonResponse({
          plugins: [{ plugin_code: "syslog", plugin_type: "input", plugin_version: "1.0.0", name: "Syslog", status: "enabled", checksum: "builtin" }],
          pagination: { page: 1, page_size: 10, total: 2, total_pages: 1 },
          type_counts: { input: 2, parser: 1, search_command: 1 }
        });
      }
      if (url === "/api/v1/plugins/catalog?plugin_type=input&status=enabled") {
        return jsonResponse({
          plugins: [
            { plugin_code: "syslog", plugin_type: "input", plugin_version: "1.0.0", name: "Syslog", status: "enabled", checksum: "builtin" },
            { plugin_code: "kafka", plugin_type: "input", plugin_version: "1.0.0", name: "Kafka", status: "enabled", checksum: "sha256:kafka", config_schema: { type: "object", properties: {} } }
          ]
        });
      }
      return jsonResponse({});
    });

    const wrapper = mount(App);
    await flushPromises();
    await wrapper.get('[data-testid="nav-plugins"]').trigger("click");
    await flushPromises();
    await wrapper.get('[data-testid="nav-collect"]').trigger("click");
    await flushPromises();
    const editButton = wrapper.findAll("button").find((button) => button.text() === "修改");
    expect(editButton).toBeTruthy();
    await editButton.trigger("click");
    await flushPromises();

    const syslog = wrapper.get('[data-testid="input-plugin-syslog"]');
    const kafka = wrapper.get('[data-testid="input-plugin-kafka"]');
    expect(syslog.attributes("disabled")).toBeDefined();
    expect(kafka.attributes("disabled")).toBeDefined();
    expect(syslog.classes()).toContain("active");

    await kafka.trigger("click");
    expect(syslog.classes()).toContain("active");
    expect(kafka.classes()).not.toContain("active");
  });

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
    expect(collectPage.get(".collect-table").classes()).toContain("align-left-table");
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
    document.body.dispatchEvent(new MouseEvent("pointerdown", { bubbles: true }));
    await flushPromises();
    expect(wrapper.find('[data-testid="input-form-card"]').exists()).toBe(false);

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

  it("renders enabled Kafka plugin schema and saves runtime config", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse({ indexes: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }))
      .mockResolvedValueOnce(jsonResponse({
        plugins: [{
          plugin_code: "kafka",
          plugin_type: "input",
          plugin_version: "1.0.0",
          name: "Kafka Input",
          runtime: "external",
          status: "enabled",
          checksum: "sha256:kafka-demo",
          config_schema: {
            type: "object",
            additionalProperties: false,
            required: ["brokers", "topic", "consumer_group", "start_offset", "security_protocol", "encoding", "log_filter_enabled"],
            properties: {
              brokers: { type: "array", items: { type: "string" }, minItems: 1, title: "Broker 地址" },
              topic: { type: "string", minLength: 1, title: "Topic" },
              consumer_group: { type: "string", minLength: 1, title: "消费组" },
              start_offset: { type: "string", enum: ["from_beginning", "from_tail"], title: "消费策略" },
              security_protocol: { type: "string", enum: ["CUSTOM_PLAINTEXT", "CUSTOM_SSL"], title: "通信协议" },
              encoding: { type: "string", enum: ["UTF-8", "GB18030"], title: "字符编码" },
              log_filter_enabled: { type: "boolean", title: "日志筛选" },
              log_filter_regex: { type: "string", title: "正则筛选", "x-required-if": { field: "log_filter_enabled", equals: true } }
            }
          },
          ui_schema: { order: ["brokers", "topic", "consumer_group", "start_offset", "security_protocol", "encoding", "log_filter_enabled", "log_filter_regex"] }
        }]
      }))
      .mockResolvedValueOnce(jsonResponse({
        id: "kafka-stream-p1",
        code: "kafka-stream-p1",
        type: "kafka",
        name: "Kafka Stream P1",
        status: "active",
        runtime_status: "running",
        listener_status: "consuming",
        listener_endpoint: "kafka://10.0.0.1:9092,10.0.0.2:9092/xdp-events",
        plugin_code: "kafka",
        plugin_runtime: "external",
        internal_raw_topic: "raw.ds_kafka_stream_p1",
        pipeline_id: "pipe_kafka_stream_p1",
        plugin_config: {
          brokers: ["10.0.0.1:9092", "10.0.0.2:9092"],
          topic: "xdp-events",
          consumer_group: "xdp-console",
          start_offset: "from_beginning",
          security_protocol: "CUSTOM_PLAINTEXT",
          encoding: "UTF-8",
          log_filter_enabled: true,
          log_filter_regex: "action=(allow|deny)"
        }
      }));

    const wrapper = mount(App);
    await flushPromises();
    await wrapper.get('[data-testid="show-input-form"]').trigger("click");
    await wrapper.get('[data-testid="input-plugin-kafka"]').trigger("click");
    await flushPromises();

    await wrapper.get('input[placeholder="请输入设备名称"]').setValue("Kafka Stream P1");
    await wrapper.get('[data-testid="kafka-brokers"]').setValue("10.0.0.1:9092,10.0.0.2:9092");
    await wrapper.get('[data-testid="kafka-topic"]').setValue("xdp-events");
    await wrapper.get('[data-testid="kafka-consumer-group"]').setValue("xdp-console");
    expect(wrapper.get('[data-testid="kafka-start-offset"]').text()).toContain("from_beginning");
    expect(wrapper.get('[data-testid="kafka-security-protocol"]').text()).toContain("CUSTOM_PLAINTEXT");
    await wrapper.get('[data-testid="kafka-start-offset"]').setValue("from_beginning");
    await wrapper.get('[data-testid="kafka-security-protocol"]').setValue("CUSTOM_PLAINTEXT");
    await wrapper.get('[data-testid="kafka-encoding"]').setValue("UTF-8");
    await wrapper.get('[data-testid="kafka-log-filter-enabled"]').setValue("on");
    await flushPromises();
    await wrapper.get('[data-testid="kafka-log-filter-regex"]').setValue("action=(allow|deny)");
    await wrapper.get('[data-testid="collect-page"] form').trigger("submit");
    await flushPromises();

    const saveCall = global.fetch.mock.calls.find(([url, options]) => url === "/api/v1/datasources" && options?.method === "POST");
    expect(saveCall).toBeTruthy();
    const body = JSON.parse(saveCall[1].body);
    expect(body).toMatchObject({ name: "Kafka Stream P1", plugin_code: "kafka", status: "active" });
    expect(body.plugin_config).toEqual({
      brokers: ["10.0.0.1:9092", "10.0.0.2:9092"],
      topic: "xdp-events",
      consumer_group: "xdp-console",
      start_offset: "from_beginning",
      security_protocol: "CUSTOM_PLAINTEXT",
      encoding: "UTF-8",
      log_filter_enabled: true,
      log_filter_regex: "action=(allow|deny)"
    });
    expect(wrapper.get('[data-testid="collect-page"]').text()).toContain("Kafka 配置已保存，运行时消费将按状态热加载");
  });

  it("checks Kafka connectivity from the dynamic plugin form", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse({ indexes: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }))
      .mockResolvedValueOnce(jsonResponse({
        plugins: [{
          plugin_code: "kafka",
          plugin_type: "input",
          plugin_version: "1.0.0",
          name: "Kafka Input",
          runtime: "external",
          status: "enabled",
          checksum: "sha256:kafka-demo",
          config_schema: {
            type: "object",
            required: ["brokers", "topic", "consumer_group", "start_offset", "security_protocol", "encoding", "log_filter_enabled"],
            properties: {
              brokers: { type: "array", items: { type: "string" }, minItems: 1, title: "Broker 地址" },
              topic: { type: "string", minLength: 1, title: "Topic" },
              consumer_group: { type: "string", minLength: 1, title: "消费组" },
              start_offset: { type: "string", enum: ["earliest", "latest"], title: "消费策略" },
              security_protocol: { type: "string", enum: ["PLAINTEXT"], title: "通信协议" },
              encoding: { type: "string", enum: ["UTF-8"], title: "字符编码" },
              log_filter_enabled: { type: "boolean", title: "日志筛选" }
            }
          },
          ui_schema: { order: ["brokers", "topic", "consumer_group", "start_offset", "security_protocol", "encoding", "log_filter_enabled"] }
        }]
      }))
      .mockResolvedValueOnce(jsonResponse({ available: true, plugin_code: "kafka", message: "Kafka 连通性正常" }));

    const wrapper = mount(App);
    await flushPromises();
    await wrapper.get('[data-testid="show-input-form"]').trigger("click");
    await wrapper.get('[data-testid="input-plugin-kafka"]').trigger("click");
    await flushPromises();

    await wrapper.get('[data-testid="kafka-brokers"]').setValue("127.0.0.1:9092");
    await wrapper.get('[data-testid="kafka-topic"]').setValue("xdp-events");
    await wrapper.get('[data-testid="kafka-consumer-group"]').setValue("xdp-console");
    await wrapper.get('[data-testid="kafka-connectivity-check"]').trigger("click");
    await flushPromises();

    const checkCall = global.fetch.mock.calls.find(([url, options]) => url === "/api/v1/datasources/connectivity-check" && options?.method === "POST");
    expect(checkCall).toBeTruthy();
    expect(JSON.parse(checkCall[1].body)).toMatchObject({
      plugin_code: "kafka",
      plugin_config: {
        brokers: ["127.0.0.1:9092"],
        topic: "xdp-events",
        consumer_group: "xdp-console"
      }
    });
    expect(wrapper.get('[data-testid="kafka-connectivity-status"]').text()).toContain("Kafka 连通性正常");
  });

  it("renders Kafka runtime as consuming and does not fall back to UDP listener port", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({
        datasources: [{
          id: "kafka-stream-p1",
          type: "kafka",
          name: "Kafka Stream P1",
          status: "active",
          runtime_status: "running",
          listener_status: "consuming",
          listener_endpoint: "kafka://127.0.0.1:9092/xdp-events",
          plugin_code: "kafka",
          plugin_config: { brokers: ["127.0.0.1:9092"], topic: "xdp-events", consumer_group: "xdp-console", start_offset: "earliest", security_protocol: "PLAINTEXT", encoding: "UTF-8", log_filter_enabled: false }
        }],
        pagination: { page: 1, page_size: 10, total: 1, total_pages: 1 }
      }))
      .mockResolvedValueOnce(jsonResponse({ indexes: [] }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }));

    const wrapper = mount(App);
    await flushPromises();

    const row = wrapper.get('[data-testid="collect-row-kafka-stream-p1"]');
    expect(row.text()).toContain("Kafka Stream P1");
    expect(row.text()).toContain("Kafka");
    expect(row.text()).toContain("kafka://127.0.0.1:9092/xdp-events");
    expect(row.text()).toContain("运行中");
    expect(row.text()).not.toContain("UDP:5514");
    expect(row.text()).not.toContain("异常");
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
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
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
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
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
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
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
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
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
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
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
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
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
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
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
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
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
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
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
