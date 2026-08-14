package handler

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"sort"
	"strconv"
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

var (
	errManualOrderReferenceConflict = errors.New("manual payment reference belongs to another order")
	errManualOrderUserInactive      = errors.New("manual order customer is inactive")
)

var manualPaymentChannelID = uuid.MustParse("019c5a19-e975-7a46-93f2-9f45149609e2")

type adminManualOrderRequest struct {
	ProductID        string                      `json:"product_id"`
	VariantID        string                      `json:"variant_id"`
	Quantity         int                         `json:"quantity"`
	Email            string                      `json:"email"`
	PaymentReference string                      `json:"payment_reference"`
	InputValues      []checkoutInputValueRequest `json:"input_values"`
}

type adminManualOrderItemDTO struct {
	ProductID   uuid.UUID  `json:"product_id"`
	VariantID   *uuid.UUID `json:"variant_id,omitempty"`
	VariantName string     `json:"variant_name,omitempty"`
	ProductName string     `json:"product_name"`
	UnitPrice   int64      `json:"unit_price"`
	Currency    string     `json:"currency"`
	Quantity    int        `json:"quantity"`
}

type adminManualOrderDTO struct {
	ID            uuid.UUID                 `json:"id"`
	OrderNo       string                    `json:"order_no"`
	UserID        *uuid.UUID                `json:"user_id,omitempty"`
	Email         string                    `json:"email"`
	Status        string                    `json:"status"`
	PaymentStatus string                    `json:"payment_status"`
	Subtotal      int64                     `json:"subtotal"`
	Discount      int64                     `json:"discount"`
	Total         int64                     `json:"total"`
	Currency      string                    `json:"currency"`
	PaymentMethod string                    `json:"payment_method"`
	PaidAt        *time.Time                `json:"paid_at,omitempty"`
	DeliveredAt   *time.Time                `json:"delivered_at,omitempty"`
	Items         []adminManualOrderItemDTO `json:"items"`
	CreatedAt     time.Time                 `json:"created_at"`
}

func toAdminManualOrderDTO(order model.Order) adminManualOrderDTO {
	items := make([]adminManualOrderItemDTO, 0, len(order.Items))
	for _, item := range order.Items {
		items = append(items, adminManualOrderItemDTO{
			ProductID: item.ProductID, VariantID: item.VariantID, VariantName: item.VariantName,
			ProductName: item.ProductName, UnitPrice: item.UnitPrice, Currency: item.Currency, Quantity: item.Quantity,
		})
	}
	return adminManualOrderDTO{
		ID: order.ID, OrderNo: order.OrderNo, UserID: order.UserID, Email: order.Email,
		Status: order.Status, PaymentStatus: order.PaymentStatus, Subtotal: order.Subtotal,
		Discount: order.Discount, Total: order.Total, Currency: order.Currency, PaymentMethod: order.PaymentMethod,
		PaidAt: order.PaidAt, DeliveredAt: order.DeliveredAt, Items: items, CreatedAt: order.CreatedAt,
	}
}

func (request *adminManualOrderRequest) normalizeAndValidate() (*uuid.UUID, *uuid.UUID, error) {
	request.Email = strings.ToLower(strings.TrimSpace(request.Email))
	address, err := mail.ParseAddress(request.Email)
	if err != nil || !strings.EqualFold(address.Address, request.Email) || len(request.Email) > 190 {
		return nil, nil, errors.New("invalid email")
	}
	productID, err := uuid.Parse(strings.TrimSpace(request.ProductID))
	if err != nil || request.Quantity < 1 || request.Quantity > 20 {
		return nil, nil, errors.New("invalid product or quantity")
	}
	var variantID *uuid.UUID
	if raw := strings.TrimSpace(request.VariantID); raw != "" {
		parsed, parseErr := uuid.Parse(raw)
		if parseErr != nil {
			return nil, nil, errors.New("invalid variant")
		}
		variantID = &parsed
	}
	request.PaymentReference = strings.TrimSpace(request.PaymentReference)
	if len([]rune(request.PaymentReference)) < 4 || len([]rune(request.PaymentReference)) > 160 || strings.IndexFunc(request.PaymentReference, unicode.IsControl) >= 0 {
		return nil, nil, errors.New("invalid payment reference")
	}
	return &productID, variantID, nil
}

