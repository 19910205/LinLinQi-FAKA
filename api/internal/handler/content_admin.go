package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/media"
	"linlinqi/api/internal/model"
	"linlinqi/api/pkg/response"
)

var (
	contentSlugPattern        = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)
	contentMIMEPattern        = regexp.MustCompile(`^(?:image/(?:jpeg|png|webp|gif)|video/(?:mp4|webm)|application/pdf)$`)
	contentSHA256Pattern      = regexp.MustCompile(`^[a-f0-9]{64}$`)
	executableMarkupPattern   = regexp.MustCompile(`(?i)<\s*(?:script|iframe|object|embed|style|link|meta)\b|\bon[a-z]+\s*=|javascript\s*:|data\s*:\s*text/html`)
	errContentInvalid         = errors.New("content request is invalid")
	errContentRelation        = errors.New("content relation is invalid")
	errContentInUse           = errors.New("content record is in use")
	errContentUnsafeDelete    = errors.New("content record must be disabled before deletion")
	allowedBannerPlacements   = map[string]bool{"home_hero": true, "home_secondary": true, "content_header": true}
	contentMIMEExtensions     = map[string][]string{"image/jpeg": {".jpg", ".jpeg"}, "image/png": {".png"}, "image/webp": {".webp"}, "image/gif": {".gif"}, "video/mp4": {".mp4"}, "video/webm": {".webm"}, "application/pdf": {".pdf"}}
	sensitiveURLQueryKeywords = []string{"token", "secret", "signature", "credential", "password", "auth", "api_key", "apikey"}
)

func normalizeContentSlug(value string, maximum int) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if utf8.RuneCountInString(value) < 2 || utf8.RuneCountInString(value) > maximum || !contentSlugPattern.MatchString(value) {
		return "", errContentInvalid
	}
	return value, nil
}

func normalizePublicHTTPSURL(value string, allowEmpty bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" && allowEmpty {
		return "", nil
	}
	if value == "" || utf8.RuneCountInString(value) > 1000 || strings.IndexFunc(value, func(r rune) bool { return r <= 0x20 || r == 0x7f }) >= 0 {
		return "", errContentInvalid
	}
	parsed, err := url.Parse(value)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.User != nil || parsed.Hostname() == "" || parsed.Fragment != "" || parsed.Port() != "" {
		return "", errContentInvalid
	}
	hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if hostname == "localhost" ||
		(!strings.Contains(hostname, ".") && net.ParseIP(hostname) == nil) ||
		strings.HasSuffix(hostname, ".localhost") ||
		strings.HasSuffix(hostname, ".local") ||
		strings.HasSuffix(hostname, ".internal") ||
		strings.HasSuffix(hostname, ".lan") ||
		strings.HasSuffix(hostname, ".home") ||
		strings.HasSuffix(hostname, ".home.arpa") ||
		strings.HasSuffix(hostname, ".test") ||
		strings.HasSuffix(hostname, ".invalid") {
		return "", errContentInvalid
	}
	if address := net.ParseIP(hostname); address != nil && (address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsUnspecified() || address.IsMulticast()) {
		return "", errContentInvalid
	}
	for key := range parsed.Query() {
		lower := strings.ToLower(key)
		for _, keyword := range sensitiveURLQueryKeywords {
			if strings.Contains(lower, keyword) {
				return "", errContentInvalid
			}
		}
	}
	parsed.Scheme = "https"
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String(), nil
}

// normalizeImageURL accepts application-owned relative paths so uploaded
// media and internal links do not need an external CDN. Loopback HTTP is
// accepted only for the native local deployment; every other absolute URL
// still goes through the public HTTPS policy. The value is only stored and
// rendered client-side, so accepting loopback here does not enable SSRF.
func normalizeImageURL(value string, allowEmpty bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" && allowEmpty {
		return "", nil
	}
	if strings.IndexFunc(value, func(r rune) bool { return r <= 0x20 || r == 0x7f }) >= 0 {
		return "", errContentInvalid
	}
	if strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//") && !strings.Contains(value, "\\") && !strings.Contains(value, "..") {
		parsed, err := url.Parse(value)
		if err == nil && parsed.IsAbs() == false && parsed.Host == "" {
			return parsed.String(), nil
		}
	}
	parsed, err := url.Parse(value)
	if err == nil && strings.EqualFold(parsed.Scheme, "http") && parsed.User == nil && parsed.Port() != "" && parsed.Fragment == "" {
		host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
		address := net.ParseIP(host)
		if host == "localhost" || (address != nil && address.IsLoopback()) {
			return parsed.String(), nil
		}
	}
	return normalizePublicHTTPSURL(value, allowEmpty)
}

// normalizeBannerURL keeps the same acceptance policy as normalizeImageURL so
// uploaded media and internal campaign links do not need an external CDN.
func normalizeBannerURL(value string, allowEmpty bool) (string, error) {
	return normalizeImageURL(value, allowEmpty)
}

func containsExecutableMarkup(value string) bool {
	return executableMarkupPattern.MatchString(value)
}

type contentCategoryRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
	Sort int    `json:"sort"`
}

