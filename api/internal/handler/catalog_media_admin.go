package handler

import (
	"errors"
	"net/http"
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

type catalogMediaDTO struct {
	ID           uuid.UUID `json:"id"`
	OwnerType    string    `json:"owner_type"`
	OwnerID      uuid.UUID `json:"owner_id"`
	AssetID      uuid.UUID `json:"asset_id"`
	Role         string    `json:"role"`
	Sort         int       `json:"sort"`
	AltText      string    `json:"alt_text"`
	URL          string    `json:"url"`
	MIME         string    `json:"mime"`
	Width        int       `json:"width,omitempty"`
	Height       int       `json:"height,omitempty"`
	SourceType   string    `json:"source_type"`
	MirrorStatus string    `json:"mirror_status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type catalogMediaAssetDTO struct {
	ID        uuid.UUID `json:"id"`
	URL       string    `json:"url"`
	FileName  string    `json:"file_name"`
	MIME      string    `json:"mime"`
	Size      int64     `json:"size"`
	SHA256    string    `json:"sha256"`
	AltText   string    `json:"alt_text"`
	Disk      string    `json:"disk"`
	CreatedAt time.Time `json:"created_at"`
}

func toCatalogMediaDTO(item model.CatalogMedia) catalogMediaDTO {
	return catalogMediaDTO{
		ID: item.ID, OwnerType: item.OwnerType, OwnerID: item.OwnerID, AssetID: item.AssetID,
		Role: item.Role, Sort: item.Sort, AltText: defaultString(item.AltText, item.Asset.AltText),
		URL: item.Asset.PublicURL, MIME: item.Asset.MIME, SourceType: item.SourceType,
		MirrorStatus: item.MirrorStatus, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func catalogMediaForOwners(db *gorm.DB, ownerType string, ownerIDs []uuid.UUID) (map[uuid.UUID][]catalogMediaDTO, error) {
	result := make(map[uuid.UUID][]catalogMediaDTO, len(ownerIDs))
	if len(ownerIDs) == 0 {
		return result, nil
	}
	var items []model.CatalogMedia
	if err := db.Preload("Asset").Where("owner_type = ? AND owner_id IN ? AND mirror_status = ?", ownerType, ownerIDs, "ready").Order("owner_id, CASE role WHEN 'cover' THEN 0 WHEN 'gallery' THEN 1 ELSE 2 END, sort ASC, created_at ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.Asset.PublicURL == "" || !strings.HasPrefix(item.Asset.MIME, "image/") {
			continue
		}
		result[item.OwnerID] = append(result[item.OwnerID], toCatalogMediaDTO(item))
	}
	for _, ownerID := range ownerIDs {
		if result[ownerID] == nil {
			result[ownerID] = []catalogMediaDTO{}
		}
	}
	return result, nil
}

func (h Handler) PublicMedia(c *gin.Context) {
	path, mimeType, err := media.New(h.Cfg).ResolveObject(c.Param("prefix"), c.Param("file"))
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Content-Type", mimeType)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.File(path)
}

func catalogMediaAlt(value string) (string, error) {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > 300 {
		return "", errors.New("alt text too long")
	}
	return value, nil
}

func (h Handler) saveCatalogMediaAsset(c *gin.Context, mirrored bool) {
	reason, ok := requireAdminChangeReason(c, "保存商品媒体")
	if !ok {
		return
	}
	var altText string
	var err error
	store := media.New(h.Cfg)
	var stored media.Object
	var sourceURL string
	if mirrored {
		var req struct {
			URL     string `json:"url"`
			AltText string `json:"alt_text"`
		}
		if decodeStrictJSON(c, &req) != nil {
			response.Error(c, 422, 42360, "error.catalog_media_fields_invalid")
			return
		}
		altText, err = catalogMediaAlt(req.AltText)
		if err != nil {
			response.Error(c, 422, 42360, "error.catalog_media_fields_invalid")
			return
		}
		sourceURL = strings.TrimSpace(req.URL)
		stored, err = store.MirrorImage(c.Request.Context(), sourceURL)
	} else {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.Cfg.MediaMaxImageBytes+(1<<20))
		altText, err = catalogMediaAlt(c.PostForm("alt_text"))
		if err != nil {
			response.Error(c, 422, 42360, "error.catalog_media_fields_invalid")
			return
		}
		file, header, fileErr := c.Request.FormFile("file")
		if fileErr != nil {
			response.Error(c, 422, 42361, "error.catalog_media_file_required")
			return
		}
		defer file.Close()
		stored, err = store.PutImage(file, header.Filename)
	}
	if err != nil {
		if errors.Is(err, media.ErrStorageUnavailable) {
			response.Error(c, 507, 50701, "error.catalog_media_storage_unavailable")
		} else {
			response.Error(c, 422, 42362, "error.catalog_media_image_invalid")
		}
		return
	}
	uploaderID, _ := uuid.Parse(c.GetString("subject"))
	asset := model.MediaAsset{
		Disk: "local", ObjectKey: stored.ObjectKey, PublicURL: stored.PublicURL,
		AltText: altText, FileName: stored.FileName, MIME: stored.MIME, Size: stored.Size,
		SHA256: stored.SHA256, UploadedBy: &uploaderID, Visibility: "public",
	}
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var existing model.MediaAsset
		findErr := tx.Where("disk = ? AND sha256 = ? AND mime = ?", "local", stored.SHA256, stored.MIME).First(&existing).Error
		if findErr == nil {
			asset = existing
			return nil
		}
		if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		return tx.Create(&asset).Error
	})
	if err != nil {
		response.Error(c, 500, 50120, "error.catalog_media_save_failed")
		return
	}
	action := "catalog_media.upload"
	if mirrored {
		action = "catalog_media.mirror"
	}
	h.audit(c, action, "media_asset", asset.ID.String(), reason)
	_ = sourceURL // provenance is persisted when the asset is attached.
	response.Created(c, catalogMediaAssetDTO{ID: asset.ID, URL: asset.PublicURL, FileName: asset.FileName, MIME: asset.MIME, Size: asset.Size, SHA256: asset.SHA256, AltText: asset.AltText, Disk: asset.Disk, CreatedAt: asset.CreatedAt})
}

func (h Handler) UploadCatalogMedia(c *gin.Context) { h.saveCatalogMediaAsset(c, false) }
func (h Handler) MirrorCatalogMedia(c *gin.Context) { h.saveCatalogMediaAsset(c, true) }

type attachCatalogMediaRequest struct {
	OwnerType  string `json:"owner_type"`
	OwnerID    string `json:"owner_id"`
	AssetID    string `json:"asset_id"`
	Role       string `json:"role"`
	Sort       int    `json:"sort"`
	AltText    string `json:"alt_text"`
	SourceURL  string `json:"source_url"`
	SourceType string `json:"source_type"`
}

func (r *attachCatalogMediaRequest) normalizeAndValidate() (uuid.UUID, uuid.UUID, error) {
	r.OwnerType = strings.ToLower(strings.TrimSpace(r.OwnerType))
	r.Role = strings.ToLower(strings.TrimSpace(r.Role))
	r.SourceType = strings.ToLower(strings.TrimSpace(defaultString(r.SourceType, "manual")))
	r.SourceURL = strings.TrimSpace(r.SourceURL)
	ownerID, ownerErr := uuid.Parse(strings.TrimSpace(r.OwnerID))
	assetID, assetErr := uuid.Parse(strings.TrimSpace(r.AssetID))
	alt, altErr := catalogMediaAlt(r.AltText)
	r.AltText = alt
	if ownerErr != nil || assetErr != nil || altErr != nil || (r.OwnerType != "category" && r.OwnerType != "product") || (r.Role != "cover" && r.Role != "gallery" && r.Role != "detail") || (r.OwnerType == "category" && r.Role != "cover") || r.Sort < 0 || r.Sort > 1000 || (r.SourceType != "manual" && r.SourceType != "upload" && r.SourceType != "supplier") || utf8.RuneCountInString(r.SourceURL) > 1000 {
		return uuid.Nil, uuid.Nil, errors.New("invalid catalog media")
	}
	return ownerID, assetID, nil
}

func ensureCatalogMediaOwner(tx *gorm.DB, ownerType string, ownerID uuid.UUID) error {
	if ownerType == "category" {
		return tx.Select("id").First(&model.Category{}, "id = ?", ownerID).Error
	}
	return tx.Select("id").First(&model.Product{}, "id = ?", ownerID).Error
}

func syncCatalogCover(tx *gorm.DB, ownerType string, ownerID uuid.UUID, asset *model.MediaAsset) error {
	assetID := any(nil)
	publicURL := ""
	if asset != nil {
		assetID = asset.ID
		publicURL = asset.PublicURL
	}
	if ownerType == "category" {
		return tx.Model(&model.Category{}).Where("id = ?", ownerID).Updates(map[string]any{"image_asset_id": assetID, "image_url": publicURL}).Error
	}
	return tx.Model(&model.Product{}).Where("id = ?", ownerID).Updates(map[string]any{"cover_asset_id": assetID, "cover_url": publicURL}).Error
}

func (h Handler) AttachCatalogMedia(c *gin.Context) {
	reason, ok := requireAdminChangeReason(c, "关联商品媒体")
	if !ok {
		return
	}
	var req attachCatalogMediaRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42360, "error.catalog_media_fields_invalid")
		return
	}
	ownerID, assetID, err := req.normalizeAndValidate()
	if err != nil {
		response.Error(c, 422, 42360, "error.catalog_media_fields_invalid")
		return
	}
	var item model.CatalogMedia
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := ensureCatalogMediaOwner(tx, req.OwnerType, ownerID); err != nil {
			return err
		}
		var asset model.MediaAsset
		if err := tx.Where("visibility = ? AND mime IN ?", "public", []string{"image/jpeg", "image/png", "image/gif", "image/webp"}).First(&asset, "id = ?", assetID).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&model.CatalogMedia{}).Where("owner_type = ? AND owner_id = ?", req.OwnerType, ownerID).Count(&count).Error; err != nil {
			return err
		}
		if count >= 30 {
			return errCatalogInUse
		}
		if req.Role == "cover" {
			if err := tx.Where("owner_type = ? AND owner_id = ? AND role = ?", req.OwnerType, ownerID, "cover").Delete(&model.CatalogMedia{}).Error; err != nil {
				return err
			}
		}
		item = model.CatalogMedia{OwnerType: req.OwnerType, OwnerID: ownerID, AssetID: asset.ID, Asset: asset, Role: req.Role, Sort: req.Sort, AltText: req.AltText, SourceURL: req.SourceURL, SourceType: req.SourceType, SourceHash: asset.SHA256, MirrorStatus: "ready"}
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		if req.Role == "cover" {
			return syncCatalogCover(tx, req.OwnerType, ownerID, &asset)
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40510, "error.catalog_media_owner_or_asset_not_found")
		return
	}
	if errors.Is(err, errCatalogInUse) {
		response.Error(c, 409, 409120, "error.catalog_media_limit_reached")
		return
	}
	if err != nil {
		response.Error(c, 409, 409121, "error.catalog_media_attach_failed")
		return
	}
	h.audit(c, "catalog_media.attach", req.OwnerType, ownerID.String(), reason)
	response.Created(c, toCatalogMediaDTO(item))
}

func (h Handler) AdminCatalogMedia(c *gin.Context) {
	ownerType := strings.ToLower(strings.TrimSpace(c.Query("owner_type")))
	ownerID, err := uuid.Parse(strings.TrimSpace(c.Query("owner_id")))
	if err != nil || (ownerType != "category" && ownerType != "product") {
		response.Error(c, 422, 42360, "error.catalog_media_fields_invalid")
		return
	}
	itemsByOwner, err := catalogMediaForOwners(h.DB, ownerType, []uuid.UUID{ownerID})
	if err != nil {
		response.Error(c, 500, 50121, "error.catalog_media_fetch_failed")
		return
	}
	response.OK(c, itemsByOwner[ownerID])
}

func (h Handler) DeleteCatalogMedia(c *gin.Context) {
	mediaID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42363, "error.catalog_media_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "移除商品媒体")
	if !ok {
		return
	}
	var item model.CatalogMedia
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", mediaID).Error; err != nil {
			return err
		}
		if err := tx.Delete(&item).Error; err != nil {
			return err
		}
		if item.Role == "cover" {
			return syncCatalogCover(tx, item.OwnerType, item.OwnerID, nil)
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40511, "error.catalog_media_not_found")
		return
	}
	if err != nil {
		response.Error(c, 500, 50122, "error.catalog_media_delete_failed")
		return
	}
	h.audit(c, "catalog_media.detach", item.OwnerType, item.OwnerID.String(), reason)
	response.OK(c, gin.H{"deleted": true})
}

func (h Handler) StorageReady() error {
	return media.New(h.Cfg).Ready()
}
