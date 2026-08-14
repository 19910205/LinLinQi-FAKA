package handler

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
	"linlinqi/api/pkg/response"
)

func (h Handler) validateAdminOTP(adminID uuid.UUID, code string) (bool, error) {
	validated := false
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		var locked model.TOTPDevice
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("realm = ? AND principal_id = ? AND enabled = ?", "admin", adminID, true).First(&locked).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				validated = true
				return nil
			}
			return err
		}
		var err error
		validated, err = h.validateLockedAdminOTP(tx, &locked, code)
		return err
	})
	return validated, err
}

func (h Handler) validateLockedAdminOTP(tx *gorm.DB, device *model.TOTPDevice, code string) (bool, error) {
	secret, err := h.Vault.Decrypt(device.SecretCipher, device.SecretNonce, device.PrincipalID[:])
	if err != nil {
		return false, err
	}
	provided := strings.ToUpper(strings.TrimSpace(code))
	if totp.Validate(provided, secret) {
		return true, nil
	}
	remaining, consumed := consumeRecoveryHash(device.RecoveryHashes, provided)
	if !consumed {
		return false, nil
	}
	if err := tx.Model(device).Update("recovery_hashes", remaining).Error; err != nil {
		return false, err
	}
	device.RecoveryHashes = remaining
	return true, nil
}

func consumeRecoveryHash(stored, provided string) (string, bool) {
	providedHash := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(provided))))
	wanted := hex.EncodeToString(providedHash[:])
	hashes := strings.Split(stored, ",")
	remaining := make([]string, 0, len(hashes))
	consumed := false
	for _, hash := range hashes {
		if !consumed && hmac.Equal([]byte(hash), []byte(wanted)) {
			consumed = true
			continue
		}
		if hash != "" {
			remaining = append(remaining, hash)
		}
	}
	return strings.Join(remaining, ","), consumed
}

type beginTOTPRequest struct {
	CurrentCode string `json:"current_code"`
	Password    string `json:"password" binding:"omitempty,max=72"`
}

var (
	errTOTPResetReauthentication = errors.New("totp reset reauthentication required")
	errTOTPVerification          = errors.New("totp verification failed")
)

func (h Handler) AdminTOTPStatus(c *gin.Context) {
	adminID, err := uuid.Parse(c.GetString("subject"))
	if err != nil {
		response.Error(c, 401, 40113, "error.invalid_admin_identity")
		return
	}
	var device model.TOTPDevice
	err = h.DB.Select("enabled", "verified_at", "pending_created_at").Where("realm = ? AND principal_id = ?", "admin", adminID).First(&device).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.OK(c, gin.H{"enabled": false, "pending_reset": false})
		return
	}
	if err != nil {
		response.Error(c, 500, 50050, "error.two_factor_status_read_failed")
		return
	}
	pending := device.PendingCreatedAt != nil && time.Since(*device.PendingCreatedAt) <= 10*time.Minute
	response.OK(c, gin.H{"enabled": device.Enabled, "verified_at": device.VerifiedAt, "pending_reset": pending, "pending_expires_at": func() any {
		if !pending {
			return nil
		}
		return device.PendingCreatedAt.Add(10 * time.Minute)
	}()})
}

func (h Handler) BeginAdminTOTP(c *gin.Context) {
	adminID, err := uuid.Parse(c.GetString("subject"))
	if err != nil {
		response.Error(c, 401, 40113, "error.invalid_admin_identity")
		return
	}
	var admin model.Admin
	if h.DB.First(&admin, "id = ?", adminID).Error != nil {
		response.Error(c, 404, 40450, "error.admin_not_found")
		return
	}
	var req beginTOTPRequest
	if c.Request.ContentLength != 0 && c.ShouldBindJSON(&req) != nil {
		response.Error(c, 422, 42252, "error.second_factor_reset_invalid")
		return
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "LinLinQi", AccountName: admin.Username, Period: 30, SecretSize: 32})
	if err != nil {
		response.Error(c, 500, 50050, "error.two_factor_secret_generate_failed")
		return
	}
	ciphertext, nonce, _, err := h.Vault.Encrypt(key.Secret(), adminID[:])
	if err != nil {
		response.Error(c, 500, 50051, "error.two_factor_secret_encrypt_failed")
		return
	}
	codes, hashes, err := recoveryCodes(10)
	if err != nil {
		response.Error(c, 500, 50052, "error.recovery_code_generate_failed")
		return
	}
	now := time.Now()
	pendingReset := false
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var device model.TOTPDevice
		findErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("realm = ? AND principal_id = ?", "admin", adminID).First(&device).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			return tx.Create(&model.TOTPDevice{Realm: "admin", PrincipalID: adminID, SecretCipher: ciphertext, SecretNonce: nonce, RecoveryHashes: strings.Join(hashes, ","), Enabled: false}).Error
		}
		if findErr != nil {
			return findErr
		}
		if device.Enabled {
			authorized := false
			if strings.TrimSpace(req.CurrentCode) != "" {
				var validateErr error
				authorized, validateErr = h.validateLockedAdminOTP(tx, &device, req.CurrentCode)
				if validateErr != nil {
					return validateErr
				}
			}
			if !authorized && req.Password != "" {
				var currentAdmin model.Admin
				if err := tx.Select("password_hash").First(&currentAdmin, "id = ? AND status = ?", adminID, "active").Error; err != nil {
					return err
				}
				authorized = bcrypt.CompareHashAndPassword([]byte(currentAdmin.PasswordHash), []byte(req.Password)) == nil
			}
			if !authorized {
				return errTOTPResetReauthentication
			}
			pendingReset = true
			return tx.Model(&device).Updates(map[string]any{
				"pending_secret_cipher":   ciphertext,
				"pending_secret_nonce":    nonce,
				"pending_recovery_hashes": strings.Join(hashes, ","),
				"pending_created_at":      &now,
			}).Error
		}
		return tx.Model(&device).Updates(map[string]any{
			"secret_cipher":           ciphertext,
			"secret_nonce":            nonce,
			"recovery_hashes":         strings.Join(hashes, ","),
			"enabled":                 false,
			"verified_at":             nil,
			"pending_secret_cipher":   nil,
			"pending_secret_nonce":    nil,
			"pending_recovery_hashes": "",
			"pending_created_at":      nil,
		}).Error
	})
	if errors.Is(err, errTOTPResetReauthentication) {
		response.Error(c, 422, 42253, "error.two_factor_verification_required")
		return
	}
	if err != nil {
		response.Error(c, 500, 50053, "error.two_factor_settings_save_failed")
		return
	}
	action := "security.2fa.begin"
	if pendingReset {
		action = "security.2fa.reset.begin"
	}
	h.audit(c, action, "admin", adminID.String(), "")
	response.OK(c, gin.H{"secret": key.Secret(), "provisioning_uri": key.URL(), "recovery_codes": codes, "pending_reset": pendingReset})
}

