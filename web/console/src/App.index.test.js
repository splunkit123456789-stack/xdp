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

function indexPayload(items) {
  return {
    indexes: items.map((item) => ({
      index_name: item.name,
      name: item.name,
      table_name: `events_${item.name}`,
      rows: item.rows ?? 0,
      ttl_days: item.ttl ?? 30,
      physical_ttl_days: item.physicalTtl ?? item.ttl ?? 30,
      storage_bytes: item.storageBytes ?? 0,
      latest_event_time: item.latestEventTime ?? "",
      storage: "clickhouse",
      status: item.status ?? "active",
      configured: true
    }))
  };
}

function routeDefaultIndexMocks(responses = {}) {
  return vi.fn(async (url, options = {}) => {
    const path = String(url);
    if (path === "/api/v1/auth") return jsonResponse(responses.auth || authStatus());
    if (path.startsWith("/api/v1/datasources")) return jsonResponse(responses.datasources || { datasources: [] });
    if (path.startsWith("/api/v1/search/favorites")) return jsonResponse(responses.savedSearches || { saved_searches: [] });
    if (path.startsWith("/api/v1/parse-rules")) return jsonResponse(responses.parseRules || { parse_rules: [] });
    if (path.startsWith("/api/v1/parser-plugins")) return jsonResponse(responses.parserPlugins || { plugins: [] });
    if (path === "/api/v1/writer/runtime") return jsonResponse(responses.writerRuntime || { status: "unknown" });
    if (path.startsWith("/api/v1/plugins")) return jsonResponse(responses.plugins || { plugins: [] });
    if (responses.onIndexes) {
      const matched = await responses.onIndexes(path, options);
      if (matched) return matched;
    }
    if (path.startsWith("/api/v1/indexes")) return jsonResponse(responses.indexes || indexPayload([]));
    return jsonResponse({});
  });
}

beforeEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("XDP index config API integration", () => {
  it("keeps the index create form hidden until add is clicked and closes on cancel", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse(indexPayload([{ name: "app" }])))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }));

    const wrapper = mount(App);
    await flushPromises();
    await wrapper.get('[data-testid="nav-index"]').trigger("click");
    await flushPromises();

    expect(wrapper.find('[data-testid="index-form-card"]').exists()).toBe(false);
    expect(wrapper.get('[data-testid="index-page"]').text()).toContain("索引列表");
    await wrapper.get('[data-testid="show-index-form"]').trigger("click");
    await flushPromises();

    expect(wrapper.get('[data-testid="index-form-card"]').text()).toContain("新增索引");
    await wrapper.get('[data-testid="cancel-index-form"]').trigger("click");
    await flushPromises();

    expect(wrapper.find('[data-testid="index-form-card"]').exists()).toBe(false);
  });

  it("shows an empty index name field with the configured placeholder", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse(indexPayload([{ name: "app" }])))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }));

    const wrapper = mount(App);
    await flushPromises();
    await wrapper.get('[data-testid="nav-index"]').trigger("click");
    await flushPromises();
    await wrapper.get('[data-testid="show-index-form"]').trigger("click");
    await flushPromises();

    const input = wrapper.get('[data-testid="index-name"]').element;
    expect(input.value).toBe("");
    expect(input.getAttribute("placeholder")).toBe("请输入index名称");
  });

  it("persists a new index through /api/v1/indexes and reloads it after refresh", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    let created = false;
    global.fetch = routeDefaultIndexMocks({
      onIndexes: async (path, options = {}) => {
        if (path === "/api/v1/indexes" && options.method === "POST") {
          created = true;
          return jsonResponse({
            index_name: "audit_p0",
            name: "audit_p0",
            table_name: "events_audit_p0",
            rows: 0,
            ttl_days: 30,
            storage: "clickhouse",
            status: "active",
            configured: true
          });
        }
        if (path.startsWith("/api/v1/indexes")) {
          return jsonResponse(indexPayload(created ? [{ name: "app" }, { name: "firewall" }, { name: "audit_p0" }] : [{ name: "app" }, { name: "firewall" }]));
        }
        return null;
      }
    });

    const wrapper = mount(App);
    await flushPromises();

    await wrapper.get('[data-testid="nav-index"]').trigger("click");
    await flushPromises();
    expect(wrapper.get('[data-testid="index-page"]').text()).not.toContain("audit_p0");

    await wrapper.get('[data-testid="show-index-form"]').trigger("click");
    await flushPromises();
    await wrapper.get('[data-testid="index-name"]').setValue("audit_p0");
    await wrapper.get('[data-testid="index-page"] form').trigger("submit");
    await flushPromises();

    const postCall = global.fetch.mock.calls.find(([url, options]) => url === "/api/v1/indexes" && options?.method === "POST");
    expect(postCall).toBeTruthy();
    expect(postCall[1].headers.Authorization).toBe("Bearer test-token");
    expect(JSON.parse(postCall[1].body)).toMatchObject({
      index_name: "audit_p0",
      ttl_days: 30
    });
    expect(wrapper.get('[data-testid="index-page"]').text()).toContain("audit_p0");
    expect(wrapper.get('[data-testid="index-page"]').text()).toContain("events_audit_p0");

    vi.restoreAllMocks();
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = routeDefaultIndexMocks({ indexes: indexPayload([{ name: "app" }, { name: "firewall" }, { name: "audit_p0" }]) });

    const refreshed = mount(App);
    await flushPromises();
    await refreshed.get('[data-testid="nav-index"]').trigger("click");
    await flushPromises();

    expect(refreshed.get('[data-testid="index-page"]').text()).toContain("audit_p0");
    expect(refreshed.get('[data-testid="index-page"]').text()).toContain("events_audit_p0");
  });

  it("deletes an index with storage drop so it does not reappear after refresh", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    let deleted = false;
    global.fetch = routeDefaultIndexMocks({
      onIndexes: async (path, options = {}) => {
        if (path.startsWith("/api/v1/indexes?") && options.method === "DELETE") {
          deleted = true;
          return jsonResponse({ status: "deleted", index_name: "audit_p0" });
        }
        if (path.startsWith("/api/v1/indexes")) {
          return jsonResponse(indexPayload(deleted ? [{ name: "app" }] : [{ name: "app" }, { name: "audit_p0" }]));
        }
        return null;
      }
    });

    const wrapper = mount(App);
    await flushPromises();

    await wrapper.get('[data-testid="nav-index"]').trigger("click");
    await flushPromises();
    expect(wrapper.get('[data-testid="index-page"]').text()).toContain("audit_p0");

    const rows = wrapper.findAll('[data-testid="index-page"] tbody tr');
    const auditRow = rows.find((row) => row.text().includes("audit_p0"));
    expect(auditRow).toBeTruthy();
    await auditRow.find(".link-btn.delete").trigger("click");
    await flushPromises();

    const deleteCall = global.fetch.mock.calls.find(([url, options]) => String(url).startsWith("/api/v1/indexes?") && options?.method === "DELETE");
    expect(deleteCall).toBeTruthy();
    expect(String(deleteCall[0])).toContain("index=audit_p0");
    expect(String(deleteCall[0])).toContain("drop_storage=true");
    expect(wrapper.get('[data-testid="index-page"]').text()).not.toContain("audit_p0");

    vi.restoreAllMocks();
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = routeDefaultIndexMocks({ indexes: indexPayload([{ name: "app" }]) });

    const refreshed = mount(App);
    await flushPromises();
    await refreshed.get('[data-testid="nav-index"]').trigger("click");
    await flushPromises();

    expect(refreshed.get('[data-testid="index-page"]').text()).not.toContain("audit_p0");
  });

  it("blocks index submit when required TTL is empty", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse(indexPayload([{ name: "app" }])))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }));

    const wrapper = mount(App);
    await flushPromises();

    await wrapper.get('[data-testid="nav-index"]').trigger("click");
    await flushPromises();
    await wrapper.get('[data-testid="show-index-form"]').trigger("click");
    await flushPromises();
    await wrapper.get('[data-testid="index-name"]').setValue("audit_required");
    await wrapper.get('[data-testid="index-ttl"]').setValue("");
    await wrapper.get('[data-testid="index-page"] form').trigger("submit");
    await flushPromises();

    const postCall = global.fetch.mock.calls.find(([url, options]) => url === "/api/v1/indexes" && options?.method === "POST");
    expect(postCall).toBeFalsy();
    expect(wrapper.get('[data-testid="index-form-error"]').text()).toContain("TTL 天数为必填项");
  });

  it("paginates the index list through API page params", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = routeDefaultIndexMocks({
      onIndexes: async (path) => {
        if (path.includes("page=2")) {
          return jsonResponse({
            ...indexPayload([{ name: "idx_11" }]),
            pagination: { page: 2, page_size: 10, total: 45, total_pages: 5 }
          });
        }
        if (path.startsWith("/api/v1/indexes")) {
          return jsonResponse({
            ...indexPayload([{ name: "idx_01" }]),
            pagination: { page: 1, page_size: 10, total: 45, total_pages: 5 }
          });
        }
        return null;
      }
    });

    const wrapper = mount(App);
    await flushPromises();
    await wrapper.get('[data-testid="nav-index"]').trigger("click");
    await flushPromises();

    expect(wrapper.get('[data-testid="index-pagination"]').text()).toContain("3");
    await wrapper.get('[data-testid="index-next"]').trigger("click");
    await flushPromises();

    const pageCall = global.fetch.mock.calls.find(([url]) => String(url).startsWith("/api/v1/indexes?") && String(url).includes("page=2") && String(url).includes("page_size=10"));
    expect(pageCall).toBeTruthy();
    expect(wrapper.get('[data-testid="index-page"]').text()).toContain("idx_11");
  });

  it("loads index trend and uses PUT when editing an index", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi.fn(async (url, options = {}) => {
      const path = String(url);
      if (path === "/api/v1/auth") return jsonResponse(authStatus());
      if (path.startsWith("/api/v1/datasources")) return jsonResponse({ datasources: [] });
      if (path.startsWith("/api/v1/plugins")) return jsonResponse({ plugins: [] });
      if (path.startsWith("/api/v1/search/favorites")) return jsonResponse({ saved_searches: [] });
      if (path.startsWith("/api/v1/parse-rules")) return jsonResponse({ parse_rules: [] });
      if (path === "/api/v1/indexes/audit/trend?days=7") {
        return jsonResponse({
          index_name: "audit",
          table_name: "events_audit",
          points: [
            { date: "2026-07-09", rows: 1, storage_bytes: 1024 },
            { date: "2026-07-10", rows: 2, storage_bytes: 2048 },
            { date: "2026-07-11", rows: 3, storage_bytes: 4096 }
          ],
          current_rows: 3,
          current_storage_bytes: 4096,
          rows_growth_7d: 2,
          storage_growth_bytes_7d: 3072,
          source: "snapshot",
          snapshot_retention_days: 90
        });
      }
      if (path === "/api/v1/indexes/audit" && options.method === "PUT") {
        return jsonResponse({
          index_name: "audit",
          name: "audit",
          table_name: "events_audit",
          ttl_days: 14,
          physical_ttl_days: 14,
          rows: 3,
          storage_bytes: 4096,
          status: "disabled",
          configured: true
        });
      }
      if (path.startsWith("/api/v1/indexes")) {
        const ttl = global.fetch.mock.calls.some(([calledURL, calledOptions]) => calledURL === "/api/v1/indexes/audit" && calledOptions?.method === "PUT") ? 14 : 7;
        return jsonResponse({
          ...indexPayload([{ name: "audit", ttl, physicalTtl: ttl, rows: 3, storageBytes: 4096, latestEventTime: "2026-07-11 14:30:00" }]),
          pagination: { page: 1, page_size: 10, total: 1, total_pages: 1 }
        });
      }
      return jsonResponse({});
    });

    const wrapper = mount(App);
    await flushPromises();
    await wrapper.get('[data-testid="nav-index"]').trigger("click");
    await flushPromises();

    expect(wrapper.get('[data-testid="index-page"]').text()).toContain("4 KB");
    await wrapper.findAll(".link-btn").find((button) => button.text() === "趋势").trigger("click");
    await flushPromises();

    const trendCall = global.fetch.mock.calls.find(([url]) => String(url) === "/api/v1/indexes/audit/trend?days=7");
    expect(trendCall).toBeTruthy();
    expect(wrapper.get('[data-testid="index-trend-panel"]').text()).toContain("近 7 天净增 2 条");
    expect(wrapper.get('[data-testid="index-trend-panel"]').text()).toContain("采样数据");
    expect(wrapper.get('[data-testid="index-trend-panel"]').text()).toContain("保留 90 天");
    expect(wrapper.get('[data-testid="index-trend-y-axis"]').text()).toContain("3 条");
    expect(wrapper.get('[data-testid="index-trend-x-axis"]').text()).toContain("07-09");
    expect(wrapper.get('[data-testid="index-trend-x-axis"]').text()).toContain("07-11");

    await wrapper.findAll(".link-btn").find((button) => button.text() === "修改").trigger("click");
    await flushPromises();
    await wrapper.get('[data-testid="index-ttl"]').setValue("14");
    await wrapper.get('[data-testid="index-status"]').setValue("disabled");
    await wrapper.get('[data-testid="index-page"] form').trigger("submit");
    await flushPromises();

    const putCall = global.fetch.mock.calls.find(([url, options]) => url === "/api/v1/indexes/audit" && options?.method === "PUT");
    expect(putCall).toBeTruthy();
    expect(JSON.parse(putCall[1].body)).toMatchObject({ index_name: "audit", ttl_days: 14, status: "disabled" });
  });

  it("loads writer runtime metrics on the index page", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi.fn(async (url) => {
      const path = String(url);
      if (path === "/api/v1/auth") return jsonResponse(authStatus());
      if (path.startsWith("/api/v1/datasources")) return jsonResponse({ datasources: [] });
      if (path.startsWith("/api/v1/plugins")) return jsonResponse({ plugins: [] });
      if (path.startsWith("/api/v1/search/favorites")) return jsonResponse({ saved_searches: [] });
      if (path.startsWith("/api/v1/parse-rules")) return jsonResponse({ parse_rules: [] });
      if (path === "/api/v1/writer/runtime") {
        return jsonResponse({
          status: "running",
          output_topic: "xdp.output.default",
          batch_size: 100,
          total_events: 2400,
          failed_events: 0,
          deadletter_events: 0,
          total_batches: 24,
          failure_rate: 0,
          eps: 326.4,
          p95_ingest_latency_ms: 18,
          last_duration_ms: 11,
          last_retry_count: 0
        });
      }
      if (path.startsWith("/api/v1/indexes")) {
        return jsonResponse({
          ...indexPayload([{ name: "audit", ttl: 7, physicalTtl: 7, rows: 3, storageBytes: 4096 }]),
          pagination: { page: 1, page_size: 10, total: 1, total_pages: 1 }
        });
      }
      return jsonResponse({});
    });

    const wrapper = mount(App);
    await flushPromises();
    await wrapper.get('[data-testid="nav-index"]').trigger("click");
    await flushPromises();

    const writerCall = global.fetch.mock.calls.find(([url]) => String(url) === "/api/v1/writer/runtime");
    expect(writerCall).toBeTruthy();
    expect(wrapper.get('[data-testid="writer-runtime-panel"]').text()).toContain("Writer 入库状态");
    expect(wrapper.get('[data-testid="writer-runtime-panel"]').text()).toContain("running");
    expect(wrapper.get('[data-testid="writer-runtime-panel"]').text()).toContain("326.4 EPS");
    expect(wrapper.get('[data-testid="writer-runtime-panel"]').text()).toContain("P95 18ms");
  });
});
