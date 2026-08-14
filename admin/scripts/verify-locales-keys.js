#!/usr/bin/env node
/**
 * Verify that all locale files under src/locales share the exact same
 * key structure (namespace + nested keys). Values may differ, keys must not.
 *
 * Usage: node scripts/verify-locales-keys.js
 */
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

const dir = path.join(__dirname, "..", "src", "locales");
const files = fs
  .readdirSync(dir)
  .filter((f) => f.endsWith(".json"))
  .sort();

function collectKeys(obj, prefix = "") {
  const keys = [];
  for (const [k, v] of Object.entries(obj)) {
    const full = prefix ? `${prefix}.${k}` : k;
    if (v && typeof v === "object" && !Array.isArray(v)) {
      keys.push(...collectKeys(v, full));
    } else {
      keys.push(full);
    }
  }
  return keys;
}

const parsed = {};
for (const f of files) {
  try {
    parsed[f] = JSON.parse(fs.readFileSync(path.join(dir, f), "utf8"));
    console.log(`parsed ✓ ${f}`);
  } catch (e) {
    console.error(`parsed ✗ ${f}: invalid JSON (${e.message})`);
    process.exitCode = 1;
  }
}

const base = files[0];
if (!parsed[base]) process.exit(1);
const baseKeys = collectKeys(parsed[base]).sort();

let ok = true;
for (const f of files.slice(1)) {
  if (!parsed[f]) continue;
  const keys = collectKeys(parsed[f]).sort();
  const missing = baseKeys.filter((k) => !keys.includes(k));
  const extra = keys.filter((k) => !baseKeys.includes(k));
  if (missing.length || extra.length) {
    ok = false;
    console.log(`structure ✗ ${f}: differs from ${base}`);
    if (missing.length) console.log(`  missing: ${missing.join(", ")}`);
    if (extra.length) console.log(`  extra:   ${extra.join(", ")}`);
  } else {
    console.log(`structure ✓ ${f}: keys match ${base}`);
  }
}

if (ok) {
  console.log(
    `\nOK — all ${files.length} locale files share the same key structure.`,
  );
} else {
  console.log("\nFAILED — locale key structures are inconsistent.");
  process.exitCode = 1;
}
