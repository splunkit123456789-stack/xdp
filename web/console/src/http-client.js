export const tokenKey = "xdp_api_token";

export async function requestJSON(url, options = {}) {
  const headers = { ...(options.headers || {}) };
  if (options.auth) {
    const token = localStorage.getItem(tokenKey);
    if (token) headers.Authorization = `Bearer ${token}`;
  }
  const response = await fetch(url, { ...options, headers });
  const text = await response.text();
  const payload = text ? JSON.parse(text) : {};
  if (!response.ok) throw new Error(errorMessage(payload, response.statusText));
  return payload;
}

export function errorMessage(payload, fallback) {
  if (payload?.error?.message) {
    return payload.error.code ? `${payload.error.code}: ${payload.error.message}` : payload.error.message;
  }
  if (typeof payload.error === "string") return payload.error;
  return fallback || "请求失败";
}
