package service

import (
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/security"
)

var (
	ErrProductUnavailable       = errors.New("product unavailable")
	ErrInsufficientStock        = errors.New("insufficient stock")
	ErrPendingOrderLimit        = errors.New("too many pending reserved items")
	ErrOrderIdempotencyConflict = errors.New("external order number was reused with different order details")
	ErrCurrencyMismatch         = errors.New("products use different currencies")
)

type CreateOrderInput struct {
	ProductID          uuid.UUID
	VariantID          *uuid.UUID
	PaymentChannelID   *uuid.UUID
	UserID             *uuid.UUID
	Quantity           int
	Email              string
	PaymentMethod      string
	ClientIP           string
	ExternalOrderNo    *string
	APICredentialID    *uuid.UUID
	CallbackEndpointID *uuid.UUID
	ResellerID         *uuid.UUID
	CouponCode         string
	FeeRate            int
	RiskDecisionID     *uuid.UUID
	Currency           string
	FXSnapshotID       *uuid.UUID
	InputValues        []SubmittedInputValue
	StorefrontRequest  *StorefrontOrderIdempotency
}

type StorefrontOrderIdempotency struct {
	IdempotencyHash string
	RequestHash     string
	ClientOrderNo   string
}

func lockStorefrontOrderRequest(tx *gorm.DB, request *StorefrontOrderIdempotency) (*model.Order, bool, error) {
	if request == nil {
		return nil, false, nil
	}
	if len(request.IdempotencyHash) != 64 || len(request.RequestHash) != 64 || len(request.ClientOrderNo) > 100 {
		return nil, false, ErrOrderIdempotencyConflict
	}
	if err := tx.Exec(
		"SELECT pg_advisory_xact_lock(hashtextextended(?, 20260809))",
		"linlinqi-storefront-order:"+request.IdempotencyHash,
	).Error; err != nil {
		return nil, false, err
	}
	var receipt model.StorefrontOrderRequest
	err := tx.Where("idempotency_hash = ?", request.IdempotencyHash).First(&receipt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if receipt.RequestHash != request.RequestHash {
		return nil, false, ErrOrderIdempotencyConflict
	}
	var order model.Order
	if err := tx.Preload("Items").First(&order, "id = ?", receipt.OrderID).Error; err != nil {
		return nil, false, err
	}
	return &order, true, nil
}

func createStorefrontOrderReceipt(tx *gorm.DB, request *StorefrontOrderIdempotency, orderID uuid.UUID) error {
	if request == nil {
		return nil
	}
	receipt := model.StorefrontOrderRequest{
		IdempotencyHash: request.IdempotencyHash,
		RequestHash:     request.RequestHash,
		ClientOrderNo:   strings.TrimSpace(request.ClientOrderNo),
		OrderID:         orderID,
	}
	return tx.Create(&receipt).Error
}

func restoreOrderLookupToken(vault *security.Vault, order *model.Order) error {
	if len(order.LookupTokenCipher) == 0 || len(order.LookupTokenNonce) == 0 {
		return nil
	}
	token, err := vault.Decrypt(order.LookupTokenCipher, order.LookupTokenNonce, append(order.ID[:], []byte("lookup-token")...))
	if err != nil {
		return err
	}
	order.LookupToken = token
	return nil
}

func sameOrderOptionalUUID(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func orderMatchesCreateInput(db *gorm.DB, vault *security.Vault, order model.Order, input CreateOrderInput) bool {
	if !strings.EqualFold(strings.TrimSpace(order.Email), strings.TrimSpace(input.Email)) ||
		order.PaymentMethod != input.PaymentMethod ||
		!sameOrderOptionalUUID(order.APICredentialID, input.APICredentialID) ||
		!sameOrderOptionalUUID(order.UserID, input.UserID) ||
		!sameOrderOptionalUUID(order.CallbackEndpointID, input.CallbackEndpointID) ||
		(strings.TrimSpace(input.Currency) != "" && !strings.EqualFold(order.Currency, input.Currency)) {
		return false
	}
	quantity := 0
	for _, item := range order.Items {
		if item.ProductID != input.ProductID || !sameOrderOptionalUUID(item.VariantID, input.VariantID) {
			return false
		}
		quantity += item.Quantity
	}
	return quantity == input.Quantity && OrderInputValuesMatch(db, vault, order.ID, input.InputValues)
}

type OrderLine struct {
	ProductID uuid.UUID
	VariantID *uuid.UUID
	Quantity  int
}

func normalizedProductCurrency(product model.Product) (string, error) {
	currencyCode := strings.ToUpper(strings.TrimSpace(product.Currency))
	if len(currencyCode) != 3 {
		return "", ErrCurrencyMismatch
	}
	return currencyCode, nil
}

const (
	// Reservations are intentionally longer than the configurable payment
	// timeout.  The order state transition/expiry worker is the source of
	// truth for releasing a hold; the timestamp is only a recovery guard for
	// orphaned rows.
	supplierPendingReservationTTL = 48 * time.Hour
	supplierPaidReservationTTL    = 24 * time.Hour
)

// supplierReservationTTL returns a conservative lease for the order being
// assembled.  It is not used as an availability shortcut: active order rows
// continue to count as held even after the lease, until their saga reaches a
// terminal state.
func supplierReservationTTL(tx *gorm.DB, orderID uuid.UUID) (time.Duration, error) {
	var order model.Order
	if err := tx.Select("id", "status").First(&order, "id = ?", orderID).Error; err != nil {
		return 0, err
	}
	if order.Status == "pending_payment" {
		return supplierPendingReservationTTL, nil
	}
	return supplierPaidReservationTTL, nil
}

// newOrderItemWithPricingSnapshot persists the immutable supplier selection
// and creates a local stock reservation in the same transaction.  The
// supplier_product row is locked before counting holds, so concurrent orders
// cannot both consume the same stale upstream stock observation.
func newOrderItemWithPricingSnapshot(tx *gorm.DB, orderID uuid.UUID, line ResolvedLine, quantity int, resellerMargin int64, reservationTTLOverride ...time.Duration) (model.OrderItem, error) {
	if quantity < 1 {
		return model.OrderItem{}, ErrProductUnavailable
	}
	currencyCode, err := normalizedProductCurrency(line.Product)
	if err != nil {
		return model.OrderItem{}, err
	}
	item := model.OrderItem{Base: model.Base{ID: uuid.New()}, OrderID: orderID, ProductID: line.Product.ID, VariantID: line.VariantID, VariantName: line.VariantName, ProductName: line.Product.Name, UnitPrice: line.Quote.UnitPrice, Currency: currencyCode, PlatformUnitPrice: line.PlatformUnitPrice, ResellerMargin: resellerMargin, Quantity: quantity, ParameterMapping: `{}`}
	if line.Product.InventoryMode != "supplier" {
		return item, nil
	}
	query := tx.Table("product_mappings pm").Select("pm.*").
		Joins("JOIN suppliers s ON s.id = pm.supplier_id AND s.deleted_at IS NULL AND s.status = ?", "active").
		Joins("JOIN supplier_products sp ON sp.supplier_id = pm.supplier_id AND sp.product_id = pm.product_id AND sp.variant_id IS NOT DISTINCT FROM pm.variant_id AND sp.external_id = pm.external_product_id AND sp.deleted_at IS NULL").
		Where("pm.product_id = ? AND pm.deleted_at IS NULL", line.Product.ID)
	if line.VariantID == nil {
		query = query.Where("pm.variant_id IS NULL")
	} else {
		query = query.Where("pm.variant_id = ?", *line.VariantID)
	}
	var mappings []model.ProductMapping
	if err := query.Order("pm.auto_sync_price DESC, sp.external_stock DESC, pm.updated_at DESC, pm.id ASC").Limit(64).Find(&mappings).Error; err != nil {
		return model.OrderItem{}, err
	}
	if len(mappings) == 0 {
		return model.OrderItem{}, ErrProductUnavailable
	}
	ttl := supplierPaidReservationTTL
	if len(reservationTTLOverride) > 0 && reservationTTLOverride[0] > 0 {
		ttl = reservationTTLOverride[0]
	} else {
		resolvedTTL, ttlErr := supplierReservationTTL(tx, orderID)
		if ttlErr != nil {
			return model.OrderItem{}, ttlErr
		}
		ttl = resolvedTTL
	}
	now := time.Now().UTC()
	for index := range mappings {
		mapping := &mappings[index]
		capacityKey := "linlinqi-supplier-stock:" + mapping.SupplierID.String() + ":" + mapping.ExternalProductID
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 20260810))", capacityKey).Error; err != nil {
			return model.OrderItem{}, err
		}
		var upstream model.SupplierProduct
		spQuery := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("supplier_id = ? AND product_id = ? AND external_id = ? AND deleted_at IS NULL", mapping.SupplierID, mapping.ProductID, mapping.ExternalProductID)
		if mapping.VariantID == nil {
			spQuery = spQuery.Where("variant_id IS NULL")
		} else {
			spQuery = spQuery.Where("variant_id = ?", *mapping.VariantID)
		}
		if err := spQuery.First(&upstream).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return model.OrderItem{}, err
		}
		if upstream.ExternalStock < int64(quantity) || upstream.ExternalStock < 0 {
			continue
		}
		// A terminal order with an abandoned reservation must not block future
		// stock. Active orders keep their hold even when ExpiresAt has passed;
		// this avoids a lease timeout causing an over-sell while a procurement is
		// still in flight.
		if err := tx.Exec(`
			UPDATE supplier_inventory_reservations r
			SET status = 'released', released_at = ?, release_reason = 'order terminal recovery', updated_at = ?
			WHERE r.status = 'reserved' AND r.expires_at <= ?
			  AND r.supplier_id = ? AND r.external_product_id = ?
			  AND NOT EXISTS (
				SELECT 1 FROM orders o
				WHERE o.id = r.order_id AND o.deleted_at IS NULL AND o.status IN ('pending_payment','paid','processing')
			  )`, now, now, now, mapping.SupplierID, mapping.ExternalProductID).Error; err != nil {
			return model.OrderItem{}, err
		}
		var held int64
		if err := tx.Table("supplier_inventory_reservations r").
			Select("COALESCE(SUM(r.quantity), 0)").
			Joins("JOIN orders o ON o.id = r.order_id AND o.deleted_at IS NULL").
			Where("r.supplier_id = ? AND r.external_product_id = ? AND r.status = ? AND o.status IN ?", mapping.SupplierID, mapping.ExternalProductID, "reserved", []string{"pending_payment", "paid", "processing"}).
			Scan(&held).Error; err != nil {
			return model.OrderItem{}, err
		}
		if held < 0 || held > upstream.ExternalStock-int64(quantity) {
			continue
		}
		if mapping.LastUpstreamPrice < 0 || len(mapping.LastUpstreamCurrency) != 3 || mapping.LastFXSnapshotID == nil {
			continue
		}
		parameterMapping, mappingErr := DecodeSupplierParameterMapping(mapping.ParameterMapping)
		if mappingErr != nil {
			continue
		}
		canonicalMapping, mappingErr := EncodeSupplierParameterMapping(parameterMapping)
		if mappingErr != nil {
			continue
		}
		item.SupplierID = &mapping.SupplierID
		item.ProductMappingID = &mapping.ID
		item.ExternalProductID = mapping.ExternalProductID
		item.ParameterMapping = string(canonicalMapping)
		item.UpstreamUnitPrice = mapping.LastUpstreamPrice
		item.UpstreamCurrency = strings.ToUpper(mapping.LastUpstreamCurrency)
		item.FXSnapshotID = mapping.LastFXSnapshotID
		reservation := model.SupplierInventoryReservation{
			Base:              model.Base{ID: uuid.New()},
			OrderID:           orderID,
			OrderItemID:       item.ID,
			SupplierID:        mapping.SupplierID,
			SupplierProductID: upstream.ID,
			ProductMappingID:  mapping.ID,
			ExternalProductID: mapping.ExternalProductID,
			Quantity:          quantity,
			Status:            "reserved",
			ExpiresAt:         now.Add(ttl),
		}
		if err := tx.Create(&reservation).Error; err != nil {
			return model.OrderItem{}, err
		}
		return item, nil
	}
	return model.OrderItem{}, ErrProductUnavailable
}

