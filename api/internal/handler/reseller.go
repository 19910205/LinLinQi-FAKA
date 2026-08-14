package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/i18n"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/service"
	"linlinqi/api/pkg/response"
)

func activeResellerForUser(tx *gorm.DB, userID uuid.UUID) (model.ResellerProfile, error) {
	var profile model.ResellerProfile
	if err := tx.Where("user_id = ?", userID).First(&profile).Error; err != nil {
		return profile, err
	}
	if profile.Status != "active" {
		return profile, errors.New("reseller account is not active")
	}
	return profile, nil
}

func normalizeResellerDomain(value string) (string, error) {
	value = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
	ascii, err := idna.Lookup.ToASCII(value)
	if err != nil || len(ascii) < 4 || len(ascii) > 253 || strings.Contains(ascii, ":") {
		return "", errors.New("invalid domain")
	}
	if _, err := netip.ParseAddr(ascii); err == nil || ascii == "localhost" || strings.HasSuffix(ascii, ".local") {
		return "", errors.New("public domain required")
	}
	if _, err := publicsuffix.EffectiveTLDPlusOne(ascii); err != nil {
		return "", errors.New("registrable domain required")
	}
	return ascii, nil
}

type resellerDomainRequest struct {
	Domain string `json:"domain"`
}

func (h Handler) CreateResellerDomain(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req resellerDomainRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42284, "error.domain_format_invalid")
		return
	}
	domain, err := normalizeResellerDomain(req.Domain)
	if err != nil {
		response.Error(c, 422, 42284, "error.public_domain_required")
		return
	}
	var item model.ResellerDomain
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		profile, err := activeResellerForUser(tx, userID)
		if err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&model.ResellerDomain{}).Where("reseller_id = ?", profile.ID).Count(&count).Error; err != nil {
			return err
		}
		if count >= 5 {
			return errors.New("domain limit reached")
		}
		item = model.ResellerDomain{
			ResellerID: profile.ID, Domain: domain, Status: "pending_verification", TLSStatus: "pending",
			VerificationToken: "linlinqi-site-verification=" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		}
		return tx.Create(&item).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40470, "error.reseller_application_required")
		return
	}
	if err != nil {
		response.Error(c, 409, 40972, "error.reseller_domain_limit_reached")
		return
	}
	response.Created(c, gin.H{"domain": item, "dns_name": "_linlinqi." + item.Domain, "dns_value": item.VerificationToken})
}

func (h Handler) VerifyResellerDomain(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	domainID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42285, "error.domain_id_invalid")
		return
	}
	var item model.ResellerDomain
	var profile model.ResellerProfile
	if err := h.DB.Where("user_id = ? AND status = ?", userID, "active").First(&profile).Error; err != nil || h.DB.Where("id = ? AND reseller_id = ?", domainID, profile.ID).First(&item).Error != nil {
		response.Error(c, 404, 40471, "error.domain_or_reseller_disabled")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	records, err := net.DefaultResolver.LookupTXT(ctx, "_linlinqi."+item.Domain)
	verified := err == nil
	if verified {
		verified = false
		for _, record := range records {
			if strings.TrimSpace(record) == item.VerificationToken {
				verified = true
				break
			}
		}
	}
	if !verified {
		response.Error(c, 409, 40973, "error.dns_txt_record_not_found")
		return
	}
	now := time.Now()
	if err := h.DB.Model(&model.ResellerDomain{}).Where("id = ? AND reseller_id = ?", item.ID, profile.ID).Updates(map[string]any{"status": "verified", "verified_at": &now, "tls_status": "pending"}).Error; err != nil {
		response.Error(c, 500, 50073, "error.domain_verification_save_failed")
		return
	}
	item.Status, item.VerifiedAt = "verified", &now
	response.OK(c, gin.H{"domain": item, "notice": i18n.Localize(c, "notice.domain_tls_pending")})
}

func (h Handler) DeleteResellerDomain(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	domainID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42285, "error.domain_id_invalid")
		return
	}
	result := h.DB.Where("id = ? AND reseller_id IN (SELECT id FROM reseller_profiles WHERE user_id = ? AND deleted_at IS NULL)", domainID, userID).Delete(&model.ResellerDomain{})
	if result.Error != nil {
		response.Error(c, 500, 50074, "error.domain_delete_failed")
		return
	}
	if result.RowsAffected == 0 {
		response.Error(c, 404, 40471, "error.domain_not_found")
		return
	}
	response.OK(c, gin.H{"deleted": true})
}

