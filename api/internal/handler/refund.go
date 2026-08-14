package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/queue"
	"linlinqi/api/internal/service"
	"linlinqi/api/pkg/response"
)

type createRefundRequest struct {
	OrderID string `json:"order_id" binding:"required"`
	Amount  int64  `json:"amount"`
	Reason  string `json:"reason" binding:"required,max=500"`
}

var errRefundIdempotencyConflict = errors.New("refund idempotency key was reused for a different request")

func refundNumberForIdempotency(requestedBy, key string) string {
	digest := sha256.Sum256([]byte("linlinqi-refund\x00" + requestedBy + "\x00" + key))
	return "LQRI" + strings.ToUpper(hex.EncodeToString(digest[:24]))
}

func walletRefundPaymentMethod(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "balance", "supplier_balance":
		return true
	default:
		return false
	}
}

func refundIdempotentRequestMatches(refund model.Refund, orderID uuid.UUID, requestedBy, reason string, requestedAmount int64) bool {
	if refund.OrderID != orderID || refund.RequestedBy != requestedBy || refund.Reason != strings.TrimSpace(reason) {
		return false
	}
	// amount=0 is the documented "all remaining" request. Once that refund has
	// succeeded, remaining is zero, so the durable refund row is the only safe
	// replay response and must be returned without recalculating another amount.
	return requestedAmount == 0 || refund.OrderAmount == requestedAmount
}

func proportionalPaymentAmount(orderPart, orderTotal, paymentTotal int64) (int64, error) {
	if orderPart < 1 || orderTotal < 1 || paymentTotal < 1 || orderPart > orderTotal {
		return 0, fmt.Errorf("invalid refund proportion")
	}
	if orderPart == orderTotal {
		return paymentTotal, nil
	}
	numerator := new(big.Int).Mul(big.NewInt(orderPart), big.NewInt(paymentTotal))
	denominator := big.NewInt(orderTotal)
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)
	if new(big.Int).Lsh(remainder, 1).Cmp(denominator) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() || quotient.Sign() <= 0 {
		return 0, fmt.Errorf("refund conversion overflow")
	}
	return quotient.Int64(), nil
}

// incrementalProportionalPaymentAmount allocates provider-currency rounding
// against the cumulative refund target. Calculating each partial refund in
// isolation can exhaust a small provider amount before the order-side total is
// fully refunded (for example 3 order units settled as 2 provider units).
func incrementalProportionalPaymentAmount(committedOrder, orderPart, orderTotal, committedPayment, paymentTotal int64) (int64, error) {
	if committedOrder < 0 || committedPayment < 0 || orderPart < 1 || orderTotal < 1 || paymentTotal < 1 || committedOrder > orderTotal-orderPart || committedPayment >= paymentTotal {
		return 0, fmt.Errorf("invalid cumulative refund proportion")
	}
	cumulativeOrder := committedOrder + orderPart
	targetPayment := paymentTotal
	if cumulativeOrder < orderTotal {
		var err error
		targetPayment, err = proportionalPaymentAmount(cumulativeOrder, orderTotal, paymentTotal)
		if err != nil {
			return 0, err
		}
	}
	increment := targetPayment - committedPayment
	if increment < 1 || increment > paymentTotal-committedPayment {
		return 0, fmt.Errorf("partial refund is below the provider currency minimum unit")
	}
	return increment, nil
}

func refundableFulfillmentStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "delivered", "completed", "failed":
		return true
	default:
		return false
	}
}

func orderHasActiveRefund(tx *gorm.DB, orderID uuid.UUID) (bool, error) {
	var count int64
	err := tx.Model(&model.Refund{}).
		Where("order_id = ? AND status IN ?", orderID, []string{"pending", "processing", "retrying"}).
		Count(&count).Error
	return count > 0, err
}

func orderHasActiveProcurement(tx *gorm.DB, orderID uuid.UUID) (bool, error) {
	var count int64
	err := tx.Model(&model.ProcurementOrder{}).
		Where("order_id = ? AND status IN ?", orderID, []string{"creating", "dispatching", "processing", "retrying"}).
		Count(&count).Error
	return count > 0, err
}

