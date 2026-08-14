package handler

import (
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/i18n"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/service"
	"linlinqi/api/pkg/response"
)

type affiliateApplicationRequest struct {
	AcceptedTerms bool `json:"accepted_terms"`
}

func (h Handler) ApplyAffiliate(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req affiliateApplicationRequest
	if decodeStrictJSON(c, &req) != nil || !req.AcceptedTerms {
		response.Error(c, 422, 42271, "error.affiliate_terms_confirmation_required")
		return
	}
	rate := 500
	var setting model.Setting
	if h.DB.Select("value").First(&setting, "key = ?", "affiliate_default_basis_points").Error == nil {
		if parsed, err := strconv.Atoi(setting.Value); err == nil && parsed >= 1 && parsed <= 3000 {
			rate = parsed
		}
	}
	now := time.Now()
	profile := model.AffiliateProfile{
		UserID: userID, ReferralCode: "LQA" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:12]),
		CommissionBasisPoint: rate, Status: "pending", AppliedAt: now,
	}
	if err := h.DB.Create(&profile).Error; err != nil {
		var existing model.AffiliateProfile
		if h.DB.Where("user_id = ?", userID).First(&existing).Error == nil {
			response.Error(c, 409, 40966, "error.affiliate_plan_application_already_submitted")
			return
		}
		response.Error(c, 500, 50068, "error.affiliate_plan_apply_failed")
		return
	}
	response.Created(c, gin.H{"profile": profile, "notice": i18n.Localize(c, "notice.affiliate_pending")})
}

type affiliateWithdrawalRequest struct {
	Amount   int64  `json:"amount"`
	Method   string `json:"method"`
	Account  string `json:"account"`
	Currency string `json:"currency"`
}

func previewPayoutAccount(value string) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= 4 {
		return "••••"
	}
	return "••••" + string(runes[len(runes)-4:])
}

func (h Handler) RequestAffiliateWithdrawal(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req affiliateWithdrawalRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42272, "error.two_factor_status_read_failed")
		return
	}
	req.Method = strings.ToLower(strings.TrimSpace(req.Method))
	req.Account = strings.TrimSpace(req.Account)
	req.Currency = strings.ToUpper(strings.TrimSpace(req.Currency))
	if (req.Method != "alipay" && req.Method != "bank" && req.Method != "usdt") || utf8.RuneCountInString(req.Account) < 3 || utf8.RuneCountInString(req.Account) > 255 {
		response.Error(c, 422, 42272, "error.withdrawal_method_unsupported")
		return
	}
	storeCurrency, err := service.StoreCurrency(h.DB)
	if err != nil {
		response.Error(c, 500, 50069, "error.store_currency_fetch_failed")
		return
	}
	if req.Currency == "" {
		req.Currency = storeCurrency
	}
	var currencyDefinition model.CurrencyDefinition
	if err := h.DB.Where("code = ? AND enabled = ?", req.Currency, true).First(&currencyDefinition).Error; err != nil {
		response.Error(c, 422, 42273, "error.currency_not_supported")
		return
	}
	scale := int64(1)
	for index := 0; index < currencyDefinition.MinorUnit; index++ {
		scale *= 10
	}
	minimum := int64(100) * scale
	maximum := int64(1_000_000) * scale
	var setting model.Setting
	if req.Currency == storeCurrency && h.DB.Select("value").First(&setting, "key = ?", "affiliate_withdrawal_minimum").Error == nil {
		if parsed, err := strconv.ParseInt(setting.Value, 10, 64); err == nil && parsed >= scale && parsed <= maximum {
			minimum = parsed
		}
	}
	if req.Amount < minimum || req.Amount > maximum {
		response.Error(c, 422, 42273, "error.withdrawal_amount_out_of_range", map[string]interface{}{"MinMinor": minimum, "MaxMinor": maximum, "Currency": req.Currency})
		return
	}
	withdrawal := model.AffiliateWithdrawal{
		Base: model.Base{ID: uuid.New()}, WithdrawalNo: "LQAW" + time.Now().UTC().Format("20060102150405") + strings.ToUpper(uuid.NewString()[:8]),
		Amount: req.Amount, Currency: req.Currency, Method: req.Method, AccountPreview: previewPayoutAccount(req.Account), Status: "pending",
	}
	ciphertext, nonce, _, err := h.Vault.Encrypt(req.Account, withdrawal.ID[:])
	req.Account = ""
	if err != nil {
		response.Error(c, 500, 50069, "error.payout_account_protect_failed")
		return
	}
	withdrawal.AccountCipher, withdrawal.AccountNonce = ciphertext, nonce
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var profile model.AffiliateProfile
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&profile).Error; err != nil {
			return err
		}
		if profile.Status != "active" {
			return errors.New("affiliate balance unavailable")
		}
		withdrawal.AffiliateID = profile.ID
		balance, err := service.LockAffiliateBalance(tx, profile.ID, withdrawal.Currency)
		if err != nil || balance.AvailableCommission < withdrawal.Amount {
			return errors.New("affiliate balance unavailable")
		}
		if err := tx.Model(&balance).Updates(map[string]any{
			"available_commission": gorm.Expr("available_commission - ?", withdrawal.Amount),
			"frozen_commission":    gorm.Expr("frozen_commission + ?", withdrawal.Amount),
		}).Error; err != nil {
			return err
		}
		return tx.Create(&withdrawal).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40464, "error.promotion_plan_not_enabled")
		return
	}
	if err != nil {
		response.Error(c, 409, 40967, "error.affiliate_account_not_enabled_or_commission_insufficient")
		return
	}
	response.Created(c, gin.H{"withdrawal": withdrawal, "notice": i18n.Localize(c, "notice.withdrawal_encrypted")})
}

