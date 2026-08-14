import { readonly, ref } from "vue";
import { adminApi } from "../stores/auth";

export interface CurrencyDefinition {
  code: string;
  name: string;
  symbol: string;
  minor_unit: number;
  enabled?: boolean;
}

export interface CurrencyDirectoryPayload {
  items: CurrencyDefinition[];
  store_currency: string;
}

const FALLBACK_CURRENCY = "CNY";
const definitions = ref<Record<string, CurrencyDefinition>>({
  CNY: {
    code: "CNY",
    name: "Chinese Yuan",
    symbol: "¥",
    minor_unit: 2,
    enabled: true,
  },
});
const activeStoreCurrency = ref(FALLBACK_CURRENCY);
let loading: Promise<void> | null = null;

export const currencyDirectory = readonly(definitions);
export const storeCurrency = readonly(activeStoreCurrency);

function normalizeCurrency(code?: string | null) {
  const normalized = String(code || "")
    .trim()
    .toUpperCase();
  return /^[A-Z]{3}$/.test(normalized) ? normalized : activeStoreCurrency.value;
}

export function registerCurrencies(
  items: CurrencyDefinition[],
  currentStoreCurrency?: string,
) {
  const next = { ...definitions.value };
  for (const item of items || []) {
    const code = normalizeCurrency(item.code);
    const minorUnit = Number(item.minor_unit);
    if (!Number.isInteger(minorUnit) || minorUnit < 0 || minorUnit > 6)
      continue;
    next[code] = {
      code,
      name: String(item.name || code),
      symbol: String(item.symbol || code),
      minor_unit: minorUnit,
      enabled: item.enabled !== false,
    };
  }
  definitions.value = next;
  if (currentStoreCurrency)
    activeStoreCurrency.value = normalizeCurrency(currentStoreCurrency);
}

function publicCurrencyDirectoryURL() {
  const configured = new URL(
    String(adminApi.defaults.baseURL || window.location.origin),
    window.location.origin,
  );
  const pathname = configured.pathname.replace(
    /\/admin\/v1\/?$/,
    "/api/v1/currency-directory",
  );
  if (pathname === configured.pathname)
    throw new Error("invalid admin API base URL");
  configured.pathname = pathname;
  configured.search = "";
  configured.hash = "";
  return configured.toString();
}

// Currency metadata is storefront-readable and is needed by every business
// module to format minor units correctly. Fetch it without an admin bearer so
// fine-grained operators never need system.view just to display money.
export async function fetchPublicCurrencyDirectory(): Promise<CurrencyDirectoryPayload> {
  const response = await fetch(publicCurrencyDirectoryURL(), {
    headers: { Accept: "application/json" },
  });
  if (!response.ok) throw new Error("currency directory request failed");
  const envelope = (await response.json()) as {
    data?: Partial<CurrencyDirectoryPayload>;
  };
  const payload = envelope?.data || {};
  return {
    items: Array.isArray(payload.items) ? payload.items : [],
    store_currency: String(payload.store_currency || FALLBACK_CURRENCY),
  };
}

export async function loadCurrencyDirectory(force = false) {
  if (loading && !force) return loading;
  loading = (async () => {
    const payload = await fetchPublicCurrencyDirectory();
    registerCurrencies(payload.items, payload.store_currency);
  })().finally(() => {
    loading = null;
  });
  return loading;
}

export function minorUnit(currency?: string | null) {
  const code = normalizeCurrency(currency);
  return definitions.value[code]?.minor_unit ?? 2;
}

function integerText(value: string | number | bigint | null | undefined) {
  if (typeof value === "bigint") return value.toString();
  if (typeof value === "number") {
    if (!Number.isSafeInteger(value)) return "0";
    return String(value);
  }
  const text = String(value ?? "0").trim();
  return /^[-+]?\d+$/.test(text) ? text : "0";
}

