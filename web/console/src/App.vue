<template>
  <main v-if="screen === 'login'" data-testid="login-page" class="login-shell">
    <div class="login-grid" aria-hidden="true"></div>
    <header class="login-topbar">
      <div class="login-brand">
        <span class="login-brand-mark">X</span>
        <span>XDP&gt;Console</span>
      </div>
      <span class="login-pill">AUTH GATEWAY</span>
      <span class="login-pill muted">MVP ACCESS</span>
    </header>

    <section class="login-layout">
      <section class="login-title-card" aria-label="XDP 登录入口">
        <p class="login-eyebrow">SECURE DATA PLATFORM</p>
        <h1>
          <span class="login-gradient">XDP</span>
          <strong>可信数据入口</strong>
        </h1>
        <p class="login-lede">
          采集、解析、索引与搜索统一入口，登录后进入 XDP 控制台。
        </p>
        <div class="login-chip-row">
          <span>Syslog Ingest</span>
          <span>props.conf Parser</span>
          <span>SPL Search</span>
        </div>
      </section>

      <section class="login-card">
        <div class="login-card-head">
          <div>
            <p class="login-eyebrow">SIGN IN</p>
            <h2>登录控制台</h2>
          </div>
          <span class="login-status-dot" aria-label="服务可用"></span>
        </div>

        <form class="login-form" @submit.prevent="submitLogin">
          <label>
            <span>用户名</span>
            <input
              v-model="credentials.username"
              name="username"
              autocomplete="username"
              placeholder="请输入用户名"
              required
            />
          </label>
          <label>
            <span>密码</span>
            <input
              v-model="credentials.password"
              name="password"
              autocomplete="current-password"
              placeholder="请输入密码"
              type="password"
              required
            />
          </label>
          <button type="submit">登录</button>
        </form>

        <p v-if="loginError" data-testid="login-error" class="login-error">{{ loginError }}</p>
        <button v-if="auth.enabled === false" class="dev-entry" type="button" @click="enterConsole">
          开发模式进入
        </button>
      </section>
    </section>
    <footer>© 2026 XDP Console</footer>
  </main>

  <main v-else class="app-shell">
    <header class="topbar">
      <div class="logo">XDP<span>&gt;</span></div>
      <div class="user">Administrator</div>
      <button data-testid="logout" class="logout" type="button" @click="logout">退出</button>
    </header>
    <nav data-testid="main-nav" class="module-nav">
      <button
        v-for="item in modules"
        :key="item.key"
        :class="{ active: currentModule === item.key }"
        type="button"
        :data-testid="`nav-${item.key}`"
        @click="selectModule(item.key)"
      >
        {{ item.label }}
      </button>
    </nav>
    <section class="workspace">
      <h1>{{ activeModule.label }}</h1>
      <p>{{ activeModule.description }}</p>
      <pre v-if="lastProtectedPayload" class="payload">{{ lastProtectedPayload }}</pre>
    </section>
  </main>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from "vue";

const tokenKey = "xdp_api_token";
const currentModuleKey = "xdp_current_module";
const defaultModuleKey = "collect";
const screen = ref("login");
const auth = reactive({ enabled: true, authenticated: false });
const credentials = reactive({ username: "admin", password: "" });
const loginError = ref("");
const currentModule = ref(defaultModuleKey);
const lastProtectedPayload = ref("");

const modules = [
  { key: "collect", label: "采集配置", description: "配置 Syslog、Kafka 等数据采集入口。" },
  { key: "parse", label: "解析配置", description: "配置 JSON、KV、分隔符、正则解析规则。" },
  { key: "index", label: "索引配置", description: "管理 index、TTL、物理表和存储状态。" },
  { key: "search", label: "搜索页", description: "输入 SPL，选择时间范围并执行搜索。" }
];

const activeModule = computed(() => modules.find((item) => item.key === currentModule.value) || modules[0]);

onMounted(async () => {
  await loadAuthStatus();
});

async function loadAuthStatus() {
  loginError.value = "";
  const response = await requestJSON("/api/v1/auth", { auth: true });
  Object.assign(auth, response);
  if (!response.enabled || response.authenticated) {
    enterConsole();
    await loadProtectedData();
    return;
  }
  screen.value = "login";
}

