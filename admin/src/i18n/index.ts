import { createI18n } from "vue-i18n";
import zhCN from "../locales/zh-CN.json";

export type LocaleCode =
  "zh-CN" | "zh-TW" | "en" | "vi" | "ru" | "ja" | "ko" | "th";

export const SUPPORTED_LOCALES: Array<{
  code: LocaleCode;
  label: string;
  flag: string;
}> = [
  { code: "zh-CN", label: "简体中文", flag: "🇨🇳" },
  { code: "zh-TW", label: "繁體中文", flag: "🇭🇰" },
  { code: "en", label: "English", flag: "🇺🇸" },
  { code: "vi", label: "Tiếng Việt", flag: "🇻🇳" },
  { code: "ru", label: "Русский", flag: "🇷🇺" },
  { code: "ja", label: "日本語", flag: "🇯🇵" },
  { code: "ko", label: "한국어", flag: "🇰🇷" },
  { code: "th", label: "ไทย", flag: "🇹🇭" },
];

const STORAGE_KEY = "linlinqi-admin-locale";

const localeLoaders: Record<LocaleCode, () => Promise<{ default: unknown }>> = {
  "zh-CN": async () => ({ default: zhCN }),
  "zh-TW": () => import("../locales/zh-TW.json"),
  en: () => import("../locales/en.json"),
  vi: () => import("../locales/vi.json"),
  ru: () => import("../locales/ru.json"),
  ja: () => import("../locales/ja.json"),
  ko: () => import("../locales/ko.json"),
  th: () => import("../locales/th.json"),
};

export function getInitialLocale(): LocaleCode {
  const saved = localStorage.getItem(STORAGE_KEY);
  if (saved && SUPPORTED_LOCALES.some((l) => l.code === saved)) {
    return saved as LocaleCode;
  }
  const nav = navigator.language?.toLowerCase() || "";
  if (nav.startsWith("zh"))
    return nav.startsWith("zh-tw") || nav.startsWith("zh-hk")
      ? "zh-TW"
      : "zh-CN";
  if (nav.startsWith("en")) return "en";
  if (nav.startsWith("vi")) return "vi";
  if (nav.startsWith("ru")) return "ru";
  if (nav.startsWith("ja")) return "ja";
  if (nav.startsWith("ko")) return "ko";
  if (nav.startsWith("th")) return "th";
  return "zh-CN";
}

export const i18n = createI18n({
  legacy: false,
  locale: "zh-CN" as LocaleCode,
  fallbackLocale: "zh-CN",
  messages: { "zh-CN": zhCN } as Partial<Record<LocaleCode, typeof zhCN>>,
});

export async function setLocale(locale: LocaleCode) {
  if (!i18n.global.availableLocales.includes(locale)) {
    const loaded = await localeLoaders[locale]();
    i18n.global.setLocaleMessage(locale, loaded.default as typeof zhCN);
  }
  i18n.global.locale.value = locale;
  localStorage.setItem(STORAGE_KEY, locale);
  document.documentElement.lang = locale;
}

export async function initializeLocale() {
  const preferred = getInitialLocale();
  try {
    await setLocale(preferred);
  } catch {
    await setLocale("zh-CN");
  }
}
