package handler

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"path/filepath"
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
	"linlinqi/api/internal/queue"
	"linlinqi/api/internal/service"
	"linlinqi/api/pkg/response"
)

const maximumReconciliationRows = 50_000

type statementRow struct {
	Direction       string
	ProviderTradeNo string
	Currency        string
	Amount          int64
	OccurredAt      time.Time
}

type reconciliationSystemRecord struct {
	Key             string
	Direction       string
	ProviderTradeNo string
	Currency        string
	OrderID         uuid.UUID
	Amount          int64
}

func normalizeStatementHeader(value string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "\ufeff")))
}

func parseReconciliationStatement(raw []byte, periodFrom, periodTo time.Time, expectedCurrencies ...string) ([]statementRow, error) {
	expectedCurrency := "CNY"
	if len(expectedCurrencies) > 0 && strings.TrimSpace(expectedCurrencies[0]) != "" {
		expectedCurrency = strings.ToUpper(strings.TrimSpace(expectedCurrencies[0]))
	}
	if !isoCurrencyCodePattern.MatchString(expectedCurrency) {
		return nil, errors.New("statement currency is invalid")
	}
	reader := csv.NewReader(bytes.NewReader(raw))
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true
	header, err := reader.Read()
	if err != nil {
		return nil, errors.New("statement header is missing")
	}
	columns := make(map[string]int, len(header))
	for index, value := range header {
		columns[normalizeStatementHeader(value)] = index
	}
	tradeColumn, hasTrade := columns["provider_trade_no"]
	amountColumn, hasAmount := columns["amount_minor"]
	if !hasAmount {
		amountColumn, hasAmount = columns["amount"]
	}
	occurredColumn, hasOccurred := columns["occurred_at"]
	if !hasTrade || !hasAmount || !hasOccurred {
		return nil, errors.New("statement requires provider_trade_no, amount_minor and occurred_at")
	}
	directionColumn, hasDirection := columns["direction"]
	statusColumn, hasStatus := columns["status"]
	currencyColumn, hasCurrency := columns["currency"]
	seen := make(map[string]struct{})
	rows := make([]statementRow, 0)
	for line := 2; ; line++ {
		record, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("invalid CSV on line %d", line)
		}
		if len(record) == 0 || (len(record) == 1 && strings.TrimSpace(record[0]) == "") {
			continue
		}
		maximumColumn := max(tradeColumn, amountColumn, occurredColumn)
		if len(record) <= maximumColumn {
			return nil, fmt.Errorf("missing required value on line %d", line)
		}
		direction := "payment"
		if hasDirection && directionColumn < len(record) && strings.TrimSpace(record[directionColumn]) != "" {
			direction = strings.ToLower(strings.TrimSpace(record[directionColumn]))
		}
		if direction != "payment" && direction != "refund" {
			return nil, fmt.Errorf("invalid direction on line %d", line)
		}
		if hasStatus && statusColumn < len(record) {
			status := strings.ToLower(strings.TrimSpace(record[statusColumn]))
			if status != "" && status != "succeeded" && status != "paid" && status != "completed" && status != "refunded" {
				continue
			}
		}
		currency := expectedCurrency
		if hasCurrency && currencyColumn < len(record) {
			provided := strings.ToUpper(strings.TrimSpace(record[currencyColumn]))
			if provided != "" && provided != expectedCurrency {
				return nil, fmt.Errorf("unsupported currency on line %d", line)
			}
			if provided != "" {
				currency = provided
			}
		}
		tradeNo := strings.TrimSpace(record[tradeColumn])
		if tradeNo == "" || utf8.RuneCountInString(tradeNo) > 160 {
			return nil, fmt.Errorf("invalid provider trade number on line %d", line)
		}
		amount, parseErr := strconv.ParseInt(strings.TrimSpace(record[amountColumn]), 10, 64)
		if parseErr != nil || amount < 1 {
			return nil, fmt.Errorf("amount_minor must be a positive integer on line %d", line)
		}
		occurredAt, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(record[occurredColumn]))
		if parseErr != nil || occurredAt.Before(periodFrom) || !occurredAt.Before(periodTo) {
			return nil, fmt.Errorf("occurred_at is outside the selected period on line %d", line)
		}
		key := direction + ":" + tradeNo
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("duplicate provider trade number on line %d", line)
		}
		seen[key] = struct{}{}
		rows = append(rows, statementRow{Direction: direction, ProviderTradeNo: tradeNo, Currency: currency, Amount: amount, OccurredAt: occurredAt.UTC()})
		if len(rows) > maximumReconciliationRows {
			return nil, fmt.Errorf("statement exceeds %d successful rows", maximumReconciliationRows)
		}
	}
	if len(rows) == 0 {
		return nil, errors.New("statement contains no successful rows")
	}
	return rows, nil
}

