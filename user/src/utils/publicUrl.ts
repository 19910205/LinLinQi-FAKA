export function safePublicHTTPURL(value: unknown) {
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
    if (import.meta.env.PROD) {
      const loopback =
        parsed.hostname === "localhost" ||
        parsed.hostname === "127.0.0.1" ||
        parsed.hostname === "[::1]";
      return parsed.protocol === "https:" ||
        (parsed.protocol === "http:" && loopback)
        ? parsed.href
        : "";
    }
    return parsed.protocol === "http:" || parsed.protocol === "https:"
      ? parsed.href
      : "";
  } catch {
    return "";
  }
}

function isLoopbackHostname(hostname: string) {
  const normalized = hostname.toLowerCase().replace(/\.$/, "");
  return (
    normalized === "localhost" ||
    normalized === "127.0.0.1" ||
    normalized === "[::1]" ||
    normalized === "::1"
  );
}

/**
 * Validate an API-provided URL before assigning it to window.location.
 *
 * Payment and OAuth providers normally use HTTPS. Plain HTTP is retained only
 * for same-origin self-hosting and loopback development, so an API compromise
 * cannot silently downgrade a checkout to an arbitrary clear-text host.
 */
export function safeNavigationURL(value: unknown) {
  if (typeof value !== "string" || !value.trim()) return "";
  if (/[\u0000-\u001f\u007f]/.test(value)) return "";
  try {
    const current = new URL(window.location.href);
    const parsed = new URL(value.trim(), current.origin);
    if (parsed.username || parsed.password) return "";
    if (parsed.protocol === "https:") return parsed.href;
    if (parsed.protocol !== "http:" || current.protocol !== "http:") return "";
    if (parsed.origin === current.origin) return parsed.href;
    return isLoopbackHostname(current.hostname) &&
      isLoopbackHostname(parsed.hostname)
      ? parsed.href
      : "";
  } catch {
    return "";
  }
}

/** Keep an OAuth/login continuation inside this storefront. */
export function safeInternalPath(value: unknown, fallback: string) {
  if (typeof value !== "string") return fallback;
  const candidate = value.trim();
  if (
    !candidate.startsWith("/") ||
    candidate.startsWith("//") ||
    candidate.includes("\\") ||
    /[\u0000-\u001f\u007f]/.test(candidate)
  )
    return fallback;
  try {
    const parsed = new URL(candidate, window.location.origin);
    if (parsed.origin !== window.location.origin) return fallback;
    let decodedPath = parsed.pathname;
    for (let pass = 0; pass < 3; pass += 1) {
      const decoded = decodeURIComponent(decodedPath);
      if (decoded === decodedPath) break;
      decodedPath = decoded;
    }
    if (
      decodedPath.startsWith("//") ||
      decodedPath.includes("\\") ||
      /[\u0000-\u001f\u007f]/.test(decodedPath)
    )
      return fallback;
    return `${parsed.pathname}${parsed.search}${parsed.hash}`;
  } catch {
    return fallback;
  }
}