func (h Handler) CreateRefund(c *gin.Context) {
	var req createRefundRequest
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.Reason) == "" {
		response.Error(c, 422, 42270, "error.refund_details_invalid")
		return
	}
	orderID, err := uuid.Parse(req.OrderID)
	if err != nil {
		response.Error(c, 422, 42271, "error.order_number_invalid")
		return
	}
	adminID := c.GetString("subject")
	requestedBy := "admin:" + adminID
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if !validIdempotencyKey(idempotencyKey) {
		response.Error(c, 422, 42299, "error.idempotency_key_required_or_invalid")
		return
	}
	refundNo := refundNumberForIdempotency(requestedBy, idempotencyKey)
	var refund model.Refund
	var localWallet, replayed bool
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var order model.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", orderID).Error; err != nil {
			return err
		}
		var existing model.Refund
		if err := tx.Where("refund_no = ?", refundNo).First(&existing).Error; err == nil {
			if !refundIdempotentRequestMatches(existing, orderID, requestedBy, req.Reason, req.Amount) {
				return errRefundIdempotencyConflict
			}
			refund, replayed, localWallet = existing, true, walletRefundPaymentMethod(order.PaymentMethod)
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if order.PaymentStatus != "paid" && order.PaymentStatus != "partially_refunded" {
			return fmt.Errorf("order is not refundable")
		}
		// A provider refund must not race an irreversible supplier purchase.
		// Operators first wait for delivery or move a failed procurement to its
		// terminal state; an in-flight order is never refunded speculatively.
		if !refundableFulfillmentStatus(order.Status) {
			return fmt.Errorf("order fulfillment is not terminal")
		}
		activeProcurement, err := orderHasActiveProcurement(tx, order.ID)
		if err != nil {
			return err
		}
		if activeProcurement {
			return fmt.Errorf("order has an active supplier procurement")
		}
		activeRefund, err := orderHasActiveRefund(tx, order.ID)
		if err != nil {
			return err
		}
		if activeRefund {
			return fmt.Errorf("order already has an active refund")
		}
		localWallet = walletRefundPaymentMethod(order.PaymentMethod)
		var intent model.PaymentIntent
		if localWallet {
			settlement, settlementErr := service.EnsureWalletOrderPaymentAuditTx(tx, order, time.Now().UTC())
			if settlementErr != nil {
				return settlementErr
			}
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&intent, "id = ?", settlement.PaymentIntentID).Error; err != nil {
				return err
			}
		} else if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("order_id = ? AND status IN ?", order.ID, []string{"succeeded", "partially_refunded"}).Order("succeeded_at DESC").First(&intent).Error; err != nil {
			return err
		}
		// The zero-channel intent is an independent wallet settlement fact.
		// This keeps old orders refundable if their presentation-level payment
		// method was empty or renamed, while the service still proves the exact
		// original wallet debit and billing owner before any credit is created.
		if !localWallet && intent.ChannelID == uuid.Nil {
			settlement, settlementErr := service.EnsureWalletOrderPaymentAuditTx(tx, order, time.Now().UTC())
			if settlementErr != nil {
				return settlementErr
			}
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&intent, "id = ?", settlement.PaymentIntentID).Error; err != nil {
				return err
			}
			localWallet = true
		}
		if intent.OrderAmount != order.Total || !strings.EqualFold(intent.OrderCurrency, order.Currency) {
			return fmt.Errorf("payment intent order amount snapshot is inconsistent")
		}
		var committed int64
		if err := tx.Model(&model.Refund{}).Where("order_id = ? AND status IN ?", order.ID, []string{"pending", "processing", "retrying", "succeeded"}).Select("COALESCE(SUM(order_amount), 0)").Scan(&committed).Error; err != nil {
			return err
		}
		var committedPayment int64
		if err := tx.Model(&model.Refund{}).Where("payment_intent_id = ? AND status IN ?", intent.ID, []string{"pending", "processing", "retrying", "succeeded"}).Select("COALESCE(SUM(amount), 0)").Scan(&committedPayment).Error; err != nil {
			return err
		}
		remaining := order.Total - committed
		amount := req.Amount
		if amount == 0 {
			amount = remaining
		}
		if amount < 1 || amount > remaining {
			return fmt.Errorf("refund exceeds remaining paid amount")
		}
		paymentAmount, err := incrementalProportionalPaymentAmount(committed, amount, intent.OrderAmount, committedPayment, intent.Amount)
		if err != nil {
			return err
		}
		status := "pending"
		if localWallet {
			status = "processing"
		}
		refund = model.Refund{
			Base:            model.Base{ID: uuid.New()},
			RefundNo:        refundNo,
			OrderID:         order.ID,
			PaymentIntentID: &intent.ID,
			Amount:          paymentAmount,
			Currency:        intent.Currency,
			OrderAmount:     amount,
			OrderCurrency:   order.Currency,
			Reason:          strings.TrimSpace(req.Reason),
			Status:          status,
			RequestedBy:     requestedBy,
		}
		if err := tx.Create(&refund).Error; err != nil {
			return err
		}
		if !localWallet {
			return nil
		}
		entry, err := service.ApplyWalletOrderRefundTx(tx, order, refund, time.Now().UTC())
		if err != nil {
			return err
		}
		if err := queue.FinalizeSuccessfulRefundTx(tx, refund, "wallet:"+entry.ID.String(), time.Now().UTC()); err != nil {
			return err
		}
		return tx.First(&refund, "id = ?", refund.ID).Error
	})
	if err != nil {
		if errors.Is(err, errRefundIdempotencyConflict) {
			response.Error(c, 409, 40971, "error.refund_idempotency_conflict")
			return
		}
		response.Error(c, 409, 40970, "error.refund_creation_failed")
		return
	}
	h.audit(c, "refund.create", "refund", refund.ID.String(), refund.RefundNo)
	if localWallet || refund.Status == "succeeded" || refund.Status == "failed" {
		response.Created(c, gin.H{"refund": refund, "queued": false, "replayed": replayed, "local_wallet": localWallet})
		return
	}
	client := queue.NewClient(h.Cfg, h.DB)
	_, enqueueErr := client.Enqueue(queue.TypeRefundProcess, map[string]string{"refund_id": refund.ID.String()})
	_ = client.Close()
	response.Created(c, gin.H{"refund": refund, "queued": enqueueErr == nil, "replayed": replayed})
}
