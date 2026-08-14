import { safePublicHTTPURL } from "./publicUrl";

const allowedElements = new Set([
  "a",
  "b",
  "blockquote",
  "br",
  "code",
  "del",
  "em",
  "h1",
  "h2",
  "h3",
  "h4",
  "h5",
  "h6",
  "hr",
  "i",
  "img",
  "li",
  "mark",
  "ol",
  "p",
  "pre",
  "s",
  "span",
  "strong",
  "sub",
  "sup",
  "table",
  "tbody",
  "td",
  "tfoot",
  "th",
  "thead",
  "tr",
  "u",
  "ul",
]);

const removeWithContents = new Set([
  "audio",
  "base",
  "button",
  "embed",
  "form",
  "iframe",
  "input",
  "link",
  "math",
  "meta",
  "object",
  "script",
  "select",
  "source",
  "style",
  "svg",
  "template",
  "textarea",
  "video",
]);

const globalAttributes = new Set(["title"]);
const elementAttributes: Record<string, Set<string>> = {
  a: new Set(["href", "target"]),
  img: new Set(["alt", "height", "loading", "src", "width"]),
  td: new Set(["colspan", "rowspan"]),
  th: new Set(["colspan", "rowspan", "scope"]),
};

function safeMailto(value: string) {
  if (!/^mailto:/i.test(value) || /[\u0000-\u001f\u007f]/.test(value))
    return "";
  try {
    const parsed = new URL(value);
    return parsed.protocol === "mailto:" && /@/.test(parsed.pathname)
      ? parsed.href
      : "";
  } catch {
    return "";
  }
}

function boundedInteger(value: string, minimum: number, maximum: number) {
  if (!/^\d{1,4}$/.test(value)) return "";
  const parsed = Number(value);
  return parsed >= minimum && parsed <= maximum ? String(parsed) : "";
}

function sanitizeElement(element: Element) {
  const tag = element.localName.toLowerCase();
  if (!allowedElements.has(tag)) {
    if (removeWithContents.has(tag)) {
      element.remove();
      return;
    }
    element.replaceWith(...Array.from(element.childNodes));
    return;
  }

  for (const attribute of Array.from(element.attributes)) {
    const name = attribute.name.toLowerCase();
    const permitted =
      globalAttributes.has(name) || elementAttributes[tag]?.has(name);
    if (!permitted) {
      element.removeAttribute(attribute.name);
      continue;
    }
    if (name === "href" || name === "src") {
      const safe =
        name === "href"
          ? safePublicHTTPURL(attribute.value) || safeMailto(attribute.value)
          : safePublicHTTPURL(attribute.value);
      if (safe) element.setAttribute(name, safe);
      else element.removeAttribute(attribute.name);
      continue;
    }
    if (name === "target") {
      if (attribute.value !== "_blank") element.removeAttribute(attribute.name);
      continue;
    }
    if (name === "loading") {
      element.setAttribute("loading", "lazy");
      continue;
    }
    if (name === "width" || name === "height") {
      const safe = boundedInteger(attribute.value, 1, 4096);
      if (safe) element.setAttribute(name, safe);
      else element.removeAttribute(attribute.name);
      continue;
    }
    if (name === "colspan" || name === "rowspan") {
      const safe = boundedInteger(attribute.value, 1, 100);
      if (safe) element.setAttribute(name, safe);
      else element.removeAttribute(attribute.name);
      continue;
    }
    if (name === "scope" && !["col", "row"].includes(attribute.value))
      element.removeAttribute(attribute.name);
  }

  if (tag === "a" && element.getAttribute("target") === "_blank")
    element.setAttribute("rel", "noopener noreferrer");
  if (tag === "img") {
    element.setAttribute("loading", "lazy");
    element.setAttribute("decoding", "async");
    element.setAttribute("referrerpolicy", "no-referrer");
  }
}

/**
 * Preserve the product-description formatting contract while removing every
 * executable element, event handler, style, DOM-clobbering identifier and
 * non-HTTP(S) resource URL before Vue's v-html sink is reached.
 */
export function sanitizeProductHTML(value: unknown) {
  if (typeof value !== "string" || !value) return "";
  const parser = new DOMParser();
  const document = parser.parseFromString(value, "text/html");
  const walker = document.createTreeWalker(
    document.body,
    NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_COMMENT,
  );
  const nodes: Node[] = [];
  while (walker.nextNode()) nodes.push(walker.currentNode);
  for (const node of nodes) {
    if (node.nodeType === Node.COMMENT_NODE) node.parentNode?.removeChild(node);
    else if (node instanceof Element && node.isConnected) sanitizeElement(node);
  }
  return document.body.innerHTML;
}
