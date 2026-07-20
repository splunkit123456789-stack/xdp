import { h } from "vue";
import { createRouter, createWebHistory } from "vue-router";

export const currentModuleKey = "xdp_current_module";
export const defaultModuleKey = "collect";
export const moduleRouteNames = ["collect", "parse", "index", "search", "plugins", "rbac"];

const RouteShell = { name: "XdpRouteShell", render: () => h("span", { style: "display:none" }) };
const NotFoundShell = { name: "XdpNotFoundShell", render: () => h("span", { style: "display:none" }) };

export function isKnownRouteModule(value) {
  return moduleRouteNames.includes(String(value || ""));
}

function readStoredModule() {
  if (typeof localStorage === "undefined") return defaultModuleKey;
  const stored = localStorage.getItem(currentModuleKey);
  return isKnownRouteModule(stored) ? stored : defaultModuleKey;
}

export const routes = [
  { path: "/", redirect: () => ({ name: readStoredModule() }) },
  { path: "/collect", name: "collect", component: RouteShell, meta: { module: "collect" } },
  { path: "/parse", name: "parse", component: RouteShell, meta: { module: "parse" } },
  { path: "/index", name: "index", component: RouteShell, meta: { module: "index" } },
  { path: "/search", name: "search", component: RouteShell, meta: { module: "search" } },
  { path: "/plugins", name: "plugins", component: RouteShell, meta: { module: "plugins" } },
  { path: "/rbac", name: "rbac", component: RouteShell, meta: { module: "rbac" } },
  { path: "/:pathMatch(.*)*", name: "not-found", component: NotFoundShell }
];

export function createXdpRouter(options = {}) {
  const router = createRouter({
    history: options.history || createWebHistory("/"),
    routes
  });
  const authContext = options.authContext;
  if (authContext) {
    router.beforeEach(async (to) => {
      if (!isKnownRouteModule(to.name)) return true;
      const authState = await authContext.ensureAuthReady();
      const loginRequired = Boolean(authState.loginRequired ?? authState.login_required);
      if (loginRequired && !authState.authenticated) return true;
      const access = authContext.resolveRouteAccess(to.name);
      if (access.allowed) return true;
      if (!access.redirectName) return true;
      const currentForbidden = Array.isArray(to.query?.forbidden) ? to.query.forbidden[0] : to.query?.forbidden;
      if (access.redirectName === to.name && currentForbidden === access.forbidden) return true;
      return { name: access.redirectName, query: { forbidden: access.forbidden } };
    });
  }
  router.afterEach((to) => {
    if (typeof localStorage === "undefined") return;
    if (!isKnownRouteModule(to.name)) return;
    localStorage.setItem(currentModuleKey, String(to.name));
  });
  return router;
}
