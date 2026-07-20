import { createApp } from "vue";
import App from "./AppMvp.vue";
import { createAuthContext, authContextKey } from "./auth-context.js";
import { createXdpRouter } from "./router.js";

const authContext = createAuthContext();
const app = createApp(App);

app.provide(authContextKey, authContext);
app.use(createXdpRouter({ authContext }));
app.mount("#app");
