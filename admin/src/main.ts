import { createApp } from "vue";
import { createPinia } from "pinia";
import App from "./App.vue";
import router from "./router";
import { i18n, initializeLocale } from "./i18n";
import "./style.css";

function ignoreInjectedWalletRejection(event: PromiseRejectionEvent) {
  const reason = event.reason as { message?: string; stack?: string } | string;
  const text =
    typeof reason === "string"
      ? reason
      : `${reason?.message || ""} ${reason?.stack || ""}`;
  if (
    /wallet must has at least one account|inpage\.js|tronweb|registerSign|_bindTronWeb/i.test(
      text,
    )
  ) {
    event.preventDefault();
  }
}
window.addEventListener("unhandledrejection", ignoreInjectedWalletRejection);

async function bootstrap() {
  await initializeLocale();
  createApp(App).use(createPinia()).use(router).use(i18n).mount("#app");
}

void bootstrap();