func (r *contentCategoryRequest) normalizeAndValidate() error {
	r.Name = strings.TrimSpace(r.Name)
	slug, err := normalizeContentSlug(r.Slug, 120)
	if err != nil || utf8.RuneCountInString(r.Name) < 1 || utf8.RuneCountInString(r.Name) > 100 || r.Sort < 0 || r.Sort > 1_000_000 {
		return errContentInvalid
	}
	r.Slug = slug
	return nil
}

type adminContentCategoryDTO struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Sort      int       `json:"sort"`
	PostCount int64     `json:"post_count"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (h Handler) AdminContentCategories(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.PostCategory{})
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("name ILIKE ? OR slug ILIKE ?", like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50100, "error.article_category_list_fetch_failed")
		return
	}
	var categories []model.PostCategory
	if err := query.Order("sort DESC, created_at ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&categories).Error; err != nil {
		response.Error(c, 500, 50100, "error.article_category_list_fetch_failed")
		return
	}
	categoryIDs := make([]uuid.UUID, 0, len(categories))
	for _, category := range categories {
		categoryIDs = append(categoryIDs, category.ID)
	}
	type categoryPostCount struct {
		CategoryID uuid.UUID `gorm:"column:category_id"`
		PostCount  int64     `gorm:"column:post_count"`
	}
	var groupedCounts []categoryPostCount
	if len(categoryIDs) > 0 {
		if err := h.DB.Model(&model.Post{}).Select("category_id, COUNT(*) AS post_count").Where("category_id IN ?", categoryIDs).Group("category_id").Scan(&groupedCounts).Error; err != nil {
			response.Error(c, 500, 50100, "error.article_category_stats_failed")
			return
		}
	}
	postCounts := make(map[uuid.UUID]int64, len(groupedCounts))
	for _, count := range groupedCounts {
		postCounts[count.CategoryID] = count.PostCount
	}
	items := make([]adminContentCategoryDTO, 0, len(categories))
	for _, category := range categories {
		items = append(items, adminContentCategoryDTO{ID: category.ID, Name: category.Name, Slug: category.Slug, Sort: category.Sort, PostCount: postCounts[category.ID], CreatedAt: category.CreatedAt, UpdatedAt: category.UpdatedAt})
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) CreateContentCategory(c *gin.Context) {
	reason, ok := requireAdminChangeReason(c, "创建文章分类")
	if !ok {
		return
	}
	var req contentCategoryRequest
	if decodeStrictJSON(c, &req) != nil || req.normalizeAndValidate() != nil {
		response.Error(c, 422, 42340, "error.category_name_id_sort_invalid")
		return
	}
	item := model.PostCategory{Name: req.Name, Slug: req.Slug, Sort: req.Sort}
	if err := h.DB.Create(&item).Error; err != nil {
		response.Error(c, 409, 409110, "error.article_category_slug_exists")
		return
	}
	h.audit(c, "content-category.create", "post_category", item.ID.String(), reason)
	response.Created(c, adminContentCategoryDTO{ID: item.ID, Name: item.Name, Slug: item.Slug, Sort: item.Sort, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt})
}

func (h Handler) UpdateContentCategory(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42341, "error.article_category_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "更新文章分类")
	if !ok {
		return
	}
	var req contentCategoryRequest
	if decodeStrictJSON(c, &req) != nil || req.normalizeAndValidate() != nil {
		response.Error(c, 422, 42340, "error.category_name_id_sort_invalid")
		return
	}
	var item model.PostCategory
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", id).Error; err != nil {
			return err
		}
		return tx.Model(&item).Updates(map[string]any{"name": req.Name, "slug": req.Slug, "sort": req.Sort}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40500, "error.article_category_not_found")
		return
	}
	if err != nil {
		response.Error(c, 409, 409110, "error.article_category_slug_conflict")
		return
	}
	h.audit(c, "content-category.update", "post_category", item.ID.String(), reason)
	h.DB.First(&item, "id = ?", item.ID)
	response.OK(c, adminContentCategoryDTO{ID: item.ID, Name: item.Name, Slug: item.Slug, Sort: item.Sort, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt})
}

func (h Handler) DeleteContentCategory(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42341, "error.article_category_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "删除文章分类")
	if !ok {
		return
	}
	var item model.PostCategory
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", id).Error; err != nil {
			return err
		}
		var posts int64
		if err := tx.Model(&model.Post{}).Where("category_id = ?", item.ID).Count(&posts).Error; err != nil {
			return err
		}
		if posts > 0 {
			return errContentInUse
		}
		return tx.Delete(&item).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40500, "error.article_category_not_found")
		return
	}
	if errors.Is(err, errContentInUse) {
		response.Error(c, 409, 409111, "error.category_has_articles")
		return
	}
	if err != nil {
		response.Error(c, 500, 50101, "error.article_category_delete_failed")
		return
	}
	h.audit(c, "content-category.delete", "post_category", item.ID.String(), reason)
	response.OK(c, gin.H{"deleted": true})
}

type contentSEORequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type contentPostRequest struct {
	CategoryID  *string           `json:"category_id"`
	Title       string            `json:"title"`
	Slug        string            `json:"slug"`
	Summary     string            `json:"summary"`
	Content     string            `json:"content"`
	CoverURL    string            `json:"cover_url"`
	Status      string            `json:"status"`
	PublishedAt *time.Time        `json:"published_at"`
	SEO         contentSEORequest `json:"seo"`
}

func (r *contentPostRequest) normalizeAndValidate(now time.Time) (*uuid.UUID, error) {
	r.Title = strings.TrimSpace(r.Title)
	r.Summary = strings.TrimSpace(r.Summary)
	r.Content = strings.TrimSpace(r.Content)
	r.Status = strings.ToLower(strings.TrimSpace(r.Status))
	r.SEO.Title = strings.TrimSpace(r.SEO.Title)
	r.SEO.Description = strings.TrimSpace(r.SEO.Description)
	slug, err := normalizeContentSlug(r.Slug, 240)
	if err != nil || utf8.RuneCountInString(r.Title) < 1 || utf8.RuneCountInString(r.Title) > 220 || utf8.RuneCountInString(r.Summary) > 600 || utf8.RuneCountInString(r.Content) < 1 || utf8.RuneCountInString(r.Content) > 500_000 || utf8.RuneCountInString(r.SEO.Title) > 120 || utf8.RuneCountInString(r.SEO.Description) > 320 || containsExecutableMarkup(r.Content) {
		return nil, errContentInvalid
	}
	r.Slug = slug
	coverURL, err := normalizeImageURL(r.CoverURL, true)
	if err != nil || utf8.RuneCountInString(coverURL) > 500 {
		return nil, errContentInvalid
	}
	r.CoverURL = coverURL
	var categoryID *uuid.UUID
	if r.CategoryID != nil && strings.TrimSpace(*r.CategoryID) != "" {
		parsed, err := uuid.Parse(strings.TrimSpace(*r.CategoryID))
		if err != nil {
			return nil, errContentInvalid
		}
		categoryID = &parsed
	}
	if r.Status != "draft" && r.Status != "published" {
		return nil, errContentInvalid
	}
	if r.Status == "draft" {
		r.PublishedAt = nil
	} else if r.PublishedAt == nil {
		publishedAt := now.UTC()
		r.PublishedAt = &publishedAt
	} else {
		publishedAt := r.PublishedAt.UTC()
		r.PublishedAt = &publishedAt
	}
	return categoryID, nil
}

type adminContentPostDTO struct {
	ID           uuid.UUID         `json:"id"`
	CategoryID   *uuid.UUID        `json:"category_id,omitempty"`
	CategoryName string            `json:"category_name"`
	Title        string            `json:"title"`
	Slug         string            `json:"slug"`
	Summary      string            `json:"summary"`
	Content      string            `json:"content"`
	CoverURL     string            `json:"cover_url"`
	Status       string            `json:"status"`
	AuthorID     *uuid.UUID        `json:"author_id,omitempty"`
	AuthorName   string            `json:"author_name"`
	PublishedAt  *time.Time        `json:"published_at,omitempty"`
	SEO          contentSEORequest `json:"seo"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