type resellerThemeRequest struct {
	Mode    string `json:"mode"`
	Density string `json:"density"`
}

type resellerSEORequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type resellerSupportRequest struct {
	Email string `json:"email"`
	URL   string `json:"url"`
}

type resellerSiteRequest struct {
	SiteName string                 `json:"site_name"`
	LogoURL  string                 `json:"logo_url"`
	Theme    resellerThemeRequest   `json:"theme"`
	SEO      resellerSEORequest     `json:"seo"`
	Support  resellerSupportRequest `json:"support"`
}

func validateOptionalPublicHTTPS(value string) bool {
	if value == "" {
		return true
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() != "" && parsed.User == nil && parsed.Fragment == ""
}

func (h Handler) UpdateResellerSite(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req resellerSiteRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42286, "error.site_config_format_invalid")
		return
	}
	req.SiteName, req.LogoURL = strings.TrimSpace(req.SiteName), strings.TrimSpace(req.LogoURL)
	req.Theme.Mode, req.Theme.Density = strings.ToLower(strings.TrimSpace(req.Theme.Mode)), strings.ToLower(strings.TrimSpace(req.Theme.Density))
	req.SEO.Title, req.SEO.Description = strings.TrimSpace(req.SEO.Title), strings.TrimSpace(req.SEO.Description)
	req.Support.Email, req.Support.URL = strings.ToLower(strings.TrimSpace(req.Support.Email)), strings.TrimSpace(req.Support.URL)
	if utf8.RuneCountInString(req.SiteName) < 2 || utf8.RuneCountInString(req.SiteName) > 160 || (req.Theme.Mode != "light" && req.Theme.Mode != "dark" && req.Theme.Mode != "system") || (req.Theme.Density != "comfortable" && req.Theme.Density != "compact") || utf8.RuneCountInString(req.SEO.Title) > 160 || utf8.RuneCountInString(req.SEO.Description) > 500 || (req.Support.Email != "" && (!strings.Contains(req.Support.Email, "@") || utf8.RuneCountInString(req.Support.Email) > 190)) || !validateOptionalPublicHTTPS(req.LogoURL) || !validateOptionalPublicHTTPS(req.Support.URL) {
		response.Error(c, 422, 42286, "error.site_settings_invalid")
		return
	}
	theme, _ := json.Marshal(req.Theme)
	seo, _ := json.Marshal(req.SEO)
	support, _ := json.Marshal(req.Support)
	var site model.ResellerSite
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		profile, err := activeResellerForUser(tx, userID)
		if err != nil {
			return err
		}
		if err := tx.Where("reseller_id = ?", profile.ID).First(&site).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			site = model.ResellerSite{ResellerID: profile.ID, SiteName: req.SiteName, LogoURL: req.LogoURL, Theme: string(theme), SEO: string(seo), Support: string(support)}
			return tx.Create(&site).Error
		} else if err != nil {
			return err
		}
		if err := tx.Model(&site).Updates(map[string]any{"site_name": req.SiteName, "logo_url": req.LogoURL, "theme": string(theme), "seo": string(seo), "support": string(support)}).Error; err != nil {
			return err
		}
		site.SiteName, site.LogoURL, site.Theme, site.SEO, site.Support = req.SiteName, req.LogoURL, string(theme), string(seo), string(support)
		return nil
	})
	if err != nil {
		response.Error(c, 409, 40974, "error.reseller_site_config_save_failed")
		return
	}
	response.OK(c, gin.H{"site": site})
}

func (h Handler) ResellerCatalog(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	profile, err := activeResellerForUser(h.DB, userID)
	if err != nil {
		response.Error(c, 409, 40972, "error.reseller_account_not_enabled")
		return
	}
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.Product{}).Preload("Category").Where("status = ?", "on_sale")
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("name ILIKE ? OR summary ILIKE ?", like, like)
	}
	var total int64
	var products []model.Product
	if err := query.Count(&total).Error; err != nil || query.Order("sort DESC, created_at DESC").Offset((page-1)*pageSize).Limit(pageSize).Find(&products).Error != nil {
		response.Error(c, 500, 50075, "error.resellable_product_list_fetch_failed")
		return
	}
	items := make([]gin.H, 0, len(products))
	for _, product := range products {
		var variants []model.ProductVariant
		var rules []model.ResellerProductRule
		h.DB.Where("product_id = ? AND status = ?", product.ID, "active").Order("sort DESC, created_at ASC").Find(&variants)
		h.DB.Where("reseller_id = ? AND product_id = ?", profile.ID, product.ID).Order("created_at ASC").Find(&rules)
		publicVariants := make([]publicProductVariantDTO, 0, len(variants))
		for _, variant := range variants {
			publicVariants = append(publicVariants, toPublicProductVariantDTO(variant, h.productStockForVariant(product, &variant.ID)))
		}
		items = append(items, gin.H{"product": toPublicProductDTO(product), "stock": h.productStock(product), "variants": publicVariants, "rules": rules})
	}
	response.Page(c, items, total, page, pageSize)
}

