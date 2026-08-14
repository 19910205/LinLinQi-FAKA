package queue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/net/html"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/currency"
	"linlinqi/api/internal/media"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/service"
	"linlinqi/api/internal/supply"
)

var errSupplierSyncInProgress = errors.New("supplier sync is already in progress")

const maxSupplierCatalogMedia = 30

type supplierCategoryMediaWork struct {
	MappingID          uuid.UUID
	CategoryID         uuid.UUID
	ExternalCategoryID string
	ImageURL           string
	AltText            string
}

type supplierProductMediaWork struct {
	Mapping model.ProductMapping
	Product supply.Product
}

type supplierMediaSpec struct {
	SourceURL string
	Role      string
	AltText   string
}

type supplierDownloadedMedia struct {
	Spec   supplierMediaSpec
	Object media.Object
}

type supplierMediaDownload struct {
	Items        []supplierDownloadedMedia
	SourceHashes map[string]string
}

type supplierPreparedFX struct {
	Source   model.CurrencyDefinition
	Target   model.CurrencyDefinition
	Snapshot model.FXRateSnapshot
}

func supplierProductCurrency(supplierModel model.Supplier, upstream supply.Product) string {
	configured := strings.ToUpper(strings.TrimSpace(supplierModel.PriceCurrency))
	detected := strings.ToUpper(strings.TrimSpace(upstream.Currency))
	if supplierModel.CurrencyMode == "auto" && len(detected) == 3 {
		return detected
	}
	return configured
}

func (w *Worker) prepareSupplierFX(ctx context.Context, supplierModel model.Supplier, products []supply.Product) (model.CurrencyDefinition, map[string]supplierPreparedFX, error) {
	storeCurrency := "CNY"
	var setting model.Setting
	if err := w.db.Where("key = ?", "store_currency").First(&setting).Error; err == nil && strings.TrimSpace(setting.Value) != "" {
		storeCurrency = strings.ToUpper(strings.TrimSpace(setting.Value))
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.CurrencyDefinition{}, nil, err
	}
	var target model.CurrencyDefinition
	if err := w.db.Where("code = ? AND enabled = ?", storeCurrency, true).First(&target).Error; err != nil {
		return model.CurrencyDefinition{}, nil, fmt.Errorf("store currency %s is unavailable: %w", storeCurrency, err)
	}
	manager := currency.Manager{DB: w.db, AllowPrivate: w.cfg.Env != "production"}
	prepared := make(map[string]supplierPreparedFX)
	sourceCodes := make(map[string]struct{})
	for _, upstream := range products {
		sourceCode := supplierProductCurrency(supplierModel, upstream)
		if len(sourceCode) != 3 {
			return model.CurrencyDefinition{}, nil, fmt.Errorf("supplier product %s has no valid source currency", upstream.ExternalID)
		}
		sourceCodes[sourceCode] = struct{}{}
	}
	var fixedPriceCurrencies []string
	if err := w.db.Model(&model.ProductMapping{}).Where("supplier_id = ? AND price_mode = ?", supplierModel.ID, "fixed_price").Distinct("fixed_price_currency").Pluck("fixed_price_currency", &fixedPriceCurrencies).Error; err != nil {
		return model.CurrencyDefinition{}, nil, err
	}
	for _, code := range fixedPriceCurrencies {
		code = strings.ToUpper(strings.TrimSpace(code))
		if len(code) != 3 {
			return model.CurrencyDefinition{}, nil, fmt.Errorf("supplier fixed price has no valid currency")
		}
		sourceCodes[code] = struct{}{}
	}
	for sourceCode := range sourceCodes {
		var source model.CurrencyDefinition
		if err := w.db.Where("code = ? AND enabled = ?", sourceCode, true).First(&source).Error; err != nil {
			return model.CurrencyDefinition{}, nil, fmt.Errorf("supplier currency %s is unavailable: %w", sourceCode, err)
		}
		snapshot, err := manager.Resolve(ctx, source.Code, target.Code)
		if err != nil {
			return model.CurrencyDefinition{}, nil, fmt.Errorf("resolve %s/%s exchange rate: %w", source.Code, target.Code, err)
		}
		prepared[sourceCode] = supplierPreparedFX{Source: source, Target: target, Snapshot: snapshot}
	}
	return target, prepared, nil
}

func (w *Worker) beginSupplierSync(supplierModel model.Supplier, trigger string) (model.SupplierSyncRun, error) {
	run := model.SupplierSyncRun{SupplierID: supplierModel.ID, Trigger: trigger, Status: "running", Protocol: normalizedSupplierProtocol(supplierModel.Protocol), StartedAt: time.Now().UTC()}
	err := w.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "supplier-sync:"+supplierModel.ID.String()).Error; err != nil {
			return err
		}
		staleBefore := time.Now().UTC().Add(-30 * time.Minute)
		_ = tx.Model(&model.SupplierSyncRun{}).Where("supplier_id = ? AND status = ? AND started_at < ?", supplierModel.ID, "running", staleBefore).Updates(map[string]any{"status": "failed", "error_summary": "stale sync run recovered", "completed_at": time.Now().UTC()}).Error
		var active int64
		if err := tx.Model(&model.SupplierSyncRun{}).Where("supplier_id = ? AND status = ?", supplierModel.ID, "running").Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return errSupplierSyncInProgress
		}
		return tx.Create(&run).Error
	})
	return run, err
}

func (w *Worker) finishSupplierSync(run *model.SupplierSyncRun, status string, cause error) {
	now := time.Now().UTC()
	updates := map[string]any{
		"status": status, "categories_seen": run.CategoriesSeen,
		"categories_created": run.CategoriesMade, "products_seen": run.ProductsSeen,
		"products_created": run.ProductsMade, "products_updated": run.ProductsUpdated,
		"media_mirrored": run.MediaMirrored, "fx_snapshot_id": run.FXSnapshotID,
		"warnings": run.Warnings, "completed_at": &now,
	}
	if cause != nil {
		updates["error_summary"] = truncate(cause.Error(), 1000)
	}
	if err := w.db.Model(&model.SupplierSyncRun{}).Where("id = ?", run.ID).Updates(updates).Error; err != nil {
		return
	}
	eventCode := "supplier.sync.succeeded"
	if status != "succeeded" {
		eventCode = "supplier.sync.failed"
	}
	w.createOperationalNotifications(w.db, eventCode, run.ID.String(), map[string]string{
		"occurred_at": now.Format(time.RFC3339),
		"status":      status,
		"channel":     "supplier",
		"summary":     supplierSyncSummary(*run),
	})
}

func snapshot(value any) (json.RawMessage, string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(payload)
	return payload, hex.EncodeToString(digest[:]), nil
}

func supplierText(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) <= maximum {
		return value
	}
	runes := []rune(value)
	return string(runes[:maximum])
}

func resolveSupplierMediaURL(baseURL, rawURL string) (string, bool) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" || utf8.RuneCountInString(rawURL) > 1000 {
		return "", false
	}
	reference, err := url.Parse(rawURL)
	if err != nil {
		return "", false
	}
	if !reference.IsAbs() {
		base, baseErr := url.Parse(strings.TrimSpace(baseURL))
		if baseErr != nil || base.Scheme == "" || base.Hostname() == "" {
			return "", false
		}
		reference = base.ResolveReference(reference)
	}
	reference.Scheme = strings.ToLower(reference.Scheme)
	reference.Fragment = ""
	if (reference.Scheme != "https" && reference.Scheme != "http") || reference.Hostname() == "" || reference.User != nil {
		return "", false
	}
	resolved := reference.String()
	if utf8.RuneCountInString(resolved) > 1000 {
		return "", false
	}
	return resolved, true
}

type supplierDescriptionImage struct {
	SourceURL string
	AltText   string
}

func supplierDescriptionImages(description, baseURL string) []supplierDescriptionImage {
	result := make([]supplierDescriptionImage, 0)
	seen := make(map[string]struct{})
	tokenizer := html.NewTokenizer(strings.NewReader(description))
	for len(result) < maxSupplierCatalogMedia {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			if tokenizer.Err() == io.EOF {
				break
			}
			return result
		}
		if tokenType != html.StartTagToken && tokenType != html.SelfClosingTagToken {
			continue
		}
		token := tokenizer.Token()
		if !strings.EqualFold(token.Data, "img") {
			continue
		}
		altText := ""
		candidates := make(map[string]string, 4)
		for _, attribute := range token.Attr {
			key := strings.ToLower(strings.TrimSpace(attribute.Key))
			switch key {
			case "alt":
				altText = supplierText(attribute.Val, 300)
			case "src", "data-src", "data-original", "data-lazy-src":
				candidates[key] = attribute.Val
			}
		}
		for _, key := range []string{"src", "data-src", "data-original", "data-lazy-src"} {
			resolved, ok := resolveSupplierMediaURL(baseURL, candidates[key])
			if !ok {
				continue
			}
			if _, exists := seen[resolved]; exists {
				break
			}
			seen[resolved] = struct{}{}
			result = append(result, supplierDescriptionImage{SourceURL: resolved, AltText: altText})
			break
		}
	}
	return result
}