func (h Handler) ImportReconciliation(c *gin.Context) {
	adminID, err := uuid.Parse(c.GetString("subject"))
	if err != nil {
		response.Error(c, 401, 40140, "error.invalid_login_state")
		return
	}
	reason, ok := requireAdminChangeReason(c, "导入对账单")
	if !ok {
		return
	}
	channelID, err := uuid.Parse(strings.TrimSpace(c.PostForm("channel_id")))
	periodFrom, fromErr := time.Parse(time.RFC3339, strings.TrimSpace(c.PostForm("period_from")))
	periodTo, toErr := time.Parse(time.RFC3339, strings.TrimSpace(c.PostForm("period_to")))
	if err != nil || fromErr != nil || toErr != nil || !periodTo.After(periodFrom) || periodTo.Sub(periodFrom) > 31*24*time.Hour || periodTo.After(time.Now().Add(5*time.Minute)) {
		response.Error(c, 422, 42278, "error.reconciliation_period_invalid")
		return
	}
	var channel model.PaymentChannel
	if h.DB.Select("id", "name").First(&channel, "id = ?", channelID).Error != nil {
		response.Error(c, 404, 40467, "error.payment_channel_not_found")
		return
	}
	currencyCode := strings.ToUpper(strings.TrimSpace(c.PostForm("currency")))
	if currencyCode == "" {
		currencyCode, err = service.StoreCurrency(h.DB)
	}
	var currencyDefinition model.CurrencyDefinition
	if err != nil || h.DB.Where("code = ? AND enabled = ?", currencyCode, true).First(&currencyDefinition).Error != nil {
		response.Error(c, 422, 42278, "error.reconciliation_currency_invalid")
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil || fileHeader.Size < 1 || fileHeader.Size > 8<<20 || !strings.EqualFold(filepath.Ext(fileHeader.Filename), ".csv") {
		response.Error(c, 422, 42279, "error.csv_reconciliation_upload_limit")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		response.Error(c, 422, 42279, "error.csv_statement_read_failed")
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, (8<<20)+1))
	if err != nil || len(raw) > 8<<20 {
		response.Error(c, 422, 42279, "error.csv_statement_read_failed")
		return
	}
	statementRows, err := parseReconciliationStatement(raw, periodFrom, periodTo, currencyCode)
	if err != nil {
		response.Error(c, 422, 42280, "error.statement_file_format_invalid", map[string]interface{}{"Err": err.Error()})
		return
	}
	digest := sha256.Sum256(raw)
	sourceName := filepath.Base(fileHeader.Filename)
	if utf8.RuneCountInString(sourceName) > 255 {
		sourceName = string([]rune(sourceName)[:255])
	}
	batch := model.ReconciliationBatch{
		BatchNo:   "LQRC" + time.Now().UTC().Format("20060102150405") + strings.ToUpper(uuid.NewString()[:8]),
		ChannelID: channel.ID, Currency: currencyCode, PeriodFrom: periodFrom.UTC(), PeriodTo: periodTo.UTC(), SourceFile: sourceName,
		StatementHash: fmt.Sprintf("%x", digest[:]), ImportedBy: adminID, Status: "pending",
	}
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&batch).Error; err != nil {
			return err
		}
		systemRecords, loadErr := loadReconciliationSystemRecords(tx, channel.ID, currencyCode, periodFrom, periodTo)
		if loadErr != nil {
			return loadErr
		}
		byKey := make(map[string]reconciliationSystemRecord, len(systemRecords))
		for _, record := range systemRecords {
			byKey[record.Key] = record
		}
		items := make([]model.ReconciliationItem, 0, len(statementRows)+len(systemRecords))
		imported := make(map[string]struct{}, len(statementRows))
		for _, statement := range statementRows {
			key := statement.Direction + ":" + statement.ProviderTradeNo
			imported[key] = struct{}{}
			occurredAt := statement.OccurredAt
			item := model.ReconciliationItem{BatchID: batch.ID, Direction: statement.Direction, ProviderTradeNo: statement.ProviderTradeNo, Currency: statement.Currency, ProviderOccurredAt: &occurredAt, ProviderAmount: statement.Amount, Difference: statement.Amount, Status: "missing_system"}
			if system, exists := byKey[key]; exists {
				item.OrderID = &system.OrderID
				item.SystemAmount = system.Amount
				item.Difference = statement.Amount - system.Amount
				item.Status = "amount_mismatch"
				if item.Difference == 0 {
					item.Status = "matched"
				}
			}
			items = append(items, item)
		}
		for _, system := range systemRecords {
			if _, exists := imported[system.Key]; exists {
				continue
			}
			orderID := system.OrderID
			items = append(items, model.ReconciliationItem{BatchID: batch.ID, OrderID: &orderID, Direction: system.Direction, ProviderTradeNo: system.ProviderTradeNo, Currency: system.Currency, SystemAmount: system.Amount, Difference: -system.Amount, Status: "missing_provider"})
		}
		if err := tx.CreateInBatches(items, 500).Error; err != nil {
			return err
		}
		return tx.Model(&batch).Update("total", len(items)).Error
	})
	if err != nil {
		response.Error(c, 409, 40970, "error.statement_import_or_save_failed")
		return
	}
	h.audit(c, "reconciliation.import", "reconciliation_batch", batch.ID.String(), reason+"；channel="+channel.Name)
	client := queue.NewClient(h.Cfg, h.DB)
	_, enqueueErr := client.Enqueue(queue.TypeReconciliationRun, map[string]string{"batch_id": batch.ID.String()})
	_ = client.Close()
	response.Created(c, gin.H{"batch": batch, "queued": enqueueErr == nil, "notice": i18n.Localize(c, "notice.reconciliation_ephemeral")})
}