func CreateOrder(db *gorm.DB, vault *security.Vault, input CreateOrderInput) (*model.Order, error) {
	if input.Quantity < 1 || input.Quantity > 20 {
		return nil, fmt.Errorf("quantity must be between 1 and 20")
	}
	if input.ExternalOrderNo != nil {
		var existing model.Order
		if err := db.Preload("Items").Where("external_order_no = ? AND api_credential_id = ?", *input.ExternalOrderNo, input.APICredentialID).First(&existing).Error; err == nil {
			if !orderMatchesCreateInput(db, vault, existing, input) {
				return nil, ErrOrderIdempotencyConflict
			}
			if err := revealOrder(vault, &existing); err != nil {
				return nil, err
			}
			return &existing, nil
		}
	}
	lookupToken, lookupHash, err := newOrderLookupToken(input.APICredentialID == nil)
	if err != nil {
		return nil, err
	}
	var result model.Order
	replayed := false
	err = db.Transaction(func(tx *gorm.DB) error {
		existing, found, err := lockStorefrontOrderRequest(tx, input.StorefrontRequest)
		if err != nil {
			return err
		}
		if found {
			result = *existing
			replayed = true
			return nil
		}
		if input.PaymentChannelID != nil {
			if err := EnsurePaymentChannelAllowedCurrency(tx, *input.PaymentChannelID, []uuid.UUID{input.ProductID}, ""); err != nil {
				return err
			}
		}
		line, err := ResolveLinePricingForReseller(tx, input.ProductID, input.VariantID, input.UserID, input.ResellerID, input.Quantity)
		if err != nil {
			return err
		}
		currencyCode, err := normalizedProductCurrency(line.Product)
		if err != nil {
			return err
		}
		coupon := ResolvedCoupon{}
		if input.ResellerID != nil {
			if strings.TrimSpace(input.CouponCode) != "" {
				return ErrCouponUnavailable
			}
		} else {
			coupon, err = ResolveCoupon(tx, input.CouponCode, input.UserID, input.Email, currencyCode, line.Quote.Total)
			if err != nil {
				return err
			}
		}
		pricing, err := convertSingleOrderPricing(tx, input, line, coupon)
		if err != nil {
			return err
		}
		line = pricing.Line
		now := time.Now()
		result = model.Order{
			Base: model.Base{ID: uuid.New()}, OrderNo: orderNo(now), LookupTokenHash: lookupHash, ExternalOrderNo: input.ExternalOrderNo, APICredentialID: input.APICredentialID, CallbackEndpointID: input.CallbackEndpointID, ResellerID: input.ResellerID, ResellerMargin: pricing.ResellerMargin, UserID: input.UserID, Email: input.Email, Status: "processing", PaymentStatus: "paid",
			Subtotal: pricing.Subtotal, Discount: pricing.Discount, Total: pricing.Total, Currency: pricing.Currency, Adjustments: pricing.Adjustments, FXSnapshotID: pricing.FXSnapshotID,
			PaymentMethod: input.PaymentMethod, PaidAt: &now, ClientIP: input.ClientIP,
		}
		if coupon.Coupon != nil {
			result.CouponID = &coupon.Coupon.ID
		}
		if lookupToken != "" {
			ciphertext, nonce, _, err := vault.Encrypt(lookupToken, append(result.ID[:], []byte("lookup-token")...))
			if err != nil {
				return err
			}
			result.LookupTokenCipher, result.LookupTokenNonce = ciphertext, nonce
		}
		if err := tx.Create(&result).Error; err != nil {
			return err
		}
		if err := createStorefrontOrderReceipt(tx, input.StorefrontRequest, result.ID); err != nil {
			return err
		}
		if err := PersistOrderInputValues(tx, vault, result.ID, []OrderLine{{ProductID: input.ProductID, VariantID: input.VariantID, Quantity: input.Quantity}}, input.InputValues); err != nil {
			return err
		}
		if input.RiskDecisionID != nil {
			if err := LinkCheckoutRisk(tx, *input.RiskDecisionID, result.ID); err != nil {
				return err
			}
		}
		if err := ConsumeCoupon(tx, coupon, result.ID, input.UserID, "consumed"); err != nil {
			return err
		}
		if line.Product.InventoryMode == "supplier" {
			item, err := newOrderItemWithPricingSnapshot(tx, result.ID, line, input.Quantity, line.ResellerMargin)
			if err != nil {
				return err
			}
			if result.FXSnapshotID == nil {
				result.FXSnapshotID = item.FXSnapshotID
			}
			if result.FXSnapshotID != nil {
				if err := tx.Model(&result).Update("fx_snapshot_id", item.FXSnapshotID).Error; err != nil {
					return err
				}
			}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
			return tx.Create(&model.OrderEvent{OrderID: result.ID, ToStatus: "processing", ActorType: "system", Reason: "supplier procurement queued"}).Error
		}
		var cards []model.Card
		if err := availableCards(tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}), line.Product.ID, line.VariantID).
			Limit(input.Quantity).Find(&cards).Error; err != nil {
			return err
		}
		if len(cards) != input.Quantity {
			return ErrInsufficientStock
		}
		for _, card := range cards {
			content, err := vault.Decrypt(card.EncryptedContent, card.Nonce, line.Product.ID[:])
			if err != nil {
				return err
			}
			if err := tx.Model(&model.Card{}).Where("id = ? AND status = ?", card.ID, "available").Updates(map[string]interface{}{
				"status": "sold", "order_id": result.ID, "sold_at": &now,
			}).Error; err != nil {
				return err
			}
			item, err := newOrderItemWithPricingSnapshot(tx, result.ID, line, 1, line.ResellerMargin/int64(input.Quantity))
			if err != nil {
				return err
			}
			item.CardCiphertext, item.CardNonce, item.CardPreview, item.CardContent = card.EncryptedContent, card.Nonce, card.Preview, content
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
		}
		result.Status = "delivered"
		result.DeliveredAt = &now
		if err := tx.Model(&result).Updates(map[string]interface{}{"status": result.Status, "delivered_at": result.DeliveredAt}).Error; err != nil {
			return err
		}
		if err := tx.Model(&line.Product).UpdateColumn("sold_count", gorm.Expr("sold_count + ?", input.Quantity)).Error; err != nil {
			return err
		}
		if err := CreateAffiliateCommissionTx(tx, result, now); err != nil {
			return err
		}
		if err := CreditResellerMarginTx(tx, result); err != nil {
			return err
		}
		if result.UserID != nil {
			_, _, err := ReconcileUserMembershipTx(tx, *result.UserID, now)
			return err
		}
		return nil
	})
	if err != nil {
		if input.ExternalOrderNo != nil {
			var existing model.Order
			if queryErr := db.Preload("Items").Where("external_order_no = ? AND api_credential_id = ?", *input.ExternalOrderNo, input.APICredentialID).First(&existing).Error; queryErr == nil {
				if !orderMatchesCreateInput(db, vault, existing, input) {
					return nil, ErrOrderIdempotencyConflict
				}
				if revealErr := revealOrder(vault, &existing); revealErr != nil {
					return nil, revealErr
				}
				return &existing, nil
			}
		}
		return nil, err
	}
	if replayed {
		if err := revealOrder(vault, &result); err != nil {
			return nil, err
		}
		if err := restoreOrderLookupToken(vault, &result); err != nil {
			return nil, err
		}
		return &result, nil
	}
	if err := db.Preload("Items").First(&result, "id = ?", result.ID).Error; err != nil {
		return nil, err
	}
	if err := revealOrder(vault, &result); err != nil {
		return nil, err
	}
	result.LookupToken = lookupToken
	return &result, nil
}