type verifyTOTPRequest struct {
	Code string `json:"code" binding:"required,len=6"`
}

func (h Handler) VerifyAdminTOTP(c *gin.Context) {
	adminID, err := uuid.Parse(c.GetString("subject"))
	if err != nil {
		response.Error(c, 401, 40113, "error.invalid_admin_identity")
		return
	}
	var req verifyTOTPRequest
	if c.ShouldBindJSON(&req) != nil {
		response.Error(c, 422, 42250, "error.six_digit_code_required")
		return
	}
	pendingReset := false
	pendingExpired := false
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var device model.TOTPDevice
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("realm = ? AND principal_id = ?", "admin", adminID).First(&device).Error; err != nil {
			return err
		}
		ciphertext, nonce := device.SecretCipher, device.SecretNonce
		if len(device.PendingSecretCipher) > 0 && len(device.PendingSecretNonce) > 0 {
			pendingReset = true
			if device.PendingCreatedAt == nil || time.Since(*device.PendingCreatedAt) > 10*time.Minute {
				pendingExpired = true
				return tx.Model(&device).Updates(map[string]any{
					"pending_secret_cipher":   nil,
					"pending_secret_nonce":    nil,
					"pending_recovery_hashes": "",
					"pending_created_at":      nil,
				}).Error
			}
			ciphertext, nonce = device.PendingSecretCipher, device.PendingSecretNonce
		}
		secret, decryptErr := h.Vault.Decrypt(ciphertext, nonce, adminID[:])
		if decryptErr != nil {
			return decryptErr
		}
		if !totp.Validate(strings.TrimSpace(req.Code), secret) {
			return errTOTPVerification
		}
		now := time.Now()
		updates := map[string]any{"enabled": true, "verified_at": &now}
		if pendingReset {
			updates["secret_cipher"] = device.PendingSecretCipher
			updates["secret_nonce"] = device.PendingSecretNonce
			updates["recovery_hashes"] = device.PendingRecoveryHashes
			updates["pending_secret_cipher"] = nil
			updates["pending_secret_nonce"] = nil
			updates["pending_recovery_hashes"] = ""
			updates["pending_created_at"] = nil
		}
		return tx.Model(&device).Updates(updates).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40451, "error.two_factor_not_initialized")
		return
	}
	if pendingExpired && err == nil {
		response.Error(c, 422, 42254, "error.two_factor_setup_expired")
		return
	}
	if errors.Is(err, errTOTPVerification) {
		response.Error(c, 422, 42251, "error.verification_code_invalid")
		return
	}
	if err != nil {
		response.Error(c, 500, 50054, "error.two_factor_enable_failed")
		return
	}
	action := "security.2fa.enable"
	if pendingReset {
		action = "security.2fa.reset.complete"
	}
	h.audit(c, action, "admin", adminID.String(), "")
	response.OK(c, gin.H{"enabled": true, "reset": pendingReset})
}

func (h Handler) RevokeAdminSessions(c *gin.Context) {
	adminID, err := uuid.Parse(c.GetString("subject"))
	if err != nil {
		response.Error(c, 401, 40113, "error.invalid_admin_identity")
		return
	}
	result := h.DB.Model(&model.Admin{}).Where("id = ? AND status = ?", adminID, "active").UpdateColumn("session_version", gorm.Expr("session_version + 1"))
	if result.Error != nil {
		response.Error(c, 500, 50055, "error.admin_session_revoke_failed")
		return
	}
	if result.RowsAffected != 1 {
		response.Error(c, 404, 40450, "error.admin_not_found")
		return
	}
	h.audit(c, "security.sessions.revoke_all", "admin", adminID.String(), "")
	response.OK(c, gin.H{"revoked": true})
}

func recoveryCodes(count int) ([]string, []string, error) {
	codes := make([]string, 0, count)
	hashes := make([]string, 0, count)
	for i := 0; i < count; i++ {
		buffer := make([]byte, 6)
		if _, err := rand.Read(buffer); err != nil {
			return nil, nil, err
		}
		code := strings.ToUpper(hex.EncodeToString(buffer[:3]) + "-" + hex.EncodeToString(buffer[3:]))
		sum := sha256.Sum256([]byte(code))
		codes = append(codes, code)
		hashes = append(hashes, hex.EncodeToString(sum[:]))
	}
	return codes, hashes, nil
}
