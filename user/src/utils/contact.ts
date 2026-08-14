const contactEmailPattern = /^\S+@\S+\.\S+$/;

export function isValidCheckoutContact(raw: string): boolean {
  const value = raw.trim().toLowerCase();
  const chars = Array.from(value);
  if (chars.length < 8 || chars.length > 190) return false;
  if (/\s|[\u0000-\u001f\u007f]/.test(value)) return false;
  if (contactEmailPattern.test(value)) return true;
  const compact = value.replace(/[^a-z0-9]/g, "");
  for (let index = 1; index < compact.length; index += 1) {
    const delta = compact.charCodeAt(index) - compact.charCodeAt(index - 1);
    if (delta !== 1 && delta !== -1) continue;
    let run = 2;
    while (index + run < compact.length) {
      const next = compact.charCodeAt(index + run);
      const previous = compact.charCodeAt(index + run - 1);
      if (next - previous !== 1 && next - previous !== -1) break;
      run += 1;
    }
    if (run >= 6) return false;
  }
  return true;
}