func ensureManualPaymentChannel(tx *gorm.DB) (model.PaymentChannel, error) {
	channel := model.PaymentChannel{
		Base: model.Base{ID: manualPaymentChannelID}, Name: "线下人工收款", Code: "manual_offline",
		Provider: "internal_manual", Enabled: false,
	}
	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "code"}},
		DoUpdates: clause.Assignments(map[string]any{
			"name": channel.Name, "provider": channel.Provider, "enabled": false, "deleted_at": nil,
		}),
	}).Create(&channel).Error; err != nil {
		return model.PaymentChannel{}, err
	}
	if err := tx.Where("code = ?", channel.Code).First(&channel).Error; err != nil {
		return model.PaymentChannel{}, err
	}
	return channel, nil
}

func manualOrderMatches(order model.Order, request adminManualOrderRequest, productID uuid.UUID, variantID *uuid.UUID) bool {
	if !strings.EqualFold(order.Email, request.Email) || len(order.Items) == 0 {
		return false
	}
	quantity := 0
	for _, item := range order.Items {
		if item.ProductID != productID || (item.VariantID == nil) != (variantID == nil) {
			return false
		}
		if variantID != nil && *item.VariantID != *variantID {
			return false
		}
		quantity += item.Quantity
	}
	return quantity == request.Quantity
}

func (h Handler) loadManualOrderByReference(db *gorm.DB, reference string, request adminManualOrderRequest, productID uuid.UUID, variantID *uuid.UUID, inputValues []service.SubmittedInputValue) (model.Order, string, bool, error) {
	var intent model.PaymentIntent
	manualChannelIDs := db.Model(&model.PaymentChannel{}).Select("id").Where("code = ? AND provider = ?", "manual_offline", "internal_manual")
	if err := db.Where("channel_id IN (?) AND provider_trade_no = ?", manualChannelIDs, reference).First(&intent).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.Order{}, "", false, nil
		}
		return model.Order{}, "", false, err
	}
	var order model.Order
	if err := db.Preload("Items").First(&order, "id = ?", intent.OrderID).Error; err != nil {
		return model.Order{}, "", false, err
	}
	if !manualOrderMatches(order, request, productID, variantID) || !service.OrderInputValuesMatch(db, h.Vault, order.ID, inputValues) {
		return model.Order{}, "", false, errManualOrderReferenceConflict
	}
	lookupToken, err := h.Vault.Decrypt(order.LookupTokenCipher, order.LookupTokenNonce, append(order.ID[:], []byte("lookup-token")...))
	if err != nil {
		return model.Order{}, "", false, err
	}
	return order, lookupToken, true, nil
}

