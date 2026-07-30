import { createApp } from "vue";
import { createPinia } from "pinia";
import {
  ElButton,
  ElDialog,
  ElDrawer,
  ElDropdown,
  ElDropdownItem,
  ElDropdownMenu,
  ElIcon,
  ElInput,
  ElOption,
  ElResult,
  ElSelect,
} from "element-plus";
import "element-plus/es/components/button/style/css";
import "element-plus/es/components/dialog/style/css";
import "element-plus/es/components/drawer/style/css";
import "element-plus/es/components/dropdown/style/css";
import "element-plus/es/components/dropdown-item/style/css";
import "element-plus/es/components/dropdown-menu/style/css";
import "element-plus/es/components/icon/style/css";
import "element-plus/es/components/input/style/css";
import "element-plus/es/components/message/style/css";
import "element-plus/es/components/message-box/style/css";
import "element-plus/es/components/option/style/css";
import "element-plus/es/components/result/style/css";
import "element-plus/es/components/select/style/css";
import "element-plus/theme-chalk/dark/css-vars.css";

import App from "./App.vue";
import { initializeTheme } from "./composables/useTheme";
import { router } from "./router";
import "./style.css";
import "./styles/variables.scss";
import "./styles/dark.scss";
import "./styles/light.scss";

initializeTheme();

const app = createApp(App);
for (const component of [
  ElButton,
  ElDialog,
  ElDrawer,
  ElDropdown,
  ElDropdownItem,
  ElDropdownMenu,
  ElIcon,
  ElInput,
  ElOption,
  ElResult,
  ElSelect,
]) {
  app.component(component.name!, component);
}
app.use(createPinia()).use(router).mount("#app");
