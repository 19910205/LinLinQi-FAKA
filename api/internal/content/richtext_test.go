package content

import (
	"strings"
	"testing"
)

func TestSanitizeRichHTMLDropsExecutableContent(t *testing.T) {
	input := `<p onclick="steal()">商品 <strong>说明</strong><script>alert(1)</script>` +
		`<img src="javascript:alert(2)" onerror="steal()"><iframe src="https://evil.example"></iframe></p>`
	got := SanitizeRichHTML(input)
	for _, forbidden := range []string{"onclick", "script", "alert(1)", "javascript:", "onerror", "iframe", "evil.example"} {
		if strings.Contains(strings.ToLower(got), forbidden) {
			t.Fatalf("sanitized HTML contains %q: %s", forbidden, got)
		}
	}
	if !strings.Contains(got, "商品 <strong>说明</strong>") {
		t.Fatalf("expected formatting and text to remain: %s", got)
	}
}

func TestSanitizeRichHTMLKeepsSafeCatalogFormatting(t *testing.T) {
	input := `<h3>规格</h3><p>本地图片</p><img src="/media/sha256/ab/a.png" alt="商品图" width="800" style="position:fixed">` +
		`<a href="https://docs.example/path">文档</a>`
	got := SanitizeRichHTML(input)
	for _, expected := range []string{`<h3>规格</h3>`, `src="/media/sha256/ab/a.png"`, `loading="lazy"`, `href="https://docs.example/path"`, `rel="nofollow noopener noreferrer"`} {
		if !strings.Contains(got, expected) {
			t.Fatalf("sanitized HTML missing %q: %s", expected, got)
		}
	}
	if strings.Contains(got, "style=") {
		t.Fatalf("style attribute must be removed: %s", got)
	}
}

func TestSanitizeRichHTMLEscapesPlainTextMarkup(t *testing.T) {
	got := SanitizeRichHTML(`1 < 2 & 3 > 2`)
	if strings.Contains(got, "< 2") || !strings.Contains(got, "&lt;") {
		t.Fatalf("plain text was not escaped: %s", got)
	}
}
