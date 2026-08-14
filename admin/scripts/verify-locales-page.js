#!/usr/bin/env node
/* 验证其余 7 个语言文件的 page 区块与 zh-CN 完全一致（含嵌套键），且 JSON 合法 */
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

const dir = path.join(__dirname, "..", "src", "locales");
const reference = "zh-CN";
const targets = ["zh-TW", "en", "vi", "ru", "ja", "ko", "th"];

function load(file) {
  const raw = fs.readFileSync(path.join(dir, file + ".json"), "utf8");
  return JSON.parse(raw); // 解析失败会抛错 -> JSON 不合法
}

function keysOf(obj) {
  return Object.keys(obj).sort();
}

function assertSameKeys(a, b, label) {
  const ka = keysOf(a);
  const kb = keysOf(b);
  if (ka.length !== kb.length || ka.some((k, i) => k !== kb[i])) {
    throw new Error(
      `${label} 键不一致:\n  zh-CN: [${ka.join(", ")}]\n  ${label}: [${kb.join(", ")}]`,
    );
  }
}

let pass = true;
try {
  const ref = load(reference);
  const refPage = ref.page;

  // 1) 参考文件结构自检
  if (!refPage) throw new Error("zh-CN.json 缺少 page 区块");
  const refRoutes = keysOf(refPage);
  if (refRoutes.length < 33)
    throw new Error(
      `zh-CN page 路由数异常，至少应有 33 个，实际 ${refRoutes.length}`,
    );
  for (const r of refRoutes) {
    const entry = refPage[r];
    const sub = keysOf(entry);
    if (sub.join(",") !== "subtitle,title") {
      throw new Error(
        `zh-CN page.${r} 应仅含 title/subtitle，实际 [${sub.join(", ")}]`,
      );
    }
  }
  console.log(
    `[OK] 参考 ${reference}.json: page 区块 ${refRoutes.length} 个路由 × {title, subtitle}`,
  );

  // 2) 逐文件校验
  for (const f of targets) {
    const data = load(f);
    const page = data.page;
    if (!page) throw new Error(`${f}.json 缺少 page 区块`);
    assertSameKeys(refPage, page, `${f}.page`);
    for (const r of refRoutes) {
      const entry = page[r];
      if (!entry || typeof entry !== "object")
        throw new Error(`${f}.page.${r} 不是对象`);
      assertSameKeys(refPage[r], entry, `${f}.page.${r}`);
    }
    console.log(
      `[OK] ${f}.json: JSON 合法，page 键结构与 zh-CN 完全一致（${refRoutes.length} 路由 × title/subtitle）`,
    );
  }
} catch (e) {
  pass = false;
  console.error(`[FAIL] ${e.message}`);
}

process.exit(pass ? 0 : 1);
