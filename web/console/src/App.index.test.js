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
      storage: "clickhouse",
      status: item.status ?? "active",
      configured: true
    }))
  };
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
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse(indexPayload([{ name: "app" }, { name: "firewall" }])))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }))
      .mockResolvedValueOnce(jsonResponse({
        index_name: "audit_p0",
        name: "audit_p0",
        table_name: "events_audit_p0",
        rows: 0,
        ttl_days: 30,
        storage: "clickhouse",
        status: "active",
        configured: true
      }))
      .mockResolvedValueOnce(jsonResponse(indexPayload([{ name: "app" }, { name: "firewall" }, { name: "audit_p0" }])));

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
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse(indexPayload([{ name: "app" }, { name: "firewall" }, { name: "audit_p0" }])))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }));

    const refreshed = mount(App);
    await flushPromises();
    await refreshed.get('[data-testid="nav-index"]').trigger("click");
    await flushPromises();

    expect(refreshed.get('[data-testid="index-page"]').text()).toContain("audit_p0");
    expect(refreshed.get('[data-testid="index-page"]').text()).toContain("events_audit_p0");
  });

  it("deletes an index with storage drop so it does not reappear after refresh", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse(indexPayload([{ name: "app" }, { name: "audit_p0" }])))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }))
      .mockResolvedValueOnce(jsonResponse({ status: "deleted", index_name: "audit_p0" }))
      .mockResolvedValueOnce(jsonResponse(indexPayload([{ name: "app" }])));

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
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse(indexPayload([{ name: "app" }])))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }));

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
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus()))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }))
      .mockResolvedValueOnce(jsonResponse({
        ...indexPayload([{ name: "idx_01" }]),
        pagination: { page: 1, page_size: 10, total: 45, total_pages: 5 }
      }))
      .mockResolvedValueOnce(jsonResponse({ plugins: [] }))
      .mockResolvedValueOnce(jsonResponse({ saved_searches: [] }))
      .mockResolvedValueOnce(jsonResponse({ parse_rules: [] }))
      .mockResolvedValueOnce(jsonResponse({
        ...indexPayload([{ name: "idx_11" }]),
        pagination: { page: 2, page_size: 10, total: 45, total_pages: 5 }
      }));

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
});