func loadReconciliationSystemRecords(tx *gorm.DB, channelID uuid.UUID, currencyCode string, periodFrom, periodTo time.Time) ([]reconciliationSystemRecord, error) {
	type paymentRow struct {
		ID              uuid.UUID
		OrderID         uuid.UUID
		ProviderTradeNo string
		Amount          int64
		Currency        string
	}
	var payments []paymentRow
	if err := tx.Table("payment_intents").Select("id, order_id, provider_trade_no, amount, currency").Where("channel_id = ? AND currency = ? AND status IN ? AND succeeded_at >= ? AND succeeded_at < ? AND deleted_at IS NULL", channelID, currencyCode, []string{"succeeded", "partially_refunded", "refunded"}, periodFrom, periodTo).Limit(maximumReconciliationRows + 1).Scan(&payments).Error; err != nil {
		return nil, err
	}
	if len(payments) > maximumReconciliationRows {
		return nil, errors.New("payment period is too broad")
	}
	type refundRow struct {
		ID               uuid.UUID
		OrderID          uuid.UUID
		ProviderRefundNo string
		Amount           int64
		Currency         string
	}
	var refunds []refundRow
	if err := tx.Table("refunds r").Select("r.id, r.order_id, r.provider_refund_no, r.amount, r.currency").Joins("JOIN payment_intents pi ON pi.id = r.payment_intent_id AND pi.deleted_at IS NULL").Where("pi.channel_id = ? AND r.currency = ? AND r.status = ? AND r.processed_at >= ? AND r.processed_at < ? AND r.deleted_at IS NULL", channelID, currencyCode, "succeeded", periodFrom, periodTo).Limit(maximumReconciliationRows + 1).Scan(&refunds).Error; err != nil {
		return nil, err
	}
	if len(refunds) > maximumReconciliationRows {
		return nil, errors.New("refund period is too broad")
	}
	type exceptionalPaymentRow struct {
		ID              uuid.UUID
		OrderID         uuid.UUID
		ProviderTradeNo string
		Amount          int64
		Currency        string
	}
	var exceptionalPayments []exceptionalPaymentRow
	if err := tx.Table("payment_transactions pt").
		Select("pt.id, pi.order_id, pi.provider_trade_no, pt.amount, pt.currency").
		Joins("JOIN payment_intents pi ON pi.id = pt.payment_intent_id AND pi.deleted_at IS NULL").
		Where("pi.channel_id = ? AND pt.direction = ? AND pt.currency = ? AND pt.status = ? AND pi.succeeded_at >= ? AND pi.succeeded_at < ? AND pt.deleted_at IS NULL", channelID, "payment", currencyCode, "requires_refund", periodFrom, periodTo).
		Limit(maximumReconciliationRows + 1).Scan(&exceptionalPayments).Error; err != nil {
		return nil, err
	}
	if len(exceptionalPayments) > maximumReconciliationRows {
		return nil, errors.New("exceptional payment period is too broad")
	}
	type rechargePaymentRow struct {
		ID              uuid.UUID
		RechargeOrderID uuid.UUID
		ProviderTradeNo string
		Amount          int64
		Currency        string
	}
	var rechargePayments []rechargePaymentRow
	if err := tx.Table("recharge_transactions rt").
		Select("rt.id, rt.recharge_order_id, rt.provider_trade_no, rt.amount, rt.currency").
		Joins("JOIN recharge_orders ro ON ro.id = rt.recharge_order_id AND ro.deleted_at IS NULL").
		Where("ro.channel_id = ? AND rt.currency = ? AND rt.status = ? AND rt.paid_at >= ? AND rt.paid_at < ? AND rt.deleted_at IS NULL", channelID, currencyCode, "succeeded", periodFrom, periodTo).
		Limit(maximumReconciliationRows + 1).Scan(&rechargePayments).Error; err != nil {
		return nil, err
	}
	if len(rechargePayments) > maximumReconciliationRows {
		return nil, errors.New("recharge payment period is too broad")
	}
	type rechargeRefundRow struct {
		ID               uuid.UUID
		RechargeOrderID  uuid.UUID
		ProviderRefundNo string
		Amount           int64
		Currency         string
	}
	var rechargeRefunds []rechargeRefundRow
	if err := tx.Table("recharge_transactions rt").
		Select("rt.id, rt.recharge_order_id, rt.provider_refund_no, rt.amount, rt.currency").
		Joins("JOIN recharge_orders ro ON ro.id = rt.recharge_order_id AND ro.deleted_at IS NULL").
		Where("ro.channel_id = ? AND rt.currency = ? AND rt.disposition = ? AND rt.refunded_at >= ? AND rt.refunded_at < ? AND rt.deleted_at IS NULL", channelID, currencyCode, "refunded", periodFrom, periodTo).
		Limit(maximumReconciliationRows + 1).Scan(&rechargeRefunds).Error; err != nil {
		return nil, err
	}
	if len(rechargeRefunds) > maximumReconciliationRows {
		return nil, errors.New("recharge refund period is too broad")
	}
	rowCount := len(payments) + len(refunds) + len(exceptionalPayments) + len(rechargePayments) + len(rechargeRefunds)
	if rowCount > maximumReconciliationRows {
		return nil, errors.New("reconciliation period is too broad")
	}
	records := make([]reconciliationSystemRecord, 0, rowCount)
	for _, payment := range payments {
		tradeNo := payment.ProviderTradeNo
		if tradeNo == "" {
			tradeNo = "INTERNAL-PAYMENT-" + payment.ID.String()
		}
		records = append(records, reconciliationSystemRecord{Key: "payment:" + tradeNo, Direction: "payment", ProviderTradeNo: tradeNo, Currency: payment.Currency, OrderID: payment.OrderID, Amount: payment.Amount})
	}
	for _, refund := range refunds {
		tradeNo := refund.ProviderRefundNo
		if tradeNo == "" {
			tradeNo = "INTERNAL-REFUND-" + refund.ID.String()
		}
		records = append(records, reconciliationSystemRecord{Key: "refund:" + tradeNo, Direction: "refund", ProviderTradeNo: tradeNo, Currency: refund.Currency, OrderID: refund.OrderID, Amount: refund.Amount})
	}
	for _, payment := range exceptionalPayments {
		tradeNo := payment.ProviderTradeNo
		if tradeNo == "" {
			tradeNo = "INTERNAL-EXCEPTIONAL-PAYMENT-" + payment.ID.String()
		}
		records = append(records, reconciliationSystemRecord{Key: "payment:" + tradeNo, Direction: "payment", ProviderTradeNo: tradeNo, Currency: payment.Currency, OrderID: payment.OrderID, Amount: payment.Amount})
	}
	for _, payment := range rechargePayments {
		tradeNo := payment.ProviderTradeNo
		if tradeNo == "" {
			tradeNo = "INTERNAL-RECHARGE-PAYMENT-" + payment.ID.String()
		}
		records = append(records, reconciliationSystemRecord{Key: "payment:" + tradeNo, Direction: "payment", ProviderTradeNo: tradeNo, Currency: payment.Currency, OrderID: payment.RechargeOrderID, Amount: payment.Amount})
	}
	for _, refund := range rechargeRefunds {
		tradeNo := refund.ProviderRefundNo
		if tradeNo == "" {
			tradeNo = "INTERNAL-RECHARGE-REFUND-" + refund.ID.String()
		}
		records = append(records, reconciliationSystemRecord{Key: "refund:" + tradeNo, Direction: "refund", ProviderTradeNo: tradeNo, Currency: refund.Currency, OrderID: refund.RechargeOrderID, Amount: refund.Amount})
	}
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		if _, duplicate := seen[record.Key]; duplicate {
			return nil, fmt.Errorf("duplicate system reconciliation identity %s", record.Key)
		}
		seen[record.Key] = struct{}{}
	}
	return records, nil
}

