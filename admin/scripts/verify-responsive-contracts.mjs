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
requireText("src/style.css", "@media (max-width: 800px)", "tablet shell");
requireText("src/style.css", "@media (max-width: 560px)", "phone shell");
requireText(
  "src/views/SupplyView.vue",
  '<th>{{ t("supply.colUpstreamSnapshot") }}</th>',
  "supplier snapshot column",
);
requireText(
  "src/views/SupplyView.vue",
  ".supply-table td > *",
  "supplier mobile cards",
);
requireText(
  "src/views/PaymentOperationsView.vue",
  "height: 100dvh",
  "payment mobile modal",
);
requireText(
  "src/components/SupplierCatalogManager.vue",
  "height: 100dvh",
  "catalog mobile modal",
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

console.log("Admin responsive contracts and static assets verified.");