func rewriteSupplierDescriptionImages(description, baseURL string, replacements map[string]string) string {
	if description == "" || len(replacements) == 0 {
		return description
	}
	var output strings.Builder
	output.Grow(len(description))
	tokenizer := html.NewTokenizer(strings.NewReader(description))
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			if tokenizer.Err() != io.EOF {
				return description
			}
			break
		}
		raw := tokenizer.Raw()
		if tokenType != html.StartTagToken && tokenType != html.SelfClosingTagToken {
			_, _ = output.Write(raw)
			continue
		}
		token := tokenizer.Token()
		if !strings.EqualFold(token.Data, "img") {
			_, _ = output.Write(raw)
			continue
		}
		replaced := ""
		hasSource := false
		attributes := make([]html.Attribute, 0, len(token.Attr)+1)
		for _, attribute := range token.Attr {
			key := strings.ToLower(strings.TrimSpace(attribute.Key))
			if key == "srcset" || key == "data-srcset" {
				continue
			}
			if key == "src" {
				hasSource = true
			}
			if key == "src" || key == "data-src" || key == "data-original" || key == "data-lazy-src" {
				if resolved, ok := resolveSupplierMediaURL(baseURL, attribute.Val); ok {
					if publicURL := replacements[resolved]; publicURL != "" {
						attribute.Val = publicURL
						replaced = publicURL
					}
				}
			}
			attributes = append(attributes, attribute)
		}
		if replaced == "" {
			_, _ = output.Write(raw)
			continue
		}
		if !hasSource {
			attributes = append(attributes, html.Attribute{Key: "src", Val: replaced})
		}
		token.Attr = attributes
		output.WriteString(token.String())
	}
	return output.String()
}

func supplierProductMediaSpecs(supplierModel model.Supplier, upstream supply.Product) ([]supplierMediaSpec, error) {
	specs := make([]supplierMediaSpec, 0, len(upstream.ImageURLs)+1)
	seen := make(map[string]struct{})
	add := func(rawURL, role, altText string, strict bool) error {
		if strings.TrimSpace(rawURL) == "" {
			return nil
		}
		resolved, ok := resolveSupplierMediaURL(supplierModel.BaseURL, rawURL)
		if !ok {
			if strict {
				return errors.New("supplier media URL is invalid")
			}
			return nil
		}
		if _, exists := seen[resolved]; exists {
			return nil
		}
		if len(specs) >= maxSupplierCatalogMedia {
			return errors.New("supplier media exceeds catalog limit")
		}
		seen[resolved] = struct{}{}
		specs = append(specs, supplierMediaSpec{SourceURL: resolved, Role: role, AltText: supplierText(altText, 300)})
		return nil
	}
	coverURL := strings.TrimSpace(upstream.CoverURL)
	if coverURL == "" {
		for _, imageURL := range upstream.ImageURLs {
			if strings.TrimSpace(imageURL) != "" {
				coverURL = imageURL
				break
			}
		}
	}
	if err := add(coverURL, "cover", upstream.Name, true); err != nil {
		return nil, err
	}
	for _, imageURL := range upstream.ImageURLs {
		if err := add(imageURL, "gallery", upstream.Name, true); err != nil {
			return nil, err
		}
	}
	for _, image := range supplierDescriptionImages(upstream.Description, supplierModel.BaseURL) {
		altText := image.AltText
		if altText == "" {
			altText = upstream.Name
		}
		if err := add(image.SourceURL, "detail", altText, false); err != nil {
			return nil, err
		}
	}
	return specs, nil
}

// supplierVariantProduct normalizes an upstream variant into the same
// catalog shape used by the mapping loop. The parent's presentation fields
// and currency are inherited while the variant's price/stock replace them.
func supplierVariantProduct(parent supply.Product, variant supply.ProductVariant) (supply.Product, error) {
	externalID := strings.TrimSpace(variant.ExternalID)
	if externalID == "" {
		externalID = strings.TrimSpace(variant.ID)
	}
	var err error
	externalID, err = supply.NormalizeExternalID(externalID)
	if err != nil {
		return supply.Product{}, errors.New("supplier variant identifier is invalid")
	}
	parentExternalID, err := supply.NormalizeExternalID(parent.ExternalID)
	if err != nil {
		return supply.Product{}, errors.New("supplier variant parent identifier is invalid")
	}
	item := parent
	item.ID = variant.ID
	item.ExternalID = externalID
	item.ParentExternalID = parentExternalID
	item.ExternalSKU = variant.ExternalSKU
	item.Name = supplierText(strings.TrimSpace(parent.Name+" / "+variant.Name), 240)
	item.Price = variant.Price
	item.Stock = variant.Stock
	item.Minimum = variant.Minimum
	if item.Minimum == 0 {
		item.Minimum = parent.Minimum
	}
	item.Maximum = variant.Maximum
	if item.Maximum == 0 {
		item.Maximum = parent.Maximum
	}
	if strings.TrimSpace(variant.Status) != "" {
		item.Status = variant.Status
	}
	item.Variants = nil
	return item, nil
}

