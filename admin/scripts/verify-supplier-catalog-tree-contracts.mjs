import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const componentPath = "src/components/SupplierCatalogManager.vue";
const source = readFileSync(join(root, componentPath), "utf8");

function fail(label, detail) {
  throw new Error(`${label}: ${componentPath} ${detail}`);
}

function requireMatch(value, pattern, label, detail) {
  if (!pattern.test(value)) fail(label, detail);
}

function forbidMatch(value, pattern, label, detail) {
  if (pattern.test(value)) fail(label, detail);
}

function sectionBetween(value, start, end, label) {
  const startMatch = start.exec(value);
  if (!startMatch) fail(label, "is missing the section start");
  const remainder = value.slice(startMatch.index + startMatch[0].length);
  const endMatch = end.exec(remainder);
  if (!endMatch) fail(label, "is missing the section end");
  return remainder.slice(0, endMatch.index);
}

function cssBlock(value, marker, label) {
  const markerIndex = value.indexOf(marker);
  if (markerIndex < 0) fail(label, `is missing ${marker}`);
  const openIndex = value.indexOf("{", markerIndex);
  if (openIndex < 0) fail(label, `has no opening brace after ${marker}`);
  let depth = 0;
  for (let index = openIndex; index < value.length; index += 1) {
    if (value[index] === "{") depth += 1;
    if (value[index] === "}") {
      depth -= 1;
      if (depth === 0) return value.slice(openIndex + 1, index);
    }
  }
  fail(label, `has an unterminated ${marker} block`);
}

function elementBlockByClass(value, classPattern, label) {
  const startPattern = /<([a-z][\w-]*)\b[^>]*class\s*=\s*"([^"]*)"[^>]*>/gi;
  let startMatch;
  while ((startMatch = startPattern.exec(value))) {
    if (!classPattern.test(startMatch[2])) continue;
    const tag = startMatch[1];
    const tagPattern = new RegExp(`<\\/?${tag}\\b[^>]*>`, "gi");
    tagPattern.lastIndex = startMatch.index;
    let depth = 0;
    let tagMatch;
    while ((tagMatch = tagPattern.exec(value))) {
      const token = tagMatch[0];
      if (token.startsWith(`</`)) depth -= 1;
      else if (!token.endsWith("/>")) depth += 1;
      if (depth === 0)
        return value.slice(startMatch.index, tagPattern.lastIndex);
    }
    fail(label, `has an unterminated <${tag}> hierarchy container`);
  }
  fail(label, "is missing an integrated product tree/hierarchy container");
}

function functionBlock(value, functionName, label) {
  const declaration = new RegExp(
    `(?:async\\s+)?function\\s+${functionName}\\s*\\([^)]*\\)\\s*\\{`,
  ).exec(value);
  if (!declaration) fail(label, `is missing function ${functionName}`);
  const openIndex = value.indexOf("{", declaration.index);
  let depth = 0;
  for (let index = openIndex; index < value.length; index += 1) {
    if (value[index] === "{") depth += 1;
    if (value[index] === "}") {
      depth -= 1;
      if (depth === 0) return value.slice(openIndex + 1, index);
    }
  }
  fail(label, `has an unterminated function ${functionName}`);
}