type affiliateProfileUpdateRequest struct {
	Status               string `json:"status"`
	CommissionBasisPoint *int   `json:"commission_basis_point"`
}

func (h Handler) UpdateAffiliateProfile(c *gin.Context) {
	profileID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42274, "error.affiliate_account_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "审核推广账户")
	if !ok {
		return
	}
	var req affiliateProfileUpdateRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42275, "error.affiliate_status_or_rate_invalid")
		return
	}
	req.Status = strings.ToLower(strings.TrimSpace(req.Status))
	if req.CommissionBasisPoint != nil && (*req.CommissionBasisPoint < 1 || *req.CommissionBasisPoint > 3000) {
		response.Error(c, 422, 42275, "error.commission_rate_range")
		return
	}
	transitions := map[string]map[string]bool{
		"pending":   {"active": true, "rejected": true},
		"active":    {"suspended": true},
		"suspended": {"active": true, "rejected": true},
		"rejected":  {"pending": true},
	}
	var profile model.AffiliateProfile
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&profile, "id = ?", profileID).Error; err != nil {
			return err
		}
		if !transitions[profile.Status][req.Status] {
			return errors.New("invalid affiliate transition")
		}
		updates := map[string]any{"status": req.Status}
		if req.CommissionBasisPoint != nil {
			updates["commission_basis_point"] = *req.CommissionBasisPoint
		}
		now := time.Now()
		if req.Status == "active" {
			updates["approved_at"] = &now
			updates["rejected_at"] = nil
		} else if req.Status == "rejected" {
			updates["rejected_at"] = &now
		}
		if err := tx.Model(&profile).Updates(updates).Error; err != nil {
			return err
		}
		profile.Status = req.Status
		if req.CommissionBasisPoint != nil {
			profile.CommissionBasisPoint = *req.CommissionBasisPoint
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40465, "error.promotion_account_not_found")
		return
	}
	if err != nil {
		response.Error(c, 409, 40968, "error.affiliate_status_transition_not_allowed")
		return
	}
	h.audit(c, "affiliate.profile.update", "affiliate_profile", profileID.String(), reason+"；status="+req.Status)
	response.OK(c, gin.H{"profile": profile})
}