// ensureSupplierVariantMappings reconciles upstream variants against local
// SKUs for an imported or automatically created parent mapping. Existing
// variant mappings receive an injected remote entry so the regular mapping
// pass can update price/stock/status/limits; missing local variants are
// created with the same pricing rule as the parent. Variants that vanished
// upstream are disabled here instead of being deleted.
func (w *Worker) ensureSupplierVariantMappings(tx *gorm.DB, supplierModel model.Supplier, parentMapping model.ProductMapping, upstream supply.Product, target model.CurrencyDefinition, rates map[string]supplierPreparedFX, remote map[string]supply.Product, now time.Time, run *model.SupplierSyncRun) error {
	if !parentMapping.AutoSyncVariants || len(upstream.Variants) == 0 {
		return nil
	}
	var existing []model.ProductMapping
	if err := tx.Where("supplier_id = ? AND product_id = ? AND variant_id IS NOT NULL", supplierModel.ID, parentMapping.ProductID).Find(&existing).Error; err != nil {
		return err
	}
	byExternal := make(map[string]model.ProductMapping, len(existing))
	for _, item := range existing {
		byExternal[item.ExternalProductID] = item
	}
	sourceCode := supplierProductCurrency(supplierModel, upstream)
	prepared, exists := rates[sourceCode]
	if !exists {
		return fmt.Errorf("exchange rate for %s is unavailable", sourceCode)
	}
	seenVariants := make(map[string]struct{}, len(upstream.Variants))
	for _, variant := range upstream.Variants {
		variantProduct, err := supplierVariantProduct(upstream, variant)
		if err != nil {
			return err
		}
		seenVariants[variantProduct.ExternalID] = struct{}{}
		remote[variantProduct.ExternalID] = variantProduct
		if _, already := byExternal[variantProduct.ExternalID]; already {
			continue
		}
		attributes, _ := json.Marshal(map[string]any{"external_id": variant.ExternalID, "external_sku": variant.ExternalSKU})
		variantStatus := "active"
		if strings.EqualFold(strings.TrimSpace(variant.Status), "inactive") {
			variantStatus = "inactive"
		}
		salePrice, convertedCost, err := supplierMappedConvertedPrice(parentMapping, variant.Price, prepared.Source.MinorUnit, target.MinorUnit, prepared.Snapshot.Rate)
		if err != nil {
			return err
		}
		localVariant := model.ProductVariant{
			ProductID:     parentMapping.ProductID,
			SKU:           supplierCatalogSlug(supplierModel.Code+"-variant", variantProduct.ExternalID),
			Name:          supplierText(strings.TrimSpace(upstream.Name+" / "+variant.Name), 160),
			Attributes:    string(attributes),
			Price:         salePrice,
			CostPrice:     convertedCost,
			Status:        variantStatus,
			PurchaseLimit: max(variant.Maximum, 0),
		}
		if err := tx.Create(&localVariant).Error; err != nil {
			return err
		}
		mapping := model.ProductMapping{
			SupplierID: supplierModel.ID, ProductID: parentMapping.ProductID, VariantID: &localVariant.ID,
			SupplierCategoryMappingID: parentMapping.SupplierCategoryMappingID,
			InheritCategoryPolicy:     parentMapping.InheritCategoryPolicy,
			ExternalProductID:         variantProduct.ExternalID, ParameterMapping: parentMapping.ParameterMapping,
			PriceMode: parentMapping.PriceMode, MarkupBasisPoint: parentMapping.MarkupBasisPoint,
			MarkupAmount: parentMapping.MarkupAmount, MarkupCurrency: parentMapping.MarkupCurrency,
			FixedPrice: parentMapping.FixedPrice, FixedPriceCurrency: parentMapping.FixedPriceCurrency,
			LastUpstreamPrice: variant.Price, LastUpstreamCurrency: sourceCode, LastConvertedCost: convertedCost,
			LastFXSnapshotID: &prepared.Snapshot.ID,
			AutoSyncPrice:    parentMapping.AutoSyncPrice, AutoSyncStock: parentMapping.AutoSyncStock,
			AutoSyncTitle: parentMapping.AutoSyncTitle, AutoSyncSummary: parentMapping.AutoSyncSummary,
			AutoSyncDescription: parentMapping.AutoSyncDescription, AutoSyncMedia: parentMapping.AutoSyncMedia,
			MirrorRemoteMedia: parentMapping.MirrorRemoteMedia, AutoSyncCategory: parentMapping.AutoSyncCategory,
			AutoSyncVariants: parentMapping.AutoSyncVariants, AutoSyncStatus: parentMapping.AutoSyncStatus,
			AutoSyncLimits: parentMapping.AutoSyncLimits,
		}
		if err := tx.Create(&mapping).Error; err != nil {
			return err
		}
		if err := tx.Model(&mapping).UpdateColumns(map[string]any{
			"supplier_category_mapping_id": mapping.SupplierCategoryMappingID,
			"inherit_category_policy":      mapping.InheritCategoryPolicy,
			"auto_sync_price":              mapping.AutoSyncPrice, "auto_sync_stock": mapping.AutoSyncStock,
			"auto_sync_title": mapping.AutoSyncTitle, "auto_sync_summary": mapping.AutoSyncSummary,
			"auto_sync_description": mapping.AutoSyncDescription, "auto_sync_media": mapping.AutoSyncMedia,
			"mirror_remote_media": mapping.MirrorRemoteMedia, "auto_sync_category": mapping.AutoSyncCategory,
			"auto_sync_variants": mapping.AutoSyncVariants, "auto_sync_status": mapping.AutoSyncStatus,
			"auto_sync_limits": mapping.AutoSyncLimits,
		}).Error; err != nil {
			return err
		}
		legacy := model.SupplierProduct{
			SupplierID: supplierModel.ID, ProductID: parentMapping.ProductID, VariantID: &localVariant.ID,
			ExternalID: variantProduct.ExternalID, ExternalPrice: variant.Price, ExternalStock: variant.Stock,
			PriceMarkupRate: parentMapping.MarkupBasisPoint, AutoSync: parentMapping.AutoSyncPrice || parentMapping.AutoSyncStock,
		}
		if err := tx.Create(&legacy).Error; err != nil {
			return err
		}
		run.ProductsUpdated++
		_ = tx.Create(&model.SupplierSyncChange{RunID: run.ID, EntityType: "variant", ExternalID: variantProduct.ExternalID, LocalID: &localVariant.ID, Action: "create", ChangedFields: json.RawMessage(`["name","price","stock"]`), Applied: true, Message: "created supplier variant mapping"}).Error
	}
	for externalID, mapping := range byExternal {
		if _, stillSeen := seenVariants[externalID]; stillSeen {
			continue
		}
		if mapping.VariantID == nil {
			continue
		}
		if err := tx.Model(&model.ProductVariant{}).Where("id = ? AND product_id = ?", *mapping.VariantID, mapping.ProductID).Update("status", "inactive").Error; err != nil {
			return err
		}
		if err := tx.Model(&mapping).Updates(map[string]any{"last_error": "", "last_synced_at": &now}).Error; err != nil {
			return err
		}
	}
	return nil
}

func supplierMediaHash(object media.Object) string {
	return object.SHA256 + "\x00" + object.MIME
}

func (w *Worker) downloadSupplierMedia(ctx context.Context, specs []supplierMediaSpec) (supplierMediaDownload, error) {
	result := supplierMediaDownload{Items: make([]supplierDownloadedMedia, 0, len(specs)), SourceHashes: make(map[string]string, len(specs))}
	store := media.New(w.cfg)
	seenHashes := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		stored, err := store.MirrorImage(ctx, spec.SourceURL)
		if err != nil {
			return supplierMediaDownload{}, fmt.Errorf("mirror supplier %s image: %w", spec.Role, err)
		}
		hash := supplierMediaHash(stored)
		result.SourceHashes[spec.SourceURL] = hash
		if _, exists := seenHashes[hash]; exists {
			continue
		}
		seenHashes[hash] = struct{}{}
		result.Items = append(result.Items, supplierDownloadedMedia{Spec: spec, Object: stored})
	}
	return result, nil
}

func supplierMediaAsset(tx *gorm.DB, stored media.Object) (model.MediaAsset, error) {
	var asset model.MediaAsset
	err := tx.Where("disk = ? AND sha256 = ? AND mime = ?", "local", stored.SHA256, stored.MIME).First(&asset).Error
	if err == nil {
		return asset, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.MediaAsset{}, err
	}
	candidate := model.MediaAsset{Disk: "local", ObjectKey: stored.ObjectKey, PublicURL: stored.PublicURL, FileName: stored.FileName, MIME: stored.MIME, Size: stored.Size, SHA256: stored.SHA256, Visibility: "public"}
	result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "object_key"}}, DoNothing: true}).Create(&candidate)
	if result.Error != nil {
		return model.MediaAsset{}, result.Error
	}
	if result.RowsAffected == 1 {
		return candidate, nil
	}
	if err := tx.Where("object_key = ?", stored.ObjectKey).First(&asset).Error; err != nil {
		return model.MediaAsset{}, err
	}
	return asset, nil
}

func nextSupplierMediaSort(used map[int]struct{}) int {
	for sort := 0; ; sort++ {
		if _, exists := used[sort]; !exists {
			used[sort] = struct{}{}
			return sort
		}
	}
}

func (w *Worker) persistSupplierCatalogMedia(ownerType string, ownerID uuid.UUID, download supplierMediaDownload, description, descriptionBaseURL string, syncDescription bool) (int, error) {
	created := 0
	err := w.db.Transaction(func(tx *gorm.DB) error {
		if ownerType == "category" {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&model.Category{}, "id = ?", ownerID).Error; err != nil {
				return err
			}
		} else if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&model.Product{}, "id = ?", ownerID).Error; err != nil {
			return err
		}

		assets := make(map[string]model.MediaAsset, len(download.Items))
		for _, item := range download.Items {
			asset, err := supplierMediaAsset(tx, item.Object)
			if err != nil {
				return err
			}
			assets[supplierMediaHash(item.Object)] = asset
		}
		replacements := make(map[string]string, len(download.SourceHashes))
		for sourceURL, hash := range download.SourceHashes {
			if asset, exists := assets[hash]; exists {
				replacements[sourceURL] = asset.PublicURL
			}
		}

		var current []model.CatalogMedia
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Preload("Asset").Where("owner_type = ? AND owner_id = ?", ownerType, ownerID).Find(&current).Error; err != nil {
			return err
		}
		hasCover := false
		for _, item := range download.Items {
			if item.Spec.Role == "cover" {
				hasCover = true
				break
			}
		}
		preservedHashes := make(map[string]struct{})
		usedSorts := map[string]map[int]struct{}{"gallery": {}, "detail": {}}
		for _, item := range current {
			remove := item.SourceType == "supplier" && (item.Role == "gallery" || item.Role == "detail")
			if hasCover && item.Role == "cover" {
				remove = true
			}
			if remove {
				if err := tx.Delete(&item).Error; err != nil {
					return err
				}
				continue
			}
			if item.Asset.SHA256 != "" && item.Asset.MIME != "" {
				preservedHashes[item.Asset.SHA256+"\x00"+item.Asset.MIME] = struct{}{}
			}
			if used, exists := usedSorts[item.Role]; exists {
				used[item.Sort] = struct{}{}
			}
		}

		now := time.Now().UTC()
		var coverAsset *model.MediaAsset
		for _, item := range download.Items {
			hash := supplierMediaHash(item.Object)
			asset := assets[hash]
			if item.Spec.Role != "cover" {
				if _, duplicate := preservedHashes[hash]; duplicate {
					continue
				}
			}
			sort := 0
			if used, exists := usedSorts[item.Spec.Role]; exists {
				sort = nextSupplierMediaSort(used)
			}
			binding := model.CatalogMedia{OwnerType: ownerType, OwnerID: ownerID, AssetID: asset.ID, Role: item.Spec.Role, Sort: sort, AltText: item.Spec.AltText, SourceURL: supplierText(item.Spec.SourceURL, 1000), SourceHash: asset.SHA256, SourceType: "supplier", MirrorStatus: "ready", MirroredAt: &now}
			if err := tx.Create(&binding).Error; err != nil {
				return err
			}
			created++
			preservedHashes[hash] = struct{}{}
			if item.Spec.Role == "cover" {
				copy := asset
				coverAsset = &copy
			}
		}
		updates := map[string]any{}
		if coverAsset != nil {
			if ownerType == "category" {
				updates["image_asset_id"], updates["image_url"] = coverAsset.ID, coverAsset.PublicURL
			} else {
				updates["cover_asset_id"], updates["cover_url"] = coverAsset.ID, coverAsset.PublicURL
			}
		}
		if ownerType == "product" && syncDescription {
			updates["description"] = supplierText(rewriteSupplierDescriptionImages(description, descriptionBaseURL, replacements), 100_000)
		}
		if len(updates) == 0 {
			return nil
		}
		if ownerType == "category" {
			return tx.Model(&model.Category{}).Where("id = ?", ownerID).Updates(updates).Error
		}
		return tx.Model(&model.Product{}).Where("id = ?", ownerID).Updates(updates).Error
	})
	return created, err
}