async function submitLogin() {
  loginError.value = "";
  const username = credentials.username.trim();
  const password = credentials.password;
  if (!username || !password.trim()) {
    loginError.value = "请输入用户名和密码";
    return;
  }
  try {
    const response = await requestJSON("/api/v1/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        username,
        password
      })
    });
    localStorage.setItem(tokenKey, response.token);
    enterConsole();
    await loadProtectedData();
  } catch (error) {
    localStorage.removeItem(tokenKey);
    loginError.value = error.message;
    screen.value = "login";
  }
}

function enterConsole() {
  currentModule.value = readStoredModule();
  persistCurrentModule(currentModule.value);
  screen.value = "app";
}

function logout() {
  localStorage.removeItem(tokenKey);
  localStorage.removeItem(currentModuleKey);
  lastProtectedPayload.value = "";
  screen.value = "login";
}

function selectModule(moduleKey) {
  if (!isValidModule(moduleKey)) return;
  currentModule.value = moduleKey;
  persistCurrentModule(moduleKey);
}

function readStoredModule() {
  const stored = localStorage.getItem(currentModuleKey);
  return isValidModule(stored) ? stored : defaultModuleKey;
}

function persistCurrentModule(moduleKey) {
  localStorage.setItem(currentModuleKey, isValidModule(moduleKey) ? moduleKey : defaultModuleKey);
}

function isValidModule(moduleKey) {
  return modules.some((item) => item.key === moduleKey);
}

async function loadProtectedData() {
  try {
    const response = await requestJSON("/api/v1/datasources", { auth: true });
    lastProtectedPayload.value = JSON.stringify(response, null, 2);
  } catch {
    lastProtectedPayload.value = "";
  }
}

async function requestJSON(url, options = {}) {
  const headers = { ...(options.headers || {}) };
  if (options.auth) {
    const token = localStorage.getItem(tokenKey);
    if (token) {
      headers.Authorization = `Bearer ${token}`;
    }
  }
  const response = await fetch(url, { ...options, headers });
  const text = await response.text();
  const payload = text ? JSON.parse(text) : {};
  if (!response.ok) {
    throw new Error(errorMessage(payload, response.statusText));
  }
  return payload;
}

function errorMessage(payload, fallback) {
  if (payload && payload.error && payload.error.message) {
    return payload.error.message;
  }
  if (typeof payload.error === "string") {
    return payload.error;
  }
  return fallback || "请求失败";
}
</script>

<style scoped>
:global(*) {
  box-sizing: border-box;
}

:global(:root) {
  --xdp-bg: #070925;
  --xdp-bg2: #12091f;
  --xdp-ink: #f8fbff;
  --xdp-muted: #a5b2d1;
  --xdp-line: rgba(151, 173, 255, 0.18);
  --xdp-glass: rgba(10, 15, 45, 0.78);
  --xdp-glass2: rgba(19, 27, 72, 0.82);
  --xdp-orange: #ffad00;
  --xdp-coral: #ff6848;
  --xdp-pink: #ff1f85;
  --xdp-cyan: #55dfff;
  --xdp-green: #67f28a;
  --xdp-danger: #ff5f61;
  --xdp-radius: 22px;
  --xdp-shadow: 0 24px 80px rgba(0, 0, 0, 0.42);
}

:global(body) {
  margin: 0;
  min-height: 100vh;
  font-family: "Avenir Next", "PingFang SC", "Microsoft YaHei", sans-serif;
  background: #edf1ee;
}

button,
input {
  font: inherit;
}