// CreateAdminManualOrder records a verified offline payment and then uses the
// same pricing, encrypted inventory allocation, supplier routing and delivery
// outbox as a normal paid order. The payment reference is the idempotency key:
// exact retries return the original order while conflicting payloads fail.
func (h Handler) CreateAdminManualOrder(c *gin.Context) {
	reason, ok := requireAdminChangeReason(c, "线下收款手工建单")
	if !ok {
		return
	}
	var request adminManualOrderRequest
	if decodeStrictJSON(c, &request) != nil {
		response.Error(c, 422, 42248, "error.manual_order_fields_invalid")
		return
	}
	productIDPointer, variantID, err := request.normalizeAndValidate()
	if err != nil {
		response.Error(c, 422, 42248, "error.manual_order_fields_invalid")
		return
	}
	productID := *productIDPointer
	inputValues, inputErr := parseSingleOrderInputValues(h.DB, productID, variantID, request.InputValues, nil)
	if inputErr != nil {
		response.Error(c, 422, 42253, "error.product_input_values_invalid")
		return
	}
	if existing, lookupToken, found, lookupErr := h.loadManualOrderByReference(h.DB, request.PaymentReference, request, productID, variantID, inputValues); lookupErr != nil {
		if errors.Is(lookupErr, errManualOrderReferenceConflict) {
			response.Error(c, 409, 40944, "error.manual_order_reference_conflict")
		} else {
			response.Error(c, 500, 50044, "error.manual_order_create_failed")
		}
		return
	} else if found {
		c.Header("Cache-Control", "no-store")
		response.OK(c, gin.H{"order": toAdminManualOrderDTO(existing), "lookup_token": lookupToken, "replayed": true})
		return
	}

	adminID, _ := uuid.Parse(c.GetString("subject"))
	var order *model.Order
	lookupToken := ""
	replayed := false
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		channel, channelErr := ensureManualPaymentChannel(tx)
		if channelErr != nil {
			return channelErr
		}
		// Serialise the entire inventory allocation by payment reference. The
		// second concurrent request waits for the first transaction, then
		// returns its order instead of observing temporarily locked stock.
		if lockErr := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 20260809))", request.PaymentReference).Error; lockErr != nil {
			return lockErr
		}
		if existing, replayToken, found, lookupErr := h.loadManualOrderByReference(tx, request.PaymentReference, request, productID, variantID, inputValues); lookupErr != nil {
			return lookupErr
		} else if found {
			order, lookupToken, replayed = &existing, replayToken, true
			return nil
		}
		var userID *uuid.UUID
		var user model.User
		if findErr := tx.Where("email = ?", request.Email).First(&user).Error; findErr == nil {
			if user.Status != "active" {
				return errManualOrderUserInactive
			}
			userID = &user.ID
		} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		created, createErr := service.CreateOrder(tx, h.Vault, service.CreateOrderInput{
			ProductID: productID, VariantID: variantID, UserID: userID, Quantity: request.Quantity,
			Email: request.Email, PaymentMethod: channel.Code, ClientIP: c.ClientIP(), InputValues: inputValues,
		})
		if createErr != nil {
			return createErr
		}
		order = created
		lookupToken = created.LookupToken
		now := time.Now().UTC()
		intent := model.PaymentIntent{
			Base: model.Base{ID: uuid.New()}, OrderID: created.ID,
			IntentNo:  "LQMI-" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")),
			ChannelID: channel.ID, Amount: created.Total, Currency: created.Currency, Status: "succeeded",
			OrderAmount: created.Total, OrderCurrency: created.Currency,
			ProviderTradeNo: request.PaymentReference, ExpiresAt: now, SucceededAt: &now,
		}
		if createErr := tx.Create(&intent).Error; createErr != nil {
			return createErr
		}
		hash := sha256.Sum256([]byte(request.PaymentReference))
		transaction := model.PaymentTransaction{
			PaymentIntentID: intent.ID, Direction: "capture",
			ProviderEventID: "manual-offline:" + hex.EncodeToString(hash[:]), Amount: created.Total,
			Currency: created.Currency, Status: "succeeded", RawPayload: `{}`,
		}
		if createErr := tx.Create(&transaction).Error; createErr != nil {
			return createErr
		}
		return tx.Create(&model.OrderEvent{
			OrderID: created.ID, ToStatus: created.Status, ActorType: "admin", ActorID: &adminID,
			Reason: "线下收款已核验；" + reason,
		}).Error
	})
	if err != nil {
		if existing, replayToken, found, lookupErr := h.loadManualOrderByReference(h.DB, request.PaymentReference, request, productID, variantID, inputValues); lookupErr == nil && found {
			c.Header("Cache-Control", "no-store")
			response.OK(c, gin.H{"order": toAdminManualOrderDTO(existing), "lookup_token": replayToken, "replayed": true})
			return
		}
		switch {
		case errors.Is(err, errManualOrderReferenceConflict):
			response.Error(c, 409, 40944, "error.manual_order_reference_conflict")
		case errors.Is(err, errManualOrderUserInactive):
			response.Error(c, 409, 40944, "error.manual_order_user_inactive")
		case errors.Is(err, service.ErrInsufficientStock):
			response.Error(c, 409, 40901, "error.partial_insufficient_stock")
		case isOrderInputValidationError(err):
			response.Error(c, 422, 42253, "error.product_input_values_invalid_or_required")
		case errors.Is(err, service.ErrProductUnavailable), errors.Is(err, service.ErrVariantRequired), errors.Is(err, service.ErrVariantUnavailable):
			response.Error(c, 422, 42243, "error.valid_spec_and_quantity_required")
		default:
			response.Error(c, 500, 50044, "error.manual_order_create_failed")
		}
		return
	}
	if replayed {
		c.Header("Cache-Control", "no-store")
		response.OK(c, gin.H{"order": toAdminManualOrderDTO(*order), "lookup_token": lookupToken, "replayed": true})
		return
	}
	h.audit(c, "order.manual-create", "order", order.ID.String(), reason+"；payment_reference="+request.PaymentReference)
	if order.Status == "processing" {
		_ = h.enqueueSupplierOrder(order.ID)
	} else if order.Status == "delivered" {
		_ = h.dispatchOrderDelivery(order.ID)
	}
	c.Header("Cache-Control", "no-store")
	response.Created(c, gin.H{"order": toAdminManualOrderDTO(*order), "lookup_token": lookupToken, "replayed": false})
}

