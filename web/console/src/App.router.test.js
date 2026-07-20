import { flushPromises, mount } from "@vue/test-utils";
import { createMemoryHistory } from "vue-router";
import { beforeEach, describe, expect, it, vi } from "vitest";
import App from "./AppMvp.vue";
import { createAuthContext, authContextKey } from "./auth-context.js";
import CollectPanel from "./panels/CollectPanel.vue";
import IndexPanel from "./panels/IndexPanel.vue";
import ParsePanel from "./panels/ParsePanel.vue";
import PluginsPanel from "./panels/PluginsPanel.vue";
import RbacPanel from "./panels/RbacPanel.vue";
import SearchPanel from "./panels/SearchPanel.vue";
import { createXdpRouter } from "./router.js";

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

function mePayload(overrides = {}) {
  return {
    user: { id: "u-admin", username: "admin", display_name: "Administrator", ...(overrides.user || {}) },
    roles: overrides.roles || [],
    permissions: overrides.permissions || [],
    scopes: overrides.scopes || { plugins: { use: [], manage: [] } }
  };
}

function installFetchStub(options = {}) {
  global.fetch = vi.fn(async (url) => {
    const path = String(url);
    if (path === "/api/v1/auth") return jsonResponse({ ...authStatus(true), rbac_enabled: Boolean(options.rbacEnabled) });
    if (path === "/api/v1/me") return jsonResponse(options.me || mePayload({
      permissions: ["datasource:read", "parse_rule:read", "index:read", "search:execute", "rbac:manage"],
      scopes: { plugins: { use: [], manage: [{ plugin_type: "input", plugin_code: "*" }] } }
    }));
    if (path.startsWith("/api/v1/datasources")) return jsonResponse({ datasources: [], pagination: { page: 1, page_size: 10, total: 0, total_pages: 1 } });
    if (path.startsWith("/api/v1/indexes")) return jsonResponse({ indexes: [], pagination: { page: 1, page_size: 10, total: 0, total_pages: 1 } });
    if (path === "/api/v1/parser-plugins") return jsonResponse({ plugins: [] });
    if (path.startsWith("/api/v1/parse-rules")) return jsonResponse({ parse_rules: [], pagination: { page: 1, page_size: 10, total: 0, total_pages: 1 } });
    if (path.startsWith("/api/v1/search/favorites")) return jsonResponse({ saved_searches: [] });
    if (path.startsWith("/api/v1/plugins/catalog")) return jsonResponse({ plugins: [] });
    if (path.startsWith("/api/v1/plugins?")) return jsonResponse({ plugins: [], pagination: { page: 1, page_size: 10, total: 0, total_pages: 1 }, type_counts: { input: 0, parser: 0, search_command: 0 } });
    if (path === "/api/v1/writer/runtime") return jsonResponse({ status: "ok" });
    return jsonResponse({});
  });
}

async function mountWithRoute(path = "/collect", options = {}) {
  localStorage.setItem("xdp_api_token", "test-token");
  installFetchStub(options);
  const router = createXdpRouter({ history: createMemoryHistory() });
  await router.push(path);
  await router.isReady();
  const wrapper = mount(App, { global: { plugins: [router] } });
  await flushPromises();
  return { wrapper, router };
}

beforeEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("XDP P3 vue-router module navigation", () => {
  it("uses provided auth context as the single menu permission source", async () => {
    installFetchStub({ rbacEnabled: true });
    const requestJSON = vi.fn(async (path) => {
      if (path === "/api/v1/auth") return { ...authStatus(true), rbac_enabled: true };
      if (path === "/api/v1/me") return mePayload({
        permissions: ["search:execute"],
        scopes: { plugins: { use: [], manage: [] } }
      });
      return {};
    });
    const authContext = createAuthContext({ requestJSON, storage: localStorage });
    await authContext.ensureAuthReady();
    const router = createXdpRouter({ history: createMemoryHistory(), authContext });

    await router.push("/search");
    await router.isReady();
    const wrapper = mount(App, {
      global: {
        plugins: [router],
        provide: { [authContextKey]: authContext }
      }
    });
    await flushPromises();

    const navText = wrapper.get('[data-testid="main-nav"]').text();
    expect(navText).toContain("搜索页");
    expect(navText).not.toContain("采集配置");
    expect(navText).not.toContain("解析配置");
    expect(navText).not.toContain("索引配置");
    expect(navText).not.toContain("插件管理");
    expect(navText).not.toContain("用户与权限");
  });

  it("keeps the target business route when auth context reports unauthenticated", async () => {
    const authContext = {
      ensureAuthReady: vi.fn(async () => ({ loginRequired: true, authenticated: false })),
      resolveRouteAccess: vi.fn()
    };
    const router = createXdpRouter({ history: createMemoryHistory(), authContext });

    await router.push("/search");
    await router.isReady();

    expect(authContext.ensureAuthReady).toHaveBeenCalled();
    expect(authContext.resolveRouteAccess).not.toHaveBeenCalled();
    expect(router.currentRoute.value.name).toBe("search");
  });

  it("uses router beforeEach to redirect forbidden business routes", async () => {
    const authContext = {
      ensureAuthReady: vi.fn(async () => ({ loginRequired: true, authenticated: true })),
      resolveRouteAccess: vi.fn(() => ({ allowed: false, redirectName: "search", forbidden: "rbac" }))
    };
    const router = createXdpRouter({ history: createMemoryHistory(), authContext });

    await router.push("/rbac");
    await router.isReady();

    expect(authContext.ensureAuthReady).toHaveBeenCalled();
    expect(authContext.resolveRouteAccess).toHaveBeenCalledWith("rbac");
    expect(router.currentRoute.value.name).toBe("search");
    expect(router.currentRoute.value.query.forbidden).toBe("rbac");
  });

  it("uses forbidden query when the fallback route equals the target route", async () => {
    const authContext = {
      ensureAuthReady: vi.fn(async () => ({ loginRequired: true, authenticated: true })),
      resolveRouteAccess: vi.fn(() => ({ allowed: false, redirectName: "collect", forbidden: "collect" }))
    };
    const router = createXdpRouter({ history: createMemoryHistory(), authContext });

    await router.push("/collect");
    await router.isReady();

    expect(authContext.ensureAuthReady).toHaveBeenCalled();
    expect(authContext.resolveRouteAccess).toHaveBeenCalledWith("collect");
    expect(router.currentRoute.value.name).toBe("collect");
    expect(router.currentRoute.value.query.forbidden).toBe("collect");
  });

  it("persists known business route modules from router afterEach", async () => {
    const router = createXdpRouter({ history: createMemoryHistory() });

    await router.push("/search");
    await router.isReady();
    expect(localStorage.getItem("xdp_current_module")).toBe("search");

    await router.push("/plugins");
    await flushPromises();
    expect(localStorage.getItem("xdp_current_module")).toBe("plugins");
  });

  it("does not let not-found routes overwrite the last business module", async () => {
    localStorage.setItem("xdp_current_module", "parse");
    const router = createXdpRouter({ history: createMemoryHistory() });

    await router.push("/nonexistent");
    await router.isReady();

    expect(router.currentRoute.value.name).toBe("not-found");
    expect(localStorage.getItem("xdp_current_module")).toBe("parse");
  });

  it("renders the module from direct route path", async () => {
    const { wrapper } = await mountWithRoute("/search");

    expect(wrapper.find('[data-testid="search-page"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="collect-page"]').exists()).toBe(false);
    expect(localStorage.getItem("xdp_current_module")).toBe("search");
  });

  it("renders business routes through extracted panel components", async () => {
    const cases = [
      ["/collect", CollectPanel, "collect-page", {}],
      ["/parse", ParsePanel, "parse-page", {}],
      ["/index", IndexPanel, "index-page", {}],
      ["/search", SearchPanel, "search-page", {}],
      ["/plugins", PluginsPanel, "plugins-page", {}],
      ["/rbac", RbacPanel, "rbac-page", { rbacEnabled: true }]
    ];

    for (const [path, component, testId, options] of cases) {
      const { wrapper } = await mountWithRoute(path, options);

      expect(wrapper.findComponent(component).exists(), `${path} should render ${component.name}`).toBe(true);
      expect(wrapper.find(`[data-testid="${testId}"]`).exists(), `${path} should keep ${testId}`).toBe(true);

      wrapper.unmount();
    }
  });

  it("updates the route when clicking top navigation", async () => {
    const { wrapper, router } = await mountWithRoute("/collect");

    await wrapper.get('[data-testid="nav-parse"]').trigger("click");
    await flushPromises();

    expect(router.currentRoute.value.name).toBe("parse");
    expect(router.currentRoute.value.path).toBe("/parse");
    expect(wrapper.find('[data-testid="parse-page"]').exists()).toBe(true);
    expect(localStorage.getItem("xdp_current_module")).toBe("parse");
  });

  it("renders top navigation with RouterLink custom while keeping button testids", async () => {
    const { wrapper } = await mountWithRoute("/collect");

    expect(wrapper.findComponent({ name: "RouterLink" }).exists()).toBe(true);
    expect(wrapper.get('[data-testid="nav-search"]').element.tagName).toBe("BUTTON");
  });

  it("restores the stored module when visiting the root route", async () => {
    localStorage.setItem("xdp_current_module", "search");
    const { wrapper, router } = await mountWithRoute("/");

    expect(router.currentRoute.value.name).toBe("search");
    expect(router.currentRoute.value.path).toBe("/search");
    expect(wrapper.find('[data-testid="search-page"]').exists()).toBe(true);
  });

  it("supports browser back navigation between modules", async () => {
    const { wrapper, router } = await mountWithRoute("/collect");

    await wrapper.get('[data-testid="nav-parse"]').trigger("click");
    await flushPromises();
    await wrapper.get('[data-testid="nav-search"]').trigger("click");
    await flushPromises();
    router.back();
    await flushPromises();

    expect(router.currentRoute.value.name).toBe("parse");
    expect(wrapper.find('[data-testid="parse-page"]').exists()).toBe(true);
  });

  it("shows a not found panel for unknown route paths", async () => {
    const { wrapper } = await mountWithRoute("/nonexistent");

    expect(wrapper.find('[data-testid="not-found-page"]').exists()).toBe(true);
    expect(wrapper.get('[data-testid="not-found-page"]').text()).toContain("404");
  });

  it("redirects unauthorized route access to the first accessible module with a 403 hint", async () => {
    const { wrapper, router } = await mountWithRoute("/rbac", {
      rbacEnabled: true,
      me: mePayload({ permissions: ["search:execute"], scopes: { plugins: { use: [], manage: [] } } })
    });

    await flushPromises();

    expect(router.currentRoute.value.name).toBe("search");
    expect(router.currentRoute.value.query.forbidden).toBe("rbac");
    expect(wrapper.get('[data-testid="module-forbidden"]').text()).toContain("403");
    expect(wrapper.find('[data-testid="search-page"]').exists()).toBe(true);
  });

  it("clears forbidden query on normal RouterLink navigation", async () => {
    const { wrapper, router } = await mountWithRoute("/search?forbidden=rbac");

    expect(wrapper.find('[data-testid="module-forbidden"]').exists()).toBe(true);

    await wrapper.get('[data-testid="nav-collect"]').trigger("click");
    await flushPromises();

    expect(router.currentRoute.value.name).toBe("collect");
    expect(router.currentRoute.value.query.forbidden).toBeUndefined();
    expect(wrapper.find('[data-testid="module-forbidden"]').exists()).toBe(false);
  });
});