type resellerProductRuleRequest struct {
	VariantID        *uuid.UUID `json:"variant_id"`
	Enabled          bool       `json:"enabled"`
	PricingMode      string     `json:"pricing_mode"`
	MarkupBasisPoint int        `json:"markup_basis_point"`
	FixedPrice       int64      `json:"fixed_price"`
}

func (h Handler) UpsertResellerProductRule(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	productID, err := uuid.Parse(c.Param("product_id"))
	if err != nil {
		response.Error(c, 422, 42287, "error.product_id_invalid")
		return
	}
	var req resellerProductRuleRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42288, "error.product_pricing_rule_invalid")
		return
	}
	req.PricingMode = strings.ToLower(strings.TrimSpace(req.PricingMode))
	if req.PricingMode != "markup" && req.PricingMode != "fixed" {
		response.Error(c, 422, 42288, "error.pricing_method_invalid")
		return
	}
	if req.MarkupBasisPoint < 0 || req.MarkupBasisPoint > 10000 || req.FixedPrice < 0 {
		response.Error(c, 422, 42288, "error.pricing_value_out_of_range")
		return
	}
	var rule model.ResellerProductRule
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		profile, err := activeResellerForUser(tx, userID)
		if err != nil {
			return err
		}
		var product model.Product
		if err := tx.Where("id = ? AND status = ?", productID, "on_sale").First(&product).Error; err != nil {
			return err
		}
		platformPrice := product.Price
		if req.VariantID != nil {
			var variant model.ProductVariant
			if err := tx.Where("id = ? AND product_id = ? AND status = ?", *req.VariantID, product.ID, "active").First(&variant).Error; err != nil {
				return err
			}
			platformPrice = variant.Price
		} else {
			var maximumVariantPrice int64
			if err := tx.Model(&model.ProductVariant{}).Where("product_id = ? AND status = ?", product.ID, "active").Select("COALESCE(MAX(price), 0)").Scan(&maximumVariantPrice).Error; err != nil {
				return err
			}
			if maximumVariantPrice > platformPrice {
				platformPrice = maximumVariantPrice
			}
		}
		if req.PricingMode == "fixed" {
			maximum := int64(math.MaxInt64)
			if platformPrice <= math.MaxInt64/10 {
				maximum = platformPrice * 10
			}
			if req.FixedPrice < platformPrice || req.FixedPrice > maximum {
				return errors.New("fixed price outside allowed range")
			}
		}
		lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("reseller_id = ? AND product_id = ?", profile.ID, product.ID)
		if req.VariantID == nil {
			lookup = lookup.Where("variant_id IS NULL")
		} else {
			lookup = lookup.Where("variant_id = ?", *req.VariantID)
		}
		findErr := lookup.First(&rule).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			rule = model.ResellerProductRule{ResellerID: profile.ID, ProductID: product.ID, VariantID: req.VariantID, Enabled: req.Enabled, PricingMode: req.PricingMode, Currency: product.Currency, MarkupBasisPoint: req.MarkupBasisPoint, FixedPrice: req.FixedPrice}
			return createWithExplicitColumns(tx, &rule, map[string]any{"enabled": req.Enabled})
		}
		if findErr != nil {
			return findErr
		}
		if err := tx.Model(&rule).Updates(map[string]any{"enabled": req.Enabled, "pricing_mode": req.PricingMode, "currency": product.Currency, "markup_basis_point": req.MarkupBasisPoint, "fixed_price": req.FixedPrice}).Error; err != nil {
			return err
		}
		rule.Enabled, rule.PricingMode, rule.Currency, rule.MarkupBasisPoint, rule.FixedPrice = req.Enabled, req.PricingMode, product.Currency, req.MarkupBasisPoint, req.FixedPrice
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40472, "error.product_spec_or_reseller_not_found")
		return
	}
	if err != nil {
		response.Error(c, 409, 40975, "error.reseller_pricing_below_platform_or_save_failed")
		return
	}
	rule.Enabled = req.Enabled
	response.OK(c, gin.H{"rule": rule})
}

