import { createApp } from "vue";
import { createPinia } from "pinia";
import ElementPlus from "element-plus";
import "element-plus/dist/index.css";
import "element-plus/theme-chalk/dark/css-vars.css";

import App from "./App.vue";
import { initializeTheme } from "./composables/useTheme";
import { router } from "./router";
import "./style.css";
import "./styles/variables.scss";
import "./styles/dark.scss";
import "./styles/light.scss";

initializeTheme();

createApp(App).use(createPinia()).use(router).use(ElementPlus).mount("#app");
