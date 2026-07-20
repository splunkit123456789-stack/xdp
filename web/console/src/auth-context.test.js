import { beforeEach, describe, expect, it, vi } from "vitest";
import { createAuthContext } from "./auth-context.js";

function makeContext(payloads = {}) {
  const requestJSON = vi.fn(async (path) => {
    if (path === "/api/v1/auth") return payloads.auth || { enabled: true, authenticated: true, rbac_enabled: true };
    if (path === "/api/v1/me") return payloads.me || {
      user: { username: "analyst", display_name: "Analyst" },
      permissions: ["search:execute"],
      scopes: { plugins: { use: [{ plugin_type: "search_command", plugin_code: "table" }], manage: [] } }
    };
    return {};
  });
  return createAuthContext({ requestJSON, storage: localStorage });
}

describe("auth-context", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it("loads current user and evaluates module permissions", async () => {
    const auth = makeContext();
    await auth.ensureAuthReady();

    expect(auth.canAccessModule("search")).toBe(true);
    expect(auth.canAccessModule("rbac")).toBe(false);
    expect(auth.firstAccessibleModule()).toBe("search");
  });

  it("allows builtin modules when rbac is disabled except rbac page", async () => {
    const auth = makeContext({ auth: { enabled: false, authenticated: true, rbac_enabled: false } });
    await auth.ensureAuthReady();

    expect(auth.canAccessModule("collect")).toBe(true);
    expect(auth.canAccessModule("plugins")).toBe(true);
    expect(auth.canAccessModule("rbac")).toBe(false);
  });

  it("checks plugin manage scope for plugins route", async () => {
    const auth = makeContext({
      me: {
        user: { username: "plugin-admin" },
        permissions: ["search:execute"],
        scopes: { plugins: { use: [], manage: [{ plugin_type: "parser", plugin_code: "*" }] } }
      }
    });
    await auth.ensureAuthReady();

    expect(auth.hasAnyManagePluginScope()).toBe(true);
    expect(auth.canAccessModule("plugins")).toBe(true);
  });

  it("resolves forbidden route access to the first accessible module", async () => {
    const auth = makeContext();
    await auth.ensureAuthReady();

    expect(auth.resolveRouteAccess("rbac")).toEqual({
      allowed: false,
      redirectName: "search",
      forbidden: "rbac"
    });
  });
});