.login-shell {
  min-height: 100vh;
  position: relative;
  display: grid;
  grid-template-rows: auto 1fr auto;
  gap: 38px;
  overflow: hidden;
  padding: 22px clamp(18px, 4vw, 56px) 28px;
  color: var(--xdp-ink);
  background:
    radial-gradient(circle at 74% 8%, rgba(255, 31, 133, 0.24), transparent 24rem),
    radial-gradient(circle at 10% 28%, rgba(85, 223, 255, 0.15), transparent 26rem),
    linear-gradient(115deg, #071348 0%, #080a2b 44%, #15061e 100%);
}

.login-grid {
  position: absolute;
  inset: 0;
  background:
    repeating-linear-gradient(90deg, transparent 0 78px, rgba(255, 115, 49, 0.13) 79px, transparent 81px),
    repeating-linear-gradient(0deg, transparent 0 138px, rgba(85, 223, 255, 0.045) 139px, transparent 141px);
  opacity: 0.58;
  pointer-events: none;
}

.login-topbar,
.login-layout,
footer {
  position: relative;
  z-index: 1;
}

.login-topbar {
  width: min(1500px, 100%);
  min-height: 62px;
  margin: 0 auto;
  display: flex;
  align-items: center;
  gap: 14px;
  border: 1px solid var(--xdp-line);
  border-radius: 999px;
  padding: 0 18px 0 24px;
  background: rgba(5, 8, 30, 0.66);
  backdrop-filter: blur(18px);
  box-shadow: 0 18px 58px rgba(0, 0, 0, 0.3);
}

.login-brand {
  display: flex;
  align-items: center;
  gap: 9px;
  margin-right: auto;
  color: #fff;
  font-size: 22px;
  font-weight: 500;
  letter-spacing: -0.03em;
}

.login-brand-mark {
  display: grid;
  place-items: center;
  width: 32px;
  height: 32px;
  border-radius: 9px;
  background: linear-gradient(135deg, var(--xdp-orange), var(--xdp-pink));
  color: #12071c;
  font-weight: 600;
  box-shadow: 0 0 30px rgba(255, 78, 86, 0.38);
}

.login-pill {
  border: 1px solid rgba(255, 255, 255, 0.13);
  border-radius: 999px;
  padding: 8px 12px;
  color: #e8eeff;
  font: 700 12px "SFMono-Regular", "Menlo", "Consolas", monospace;
  letter-spacing: 0.08em;
}

.login-pill.muted {
  color: var(--xdp-muted);
}

.login-layout {
  width: min(1280px, 100%);
  margin: auto;
  display: grid;
  grid-template-columns: minmax(0, 1.08fr) minmax(380px, 480px);
  gap: 28px;
  align-items: stretch;
}

.login-title-card,
.login-card {
  border: 1px solid var(--xdp-line);
  border-radius: var(--xdp-radius);
  background: linear-gradient(180deg, rgba(20, 29, 76, 0.82), rgba(6, 10, 35, 0.74));
  box-shadow: var(--xdp-shadow);
  backdrop-filter: blur(18px);
}

.login-title-card {
  min-height: 520px;
  position: relative;
  display: flex;
  flex-direction: column;
  justify-content: center;
  overflow: hidden;
  padding: clamp(30px, 5vw, 58px);
}

.login-title-card::after {
  content: "";
  position: absolute;
  right: -92px;
  bottom: -76px;
  width: 360px;
  height: 260px;
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 60px;
  background: linear-gradient(135deg, rgba(255, 173, 0, 0.12), rgba(255, 31, 133, 0.14));
  transform: rotate(18deg);
}

.login-eyebrow {
  margin: 0;
  color: #c8d4f5;
  font: 700 13px "SFMono-Regular", "Menlo", "Consolas", monospace;
  letter-spacing: 0.1em;
  text-transform: uppercase;
}

.login-title-card h1 {
  position: relative;
  z-index: 1;
  margin: 18px 0 0;
  display: grid;
  gap: 12px;
}

.login-gradient {
  display: inline-block;
  width: max-content;
  padding-right: 0.18em;
  background: linear-gradient(90deg, var(--xdp-orange), var(--xdp-coral) 46%, var(--xdp-pink));
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
  font-size: clamp(48px, 6.4vw, 84px);
  font-weight: 700;
  line-height: 1;
  letter-spacing: -0.025em;
  text-shadow: 0 18px 70px rgba(255, 54, 117, 0.26);
}

.login-title-card strong {
  max-width: 650px;
  color: #fff;
  font-size: clamp(24px, 2.8vw, 36px);
  font-weight: 700;
  line-height: 1.16;
  letter-spacing: -0.05em;
}

.login-lede {
  position: relative;
  z-index: 1;
  max-width: 560px;
  margin: 24px 0 0;
  color: var(--xdp-muted);
  font-size: 17px;
  line-height: 1.7;
}

.login-chip-row {
  position: relative;
  z-index: 1;
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 34px;
}

.login-chip-row span {
  border: 1px solid rgba(255, 255, 255, 0.14);
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.07);
  color: #e8eeff;
  padding: 8px 12px;
  font-size: 12px;
  font-weight: 700;
}

.login-card {
  align-self: center;
  min-height: 470px;
  padding: 28px;
}

.login-card-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 18px;
  margin-bottom: 26px;
}

.login-card h2 {
  margin: 8px 0 0;
  color: #fff;
  font-size: 30px;
  line-height: 1.1;
  letter-spacing: -0.04em;
}

.login-status-dot {
  width: 14px;
  height: 14px;
  margin-top: 4px;
  border-radius: 999px;
  background: var(--xdp-green);
  box-shadow: 0 0 0 7px rgba(103, 242, 138, 0.1), 0 0 28px rgba(103, 242, 138, 0.68);
}