func (h Handler) MyResellerOrders(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var profile model.ResellerProfile
	if h.DB.Where("user_id = ?", userID).First(&profile).Error != nil {
		response.Error(c, 404, 40470, "error.reseller_application_required")
		return
	}
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.Order{}).Where("reseller_id = ?", profile.ID)
	var total int64
	var orders []model.Order
	if err := query.Count(&total).Error; err != nil || query.Select("id", "order_no", "status", "payment_status", "subtotal", "discount", "total", "currency", "reseller_id", "reseller_margin", "created_at").Order("created_at DESC").Offset((page-1)*pageSize).Limit(pageSize).Find(&orders).Error != nil {
		response.Error(c, 500, 50076, "error.distribution_order_list_fetch_failed")
		return
	}
	items := make([]gin.H, 0, len(orders))
	for _, order := range orders {
		items = append(items, gin.H{"id": order.ID, "order_no": order.OrderNo, "status": order.Status, "payment_status": order.PaymentStatus, "total": order.Total, "currency": order.Currency, "margin": order.ResellerMargin, "created_at": order.CreatedAt})
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) AdminResellerProfiles(c *gin.Context) {
	currencyCode, err := service.StoreCurrency(h.DB)
	if err != nil {
		response.Error(c, 500, 50078, "error.store_currency_fetch_failed")
		return
	}
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.ResellerProfile{})
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		query = query.Where("status = ?", status)
	}
	var total int64
	var profiles []model.ResellerProfile
	if err := query.Count(&total).Error; err != nil || query.Order("created_at DESC").Offset((page-1)*pageSize).Limit(pageSize).Find(&profiles).Error != nil {
		response.Error(c, 500, 50078, "error.reseller_profile_list_fetch_failed")
		return
	}

	resellerIDs := make([]uuid.UUID, 0, len(profiles))
	levels := make([]int, 0, len(profiles))
	for _, profile := range profiles {
		resellerIDs = append(resellerIDs, profile.ID)
		levels = append(levels, profile.WholesaleLevel)
	}
	var wallets []model.WalletAccount
	var creditPolicies []model.ResellerCreditPolicy
	var tiers []model.ResellerWholesaleTier
	if len(resellerIDs) > 0 {
		if err := h.DB.Where("owner_type = ? AND currency = ? AND owner_id IN ?", "reseller", currencyCode, resellerIDs).Find(&wallets).Error; err != nil {
			response.Error(c, 500, 50078, "error.reseller_profile_list_fetch_failed")
			return
		}
		if err := h.DB.Where("currency = ? AND reseller_id IN ?", currencyCode, resellerIDs).Find(&creditPolicies).Error; err != nil {
			response.Error(c, 500, 50078, "error.reseller_profile_list_fetch_failed")
			return
		}
	}
	if len(levels) > 0 {
		if err := h.DB.Where("level IN ?", levels).Find(&tiers).Error; err != nil {
			response.Error(c, 500, 50078, "error.reseller_profile_list_fetch_failed")
			return
		}
	}
	walletByOwner := make(map[uuid.UUID]model.WalletAccount, len(wallets))
	creditLimitByOwner := make(map[uuid.UUID]int64, len(creditPolicies))
	for _, wallet := range wallets {
		walletByOwner[wallet.OwnerID] = wallet
	}
	for _, policy := range creditPolicies {
		creditLimitByOwner[policy.ResellerID] = policy.CreditLimit
	}
	tierByLevel := make(map[int]model.ResellerWholesaleTier, len(tiers))
	for _, tier := range tiers {
		tierByLevel[tier.Level] = tier
	}

	items := make([]gin.H, 0, len(profiles))
	for _, profile := range profiles {
		wallet := walletByOwner[profile.ID]
		creditLimit := creditLimitByOwner[profile.ID]
		credit, err := service.CalculateResellerCreditState(wallet.Balance, wallet.Frozen, creditLimit)
		if err != nil {
			response.Error(c, 500, 50078, "error.reseller_profile_credit_state_invalid")
			return
		}
		tier, configured := tierByLevel[profile.WholesaleLevel]
		items = append(items, gin.H{
			"id": profile.ID, "user_id": profile.UserID, "name": profile.Name, "code": profile.Code,
			"status": profile.Status, "credit_limit": creditLimit, "wholesale_level": profile.WholesaleLevel,
			"applied_at": profile.AppliedAt, "verified_at": profile.VerifiedAt, "rejected_at": profile.RejectedAt,
			"created_at": profile.CreatedAt, "updated_at": profile.UpdatedAt,
			"wallet_balance": wallet.Balance, "wallet_frozen": wallet.Frozen,
			"currency":        currencyCode,
			"credit_exposure": credit.Exposure, "credit_remaining": credit.Remaining, "credit_breached": credit.Breached,
			"wholesale_tier_name": tier.Name, "wholesale_discount_basis_point": tier.DiscountBasisPoint,
			"wholesale_configured": configured && tier.Enabled,
		})
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) AdminResellerDomains(c *gin.Context) {
	page, pageSize := pagination(c)
	listResource[model.ResellerDomain](c, h.DB, page, pageSize)
}