func normalizeSupplierSnapshotIdentities(categories []supply.Category, products []supply.Product) error {
	seenCategories := make(map[string]struct{}, len(categories))
	for index := range categories {
		externalID, err := supply.NormalizeExternalID(categories[index].ExternalID)
		if err != nil {
			return errors.New("supplier category identifier is invalid")
		}
		externalParentID, err := supply.NormalizeOptionalExternalID(categories[index].ExternalParentID)
		if err != nil || externalParentID == externalID {
			return errors.New("supplier category parent identifier is invalid")
		}
		if _, duplicate := seenCategories[externalID]; duplicate {
			return errors.New("supplier category identifiers are not unique")
		}
		seenCategories[externalID] = struct{}{}
		categories[index].ExternalID = externalID
		categories[index].ExternalParentID = externalParentID
	}

	seenProducts := make(map[string]struct{}, len(products))
	for index := range products {
		externalID, err := supply.NormalizeExternalID(products[index].ExternalID)
		if err != nil {
			return errors.New("supplier product identifier is invalid")
		}
		parentExternalID, err := supply.NormalizeOptionalExternalID(products[index].ParentExternalID)
		if err != nil || parentExternalID == externalID {
			return errors.New("supplier product parent identifier is invalid")
		}
		externalCategoryID, err := supply.NormalizeOptionalExternalID(products[index].ExternalCategoryID)
		if err != nil {
			return errors.New("supplier product category identifier is invalid")
		}
		if _, duplicate := seenProducts[externalID]; duplicate {
			return errors.New("supplier product identifiers are not unique")
		}
		seenProducts[externalID] = struct{}{}
		products[index].ExternalID = externalID
		products[index].ParentExternalID = parentExternalID
		products[index].ExternalCategoryID = externalCategoryID

		for variantIndex := range products[index].Variants {
			variant := &products[index].Variants[variantIndex]
			variantExternalID := variant.ExternalID
			if strings.TrimSpace(variantExternalID) == "" {
				variantExternalID = variant.ID
			}
			variantExternalID, err = supply.NormalizeExternalID(variantExternalID)
			if err != nil || variantExternalID == externalID {
				return errors.New("supplier variant identifier is invalid")
			}
			if _, duplicate := seenProducts[variantExternalID]; duplicate {
				return errors.New("supplier product and variant identifiers are not unique")
			}
			seenProducts[variantExternalID] = struct{}{}
			variant.ExternalID = variantExternalID
		}
	}
	return nil
}

func (w *Worker) upsertSupplierSnapshots(tx *gorm.DB, supplierModel model.Supplier, categories []supply.Category, products []supply.Product, seenAt time.Time) error {
	if err := normalizeSupplierSnapshotIdentities(categories, products); err != nil {
		return err
	}
	seenCategories := make([]string, 0, len(categories))
	for _, category := range categories {
		if category.ExternalID == "" || supplierText(category.Name, 200) == "" {
			return errors.New("supplier category identifier or name is invalid")
		}
		raw, hash, err := snapshot(category)
		if err != nil {
			return err
		}
		seenCategories = append(seenCategories, category.ExternalID)
		var stored model.SupplierCategory
		err = tx.Where("supplier_id = ? AND external_id = ?", supplierModel.ID, category.ExternalID).First(&stored).Error
		updates := map[string]any{"external_parent_id": category.ExternalParentID, "name": supplierText(category.Name, 200), "description": supplierText(category.Description, 20_000), "image_url": supplierText(category.ImageURL, 1000), "sort": max(category.Sort, 0), "status": defaultSupplierStatus(category.Status), "raw_snapshot": raw, "snapshot_hash": hash, "last_seen_at": seenAt}
		if err == nil {
			if err := tx.Model(&stored).Updates(updates).Error; err != nil {
				return err
			}
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			stored = model.SupplierCategory{SupplierID: supplierModel.ID, ExternalID: category.ExternalID, ExternalParentID: category.ExternalParentID, Name: supplierText(category.Name, 200), Description: supplierText(category.Description, 20_000), ImageURL: supplierText(category.ImageURL, 1000), Sort: max(category.Sort, 0), Status: defaultSupplierStatus(category.Status), RawSnapshot: raw, SnapshotHash: hash, LastSeenAt: seenAt}
			if err := tx.Create(&stored).Error; err != nil {
				return err
			}
		} else {
			return err
		}
	}
	if len(seenCategories) > 0 {
		if err := tx.Model(&model.SupplierCategory{}).Where("supplier_id = ? AND external_id NOT IN ? AND status <> ?", supplierModel.ID, seenCategories, "missing").Update("status", "missing").Error; err != nil {
			return err
		}
	}
	seenProducts := make([]string, 0, len(products))
	for _, product := range products {
		raw, hash, err := snapshot(product)
		if err != nil {
			return err
		}
		variants, _ := json.Marshal(product.Variants)
		inputFields, _ := json.Marshal(product.InputFields)
		images, _ := json.Marshal(product.ImageURLs)
		tags, _ := json.Marshal(product.Tags)
		wholesale := product.WholesalePrices
		if len(wholesale) == 0 || !json.Valid(wholesale) {
			wholesale = json.RawMessage(`{}`)
		}
		seenProducts = append(seenProducts, product.ExternalID)
		updates := map[string]any{"parent_external_id": product.ParentExternalID, "external_category_id": product.ExternalCategoryID, "external_sku": supplierText(product.ExternalSKU, 180), "name": supplierText(product.Name, 240), "summary": supplierText(product.Summary, 1000), "description": supplierText(product.Description, 100_000), "cover_url": supplierText(product.CoverURL, 1000), "image_urls": images, "country": supplierText(product.Country, 8), "tags": tags, "currency": supplierProductCurrency(supplierModel, product), "price": product.Price, "original_price": product.OriginalPrice, "member_price": product.MemberPrice, "wholesale_prices": wholesale, "stock": product.Stock, "stock_status": supplierText(product.StockStatus, 24), "minimum": max(product.Minimum, 1), "maximum": max(product.Maximum, 0), "fulfillment_type": supplierText(product.FulfillmentType, 24), "status": defaultSupplierStatus(product.Status), "upstream_created_at": product.UpstreamCreatedAt, "upstream_updated_at": product.UpstreamUpdatedAt, "variants": variants, "input_fields": inputFields, "raw_snapshot": raw, "snapshot_hash": hash, "last_seen_at": seenAt}
		var stored model.SupplierCatalogProduct
		err = tx.Where("supplier_id = ? AND external_id = ?", supplierModel.ID, product.ExternalID).First(&stored).Error
		if err == nil {
			if err := tx.Model(&stored).Updates(updates).Error; err != nil {
				return err
			}
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			stored = model.SupplierCatalogProduct{SupplierID: supplierModel.ID, ExternalID: product.ExternalID}
			if err := tx.Create(&stored).Error; err != nil {
				return err
			}
			if err := tx.Model(&stored).Updates(updates).Error; err != nil {
				return err
			}
		} else {
			return err
		}
	}
	if len(seenProducts) > 0 {
		if err := tx.Model(&model.SupplierCatalogProduct{}).Where("supplier_id = ? AND external_id NOT IN ? AND status <> ?", supplierModel.ID, seenProducts, "missing").Update("status", "missing").Error; err != nil {
			return err
		}
	}
	return nil
}

func defaultSupplierStatus(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "inactive") {
		return "inactive"
	}
	return "active"
}

var supplierSlugInvalid = regexp.MustCompile(`[^a-z0-9]+`)

func supplierCatalogSlug(prefix, externalID string) string {
	base := strings.ToLower(strings.TrimSpace(prefix + "-" + externalID))
	base = strings.Trim(supplierSlugInvalid.ReplaceAllString(base, "-"), "-")
	if base == "" {
		base = "supplier-item"
	}
	digest := sha256.Sum256([]byte(prefix + ":" + externalID))
	suffix := hex.EncodeToString(digest[:4])
	if len(base) > 168 {
		base = strings.Trim(base[:168], "-")
	}
	return base + "-" + suffix
}