// CreatePendingOrder atomically reserves encrypted inventory without exposing it.
// A verified payment callback must call FulfillOrder before the order is deliverable.
func CreatePendingOrder(db *gorm.DB, vault *security.Vault, input CreateOrderInput) (*model.Order, error) {
	if input.Quantity < 1 || input.Quantity > 20 {
		return nil, fmt.Errorf("quantity must be between 1 and 20")
	}
	lookupToken, lookupHash, err := newOrderLookupToken(true)
	if err != nil {
		return nil, err
	}
	var result model.Order
	replayed := false
	err = db.Transaction(func(tx *gorm.DB) error {
		existing, found, err := lockStorefrontOrderRequest(tx, input.StorefrontRequest)
		if err != nil {
			return err
		}
		if found {
			result = *existing
			replayed = true
			return nil
		}
		if input.PaymentChannelID != nil {
			if err := EnsurePaymentChannelAllowedCurrency(tx, *input.PaymentChannelID, []uuid.UUID{input.ProductID}, ""); err != nil {
				return err
			}
		}
		if err := enforcePendingReservationQuota(tx, input.UserID, input.Email, input.ClientIP, input.Quantity); err != nil {
			return err
		}
		line, err := ResolveLinePricingForReseller(tx, input.ProductID, input.VariantID, input.UserID, input.ResellerID, input.Quantity)
		if err != nil {
			return err
		}
		currencyCode, err := normalizedProductCurrency(line.Product)
		if err != nil {
			return err
		}
		coupon := ResolvedCoupon{}
		if input.ResellerID != nil {
			if strings.TrimSpace(input.CouponCode) != "" {
				return ErrCouponUnavailable
			}
		} else {
			coupon, err = ResolveCoupon(tx, input.CouponCode, input.UserID, input.Email, currencyCode, line.Quote.Total)
			if err != nil {
				return err
			}
		}
		pricing, err := convertSingleOrderPricing(tx, input, line, coupon)
		if err != nil {
			return err
		}
		line = pricing.Line
		result = model.Order{
			Base: model.Base{ID: uuid.New()}, OrderNo: orderNo(time.Now()), LookupTokenHash: lookupHash, ResellerID: input.ResellerID, ResellerMargin: pricing.ResellerMargin, UserID: input.UserID, Email: input.Email, Status: "pending_payment", PaymentStatus: "pending",
			Subtotal: pricing.Subtotal, Discount: pricing.Discount, Total: pricing.Total, Currency: pricing.Currency, Adjustments: pricing.Adjustments, FXSnapshotID: pricing.FXSnapshotID,
			PaymentMethod: input.PaymentMethod, ClientIP: input.ClientIP,
		}
		if coupon.Coupon != nil {
			result.CouponID = &coupon.Coupon.ID
		}
		ciphertext, nonce, _, err := vault.Encrypt(lookupToken, append(result.ID[:], []byte("lookup-token")...))
		if err != nil {
			return err
		}
		result.LookupTokenCipher, result.LookupTokenNonce = ciphertext, nonce
		if err := tx.Create(&result).Error; err != nil {
			return err
		}
		if err := createStorefrontOrderReceipt(tx, input.StorefrontRequest, result.ID); err != nil {
			return err
		}
		if err := PersistOrderInputValues(tx, vault, result.ID, []OrderLine{{ProductID: input.ProductID, VariantID: input.VariantID, Quantity: input.Quantity}}, input.InputValues); err != nil {
			return err
		}
		if input.RiskDecisionID != nil {
			if err := LinkCheckoutRisk(tx, *input.RiskDecisionID, result.ID); err != nil {
				return err
			}
		}
		if err := ConsumeCoupon(tx, coupon, result.ID, input.UserID, "reserved"); err != nil {
			return err
		}
		if line.Product.InventoryMode == "supplier" {
			item, err := newOrderItemWithPricingSnapshot(tx, result.ID, line, input.Quantity, line.ResellerMargin)
			if err != nil {
				return err
			}
			if result.FXSnapshotID == nil {
				result.FXSnapshotID = item.FXSnapshotID
			}
			if result.FXSnapshotID != nil {
				if err := tx.Model(&result).Update("fx_snapshot_id", item.FXSnapshotID).Error; err != nil {
					return err
				}
			}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
			return tx.Create(&model.OrderEvent{OrderID: result.ID, ToStatus: "pending_payment", ActorType: "system", Reason: "supplier inventory selected"}).Error
		}
		var cards []model.Card
		if err := availableCards(tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}), line.Product.ID, line.VariantID).Limit(input.Quantity).Find(&cards).Error; err != nil {
			return err
		}
		if len(cards) != input.Quantity {
			return ErrInsufficientStock
		}
		for _, card := range cards {
			if err := tx.Model(&model.Card{}).Where("id = ? AND status = ?", card.ID, "available").Updates(map[string]any{"status": "locked", "order_id": result.ID}).Error; err != nil {
				return err
			}
			item, err := newOrderItemWithPricingSnapshot(tx, result.ID, line, 1, line.ResellerMargin/int64(input.Quantity))
			if err != nil {
				return err
			}
			item.CardCiphertext, item.CardNonce, item.CardPreview = card.EncryptedContent, card.Nonce, card.Preview
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
		}
		return tx.Create(&model.OrderEvent{OrderID: result.ID, ToStatus: "pending_payment", ActorType: "system", Reason: "inventory reserved"}).Error
	})
	if err != nil {
		return nil, err
	}
	if replayed {
		if err := restoreOrderLookupToken(vault, &result); err != nil {
			return nil, err
		}
		return &result, nil
	}
	if err := db.Preload("Items").First(&result, "id = ?", result.ID).Error; err != nil {
		return nil, err
	}
	result.LookupToken = lookupToken
	return &result, nil
}