func parseContentSEO(value string) contentSEORequest {
	var seo contentSEORequest
	if json.Unmarshal([]byte(value), &seo) != nil {
		return contentSEORequest{}
	}
	return seo
}

func contentSEOJSON(value contentSEORequest) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func (h Handler) contentPostDTOs(posts []model.Post) []adminContentPostDTO {
	categoryIDs := make([]uuid.UUID, 0, len(posts))
	authorIDs := make([]uuid.UUID, 0, len(posts))
	for _, post := range posts {
		if post.CategoryID != nil {
			categoryIDs = append(categoryIDs, *post.CategoryID)
		}
		if post.AuthorID != nil {
			authorIDs = append(authorIDs, *post.AuthorID)
		}
	}
	var categories []model.PostCategory
	var admins []model.Admin
	if len(categoryIDs) > 0 {
		h.DB.Select("id", "name").Where("id IN ?", categoryIDs).Find(&categories)
	}
	if len(authorIDs) > 0 {
		h.DB.Select("id", "name", "username").Where("id IN ?", authorIDs).Find(&admins)
	}
	categoryNames := map[uuid.UUID]string{}
	authorNames := map[uuid.UUID]string{}
	for _, item := range categories {
		categoryNames[item.ID] = item.Name
	}
	for _, item := range admins {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = item.Username
		}
		authorNames[item.ID] = name
	}
	items := make([]adminContentPostDTO, 0, len(posts))
	for _, post := range posts {
		dto := adminContentPostDTO{ID: post.ID, CategoryID: post.CategoryID, Title: post.Title, Slug: post.Slug, Summary: post.Summary, Content: post.Content, CoverURL: post.CoverURL, Status: post.Status, AuthorID: post.AuthorID, PublishedAt: post.PublishedAt, SEO: parseContentSEO(post.SEO), CreatedAt: post.CreatedAt, UpdatedAt: post.UpdatedAt}
		if post.CategoryID != nil {
			dto.CategoryName = categoryNames[*post.CategoryID]
		}
		if post.AuthorID != nil {
			dto.AuthorName = authorNames[*post.AuthorID]
		}
		items = append(items, dto)
	}
	return items
}

