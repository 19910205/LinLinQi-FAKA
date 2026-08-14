package handler

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5" // #nosec G501 -- Dujiao body digest; callback authentication is HMAC-SHA256
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
	"linlinqi/api/internal/supply"
	"linlinqi/api/pkg/response"
)

const maxSupplierCallbackBody = 1 << 20

type supplierCallbackPayload struct {
	EventID    string                       `json:"event_id"`
	Event      string                       `json:"event"`
	OccurredAt time.Time                    `json:"occurred_at"`
	Data       supplierCallbackDeliveryData `json:"data"`
}

type supplierCallbackDeliveryData struct {
	ClientOrderNo   string   `json:"client_order_no"`
	ExternalOrderNo string   `json:"external_order_no"`
	Status          string   `json:"status"`
	Deliveries      []string `json:"deliveries"`
	Cost            int64    `json:"cost"`
}

func verifySupplierCallbackSignature(timestampValue, signatureValue, secret string, body []byte, now time.Time) error {
	timestamp, err := strconv.ParseInt(strings.TrimSpace(timestampValue), 10, 64)
	if err != nil {
		return errors.New("invalid callback timestamp")
	}
	signedAt := time.Unix(timestamp, 0)
	if signedAt.Before(now.Add(-5*time.Minute)) || signedAt.After(now.Add(time.Minute)) {
		return errors.New("expired callback timestamp")
	}
	provided, err := hex.DecodeString(strings.TrimSpace(signatureValue))
	if err != nil || len(provided) != sha256.Size {
		return errors.New("invalid callback signature")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(timestamp, 10) + "."))
	mac.Write(body)
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return errors.New("invalid callback signature")
	}
	return nil
}

func decodeSupplierCallback(body []byte) (supplierCallbackPayload, error) {
	var payload supplierCallbackPayload
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return payload, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return payload, errors.New("callback body must contain one JSON object")
	}
	payload.EventID = strings.TrimSpace(payload.EventID)
	payload.Event = strings.ToLower(strings.TrimSpace(payload.Event))
	payload.Data.ClientOrderNo = strings.TrimSpace(payload.Data.ClientOrderNo)
	payload.Data.ExternalOrderNo = strings.TrimSpace(payload.Data.ExternalOrderNo)
	payload.Data.Status = strings.ToLower(strings.TrimSpace(payload.Data.Status))
	if !validOpenAPIIdentifier(payload.EventID, 160) || payload.Event != "order.delivered" || payload.OccurredAt.IsZero() ||
		!validOpenAPIIdentifier(payload.Data.ClientOrderNo, 100) || !validOpenAPIIdentifier(payload.Data.ExternalOrderNo, 160) ||
		(payload.Data.Status != "delivered" && payload.Data.Status != "succeeded" && payload.Data.Status != "completed") ||
		payload.Data.Cost < 0 || payload.Data.Cost > 1_000_000_000_000 ||
		!supply.ValidateDeliveries(payload.Data.Deliveries, len(payload.Data.Deliveries)) {
		return payload, errors.New("invalid callback payload")
	}
	return payload, nil
}

type dujiaoCallbackPayload struct {
	Event             string          `json:"event"`
	OrderID           json.RawMessage `json:"order_id"`
	OrderNo           string          `json:"order_no"`
	DownstreamOrderNo string          `json:"downstream_order_no"`
	Status            string          `json:"status"`
	Fulfillment       struct {
		Type         string          `json:"type"`
		Status       string          `json:"status"`
		Payload      string          `json:"payload"`
		DeliveryData json.RawMessage `json:"delivery_data"`
		DeliveredAt  *time.Time      `json:"delivered_at"`
	} `json:"fulfillment"`
	Timestamp int64 `json:"timestamp"`
}