func supplierCategoryProductPricing(mapping model.SupplierCategoryMapping, targetCode string) (model.ProductMapping, error) {
	pricing := model.ProductMapping{
		PriceMode: mapping.PriceMode, MarkupBasisPoint: mapping.MarkupBasisPoint,
		MarkupAmount: mapping.MarkupAmount, MarkupCurrency: mapping.MarkupCurrency,
	}
	if pricing.PriceMode == "fixed_amount" && strings.ToUpper(strings.TrimSpace(pricing.MarkupCurrency)) != targetCode {
		return model.ProductMapping{}, fmt.Errorf("category mapping fixed amount currency %s does not match store currency %s", pricing.MarkupCurrency, targetCode)
	}
	return pricing, nil
}

func inheritSupplierCategoryProductPolicy(mapping *model.ProductMapping, binding model.SupplierCategoryMapping, targetCode string) error {
	pricing, err := supplierCategoryProductPricing(binding, targetCode)
	if err != nil {
		return err
	}
	mapping.SupplierCategoryMappingID = &binding.ID
	mapping.InheritCategoryPolicy = true
	mapping.PriceMode = pricing.PriceMode
	mapping.MarkupBasisPoint = pricing.MarkupBasisPoint
	mapping.MarkupAmount = pricing.MarkupAmount
	mapping.MarkupCurrency = pricing.MarkupCurrency
	mapping.FixedPrice = 0
	mapping.AutoSyncPrice = binding.SyncPrice
	mapping.AutoSyncStock = binding.SyncStock
	mapping.AutoSyncTitle = binding.SyncTitle
	return nil
}

func inheritedSupplierCategoryProductColumns(mapping model.ProductMapping) map[string]any {
	return map[string]any{
		"supplier_category_mapping_id": mapping.SupplierCategoryMappingID,
		"inherit_category_policy":      mapping.InheritCategoryPolicy,
		"price_mode":                   mapping.PriceMode,
		"markup_basis_point":           mapping.MarkupBasisPoint,
		"markup_amount":                mapping.MarkupAmount,
		"markup_currency":              mapping.MarkupCurrency,
		"fixed_price":                  mapping.FixedPrice,
		"auto_sync_price":              mapping.AutoSyncPrice,
		"auto_sync_stock":              mapping.AutoSyncStock,
		"auto_sync_title":              mapping.AutoSyncTitle,
	}
}

func activeSupplierCategoryMapping(tx *gorm.DB, supplierID uuid.UUID, externalCategoryID string) (model.SupplierCategoryMapping, error) {
	var mapping model.SupplierCategoryMapping
	err := tx.Where(`supplier_id = ? AND external_category_id = ? AND category_id IS NOT NULL AND enabled = ?
		AND category_id IN (SELECT id FROM categories WHERE deleted_at IS NULL)`, supplierID, externalCategoryID, true).First(&mapping).Error
	return mapping, err
}

func supplierCategoryParentWouldCycle(tx *gorm.DB, childID, parentID uuid.UUID) (bool, error) {
	if childID == parentID {
		return true, nil
	}
	var cycle bool
	err := tx.Raw(`WITH RECURSIVE ancestors AS (
		SELECT id, parent_id FROM categories WHERE id = ? AND deleted_at IS NULL
		UNION
		SELECT c.id, c.parent_id
		FROM categories c JOIN ancestors a ON c.id = a.parent_id
		WHERE c.deleted_at IS NULL
	)
	SELECT EXISTS (SELECT 1 FROM ancestors WHERE id = ?)`, parentID, childID).Scan(&cycle).Error
	return cycle, err
}

func (w *Worker) applySupplierCategoryPolicy(tx *gorm.DB, supplierModel model.Supplier, policy model.SupplierSyncPolicy, categories []supply.Category, run *model.SupplierSyncRun) ([]supplierCategoryMediaWork, error) {
	if !policy.AutoSyncCategories {
		return nil, nil
	}
	mediaWork := make([]supplierCategoryMediaWork, 0)
	now := time.Now().UTC()
	categoryByExternal := make(map[string]supply.Category, len(categories))
	for _, category := range categories {
		categoryByExternal[category.ExternalID] = category
	}
	for _, category := range categories {
		var mapping model.SupplierCategoryMapping
		err := tx.Where("supplier_id = ? AND external_category_id = ?", supplierModel.ID, category.ExternalID).First(&mapping).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			var tombstones int64
			if err := tx.Unscoped().Model(&model.SupplierCategoryMapping{}).
				Where("supplier_id = ? AND external_category_id = ? AND deleted_at IS NOT NULL", supplierModel.ID, category.ExternalID).
				Count(&tombstones).Error; err != nil {
				return nil, err
			}
			// An operator deletion is a durable opt-out from automatic category
			// creation. An explicit catalog import may create a fresh binding.
			if tombstones > 0 || !policy.AutoCreateCategories {
				continue
			}
			local := model.Category{Name: supplierText(category.Name, 100), Slug: supplierCatalogSlug(supplierModel.Code, category.ExternalID), Description: supplierText(category.Description, 2000), Icon: "", ImageURL: "", Sort: max(category.Sort, 0), Enabled: false}
			if err := tx.Create(&local).Error; err != nil {
				return nil, err
			}
			if err := tx.Model(&local).UpdateColumn("enabled", false).Error; err != nil {
				return nil, err
			}
			mapping = model.SupplierCategoryMapping{
				SupplierID: supplierModel.ID, ExternalCategoryID: category.ExternalID,
				ExternalCategoryName: category.Name, CategoryID: &local.ID, AutoCreate: true,
				SyncName: policy.SyncTitle, SyncTitle: policy.SyncTitle, SyncDescription: policy.SyncDescription,
				SyncImage: policy.SyncMedia, MirrorRemoteImage: policy.MirrorRemoteMedia,
				SyncPrice: true, SyncStock: true, PriceMode: "fixed_markup", Enabled: true,
			}
			if err := tx.Create(&mapping).Error; err != nil {
				return nil, err
			}
			if err := tx.Model(&mapping).UpdateColumns(map[string]any{"sync_price": true, "sync_stock": true, "enabled": true}).Error; err != nil {
				return nil, err
			}
			run.CategoriesMade++
			_ = tx.Create(&model.SupplierSyncChange{RunID: run.ID, EntityType: "category", ExternalID: category.ExternalID, LocalID: &local.ID, Action: "create", ChangedFields: json.RawMessage(`["name","description"]`), Applied: true, Message: "created disabled category for operator review"}).Error
		} else if err != nil {
			return nil, err
		}
		if mapping.CategoryID == nil || !mapping.Enabled {
			continue
		}
		var localCategoryCount int64
		if err := tx.Model(&model.Category{}).Where("id = ?", *mapping.CategoryID).Count(&localCategoryCount).Error; err != nil {
			return nil, err
		}
		if localCategoryCount != 1 {
			continue
		}
		updates := map[string]any{}
		if mapping.SyncName {
			updates["name"] = supplierText(category.Name, 100)
		}
		if mapping.SyncDescription {
			updates["description"] = supplierText(category.Description, 2000)
		}
		if mapping.AutoPublish {
			updates["enabled"] = true
		}
		if mapping.SyncImage && category.ImageURL != "" {
			if mapping.MirrorRemoteImage {
				mediaWork = append(mediaWork, supplierCategoryMediaWork{MappingID: mapping.ID, CategoryID: *mapping.CategoryID, ExternalCategoryID: category.ExternalID, ImageURL: category.ImageURL, AltText: category.Name})
			} else {
				resolved, ok := resolveSupplierMediaURL(supplierModel.BaseURL, category.ImageURL)
				if !ok {
					return nil, errors.New("supplier category image URL is invalid")
				}
				updates["image_asset_id"], updates["image_url"] = nil, resolved
			}
		}
		if len(updates) > 0 {
			if err := tx.Model(&model.Category{}).Where("id = ?", *mapping.CategoryID).Updates(updates).Error; err != nil {
				return nil, err
			}
		}
		if mapping.SyncParent {
			if strings.TrimSpace(category.ExternalParentID) == "" {
				if err := tx.Model(&model.Category{}).Where("id = ?", *mapping.CategoryID).Update("parent_id", nil).Error; err != nil {
					return nil, err
				}
			} else if parent := categoryByExternal[category.ExternalParentID]; parent.ExternalID != "" {
				parentMapping, parentErr := activeSupplierCategoryMapping(tx, supplierModel.ID, parent.ExternalID)
				if parentErr == nil && parentMapping.CategoryID != nil {
					cycle, err := supplierCategoryParentWouldCycle(tx, *mapping.CategoryID, *parentMapping.CategoryID)
					if err != nil {
						return nil, err
					}
					if cycle {
						return nil, errors.New("supplier category hierarchy would create a cycle")
					}
					if err := tx.Model(&model.Category{}).Where("id = ?", *mapping.CategoryID).Update("parent_id", *parentMapping.CategoryID).Error; err != nil {
						return nil, err
					}
				} else if parentErr != nil && !errors.Is(parentErr, gorm.ErrRecordNotFound) {
					return nil, parentErr
				}
			}
		}
		if err := tx.Model(&mapping).Updates(map[string]any{
			"external_category_name": category.Name,
			"last_synced_at":         &now,
			"last_error":             "",
		}).Error; err != nil {
			return nil, err
		}
	}
	return mediaWork, nil
}

