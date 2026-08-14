package handler

import (
	"strings"
	"testing"

	"linlinqi/api/internal/model"
)

func TestApplyResellerProductPresentationSanitizesRichDescription(t *testing.T) {
	dto := publicProductDTO{
		Name:        "平台标题",
		Summary:     "平台摘要",
		Description: "<p>平台描述</p>",
		CoverURL:    "/media/platform.webp",
	}
	presentation := model.ResellerProductPresentation{
		Name:        "分站标题",
		Summary:     "分站摘要",
		Description: `<p onclick="steal()">可见内容<img src="/media/detail.webp" onerror="steal()"><script>alert(1)</script></p>`,
		CoverURL:    "/media/reseller.webp",
	}

	result := applyResellerProductPresentation(dto, presentation)
	if result.Name != presentation.Name || result.Summary != presentation.Summary || result.CoverURL != presentation.CoverURL {
		t.Fatalf("plain reseller presentation fields were not applied: %#v", result)
	}
	for _, forbidden := range []string{"onclick", "onerror", "script", "alert(1)", "steal()"} {
		if strings.Contains(strings.ToLower(result.Description), forbidden) {
			t.Fatalf("reseller description leaked unsafe token %q: %s", forbidden, result.Description)
		}
	}
	for _, expected := range []string{"可见内容", `/media/detail.webp`} {
		if !strings.Contains(result.Description, expected) {
			t.Fatalf("reseller description lost safe content %q: %s", expected, result.Description)
		}
	}
}