.brand,
.logo {
  letter-spacing: -0.06em;
  font-size: 46px;
  font-weight: 500;
}

.brand span,
.logo span {
  color: #48bf53;
}

.login-form {
  display: grid;
  gap: 16px;
}

.login-form label {
  display: grid;
  gap: 8px;
  color: #dce5fb;
  font-size: 13px;
  font-weight: 700;
}

.login-form input {
  width: 100%;
  height: 56px;
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 14px;
  outline: none;
  padding: 0 16px;
  color: #fff;
  background: rgba(1, 4, 22, 0.52);
  font-size: 16px;
  transition: border-color 160ms ease, box-shadow 160ms ease, background 160ms ease;
}

.login-form input::placeholder {
  color: #7887aa;
}

.login-form input:focus {
  border-color: rgba(85, 223, 255, 0.78);
  background: rgba(3, 9, 34, 0.74);
  box-shadow: 0 0 0 4px rgba(85, 223, 255, 0.1);
}

.login-form button,
.dev-entry,
.logout,
.module-nav button {
  border: 0;
  color: #fff;
  background: #198f34;
  cursor: pointer;
  font-weight: 700;
}

.login-form button {
  height: 56px;
  margin-top: 6px;
  border-radius: 14px;
  background: linear-gradient(90deg, var(--xdp-orange), var(--xdp-coral) 46%, var(--xdp-pink));
  color: #14071c;
  font-size: 18px;
  box-shadow: 0 18px 42px rgba(255, 57, 116, 0.26);
  transition: transform 160ms ease, box-shadow 160ms ease;
}

.login-form button:hover {
  transform: translateY(-1px);
  box-shadow: 0 22px 52px rgba(255, 57, 116, 0.34);
}

.login-form button:focus-visible,
.dev-entry:focus-visible {
  outline: 3px solid rgba(85, 223, 255, 0.52);
  outline-offset: 3px;
}

.login-error {
  margin: 16px 0 0;
  border: 1px solid rgba(255, 95, 97, 0.35);
  border-radius: 14px;
  padding: 10px 12px;
  color: #ffcbc6;
  background: rgba(255, 95, 97, 0.1);
}

.dev-entry {
  margin-top: 16px;
  padding: 10px 14px;
  border: 1px solid rgba(85, 223, 255, 0.28);
  border-radius: 999px;
  color: #dffaff;
  background: rgba(85, 223, 255, 0.1);
}

footer {
  justify-self: center;
  color: #7f8fb7;
  font-size: 13px;
}

.app-shell {
  min-height: 100vh;
  display: grid;
  grid-template-rows: 48px 52px 1fr;
}

.topbar {
  display: flex;
  align-items: center;
  gap: 22px;
  padding: 0 22px;
  color: #eef5ef;
  background: #111917;
}

.topbar .logo {
  font-size: 24px;
}

.user {
  margin-left: auto;
  color: #bdc9c4;
}

.logout {
  padding: 7px 12px;
  border-radius: 3px;
}

.module-nav {
  display: flex;
  background: #2f3a41;
}

.module-nav button {
  padding: 0 22px;
  background: transparent;
  color: #d9e1dd;
}

.module-nav button.active {
  color: #fff;
  border-bottom: 4px solid #48bf53;
}

.workspace {
  padding: 32px;
}

.workspace h1 {
  margin: 0;
  font-size: 34px;
}

.payload {
  margin-top: 24px;
  padding: 16px;
  overflow: auto;
  color: #d8f6df;
  background: #101715;
}

@media (max-width: 720px) {
  .login-shell {
    gap: 22px;
    padding: 14px 14px 22px;
  }

  .login-topbar {
    min-height: auto;
    align-items: flex-start;
    border-radius: 24px;
    padding: 14px;
  }

  .login-pill {
    display: none;
  }

  .login-layout {
    grid-template-columns: 1fr;
    gap: 16px;
  }

  .login-title-card {
    min-height: auto;
    padding: 24px;
  }

  .login-gradient {
    font-size: clamp(44px, 13vw, 64px);
  }

  .login-title-card strong {
    font-size: 22px;
  }

  .login-card {
    min-height: auto;
    padding: 22px;
  }

  .login-form input,
  .login-form button {
    border-radius: 12px;
  }

  .module-nav {
    overflow-x: auto;
  }
}
</style>