func decodeDujiaoCallback(body []byte) (supplierCallbackPayload, bool, error) {
	var input dujiaoCallbackPayload
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(&input); err != nil {
		return supplierCallbackPayload{}, false, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return supplierCallbackPayload{}, false, errors.New("callback body must contain one JSON object")
	}
	input.Event = strings.ToLower(strings.TrimSpace(input.Event))
	input.OrderNo = strings.TrimSpace(input.OrderNo)
	input.DownstreamOrderNo = strings.TrimSpace(input.DownstreamOrderNo)
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	input.Fulfillment.Status = strings.ToLower(strings.TrimSpace(input.Fulfillment.Status))
	if input.Event != "order.status_changed" || !validOpenAPIIdentifier(input.DownstreamOrderNo, 100) || input.Timestamp <= 0 {
		return supplierCallbackPayload{}, false, errors.New("invalid dujiao callback payload")
	}
	orderID, err := dujiaoCallbackOrderID(input.OrderID)
	if err != nil {
		return supplierCallbackPayload{}, false, err
	}
	externalOrderNo := orderID
	if externalOrderNo == "" {
		externalOrderNo = input.OrderNo
	}
	if !validOpenAPIIdentifier(externalOrderNo, 160) {
		return supplierCallbackPayload{}, false, errors.New("invalid dujiao callback order")
	}
	status := input.Status
	if status == "paid" || status == "pending" || status == "submitted" || status == "accepted" {
		status = "processing"
	}
	if status == "canceled" || status == "refunded" || status == "rejected" {
		status = "cancelled"
	}
	isDelivery := status == "delivered" || status == "fulfilled" || status == "completed" || input.Fulfillment.Status == "delivered"
	deliveries := []string(nil)
	if isDelivery {
		status = "delivered"
		deliveries = supplierCallbackDeliveries(input.Fulfillment.Payload)
		if len(deliveries) == 0 || !supply.ValidateDeliveries(deliveries, len(deliveries)) {
			return supplierCallbackPayload{}, false, errors.New("invalid dujiao callback delivery")
		}
	} else if status != "processing" && status != "failed" && status != "cancelled" {
		return supplierCallbackPayload{}, false, errors.New("invalid dujiao callback status")
	}
	eventSeed := orderID + "\x00" + input.OrderNo + "\x00" + input.Status + "\x00" + strconv.FormatInt(input.Timestamp, 10)
	eventHash := sha256.Sum256([]byte(eventSeed))
	return supplierCallbackPayload{
		EventID: "dujiao:" + hex.EncodeToString(eventHash[:]), Event: input.Event,
		OccurredAt: time.Unix(input.Timestamp, 0).UTC(),
		Data: supplierCallbackDeliveryData{
			ClientOrderNo: input.DownstreamOrderNo, ExternalOrderNo: externalOrderNo,
			Status: status, Deliveries: deliveries,
		},
	}, isDelivery, nil
}

func dujiaoCallbackOrderID(raw json.RawMessage) (string, error) {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return "", nil
	}
	if strings.HasPrefix(value, `"`) {
		var decoded string
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return "", errors.New("invalid dujiao callback order id")
		}
		value = strings.TrimSpace(decoded)
	}
	if !validOpenAPIIdentifier(value, 160) || strings.IndexFunc(value, func(character rune) bool { return character < '0' || character > '9' }) >= 0 {
		return "", errors.New("invalid dujiao callback order id")
	}
	return value, nil
}

func supplierCallbackDeliveries(value string) []string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\r\n", "\n"))
	if value == "" {
		return nil
	}
	parts := strings.Split(value, "\n")
	result := make([]string, 0, len(parts))
	for _, item := range parts {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func verifyDujiaoCallbackSignature(c *gin.Context, secret string, body []byte, occurredAt time.Time, now time.Time) error {
	timestampValue := strings.TrimSpace(c.GetHeader("Dujiao-Next-Timestamp"))
	timestamp, err := strconv.ParseInt(timestampValue, 10, 64)
	if err != nil || timestamp != occurredAt.Unix() || occurredAt.Before(now.Add(-90*time.Second)) || occurredAt.After(now.Add(time.Minute)) {
		return errors.New("invalid dujiao callback timestamp")
	}
	if strings.TrimSpace(c.GetHeader("Dujiao-Next-Api-Key")) == "" {
		return errors.New("missing dujiao callback api key")
	}
	provided, err := hex.DecodeString(strings.TrimSpace(c.GetHeader("Dujiao-Next-Signature")))
	if err != nil || len(provided) != sha256.Size {
		return errors.New("invalid dujiao callback signature")
	}
	// The upstream canonical format specifies MD5 for the body digest. The
	// actual authenticator remains constant-time HMAC-SHA256 and the timestamp
	// window below prevents replay of a previously signed callback.
	digest := md5.Sum(body) // #nosec G401 -- compatibility digest inside HMAC-SHA256
	path := c.Request.URL.EscapedPath()
	message := http.MethodPost + "\n" + path + "\n" + timestampValue + "\n" + hex.EncodeToString(digest[:])
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(message))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return errors.New("invalid dujiao callback signature")
	}
	return nil
}