func (w *Worker) autoCreateSupplierProducts(tx *gorm.DB, supplierModel model.Supplier, policy model.SupplierSyncPolicy, products []supply.Product, target model.CurrencyDefinition, rates map[string]supplierPreparedFX, run *model.SupplierSyncRun) error {
	if !policy.AutoSyncProducts || !policy.AutoCreateProducts {
		return nil
	}
	for _, upstream := range products {
		sourceCode := supplierProductCurrency(supplierModel, upstream)
		prepared, exists := rates[sourceCode]
		if !exists {
			return fmt.Errorf("exchange rate for %s is unavailable", sourceCode)
		}
		var count int64
		if err := tx.Model(&model.ProductMapping{}).Where("supplier_id = ? AND external_product_id = ?", supplierModel.ID, upstream.ExternalID).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		categoryMapping, categoryErr := activeSupplierCategoryMapping(tx, supplierModel.ID, upstream.ExternalCategoryID)
		if upstream.ExternalCategoryID == "" || categoryErr != nil || categoryMapping.CategoryID == nil {
			if categoryErr != nil && !errors.Is(categoryErr, gorm.ErrRecordNotFound) {
				return categoryErr
			}
			run.Warnings++
			_ = tx.Create(&model.SupplierSyncChange{RunID: run.ID, EntityType: "product", ExternalID: upstream.ExternalID, Action: "skip", ChangedFields: json.RawMessage(`[]`), Applied: false, Message: "category mapping is required before automatic product creation"}).Error
			continue
		}
		deliveryType := strings.ToLower(strings.TrimSpace(upstream.FulfillmentType))
		if deliveryType != "manual" {
			deliveryType = "auto"
		}
		pricing, err := supplierCategoryProductPricing(categoryMapping, target.Code)
		if err != nil {
			return err
		}
		salePrice, convertedCost, err := supplierMappedConvertedPrice(pricing, upstream.Price, prepared.Source.MinorUnit, target.MinorUnit, prepared.Snapshot.Rate)
		if err != nil {
			return err
		}
		cost := convertedCost
		comparePrice := int64(0)
		if upstream.OriginalPrice > upstream.Price {
			comparePrice, _, err = supplierMappedConvertedPrice(pricing, upstream.OriginalPrice, prepared.Source.MinorUnit, target.MinorUnit, prepared.Snapshot.Rate)
			if err != nil {
				return err
			}
			if comparePrice <= salePrice {
				comparePrice = 0
			}
		}
		status := "draft"
		if categoryMapping.AutoPublish && upstream.Status == "active" {
			status = "on_sale"
		}
		local := model.Product{CategoryID: *categoryMapping.CategoryID, Name: supplierText(upstream.Name, 160), Slug: supplierCatalogSlug(supplierModel.Code, upstream.ExternalID), Summary: supplierText(upstream.Summary, 500), Description: supplierText(upstream.Description, 100_000), CoverURL: categoryMapping.DefaultCoverURL, Currency: target.Code, Price: salePrice, ComparePrice: comparePrice, CostPrice: cost, DeliveryType: deliveryType, InventoryMode: "supplier", MinimumPurchase: max(upstream.Minimum, 1), MaximumPurchase: max(upstream.Maximum, 0), Status: status, Tags: supplierText(strings.Join(upstream.Tags, ","), 500)}
		if err := tx.Create(&local).Error; err != nil {
			return err
		}
		inputFields, parameterMapping, err := service.NormalizeSupplierInputFields(local.ID, upstream.InputFields)
		if err != nil {
			return err
		}
		for index := range inputFields {
			if err := tx.Create(&inputFields[index]).Error; err != nil {
				return err
			}
		}
		encodedMapping, err := service.EncodeSupplierParameterMapping(parameterMapping)
		if err != nil {
			return err
		}
		mapping := model.ProductMapping{SupplierID: supplierModel.ID, SupplierCategoryMappingID: &categoryMapping.ID, InheritCategoryPolicy: true, ProductID: local.ID, ExternalProductID: upstream.ExternalID, ParameterMapping: encodedMapping, PriceMode: categoryMapping.PriceMode, MarkupBasisPoint: categoryMapping.MarkupBasisPoint, MarkupAmount: categoryMapping.MarkupAmount, MarkupCurrency: target.Code, FixedPriceCurrency: target.Code, LastUpstreamPrice: upstream.Price, LastUpstreamCurrency: sourceCode, LastConvertedCost: cost, LastFXSnapshotID: &prepared.Snapshot.ID, AutoSyncPrice: categoryMapping.SyncPrice, AutoSyncStock: categoryMapping.SyncStock, AutoSyncTitle: categoryMapping.SyncTitle, AutoSyncSummary: policy.SyncSummary, AutoSyncDescription: policy.SyncDescription, AutoSyncMedia: policy.SyncMedia && categoryMapping.DefaultCoverURL == "", MirrorRemoteMedia: policy.MirrorRemoteMedia, AutoSyncCategory: policy.AutoSyncCategories, AutoSyncVariants: policy.SyncVariants, AutoSyncStatus: policy.SyncStatus, AutoSyncLimits: policy.SyncPurchaseLimits}
		if err := tx.Create(&mapping).Error; err != nil {
			return err
		}
		if err := tx.Model(&mapping).Updates(map[string]any{"supplier_category_mapping_id": categoryMapping.ID, "inherit_category_policy": true, "auto_sync_price": mapping.AutoSyncPrice, "auto_sync_stock": mapping.AutoSyncStock, "auto_sync_title": mapping.AutoSyncTitle, "auto_sync_summary": mapping.AutoSyncSummary, "auto_sync_description": mapping.AutoSyncDescription, "auto_sync_media": mapping.AutoSyncMedia, "mirror_remote_media": mapping.MirrorRemoteMedia, "auto_sync_category": mapping.AutoSyncCategory, "auto_sync_variants": mapping.AutoSyncVariants, "auto_sync_status": mapping.AutoSyncStatus, "auto_sync_limits": mapping.AutoSyncLimits}).Error; err != nil {
			return err
		}
		run.ProductsMade++
		_ = tx.Create(&model.SupplierSyncChange{RunID: run.ID, EntityType: "product", ExternalID: upstream.ExternalID, LocalID: &local.ID, Action: "create", ChangedFields: json.RawMessage(`["name","summary","description","price","stock"]`), Applied: true, Message: "created draft supplier product for operator review"}).Error
	}
	return nil
}

