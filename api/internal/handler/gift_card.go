package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/i18n"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/service"
	"linlinqi/api/pkg/response"
)

var (
	errGiftCardUnavailable = errors.New("gift card unavailable")
	errGiftCardBalance     = errors.New("gift card balance invalid")
)

type issueGiftCardBatchRequest struct {
	Name      string     `json:"name"`
	Quantity  int        `json:"quantity"`
	CardValue int64      `json:"card_value"`
	Currency  string     `json:"currency"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type issuedGiftCard struct {
	ID          uuid.UUID  `json:"id"`
	Code        string     `json:"code"`
	CodePreview string     `json:"code_preview"`
	CardValue   int64      `json:"card_value"`
	Currency    string     `json:"currency"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

type adminGiftCardItem struct {
	ID             uuid.UUID  `json:"id"`
	BatchID        *uuid.UUID `json:"batch_id,omitempty"`
	BatchNo        string     `json:"batch_no,omitempty"`
	BatchName      string     `json:"batch_name,omitempty"`
	CodePreview    string     `json:"code_preview"`
	InitialBalance int64      `json:"initial_balance"`
	Balance        int64      `json:"balance"`
	Currency       string     `json:"currency"`
	Status         string     `json:"status"`
	RedeemedBy     *uuid.UUID `json:"redeemed_by,omitempty"`
	RedeemedAt     *time.Time `json:"redeemed_at,omitempty"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

func (r *issueGiftCardBatchRequest) normalizeAndValidate(now time.Time) error {
	r.Name = strings.TrimSpace(r.Name)
	r.Currency = strings.ToUpper(strings.TrimSpace(r.Currency))
	if r.Currency != "" && !isoCurrencyCodePattern.MatchString(r.Currency) {
		return errors.New("gift card currency is invalid")
	}
	if len([]rune(r.Name)) < 2 || len([]rune(r.Name)) > 160 {
		return errors.New("batch name must contain 2 to 160 characters")
	}
	if r.Quantity < 1 || r.Quantity > 500 {
		return errors.New("quantity must be between 1 and 500")
	}
	if r.CardValue < 1 || r.CardValue > 100_000_000 {
		return errors.New("card value must be between 0.01 and 1,000,000 CNY")
	}
	if r.ExpiresAt != nil {
		expiresAt := r.ExpiresAt.UTC()
		if !expiresAt.After(now.UTC().Add(5*time.Minute)) || expiresAt.After(now.UTC().AddDate(10, 0, 0)) {
			return errors.New("expiry must be between five minutes and ten years from now")
		}
		r.ExpiresAt = &expiresAt
	}
	return nil
}

func normalizeGiftCardCode(value string) (string, error) {
	compact := strings.Map(func(r rune) rune {
		if r == '-' || unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToUpper(r)
	}, strings.TrimSpace(value))
	if len(compact) < 30 || len(compact) > 80 || !strings.HasPrefix(compact, "LLQ") {
		return "", errors.New("invalid gift card code")
	}
	for _, r := range compact {
		if (r < 'A' || r > 'Z') && (r < '2' || r > '7') {
			return "", errors.New("invalid gift card code")
		}
	}
	return compact, nil
}

func giftCardHash(value string) (string, error) {
	normalized, err := normalizeGiftCardCode(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("%x", digest[:]), nil
}

func generateGiftCardCode() (string, string, error) {
	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		return "", "", err
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(random)
	parts := make([]string, 0, 8)
	for len(encoded) > 0 {
		width := 6
		if len(encoded) < width {
			width = len(encoded)
		}
		parts = append(parts, encoded[:width])
		encoded = encoded[width:]
	}
	code := "LLQ-" + strings.Join(parts, "-")
	normalized, _ := normalizeGiftCardCode(code)
	preview := normalized[:7] + "••••••" + normalized[len(normalized)-6:]
	return code, preview, nil
}

func (h Handler) IssueGiftCardBatch(c *gin.Context) {
	adminID, err := uuid.Parse(c.GetString("subject"))
	if err != nil {
		response.Error(c, 401, 40140, "error.invalid_login_state")
		return
	}
	reason := strings.TrimSpace(c.GetHeader("X-Change-Reason"))
	if len([]rune(reason)) < 4 || len([]rune(reason)) > 500 {
		response.Error(c, 422, 42257, "error.change_reason_issue_required")
		return
	}
	var req issueGiftCardBatchRequest
	if decodeStrictJSON(c, &req) != nil || req.normalizeAndValidate(time.Now()) != nil {
		response.Error(c, 422, 42260, "error.batch_details_invalid")
		return
	}
	if req.Currency == "" {
		req.Currency, err = service.StoreCurrency(h.DB)
		if err != nil {
			response.Error(c, 500, 50060, "error.store_currency_fetch_failed")
			return
		}
	}
	var currencyDefinition model.CurrencyDefinition
	if h.DB.Where("code = ? AND enabled = ?", req.Currency, true).First(&currencyDefinition).Error != nil {
		response.Error(c, 422, 42260, "error.gift_card_currency_invalid")
		return
	}
	batch := model.GiftCardBatch{
		BatchNo:   "GCB" + time.Now().UTC().Format("20060102150405") + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:8]),
		Name:      req.Name,
		Quantity:  req.Quantity,
		CardValue: req.CardValue,
		Currency:  req.Currency,
		Status:    "active",
		ExpiresAt: req.ExpiresAt,
		IssuedBy:  adminID,
	}
	issued := make([]issuedGiftCard, 0, req.Quantity)
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&batch).Error; err != nil {
			return err
		}
		seen := make(map[string]struct{}, req.Quantity)
		cards := make([]model.GiftCard, 0, req.Quantity)
		for len(cards) < req.Quantity {
			code, preview, generateErr := generateGiftCardCode()
			if generateErr != nil {
				return generateErr
			}
			hash, hashErr := giftCardHash(code)
			if hashErr != nil {
				return hashErr
			}
			if _, exists := seen[hash]; exists {
				continue
			}
			seen[hash] = struct{}{}
			card := model.GiftCard{
				BatchID:        &batch.ID,
				CodeHash:       hash,
				CodePreview:    preview,
				InitialBalance: req.CardValue,
				Balance:        req.CardValue,
				Currency:       req.Currency,
				Status:         "active",
				ExpiresAt:      req.ExpiresAt,
			}
			cards = append(cards, card)
			issued = append(issued, issuedGiftCard{ID: card.ID, Code: code, CodePreview: preview, CardValue: req.CardValue, Currency: req.Currency, ExpiresAt: req.ExpiresAt})
		}
		if err := tx.CreateInBatches(cards, 100).Error; err != nil {
			return err
		}
		for index := range cards {
			issued[index].ID = cards[index].ID
		}
		return nil
	})
	if err != nil {
		response.Error(c, 500, 50060, "error.gift_card_issue_failed")
		return
	}
	h.audit(c, "gift_card.batch.issue", "gift_card_batch", batch.ID.String(), fmt.Sprintf("%s；数量=%d；面值=%d", reason, req.Quantity, req.CardValue))
	response.Created(c, gin.H{
		"batch":  batch,
		"cards":  issued,
		"notice": i18n.Localize(c, "notice.giftcard_codes_once"),
	})
}

func (h Handler) AdminGiftCardBatches(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.GiftCardBatch{})
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		query = query.Where("status = ?", status)
	}
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("batch_no ILIKE ? OR name ILIKE ?", like, like)
	}
	var total int64
	var items []model.GiftCardBatch
	if err := query.Count(&total).Error; err != nil || query.Order("created_at DESC").Offset((page-1)*pageSize).Limit(pageSize).Find(&items).Error != nil {
		response.Error(c, 500, 50061, "error.gift_card_batch_fetch_failed")
		return
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) AdminGiftCards(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Table("gift_cards gc").
		Select("gc.id, gc.batch_id, COALESCE(gcb.batch_no, '') AS batch_no, COALESCE(gcb.name, '') AS batch_name, gc.code_preview, gc.initial_balance, gc.balance, gc.currency, gc.status, gc.redeemed_by, gc.redeemed_at, gc.expires_at, gc.created_at").
		Joins("LEFT JOIN gift_card_batches gcb ON gcb.id = gc.batch_id AND gcb.deleted_at IS NULL").
		Where("gc.deleted_at IS NULL")
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		query = query.Where("gc.status = ?", status)
	}
	if batchID := strings.TrimSpace(c.Query("batch_id")); batchID != "" {
		parsed, err := uuid.Parse(batchID)
		if err != nil {
			response.Error(c, 422, 42261, "error.gift_card_batch_id_invalid")
			return
		}
		query = query.Where("gc.batch_id = ?", parsed)
	}
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("gc.code_preview ILIKE ? OR gcb.batch_no ILIKE ? OR gcb.name ILIKE ?", like, like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50062, "error.gift_card_fetch_failed")
		return
	}
	var items []adminGiftCardItem
	if err := query.Order("gc.created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&items).Error; err != nil {
		response.Error(c, 500, 50062, "error.gift_card_fetch_failed")
		return
	}
	response.Page(c, items, total, page, pageSize)
}

type giftCardStatusRequest struct {
	Status string `json:"status"`
}

func (h Handler) UpdateGiftCardStatus(c *gin.Context) {
	cardID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42262, "error.gift_card_id_invalid")
		return
	}
	reason := strings.TrimSpace(c.GetHeader("X-Change-Reason"))
	if len([]rune(reason)) < 4 || len([]rune(reason)) > 500 {
		response.Error(c, 422, 42257, "error.change_reason_update_required")
		return
	}
	var req giftCardStatusRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42263, "error.gift_card_status_invalid")
		return
	}
	req.Status = strings.ToLower(strings.TrimSpace(req.Status))
	if req.Status != "active" && req.Status != "disabled" {
		response.Error(c, 422, 42263, "error.gift_card_activate_or_deactivate_only")
		return
	}
	var card model.GiftCard
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&card, "id = ?", cardID).Error; err != nil {
			return err
		}
		if card.RedeemedAt != nil || card.Balance <= 0 || card.Status == "redeemed" || (card.ExpiresAt != nil && !card.ExpiresAt.After(time.Now())) {
			return errGiftCardUnavailable
		}
		if req.Status == "active" && card.BatchID != nil {
			var batch model.GiftCardBatch
			if err := tx.Select("status").First(&batch, "id = ?", *card.BatchID).Error; err != nil || batch.Status != "active" {
				return errGiftCardUnavailable
			}
		}
		return tx.Model(&card).Update("status", req.Status).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40460, "error.gift_card_not_found")
		return
	}
	if errors.Is(err, errGiftCardUnavailable) {
		response.Error(c, 409, 40960, "error.gift_card_not_changeable")
		return
	}
	if err != nil {
		response.Error(c, 500, 50063, "error.gift_card_update_failed")
		return
	}
	h.audit(c, "gift_card.status.update", "gift_card", cardID.String(), reason+"；status="+req.Status)
	response.OK(c, gin.H{"card": card})
}

func (h Handler) DisableGiftCardBatch(c *gin.Context) {
	batchID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42264, "error.gift_card_batch_id_invalid")
		return
	}
	reason := strings.TrimSpace(c.GetHeader("X-Change-Reason"))
	if len([]rune(reason)) < 4 || len([]rune(reason)) > 500 {
		response.Error(c, 422, 42257, "error.change_reason_disable_required")
		return
	}
	var req giftCardStatusRequest
	if decodeStrictJSON(c, &req) != nil || strings.ToLower(strings.TrimSpace(req.Status)) != "disabled" {
		response.Error(c, 422, 42265, "error.batch_irreversible_disable_only")
		return
	}
	now := time.Now()
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var batch model.GiftCardBatch
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&batch, "id = ?", batchID).Error; err != nil {
			return err
		}
		if batch.Status == "disabled" {
			return nil
		}
		if err := tx.Model(&batch).Updates(map[string]any{"status": "disabled", "disabled_at": &now}).Error; err != nil {
			return err
		}
		return tx.Model(&model.GiftCard{}).Where("batch_id = ? AND status = ?", batch.ID, "active").Update("status", "disabled").Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40461, "error.gift_card_batch_not_found")
		return
	}
	if err != nil {
		response.Error(c, 500, 50064, "error.gift_card_batch_disable_failed")
		return
	}
	h.audit(c, "gift_card.batch.disable", "gift_card_batch", batchID.String(), reason)
	response.OK(c, gin.H{"id": batchID, "status": "disabled"})
}

type redeemGiftCardRequest struct {
	Code string `json:"code"`
}

func (h Handler) RedeemGiftCard(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req redeemGiftCardRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42266, "error.gift_card_redeem_code_invalid")
		return
	}
	hash, err := giftCardHash(req.Code)
	if err != nil {
		response.Error(c, 422, 42266, "error.gift_card_redeem_code_invalid")
		return
	}
	var card model.GiftCard
	var account model.WalletAccount
	var giftEntry model.GiftCardEntry
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("code_hash = ?", hash).First(&card).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errGiftCardUnavailable
			}
			return err
		}
		now := time.Now()
		if card.Status != "active" || card.RedeemedAt != nil || card.RedeemedBy != nil || card.Balance <= 0 || !isoCurrencyCodePattern.MatchString(card.Currency) {
			return errGiftCardUnavailable
		}
		if card.ExpiresAt != nil && !card.ExpiresAt.After(now) {
			if updateErr := tx.Model(&card).Update("status", "expired").Error; updateErr != nil {
				return updateErr
			}
			return errGiftCardUnavailable
		}
		if card.BatchID != nil {
			var batch model.GiftCardBatch
			if err := tx.Select("status", "expires_at").First(&batch, "id = ?", *card.BatchID).Error; err != nil || batch.Status != "active" || (batch.ExpiresAt != nil && !batch.ExpiresAt.After(now)) {
				return errGiftCardUnavailable
			}
		}
		candidate := model.WalletAccount{OwnerType: "user", OwnerID: userID, Currency: card.Currency}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&candidate).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("owner_type = ? AND owner_id = ? AND currency = ?", "user", userID, card.Currency).First(&account).Error; err != nil {
			return err
		}
		if account.Balance > math.MaxInt64-card.Balance {
			return errGiftCardBalance
		}
		balanceAfter := account.Balance + card.Balance
		if err := tx.Model(&account).Updates(map[string]any{"balance": balanceAfter, "version": gorm.Expr("version + 1")}).Error; err != nil {
			return err
		}
		walletEntry := model.WalletEntry{
			AccountID: account.ID, EntryNo: "GCR-" + strings.ToUpper(strings.ReplaceAll(card.ID.String(), "-", "")),
			Type: "gift_card_redeem", Amount: card.Balance, BalanceAfter: balanceAfter,
			ReferenceType: "gift_card", ReferenceID: &card.ID, Description: "礼品卡兑换入账 " + card.CodePreview,
		}
		if err := tx.Create(&walletEntry).Error; err != nil {
			return err
		}
		amount := card.Balance
		giftEntry = model.GiftCardEntry{GiftCardID: card.ID, UserID: &userID, Amount: -amount, BalanceAfter: 0, Type: "redeem"}
		if err := tx.Create(&giftEntry).Error; err != nil {
			return err
		}
		if err := tx.Model(&card).Updates(map[string]any{"balance": 0, "status": "redeemed", "redeemed_by": &userID, "redeemed_at": &now}).Error; err != nil {
			return err
		}
		card.Balance = 0
		card.Status = "redeemed"
		card.RedeemedBy = &userID
		card.RedeemedAt = &now
		account.Balance = balanceAfter
		return nil
	})
	if errors.Is(err, errGiftCardUnavailable) {
		response.Error(c, 409, 40961, "error.redemption_code_invalid")
		return
	}
	if errors.Is(err, errGiftCardBalance) {
		response.Error(c, 409, 40962, "error.wallet_balance_exceeds_limit")
		return
	}
	if err != nil {
		response.Error(c, 500, 50065, "error.gift_card_redeem_failed")
		return
	}
	response.OK(c, gin.H{
		"card":           toUserGiftCardDTO(card),
		"entry":          toUserGiftCardEntryDTO(giftEntry),
		"wallet_balance": account.Balance,
	})
}
