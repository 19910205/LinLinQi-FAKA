// Go validates catalog field patterns with RE2 on the server. JavaScript uses
// a backtracking engine, so patterns that are linear-time in Go can freeze a
// browser. Only execute a conservative, browser-safe subset for instant UI
// feedback; every pattern is still enforced authoritatively by the API.
function hasRiskyRepeatedGroup(pattern: string) {
  const stack: Array<{ alternation: boolean; repetition: boolean }> = [];
  let characterClass = false;
  for (let index = 0; index < pattern.length; index += 1) {
    const character = pattern[index];
    if (character === "\\") {
      index += 1;
      continue;
    }
    if (character === "[") {
      characterClass = true;
      continue;
    }
    if (character === "]" && characterClass) {
      characterClass = false;
      continue;
    }
    if (characterClass) continue;
    if (character === "(") {
      stack.push({ alternation: false, repetition: false });
      if (pattern[index + 1] === "?" && pattern[index + 2] === ":") index += 2;
      continue;
    }
    if (character === "|") {
      for (const group of stack) group.alternation = true;
      continue;
    }
    if (character === "*") {
      for (const group of stack) group.repetition = true;
      continue;
    }
    if (character === "+" || character === "?") {
      for (const group of stack) group.repetition = true;
      continue;
    }
    if (character === "{") {
      const quantifier = pattern.slice(index).match(/^\{\d+(?:,\d*)?\}/)?.[0];
      if (quantifier) {
        for (const group of stack) group.repetition = true;
        index += quantifier.length - 1;
      }
      continue;
    }
    if (character !== ")" || !stack.length) continue;
    const group = stack.pop()!;
    const suffix = pattern.slice(index + 1);
    const repeated =
      suffix.startsWith("*") ||
      suffix.startsWith("+") ||
      /^\{\d+(?:,\d*)?\}/.test(suffix);
    if (repeated && (group.repetition || group.alternation)) return true;
  }
  return false;
}

export function safeBrowserPattern(pattern: string): RegExp | null {
  if (!pattern || pattern.length > 300) return null;
  if (/\\[1-9]|\\k<|\(\?[=!<]/.test(pattern)) return null;
  if (/(?:\.\*|\.\+).*(?:\.\*|\.\+)/.test(pattern)) return null;
  if (hasRiskyRepeatedGroup(pattern)) return null;
  if (
    (pattern.match(/(?:^|[^\\])(?:[*+]|\{\d+(?:,\d*)?\})/g) || []).length > 24
  )
    return null;
  try {
    return new RegExp(`^(?:${pattern})$`, "u");
  } catch {
    return null;
  }
}