const templateStart = source.indexOf("<template>");
const styleStart = source.indexOf("<style", templateStart);
const templateEnd = source.lastIndexOf("</template>", styleStart);
if (templateStart < 0 || styleStart < 0 || templateEnd < templateStart) {
  fail("component template", "is missing the outer template/style boundary");
}
const template = source.slice(templateStart + "<template>".length, templateEnd);
const styles = sectionBetween(
  source,
  /<style[^>]*>/,
  /<\/style>/,
  "component styles",
);
const productSection = sectionBetween(
  template,
  /v-if="tab === 'products'"/,
  /v-else-if="tab === 'categories'"/,
  "product list",
);
// Selection is one integrated hierarchy in the product tab. A detached
// category pane plus a separate product result pane does not satisfy the
// archived interaction model.
const integratedHierarchy = elementBlockByClass(
  productSection,
  /(?:(?:product|catalog).*(?:tree|hierarchy)|(?:tree|hierarchy).*(?:product|catalog))/i,
  "integrated product hierarchy",
);
requireMatch(
  integratedHierarchy,
  /class\s*=\s*"[^"]*category[^"]*(?:row|item|node)[^"]*"/i,
  "integrated product hierarchy",
  "must render category nodes inside the product hierarchy",
);
requireMatch(
  integratedHierarchy,
  /class\s*=\s*"[^"]*product[^"]*(?:row|item|leaf)[^"]*"/i,
  "integrated product hierarchy",
  "must render product leaves in the same product hierarchy",
);
forbidMatch(
  productSection,
  /(?:<aside\b[^>]*class\s*=\s*"[^"]*(?:category|tree)[^"]*panel|class\s*=\s*"[^"]*(?:split|two-pane|tree-pane|product-pane|product-category-panel|remote-product-list-panel|catalog-product-browser)[^"]*")/i,
  "integrated product hierarchy",
  "must not implement category and product selection as detached panes",
);

// Category hierarchy is operable and defaults to collapsed.
requireMatch(
  source,
  /expanded[A-Za-z0-9]*Category[A-Za-z0-9]*IDs\s*=\s*ref(?:<[^>]+>)?\(\s*\[\s*\]\s*\)/,
  "category expansion",
  "must initialize category expansion with an empty set/list",
);
requireMatch(
  integratedHierarchy,
  /<(?:button|summary)\b/i,
  "category expansion",
  "must use an interactive expand/collapse control",
);
requireMatch(
  integratedHierarchy,
  /aria-expanded\s*=/i,
  "category expansion",
  "must expose aria-expanded on the expand/collapse control",
);
requireMatch(
  integratedHierarchy,
  /type\s*=\s*"checkbox"/i,
  "category selection",
  "must provide a checkbox for selecting a category branch",
);
requireMatch(
  integratedHierarchy,
  /(?:indeterminate|aria-checked\s*=\s*"[^"]*mixed)/i,
  "category selection",
  "must expose a mixed/indeterminate state for partially selected branches",
);
requireMatch(
  integratedHierarchy,
  /@(?:click|change)\s*=\s*"[^"]*(?:toggle|select|category)[^"]*"/i,
  "category interaction",
  "must wire category expansion/selection to an explicit interaction handler",
);
requireMatch(
  source,
  /(?:descendant|branch)[A-Za-z0-9]*(?:Category|Product)[A-Za-z0-9]*IDs|categoryDescendantIDs/i,
  "recursive category selection",
  "must derive product selection from the complete descendant category branch",
);