func CreatePendingCartOrder(db *gorm.DB, vault *security.Vault, userID, resellerID *uuid.UUID, email, paymentMethod, clientIP, couponCode, currencyCode string, fxSnapshotID *uuid.UUID, feeRate int, paymentChannelID uuid.UUID, riskDecisionID *uuid.UUID, lines []OrderLine, inputValues []SubmittedInputValue, storefrontRequest *StorefrontOrderIdempotency) (*model.Order, error) {
	aggregated := make(map[string]OrderLine)
	totalQuantity := 0
	for _, line := range lines {
		if line.ProductID == uuid.Nil || line.Quantity < 1 || line.Quantity > 20 {
			return nil, fmt.Errorf("invalid cart line")
		}
		key := line.ProductID.String() + ":"
		if line.VariantID != nil {
			key += line.VariantID.String()
		}
		existing := aggregated[key]
		if existing.ProductID == uuid.Nil {
			existing.ProductID, existing.VariantID = line.ProductID, line.VariantID
		}
		existing.Quantity += line.Quantity
		aggregated[key] = existing
		totalQuantity += line.Quantity
	}
	if len(aggregated) == 0 || totalQuantity > 20 {
		return nil, fmt.Errorf("cart must contain between 1 and 20 items")
	}
	lookupToken, lookupHash, err := newOrderLookupToken(true)
	if err != nil {
		return nil, err
	}
	var result model.Order
	replayed := false
	err = db.Transaction(func(tx *gorm.DB) error {
		existing, found, err := lockStorefrontOrderRequest(tx, storefrontRequest)
		if err != nil {
			return err
		}
		if found {
			result = *existing
			replayed = true
			return nil
		}
		productIDs := make([]uuid.UUID, 0, len(aggregated))
		for _, line := range aggregated {
			productIDs = append(productIDs, line.ProductID)
		}
		if paymentChannelID != uuid.Nil {
			if err := EnsurePaymentChannelAllowedCurrency(tx, paymentChannelID, productIDs, ""); err != nil {
				return err
			}
		}
		if err := enforcePendingReservationQuota(tx, userID, email, clientIP, totalQuantity); err != nil {
			return err
		}
		type pricedLine struct {
			line     ResolvedLine
			quantity int
		}
		keys := make([]string, 0, len(aggregated))
		for key := range aggregated {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		priced := make([]pricedLine, 0, len(keys))
		orderCurrency := ""
		subtotal, lineDiscount := int64(0), int64(0)
		adjustments := make([]PriceAdjustment, 0)
		for _, key := range keys {
			requestLine := aggregated[key]
			resolved, err := ResolveLinePricingForReseller(tx, requestLine.ProductID, requestLine.VariantID, userID, resellerID, requestLine.Quantity)
			if err != nil {
				return err
			}
			lineCurrency, err := normalizedProductCurrency(resolved.Product)
			if err != nil {
				return err
			}
			if orderCurrency == "" {
				orderCurrency = lineCurrency
			} else if lineCurrency != orderCurrency {
				return ErrCurrencyMismatch
			}
			priced = append(priced, pricedLine{line: resolved, quantity: requestLine.Quantity})
			subtotal, err = checkedAddInt64(subtotal, resolved.Quote.Subtotal)
			if err != nil {
				return err
			}
			lineDiscount, err = checkedAddInt64(lineDiscount, resolved.Quote.Discount)
			if err != nil {
				return err
			}
			for _, adjustment := range resolved.Quote.Adjustments {
				adjustment.Label = resolved.Product.Name + " · " + adjustment.Label
				adjustments = append(adjustments, adjustment)
			}
		}
		coupon := ResolvedCoupon{}
		if resellerID != nil {
			if strings.TrimSpace(couponCode) != "" {
				return ErrCouponUnavailable
			}
		} else {
			coupon, err = ResolveCoupon(tx, couponCode, userID, email, orderCurrency, subtotal-lineDiscount)
			if err != nil {
				return err
			}
		}
		conversion, err := LoadCheckoutCurrencyConversion(tx, orderCurrency, currencyCode, fxSnapshotID, time.Now())
		if err != nil {
			return err
		}
		subtotal, lineDiscount, adjustments = 0, 0, make([]PriceAdjustment, 0)
		for index := range priced {
			entry := &priced[index]
			entry.line.Quote, err = conversion.PriceQuote(entry.line.Quote)
			if err != nil {
				return err
			}
			entry.line.PlatformUnitPrice, err = conversion.Amount(entry.line.PlatformUnitPrice)
			if err != nil {
				return err
			}
			entry.line.ResellerMargin, err = conversion.Amount(entry.line.ResellerMargin)
			if err != nil {
				return err
			}
			entry.line.Product.Currency = conversion.Target.Code
			subtotal, err = checkedAddInt64(subtotal, entry.line.Quote.Subtotal)
			if err != nil {
				return err
			}
			lineDiscount, err = checkedAddInt64(lineDiscount, entry.line.Quote.Discount)
			if err != nil {
				return err
			}
			for _, adjustment := range entry.line.Quote.Adjustments {
				adjustment.Label = entry.line.Product.Name + " · " + adjustment.Label
				adjustments = append(adjustments, adjustment)
			}
		}
		couponDiscount, err := conversion.Amount(coupon.Discount)
		if err != nil {
			return err
		}
		if couponDiscount > 0 {
			adjustments = append(adjustments, PriceAdjustment{Code: "coupon", Label: "优惠券", Amount: -couponDiscount})
		}
		netAmount := subtotal - lineDiscount - couponDiscount
		if netAmount < 1 {
			return ErrProductUnavailable
		}
		fee, err := roundedNearestRatio(netAmount, int64(feeRate), 10000)
		if err != nil {
			return err
		}
		total, err := checkedAddInt64(netAmount, fee)
		if err != nil {
			return err
		}
		adjustmentJSON, _ := json.Marshal(adjustments)
		resellerMargin := int64(0)
		for _, entry := range priced {
			resellerMargin, err = checkedAddInt64(resellerMargin, entry.line.ResellerMargin)
			if err != nil {
				return err
			}
		}
		var quoteSnapshotID *uuid.UUID
		if conversion.Snapshot != nil {
			id := conversion.Snapshot.ID
			quoteSnapshotID = &id
		}
		result = model.Order{Base: model.Base{ID: uuid.New()}, OrderNo: orderNo(time.Now()), LookupTokenHash: lookupHash, ResellerID: resellerID, ResellerMargin: resellerMargin, UserID: userID, Email: email, Status: "pending_payment", PaymentStatus: "pending", Subtotal: subtotal, Discount: lineDiscount + couponDiscount, Total: total, Currency: conversion.Target.Code, Adjustments: adjustmentJSON, FXSnapshotID: quoteSnapshotID, PaymentMethod: paymentMethod, ClientIP: clientIP}
		if coupon.Coupon != nil {
			result.CouponID = &coupon.Coupon.ID
		}
		ciphertext, nonce, _, err := vault.Encrypt(lookupToken, append(result.ID[:], []byte("lookup-token")...))
		if err != nil {
			return err
		}
		result.LookupTokenCipher, result.LookupTokenNonce = ciphertext, nonce
		if err := tx.Create(&result).Error; err != nil {
			return err
		}
		if err := createStorefrontOrderReceipt(tx, storefrontRequest, result.ID); err != nil {
			return err
		}
		aggregatedLines := make([]OrderLine, 0, len(keys))
		for _, key := range keys {
			aggregatedLines = append(aggregatedLines, aggregated[key])
		}
		if err := PersistOrderInputValues(tx, vault, result.ID, aggregatedLines, inputValues); err != nil {
			return err
		}
		if riskDecisionID != nil {
			if err := LinkCheckoutRisk(tx, *riskDecisionID, result.ID); err != nil {
				return err
			}
		}
		if err := ConsumeCoupon(tx, coupon, result.ID, userID, "reserved"); err != nil {
			return err
		}
		for _, entry := range priced {
			line := entry.line
			if line.Product.InventoryMode == "supplier" {
				item, err := newOrderItemWithPricingSnapshot(tx, result.ID, line, entry.quantity, line.ResellerMargin)
				if err != nil {
					return err
				}
				if err := tx.Create(&item).Error; err != nil {
					return err
				}
				continue
			}
			var cards []model.Card
			if err := availableCards(tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}), line.Product.ID, line.VariantID).Limit(entry.quantity).Find(&cards).Error; err != nil {
				return err
			}
			if len(cards) != entry.quantity {
				return ErrInsufficientStock
			}
			for _, card := range cards {
				if err := tx.Model(&model.Card{}).Where("id = ? AND status = ?", card.ID, "available").Updates(map[string]any{"status": "locked", "order_id": result.ID}).Error; err != nil {
					return err
				}
				item, err := newOrderItemWithPricingSnapshot(tx, result.ID, line, 1, line.ResellerMargin/int64(entry.quantity))
				if err != nil {
					return err
				}
				item.CardCiphertext, item.CardNonce, item.CardPreview = card.EncryptedContent, card.Nonce, card.Preview
				if err := tx.Create(&item).Error; err != nil {
					return err
				}
			}
		}
		return tx.Create(&model.OrderEvent{OrderID: result.ID, ToStatus: "pending_payment", ActorType: "system", Reason: "cart inventory reserved"}).Error
	})
	if err != nil {
		return nil, err
	}
	if replayed {
		if err := restoreOrderLookupToken(vault, &result); err != nil {
			return nil, err
		}
		return &result, nil
	}
	if err := db.Preload("Items").First(&result, "id = ?", result.ID).Error; err != nil {
		return nil, err
	}
	result.LookupToken = lookupToken
	return &result, nil
}

