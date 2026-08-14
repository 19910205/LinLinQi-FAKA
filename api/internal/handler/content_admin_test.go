package handler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"linlinqi/api/internal/model"
	"linlinqi/api/pkg/response"
)

func TestNormalizePublicHTTPSURLRejectsPrivateAndCredentialedLocations(t *testing.T) {
	valid, err := normalizePublicHTTPSURL(" https://CDN.Example.com/assets/banner.webp?version=2 ", false)
	if err != nil || valid != "https://cdn.example.com/assets/banner.webp?version=2" {
		t.Fatalf("valid CDN URL was not normalized: value=%q err=%v", valid, err)
	}
	for _, invalid := range []string{
		"http://cdn.example.com/image.png",
		"https://user:password@cdn.example.com/image.png",
		"https://localhost/image.png",
		"https://intranet/image.png",
		"https://assets.lan/image.png",
		"https://127.0.0.1/image.png",
		"https://10.0.0.3/image.png",
		"https://[::1]/image.png",
		"https://cdn.example.com:8443/image.png",
		"https://cdn.example.com/image.png#fragment",
		"https://cdn.example.com/image.png?access_token=secret",
	} {
		if value, err := normalizePublicHTTPSURL(invalid, false); err == nil {
			t.Fatalf("unsafe public URL accepted: input=%q normalized=%q", invalid, value)
		}
	}
	if value, err := normalizePublicHTTPSURL("", true); err != nil || value != "" {
		t.Fatalf("optional empty URL rejected: value=%q err=%v", value, err)
	}
}

func TestNormalizeImageURLAcceptsUploadedMediaAndRejectsDowngradeAttempts(t *testing.T) {
	for input, expected := range map[string]string{
		"/media/sha256/abc/def.jpg":                    "/media/sha256/abc/def.jpg",
		" http://127.0.0.1:8080/media/logo.webp ":      "http://127.0.0.1:8080/media/logo.webp",
		"http://localhost:8080/media/logo.png":          "http://localhost:8080/media/logo.png",
		"https://cdn.example.com/post-cover.webp?v=2":   "https://cdn.example.com/post-cover.webp?v=2",
	} {
		got, err := normalizeImageURL(input, false)
		if err != nil || got != expected {
			t.Fatalf("uploaded media URL rejected: input=%q got=%q err=%v", input, got, err)
		}
	}
	for _, invalid := range []string{
		"http://cdn.example.com/image.png",
		"//evil.example.com/image.png",
		"/admin/../../etc/passwd",
		"https://localhost/image.png",
		"https://10.0.0.3/image.png",
		"https://cdn.example.com/image.png?access_token=secret",
		"http://127.0.0.1:8080/media/logo.png\r\nX-Evil: 1",
	} {
		if value, err := normalizeImageURL(invalid, false); err == nil {
			t.Fatalf("unsafe image URL accepted: input=%q normalized=%q", invalid, value)
		}
	}
}

func TestContentPostRequestNormalizesPublicationAndRejectsExecutableMarkup(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	req := contentPostRequest{
		Title: " 发布指南 ", Slug: " Release-Guide ", Summary: " Markdown 内容 ",
		Content: "# 安全部署\n\n只渲染为文本。", CoverURL: "https://cdn.example.com/post.webp",
		Status: "PUBLISHED", SEO: contentSEORequest{Title: " 发布指南 ", Description: " 生产部署说明 "},
	}
	categoryID, err := req.normalizeAndValidate(now)
	if err != nil || categoryID != nil {
		t.Fatalf("valid post rejected: category=%v err=%v", categoryID, err)
	}
	if req.Title != "发布指南" || req.Slug != "release-guide" || req.Status != "published" || req.PublishedAt == nil || !req.PublishedAt.Equal(now.UTC()) {
		t.Fatalf("post was not normalized: %#v", req)
	}

	draftTime := now.Add(time.Hour)
	draft := req
	draft.Status = "draft"
	draft.PublishedAt = &draftTime
	if _, err := draft.normalizeAndValidate(now); err != nil || draft.PublishedAt != nil {
		t.Fatalf("draft did not clear publication time: time=%v err=%v", draft.PublishedAt, err)
	}

	unsafe := req
	unsafe.Content = "# 标题\n<script>alert(1)</script>"
	if _, err := unsafe.normalizeAndValidate(now); err == nil {
		t.Fatal("executable markup was accepted as article content")
	}
	unsafe.Content = `<img src="x" onerror="alert(1)">`
	if _, err := unsafe.normalizeAndValidate(now); err == nil {
		t.Fatal("event-handler markup was accepted as article content")
	}
}

