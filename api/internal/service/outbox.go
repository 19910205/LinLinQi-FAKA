package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/security"
)

type DeliveryOutbox struct {
	NotificationID *uuid.UUID
	WebhookIDs     []uuid.UUID
}

// CreateDeliveryOutbox persists delivery notifications before they are sent.
// Repeated calls reuse existing records, so callback and worker retries cannot
// fan out duplicate customer messages.
func CreateDeliveryOutbox(db *gorm.DB, vault *security.Vault, orderID uuid.UUID, userAppURL string) (DeliveryOutbox, error) {
	var out DeliveryOutbox
	err := db.Transaction(func(tx *gorm.DB) error {
		var order model.Order
		if err := tx.Preload("Items").Where("id = ? AND status IN ? AND payment_status = ?", orderID, []string{"delivered", "completed"}, "paid").First(&order).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil
			}
			return err
		}
		subject := "LinLinQi 订单交付 " + order.OrderNo
		idempotencyKey := "order-delivered:" + order.ID.String()
		var existing model.NotificationDelivery
		if tx.Where("idempotency_key = ?", idempotencyKey).First(&existing).Error == nil {
			out.NotificationID = &existing.ID
		} else {
			lines := []string{"订单 " + order.OrderNo + " 已完成交付。", ""}
			for _, item := range order.Items {
				content, err := vault.Decrypt(item.CardCiphertext, item.CardNonce, item.ProductID[:])
				if err != nil {
					return err
				}
				lines = append(lines, fmt.Sprintf("%s × %d", item.ProductName, item.Quantity), content, "")
			}
			lines = append(lines, "订单查询："+strings.TrimRight(userAppURL, "/")+"/orders")
			if len(order.LookupTokenCipher) > 0 {
				lookupToken, err := vault.Decrypt(order.LookupTokenCipher, order.LookupTokenNonce, append(order.ID[:], []byte("lookup-token")...))
				if err != nil {
					return err
				}
				lines = append(lines, "查询密钥（请妥善保存）："+lookupToken)
			}
			delivery := model.NotificationDelivery{Base: model.Base{ID: uuid.New()}, IdempotencyKey: idempotencyKey, Channel: "email", Recipient: order.Email, Subject: subject, Status: "queued"}
			ciphertext, nonce, _, err := vault.Encrypt(strings.Join(lines, "\n"), delivery.ID[:])
			if err != nil {
				return err
			}
			delivery.BodyCipher, delivery.BodyNonce = ciphertext, nonce
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "idempotency_key"}}, DoNothing: true}).Create(&delivery).Error; err != nil {
				return err
			}
			if err := tx.Where("idempotency_key = ?", idempotencyKey).First(&existing).Error; err != nil {
				return err
			}
			out.NotificationID = &existing.ID
		}
		ownerClauses := tx.Where("1 = 0")
		if order.UserID != nil {
			ownerClauses = tx.Where("owner_type = ? AND owner_id = ?", "user", *order.UserID)
		}
		if order.CallbackEndpointID != nil {
			if order.UserID != nil {
				ownerClauses = ownerClauses.Or("id = ?", *order.CallbackEndpointID)
			} else {
				ownerClauses = tx.Where("id = ?", *order.CallbackEndpointID)
			}
		}
		var endpoints []model.WebhookEndpoint
		if err := ownerClauses.Where("enabled = ? AND events @> CAST(? AS jsonb)", true, `["order.delivered"]`).Find(&endpoints).Error; err != nil {
			return err
		}
		eventID := "order.delivered:" + order.ID.String()
		userPayload, _ := json.Marshal(map[string]any{"event_id": eventID, "event": "order.delivered", "occurred_at": order.DeliveredAt, "data": map[string]any{"order_no": order.OrderNo, "status": order.Status, "total": order.Total, "delivered_at": order.DeliveredAt}})
		for _, endpoint := range endpoints {
			payload := userPayload
			delivery := model.WebhookDelivery{Base: model.Base{ID: uuid.New()}, EndpointID: endpoint.ID, EventID: eventID, EventType: "order.delivered", Payload: string(payload), Status: "queued"}
			if endpoint.OwnerType == "api_credential" {
				deliveries := make([]string, 0)
				for _, item := range order.Items {
					values, err := DecryptDeliveryItems(vault, item)
					if err != nil {
						return err
					}
					deliveries = append(deliveries, values...)
				}
				clientOrderNo := ""
				if order.ExternalOrderNo != nil {
					clientOrderNo = *order.ExternalOrderNo
				}
				payload, _ = json.Marshal(map[string]any{
					"event_id": eventID, "event": "order.delivered", "occurred_at": order.DeliveredAt,
					"data": map[string]any{"client_order_no": clientOrderNo, "external_order_no": order.OrderNo, "status": order.Status, "deliveries": deliveries, "cost": order.Total},
				})
				ciphertext, nonce, _, err := vault.Encrypt(string(payload), delivery.ID[:])
				if err != nil {
					return err
				}
				delivery.Payload = `{}`
				delivery.PayloadCipher, delivery.PayloadNonce = ciphertext, nonce
			}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "endpoint_id"}, {Name: "event_id"}}, DoNothing: true}).Create(&delivery).Error; err != nil {
				return err
			}
			if err := tx.Where("endpoint_id = ? AND event_id = ?", endpoint.ID, eventID).First(&delivery).Error; err != nil {
				return err
			}
			out.WebhookIDs = append(out.WebhookIDs, delivery.ID)
		}
		return nil
	})
	return out, err
}