func (h Handler) AdminContentPosts(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.Post{})
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("title ILIKE ? OR slug ILIKE ? OR summary ILIKE ?", like, like, like)
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		if status != "draft" && status != "published" {
			response.Error(c, 422, 42342, "error.article_status_filter_invalid")
			return
		}
		query = query.Where("status = ?", status)
	}
	if categoryID := strings.TrimSpace(c.Query("category_id")); categoryID != "" {
		parsed, err := uuid.Parse(categoryID)
		if err != nil {
			response.Error(c, 422, 42343, "error.article_category_filter_id_invalid")
			return
		}
		query = query.Where("category_id = ?", parsed)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50102, "error.article_list_fetch_failed")
		return
	}
	var posts []model.Post
	if err := query.Order("updated_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&posts).Error; err != nil {
		response.Error(c, 500, 50102, "error.article_list_fetch_failed")
		return
	}
	response.Page(c, h.contentPostDTOs(posts), total, page, pageSize)
}

func ensurePostCategory(tx *gorm.DB, categoryID *uuid.UUID) error {
	if categoryID == nil {
		return nil
	}
	var category model.PostCategory
	if err := tx.Select("id").First(&category, "id = ?", *categoryID).Error; err != nil {
		return errContentRelation
	}
	return nil
}

func (h Handler) CreateContentPost(c *gin.Context) {
	reason, ok := requireAdminChangeReason(c, "创建文章")
	if !ok {
		return
	}
	var req contentPostRequest
	categoryID, err := func() (*uuid.UUID, error) {
		if decodeStrictJSON(c, &req) != nil {
			return nil, errContentInvalid
		}
		return req.normalizeAndValidate(time.Now())
	}()
	if err != nil {
		response.Error(c, 422, 42344, "error.article_fields_invalid")
		return
	}
	authorID, _ := uuid.Parse(c.GetString("subject"))
	item := model.Post{CategoryID: categoryID, Title: req.Title, Slug: req.Slug, Summary: req.Summary, Content: req.Content, CoverURL: req.CoverURL, Status: req.Status, AuthorID: &authorID, PublishedAt: req.PublishedAt, SEO: contentSEOJSON(req.SEO)}
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := ensurePostCategory(tx, categoryID); err != nil {
			return err
		}
		return tx.Create(&item).Error
	})
	if errors.Is(err, errContentRelation) {
		response.Error(c, 422, 42345, "error.article_category_not_found")
		return
	}
	if err != nil {
		response.Error(c, 409, 409112, "error.article_slug_exists_or_save_failed")
		return
	}
	h.audit(c, "post.create", "post", item.ID.String(), reason)
	response.Created(c, h.contentPostDTOs([]model.Post{item})[0])
}

func (h Handler) UpdateContentPost(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42346, "error.article_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "更新文章")
	if !ok {
		return
	}
	var req contentPostRequest
	categoryID, err := func() (*uuid.UUID, error) {
		if decodeStrictJSON(c, &req) != nil {
			return nil, errContentInvalid
		}
		return req.normalizeAndValidate(time.Now())
	}()
	if err != nil {
		response.Error(c, 422, 42344, "error.article_fields_invalid")
		return
	}
	var item model.Post
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", id).Error; err != nil {
			return err
		}
		if err := ensurePostCategory(tx, categoryID); err != nil {
			return err
		}
		updates := map[string]any{"category_id": categoryID, "title": req.Title, "slug": req.Slug, "summary": req.Summary, "content": req.Content, "cover_url": req.CoverURL, "status": req.Status, "published_at": req.PublishedAt, "seo": contentSEOJSON(req.SEO)}
		return tx.Model(&item).Updates(updates).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40501, "error.article_not_found")
		return
	}
	if errors.Is(err, errContentRelation) {
		response.Error(c, 422, 42345, "error.article_category_not_found")
		return
	}
	if err != nil {
		response.Error(c, 409, 409112, "error.article_slug_exists_or_save_failed")
		return
	}
	h.audit(c, "post.update", "post", item.ID.String(), reason)
	h.DB.First(&item, "id = ?", item.ID)
	response.OK(c, h.contentPostDTOs([]model.Post{item})[0])
}

func (h Handler) DeleteContentPost(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42346, "error.article_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "删除文章")
	if !ok {
		return
	}
	var item model.Post
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", id).Error; err != nil {
			return err
		}
		if item.Status != "draft" {
			return errContentUnsafeDelete
		}
		return tx.Delete(&item).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40501, "error.article_not_found")
		return
	}
	if errors.Is(err, errContentUnsafeDelete) {
		response.Error(c, 409, 409113, "error.published_article_must_be_draft_first")
		return
	}
	if err != nil {
		response.Error(c, 500, 50103, "error.article_delete_failed")
		return
	}
	h.audit(c, "post.delete", "post", item.ID.String(), reason)
	response.OK(c, gin.H{"deleted": true})
}

type contentAnnouncementRequest struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Level   string `json:"level"`
	Enabled bool   `json:"enabled"`
	Sort    int    `json:"sort"`
}

func (r *contentAnnouncementRequest) normalizeAndValidate() error {
	r.Title = strings.TrimSpace(r.Title)
	r.Content = strings.TrimSpace(r.Content)
	r.Level = strings.ToLower(strings.TrimSpace(r.Level))
	if utf8.RuneCountInString(r.Title) < 1 || utf8.RuneCountInString(r.Title) > 160 || utf8.RuneCountInString(r.Content) < 1 || utf8.RuneCountInString(r.Content) > 20_000 || (r.Level != "info" && r.Level != "important" && r.Level != "warning") || r.Sort < 0 || r.Sort > 1_000_000 || containsExecutableMarkup(r.Content) {
		return errContentInvalid
	}
	return nil
}

