package service

import (
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
)

var orderTransitions = map[string]map[string]bool{
	"pending_payment": {"cancelled": true, "expired": true},
	"pending":         {"paid": true, "cancelled": true, "expired": true, "risk_review": true},
	"risk_review":     {"pending": true, "cancelled": true},
	"paid":            {"processing": true, "refunding": true},
	"processing":      {"failed": true, "refunding": true},
	"failed":          {"processing": true, "refunding": true},
	"delivered":       {"refunding": true, "completed": true},
	"refunding":       {"refunded": true, "delivered": true},
}

func TransitionOrder(db *gorm.DB, orderID uuid.UUID, target, actorType string, actorID *uuid.UUID, reason string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var order model.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", orderID).Error; err != nil {
			return err
		}
		if !orderTransitions[order.Status][target] {
			return fmt.Errorf("invalid order transition %s -> %s", order.Status, target)
		}
		if target == "processing" {
			var activeRefunds int64
			if err := tx.Model(&model.Refund{}).
				Where("order_id = ? AND status IN ?", order.ID, []string{"pending", "processing", "retrying"}).
				Count(&activeRefunds).Error; err != nil {
				return err
			}
			if activeRefunds > 0 {
				return fmt.Errorf("order has an active refund")
			}
		}
		from := order.Status
		if order.PaymentStatus == "pending" && (target == "cancelled" || target == "expired") {
			if err := ReleaseCouponReservation(tx, order.ID); err != nil {
				return err
			}
			if err := tx.Model(&model.Card{}).Where("order_id = ? AND status = ?", order.ID, "locked").Updates(map[string]any{"status": "available", "order_id": nil}).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.OrderItem{}).Where("order_id = ?", order.ID).Updates(map[string]any{"card_ciphertext": nil, "card_nonce": nil, "card_preview": ""}).Error; err != nil {
				return err
			}
		}
		if supplierReservationTerminalStatus(target) {
			if err := ReleaseSupplierInventoryReservationsTx(tx, order.ID, "order "+target); err != nil {
				return err
			}
		}
		if err := tx.Model(&order).Update("status", target).Error; err != nil {
			return err
		}
		return tx.Create(&model.OrderEvent{OrderID: order.ID, FromStatus: from, ToStatus: target, ActorType: actorType, ActorID: actorID, Reason: reason}).Error
	})
}

func supplierReservationTerminalStatus(status string) bool {
	switch status {
	case "cancelled", "expired", "failed", "delivered", "completed", "refunded":
		return true
	default:
		return false
	}
}