type adminAffiliateWithdrawalItem struct {
	model.AffiliateWithdrawal
	ReferralCode string `json:"referral_code"`
	UserEmail    string `json:"user_email"`
}

type adminAffiliateProfileItem struct {
	model.AffiliateProfile
	Currency            string `json:"currency"`
	TotalCommission     int64  `json:"total_commission"`
	AvailableCommission int64  `json:"available_commission"`
	FrozenCommission    int64  `json:"frozen_commission"`
}

// AdminAffiliateProfiles exposes the current store-currency ledger without
// ever aggregating integer amounts from different currencies.
func (h Handler) AdminAffiliateProfiles(c *gin.Context) {
	currencyCode, err := service.StoreCurrency(h.DB)
	if err != nil {
		response.Error(c, 500, 50070, "error.store_currency_fetch_failed")
		return
	}
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.AffiliateProfile{})
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		query = query.Where("status = ?", status)
	}
	var total int64
	var profiles []model.AffiliateProfile
	if err := query.Count(&total).Error; err != nil || query.Order("created_at DESC").Offset((page-1)*pageSize).Limit(pageSize).Find(&profiles).Error != nil {
		response.Error(c, 500, 50070, "error.affiliate_account_fetch_failed")
		return
	}
	ids := make([]uuid.UUID, 0, len(profiles))
	for _, profile := range profiles {
		ids = append(ids, profile.ID)
	}
	balances := make(map[uuid.UUID]model.AffiliateBalance, len(ids))
	if len(ids) > 0 {
		var records []model.AffiliateBalance
		if err := h.DB.Where("affiliate_id IN ? AND currency = ?", ids, currencyCode).Find(&records).Error; err != nil {
			response.Error(c, 500, 50070, "error.affiliate_balance_fetch_failed")
			return
		}
		for _, record := range records {
			balances[record.AffiliateID] = record
		}
	}
	items := make([]adminAffiliateProfileItem, 0, len(profiles))
	for _, profile := range profiles {
		balance := balances[profile.ID]
		items = append(items, adminAffiliateProfileItem{
			AffiliateProfile: profile, Currency: currencyCode,
			TotalCommission: balance.TotalCommission, AvailableCommission: balance.AvailableCommission,
			FrozenCommission: balance.FrozenCommission,
		})
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) AdminAffiliateWithdrawals(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Table("affiliate_withdrawals aw").
		Select("aw.*, ap.referral_code, u.email AS user_email").
		Joins("JOIN affiliate_profiles ap ON ap.id = aw.affiliate_id AND ap.deleted_at IS NULL").
		Joins("JOIN users u ON u.id = ap.user_id AND u.deleted_at IS NULL").
		Where("aw.deleted_at IS NULL")
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		query = query.Where("aw.status = ?", status)
	}
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("aw.withdrawal_no ILIKE ? OR ap.referral_code ILIKE ? OR u.email ILIKE ?", like, like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50070, "error.affiliate_withdrawal_fetch_failed")
		return
	}
	var items []adminAffiliateWithdrawalItem
	if err := query.Order("aw.created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&items).Error; err != nil {
		response.Error(c, 500, 50070, "error.affiliate_withdrawal_fetch_failed")
		return
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) AffiliateWithdrawalDetail(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42276, "error.withdrawal_id_invalid")
		return
	}
	var withdrawal model.AffiliateWithdrawal
	if err := h.DB.First(&withdrawal, "id = ?", id).Error; err != nil {
		response.Error(c, 404, 40466, "error.withdrawal_request_not_found")
		return
	}
	account := withdrawal.Account
	if len(withdrawal.AccountCipher) > 0 && len(withdrawal.AccountNonce) > 0 {
		account, err = h.Vault.Decrypt(withdrawal.AccountCipher, withdrawal.AccountNonce, withdrawal.ID[:])
		if err != nil {
			response.Error(c, 500, 50071, "error.payout_account_decrypt_failed")
			return
		}
	}
	h.audit(c, "affiliate.withdrawal.account.view", "affiliate_withdrawal", withdrawal.ID.String(), "查看加密收款账户")
	response.OK(c, gin.H{"withdrawal": withdrawal, "account": account})
}

