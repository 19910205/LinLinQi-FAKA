#!/usr/bin/env node
/**
 * LinLinQi production configuration generator.
 *
 * Fills every replace-* / weak placeholder with a strong random value and
 * derives public URLs from explicit flags or an --auto-host FQDN, then writes
 * a chmod-600 .env. Existing non-placeholder values are preserved so repeated
 * runs never rotate live credentials.
 *
 * Usage:
 *   node scripts/generate-production-env.mjs \
 *     --api-url https://api.example.com \
 *     --user-url https://store.example.com \
 *     --admin-url https://admin.example.com \
 *     --support-email ops@example.com \
 *     [--out .env]
 *
 * For a host with DNS names already pointing at this machine:
 *   node scripts/generate-production-env.mjs --auto-host store.example.com
 */

import {
  existsSync,
  readFileSync,
  writeFileSync,
  chmodSync,
} from "node:fs";
import { randomBytes, randomUUID } from "node:crypto";
import { fileURLToPath } from "node:url";
import { dirname, join, resolve } from "node:path";
import { execFileSync } from "node:child_process";

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const defaultsPath = join(root, ".env.example");

function parseEnv(text) {
  const result = {};
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.trim();
    if (!line || line.startsWith("#") || !line.includes("=")) continue;
    const separator = line.indexOf("=");
    const key = line.slice(0, separator).trim();
    let value = line.slice(separator + 1).trim();
    if (
      (value.startsWith('"') && value.endsWith('"')) ||
      (value.startsWith("'") && value.endsWith("'"))
    ) {
      value = value.slice(1, -1);
    }
    result[key] = value;
  }
  return result;
}

function randomToken(bytes) {
  return randomBytes(bytes).toString("hex");
}

function randomKey() {
  return randomBytes(32).toString("base64url");
}

function isPlaceholder(value) {
  if (!value) return true;
  const lower = value.toLowerCase();
  return (
    lower.includes("replace") ||
    lower.includes("change-me") ||
    lower.includes("demo") ||
    lower.includes("example.com") ||
    lower === "linlinqi_demo_key" ||
    lower === "linlinqi_live_replace"
  );
}

function requireHTTPS(url, label) {
  if (!/^https:\/\//i.test(url)) {
    throw new Error(
      `${label} must be a public HTTPS URL in production, received ${url}`,
    );
  }
  return url.replace(/\/+$/, "");
}

function normalizeEmail(value) {
  if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(value)) {
    throw new Error(`SUPPORT_EMAIL must be a valid email, received ${value}`);
  }
  return value;
}