func TestContentRequestsValidateLevelsPlacementsWindowsAndMedia(t *testing.T) {
	announcement := contentAnnouncementRequest{Title: "维护通知", Content: "预计十分钟。", Level: "IMPORTANT", Enabled: true, Sort: 10}
	if err := announcement.normalizeAndValidate(); err != nil || announcement.Level != "important" {
		t.Fatalf("valid announcement rejected: %#v err=%v", announcement, err)
	}
	announcement.Level = "critical"
	if err := announcement.normalizeAndValidate(); err == nil {
		t.Fatal("unsupported announcement level accepted")
	}

	startsAt := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	endsAt := startsAt.Add(24 * time.Hour)
	banner := contentBannerRequest{Title: "夏季活动", ImageURL: "https://cdn.example.com/banner.webp", TargetURL: "https://shop.example.com/promotion", Placement: "HOME_HERO", Sort: 20, Enabled: true, StartsAt: &startsAt, EndsAt: &endsAt}
	if err := banner.normalizeAndValidate(); err != nil || banner.Placement != "home_hero" {
		t.Fatalf("valid banner rejected: %#v err=%v", banner, err)
	}
	banner.EndsAt = &startsAt
	if err := banner.normalizeAndValidate(); err == nil {
		t.Fatal("non-positive banner window accepted")
	}
	banner.EndsAt = &endsAt
	banner.Placement = "checkout_popup"
	if err := banner.normalizeAndValidate(); err == nil {
		t.Fatal("unsupported banner placement accepted")
	}

	media := contentMediaRequest{PublicURL: "https://cdn.example.com/manual.pdf", AltText: "产品使用手册", FileName: "manual.pdf", MIME: "APPLICATION/PDF", Size: 1024, SHA256: strings.Repeat("a", 64)}
	if err := media.normalizeAndValidate(); err != nil || media.MIME != "application/pdf" {
		t.Fatalf("valid external media rejected: %#v err=%v", media, err)
	}
	if first, second := externalMediaObjectKey(media.PublicURL), externalMediaObjectKey(media.PublicURL); first != second || !strings.HasPrefix(first, "external/") {
		t.Fatalf("external object key is not stable and opaque: %q / %q", first, second)
	}
	media.FileName = "../manual.pdf"
	if err := media.normalizeAndValidate(); err == nil {
		t.Fatal("path-like media filename accepted")
	}
	media.FileName = "manual.png"
	if err := media.normalizeAndValidate(); err == nil {
		t.Fatal("media filename extension mismatching its MIME type accepted")
	}
	media.FileName = "manual.pdf"
	media.Size = 0
	if err := media.normalizeAndValidate(); err == nil {
		t.Fatal("zero-sized external media accepted")
	}
	media.Size = 1024
	media.FileName = "manual.exe"
	media.MIME = "application/octet-stream"
	if err := media.normalizeAndValidate(); err == nil {
		t.Fatal("unsupported media type accepted")
	}
}

func TestContentPostRequestRejectsUnknownTopLevelAndSEOFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{
		`{"title":"指南","slug":"guide","summary":"","content":"正文","cover_url":"","status":"draft","published_at":null,"seo":{"title":"","description":""},"author_id":"server-must-own"}`,
		`{"title":"指南","slug":"guide","summary":"","content":"正文","cover_url":"","status":"draft","published_at":null,"seo":{"title":"","description":"","keywords":"unsafe"}}`,
	} {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest("POST", "/admin/v1/content/posts", strings.NewReader(body))
		context.Request.Header.Set("Content-Type", "application/json")
		var request contentPostRequest
		if err := decodeStrictJSON(context, &request); err == nil {
			t.Fatalf("unknown content field accepted: %s", body)
		}
	}
}

