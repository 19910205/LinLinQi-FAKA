package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/payment"
	"linlinqi/api/internal/service"
	"linlinqi/api/pkg/response"
)

type rechargeRequest struct {
	Amount      int64  `json:"amount"`
	ChannelCode string `json:"channel_code"`
	Currency    string `json:"currency"`
}

type rechargeDTO struct {
	ID             uuid.UUID  `json:"id"`
	RechargeNo     string     `json:"recharge_no"`
	Amount         int64      `json:"amount"`
	Bonus          int64      `json:"bonus"`
	Currency       string     `json:"currency"`
	CreditAmount   int64      `json:"credit_amount"`
	CreditCurrency string     `json:"credit_currency"`
	FXSnapshotID   *uuid.UUID `json:"fx_snapshot_id,omitempty"`
	ChannelCode    string     `json:"channel_code"`
	ChannelName    string     `json:"channel_name"`
	Status         string     `json:"status"`
	CheckoutURL    string     `json:"checkout_url,omitempty"`
	ExpiresAt      time.Time  `json:"expires_at"`
	PaidAt         *time.Time `json:"paid_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func toRechargeDTO(order model.RechargeOrder, channel model.PaymentChannel) rechargeDTO {
	return rechargeDTO{
		ID: order.ID, RechargeNo: order.RechargeNo, Amount: order.Amount, Bonus: order.Bonus,
		Currency: order.Currency, CreditAmount: order.CreditAmount, CreditCurrency: order.CreditCurrency, FXSnapshotID: order.FXSnapshotID, ChannelCode: channel.Code, ChannelName: channel.Name,
		Status: order.Status, CheckoutURL: order.CheckoutURL, ExpiresAt: order.ExpiresAt,
		PaidAt: order.PaidAt, CreatedAt: order.CreatedAt, UpdatedAt: order.UpdatedAt,
	}
}

func validIdempotencyKey(value string) bool {
	if len(value) < 16 || len(value) > 100 {
		return false
	}
	for _, character := range value {
		if character > unicode.MaxASCII || unicode.IsControl(character) || unicode.IsSpace(character) {
			return false
		}
	}
	return true
}

func hashIdempotencyKey(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (h Handler) MyRecharges(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	page, pageSize := pagination(c)
	if err := h.DB.Model(&model.RechargeOrder{}).
		Where("user_id = ? AND status = ? AND expires_at < ?", userID, "pending", time.Now()).
		Update("status", "expired").Error; err != nil {
		response.Error(c, 500, 50120, "error.wallet_recharge_list_fetch_failed")
		return
	}
	query := h.DB.Table("recharge_orders ro").
		Select("ro.*, pc.code AS channel_code, pc.name AS channel_name").
		Joins("JOIN payment_channels pc ON pc.id = ro.channel_id AND pc.deleted_at IS NULL").
		Where("ro.deleted_at IS NULL AND ro.user_id = ?", userID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50120, "error.wallet_recharge_list_fetch_failed")
		return
	}
	type row struct {
		model.RechargeOrder
		ChannelCode string
		ChannelName string
	}
	var rows []row
	if err := query.Order("ro.created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error; err != nil {
		response.Error(c, 500, 50120, "error.wallet_recharge_list_fetch_failed")
		return
	}
	items := make([]rechargeDTO, 0, len(rows))
	for _, item := range rows {
		items = append(items, toRechargeDTO(item.RechargeOrder, model.PaymentChannel{Code: item.ChannelCode, Name: item.ChannelName}))
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) CreateRecharge(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if !validIdempotencyKey(idempotencyKey) {
		response.Error(c, 422, 42299, "error.spec_fields_invalid")
		return
	}
	var req rechargeRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42299, "error.task_id_invalid")
		return
	}
	req.ChannelCode = strings.ToLower(strings.TrimSpace(req.ChannelCode))
	if !paymentChannelCodePattern.MatchString(req.ChannelCode) {
		response.Error(c, 422, 42299, "error.recharge_amount_channel_invalid")
		return
	}
	var channel model.PaymentChannel
	if err := h.DB.Where("code = ? AND enabled = ?", req.ChannelCode, true).First(&channel).Error; err != nil {
		response.Error(c, 422, 42299, "error.payment_channel_unavailable")
		return
	}
	storeCurrency, err := service.StoreCurrency(h.DB)
	if err != nil {
		response.Error(c, 500, 50121, "error.store_currency_fetch_failed")
		return
	}
	if requested := strings.ToUpper(strings.TrimSpace(req.Currency)); requested != "" && requested != storeCurrency {
		response.Error(c, 409, 40999, "error.recharge_currency_must_match_store")
		return
	}
	currencyDefinition, err := resolveEnabledCurrencyDefinition(h.DB, storeCurrency, true)
	if errors.Is(err, errCurrencySelectionInvalid) {
		response.Error(c, 422, 42299, "error.currency_code_invalid")
		return
	}
	if errors.Is(err, errCurrencySelectionUnavailable) {
		response.Error(c, 422, 42299, "error.currency_unavailable")
		return
	}
	if err != nil {
		response.Error(c, 500, 50121, "error.currency_definition_fetch_failed")
		return
	}
	scale, err := currencyMinorScale(currencyDefinition)
	if err != nil {
		response.Error(c, 500, 50121, "error.currency_definition_invalid")
		return
	}
	minimumRechargeAmount := scale
	maximumRechargeAmount := int64(100_000) * scale
	if req.Amount < minimumRechargeAmount || req.Amount > maximumRechargeAmount {
		response.Error(c, 422, 42299, "error.recharge_amount_out_of_range", gin.H{
			"currency": currencyDefinition.Code, "minimum_minor": minimumRechargeAmount, "maximum_minor": maximumRechargeAmount,
		})
		return
	}
	settlementCurrency, settlementErr := paymentChannelSettlementCurrency(channel)
	if settlementErr != nil {
		response.Error(c, 422, 42299, "error.payment_channel_settlement_currency_invalid")
		return
	}
	conversion, conversionErr := h.paymentCurrencyConversion(c, currencyDefinition.Code, settlementCurrency)
	if conversionErr != nil {
		response.Error(c, 503, 50361, "error.payment_settlement_rate_unavailable")
		return
	}
	paymentAmount, conversionErr := conversion.Amount(req.Amount)
	if conversionErr != nil || paymentAmount < 1 {
		response.Error(c, 503, 50361, "error.payment_settlement_rate_unavailable")
		return
	}
	driver, err := h.paymentDriver(channel)
	if err != nil {
		response.Error(c, 503, 50360, "error.payment_channel_not_production_configured")
		return
	}

	now := time.Now()
	hash := hashIdempotencyKey(idempotencyKey)
	var recharge model.RechargeOrder
	var created, returnExisting, creationInProgress bool
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockCurrentPaymentChannelTx(tx, channel); err != nil {
			return err
		}
		if err := tx.Exec(
			"SELECT pg_advisory_xact_lock(hashtextextended(?, 20260809))",
			"linlinqi-recharge:"+userID.String()+":"+hash,
		).Error; err != nil {
			return err
		}
		findErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND idempotency_key_hash = ?", userID, hash).First(&recharge).Error
		if findErr == nil {
			if recharge.CreditAmount != req.Amount || recharge.ChannelID != channel.ID || recharge.CreditCurrency != currencyDefinition.Code || recharge.Amount != paymentAmount || recharge.Currency != settlementCurrency {
				return service.ErrIdempotencyConflict
			}
			if recharge.Status != "creating" {
				returnExisting = true
				return nil
			}
			if recharge.UpdatedAt.After(now.Add(-30 * time.Second)) {
				creationInProgress = true
				return nil
			}
			return tx.Model(&recharge).Update("updated_at", now).Error
		}
		if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		recharge = model.RechargeOrder{
			Base:               model.Base{ID: uuid.New()},
			RechargeNo:         "LQRC" + now.UTC().Format("20060102150405") + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:10]),
			IntentNo:           "LQRCI" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")),
			IdempotencyKeyHash: hash, UserID: userID, Amount: paymentAmount, Bonus: 0,
			Currency: settlementCurrency, CreditAmount: req.Amount, CreditCurrency: currencyDefinition.Code, ChannelID: channel.ID, Status: "creating", ExpiresAt: now.Add(15 * time.Minute),
		}
		if conversion.Snapshot != nil {
			id := conversion.Snapshot.ID
			recharge.FXSnapshotID = &id
		}
		created = true
		return tx.Create(&recharge).Error
	})
	if errors.Is(err, service.ErrIdempotencyConflict) {
		response.Error(c, 409, 40999, "error.idempotency_key_used_for_different_topup")
		return
	}
	if errors.Is(err, errPaymentChannelChanged) {
		response.Error(c, 409, 40964, "error.payment_channel_changed_retry")
		return
	}
	if err != nil {
		response.Error(c, 500, 50121, "error.wallet_recharge_order_create_failed")
		return
	}
	if returnExisting {
		response.OK(c, gin.H{"recharge": toRechargeDTO(recharge, channel)})
		return
	}
	if creationInProgress {
		response.Error(c, 409, 40999, "error.topup_creating_retry_soon")
		return
	}

	result, err := driver.Create(c, payment.CreateRequest{
		IntentNo: recharge.IntentNo, OrderNo: recharge.RechargeNo, Amount: recharge.Amount,
		Currency: recharge.Currency, Subject: "LinLinQi 钱包充值 " + recharge.RechargeNo,
		NotifyURL: strings.TrimRight(h.Cfg.AppURL, "/") + "/api/v1/payments/" + channel.Code + "/callback",
		ReturnURL: strings.TrimRight(h.Cfg.UserAppURL, "/") + "/account/wallet",
	})
	if err != nil {
		h.DB.Model(&recharge).Update("updated_at", time.Now())
		response.Error(c, 502, 50262, "error.payment_response_uncertain_retry_idempotency")
		return
	}
	if result.ProviderTradeNo == "" || len(result.ProviderTradeNo) > 160 || !validCheckoutURL(result.CheckoutURL, h.Cfg.Env != "production") {
		h.DB.Model(&recharge).Update("updated_at", time.Now())
		response.Error(c, 502, 50261, "error.payment_unsafe_incomplete_txn")
		return
	}
	if result.ExpiresAt.IsZero() {
		result.ExpiresAt = time.Now().Add(15 * time.Minute)
	}
	updates := map[string]any{
		"status": "pending", "provider_trade_no": result.ProviderTradeNo,
		"checkout_url": result.CheckoutURL, "expires_at": result.ExpiresAt,
	}
	if result := h.DB.Model(&model.RechargeOrder{}).
		Where("id = ? AND status = ?", recharge.ID, "creating").Updates(updates); result.Error != nil || result.RowsAffected != 1 {
		response.Error(c, 500, 50122, "error.payment_txn_save_failed_retry_idempotency")
		return
	}
	if err := h.DB.First(&recharge, "id = ?", recharge.ID).Error; err != nil {
		response.Error(c, 500, 50122, "error.payment_txn_save_failed_retry_idempotency")
		return
	}
	_ = h.createOperationalNotifications(h.DB, "recharge.created", recharge.ID.String(), map[string]string{"user_id": recharge.UserID.String(), "order_no": recharge.RechargeNo, "status": recharge.Status, "amount": strconv.FormatInt(recharge.CreditAmount, 10), "currency": recharge.CreditCurrency, "channel": channel.Code, "summary": "用户充值单已创建"})
	payload := gin.H{"recharge": toRechargeDTO(recharge, channel), "qr_code": result.QRCode}
	if created {
		response.Created(c, payload)
		return
	}
	response.OK(c, payload)
}

type adminRechargeDTO struct {
	rechargeDTO
	UserID               uuid.UUID `json:"user_id"`
	UserEmail            string    `json:"user_email"`
	ProviderTrade        string    `json:"provider_trade_no"`
	ExceptionCount       int64     `json:"exception_count"`
	RefundDisposition    string    `json:"refund_disposition,omitempty"`
	RefundMismatchReason string    `json:"refund_mismatch_reason,omitempty"`
	RefundActualAmount   int64     `json:"refund_actual_amount,omitempty"`
	RefundActualCurrency string    `json:"refund_actual_currency,omitempty"`
	RefundAttempts       int       `json:"refund_attempts,omitempty"`
	RefundLastError      string    `json:"refund_last_error,omitempty"`
}

func (h Handler) AdminRecharges(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Table("recharge_orders ro").
		Select(`ro.*, pc.code AS channel_code, pc.name AS channel_name, u.email AS user_email,
			(SELECT COUNT(*) FROM recharge_transactions rt WHERE rt.recharge_order_id = ro.id AND rt.deleted_at IS NULL AND rt.disposition NOT IN ('credited', 'ignored')) AS exception_count,
			COALESCE((SELECT rt.disposition FROM recharge_transactions rt WHERE rt.recharge_order_id = ro.id AND rt.deleted_at IS NULL AND rt.disposition NOT IN ('credited', 'ignored') ORDER BY rt.created_at DESC LIMIT 1), '') AS refund_disposition,
			COALESCE((SELECT rt.mismatch_reason FROM recharge_transactions rt WHERE rt.recharge_order_id = ro.id AND rt.deleted_at IS NULL AND rt.disposition NOT IN ('credited', 'ignored') ORDER BY rt.created_at DESC LIMIT 1), '') AS refund_mismatch_reason,
			COALESCE((SELECT rt.amount FROM recharge_transactions rt WHERE rt.recharge_order_id = ro.id AND rt.deleted_at IS NULL AND rt.disposition NOT IN ('credited', 'ignored') ORDER BY rt.created_at DESC LIMIT 1), 0) AS refund_actual_amount,
			COALESCE((SELECT rt.currency FROM recharge_transactions rt WHERE rt.recharge_order_id = ro.id AND rt.deleted_at IS NULL AND rt.disposition NOT IN ('credited', 'ignored') ORDER BY rt.created_at DESC LIMIT 1), '') AS refund_actual_currency,
			COALESCE((SELECT rt.refund_attempts FROM recharge_transactions rt WHERE rt.recharge_order_id = ro.id AND rt.deleted_at IS NULL AND rt.disposition NOT IN ('credited', 'ignored') ORDER BY rt.created_at DESC LIMIT 1), 0) AS refund_attempts,
			COALESCE((SELECT rt.refund_last_error FROM recharge_transactions rt WHERE rt.recharge_order_id = ro.id AND rt.deleted_at IS NULL AND rt.disposition NOT IN ('credited', 'ignored') ORDER BY rt.created_at DESC LIMIT 1), '') AS refund_last_error`).
		Joins("JOIN payment_channels pc ON pc.id = ro.channel_id AND pc.deleted_at IS NULL").
		Joins("JOIN users u ON u.id = ro.user_id AND u.deleted_at IS NULL").
		Where("ro.deleted_at IS NULL")
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		query = query.Where("ro.status = ?", status)
	}
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("ro.recharge_no ILIKE ? OR ro.provider_trade_no ILIKE ? OR u.email ILIKE ?", like, like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50123, "error.recharge_operation_list_fetch_failed")
		return
	}
	type row struct {
		model.RechargeOrder
		ChannelCode          string
		ChannelName          string
		UserEmail            string
		ExceptionCount       int64
		RefundDisposition    string
		RefundMismatchReason string
		RefundActualAmount   int64
		RefundActualCurrency string
		RefundAttempts       int
		RefundLastError      string
	}
	var rows []row
	if err := query.Order("ro.created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error; err != nil {
		response.Error(c, 500, 50123, "error.recharge_operation_list_fetch_failed")
		return
	}
	items := make([]adminRechargeDTO, 0, len(rows))
	for _, item := range rows {
		items = append(items, adminRechargeDTO{
			rechargeDTO: toRechargeDTO(item.RechargeOrder, model.PaymentChannel{Code: item.ChannelCode, Name: item.ChannelName}),
			UserID:      item.UserID, UserEmail: item.UserEmail, ProviderTrade: item.ProviderTradeNo,
			ExceptionCount: item.ExceptionCount, RefundDisposition: item.RefundDisposition,
			RefundMismatchReason: item.RefundMismatchReason, RefundActualAmount: item.RefundActualAmount,
			RefundActualCurrency: item.RefundActualCurrency, RefundAttempts: item.RefundAttempts,
			RefundLastError: item.RefundLastError,
		})
	}
	response.Page(c, items, total, page, pageSize)
}

type rechargeCallbackOutcome struct {
	RefundTransactionID *uuid.UUID
}

func rechargeCallbackCanCredit(recharge model.RechargeOrder, callback payment.CallbackResult, paidAt time.Time) bool {
	return callback.Status == "succeeded" && callback.Amount == recharge.Amount && callback.Currency == recharge.Currency &&
		(recharge.Status == "creating" || recharge.Status == "pending") &&
		(recharge.ProviderTradeNo == "" || recharge.ProviderTradeNo == callback.ProviderTradeNo) &&
		recharge.ExpiresAt.After(paidAt)
}

func rechargeCallbackRefundReason(recharge model.RechargeOrder, callback payment.CallbackResult, paidAt time.Time) string {
	if callback.Currency != recharge.Currency {
		return "充值实收币种与渠道应收币种不一致，系统自动原路退款"
	}
	if callback.Amount != recharge.Amount {
		return "充值实收金额与渠道应收金额不一致，系统自动原路退款"
	}
	if recharge.ProviderTradeNo != "" && recharge.ProviderTradeNo != callback.ProviderTradeNo {
		return "同一充值意图收到额外渠道交易，系统自动原路退款"
	}
	if !recharge.ExpiresAt.After(paidAt) || recharge.Status == "expired" {
		return "充值支付在充值单失效后到账，系统自动原路退款"
	}
	return "充值单当前状态不可入账，系统自动原路退款"
}

func minimizedRechargeCallbackPayload(callback payment.CallbackResult, body []byte) (string, error) {
	digest := sha256.Sum256(body)
	payload := map[string]any{
		"verified": true, "event_id": callback.EventID, "status": callback.Status,
		"amount": callback.Amount, "currency": callback.Currency,
		"payload_sha256": hex.EncodeToString(digest[:]),
	}
	if callback.PaidAt != nil {
		payload["paid_at"] = callback.PaidAt.UTC().Format(time.RFC3339Nano)
	}
	encoded, err := json.Marshal(payload)
	return string(encoded), err
}

func (h Handler) processRechargeCallback(
	tx *gorm.DB,
	channel model.PaymentChannel,
	callback payment.CallbackResult,
	eventID string,
	body []byte,
) (rechargeCallbackOutcome, error) {
	var outcome rechargeCallbackOutcome
	var recharge model.RechargeOrder
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("intent_no = ? AND channel_id = ?", callback.IntentNo, channel.ID).First(&recharge).Error; err != nil {
		return outcome, err
	}
	if err := tx.Exec(
		"SELECT pg_advisory_xact_lock(hashtextextended(?, 20260810))",
		"linlinqi-recharge-receipt:"+recharge.ID.String()+":"+callback.ProviderTradeNo,
	).Error; err != nil {
		return outcome, err
	}
	var existing model.RechargeTransaction
	findExisting := tx.Where("provider_event_id = ? OR (recharge_order_id = ? AND provider_trade_no = ?)", eventID, recharge.ID, callback.ProviderTradeNo).First(&existing).Error
	if findExisting == nil {
		if existing.Disposition == "refund_pending" || existing.Disposition == "refund_retrying" {
			id := existing.ID
			outcome.RefundTransactionID = &id
		}
		return outcome, nil
	}
	if !errors.Is(findExisting, gorm.ErrRecordNotFound) {
		return outcome, findExisting
	}
	if callback.Status != "succeeded" {
		return outcome, fmt.Errorf("recharge callback state is not succeeded")
	}
	paidAt := time.Now()
	if callback.PaidAt != nil {
		paidAt = *callback.PaidAt
	}
	minimalPayload, err := minimizedRechargeCallbackPayload(callback, body)
	if err != nil {
		return outcome, err
	}
	canCredit := rechargeCallbackCanCredit(recharge, callback, paidAt)
	transaction := model.RechargeTransaction{
		RechargeOrderID: recharge.ID, ProviderEventID: eventID, ProviderTradeNo: callback.ProviderTradeNo,
		Amount: callback.Amount, Currency: callback.Currency, ExpectedAmount: recharge.Amount, ExpectedCurrency: recharge.Currency,
		Status: "succeeded", Disposition: "credited", RawPayload: minimalPayload, PaidAt: &paidAt,
	}
	if !canCredit {
		transaction.Disposition = "refund_pending"
		transaction.MismatchReason = rechargeCallbackRefundReason(recharge, callback, paidAt)
		transaction.RefundNo = fmt.Sprintf("LQRR%s%s", time.Now().UTC().Format("20060102150405"), strings.ToUpper(uuid.NewString()[:8]))
	}
	if err := tx.Create(&transaction).Error; err != nil {
		return outcome, err
	}
	if !canCredit {
		updates := map[string]any{"paid_at": &paidAt}
		if recharge.ProviderTradeNo == "" {
			updates["provider_trade_no"] = callback.ProviderTradeNo
		}
		if recharge.Status != "succeeded" {
			updates["status"] = "requires_refund"
		}
		if err := tx.Model(&recharge).Updates(updates).Error; err != nil {
			return outcome, err
		}
		id := transaction.ID
		outcome.RefundTransactionID = &id
		return outcome, nil
	}
	account := model.WalletAccount{OwnerType: "user", OwnerID: recharge.UserID, Currency: recharge.CreditCurrency}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&account).Error; err != nil {
		return outcome, err
	}
	if err := tx.Where("owner_type = ? AND owner_id = ? AND currency = ?", "user", recharge.UserID, recharge.CreditCurrency).First(&account).Error; err != nil {
		return outcome, err
	}
	credit := recharge.CreditAmount + recharge.Bonus
	_, err = service.ApplyWalletMutation(tx, service.WalletMutation{
		EntryNo: "LQW-RC-" + recharge.ID.String(), AccountID: account.ID, Amount: credit,
		Type: "recharge", ReferenceType: "recharge", ReferenceID: &recharge.ID,
		Description: "钱包充值 " + recharge.RechargeNo,
	})
	if err != nil {
		return outcome, err
	}
	if err := tx.Model(&recharge).Updates(map[string]any{
		"status": "succeeded", "provider_trade_no": callback.ProviderTradeNo, "paid_at": &paidAt,
	}).Error; err != nil {
		return outcome, err
	}
	return outcome, h.createOperationalNotifications(tx, "recharge.succeeded", recharge.ID.String(), map[string]string{"user_id": recharge.UserID.String(), "order_no": recharge.RechargeNo, "status": "succeeded", "amount": strconv.FormatInt(credit, 10), "currency": recharge.CreditCurrency, "channel": channel.Code, "summary": "用户充值已到账"})
}