type affiliateWithdrawalUpdateRequest struct {
	Status          string `json:"status"`
	PayoutReference string `json:"payout_reference"`
}

func (h Handler) UpdateAffiliateWithdrawal(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42276, "error.withdrawal_id_invalid")
		return
	}
	adminID, err := uuid.Parse(c.GetString("subject"))
	if err != nil {
		response.Error(c, 401, 40140, "error.invalid_login_state")
		return
	}
	reason, ok := requireAdminChangeReason(c, "处理提现")
	if !ok {
		return
	}
	var req affiliateWithdrawalUpdateRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42277, "error.withdrawal_status_invalid")
		return
	}
	req.Status = strings.ToLower(strings.TrimSpace(req.Status))
	req.PayoutReference = strings.TrimSpace(req.PayoutReference)
	if (req.Status != "processing" && req.Status != "completed" && req.Status != "rejected") || (req.Status == "completed" && (utf8.RuneCountInString(req.PayoutReference) < 4 || utf8.RuneCountInString(req.PayoutReference) > 160)) {
		response.Error(c, 422, 42277, "error.withdrawal_payment_ref_required")
		return
	}
	var withdrawal model.AffiliateWithdrawal
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&withdrawal, "id = ?", id).Error; err != nil {
			return err
		}
		allowed := (withdrawal.Status == "pending" && (req.Status == "processing" || req.Status == "completed" || req.Status == "rejected")) || (withdrawal.Status == "processing" && (req.Status == "completed" || req.Status == "rejected"))
		if !allowed {
			return errors.New("invalid withdrawal transition")
		}
		balance, err := service.LockAffiliateBalance(tx, withdrawal.AffiliateID, withdrawal.Currency)
		if err != nil {
			return err
		}
		if balance.FrozenCommission < withdrawal.Amount {
			return errors.New("frozen affiliate balance is inconsistent")
		}
		profileUpdates := map[string]any{}
		if req.Status == "completed" {
			if balance.AvailableCommission < 0 {
				return errors.New("affiliate clawback must be resolved before payout")
			}
			profileUpdates["frozen_commission"] = gorm.Expr("frozen_commission - ?", withdrawal.Amount)
		} else if req.Status == "rejected" {
			profileUpdates["frozen_commission"] = gorm.Expr("frozen_commission - ?", withdrawal.Amount)
			profileUpdates["available_commission"] = gorm.Expr("available_commission + ?", withdrawal.Amount)
		}
		if len(profileUpdates) > 0 {
			if err := tx.Model(&balance).Updates(profileUpdates).Error; err != nil {
				return err
			}
		}
		updates := map[string]any{"status": req.Status, "processed_by": &adminID, "reason": reason}
		if req.PayoutReference != "" {
			updates["payout_reference"] = req.PayoutReference
		}
		if req.Status == "completed" || req.Status == "rejected" {
			now := time.Now()
			updates["processed_at"] = &now
		}
		return tx.Model(&withdrawal).Updates(updates).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40466, "error.withdrawal_request_not_found")
		return
	}
	if err != nil {
		response.Error(c, 409, 40969, "error.withdrawal_state_not_allowed")
		return
	}
	h.audit(c, "affiliate.withdrawal.update", "affiliate_withdrawal", id.String(), reason+"；status="+req.Status)
	response.OK(c, gin.H{"id": id, "status": req.Status})
}