function deriveURLs(host) {
  const clean = host.replace(/^https?:\/\//, "").replace(/\/+$/, "");
  if (!clean.includes(".") || /^\d+\.\d+\.\d+\.\d+$/.test(clean)) {
    throw new Error(
      `--auto-host must be a DNS name (received ${host}); pass --api-url/--user-url/--admin-url explicitly for bare hosts or IPs`,
    );
  }
  const labels = clean.split(".");
  const apex = labels.slice(labels.length - 2).join(".");
  const storeHost = labels.length >= 3 ? clean : `store.${apex}`;
  return {
    api: `https://api.${apex}`,
    user: `https://${storeHost}`,
    admin: `https://admin.${apex}`,
  };
}

function machineFQDN() {
  try {
    return execFileSync("hostname", ["-f"], { encoding: "utf8" }).trim();
  } catch {
    return "";
  }
}

function argumentsOf(argv) {
  const options = {};
  for (let index = 0; index < argv.length; index += 2) {
    const key = argv[index];
    if (!key.startsWith("--")) {
      throw new Error(`unexpected argument ${key}`);
    }
    const value = argv[index + 1];
    if (value === undefined || value.startsWith("--")) {
      throw new Error(`${key} requires a value`);
    }
    options[key.slice(2).replaceAll("-", "_")] = value;
  }
  return options;
}

const options = argumentsOf(process.argv.slice(2));
const outPath = resolve(root, options.out || ".env");
const generated = new Set();

const defaults = parseEnv(readFileSync(defaultsPath, "utf8"));
const existing = existsSync(outPath) ? parseEnv(readFileSync(outPath, "utf8")) : {};
const values = { ...defaults, ...existing };

const secrets = {
  POSTGRES_PASSWORD: randomToken(24),
  REDIS_PASSWORD: randomToken(24),
  JWT_SECRET: randomKey(),
  ADMIN_JWT_SECRET: randomKey(),
  DATA_ENCRYPTION_KEY: randomKey(),
  OPENAPI_KEY: `linlinqi_live_${randomToken(16)}`,
  OPENAPI_SECRET: randomKey(),
  METRICS_TOKEN: randomToken(32),
  NOTIFICATION_RELAY_SECRET: randomKey(),
};
for (const [key, generatedValue] of Object.entries(secrets)) {
  if (isPlaceholder(values[key])) {
    values[key] = generatedValue;
    generated.add(key);
  }
}

let urls = {};
if (options.auto_host) {
  urls = deriveURLs(options.auto_host);
} else if (options.api_url || options.user_url || options.admin_url) {
  urls = {
    api: options.api_url || existing.APP_URL || existing.MEDIA_PUBLIC_BASE_URL?.replace(/\/media$/, "") || "",
    user: options.user_url || existing.USER_APP_URL || "",
    admin: options.admin_url || existing.CORS_ORIGINS?.split(",")[1]?.trim() || "",
  };
  if (!urls.api || !urls.user || !urls.admin) {
    throw new Error(
      "pass --api-url, --user-url and --admin-url together (or --auto-host)",
    );
  }
} else {
  const detected = machineFQDN();
  if (detected) {
    urls = deriveURLs(detected);
    console.log(`Auto-detected hostname: ${detected}`);
  } else {
    throw new Error(
      "no URL flags and no usable hostname; pass --auto-host or explicit --api-url/--user-url/--admin-url",
    );
  }
}

urls.api = requireHTTPS(urls.api, "APP_URL");
urls.user = requireHTTPS(urls.user, "USER_APP_URL");
urls.admin = requireHTTPS(urls.admin, "ADMIN_APP_URL");
const apiURL = new URL(urls.api);

values.APP_URL = urls.api;
values.USER_APP_URL = urls.user;
values.MEDIA_PUBLIC_BASE_URL = `${urls.api}/media`;
values.SUPPORT_EMAIL = normalizeEmail(
  options.support_email || existing.SUPPORT_EMAIL || `ops@${apiURL.hostname}`,
);
values.CORS_ORIGINS = `${urls.user},${urls.admin}`;
values.SUPPLIER_CALLBACK_URL = values.SUPPLIER_CALLBACK_URL || urls.api;
values.BOOTSTRAP_ADMIN = options.bootstrap_admin === "true" ? "true" : values.BOOTSTRAP_ADMIN || "false";
if (options.bootstrap_admin_password) {
  values.BOOTSTRAP_ADMIN_PASSWORD = options.bootstrap_admin_password;
}
values.SEED_DATA = "false";
values.APP_ENV = "production";

const output = [];
for (const [key, value] of Object.entries(defaults)) {
  output.push(`${key}=${values[key] ?? ""}`);
}
writeFileSync(outPath, `${output.join("\n")}\n`, { mode: 0o600 });
chmodSync(outPath, 0o600);

console.log(`LinLinQi production configuration written to ${outPath} (chmod 600)`);
console.log(`  APP_URL        ${values.APP_URL}`);
console.log(`  USER_APP_URL   ${values.USER_APP_URL}`);
console.log(`  ADMIN (CORS)   ${values.CORS_ORIGINS}`);
console.log(`  SUPPORT_EMAIL  ${values.SUPPORT_EMAIL}`);
console.log(`  SEED_DATA      false`);
if (generated.size > 0) {
  console.log(
    `Generated strong values for: ${[...generated].sort().join(", ")}`,
  );
} else {
  console.log("All secrets already present; nothing rotated.");
}
if (options.bootstrap_admin === "true") {
  console.log(
    "BOOTSTRAP_ADMIN=true is set: create the admin once, then rerun this script or clear the password.",
  );
}
