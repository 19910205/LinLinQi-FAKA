export function safeAdminHTTPURL(value: unknown) {
  if (typeof value !== "string" || !value.trim()) return "";
  const candidate = value.trim();
  if (
    candidate.startsWith("/") &&
    !candidate.startsWith("//") &&
    !candidate.includes("\\") &&
    !/[\u0000-\u001f\u007f]/.test(candidate)
  ) {
    try {
      const parsed = new URL(candidate, window.location.origin);
      return `${parsed.pathname}${parsed.search}${parsed.hash}`;
    } catch {
      return "";
    }
  }
  try {
    const parsed = new URL(candidate);
    if (parsed.username || parsed.password) return "";
    if (parsed.protocol === "https:") return parsed.href;
    const loopback = ["localhost", "127.0.0.1", "[::1]", "::1"].includes(
      parsed.hostname.toLowerCase(),
    );
    return import.meta.env.DEV && parsed.protocol === "http:" && loopback
      ? parsed.href
      : "";
  } catch {
    return "";
  }
}