func (h Handler) ReconciliationDetail(c *gin.Context) {
	batchID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42281, "error.reconciliation_batch_id_invalid")
		return
	}
	var batch model.ReconciliationBatch
	if h.DB.First(&batch, "id = ?", batchID).Error != nil {
		response.Error(c, 404, 40468, "error.reconciliation_batch_not_found")
		return
	}
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.ReconciliationItem{}).Where("batch_id = ?", batch.ID)
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		query = query.Where("status = ?", status)
	}
	var total int64
	var items []model.ReconciliationItem
	if err := query.Count(&total).Error; err != nil || query.Order("created_at ASC").Offset((page-1)*pageSize).Limit(pageSize).Find(&items).Error != nil {
		response.Error(c, 500, 50072, "error.reconciliation_detail_fetch_failed")
		return
	}
	response.OK(c, gin.H{"batch": batch, "items": items, "total": total, "page": page, "page_size": pageSize})
}

type reconciliationResolutionRequest struct {
	ResolutionCode string `json:"resolution_code"`
	Resolution     string `json:"resolution"`
}

func (h Handler) ResolveReconciliationItem(c *gin.Context) {
	itemID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42282, "error.reconciliation_detail_id_invalid")
		return
	}
	adminID, err := uuid.Parse(c.GetString("subject"))
	if err != nil {
		response.Error(c, 401, 40140, "error.invalid_login_state")
		return
	}
	reason, ok := requireAdminChangeReason(c, "处理对账差异")
	if !ok {
		return
	}
	var req reconciliationResolutionRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42283, "error.discrepancy_resolution_invalid")
		return
	}
	req.ResolutionCode = strings.ToLower(strings.TrimSpace(req.ResolutionCode))
	req.Resolution = strings.TrimSpace(req.Resolution)
	allowed := map[string]bool{"accepted_provider": true, "accepted_system": true, "refund_created": true, "adjusted": true, "provider_dispute": true}
	if !allowed[req.ResolutionCode] || utf8.RuneCountInString(req.Resolution) < 4 || utf8.RuneCountInString(req.Resolution) > 2000 {
		response.Error(c, 422, 42283, "error.discrepancy_resolution_evidence_required")
		return
	}
	var item model.ReconciliationItem
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", itemID).Error; err != nil {
			return err
		}
		if item.Status == "matched" || item.Status == "resolved" {
			return errors.New("item already final")
		}
		now := time.Now()
		if err := tx.Model(&item).Updates(map[string]any{"status": "resolved", "resolution_code": req.ResolutionCode, "resolution": req.Resolution, "resolved_by": &adminID, "resolved_at": &now}).Error; err != nil {
			return err
		}
		return refreshReconciliationBatch(tx, item.BatchID)
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40469, "error.reconciliation_detail_not_found")
		return
	}
	if err != nil {
		response.Error(c, 409, 40971, "error.reconciliation_difference_already_processed")
		return
	}
	h.audit(c, "reconciliation.item.resolve", "reconciliation_item", item.ID.String(), reason+"；resolution="+req.ResolutionCode)
	response.OK(c, gin.H{"id": item.ID, "status": "resolved"})
}

func refreshReconciliationBatch(tx *gorm.DB, batchID uuid.UUID) error {
	type counts struct {
		Total      int64
		Matched    int64
		Resolved   int64
		Unresolved int64
	}
	var result counts
	if err := tx.Model(&model.ReconciliationItem{}).Where("batch_id = ?", batchID).Select("COUNT(*) AS total, COUNT(*) FILTER (WHERE status = 'matched') AS matched, COUNT(*) FILTER (WHERE status = 'resolved') AS resolved, COUNT(*) FILTER (WHERE status NOT IN ('matched', 'resolved')) AS unresolved").Scan(&result).Error; err != nil {
		return err
	}
	status := "completed"
	if result.Unresolved > 0 {
		status = "differences_found"
	}
	now := time.Now()
	return tx.Model(&model.ReconciliationBatch{}).Where("id = ?", batchID).Updates(map[string]any{"total": result.Total, "matched": result.Matched, "resolved": result.Resolved, "mismatched": result.Unresolved, "status": status, "completed_at": &now}).Error
}
