import { createApp } from "vue";
import { createPinia } from "pinia";
import ui from "@nuxt/ui/vue-plugin";

import App from "./App.vue";
import { initializeTheme } from "./composables/useTheme";
import { router } from "./router";
import "./styles/app.css";
import "./style.css";

initializeTheme();

const app = createApp(App);
app.use(ui).use(createPinia()).use(router).mount("#app");