func callbackEventID(supplierID uuid.UUID, externalEventID string) string {
	digest := sha256.Sum256([]byte(supplierID.String() + "\x00" + externalEventID))
	return "supplier:" + hex.EncodeToString(digest[:])
}

func callbackSupplierProtocol(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || value == "standard" {
		return "linlinqi-standard"
	}
	return value
}

func supplierCallbackEndpoint(identifier string) string {
	return "/api/v1/supplier-callbacks/" + identifier
}

// SupplierCallback persists a signed delivery before acknowledging it. The
// fulfillment worker performs the order mutation, so callback retries and
// polling share one idempotent delivery path.
func (h Handler) SupplierCallback(c *gin.Context) {
	identifier := strings.TrimSpace(c.Param("supplier"))
	if identifier == "" || len(identifier) > 60 || strings.IndexFunc(identifier, unicode.IsControl) >= 0 {
		response.Error(c, http.StatusNotFound, 40480, "error.supplier_callback_not_found")
		return
	}
	var supplier model.Supplier
	query := h.DB
	if supplierID, err := uuid.Parse(identifier); err == nil {
		query = query.Where("id = ?", supplierID)
	} else {
		query = query.Where("code = ?", strings.ToLower(identifier))
	}
	if err := query.First(&supplier).Error; err != nil {
		response.Error(c, http.StatusNotFound, 40480, "error.supplier_callback_not_found")
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxSupplierCallbackBody+1))
	if err != nil || len(body) == 0 || len(body) > maxSupplierCallbackBody {
		response.Error(c, http.StatusUnprocessableEntity, 42280, "error.supplier_callback_payload_invalid")
		return
	}
	protocol := callbackSupplierProtocol(supplier.Protocol)
	payload := supplierCallbackPayload{}
	isDelivery := true
	if protocol == "dujiao-next" {
		payload, isDelivery, err = decodeDujiaoCallback(body)
	} else {
		payload, err = decodeSupplierCallback(body)
	}
	if err != nil {
		response.Error(c, http.StatusUnprocessableEntity, 42280, "error.supplier_callback_payload_invalid")
		return
	}
	var verificationProcurement model.ProcurementOrder
	lookupErr := h.DB.Select("id", "callback_secret_cipher", "callback_secret_nonce").
		Where("supplier_id = ? AND procurement_no = ?", supplier.ID, payload.Data.ClientOrderNo).
		First(&verificationProcurement).Error
	if lookupErr != nil && !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		response.Error(c, http.StatusServiceUnavailable, 50380, "error.supplier_callback_temporarily_unavailable")
		return
	}
	secret := ""
	if lookupErr == nil && (len(verificationProcurement.CallbackSecretCipher) > 0 || len(verificationProcurement.CallbackSecretNonce) > 0) {
		if len(verificationProcurement.CallbackSecretCipher) == 0 || len(verificationProcurement.CallbackSecretNonce) == 0 {
			response.Error(c, http.StatusUnauthorized, 40180, "error.supplier_callback_signature_invalid")
			return
		}
		secret, err = service.DecryptProcurementCallbackSecret(h.Vault, verificationProcurement.ID, verificationProcurement.CallbackSecretCipher, verificationProcurement.CallbackSecretNonce)
	} else {
		secret, err = h.Vault.Decrypt(supplier.APISecretCipher, supplier.APISecretNonce, append(supplier.ID[:], []byte("api-secret")...))
	}
	if err != nil {
		response.Error(c, http.StatusUnauthorized, 40180, "error.supplier_callback_signature_invalid")
		return
	}
	now := time.Now().UTC()
	if protocol == "dujiao-next" {
		err = verifyDujiaoCallbackSignature(c, secret, body, payload.OccurredAt, now)
	} else {
		err = verifySupplierCallbackSignature(c.GetHeader("X-LinLinQi-Timestamp"), c.GetHeader("X-LinLinQi-Signature"), secret, body, now)
	}
	if err != nil {
		response.Error(c, http.StatusUnauthorized, 40180, "error.supplier_callback_signature_invalid")
		return
	}
	var procurement model.ProcurementOrder
	created := false
	eventStatus := "queued"
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("supplier_id = ? AND procurement_no = ?", supplier.ID, payload.Data.ClientOrderNo).First(&procurement).Error; err != nil {
			return err
		}
		if (isDelivery && len(payload.Data.Deliveries) != procurement.Quantity) || (!isDelivery && len(payload.Data.Deliveries) != 0) || payload.OccurredAt.After(now.Add(5*time.Minute)) || payload.OccurredAt.Before(procurement.CreatedAt.Add(-time.Hour)) {
			return errors.New("callback order metadata mismatch")
		}
		if procurement.ExternalOrderNo != "" && procurement.ExternalOrderNo != payload.Data.ExternalOrderNo {
			return errors.New("callback external order mismatch")
		}
		if !isDelivery {
			eventStatus = "ignored"
		} else if procurement.Status == "completed" || procurement.Status == "failed" || procurement.Status == "cancelled" {
			eventStatus = "ignored"
		}
		result := supply.OrderResult{ExternalOrderNo: payload.Data.ExternalOrderNo, Status: payload.Data.Status, Deliveries: payload.Data.Deliveries, Cost: payload.Data.Cost}
		encoded, err := json.Marshal(result)
		if err != nil {
			return err
		}
		processedAt := (*time.Time)(nil)
		responseText := ""
		if eventStatus == "ignored" {
			processedAt = &now
			if isDelivery {
				responseText = "procurement is already terminal"
			} else {
				responseText = "non-delivery status recorded; polling scheduled"
			}
		}
		event := model.WebhookEvent{
			Base: model.Base{ID: uuid.New()}, EventID: callbackEventID(supplier.ID, payload.EventID), EventType: payload.Event,
			Endpoint: supplierCallbackEndpoint(identifier), Payload: `{}`, SupplierID: &supplier.ID, ProcurementOrderID: &procurement.ID,
			Status: eventStatus, Response: responseText, ProcessedAt: processedAt,
		}
		ciphertext, nonce, _, err := h.Vault.Encrypt(string(encoded), event.ID[:])
		if err != nil {
			return err
		}
		event.PayloadCipher, event.PayloadNonce = ciphertext, nonce
		create := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "event_id"}}, DoNothing: true}).Create(&event)
		if create.Error != nil {
			return create.Error
		}
		created = create.RowsAffected == 1
		if created && (eventStatus == "queued" || !isDelivery) {
			if err := tx.Model(&model.ProcurementOrder{}).
				Where("id = ? AND status IN ?", procurement.ID, []string{"creating", "retrying", "processing"}).
				Update("next_poll_at", now).Error; err != nil {
				return err
			}
		}
		if !created {
			var existing model.WebhookEvent
			if err := tx.Select("status").First(&existing, "event_id = ?", event.EventID).Error; err != nil {
				return err
			}
			eventStatus = existing.Status
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, http.StatusNotFound, 40481, "error.supplier_procurement_not_found")
		return
	}
	if err != nil {
		response.Error(c, http.StatusConflict, 40980, "error.supplier_callback_order_mismatch")
		return
	}
	if created && eventStatus == "queued" {
		if err := h.enqueueSupplierOrder(procurement.OrderID); err != nil {
			// The durable inbox is intentionally retained. The scheduler will retry
			// the procurement even when Redis is momentarily unavailable.
			_ = h.DB.Model(&model.WebhookEvent{}).Where("event_id = ?", callbackEventID(supplier.ID, payload.EventID)).Update("response", fmt.Sprintf("enqueue deferred: %s", err)).Error
		}
	}
	c.Header("Cache-Control", "no-store")
	response.OK(c, gin.H{"accepted": true, "replayed": !created, "status": eventStatus})
}