export function minorToMajor(
  value: string | number | bigint | null | undefined,
  currency?: string | null,
) {
  const digits = minorUnit(currency);
  const integer = BigInt(integerText(value));
  const negative = integer < 0n;
  const absolute = negative ? -integer : integer;
  if (!digits) return `${negative ? "-" : ""}${absolute}`;
  const scale = 10n ** BigInt(digits);
  const whole = absolute / scale;
  const fraction = (absolute % scale).toString().padStart(digits, "0");
  return `${negative ? "-" : ""}${whole}.${fraction}`;
}

export function majorToMinor(
  value: string | number | null | undefined,
  currency?: string | null,
) {
  const digits = minorUnit(currency);
  const text = String(value ?? "").trim();
  const match = /^([+-]?)(\d+)(?:\.(\d*))?$/.exec(text);
  if (!match) throw new Error("invalid monetary decimal");
  const fraction = match[3] || "";
  if (fraction.length > digits)
    throw new Error("too many monetary decimal places");
  const scale = 10n ** BigInt(digits);
  const absolute =
    BigInt(match[2]) * scale + BigInt(fraction.padEnd(digits, "0") || "0");
  return `${match[1] === "-" ? "-" : ""}${absolute}`;
}

export function minorToSafeNumber(
  value: string | number | bigint | null | undefined,
) {
  const integer = BigInt(integerText(value));
  if (
    integer > BigInt(Number.MAX_SAFE_INTEGER) ||
    integer < BigInt(Number.MIN_SAFE_INTEGER)
  )
    throw new Error("monetary minor value exceeds the safe JSON integer range");
  return Number(integer);
}

function decimalSeparator(locale: string) {
  return (
    new Intl.NumberFormat(locale, {
      minimumFractionDigits: 1,
      maximumFractionDigits: 1,
    })
      .formatToParts(1.1)
      .find((part) => part.type === "decimal")?.value || "."
  );
}

function currencyAffixes(locale: string, code: string, digits: number) {
  const parts = new Intl.NumberFormat(locale, {
    style: "currency",
    currency: code,
    currencyDisplay: "code",
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).formatToParts(0);
  const numeric = new Set(["integer", "group", "decimal", "fraction"]);
  const first = parts.findIndex((part) => numeric.has(part.type));
  let last = parts.length - 1;
  while (last >= 0 && !numeric.has(parts[last].type)) last -= 1;
  return {
    prefix: parts
      .slice(0, first)
      .map((part) => part.value)
      .join(""),
    suffix: parts
      .slice(last + 1)
      .map((part) => part.value)
      .join(""),
  };
}

export function formatMoney(
  value: string | number | bigint | null | undefined,
  currency?: string | null,
  locale = "zh-CN",
) {
  const code = normalizeCurrency(currency);
  const digits = minorUnit(code);
  const major = minorToMajor(value, code);
  const negative = major.startsWith("-");
  const absolute = negative ? major.slice(1) : major;
  const [whole, fraction = ""] = absolute.split(".");
  const grouped = new Intl.NumberFormat(locale, {
    useGrouping: true,
    maximumFractionDigits: 0,
  }).format(BigInt(whole || "0"));
  const numeric = digits
    ? `${grouped}${decimalSeparator(locale)}${fraction.padEnd(digits, "0")}`
    : grouped;
  const { prefix, suffix } = currencyAffixes(locale, code, digits);
  return `${prefix}${negative ? "-" : ""}${numeric}${suffix}`;
}

export function formatSignedMoney(
  value: string | number | bigint | null | undefined,
  currency?: string | null,
  locale = "zh-CN",
) {
  const integer = BigInt(integerText(value));
  if (integer === 0n) return formatMoney(0, currency, locale);
  const absolute = integer < 0n ? -integer : integer;
  return `${integer > 0n ? "+" : "−"}${formatMoney(absolute, currency, locale)}`;
}

export function majorInputStep(currency?: string | null) {
  const digits = minorUnit(currency);
  return digits ? `0.${"0".repeat(digits - 1)}1` : "1";
}
