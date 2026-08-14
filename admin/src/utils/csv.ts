/**
 * Quote a CSV cell and neutralize spreadsheet formula prefixes in strings.
 * Numeric values remain numeric so financial exports retain their semantics.
 */
export function safeCSVCell(value: unknown) {
  let text = String(value ?? "");
  if (typeof value !== "number" && /^[\s\u0000-\u001f]*[=+\-@]/.test(text))
    text = `'${text}`;
  return `"${text.replaceAll('"', '""')}"`;
}