func (w *Worker) applySupplierMappings(tx *gorm.DB, supplierModel model.Supplier, policy model.SupplierSyncPolicy, remote map[string]supply.Product, target model.CurrencyDefinition, rates map[string]supplierPreparedFX, now time.Time, run *model.SupplierSyncRun) ([]struct {
	mapping model.ProductMapping
	product supply.Product
}, error) {
	var mappings []model.ProductMapping
	if err := tx.Where("supplier_id = ?", supplierModel.ID).Find(&mappings).Error; err != nil {
		return nil, err
	}
	mediaWork := make([]struct {
		mapping model.ProductMapping
		product supply.Product
	}, 0)
	for _, mapping := range mappings {
		upstream, exists := remote[mapping.ExternalProductID]
		if !exists {
			if mapping.VariantID != nil {
				// Variant freshness is reconciled by the parent product pass
				// in ensureSupplierVariantMappings; defer to it.
				continue
			}
			updates := map[string]any{"last_error": "external product missing", "last_synced_at": &now}
			if policy.MissingProductAction == "unpublish" && mapping.VariantID == nil {
				if err := tx.Model(&model.Product{}).Where("id = ? AND status = ?", mapping.ProductID, "on_sale").Update("status", "off_sale").Error; err != nil {
					return nil, err
				}
			}
			if policy.MissingProductAction == "disable_mapping" {
				legacyQuery := tx.Where("supplier_id = ? AND product_id = ?", mapping.SupplierID, mapping.ProductID)
				if mapping.VariantID == nil {
					legacyQuery = legacyQuery.Where("variant_id IS NULL")
				} else {
					legacyQuery = legacyQuery.Where("variant_id = ?", *mapping.VariantID)
				}
				if err := legacyQuery.Delete(&model.SupplierProduct{}).Error; err != nil {
					return nil, err
				}
				if err := tx.Delete(&mapping).Error; err != nil {
					return nil, err
				}
				run.ProductsUpdated++
				_ = tx.Create(&model.SupplierSyncChange{RunID: run.ID, EntityType: "product_mapping", ExternalID: mapping.ExternalProductID, LocalID: &mapping.ProductID, Action: "disable", ChangedFields: json.RawMessage(`[]`), Applied: true, Message: "disabled mapping because upstream product is missing"}).Error
				continue
			}
			if err := tx.Model(&mapping).Updates(updates).Error; err != nil {
				return nil, err
			}
			continue
		}
		productUpdates := map[string]any{}
		variantUpdates := map[string]any{}
		mappingUpdates := map[string]any{"last_error": "", "last_synced_at": &now}
		if mapping.InheritCategoryPolicy {
			categoryBinding, categoryErr := activeSupplierCategoryMapping(tx, supplierModel.ID, upstream.ExternalCategoryID)
			if categoryErr == nil {
				if err := inheritSupplierCategoryProductPolicy(&mapping, categoryBinding, target.Code); err != nil {
					return nil, err
				}
				for column, value := range inheritedSupplierCategoryProductColumns(mapping) {
					mappingUpdates[column] = value
				}
				if mapping.VariantID == nil {
					if categoryBinding.CategoryID != nil {
						productUpdates["category_id"] = *categoryBinding.CategoryID
					}
					if categoryBinding.DefaultCoverURL != "" {
						productUpdates["cover_url"] = categoryBinding.DefaultCoverURL
					}
				}
			} else if errors.Is(categoryErr, gorm.ErrRecordNotFound) {
				mapping.AutoSyncPrice, mapping.AutoSyncStock, mapping.AutoSyncTitle = false, false, false
				mappingUpdates["auto_sync_price"] = false
				mappingUpdates["auto_sync_stock"] = false
				mappingUpdates["auto_sync_title"] = false
				mappingUpdates["last_error"] = "category binding unavailable"
			} else {
				return nil, categoryErr
			}
		}
		if mapping.AutoSyncPrice {
			sourceCode := supplierProductCurrency(supplierModel, upstream)
			prepared, exists := rates[sourceCode]
			if !exists {
				return nil, fmt.Errorf("exchange rate for %s is unavailable", sourceCode)
			}
			if mapping.PriceMode == "fixed_amount" && strings.ToUpper(strings.TrimSpace(mapping.MarkupCurrency)) != target.Code {
				return nil, fmt.Errorf("fixed amount markup currency must match store currency")
			}
			localPrice, convertedCost, err := supplierMappedConvertedPrice(mapping, upstream.Price, prepared.Source.MinorUnit, target.MinorUnit, prepared.Snapshot.Rate)
			var priceFXSnapshotID *uuid.UUID
			if err == nil && mapping.PriceMode == "fixed_price" {
				fixedFX, available := rates[strings.ToUpper(strings.TrimSpace(mapping.FixedPriceCurrency))]
				if !available {
					err = fmt.Errorf("fixed price exchange rate is unavailable")
				} else {
					localPrice, err = currency.Convert(mapping.FixedPrice, fixedFX.Source.MinorUnit, target.MinorUnit, fixedFX.Snapshot.Rate)
					if err == nil {
						priceFXSnapshotID = &fixedFX.Snapshot.ID
					}
				}
			}
			if err != nil {
				if err := tx.Model(&mapping).Updates(map[string]any{"last_error": "pricing rule invalid", "last_synced_at": &now}).Error; err != nil {
					return nil, err
				}
				continue
			}
			productUpdates["price"], productUpdates["cost_price"], productUpdates["currency"] = localPrice, convertedCost, target.Code
			variantUpdates["price"], variantUpdates["cost_price"] = localPrice, convertedCost
			mappingUpdates["last_upstream_price"], mappingUpdates["last_upstream_currency"] = upstream.Price, sourceCode
			mappingUpdates["last_converted_cost"], mappingUpdates["last_fx_snapshot_id"] = convertedCost, prepared.Snapshot.ID
			mappingUpdates["last_price_fx_snapshot_id"] = priceFXSnapshotID
		}
		if mapping.AutoSyncTitle {
			if mapping.VariantID == nil {
				productUpdates["name"] = supplierText(upstream.Name, 160)
			} else {
				variantUpdates["name"] = supplierText(upstream.Name, 160)
			}
		}
		if mapping.AutoSyncSummary && mapping.VariantID == nil {
			productUpdates["summary"] = supplierText(upstream.Summary, 500)
		}
		deferDescriptionForMedia := mapping.AutoSyncDescription && mapping.AutoSyncMedia && mapping.MirrorRemoteMedia && len(supplierDescriptionImages(upstream.Description, supplierModel.BaseURL)) > 0
		if mapping.AutoSyncDescription && mapping.VariantID == nil && !deferDescriptionForMedia {
			productUpdates["description"] = supplierText(upstream.Description, 100_000)
		}
		if mapping.AutoSyncStatus {
			if mapping.VariantID == nil {
				if upstream.Status == "active" {
					productUpdates["status"] = "on_sale"
				} else {
					productUpdates["status"] = "off_sale"
				}
			} else if upstream.Status == "active" {
				variantUpdates["status"] = "active"
			} else {
				variantUpdates["status"] = "inactive"
			}
		}
		if mapping.AutoSyncCategory && mapping.VariantID == nil && upstream.ExternalCategoryID != "" {
			categoryMapping, categoryErr := activeSupplierCategoryMapping(tx, supplierModel.ID, upstream.ExternalCategoryID)
			if categoryErr == nil && categoryMapping.CategoryID != nil {
				productUpdates["category_id"] = *categoryMapping.CategoryID
			} else if categoryErr != nil && !errors.Is(categoryErr, gorm.ErrRecordNotFound) {
				return nil, categoryErr
			}
		}
		if mapping.AutoSyncLimits && mapping.VariantID != nil && upstream.Maximum > 0 {
			variantUpdates["purchase_limit"] = upstream.Maximum
		}
		if mapping.AutoSyncLimits && mapping.VariantID == nil {
			productUpdates["minimum_purchase"] = max(upstream.Minimum, 1)
			productUpdates["maximum_purchase"] = max(upstream.Maximum, 0)
		}
		var result *gorm.DB
		if mapping.VariantID == nil {
			if len(productUpdates) > 0 {
				result = tx.Model(&model.Product{}).Where("id = ? AND inventory_mode = ?", mapping.ProductID, "supplier").Updates(productUpdates)
			}
		} else if len(variantUpdates) > 0 {
			result = tx.Model(&model.ProductVariant{}).Where("id = ? AND product_id = ?", *mapping.VariantID, mapping.ProductID).Updates(variantUpdates)
		}
		if result != nil && result.Error != nil {
			return nil, result.Error
		}
		externalStock := upstream.Stock
		if !mapping.AutoSyncStock {
			// A disabled stock switch freezes the last observed executable
			// capacity; writing zero here would make every toggle look sold out.
			var existingStock model.SupplierProduct
			stockQuery := tx.Where("supplier_id = ? AND product_id = ? AND external_id = ?", supplierModel.ID, mapping.ProductID, upstream.ExternalID)
			if mapping.VariantID == nil {
				stockQuery = stockQuery.Where("variant_id IS NULL")
			} else {
				stockQuery = stockQuery.Where("variant_id = ?", *mapping.VariantID)
			}
			if stockQuery.First(&existingStock).Error == nil {
				externalStock = existingStock.ExternalStock
			}
		}
		autoSync := mapping.AutoSyncPrice || mapping.AutoSyncStock
		var legacy model.SupplierProduct
		lookup := tx.Where("supplier_id = ? AND product_id = ? AND external_id = ?", supplierModel.ID, mapping.ProductID, upstream.ExternalID)
		if mapping.VariantID == nil {
			lookup = lookup.Where("variant_id IS NULL")
		} else {
			lookup = lookup.Where("variant_id = ?", *mapping.VariantID)
		}
		lookupErr := lookup.First(&legacy).Error
		legacyUpdates := map[string]any{"external_price": upstream.Price, "price_markup_rate": mapping.MarkupBasisPoint, "auto_sync": autoSync}
		if mapping.AutoSyncStock {
			legacyUpdates["external_stock"] = externalStock
		}
		if lookupErr == nil {
			if err := tx.Model(&legacy).Updates(legacyUpdates).Error; err != nil {
				return nil, err
			}
		} else if errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			legacy = model.SupplierProduct{SupplierID: supplierModel.ID, ProductID: mapping.ProductID, VariantID: mapping.VariantID, ExternalID: upstream.ExternalID, ExternalPrice: upstream.Price, ExternalStock: externalStock, PriceMarkupRate: mapping.MarkupBasisPoint, AutoSync: autoSync}
			if err := tx.Create(&legacy).Error; err != nil {
				return nil, err
			}
			if err := tx.Model(&legacy).Updates(legacyUpdates).Error; err != nil {
				return nil, err
			}
		} else {
			return nil, lookupErr
		}
		if err := tx.Model(&mapping).Updates(mappingUpdates).Error; err != nil {
			return nil, err
		}
		run.ProductsUpdated++
		if mapping.VariantID == nil && mapping.AutoSyncVariants {
			if err := w.ensureSupplierVariantMappings(tx, supplierModel, mapping, upstream, target, rates, remote, now, run); err != nil {
				return nil, err
			}
		}
		if mapping.AutoSyncMedia && mapping.VariantID == nil {
			mediaWork = append(mediaWork, struct {
				mapping model.ProductMapping
				product supply.Product
			}{mapping: mapping, product: upstream})
		}
	}
	return mediaWork, nil
}