type adminContentAnnouncementDTO struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Level     string    `json:"level"`
	Enabled   bool      `json:"enabled"`
	Sort      int       `json:"sort"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func announcementDTO(item model.Announcement) adminContentAnnouncementDTO {
	return adminContentAnnouncementDTO{ID: item.ID, Title: item.Title, Content: item.Content, Level: item.Level, Enabled: item.Enabled, Sort: item.Sort, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}

func (h Handler) AdminContentAnnouncements(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.Announcement{})
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("title ILIKE ? OR content ILIKE ?", like, like)
	}
	if level := strings.TrimSpace(c.Query("level")); level != "" {
		if level != "info" && level != "important" && level != "warning" {
			response.Error(c, 422, 42347, "error.announcement_level_filter_invalid")
			return
		}
		query = query.Where("level = ?", level)
	}
	if enabled := strings.TrimSpace(c.Query("enabled")); enabled != "" {
		if enabled != "true" && enabled != "false" {
			response.Error(c, 422, 42348, "error.announcement_status_filter_invalid")
			return
		}
		query = query.Where("enabled = ?", enabled == "true")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50104, "error.announcement_list_fetch_failed")
		return
	}
	var records []model.Announcement
	if err := query.Order("sort DESC, updated_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&records).Error; err != nil {
		response.Error(c, 500, 50104, "error.announcement_list_fetch_failed")
		return
	}
	items := make([]adminContentAnnouncementDTO, 0, len(records))
	for _, item := range records {
		items = append(items, announcementDTO(item))
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) saveContentAnnouncement(c *gin.Context, id *uuid.UUID) {
	action := "创建公告"
	if id != nil {
		action = "更新公告"
	}
	reason, ok := requireAdminChangeReason(c, action)
	if !ok {
		return
	}
	var req contentAnnouncementRequest
	if decodeStrictJSON(c, &req) != nil || req.normalizeAndValidate() != nil {
		response.Error(c, 422, 42349, "error.announcement_fields_invalid")
		return
	}
	var item model.Announcement
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if id == nil {
			item = model.Announcement{Title: req.Title, Content: req.Content, Level: req.Level, Enabled: req.Enabled, Sort: req.Sort}
			return createWithExplicitColumns(tx, &item, map[string]any{"enabled": req.Enabled})
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", *id).Error; err != nil {
			return err
		}
		return tx.Model(&item).Updates(map[string]any{"title": req.Title, "content": req.Content, "level": req.Level, "enabled": req.Enabled, "sort": req.Sort}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40502, "error.announcement_not_found")
		return
	}
	if err != nil {
		response.Error(c, 500, 50105, "error.announcement_save_failed")
		return
	}
	item.Enabled = req.Enabled
	auditAction := "announcement.create"
	if id != nil {
		auditAction = "announcement.update"
		h.DB.First(&item, "id = ?", item.ID)
	}
	h.audit(c, auditAction, "announcement", item.ID.String(), reason)
	if id == nil {
		response.Created(c, announcementDTO(item))
	} else {
		response.OK(c, announcementDTO(item))
	}
}

func (h Handler) CreateContentAnnouncement(c *gin.Context) {
	h.saveContentAnnouncement(c, nil)
}

func (h Handler) UpdateContentAnnouncement(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42350, "error.announcement_id_invalid")
		return
	}
	h.saveContentAnnouncement(c, &id)
}

func (h Handler) DeleteContentAnnouncement(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42350, "error.announcement_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "删除公告")
	if !ok {
		return
	}
	var item model.Announcement
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", id).Error; err != nil {
			return err
		}
		if item.Enabled {
			return errContentUnsafeDelete
		}
		return tx.Delete(&item).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40502, "error.announcement_not_found")
		return
	}
	if errors.Is(err, errContentUnsafeDelete) {
		response.Error(c, 409, 409114, "error.active_announcement_must_be_disabled_first")
		return
	}
	if err != nil {
		response.Error(c, 500, 50106, "error.announcement_delete_failed")
		return
	}
	h.audit(c, "announcement.delete", "announcement", item.ID.String(), reason)
	response.OK(c, gin.H{"deleted": true})
}

type contentBannerRequest struct {
	Title     string     `json:"title"`
	ImageURL  string     `json:"image_url"`
	TargetURL string     `json:"target_url"`
	Placement string     `json:"placement"`
	Sort      int        `json:"sort"`
	Enabled   bool       `json:"enabled"`
	StartsAt  *time.Time `json:"starts_at"`
	EndsAt    *time.Time `json:"ends_at"`
}

func (r *contentBannerRequest) normalizeAndValidate() error {
	r.Title = strings.TrimSpace(r.Title)
	r.Placement = strings.ToLower(strings.TrimSpace(r.Placement))
	imageURL, err := normalizeBannerURL(r.ImageURL, false)
	if err != nil || utf8.RuneCountInString(imageURL) > 500 {
		return errContentInvalid
	}
	targetURL, err := normalizeBannerURL(r.TargetURL, true)
	if err != nil || utf8.RuneCountInString(targetURL) > 500 || utf8.RuneCountInString(r.Title) < 1 || utf8.RuneCountInString(r.Title) > 160 || !allowedBannerPlacements[r.Placement] || r.Sort < 0 || r.Sort > 1_000_000 {
		return errContentInvalid
	}
	r.ImageURL, r.TargetURL = imageURL, targetURL
	if r.StartsAt != nil {
		value := r.StartsAt.UTC()
		r.StartsAt = &value
	}
	if r.EndsAt != nil {
		value := r.EndsAt.UTC()
		r.EndsAt = &value
	}
	if r.StartsAt != nil && r.EndsAt != nil && !r.EndsAt.After(*r.StartsAt) {
		return errContentInvalid
	}
	return nil
}

type adminContentBannerDTO struct {
	ID        uuid.UUID  `json:"id"`
	Title     string     `json:"title"`
	ImageURL  string     `json:"image_url"`
	TargetURL string     `json:"target_url"`
	Placement string     `json:"placement"`
	Sort      int        `json:"sort"`
	Enabled   bool       `json:"enabled"`
	StartsAt  *time.Time `json:"starts_at,omitempty"`
	EndsAt    *time.Time `json:"ends_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func bannerDTO(item model.Banner) adminContentBannerDTO {
	return adminContentBannerDTO{ID: item.ID, Title: item.Title, ImageURL: item.ImageURL, TargetURL: item.TargetURL, Placement: item.Placement, Sort: item.Sort, Enabled: item.Enabled, StartsAt: item.StartsAt, EndsAt: item.EndsAt, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}