// Keep enough category provenance for selected products that are no longer on
// the current page. Incomplete branch state must include this mapping so a
// fold/search/page change cannot make a partially selected category look empty.
requireMatch(
  source,
  /selectedProductCategoryIDs\s*=\s*ref(?:<[^()\n]+>)?\(\s*\{\s*\}\s*\)/,
  "selected product category provenance",
  "must keep a product-to-category selection mapping",
);
const branchProductIDs = functionBlock(
  source,
  "branchProductIDs",
  "selected product category provenance",
);
requireMatch(
  source,
  /selectedProductIDsByCategory\s*=\s*computed\([\s\S]{0,500}?new\s+Map[\s\S]{0,500}?Object\.entries\(\s*selectedProductCategoryIDs\.value/,
  "selected product category provenance",
  "must build a computed category-to-selected-product Map from the provenance mapping",
);
requireMatch(
  branchProductIDs,
  /if\s*\(\s*complete\s*\)[\s\S]*?\}\s*else\s*\{[\s\S]*?for\s*\(\s*const\s+categoryID\s+of\s+branch\s*\)[\s\S]{0,240}?selectedProductIDsByCategory\.value\.get\(\s*categoryID/,
  "selected product category provenance",
  "must read the indexed selected-product Map by branch category when coverage is incomplete",
);
forbidMatch(
  branchProductIDs,
  /Object\.entries\(\s*selectedProductCategoryIDs\.value/,
  "selected product category provenance",
  "must not scan every selected product mapping while resolving one incomplete branch",
);
for (const functionName of [
  "toggleProduct",
  "toggleCurrentPage",
  "toggleProductCategorySelection",
]) {
  const body = functionBlock(
    source,
    functionName,
    "selected product category provenance",
  );
  requireMatch(
    body,
    /[A-Za-z]*[Cc]ategories\s*\[[^\]]+\]\s*=/,
    "selected product category provenance",
    `must record product-to-category entries in ${functionName}`,
  );
  requireMatch(
    body,
    /selectedProductCategoryIDs\.value\s*=\s*[A-Za-z]+/,
    "selected product category provenance",
    `must persist the updated mapping in ${functionName}`,
  );
}
for (const functionName of ["clearSelection", "importSelected"]) {
  requireMatch(
    functionBlock(source, functionName, "selected product category provenance"),
    /selectedProductCategoryIDs\.value\s*=\s*\{\s*\}/,
    "selected product category provenance",
    `must clear product-to-category entries in ${functionName}`,
  );
}
requireMatch(
  source,
  /watch\(\s*\(\)\s*=>\s*props\.supplier\.id\s*,[\s\S]{0,1000}?selectedProductCategoryIDs\.value\s*=\s*\{\s*\}/,
  "selected product category provenance",
  "must clear product-to-category entries when the supplier changes",
);

// Searching is still a tree interaction: matching product/category paths are
// revealed inline, and neither loading a search nor folding a branch may reset
// selections made before the operation.
const loadProducts = functionBlock(
  source,
  "loadProducts",
  "category search expansion",
);
const revealProductCategory = functionBlock(
  source,
  "revealProductCategory",
  "category search expansion",
);
requireMatch(
  loadProducts,
  /productSearchMode[\s\S]*revealProductCategory\s*\([^)]*(?:external_category_id|category)/i,
  "category search expansion",
  "must reveal the category path for products returned by a search",
);
requireMatch(
  revealProductCategory,
  /expanded[A-Za-z0-9]*Category[A-Za-z0-9]*IDs[\s\S]*(?:external_parent_id|parent[A-Za-z0-9]*ID)/i,
  "category search expansion",
  "must expand the matching category and its ancestor path",
);
for (const [operation, body] of [
  ["product search", loadProducts],
  [
    "category folding",
    functionBlock(
      source,
      "toggleProductCategoryExpanded",
      "selection persistence",
    ),
  ],
]) {
  forbidMatch(
    body,
    /selectedIDs\.value\s*=|selectedIDs\.value\.(?:splice|length\s*=)/,
    "selection persistence",
    `must not clear or replace selected product IDs during ${operation}`,
  );
}

// Product results use a compact operational list. Card grids hide the category
// relationship and are deliberately forbidden by this parity contract.
forbidMatch(
  source,
  /remote-product-(?:grid|card)/,
  "compact product list",
  "must not retain the remote-product-grid/card implementation",
);
requireMatch(
  productSection,
  /(?:<table\b|role\s*=\s*"(?:list|tree)"|class\s*=\s*"[^"]*(?:product|catalog)[^"]*(?:list|table|tree|hierarchy)[^"]*")/i,
  "compact product list",
  "must render products as a semantic table/list or an explicitly named compact list",
);
requireMatch(
  productSection,
  /v-for\s*=\s*"\s*(?:product|item|row)(?:\s*,[^)]*)?\s+in\s+(?:products|[A-Za-z0-9]*Product[A-Za-z0-9]*(?:Rows|Items|Leaves))\s*"/i,
  "compact product list",
  "must render one repeatable row/item per remote product",
);
requireMatch(
  productSection,
  /(?:所属分类|catalog(?:Product)?CategoryColumn|class\s*=\s*"[^"]*(?:product-category|category-(?:column|cell))[^"]*")/i,
  "product category column",
  "must show an explicit 所属分类 column/cell rather than an unlabeled metadata chip",
);

// A 390px viewport is covered by the <=760px contract. The compact list must
// switch to a narrow layout and all growing text cells must be shrinkable.
const phoneStyles = cssBlock(
  styles,
  "@media (max-width: 760px)",
  "phone responsive contract",
);
requireMatch(
  phoneStyles,
  /\.[a-z0-9-]*(?:product|catalog)[a-z0-9-]*(?:tree|hierarchy|list|row|item|leaf)/i,
  "phone responsive contract",
  "must include the integrated product hierarchy inside <=760px",
);
requireMatch(
  phoneStyles,
  /\.[a-z0-9-]*category[a-z0-9-]*(?:row|item|node)/i,
  "phone responsive contract",
  "must include category-node layout inside <=760px",
);
requireMatch(
  phoneStyles,
  /(?:grid-template-columns\s*:[^;]*(?:minmax\(0\s*,\s*1fr\)|\b1fr\b)|flex-direction\s*:\s*column|display\s*:\s*block)/i,
  "390px layout",
  "must collapse product/category rows to a shrinkable narrow layout",
);
requireMatch(
  styles,
  /(?:product|category)[^{]*\{[^}]*(?:min-width\s*:\s*0|overflow-wrap\s*:\s*anywhere|word-break\s*:\s*break-word|overflow\s*:\s*hidden)/i,
  "390px overflow safety",
  "must make product/category text cells shrink or wrap without horizontal overflow",
);

for (const match of styles.matchAll(
  /([^{}]*(?:product|category)[^{}]*)\{([^{}]*)\}/gi,
)) {
  for (const minimum of match[2].matchAll(/min-width\s*:\s*(\d+)px/gi)) {
    if (Number(minimum[1]) > 390) {
      fail(
        "390px overflow safety",
        `sets ${match[1].trim()} to min-width ${minimum[1]}px`,
      );
    }
  }
}

console.log("Supplier catalog tree/list responsive contracts verified.");