type resellerProfileUpdateRequest struct {
	Status         string `json:"status"`
	CreditLimit    *int64 `json:"credit_limit"`
	WholesaleLevel *int   `json:"wholesale_level"`
}

var (
	errResellerTransitionInvalid = errors.New("invalid reseller transition")
	errResellerProfileUnchanged  = errors.New("reseller profile is unchanged")
	errResellerCreditExposure    = errors.New("reseller credit exposure exceeds requested limit")
	errResellerWholesalePolicy   = errors.New("reseller wholesale policy is unavailable")
)

func (h Handler) UpdateResellerProfile(c *gin.Context) {
	profileID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42289, "error.distributor_account_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "审核分销账户")
	if !ok {
		return
	}
	var req resellerProfileUpdateRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42290, "error.distributor_review_fields_invalid")
		return
	}
	currencyCode, err := service.StoreCurrency(h.DB)
	if err != nil {
		response.Error(c, 500, 50078, "error.store_currency_fetch_failed")
		return
	}
	req.Status = strings.ToLower(strings.TrimSpace(req.Status))
	if req.CreditLimit != nil && (*req.CreditLimit < 0 || *req.CreditLimit > 1_000_000_000) || req.WholesaleLevel != nil && (*req.WholesaleLevel < 0 || *req.WholesaleLevel > 10) {
		response.Error(c, 422, 42290, "error.credit_limit_out_of_range")
		return
	}
	transitions := map[string]map[string]bool{"pending": {"active": true, "rejected": true}, "active": {"suspended": true}, "suspended": {"active": true, "rejected": true}, "rejected": {"pending": true}}
	var profile model.ResellerProfile
	var previous model.ResellerProfile
	var credit service.ResellerCreditState
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&profile, "id = ?", profileID).Error; err != nil {
			return err
		}
		previous = profile
		creditPolicy, err := service.LockResellerCreditPolicy(tx, profile.ID, currencyCode)
		if err != nil {
			return err
		}
		previous.CreditLimit = creditPolicy.CreditLimit
		targetStatus := req.Status
		if targetStatus == "" {
			targetStatus = profile.Status
		}
		if targetStatus != profile.Status && !transitions[profile.Status][targetStatus] {
			return errResellerTransitionInvalid
		}
		candidateLimit := creditPolicy.CreditLimit
		if req.CreditLimit != nil {
			candidateLimit = *req.CreditLimit
		}
		candidateLevel := profile.WholesaleLevel
		if req.WholesaleLevel != nil {
			candidateLevel = *req.WholesaleLevel
		}

		walletLookup := model.WalletAccount{OwnerType: "reseller", OwnerID: profile.ID, Currency: currencyCode}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "owner_type"}, {Name: "owner_id"}, {Name: "currency"}}, DoNothing: true}).Create(&walletLookup).Error; err != nil {
			return err
		}
		var wallet model.WalletAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("owner_type = ? AND owner_id = ? AND currency = ?", "reseller", profile.ID, currencyCode).First(&wallet).Error; err != nil {
			return err
		}
		var stateErr error
		credit, stateErr = service.CalculateResellerCreditState(wallet.Balance, wallet.Frozen, candidateLimit)
		if stateErr != nil {
			return stateErr
		}
		if req.CreditLimit != nil && candidateLimit < creditPolicy.CreditLimit && credit.Exposure > candidateLimit {
			return errResellerCreditExposure
		}
		if targetStatus == "active" && credit.Breached {
			return errResellerCreditExposure
		}
		if targetStatus == "active" || req.WholesaleLevel != nil && candidateLevel != profile.WholesaleLevel {
			var tier model.ResellerWholesaleTier
			if err := tx.Where("level = ? AND enabled = ?", candidateLevel, true).First(&tier).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return errResellerWholesalePolicy
				}
				return err
			}
		}
		updates := map[string]any{}
		creditChanged := false
		if targetStatus != profile.Status {
			updates["status"] = targetStatus
		}
		if req.CreditLimit != nil && *req.CreditLimit != creditPolicy.CreditLimit {
			if err := tx.Model(&creditPolicy).Update("credit_limit", *req.CreditLimit).Error; err != nil {
				return err
			}
			creditChanged = true
		}
		if req.WholesaleLevel != nil && *req.WholesaleLevel != profile.WholesaleLevel {
			updates["wholesale_level"] = *req.WholesaleLevel
		}
		if len(updates) == 0 && !creditChanged {
			return errResellerProfileUnchanged
		}
		now := time.Now()
		if targetStatus != profile.Status && targetStatus == "active" {
			updates["verified_at"] = &now
			updates["rejected_at"] = nil
		} else if targetStatus != profile.Status && targetStatus == "rejected" {
			updates["rejected_at"] = &now
		}
		if len(updates) > 0 {
			if err := tx.Model(&profile).Updates(updates).Error; err != nil {
				return err
			}
		}
		profile.Status = targetStatus
		if req.CreditLimit != nil {
			profile.CreditLimit = candidateLimit
		}
		if req.WholesaleLevel != nil {
			profile.WholesaleLevel = *req.WholesaleLevel
		}
		site := model.ResellerSite{ResellerID: profile.ID, SiteName: profile.Name, Theme: `{"mode":"system","density":"comfortable"}`, SEO: `{}`, Support: `{}`}
		return tx.Where("reseller_id = ?", profile.ID).FirstOrCreate(&site).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40473, "error.reseller_account_not_found")
		return
	}
	if errors.Is(err, errResellerCreditExposure) {
		response.Error(c, 409, 40977, "error.reseller_credit_exposure_exceeds_limit")
		return
	}
	if errors.Is(err, errResellerWholesalePolicy) {
		response.Error(c, 409, 40978, "error.reseller_wholesale_policy_unavailable")
		return
	}
	if errors.Is(err, errResellerProfileUnchanged) {
		response.Error(c, 409, 40979, "error.reseller_profile_unchanged")
		return
	}
	if err != nil {
		response.Error(c, 409, 40976, "error.reseller_status_transition_not_allowed")
		return
	}
	h.audit(c, "reseller.profile.update", "reseller_profile", profileID.String(), fmt.Sprintf("%s；status=%s->%s；credit_limit=%d->%d；wholesale_level=%d->%d；credit_exposure=%d", reason, previous.Status, profile.Status, previous.CreditLimit, profile.CreditLimit, previous.WholesaleLevel, profile.WholesaleLevel, credit.Exposure))
	response.OK(c, gin.H{"profile": profile})
}

