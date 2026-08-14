const MAX_SUPPLIER_EXTERNAL_ID_RUNES = 180;

export function validSupplierExternalID(raw: string) {
  let value = raw.trim();
  if (!value || Array.from(value).length > MAX_SUPPLIER_EXTERNAL_ID_RUNES) {
    return false;
  }
  if (/\p{Cc}|\p{Cf}|\p{Z}/u.test(value)) return false;
  for (let attempt = 0; attempt < 4; attempt += 1) {
    const path = value.replaceAll("\\", "/");
    if (
      path.startsWith("/") ||
      path.split("/").some((part) => part === "." || part === "..")
    ) {
      return false;
    }
    try {
      const decoded = decodeURIComponent(value);
      if (decoded === value) break;
      value = decoded;
    } catch {
      break;
    }
  }
  return true;
}