func applyAdminOrderFilters(query *gorm.DB, c *gin.Context) *gorm.DB {
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		query = query.Where("status = ?", status)
	}
	if paymentStatus := strings.TrimSpace(c.Query("payment_status")); paymentStatus != "" {
		query = query.Where("payment_status = ?", paymentStatus)
	}
	if paymentMethod := strings.TrimSpace(c.Query("payment_method")); paymentMethod != "" {
		query = query.Where("payment_method = ?", paymentMethod)
	}
	if resellerID, err := uuid.Parse(strings.TrimSpace(c.Query("reseller_id"))); err == nil {
		query = query.Where("reseller_id = ?", resellerID)
	}
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + strings.ToLower(keyword) + "%"
		query = query.Where("LOWER(order_no) LIKE ? OR LOWER(email) LIKE ?", like, like)
	}
	if createdFrom, err := time.Parse(time.RFC3339, strings.TrimSpace(c.Query("created_from"))); err == nil {
		query = query.Where("created_at >= ?", createdFrom)
	}
	if createdTo, err := time.Parse(time.RFC3339, strings.TrimSpace(c.Query("created_to"))); err == nil {
		query = query.Where("created_at < ?", createdTo)
	}
	return query
}

func protectCSVCell(value string) string {
	value = strings.ReplaceAll(value, "\x00", "")
	if value != "" && strings.ContainsRune("=+-@\t\r", rune(value[0])) {
		return "'" + value
	}
	return value
}

func formatCSVAmount(amount int64, minorUnit int) string {
	if minorUnit < 0 || minorUnit > 6 {
		return strconv.FormatInt(amount, 10)
	}
	raw := strconv.FormatInt(amount, 10)
	sign := ""
	if strings.HasPrefix(raw, "-") {
		sign, raw = "-", strings.TrimPrefix(raw, "-")
	}
	if minorUnit == 0 {
		return sign + raw
	}
	for len(raw) <= minorUnit {
		raw = "0" + raw
	}
	cut := len(raw) - minorUnit
	return sign + raw[:cut] + "." + raw[cut:]
}