func TestPublicContentResponseUsesMinimalExplicitDTOs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 8, 9, 12, 30, 0, 0, time.UTC)
	authorID := uuid.New()
	post := model.Post{
		Base:        model.Base{ID: uuid.New(), CreatedAt: now.Add(-time.Hour), UpdatedAt: now},
		CategoryID:  &authorID,
		Title:       "交付指南",
		Slug:        "delivery-guide",
		Summary:     "安全交付说明",
		Content:     `<p>安全正文</p><img src="https://cdn.example.com/a.webp" onerror="alert(1)"><a href="jav&#x61;script:alert(2)">危险链接</a><script>alert(3)</script>`,
		CoverURL:    "https://cdn.example.com/guide.webp",
		Status:      "published",
		AuthorID:    &authorID,
		PublishedAt: &now,
		SEO:         `{"title":"后台 SEO"}`,
	}
	banner := model.Banner{
		Base:      model.Base{ID: uuid.New(), CreatedAt: now.Add(-time.Hour), UpdatedAt: now},
		Title:     "首页横幅",
		ImageURL:  "https://cdn.example.com/banner.webp",
		TargetURL: "https://shop.example.com/promotion",
		Placement: "home_hero",
		Sort:      100,
		Enabled:   true,
		StartsAt:  &now,
	}
	announcement := model.Announcement{
		Base:    model.Base{ID: uuid.New(), CreatedAt: now.Add(-time.Hour), UpdatedAt: now},
		Title:   "维护通知",
		Content: `<strong>预计十分钟。</strong><svg onload="alert(4)"></svg><iframe src="https://evil.example"></iframe>`,
		Level:   "important",
		Enabled: true,
		Sort:    20,
	}

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	response.OK(context, gin.H{
		"posts":         []publicContentPostDTO{toPublicContentPostDTO(post)},
		"banners":       []publicContentBannerDTO{toPublicContentBannerDTO(banner)},
		"announcements": []publicContentAnnouncementDTO{toPublicContentAnnouncementDTO(announcement)},
	})
	var envelope struct {
		Data map[string][]map[string]any `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode public content response: %v", err)
	}
	for _, name := range []string{"posts", "banners", "announcements"} {
		if len(envelope.Data[name]) != 1 {
			t.Fatalf("unexpected %s public response: %#v", name, envelope.Data[name])
		}
	}
	assertPublicKeys := func(name string, item map[string]any, required, forbidden []string) {
		t.Helper()
		for _, key := range required {
			if _, exists := item[key]; !exists {
				t.Errorf("%s public response is missing %q: %#v", name, key, item)
			}
		}
		for _, key := range forbidden {
			if _, exists := item[key]; exists {
				t.Errorf("%s public response leaked %q: %#v", name, key, item)
			}
		}
	}
	assertPublicKeys("post", envelope.Data["posts"][0],
		[]string{"id", "title", "slug", "summary", "content", "cover_url", "published_at"},
		[]string{"category_id", "author_id", "status", "seo", "created_at", "updated_at", "deleted_at"},
	)
	postContent, _ := envelope.Data["posts"][0]["content"].(string)
	announcementContent, _ := envelope.Data["announcements"][0]["content"].(string)
	for name, value := range map[string]string{"post": postContent, "announcement": announcementContent} {
		lower := strings.ToLower(value)
		for _, forbidden := range []string{"javascript:", "onerror", "onload", "<script", "<svg", "<iframe"} {
			if strings.Contains(lower, forbidden) {
				t.Errorf("%s public content retained executable markup %q: %s", name, forbidden, value)
			}
		}
	}
	if !strings.Contains(postContent, "安全正文") || !strings.Contains(announcementContent, "预计十分钟") {
		t.Fatalf("public sanitizer removed safe content: post=%q announcement=%q", postContent, announcementContent)
	}
	assertPublicKeys("banner", envelope.Data["banners"][0],
		[]string{"id", "title", "image_url", "target_url", "placement"},
		[]string{"enabled", "sort", "starts_at", "ends_at", "created_at", "updated_at", "deleted_at"},
	)
	assertPublicKeys("announcement", envelope.Data["announcements"][0],
		[]string{"id", "title", "content", "level", "created_at"},
		[]string{"enabled", "sort", "updated_at", "deleted_at"},
	)
}
