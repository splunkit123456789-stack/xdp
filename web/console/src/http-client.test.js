import { beforeEach, describe, expect, it, vi } from "vitest";
import { errorMessage, requestJSON, tokenKey } from "./http-client.js";

function response(payload, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: status === 200 ? "OK" : "Forbidden",
    text: async () => JSON.stringify(payload)
  };
}

describe("http-client", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it("adds bearer token when auth is true", async () => {
    localStorage.setItem(tokenKey, "abc");
    global.fetch = vi.fn(async () => response({ ok: true }));

    await requestJSON("/api/v1/me", { auth: true });

    expect(global.fetch).toHaveBeenCalledWith("/api/v1/me", {
      auth: true,
      headers: { Authorization: "Bearer abc" }
    });
  });

  it("formats structured api errors", async () => {
    expect(errorMessage({ error: { code: "FORBIDDEN", message: "permission denied" } }, "x")).toBe("FORBIDDEN: permission denied");
  });
});