func FulfillOrder(db *gorm.DB, orderID uuid.UUID) error {
	return db.Transaction(func(tx *gorm.DB) error { return FulfillOrderTx(tx, orderID) })
}

func FulfillOrderTx(tx *gorm.DB, orderID uuid.UUID) error {
	var order model.Order
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", orderID).Error; err != nil {
		return err
	}
	if order.Status == "delivered" || order.Status == "completed" {
		if err := CreateAffiliateCommissionTx(tx, order, time.Now()); err != nil {
			return err
		}
		if err := CreditResellerMarginTx(tx, order); err != nil {
			return err
		}
		if order.UserID != nil {
			_, _, err := ReconcileUserMembershipTx(tx, *order.UserID, time.Now())
			return err
		}
		return nil
	}
	if order.Status != "pending_payment" {
		return fmt.Errorf("order cannot be fulfilled from %s", order.Status)
	}
	if err := CompleteCouponReservation(tx, order.ID); err != nil {
		return err
	}
	var items []model.OrderItem
	if err := tx.Where("order_id = ?", order.ID).Find(&items).Error; err != nil {
		return err
	}
	if len(items) == 0 {
		return ErrInsufficientStock
	}
	productModes := make(map[uuid.UUID]string)
	localItemIDs := make([]uuid.UUID, 0, len(items))
	hasSupplier := false
	for _, item := range items {
		mode, exists := productModes[item.ProductID]
		if !exists {
			var product model.Product
			if err := tx.Select("id", "inventory_mode").First(&product, "id = ?", item.ProductID).Error; err != nil {
				return err
			}
			mode = product.InventoryMode
			productModes[item.ProductID] = mode
		}
		if mode == "supplier" {
			hasSupplier = true
		} else {
			localItemIDs = append(localItemIDs, item.ID)
		}
	}
	var cards []model.Card
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("order_id = ? AND status = ?", order.ID, "locked").Find(&cards).Error; err != nil {
		return err
	}
	if len(cards) != len(localItemIDs) {
		return ErrInsufficientStock
	}
	now := time.Now()
	if hasSupplier {
		if err := ExtendSupplierInventoryReservationsTx(tx, order.ID, supplierPaidReservationTTL); err != nil {
			return err
		}
	}
	if err := tx.Model(&model.Card{}).Where("order_id = ? AND status = ?", order.ID, "locked").Updates(map[string]any{"status": "sold", "sold_at": &now}).Error; err != nil {
		return err
	}
	targetStatus := "delivered"
	updates := map[string]any{"status": targetStatus, "payment_status": "paid", "paid_at": &now, "delivered_at": &now}
	if hasSupplier {
		targetStatus = "processing"
		updates["status"] = targetStatus
		updates["delivered_at"] = nil
	}
	if err := tx.Model(&order).Updates(updates).Error; err != nil {
		return err
	}
	if err := tx.Create(&model.OrderEvent{OrderID: order.ID, FromStatus: order.Status, ToStatus: targetStatus, ActorType: "payment", Reason: "verified provider callback"}).Error; err != nil {
		return err
	}
	var quantities []struct {
		ProductID uuid.UUID
		Quantity  int64
	}
	if len(localItemIDs) > 0 {
		if err := tx.Model(&model.OrderItem{}).Select("product_id, SUM(quantity) AS quantity").Where("id IN ?", localItemIDs).Group("product_id").Scan(&quantities).Error; err != nil {
			return err
		}
	}
	for _, quantity := range quantities {
		if err := tx.Model(&model.Product{}).Where("id = ?", quantity.ProductID).UpdateColumn("sold_count", gorm.Expr("sold_count + ?", quantity.Quantity)).Error; err != nil {
			return err
		}
	}
	if targetStatus == "delivered" {
		order.Status = targetStatus
		order.PaymentStatus = "paid"
		order.PaidAt = &now
		order.DeliveredAt = &now
		if err := CreateAffiliateCommissionTx(tx, order, now); err != nil {
			return err
		}
		if err := CreditResellerMarginTx(tx, order); err != nil {
			return err
		}
		if order.UserID != nil {
			if _, _, err := ReconcileUserMembershipTx(tx, *order.UserID, now); err != nil {
				return err
			}
		}
	}
	return nil
}