// ExportAdminOrders produces a bounded, audited CSV without any lookup token,
// IP address or delivery secret. Formula-like cells are escaped for spreadsheet
// clients to avoid CSV injection.
func (h Handler) ExportAdminOrders(c *gin.Context) {
	reason, ok := requireAdminChangeReason(c, "导出订单")
	if !ok {
		return
	}
	var orders []model.Order
	query := applyAdminOrderFilters(h.DB.Model(&model.Order{}), c).
		Select("id", "order_no", "email", "status", "payment_status", "total", "currency", "payment_method", "created_at").
		Order("created_at DESC").Limit(50001)
	if err := query.Find(&orders).Error; err != nil {
		response.Error(c, 500, 50040, "error.order_export_fetch_failed")
		return
	}
	truncated := len(orders) > 50000
	if truncated {
		orders = orders[:50000]
	}
	currencyCodes := make([]string, 0)
	seenCurrencies := map[string]bool{}
	for _, order := range orders {
		if !seenCurrencies[order.Currency] {
			seenCurrencies[order.Currency] = true
			currencyCodes = append(currencyCodes, order.Currency)
		}
	}
	var currencyDefinitions []model.CurrencyDefinition
	if len(currencyCodes) > 0 && h.DB.Where("code IN ?", currencyCodes).Find(&currencyDefinitions).Error != nil {
		response.Error(c, 500, 50040, "error.currency_fetch_failed")
		return
	}
	minorUnits := make(map[string]int, len(currencyDefinitions))
	for _, definition := range currencyDefinitions {
		minorUnits[definition.Code] = definition.MinorUnit
	}
	if len(minorUnits) != len(currencyCodes) {
		response.Error(c, 500, 50040, "error.currency_definition_missing")
		return
	}
	h.audit(c, "order.export", "order", "", fmt.Sprintf("%s；rows=%d；truncated=%t", reason, len(orders), truncated))
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="linlinqi-orders-`+time.Now().UTC().Format("20060102T150405Z")+`.csv"`)
	c.Header("Cache-Control", "no-store")
	if truncated {
		c.Header("X-Export-Truncated", "true")
	}
	c.Status(http.StatusOK)
	_, _ = c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(c.Writer)
	_ = writer.Write([]string{"订单号", "客户邮箱", "订单状态", "支付状态", "实付金额", "币种", "支付方式", "下单时间"})
	for _, order := range orders {
		_ = writer.Write([]string{
			protectCSVCell(order.OrderNo), protectCSVCell(order.Email), order.Status,
			order.PaymentStatus, formatCSVAmount(order.Total, minorUnits[order.Currency]), order.Currency, protectCSVCell(order.PaymentMethod),
			order.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	writer.Flush()
}

var adminManualOrderTransitions = map[string][]string{
	"pending_payment": {"cancelled", "expired"},
	"pending":         {"risk_review", "cancelled", "expired"},
	"risk_review":     {"pending", "cancelled"},
	"paid":            {"processing"},
	"failed":          {"processing"},
	"delivered":       {"completed"},
}

func allowedAdminOrderTransitions(status string) []string {
	values := append([]string(nil), adminManualOrderTransitions[status]...)
	sort.Strings(values)
	return values
}

// AdminOrderDetail assembles an operational timeline without exposing card
// ciphertext, lookup credentials, connector payloads or decrypted delivery
// content. Sensitive delivery content remains available only to the owner via
// the normal authenticated order flow.
func (h Handler) AdminOrderDetail(c *gin.Context) {
	orderID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42246, "error.order_number_invalid")
		return
	}
	var order model.Order
	if err := h.DB.Preload("Items").First(&order, "id = ?", orderID).Error; err != nil {
		response.Error(c, 404, 40440, "error.order_not_found")
		return
	}
	var events []model.OrderEvent
	var intents []model.PaymentIntent
	var refunds []model.Refund
	var fulfillment []model.FulfillmentAttempt
	var procurements []model.ProcurementOrder
	var riskDecisions []model.RiskDecision
	inputValues, inputErr := service.MaskedOrderInputValues(h.DB, order.ID)
	if inputErr != nil {
		response.Error(c, 500, 50045, "error.order_input_values_fetch_failed")
		return
	}
	h.DB.Where("order_id = ?", order.ID).Order("created_at ASC").Find(&events)
	h.DB.Where("order_id = ?", order.ID).Order("created_at DESC").Find(&intents)
	h.DB.Where("order_id = ?", order.ID).Order("created_at DESC").Find(&refunds)
	h.DB.Where("order_id = ?", order.ID).Order("created_at DESC").Find(&fulfillment)
	h.DB.Where("order_id = ?", order.ID).Order("created_at DESC").Find(&procurements)
	h.DB.Where("order_id = ?", order.ID).Order("created_at DESC").Find(&riskDecisions)
	intentIDs := make([]uuid.UUID, 0, len(intents))
	for _, intent := range intents {
		intentIDs = append(intentIDs, intent.ID)
	}
	var transactions []model.PaymentTransaction
	if len(intentIDs) > 0 {
		h.DB.Where("payment_intent_id IN ?", intentIDs).Order("created_at DESC").Find(&transactions)
	}
	var committedRefund int64
	h.DB.Model(&model.Refund{}).
		Where("order_id = ? AND status IN ?", order.ID, []string{"pending", "processing", "retrying", "succeeded"}).
		Select("COALESCE(SUM(order_amount), 0)").Scan(&committedRefund)
	refundable := order.Total - committedRefund
	if refundable < 0 {
		refundable = 0
	}
	response.OK(c, gin.H{
		"order":                order,
		"events":               events,
		"payment_intents":      intents,
		"payment_transactions": transactions,
		"refunds":              refunds,
		"fulfillment_attempts": fulfillment,
		"procurements":         procurements,
		"risk_decisions":       riskDecisions,
		"input_values":         inputValues,
		"allowed_transitions":  allowedAdminOrderTransitions(order.Status),
		"refundable_amount":    refundable,
		"can_refund":           refundable > 0 && (order.PaymentStatus == "paid" || order.PaymentStatus == "partially_refunded"),
	})
}

// RevealAdminOrderInputValues is intentionally a separate, audited, no-store
// action. The ordinary order detail response contains masked previews only.
func (h Handler) RevealAdminOrderInputValues(c *gin.Context) {
	orderID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42246, "error.order_number_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "查看订单下单字段")
	if !ok {
		return
	}
	var order model.Order
	if err := h.DB.Select("id", "order_no").First(&order, "id = ?", orderID).Error; err != nil {
		response.Error(c, 404, 40440, "error.order_not_found")
		return
	}
	values, err := service.RevealOrderInputValues(h.DB, h.Vault, order.ID, false)
	if err != nil {
		response.Error(c, 500, 50045, "error.order_input_values_decrypt_failed")
		return
	}
	h.audit(c, "order.input-values.reveal", "order", order.ID.String(), reason)
	c.Header("Cache-Control", "no-store")
	response.OK(c, gin.H{"order_no": order.OrderNo, "input_values": values})
}

func validAdminManualOrderTransition(from, to string) bool {
	to = strings.TrimSpace(to)
	for _, allowed := range adminManualOrderTransitions[from] {
		if to == allowed {
			return true
		}
	}
	return false
}
