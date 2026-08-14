package handler

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
	"linlinqi/api/pkg/response"
)

type adminResellerWholesaleTierItem struct {
	model.ResellerWholesaleTier
	ActiveResellerCount int64 `json:"active_reseller_count"`
	TotalResellerCount  int64 `json:"total_reseller_count"`
}

func (h Handler) AdminResellerWholesaleTiers(c *gin.Context) {
	var tiers []model.ResellerWholesaleTier
	if err := h.DB.Order("level ASC").Find(&tiers).Error; err != nil {
		response.Error(c, 500, 50079, "error.reseller_wholesale_tier_list_fetch_failed")
		return
	}
	items := make([]adminResellerWholesaleTierItem, 0, len(tiers))
	for _, tier := range tiers {
		item := adminResellerWholesaleTierItem{ResellerWholesaleTier: tier}
		if err := h.DB.Model(&model.ResellerProfile{}).Where("wholesale_level = ?", tier.Level).Count(&item.TotalResellerCount).Error; err != nil {
			response.Error(c, 500, 50079, "error.reseller_wholesale_tier_list_fetch_failed")
			return
		}
		if err := h.DB.Model(&model.ResellerProfile{}).Where("wholesale_level = ? AND status = ?", tier.Level, "active").Count(&item.ActiveResellerCount).Error; err != nil {
			response.Error(c, 500, 50079, "error.reseller_wholesale_tier_list_fetch_failed")
			return
		}
		items = append(items, item)
	}
	response.OK(c, items)
}

type resellerWholesaleTierRequest struct {
	Name               string `json:"name"`
	DiscountBasisPoint *int   `json:"discount_basis_point"`
	Enabled            *bool  `json:"enabled"`
}

var errResellerTierAssignedToActive = errors.New("reseller wholesale tier is assigned to active profiles")

func (h Handler) PutResellerWholesaleTier(c *gin.Context) {
	level, err := strconv.Atoi(strings.TrimSpace(c.Param("level")))
	if err != nil || level < 0 || level > 10 {
		response.Error(c, 422, 42293, "error.reseller_wholesale_tier_level_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "更新经销商批发政策")
	if !ok {
		return
	}
	var req resellerWholesaleTierRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42294, "error.reseller_wholesale_tier_fields_invalid")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if len([]rune(req.Name)) < 2 || len([]rune(req.Name)) > 100 || req.DiscountBasisPoint == nil || *req.DiscountBasisPoint < 0 || *req.DiscountBasisPoint > 10000 || req.Enabled == nil {
		response.Error(c, 422, 42294, "error.reseller_wholesale_tier_fields_invalid")
		return
	}
	if level == 0 && (*req.DiscountBasisPoint != 0 || !*req.Enabled) {
		response.Error(c, 422, 42295, "error.reseller_wholesale_base_tier_immutable_policy")
		return
	}

	var before *model.ResellerWholesaleTier
	var saved model.ResellerWholesaleTier
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var tier model.ResellerWholesaleTier
		findErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("level = ?", level).First(&tier).Error
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		if !*req.Enabled {
			var activeCount int64
			if err := tx.Model(&model.ResellerProfile{}).Where("wholesale_level = ? AND status = ?", level, "active").Count(&activeCount).Error; err != nil {
				return err
			}
			if activeCount > 0 {
				return errResellerTierAssignedToActive
			}
		}
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			saved = model.ResellerWholesaleTier{Level: level, Name: req.Name, DiscountBasisPoint: *req.DiscountBasisPoint, Enabled: *req.Enabled}
			return tx.Create(&saved).Error
		}
		copy := tier
		before = &copy
		if tier.Name == req.Name && tier.DiscountBasisPoint == *req.DiscountBasisPoint && tier.Enabled == *req.Enabled {
			return errResellerProfileUnchanged
		}
		if err := tx.Model(&tier).Updates(map[string]any{"name": req.Name, "discount_basis_point": *req.DiscountBasisPoint, "enabled": *req.Enabled}).Error; err != nil {
			return err
		}
		tier.Name, tier.DiscountBasisPoint, tier.Enabled = req.Name, *req.DiscountBasisPoint, *req.Enabled
		saved = tier
		return nil
	})
	if errors.Is(err, errResellerTierAssignedToActive) {
		response.Error(c, 409, 40980, "error.reseller_wholesale_tier_assigned_to_active_profiles")
		return
	}
	if errors.Is(err, errResellerProfileUnchanged) {
		response.Error(c, 409, 40981, "error.reseller_wholesale_tier_unchanged")
		return
	}
	if err != nil {
		response.Error(c, 409, 40982, "error.reseller_wholesale_tier_save_failed")
		return
	}
	previous := "new"
	if before != nil {
		previous = fmt.Sprintf("name=%s,discount_bp=%d,enabled=%t", before.Name, before.DiscountBasisPoint, before.Enabled)
	}
	h.audit(c, "reseller.wholesale_tier.put", "reseller_wholesale_tier", saved.ID.String(), fmt.Sprintf("%s；level=%d；before=%s；after=name=%s,discount_bp=%d,enabled=%t", reason, level, previous, saved.Name, saved.DiscountBasisPoint, saved.Enabled))
	response.OK(c, gin.H{"tier": saved})
}