// ReleaseSupplierInventoryReservationsTx returns all still-held upstream
// capacity for an order. It is idempotent and deliberately only changes
// `reserved` rows, so a late retry cannot undo a consumed reservation.
func ReleaseSupplierInventoryReservationsTx(tx *gorm.DB, orderID uuid.UUID, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "order terminal"
	}
	if len([]rune(reason)) > 120 {
		reason = string([]rune(reason)[:120])
	}
	now := time.Now().UTC()
	return tx.Model(&model.SupplierInventoryReservation{}).
		Where("order_id = ? AND status = ?", orderID, "reserved").
		Updates(map[string]any{"status": "released", "released_at": &now, "release_reason": reason}).Error
}

// ExtendSupplierInventoryReservationsTx moves pending-payment holds onto the
// paid-order recovery lease. The order status remains the availability source
// of truth; extending the timestamp makes operational inspection and orphan
// recovery accurately reflect the payment transition.
func ExtendSupplierInventoryReservationsTx(tx *gorm.DB, orderID uuid.UUID, ttl time.Duration) error {
	if ttl <= 0 {
		return fmt.Errorf("supplier inventory reservation TTL must be positive")
	}
	expiresAt := time.Now().UTC().Add(ttl)
	return tx.Model(&model.SupplierInventoryReservation{}).
		Where("order_id = ? AND status = ?", orderID, "reserved").
		Update("expires_at", expiresAt).Error
}

