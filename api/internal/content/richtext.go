package content

import (
	"html"
	"net/url"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var allowedRichTextElements = map[string]struct{}{
	"a": {}, "b": {}, "blockquote": {}, "br": {}, "code": {}, "del": {},
	"div": {}, "em": {}, "h1": {}, "h2": {}, "h3": {}, "h4": {}, "h5": {}, "h6": {},
	"hr": {}, "i": {}, "img": {}, "li": {}, "ol": {}, "p": {}, "pre": {},
	"s": {}, "span": {}, "strong": {}, "table": {}, "tbody": {}, "td": {}, "th": {},
	"thead": {}, "tr": {}, "u": {}, "ul": {},
}

var blockedRichTextElements = map[string]struct{}{
	"applet": {}, "audio": {}, "base": {}, "button": {}, "canvas": {}, "embed": {},
	"form": {}, "frame": {}, "frameset": {}, "iframe": {}, "input": {}, "link": {},
	"math": {}, "meta": {}, "noscript": {}, "object": {}, "option": {}, "picture": {},
	"script": {}, "select": {}, "source": {}, "style": {}, "svg": {}, "textarea": {},
	"track": {}, "video": {},
}

// SanitizeRichHTML returns a conservative HTML fragment suitable for public
// product descriptions. It intentionally drops scripts, forms, embedded
// documents, CSS and event handlers while retaining catalog formatting and
// mirrored images. The function is idempotent and safe for plain text input.
func SanitizeRichHTML(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	context := &xhtml.Node{Type: xhtml.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := xhtml.ParseFragment(strings.NewReader(value), context)
	if err != nil {
		return html.EscapeString(value)
	}
	var output strings.Builder
	output.Grow(len(value))
	for _, node := range nodes {
		renderRichTextNode(&output, node)
	}
	return strings.TrimSpace(output.String())
}

func renderRichTextNode(output *strings.Builder, node *xhtml.Node) {
	switch node.Type {
	case xhtml.TextNode:
		output.WriteString(html.EscapeString(node.Data))
	case xhtml.ElementNode:
		tag := strings.ToLower(node.Data)
		if _, blocked := blockedRichTextElements[tag]; blocked {
			return
		}
		if _, allowed := allowedRichTextElements[tag]; !allowed {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				renderRichTextNode(output, child)
			}
			return
		}
		output.WriteByte('<')
		output.WriteString(tag)
		writeRichTextAttributes(output, tag, node.Attr)
		output.WriteByte('>')
		if tag == "br" || tag == "hr" || tag == "img" {
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			renderRichTextNode(output, child)
		}
		output.WriteString("</")
		output.WriteString(tag)
		output.WriteByte('>')
	}
}

func writeRichTextAttributes(output *strings.Builder, tag string, attributes []xhtml.Attribute) {
	values := make(map[string]string, len(attributes))
	for _, attribute := range attributes {
		key := strings.ToLower(strings.TrimSpace(attribute.Key))
		value := strings.TrimSpace(attribute.Val)
		if strings.HasPrefix(key, "on") || key == "style" || key == "class" || key == "id" {
			continue
		}
		switch tag {
		case "a":
			if key == "href" && safeRichTextURL(value, true) {
				values[key] = value
			} else if key == "title" {
				values[key] = limitRunes(value, 300)
			}
		case "img":
			switch key {
			case "src":
				if safeRichTextURL(value, false) {
					values[key] = value
				}
			case "alt", "title":
				values[key] = limitRunes(value, 300)
			case "width", "height":
				if dimension, err := strconv.Atoi(value); err == nil && dimension > 0 && dimension <= 4096 {
					values[key] = strconv.Itoa(dimension)
				}
			}
		case "td", "th":
			if key == "colspan" || key == "rowspan" {
				if span, err := strconv.Atoi(value); err == nil && span > 0 && span <= 100 {
					values[key] = strconv.Itoa(span)
				}
			}
		}
	}
	for _, key := range []string{"href", "src", "alt", "title", "width", "height", "colspan", "rowspan"} {
		if value, ok := values[key]; ok && value != "" {
			output.WriteByte(' ')
			output.WriteString(key)
			output.WriteString(`="`)
			output.WriteString(html.EscapeString(value))
			output.WriteByte('"')
		}
	}
	if tag == "a" && values["href"] != "" {
		output.WriteString(` target="_blank" rel="nofollow noopener noreferrer"`)
	}
	if tag == "img" && values["src"] != "" {
		output.WriteString(` loading="lazy" decoding="async"`)
	}
}

func safeRichTextURL(value string, allowFragment bool) bool {
	if value == "" || len(value) > 1000 || strings.HasPrefix(value, "//") {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil {
		return false
	}
	if parsed.IsAbs() {
		return parsed.Scheme == "https" || parsed.Scheme == "http"
	}
	if parsed.Host != "" {
		return false
	}
	if parsed.Path == "" {
		return allowFragment && parsed.Fragment != ""
	}
	return strings.HasPrefix(parsed.Path, "/") || strings.HasPrefix(parsed.Path, "./") || strings.HasPrefix(parsed.Path, "../")
}

func limitRunes(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) > maximum {
		runes = runes[:maximum]
	}
	return string(runes)
}
