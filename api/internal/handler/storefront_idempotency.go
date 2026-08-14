package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"linlinqi/api/internal/service"
)

var errStorefrontIdempotencyKeyInvalid = errors.New("storefront idempotency key invalid")

func storefrontOrderIdempotency(c *gin.Context, email, clientOrderNo string, request any) (*service.StorefrontOrderIdempotency, error) {
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if !validIdempotencyKey(key) {
		return nil, errStorefrontIdempotencyKeyInvalid
	}
	clientOrderNo = strings.TrimSpace(clientOrderNo)
	if len(clientOrderNo) > 100 || strings.IndexFunc(clientOrderNo, func(character rune) bool {
		return character > unicode.MaxASCII || unicode.IsControl(character)
	}) >= 0 {
		return nil, errStorefrontIdempotencyKeyInvalid
	}
	scope := "guest:" + strings.ToLower(strings.TrimSpace(email))
	if userID, err := uuid.Parse(c.GetString("subject")); err == nil {
		scope = "user:" + userID.String()
	}
	idempotencyDigest := sha256.Sum256([]byte(scope + "\x00" + key))
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	requestDigest := sha256.Sum256(payload)
	return &service.StorefrontOrderIdempotency{
		IdempotencyHash: hex.EncodeToString(idempotencyDigest[:]),
		RequestHash:     hex.EncodeToString(requestDigest[:]),
		ClientOrderNo:   clientOrderNo,
	}, nil
}