// RestoreSupplierInventoryReservationsTx re-acquires capacity before an
// administrator retries a failed supplier order. A failed saga releases every
// outstanding hold, while the retry worker operates on every unfinished line;
// consequently all unfinished supplier lines must be restored atomically.
func RestoreSupplierInventoryReservationsTx(tx *gorm.DB, orderID uuid.UUID) error {
	var order model.Order
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "status", "payment_status").First(&order, "id = ?", orderID).Error; err != nil {
		return err
	}
	if order.PaymentStatus != "paid" || (order.Status != "failed" && order.Status != "processing") {
		return fmt.Errorf("supplier inventory reservations cannot be restored from order state %s/%s", order.Status, order.PaymentStatus)
	}
	var items []model.OrderItem
	if err := tx.Where("order_id = ? AND supplier_id IS NOT NULL AND product_mapping_id IS NOT NULL AND card_ciphertext IS NULL", orderID).
		Order("product_id ASC, variant_id ASC NULLS FIRST, id ASC").Find(&items).Error; err != nil {
		return err
	}
	now := time.Now().UTC()
	for index := range items {
		item := &items[index]
		if item.SupplierID == nil || item.ProductMappingID == nil || strings.TrimSpace(item.ExternalProductID) == "" || item.Quantity < 1 {
			return ErrProductUnavailable
		}
		capacityKey := "linlinqi-supplier-stock:" + item.SupplierID.String() + ":" + item.ExternalProductID
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 20260810))", capacityKey).Error; err != nil {
			return err
		}
		var upstream model.SupplierProduct
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("supplier_id = ? AND product_id = ? AND external_id = ? AND deleted_at IS NULL", *item.SupplierID, item.ProductID, item.ExternalProductID)
		if item.VariantID == nil {
			query = query.Where("variant_id IS NULL")
		} else {
			query = query.Where("variant_id = ?", *item.VariantID)
		}
		if err := query.First(&upstream).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrProductUnavailable
			}
			return err
		}
		var held int64
		if err := tx.Table("supplier_inventory_reservations r").
			Select("COALESCE(SUM(r.quantity), 0)").
			Joins("JOIN orders o ON o.id = r.order_id AND o.deleted_at IS NULL").
			Where("r.supplier_id = ? AND r.external_product_id = ? AND r.status = ? AND o.status IN ? AND r.order_item_id <> ?", *item.SupplierID, item.ExternalProductID, "reserved", []string{"pending_payment", "paid", "processing"}, item.ID).
			Scan(&held).Error; err != nil {
			return err
		}
		if upstream.ExternalStock < int64(item.Quantity) || held < 0 || held > upstream.ExternalStock-int64(item.Quantity) {
			return ErrInsufficientStock
		}
		var reservation model.SupplierInventoryReservation
		err := tx.Where("order_item_id = ?", item.ID).First(&reservation).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			reservation = model.SupplierInventoryReservation{
				Base: model.Base{ID: uuid.New()}, OrderID: order.ID, OrderItemID: item.ID,
				SupplierID: *item.SupplierID, SupplierProductID: upstream.ID, ProductMappingID: *item.ProductMappingID,
				ExternalProductID: item.ExternalProductID, Quantity: item.Quantity, Status: "reserved", ExpiresAt: now.Add(supplierPaidReservationTTL),
			}
			if err := tx.Create(&reservation).Error; err != nil {
				return err
			}
		case err != nil:
			return err
		case reservation.Status == "consumed":
			return fmt.Errorf("supplier inventory reservation was already consumed")
		default:
			updates := map[string]any{
				"status": "reserved", "supplier_id": *item.SupplierID, "supplier_product_id": upstream.ID,
				"product_mapping_id": *item.ProductMappingID, "external_product_id": item.ExternalProductID, "quantity": item.Quantity,
				"expires_at": now.Add(supplierPaidReservationTTL), "consumed_at": nil, "released_at": nil, "release_reason": "",
			}
			if err := tx.Model(&reservation).Updates(updates).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// consumeSupplierInventoryReservationsTx converts matching holds into
// consumed stock. The reservation transition and the observed-stock debit are
// committed together. This is important because availability subtracts only
// active reservations: changing a hold to consumed without lowering the
// current SupplierProduct observation would make the same upstream unit
// sellable again until the next sync.
//
// Capacity advisory locks are the same locks used by checkout and retry
// restoration. They close the race between removing the hold and lowering the
// observation. The product row lock also serializes an authoritative sync that
// is writing a new observation. A later sync deliberately overwrites
// ExternalStock with the newly observed remaining stock, so historical
// consumption is never deducted twice.
func consumeSupplierInventoryReservationsTx(tx *gorm.DB, condition string, args ...any) error {
	var candidates []model.SupplierInventoryReservation
	if err := tx.Where(condition, args...).Where("status = ?", "reserved").
		Order("supplier_id ASC, external_product_id ASC, id ASC").Find(&candidates).Error; err != nil {
		return err
	}
	for index := range candidates {
		candidate := &candidates[index]
		capacityKey := "linlinqi-supplier-stock:" + candidate.SupplierID.String() + ":" + candidate.ExternalProductID
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 20260810))", capacityKey).Error; err != nil {
			return err
		}

		var reservation model.SupplierInventoryReservation
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND status = ?", candidate.ID, "reserved").First(&reservation).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// A concurrent terminal transition or an idempotent delivery retry
			// already owns the lifecycle decision.
			continue
		}
		if err != nil {
			return err
		}

		now := time.Now().UTC()
		transition := tx.Model(&model.SupplierInventoryReservation{}).
			Where("id = ? AND status = ?", reservation.ID, "reserved").
			Updates(map[string]any{"status": "consumed", "consumed_at": &now, "release_reason": ""})
		if transition.Error != nil {
			return transition.Error
		}
		if transition.RowsAffected != 1 {
			return fmt.Errorf("supplier inventory reservation consumption lost atomic claim")
		}

		var supplierProduct model.SupplierProduct
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND supplier_id = ? AND external_id = ?", reservation.SupplierProductID, reservation.SupplierID, reservation.ExternalProductID).
			First(&supplierProduct).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// A disabled/deleted mapping has no executable capacity to expose.
			// Keep the reservation terminal so a late delivery cannot resurrect
			// its hold.
			continue
		}
		if err != nil {
			return err
		}
		debit := tx.Model(&model.SupplierProduct{}).
			Where("id = ?", supplierProduct.ID).
			Updates(map[string]any{
				"external_stock": gorm.Expr("GREATEST(external_stock - ?, 0)", reservation.Quantity),
				"updated_at":     now,
			})
		if debit.Error != nil {
			return debit.Error
		}
		if debit.RowsAffected != 1 {
			return fmt.Errorf("supplier stock observation debit lost atomic claim")
		}
	}
	return nil
}