// syncSupplierProductMedia mirrors the complete upstream media set (cover,
// gallery and images embedded in the description).  A failed download never
// replaces an already published local asset; callers record the error and the
// next sync retries it.  When mirroring is disabled we retain the upstream URL
// for compatibility, but do not create CatalogMedia rows with a missing local
// asset foreign key.
func (w *Worker) syncSupplierProductMedia(ctx context.Context, supplierModel model.Supplier, mapping model.ProductMapping, upstream supply.Product) error {
	if !mapping.MirrorRemoteMedia {
		cover := strings.TrimSpace(upstream.CoverURL)
		if cover == "" && len(upstream.ImageURLs) > 0 {
			cover = strings.TrimSpace(upstream.ImageURLs[0])
		}
		if cover != "" {
			resolved, ok := resolveSupplierMediaURL(supplierModel.BaseURL, cover)
			if !ok {
				return errors.New("supplier cover URL is invalid")
			}
			if err := w.db.Model(&model.Product{}).Where("id = ?", mapping.ProductID).Update("cover_url", resolved).Error; err != nil {
				return err
			}
		}
		if mapping.AutoSyncDescription {
			return w.db.Model(&model.Product{}).Where("id = ?", mapping.ProductID).Update("description", supplierText(upstream.Description, 100_000)).Error
		}
		return nil
	}
	specs, err := supplierProductMediaSpecs(supplierModel, upstream)
	if err != nil {
		return err
	}
	if len(specs) == 0 {
		if mapping.AutoSyncDescription {
			return w.db.Model(&model.Product{}).Where("id = ?", mapping.ProductID).Update("description", supplierText(upstream.Description, 100_000)).Error
		}
		return nil
	}
	download, err := w.downloadSupplierMedia(ctx, specs)
	if err != nil {
		return err
	}
	_, err = w.persistSupplierCatalogMedia("product", mapping.ProductID, download, upstream.Description, supplierModel.BaseURL, mapping.AutoSyncDescription)
	return err
}

func (w *Worker) syncSupplierCatalog(ctx context.Context, supplierModel model.Supplier, trigger string) (returnErr error) {
	run, err := w.beginSupplierSync(supplierModel, trigger)
	if errors.Is(err, errSupplierSyncInProgress) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			w.finishSupplierSync(&run, "failed", returnErr)
		}
	}()
	gateway, _, err := w.gatewayForSupplier(supplierModel)
	if err != nil {
		return err
	}
	categories, categoryErr := gateway.Categories(ctx)
	if categoryErr != nil && !errors.Is(categoryErr, supply.ErrCapabilityUnsupported) {
		return categoryErr
	}
	products, err := gateway.Products(ctx)
	if err != nil {
		return err
	}
	if err := normalizeSupplierSnapshotIdentities(categories, products); err != nil {
		return err
	}
	balance, balanceErr := gateway.Balance(ctx)
	if balanceErr != nil && !errors.Is(balanceErr, supply.ErrCapabilityUnsupported) {
		return balanceErr
	}
	targetCurrency, rates, err := w.prepareSupplierFX(ctx, supplierModel, products)
	if err != nil {
		return err
	}
	for code, prepared := range rates {
		if code != targetCurrency.Code || len(rates) == 1 {
			run.FXSnapshotID = &prepared.Snapshot.ID
			break
		}
	}
	run.CategoriesSeen, run.ProductsSeen = len(categories), len(products)
	remote := make(map[string]supply.Product, len(products))
	for _, product := range products {
		remote[product.ExternalID] = product
	}
	seenAt := time.Now().UTC()
	var mediaWork []struct {
		mapping model.ProductMapping
		product supply.Product
	}
	var categoryMediaWork []supplierCategoryMediaWork
	err = w.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&model.Supplier{}, "id = ?", supplierModel.ID).Error; err != nil {
			return err
		}
		if err := w.upsertSupplierSnapshots(tx, supplierModel, categories, products, seenAt); err != nil {
			return err
		}
		var policy model.SupplierSyncPolicy
		if err := tx.Where("supplier_id = ?", supplierModel.ID).First(&policy).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			policy = model.SupplierSyncPolicy{SupplierID: supplierModel.ID, MissingProductAction: "keep"}
		} else if err != nil {
			return err
		}
		var categoryErr error
		categoryMediaWork, categoryErr = w.applySupplierCategoryPolicy(tx, supplierModel, policy, categories, &run)
		if categoryErr != nil {
			return categoryErr
		}
		if err := w.autoCreateSupplierProducts(tx, supplierModel, policy, products, targetCurrency, rates, &run); err != nil {
			return err
		}
		var mappingErr error
		mediaWork, mappingErr = w.applySupplierMappings(tx, supplierModel, policy, remote, targetCurrency, rates, seenAt, &run)
		if mappingErr != nil {
			return mappingErr
		}
		updates := map[string]any{"last_sync_at": &seenAt}
		if balanceErr == nil {
			currency := strings.ToUpper(strings.TrimSpace(balance.Currency))
			if currency == "" {
				currency = supplierModel.BalanceCurrency
			}
			updates["balance"], updates["balance_currency"], updates["balance_synced_at"] = balance.Balance, supplierText(currency, 8), &seenAt
		}
		return tx.Model(&model.Supplier{}).Where("id = ?", supplierModel.ID).Updates(updates).Error
	})
	if err != nil {
		return err
	}
	for _, work := range categoryMediaWork {
		resolved, ok := resolveSupplierMediaURL(supplierModel.BaseURL, work.ImageURL)
		if !ok {
			run.Warnings++
			continue
		}
		stored, mirrorErr := media.New(w.cfg).MirrorImage(ctx, resolved)
		if mirrorErr != nil {
			run.Warnings++
			_ = w.db.Model(&model.SupplierCategoryMapping{}).Where("id = ?", work.MappingID).Update("last_error", truncate("media sync: "+mirrorErr.Error(), 1000)).Error
			continue
		}
		download := supplierMediaDownload{Items: []supplierDownloadedMedia{{Spec: supplierMediaSpec{SourceURL: resolved, Role: "cover", AltText: work.AltText}, Object: stored}}, SourceHashes: map[string]string{resolved: supplierMediaHash(stored)}}
		if _, persistErr := w.persistSupplierCatalogMedia("category", work.CategoryID, download, "", supplierModel.BaseURL, false); persistErr != nil {
			run.Warnings++
			_ = w.db.Model(&model.SupplierCategoryMapping{}).Where("id = ?", work.MappingID).Update("last_error", truncate("media persist: "+persistErr.Error(), 1000)).Error
			continue
		}
		_ = w.db.Model(&model.SupplierCategoryMapping{}).Where("id = ?", work.MappingID).Updates(map[string]any{"last_synced_at": time.Now().UTC(), "last_error": ""}).Error
		run.MediaMirrored++
	}
	for _, work := range mediaWork {
		if err := w.syncSupplierProductMedia(ctx, supplierModel, work.mapping, work.product); err != nil {
			run.Warnings++
			_ = w.db.Model(&model.ProductMapping{}).Where("id = ?", work.mapping.ID).Update("last_error", truncate("media sync: "+err.Error(), 1000)).Error
			_ = w.db.Create(&model.SupplierSyncChange{RunID: run.ID, EntityType: "product", ExternalID: work.product.ExternalID, LocalID: &work.mapping.ProductID, Action: "error", ChangedFields: json.RawMessage(`["media"]`), Applied: false, Message: truncate(err.Error(), 1000)}).Error
			continue
		}
		run.MediaMirrored++
	}
	w.finishSupplierSync(&run, "succeeded", nil)
	return nil
}

func supplierSyncSummary(run model.SupplierSyncRun) string {
	return fmt.Sprintf("categories=%d products=%d created=%d updated=%d media=%d warnings=%d", run.CategoriesSeen, run.ProductsSeen, run.ProductsMade, run.ProductsUpdated, run.MediaMirrored, run.Warnings)
}
