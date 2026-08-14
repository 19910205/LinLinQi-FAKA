import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const read = (path) => readFileSync(join(root, path), "utf8");

function requireText(path, value, label) {
  if (!read(path).includes(value)) {
    throw new Error(`${label}: ${path} is missing ${JSON.stringify(value)}`);
  }
}

function filesBelow(path) {
  const absolute = join(root, path);
  return readdirSync(absolute).flatMap((name) => {
    const child = join(absolute, name);
    return statSync(child).isDirectory()
      ? filesBelow(child.slice(root.length + 1))
      : [child];
  });
}

requireText("index.html", "width=device-width, initial-scale=1.0", "viewport");
requireText("src/style.css", "@media (max-width: 900px)", "tablet shell");
requireText("src/style.css", "@media (max-width: 620px)", "phone shell");
requireText("src/style.css", "@media (max-width: 360px)", "320px shell");
requireText(
  "src/style.css",
  "grid-template-columns: 76px minmax(0, 1fr) auto auto",
  "cart desktop grid",
);
requireText("src/style.css", "top: 100%", "density-safe mobile menu");
requireText(
  "src/style.css",
  "color-mix(in srgb, var(--bg) 82%, transparent)",
  "phone hero contrast scrim",
);
requireText(
  "src/components/ResellerConsole.vue",
  ":data-label=\"t('reseller.console.orderNo')\"",
  "reseller order cards",
);
requireText(
  "src/components/ResellerConsole.vue",
  ".order-ledger td::before",
  "reseller phone ledger",
);

for (const file of filesBelow("src")) {
  if (!/\.(?:vue|ts|css)$/.test(file)) continue;
  const source = readFileSync(file, "utf8");
  for (const match of source.matchAll(
    /(?<![A-Za-z0-9.:])\/assets\/[A-Za-z0-9_./-]+/g,
  )) {
    const publicFile = join(root, "public", match[0].slice(1));
    if (!existsSync(publicFile)) {
      throw new Error(`missing static asset ${match[0]} referenced by ${file}`);
    }
  }
}

console.log("Storefront responsive contracts and static assets verified.");