type resellerDomainAdminRequest struct {
	Status    string `json:"status"`
	TLSStatus string `json:"tls_status"`
}

func (h Handler) UpdateResellerDomain(c *gin.Context) {
	domainID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42285, "error.domain_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "更新分销域名")
	if !ok {
		return
	}
	var req resellerDomainAdminRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42291, "error.domain_tls_status_invalid")
		return
	}
	req.Status, req.TLSStatus = strings.ToLower(strings.TrimSpace(req.Status)), strings.ToLower(strings.TrimSpace(req.TLSStatus))
	allowedStatus := map[string]bool{"verified": true, "active": true, "suspended": true, "rejected": true}
	allowedTLS := map[string]bool{"pending": true, "provisioning": true, "active": true, "failed": true, "disabled": true}
	if !allowedStatus[req.Status] || !allowedTLS[req.TLSStatus] || (req.Status == "active" && req.TLSStatus != "active") {
		response.Error(c, 422, 42291, "error.tls_verification_required")
		return
	}
	var item model.ResellerDomain
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", domainID).Error; err != nil {
			return err
		}
		if item.VerifiedAt == nil && req.Status != "rejected" {
			return errors.New("domain ownership is not verified")
		}
		return tx.Model(&item).Updates(map[string]any{"status": req.Status, "tls_status": req.TLSStatus}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40471, "error.domain_not_found")
		return
	}
	if err != nil {
		response.Error(c, 409, 40977, "error.domain_not_verified_or_status_not_allowed")
		return
	}
	h.audit(c, "reseller.domain.update", "reseller_domain", domainID.String(), reason+"；status="+req.Status+"；tls="+req.TLSStatus)
	response.OK(c, gin.H{"id": domainID, "status": req.Status, "tls_status": req.TLSStatus})
}
