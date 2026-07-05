import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import App from "./App.vue";

function jsonResponse(payload, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: status === 200 ? "OK" : "Error",
    text: async () => JSON.stringify(payload),
    json: async () => payload
  };
}

function authStatus(authenticated = false) {
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

describe("XDP Vue login and basic auth", () => {
  it("shows the login page when auth is enabled and the request is not authenticated", async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse(authStatus(false)));

    const wrapper = mount(App);
    await flushPromises();

    expect(wrapper.get('[data-testid="login-page"]').text()).toContain("XDP");
    expect(wrapper.find('[data-testid="main-nav"]').exists()).toBe(false);
    expect(wrapper.get('input[name="username"]').exists()).toBe(true);
    expect(wrapper.get('input[name="password"]').exists()).toBe(true);
  });

  it("logs in, stores the token, and calls protected APIs with Authorization bearer header", async () => {
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus(false)))
      .mockResolvedValueOnce(jsonResponse({
        token: "test-token",
        token_type: "Bearer",
        expires_in: 0,
        user: { username: "admin", role: "admin" }
      }))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }));

    const wrapper = mount(App);
    await flushPromises();

    await wrapper.get('input[name="username"]').setValue("admin");
    await wrapper.get('input[name="password"]').setValue("xdp");
    await wrapper.get("form").trigger("submit.prevent");
    await flushPromises();

    expect(localStorage.getItem("xdp_api_token")).toBe("test-token");
    const nav = wrapper.get('[data-testid="main-nav"]');
    expect(nav.text()).toContain("采集配置");
    expect(nav.text()).toContain("解析配置");
    expect(nav.text()).toContain("索引配置");
    expect(nav.text()).toContain("搜索页");
    expect(nav.findAll("button")).toHaveLength(4);
    const protectedCall = global.fetch.mock.calls.find(([url]) => url === "/api/v1/datasources");
    expect(protectedCall[1].headers.Authorization).toBe("Bearer test-token");
  });

  it("keeps the user on the login page and shows an error when credentials are invalid", async () => {
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus(false)))
      .mockResolvedValueOnce(jsonResponse({
        error: { code: "INVALID_CREDENTIALS", message: "invalid username or password" },
        request_id: "req_test"
      }, 401));

    const wrapper = mount(App);
    await flushPromises();

    await wrapper.get('input[name="username"]').setValue("admin");
    await wrapper.get('input[name="password"]').setValue("wrong");
    await wrapper.get("form").trigger("submit.prevent");
    await flushPromises();

    expect(wrapper.get('[data-testid="login-error"]').text()).toContain("invalid username or password");
    expect(localStorage.getItem("xdp_api_token")).toBeNull();
    expect(wrapper.find('[data-testid="main-nav"]').exists()).toBe(false);
  });

  it("blocks login on the frontend when username or password is empty", async () => {
    global.fetch = vi.fn().mockResolvedValue(jsonResponse(authStatus(false)));

    const wrapper = mount(App);
    await flushPromises();

    await wrapper.get('input[name="username"]').setValue("");
    await wrapper.get('input[name="password"]').setValue("");
    await wrapper.get("form").trigger("submit.prevent");
    await flushPromises();

    expect(wrapper.get('[data-testid="login-error"]').text()).toContain("请输入用户名和密码");
    expect(global.fetch).toHaveBeenCalledTimes(1);

    await wrapper.get('input[name="username"]').setValue("admin");
    await wrapper.get('input[name="password"]').setValue("");
    await wrapper.get("form").trigger("submit.prevent");
    await flushPromises();

    expect(wrapper.get('[data-testid="login-error"]').text()).toContain("请输入用户名和密码");
    expect(global.fetch).toHaveBeenCalledTimes(1);
    expect(global.fetch).not.toHaveBeenCalledWith("/api/v1/login", expect.anything());
  });

  it("logs out by clearing the local token and returning to the login page", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus(true)))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }));

    const wrapper = mount(App);
    await flushPromises();

    expect(wrapper.get('[data-testid="main-nav"]').exists()).toBe(true);
    await wrapper.get('[data-testid="logout"]').trigger("click");

    expect(localStorage.getItem("xdp_api_token")).toBeNull();
    expect(wrapper.get('[data-testid="login-page"]').exists()).toBe(true);
  });

  it("restores the current module from localStorage and clears it on logout", async () => {
    localStorage.setItem("xdp_api_token", "test-token");
    localStorage.setItem("xdp_current_module", "search");
    global.fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(authStatus(true)))
      .mockResolvedValueOnce(jsonResponse({ datasources: [] }));

    const wrapper = mount(App);
    await flushPromises();

    expect(wrapper.get(".workspace").text()).toContain("搜索页");
    await wrapper.get('[data-testid="nav-parse"]').trigger("click");
    expect(localStorage.getItem("xdp_current_module")).toBe("parse");

    await wrapper.get('[data-testid="logout"]').trigger("click");

    expect(localStorage.getItem("xdp_current_module")).toBeNull();
    expect(localStorage.getItem("xdp_api_token")).toBeNull();
  });
});
