import { computed, readonly, ref, type Ref } from "vue";

export interface CurrencyDefinition {
  code: string;
  name: string;
  symbol: string;
  minor_unit: number;
}

// Used only while the public currency registry is unavailable. Values are ISO
// 4217 minor-unit exponents; the API remains authoritative after it loads.
const fallbackMinorUnits: Record<string, number> = {
  BHD: 3,
  CLP: 0,
  DJF: 0,
  GNF: 0,
  IQD: 3,
  ISK: 0,
  JOD: 3,
  JPY: 0,
  KMF: 0,
  KRW: 0,
  KWD: 3,
  LYD: 3,
  OMR: 3,
  PYG: 0,
  RWF: 0,
  TND: 3,
  UGX: 0,
  VND: 0,
  VUV: 0,
  XAF: 0,
  XOF: 0,
  XPF: 0,
};

const registry = ref<Record<string, CurrencyDefinition>>({});
const configuredStoreCurrency = ref("");
const selectedDisplayCurrency = ref("");
const currencyStorageKey = "linlinqi-currency";

export function normalizeCurrency(value?: string | null) {
  const code = String(value || "")
    .trim()
    .toUpperCase();
  return /^[A-Z]{3}$/.test(code) ? code : "";
}

export function registerCurrencies(values: CurrencyDefinition[]) {
  const next: Record<string, CurrencyDefinition> = {};
  for (const value of values) {
    const code = normalizeCurrency(value?.code);
    const minorUnit = Number(value?.minor_unit);
    if (!code || !Number.isInteger(minorUnit) || minorUnit < 0 || minorUnit > 4)
      continue;
    next[code] = { ...value, code, minor_unit: minorUnit };
  }
  registry.value = next;
  restoreSelectedCurrency();
}

export function setStoreCurrency(value?: string | null) {
  configuredStoreCurrency.value = normalizeCurrency(value);
  restoreSelectedCurrency();
}

export const storeCurrency = readonly(configuredStoreCurrency);
export const selectedCurrency = readonly(selectedDisplayCurrency);
export const availableCurrencies = computed(() =>
  Object.values(registry.value).sort((left, right) =>
    left.code.localeCompare(right.code),
  ),
);

function restoreSelectedCurrency() {
  const saved = normalizeCurrency(localStorage.getItem(currencyStorageKey));
  const fallback = configuredStoreCurrency.value;
  if (saved && registry.value[saved]) {
    selectedDisplayCurrency.value = saved;
  } else if (
    fallback &&
    (!Object.keys(registry.value).length || registry.value[fallback])
  ) {
    selectedDisplayCurrency.value = fallback;
  }
}

export function selectCurrency(value: string) {
  const code = normalizeCurrency(value);
  if (!code || !registry.value[code]) return false;
  selectedDisplayCurrency.value = code;
  localStorage.setItem(currencyStorageKey, code);
  return true;
}

export function minorUnit(currency?: string | null) {
  const code = normalizeCurrency(currency);
  return registry.value[code]?.minor_unit ?? fallbackMinorUnits[code] ?? 2;
}

export function formatMinor(
  amount: number | null | undefined,
  currency?: string | null,
  locale?: string | Ref<string>,
) {
  const code = normalizeCurrency(currency) || configuredStoreCurrency.value;
  if (!code) return "—";
  const integer = Number(amount ?? 0);
  const exponent = minorUnit(code);
  const language = typeof locale === "string" ? locale : locale?.value;
  const value = Number.isSafeInteger(integer) ? integer / 10 ** exponent : 0;
  try {
    return new Intl.NumberFormat(language, {
      style: "currency",
      currency: code,
      currencyDisplay: "code",
      minimumFractionDigits: exponent,
      maximumFractionDigits: exponent,
    }).format(value);
  } catch {
    return `${code} ${value.toFixed(exponent)}`;
  }
}

export function minorToMajorInput(amount: number, currency?: string | null) {
  const exponent = minorUnit(currency || configuredStoreCurrency.value);
  const negative = amount < 0;
  const digits = String(Math.abs(Math.trunc(amount))).padStart(
    exponent + 1,
    "0",
  );
  if (!exponent) return `${negative ? "-" : ""}${digits}`;
  return `${negative ? "-" : ""}${digits.slice(0, -exponent)}.${digits.slice(-exponent)}`;
}

export function parseMajorToMinor(
  value: string,
  currency?: string | null,
): number | null {
  const normalized = value.trim();
  const exponent = minorUnit(currency || configuredStoreCurrency.value);
  const pattern = new RegExp(
    `^(?:0|[1-9]\\d{0,12})(?:\\.\\d{1,${Math.max(1, exponent)}})?$`,
  );
  if (!pattern.test(normalized)) return null;
  const [whole, fraction = ""] = normalized.split(".");
  if (!exponent && fraction) return null;
  const minor = Number(`${whole}${fraction.padEnd(exponent, "0") || ""}`);
  return Number.isSafeInteger(minor) ? minor : null;
}

export function useStoreMoney(locale: Ref<string>) {
  const currency = computed(
    () => selectedDisplayCurrency.value || configuredStoreCurrency.value,
  );
  return {
    currency,
    money: (amount: number | null | undefined, code?: string | null) =>
      formatMinor(amount, code || currency.value, locale),
  };
}
