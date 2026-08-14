package handler

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/service"
	"linlinqi/api/pkg/response"
)

var marketingCodePattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_-]{2,79}$`)

type promotionRuleRequest struct {
	BasisPoints int   `json:"basis_points"`
	Amount      int64 `json:"amount"`
	MinAmount   int64 `json:"min_amount"`
	MaxDiscount int64 `json:"max_discount"`
	UnitPrice   int64 `json:"unit_price"`
}

type promotionRequest struct {
	Name       string               `json:"name"`
	Code       string               `json:"code"`
	Type       string               `json:"type"`
	Rules      promotionRuleRequest `json:"rules"`
	Priority   int                  `json:"priority"`
	Stackable  bool                 `json:"stackable"`
	StartsAt   time.Time            `json:"starts_at"`
	EndsAt     time.Time            `json:"ends_at"`
	Status     string               `json:"status"`
	ProductIDs []uuid.UUID          `json:"product_ids"`
}

type adminPromotionItem struct {
	model.Promotion
	ProductIDs []uuid.UUID `json:"product_ids"`
}

func requireAdminChangeReason(c *gin.Context, action string) (string, bool) {
	reason := strings.TrimSpace(c.GetHeader("X-Change-Reason"))
	if len([]rune(reason)) < 4 || len([]rune(reason)) > 500 || strings.IndexFunc(reason, unicode.IsControl) >= 0 {
		response.Error(c, 422, 42257, "error.change_reason_for_action", map[string]interface{}{"Action": action})
		return "", false
	}
	return reason, true
}

func (r *promotionRequest) normalizeAndValidate(now time.Time) error {
	r.Name = strings.TrimSpace(r.Name)
	r.Code = strings.ToUpper(strings.TrimSpace(r.Code))
	r.Type = strings.ToLower(strings.TrimSpace(r.Type))
	r.Status = strings.ToLower(strings.TrimSpace(r.Status))
	if len([]rune(r.Name)) < 2 || len([]rune(r.Name)) > 160 || !marketingCodePattern.MatchString(r.Code) {
		return errors.New("invalid promotion identity")
	}
	if r.Priority < -100_000 || r.Priority > 100_000 || len(r.ProductIDs) < 1 || len(r.ProductIDs) > 200 {
		return errors.New("invalid promotion scope")
	}
	if r.StartsAt.IsZero() || r.EndsAt.IsZero() || !r.EndsAt.After(r.StartsAt) || r.EndsAt.After(now.AddDate(10, 0, 0)) {
		return errors.New("invalid promotion period")
	}
	r.StartsAt, r.EndsAt = r.StartsAt.UTC(), r.EndsAt.UTC()
	if r.Status != "draft" && r.Status != "active" && r.Status != "paused" && r.Status != "archived" {
		return errors.New("invalid promotion status")
	}
	if r.Rules.MinAmount < 0 || r.Rules.MaxDiscount < 0 {
		return errors.New("invalid promotion rules")
	}
	switch r.Type {
	case "percentage":
		if r.Rules.BasisPoints < 1 || r.Rules.BasisPoints > 10000 {
			return errors.New("invalid percentage")
		}
	case "fixed", "threshold_fixed":
		if r.Rules.Amount < 1 || r.Rules.Amount > 100_000_000 {
			return errors.New("invalid fixed amount")
		}
	case "flash_price":
		if r.Rules.UnitPrice < 0 || r.Rules.UnitPrice > 100_000_000 {
			return errors.New("invalid flash price")
		}
	default:
		return errors.New("invalid promotion type")
	}
	seen := make(map[uuid.UUID]struct{}, len(r.ProductIDs))
	for _, productID := range r.ProductIDs {
		if productID == uuid.Nil {
			return errors.New("invalid product")
		}
		seen[productID] = struct{}{}
	}
	if len(seen) != len(r.ProductIDs) {
		return errors.New("duplicate product")
	}
	return nil
}

func (h Handler) AdminPromotions(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.Promotion{})
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		query = query.Where("status = ?", status)
	}
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("name ILIKE ? OR code ILIKE ?", like, like)
	}
	var total int64
	var promotions []model.Promotion
	if err := query.Count(&total).Error; err != nil || query.Order("priority DESC, created_at DESC").Offset((page-1)*pageSize).Limit(pageSize).Find(&promotions).Error != nil {
		response.Error(c, 500, 50066, "error.campaign_fetch_failed")
		return
	}
	items := make([]adminPromotionItem, 0, len(promotions))
	if len(promotions) > 0 {
		ids := make([]uuid.UUID, 0, len(promotions))
		for _, promotion := range promotions {
			ids = append(ids, promotion.ID)
		}
		var links []model.PromotionProduct
		if err := h.DB.Where("promotion_id IN ?", ids).Find(&links).Error; err != nil {
			response.Error(c, 500, 50066, "error.campaign_product_scope_fetch_failed")
			return
		}
		productIDs := make(map[uuid.UUID][]uuid.UUID, len(promotions))
		for _, link := range links {
			productIDs[link.PromotionID] = append(productIDs[link.PromotionID], link.ProductID)
		}
		for _, promotion := range promotions {
			items = append(items, adminPromotionItem{Promotion: promotion, ProductIDs: productIDs[promotion.ID]})
		}
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) CreatePromotion(c *gin.Context) {
	reason, ok := requireAdminChangeReason(c, "创建活动")
	if !ok {
		return
	}
	var req promotionRequest
	if decodeStrictJSON(c, &req) != nil || req.normalizeAndValidate(time.Now()) != nil {
		response.Error(c, 422, 42267, "error.campaign_fields_invalid")
		return
	}
	item, err := h.savePromotion(uuid.Nil, req)
	if err != nil {
		response.Error(c, 409, 40963, "error.campaign_code_duplicate_or_save_failed")
		return
	}
	h.audit(c, "promotion.create", "promotion", item.ID.String(), reason)
	response.Created(c, item)
}

func (h Handler) UpdatePromotion(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42268, "error.campaign_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "修改活动")
	if !ok {
		return
	}
	var req promotionRequest
	if decodeStrictJSON(c, &req) != nil || req.normalizeAndValidate(time.Now()) != nil {
		response.Error(c, 422, 42267, "error.campaign_fields_invalid")
		return
	}
	item, err := h.savePromotion(id, req)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40462, "error.marketing_campaign_not_found")
		return
	}
	if err != nil {
		response.Error(c, 409, 40963, "error.campaign_code_duplicate_or_save_failed")
		return
	}
	h.audit(c, "promotion.update", "promotion", item.ID.String(), reason)
	response.OK(c, item)
}

func (h Handler) savePromotion(id uuid.UUID, req promotionRequest) (model.Promotion, error) {
	rules, err := json.Marshal(req.Rules)
	if err != nil {
		return model.Promotion{}, err
	}
	item := model.Promotion{
		Name: req.Name, Code: req.Code, Type: req.Type, Rules: string(rules), Priority: req.Priority,
		Stackable: req.Stackable, StartsAt: req.StartsAt, EndsAt: req.EndsAt, Status: req.Status,
	}
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&model.Product{}).Where("id IN ?", req.ProductIDs).Count(&count).Error; err != nil || count != int64(len(req.ProductIDs)) {
			if err != nil {
				return err
			}
			return gorm.ErrRecordNotFound
		}
		var productCurrencies []string
		if err := tx.Model(&model.Product{}).Where("id IN ?", req.ProductIDs).Distinct("currency").Pluck("currency", &productCurrencies).Error; err != nil {
			return err
		}
		if len(productCurrencies) != 1 {
			return service.ErrCurrencyMismatch
		}
		item.Currency = productCurrencies[0]
		if id == uuid.Nil {
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
		} else {
			var locked model.Promotion
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, "id = ?", id).Error; err != nil {
				return err
			}
			item.Base = locked.Base
			if err := tx.Model(&locked).Updates(map[string]any{
				"name": item.Name, "code": item.Code, "type": item.Type, "currency": item.Currency, "rules": item.Rules,
				"priority": item.Priority, "stackable": item.Stackable, "starts_at": item.StartsAt,
				"ends_at": item.EndsAt, "status": item.Status,
			}).Error; err != nil {
				return err
			}
			if err := tx.Where("promotion_id = ?", id).Delete(&model.PromotionProduct{}).Error; err != nil {
				return err
			}
		}
		links := make([]model.PromotionProduct, 0, len(req.ProductIDs))
		for _, productID := range req.ProductIDs {
			links = append(links, model.PromotionProduct{PromotionID: item.ID, ProductID: productID})
		}
		return tx.Create(&links).Error
	})
	return item, err
}

type couponRequest struct {
	Code       string    `json:"code"`
	Type       string    `json:"type"`
	Currency   string    `json:"currency"`
	Value      int64     `json:"value"`
	MinAmount  int64     `json:"min_amount"`
	UsageLimit int       `json:"usage_limit"`
	StartsAt   time.Time `json:"starts_at"`
	EndsAt     time.Time `json:"ends_at"`
	Enabled    bool      `json:"enabled"`
}

func (r *couponRequest) normalizeAndValidate(now time.Time) error {
	r.Code = strings.ToUpper(strings.TrimSpace(r.Code))
	r.Type = strings.ToLower(strings.TrimSpace(r.Type))
	r.Currency = strings.ToUpper(strings.TrimSpace(r.Currency))
	if !marketingCodePattern.MatchString(r.Code) || (r.Currency != "" && len(r.Currency) != 3) || r.MinAmount < 0 || r.UsageLimit < 0 {
		return errors.New("invalid coupon")
	}
	if r.StartsAt.IsZero() || r.EndsAt.IsZero() || !r.EndsAt.After(r.StartsAt) || r.EndsAt.After(now.AddDate(10, 0, 0)) {
		return errors.New("invalid coupon period")
	}
	r.StartsAt, r.EndsAt = r.StartsAt.UTC(), r.EndsAt.UTC()
	switch r.Type {
	case "fixed":
		if r.Value < 1 || r.Value > 100_000_000 {
			return errors.New("invalid fixed coupon value")
		}
	case "percentage":
		if r.Value < 1 || r.Value > 10000 {
			return errors.New("invalid percentage coupon value")
		}
	default:
		return errors.New("invalid coupon type")
	}
	return nil
}

func (h Handler) AdminCoupons(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.Coupon{})
	if enabled := strings.TrimSpace(c.Query("enabled")); enabled == "true" || enabled == "false" {
		query = query.Where("enabled = ?", enabled == "true")
	}
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		query = query.Where("code ILIKE ?", "%"+keyword+"%")
	}
	var total int64
	var items []model.Coupon
	if err := query.Count(&total).Error; err != nil || query.Order("created_at DESC").Offset((page-1)*pageSize).Limit(pageSize).Find(&items).Error != nil {
		response.Error(c, 500, 50067, "error.coupon_fetch_failed")
		return
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) CreateCoupon(c *gin.Context) {
	reason, ok := requireAdminChangeReason(c, "创建优惠券")
	if !ok {
		return
	}
	var req couponRequest
	if decodeStrictJSON(c, &req) != nil || req.normalizeAndValidate(time.Now()) != nil {
		response.Error(c, 422, 42269, "error.discount_details_invalid")
		return
	}
	if req.Currency == "" {
		var err error
		req.Currency, err = service.StoreCurrency(h.DB)
		if err != nil {
			response.Error(c, 500, 50067, "error.store_currency_fetch_failed")
			return
		}
	}
	var currencyDefinition model.CurrencyDefinition
	if h.DB.Where("code = ? AND enabled = ?", req.Currency, true).First(&currencyDefinition).Error != nil {
		response.Error(c, 422, 42269, "error.currency_not_supported")
		return
	}
	item := model.Coupon{Code: req.Code, Type: req.Type, Currency: req.Currency, Value: req.Value, MinAmount: req.MinAmount, UsageLimit: req.UsageLimit, StartsAt: req.StartsAt, EndsAt: req.EndsAt, Enabled: req.Enabled}
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		return createWithExplicitColumns(tx, &item, map[string]any{"enabled": req.Enabled})
	}); err != nil {
		response.Error(c, 409, 40964, "error.coupon_duplicate_or_save_failed")
		return
	}
	item.Enabled = req.Enabled
	h.audit(c, "coupon.create", "coupon", item.ID.String(), reason)
	response.Created(c, item)
}

func (h Handler) UpdateCoupon(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42270, "error.coupon_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "修改优惠券")
	if !ok {
		return
	}
	var req couponRequest
	if decodeStrictJSON(c, &req) != nil || req.normalizeAndValidate(time.Now()) != nil {
		response.Error(c, 422, 42269, "error.discount_details_invalid")
		return
	}
	if req.Currency == "" {
		var err error
		req.Currency, err = service.StoreCurrency(h.DB)
		if err != nil {
			response.Error(c, 500, 50067, "error.store_currency_fetch_failed")
			return
		}
	}
	var currencyDefinition model.CurrencyDefinition
	if h.DB.Where("code = ? AND enabled = ?", req.Currency, true).First(&currencyDefinition).Error != nil {
		response.Error(c, 422, 42269, "error.currency_not_supported")
		return
	}
	var item model.Coupon
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", id).Error; err != nil {
			return err
		}
		if req.UsageLimit > 0 && req.UsageLimit < item.UsedCount {
			return errors.New("usage limit below used count")
		}
		return tx.Model(&item).Updates(map[string]any{
			"code": req.Code, "type": req.Type, "currency": req.Currency, "value": req.Value, "min_amount": req.MinAmount,
			"usage_limit": req.UsageLimit, "starts_at": req.StartsAt, "ends_at": req.EndsAt, "enabled": req.Enabled,
		}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40463, "error.coupon_not_found")
		return
	}
	if err != nil {
		response.Error(c, 409, 40965, "error.coupon_usage_limit_invalid")
		return
	}
	item.Code, item.Type, item.Currency, item.Value, item.MinAmount, item.UsageLimit, item.StartsAt, item.EndsAt, item.Enabled = req.Code, req.Type, req.Currency, req.Value, req.MinAmount, req.UsageLimit, req.StartsAt, req.EndsAt, req.Enabled
	h.audit(c, "coupon.update", "coupon", item.ID.String(), reason)
	response.OK(c, item)
}