func (h Handler) AdminContentBanners(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.Banner{})
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("title ILIKE ? OR image_url ILIKE ?", like, like)
	}
	if placement := strings.TrimSpace(c.Query("placement")); placement != "" {
		if !allowedBannerPlacements[placement] {
			response.Error(c, 422, 42351, "error.banner_position_filter_invalid")
			return
		}
		query = query.Where("placement = ?", placement)
	}
	if enabled := strings.TrimSpace(c.Query("enabled")); enabled != "" {
		if enabled != "true" && enabled != "false" {
			response.Error(c, 422, 42352, "error.banner_status_filter_invalid")
			return
		}
		query = query.Where("enabled = ?", enabled == "true")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50107, "error.banner_list_fetch_failed")
		return
	}
	var records []model.Banner
	if err := query.Order("sort DESC, updated_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&records).Error; err != nil {
		response.Error(c, 500, 50107, "error.banner_list_fetch_failed")
		return
	}
	items := make([]adminContentBannerDTO, 0, len(records))
	for _, item := range records {
		items = append(items, bannerDTO(item))
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) saveContentBanner(c *gin.Context, id *uuid.UUID) {
	action := "创建横幅"
	if id != nil {
		action = "更新横幅"
	}
	reason, ok := requireAdminChangeReason(c, action)
	if !ok {
		return
	}
	var req contentBannerRequest
	if decodeStrictJSON(c, &req) != nil || req.normalizeAndValidate() != nil {
		response.Error(c, 422, 42353, "error.banner_fields_invalid")
		return
	}
	var item model.Banner
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if id == nil {
			item = model.Banner{Title: req.Title, ImageURL: req.ImageURL, TargetURL: req.TargetURL, Placement: req.Placement, Sort: req.Sort, Enabled: req.Enabled, StartsAt: req.StartsAt, EndsAt: req.EndsAt}
			return createWithExplicitColumns(tx, &item, map[string]any{"enabled": req.Enabled})
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", *id).Error; err != nil {
			return err
		}
		return tx.Model(&item).Updates(map[string]any{"title": req.Title, "image_url": req.ImageURL, "target_url": req.TargetURL, "placement": req.Placement, "sort": req.Sort, "enabled": req.Enabled, "starts_at": req.StartsAt, "ends_at": req.EndsAt}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40503, "error.banner_not_found")
		return
	}
	if err != nil {
		response.Error(c, 500, 50108, "error.banner_save_failed")
		return
	}
	item.Enabled = req.Enabled
	auditAction := "banner.create"
	if id != nil {
		auditAction = "banner.update"
		h.DB.First(&item, "id = ?", item.ID)
	}
	h.audit(c, auditAction, "banner", item.ID.String(), reason)
	if id == nil {
		response.Created(c, bannerDTO(item))
	} else {
		response.OK(c, bannerDTO(item))
	}
}

func (h Handler) CreateContentBanner(c *gin.Context) {
	h.saveContentBanner(c, nil)
}

func (h Handler) UpdateContentBanner(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42354, "error.banner_id_invalid")
		return
	}
	h.saveContentBanner(c, &id)
}

func (h Handler) DeleteContentBanner(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42354, "error.banner_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "删除横幅")
	if !ok {
		return
	}
	var item model.Banner
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", id).Error; err != nil {
			return err
		}
		if item.Enabled {
			return errContentUnsafeDelete
		}
		return tx.Delete(&item).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40503, "error.banner_not_found")
		return
	}
	if errors.Is(err, errContentUnsafeDelete) {
		response.Error(c, 409, 409115, "error.active_banner_must_be_disabled_first")
		return
	}
	if err != nil {
		response.Error(c, 500, 50109, "error.banner_delete_failed")
		return
	}
	h.audit(c, "banner.delete", "banner", item.ID.String(), reason)
	response.OK(c, gin.H{"deleted": true})
}