// ConsumeSupplierInventoryReservationsTx finalizes every local hold when an
// upstream procurement has delivered. Missing rows are allowed for legacy
// orders created before reservations were introduced.
func ConsumeSupplierInventoryReservationsTx(tx *gorm.DB, orderID uuid.UUID) error {
	return consumeSupplierInventoryReservationsTx(tx, "order_id = ?", orderID)
}

// ConsumeSupplierInventoryReservationTx is the per-line form used by carts:
// one completed procurement must not release or consume another line's hold.
func ConsumeSupplierInventoryReservationTx(tx *gorm.DB, orderItemID uuid.UUID) error {
	return consumeSupplierInventoryReservationsTx(tx, "order_item_id = ?", orderItemID)
}

func ExpirePendingOrders(db *gorm.DB, before time.Time) (int64, error) {
	var expired int64
	err := db.Transaction(func(tx *gorm.DB) error {
		var orders []model.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("status = ? AND created_at < ?", "pending_payment", before).Limit(2000).Find(&orders).Error; err != nil {
			return err
		}
		for _, order := range orders {
			if err := ReleaseCouponReservation(tx, order.ID); err != nil {
				return err
			}
			if err := ReleaseSupplierInventoryReservationsTx(tx, order.ID, "payment timeout"); err != nil {
				return err
			}
			if err := tx.Model(&model.Card{}).Where("order_id = ? AND status = ?", order.ID, "locked").Updates(map[string]any{"status": "available", "order_id": nil}).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.OrderItem{}).Where("order_id = ?", order.ID).Updates(map[string]any{"card_ciphertext": nil, "card_nonce": nil, "card_preview": ""}).Error; err != nil {
				return err
			}
			if err := tx.Model(&order).Updates(map[string]any{"status": "expired", "payment_status": "expired"}).Error; err != nil {
				return err
			}
			if err := tx.Create(&model.OrderEvent{OrderID: order.ID, FromStatus: order.Status, ToStatus: "expired", ActorType: "system", Reason: "payment timeout"}).Error; err != nil {
				return err
			}
			expired++
		}
		return nil
	})
	return expired, err
}

func enforcePendingReservationQuota(tx *gorm.DB, userID *uuid.UUID, email, clientIP string, requested int) error {
	keys := []string{"ip:" + clientIP, "email:" + email}
	if userID != nil {
		keys = append(keys, "user:"+userID.String())
	}
	for index := range keys {
		digest := sha256.Sum256([]byte(keys[index]))
		keys[index] = hex.EncodeToString(digest[:])
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "linlinqi-pending:"+key).Error; err != nil {
			return err
		}
	}
	reservedQuantity := func(condition string, value any) (int64, error) {
		var quantity int64
		err := tx.Table("order_items oi").
			Select("COALESCE(SUM(oi.quantity), 0)").
			Joins("JOIN orders o ON o.id = oi.order_id AND o.deleted_at IS NULL").
			Where("oi.deleted_at IS NULL AND o.status = ?", "pending_payment").
			Where(condition, value).Scan(&quantity).Error
		return quantity, err
	}
	limit := int64(20)
	if userID != nil {
		limit = 100
		quantity, err := reservedQuantity("o.user_id = ?", *userID)
		if err != nil {
			return err
		}
		if quantity+int64(requested) > limit {
			return ErrPendingOrderLimit
		}
	}
	ipLimit := limit
	if userID != nil {
		ipLimit = 200
	}
	ipQuantity, err := reservedQuantity("o.client_ip = ?", clientIP)
	if err != nil {
		return err
	}
	if ipQuantity+int64(requested) > ipLimit {
		return ErrPendingOrderLimit
	}
	emailQuantity, err := reservedQuantity("o.email = ?", email)
	if err != nil {
		return err
	}
	if emailQuantity+int64(requested) > limit {
		return ErrPendingOrderLimit
	}
	return nil
}

func availableCards(tx *gorm.DB, productID uuid.UUID, variantID *uuid.UUID) *gorm.DB {
	query := tx.Where("product_id = ? AND status = ?", productID, "available")
	if variantID == nil {
		return query.Where("variant_id IS NULL")
	}
	return query.Where("variant_id = ?", *variantID)
}

func newOrderLookupToken(enabled bool) (string, string, error) {
	if !enabled {
		return "", "", nil
	}
	random := make([]byte, 32)
	if _, err := cryptorand.Read(random); err != nil {
		return "", "", fmt.Errorf("generate order lookup token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	return token, HashOrderLookupToken(token), nil
}

func HashOrderLookupToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func revealOrder(vault *security.Vault, order *model.Order) error {
	if (order.Status != "delivered" && order.Status != "completed") || order.PaymentStatus != "paid" {
		return nil
	}
	for i := range order.Items {
		if len(order.Items[i].CardCiphertext) == 0 {
			continue
		}
		content, err := vault.Decrypt(order.Items[i].CardCiphertext, order.Items[i].CardNonce, order.Items[i].ProductID[:])
		if err != nil {
			return err
		}
		order.Items[i].CardContent = content
	}
	return nil
}

func orderNo(now time.Time) string {
	// Preserve the external order-number format while deriving its suffix from
	// the same cryptographically secure randomness used by UUID v4 identifiers.
	randomID := uuid.New()
	suffix := binary.BigEndian.Uint32(randomID[:4]) % 1_000_000
	return fmt.Sprintf("LLQ%s%06d", now.Format("20060102150405"), suffix)
}