type contentMediaRequest struct {
	PublicURL string `json:"public_url"`
	AltText   string `json:"alt_text"`
	FileName  string `json:"file_name"`
	MIME      string `json:"mime"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
}

func (r *contentMediaRequest) normalizeAndValidate() error {
	publicURL, err := normalizePublicHTTPSURL(r.PublicURL, false)
	r.PublicURL = publicURL
	r.AltText = strings.TrimSpace(r.AltText)
	r.FileName = strings.TrimSpace(r.FileName)
	r.MIME = strings.ToLower(strings.TrimSpace(r.MIME))
	r.SHA256 = strings.ToLower(strings.TrimSpace(r.SHA256))
	validExtension := false
	fileName := strings.ToLower(r.FileName)
	for _, extension := range contentMIMEExtensions[r.MIME] {
		if strings.HasSuffix(fileName, extension) {
			validExtension = true
			break
		}
	}
	if err != nil || utf8.RuneCountInString(r.AltText) < 1 || utf8.RuneCountInString(r.AltText) > 300 || utf8.RuneCountInString(r.FileName) < 1 || utf8.RuneCountInString(r.FileName) > 255 || strings.ContainsAny(r.FileName, `/\\`) || strings.IndexFunc(r.FileName, func(value rune) bool { return value < 0x20 || value == 0x7f }) >= 0 || !contentMIMEPattern.MatchString(r.MIME) || !validExtension || r.Size < 1 || r.Size > 10_000_000_000_000 || (r.SHA256 != "" && !contentSHA256Pattern.MatchString(r.SHA256)) {
		return errContentInvalid
	}
	return nil
}

func externalMediaObjectKey(publicURL string) string {
	digest := sha256.Sum256([]byte(publicURL))
	return "external/" + hex.EncodeToString(digest[:])
}

type adminContentMediaDTO struct {
	ID           uuid.UUID  `json:"id"`
	PublicURL    string     `json:"public_url"`
	AltText      string     `json:"alt_text"`
	FileName     string     `json:"file_name"`
	MIME         string     `json:"mime"`
	Size         int64      `json:"size"`
	SHA256       string     `json:"sha256"`
	UploadedBy   *uuid.UUID `json:"uploaded_by,omitempty"`
	UploaderName string     `json:"uploader_name"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (h Handler) contentMediaDTOs(records []model.MediaAsset) []adminContentMediaDTO {
	adminIDs := make([]uuid.UUID, 0, len(records))
	for _, item := range records {
		if item.UploadedBy != nil {
			adminIDs = append(adminIDs, *item.UploadedBy)
		}
	}
	var admins []model.Admin
	if len(adminIDs) > 0 {
		h.DB.Select("id", "name", "username").Where("id IN ?", adminIDs).Find(&admins)
	}
	names := map[uuid.UUID]string{}
	for _, admin := range admins {
		name := strings.TrimSpace(admin.Name)
		if name == "" {
			name = admin.Username
		}
		names[admin.ID] = name
	}
	items := make([]adminContentMediaDTO, 0, len(records))
	for _, item := range records {
		dto := adminContentMediaDTO{ID: item.ID, PublicURL: item.PublicURL, AltText: item.AltText, FileName: item.FileName, MIME: item.MIME, Size: item.Size, SHA256: item.SHA256, UploadedBy: item.UploadedBy, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
		if item.UploadedBy != nil {
			dto.UploaderName = names[*item.UploadedBy]
		}
		items = append(items, dto)
	}
	return items
}

func (h Handler) AdminContentMedia(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.MediaAsset{}).Where("disk = ?", "external")
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("file_name ILIKE ? OR alt_text ILIKE ? OR public_url ILIKE ?", like, like, like)
	}
	if kind := strings.TrimSpace(c.Query("kind")); kind != "" {
		switch kind {
		case "image":
			query = query.Where("mime LIKE ?", "image/%")
		case "video":
			query = query.Where("mime LIKE ?", "video/%")
		case "document":
			query = query.Where("mime = ?", "application/pdf")
		default:
			response.Error(c, 422, 42355, "error.media_type_filter_invalid")
			return
		}
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50110, "error.media_asset_list_fetch_failed")
		return
	}
	var records []model.MediaAsset
	if err := query.Order("updated_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&records).Error; err != nil {
		response.Error(c, 500, 50110, "error.media_asset_list_fetch_failed")
		return
	}
	response.Page(c, h.contentMediaDTOs(records), total, page, pageSize)
}

func (h Handler) saveContentMedia(c *gin.Context, id *uuid.UUID) {
	action := "登记外部媒体"
	if id != nil {
		action = "更新外部媒体"
	}
	reason, ok := requireAdminChangeReason(c, action)
	if !ok {
		return
	}
	var req contentMediaRequest
	if decodeStrictJSON(c, &req) != nil || req.normalizeAndValidate() != nil {
		response.Error(c, 422, 42356, "error.media_fields_invalid")
		return
	}
	var item model.MediaAsset
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if id == nil {
			uploaderID, _ := uuid.Parse(c.GetString("subject"))
			item = model.MediaAsset{Disk: "external", ObjectKey: externalMediaObjectKey(req.PublicURL), PublicURL: req.PublicURL, AltText: req.AltText, FileName: req.FileName, MIME: req.MIME, Size: req.Size, SHA256: req.SHA256, UploadedBy: &uploaderID, Visibility: "public"}
			return tx.Create(&item).Error
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("disk = ?", "external").First(&item, "id = ?", *id).Error; err != nil {
			return err
		}
		if item.PublicURL != req.PublicURL {
			var posts, banners int64
			if err := tx.Model(&model.Post{}).Where("cover_url = ?", item.PublicURL).Count(&posts).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.Banner{}).Where("image_url = ?", item.PublicURL).Count(&banners).Error; err != nil {
				return err
			}
			if posts+banners > 0 {
				return errContentInUse
			}
		}
		updates := map[string]any{"object_key": externalMediaObjectKey(req.PublicURL), "public_url": req.PublicURL, "alt_text": req.AltText, "file_name": req.FileName, "mime": req.MIME, "size": req.Size, "sha256": req.SHA256, "visibility": "public"}
		return tx.Model(&item).Updates(updates).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40504, "error.external_media_not_found")
		return
	}
	if errors.Is(err, errContentInUse) {
		response.Error(c, 409, 409117, "error.media_referenced_cannot_change_public_url")
		return
	}
	if err != nil {
		response.Error(c, 409, 409116, "error.public_url_registered_or_media_save_failed")
		return
	}
	auditAction := "media.register"
	if id != nil {
		auditAction = "media.update"
		h.DB.First(&item, "id = ?", item.ID)
	}
	h.audit(c, auditAction, "media_asset", item.ID.String(), reason)
	dto := h.contentMediaDTOs([]model.MediaAsset{item})[0]
	if id == nil {
		response.Created(c, dto)
	} else {
		response.OK(c, dto)
	}
}

func (h Handler) CreateContentMedia(c *gin.Context) {
	h.saveContentMedia(c, nil)
}

// uploadAdminImage stores a validated image in content-addressed local storage
// and registers it as a public media asset. The browser never needs to know
// STORAGE_ROOT or construct a URL by hand. A non-empty audit reason is
// mandatory for every write.
func (h Handler) uploadAdminImage(c *gin.Context, action, auditAction string) (adminContentMediaDTO, bool) {
	reason, ok := requireAdminChangeReason(c, action)
	if !ok {
		return adminContentMediaDTO{}, false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.Cfg.MediaMaxImageBytes+(1<<20))
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.Error(c, 422, 42361, "error.catalog_media_file_required")
		return adminContentMediaDTO{}, false
	}
	defer file.Close()
	stored, err := media.New(h.Cfg).PutImage(file, header.Filename)
	if err != nil {
		response.Error(c, 422, 42362, "error.catalog_media_image_invalid")
		return adminContentMediaDTO{}, false
	}
	uploaderID, _ := uuid.Parse(c.GetString("subject"))
	asset := model.MediaAsset{Disk: "local", ObjectKey: stored.ObjectKey, PublicURL: stored.PublicURL, AltText: strings.TrimSpace(c.PostForm("alt_text")), FileName: stored.FileName, MIME: stored.MIME, Size: stored.Size, SHA256: stored.SHA256, UploadedBy: &uploaderID, Visibility: "public"}
	if asset.AltText == "" || utf8.RuneCountInString(asset.AltText) > 300 {
		response.Error(c, 422, 42360, "error.media_fields_invalid")
		return adminContentMediaDTO{}, false
	}
	if err := h.DB.Where("disk = ? AND sha256 = ? AND mime = ?", "local", asset.SHA256, asset.MIME).FirstOrCreate(&asset).Error; err != nil {
		response.Error(c, 500, 50120, "error.catalog_media_save_failed")
		return adminContentMediaDTO{}, false
	}
	h.audit(c, auditAction, "media_asset", asset.ID.String(), reason)
	return h.contentMediaDTOs([]model.MediaAsset{asset})[0], true
}

// UploadContentMedia stores a validated image for content/editorial use.
func (h Handler) UploadContentMedia(c *gin.Context) {
	dto, ok := h.uploadAdminImage(c, "上传内容图片", "content_media.upload")
	if !ok {
		return
	}
	response.Created(c, dto)
}

func (h Handler) UpdateContentMedia(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42357, "error.media_asset_id_invalid")
		return
	}
	h.saveContentMedia(c, &id)
}

func (h Handler) DeleteContentMedia(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42357, "error.media_asset_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "删除外部媒体登记")
	if !ok {
		return
	}
	var item model.MediaAsset
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("disk = ?", "external").First(&item, "id = ?", id).Error; err != nil {
			return err
		}
		var posts, banners int64
		if err := tx.Model(&model.Post{}).Where("cover_url = ?", item.PublicURL).Count(&posts).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.Banner{}).Where("image_url = ?", item.PublicURL).Count(&banners).Error; err != nil {
			return err
		}
		if posts+banners > 0 {
			return errContentInUse
		}
		if err := tx.Model(&item).Update("object_key", item.ObjectKey+"/deleted/"+item.ID.String()).Error; err != nil {
			return err
		}
		return tx.Delete(&item).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40504, "error.external_media_not_found")
		return
	}
	if errors.Is(err, errContentInUse) {
		response.Error(c, 409, 409117, "error.media_referenced_cannot_delete")
		return
	}
	if err != nil {
		response.Error(c, 500, 50111, "error.media_asset_delete_failed")
		return
	}
	h.audit(c, "media.delete", "media_asset", item.ID.String(), reason)
	response.OK(c, gin.H{"deleted": true})
}
