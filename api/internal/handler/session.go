package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/i18n"
	"linlinqi/api/internal/middleware"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/queue"
	"linlinqi/api/pkg/response"
)

func randomRefreshToken() (string, string, error) {
	buffer := make([]byte, 48)
	if _, err := rand.Read(buffer); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(buffer)
	sum := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(sum[:]), nil
}

func refreshHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func validateUserPassword(password string) error {
	// bcrypt rejects passwords over 72 bytes. Validate the byte boundary before
	// hashing so a multi-byte password can never create an account with an empty
	// or unusable hash.
	if !utf8.ValidString(password) || len([]rune(password)) < 8 || len(password) > 72 {
		return errors.New("password must contain 8 or more characters and at most 72 bytes")
	}
	for _, character := range password {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return errors.New("password contains whitespace or control characters")
		}
	}
	lower := strings.ToLower(password)
	for _, weak := range []string{"password", "qwerty", "12345678", "linlinqi"} {
		if strings.Contains(lower, weak) {
			return errors.New("password contains a common weak phrase")
		}
	}
	return nil
}

func (h Handler) createUserSession(c *gin.Context, userID uuid.UUID) (string, uuid.UUID, error) {
	token, hash, err := randomRefreshToken()
	if err != nil {
		return "", uuid.Nil, err
	}
	now := time.Now()
	session := model.UserSession{UserID: userID, RefreshHash: hash, Device: truncateSecurityValue(c.GetHeader("X-Device-Name"), 200), IP: truncateSecurityValue(c.ClientIP(), 64), UserAgent: truncateSecurityValue(c.Request.UserAgent(), 500), LastActiveAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour)}
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&session).Error; err != nil {
			return err
		}
		return tx.Create(&model.UserSessionToken{UserSessionID: session.ID, UserID: userID, TokenHash: hash, ExpiresAt: session.ExpiresAt}).Error
	}); err != nil {
		return "", uuid.Nil, err
	}
	return token, session.ID, nil
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

var errRefreshSessionInvalid = errors.New("refresh session invalid")

func (h Handler) RefreshUserSession(c *gin.Context) {
	var req refreshRequest
	if c.ShouldBindJSON(&req) != nil || len(req.RefreshToken) < 40 || len(req.RefreshToken) > 100 {
		response.Error(c, 422, 42212, "error.refresh_token_required")
		return
	}
	newToken, newHash, err := randomRefreshToken()
	if err != nil {
		response.Error(c, 500, 50012, "error.session_refresh_failed")
		return
	}
	incomingHash := refreshHash(req.RefreshToken)
	now := time.Now()
	var session model.UserSession
	reuseDetected := false
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var tokenRecord model.UserSessionToken
		tokenErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("token_hash = ?", incomingHash).First(&tokenRecord).Error
		if errors.Is(tokenErr, gorm.ErrRecordNotFound) {
			// Compatibility path for sessions created before token-family history
			// was introduced. The legacy token becomes tracked before rotation.
			legacyErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("refresh_hash = ?", incomingHash).First(&session).Error
			if errors.Is(legacyErr, gorm.ErrRecordNotFound) {
				// A concurrent request can create the compatibility row while this
				// transaction waits on the session lock. Re-read it before deciding
				// the token is unknown so the second request is treated as reuse.
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("token_hash = ?", incomingHash).First(&tokenRecord).Error; err != nil {
					return errRefreshSessionInvalid
				}
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&session, "id = ?", tokenRecord.UserSessionID).Error; err != nil {
					return errRefreshSessionInvalid
				}
			} else if legacyErr != nil {
				return legacyErr
			} else {
				tokenRecord = model.UserSessionToken{UserSessionID: session.ID, UserID: session.UserID, TokenHash: incomingHash, ExpiresAt: session.ExpiresAt}
				if err := tx.Create(&tokenRecord).Error; err != nil {
					return err
				}
			}
		} else if tokenErr != nil {
			return tokenErr
		} else if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&session, "id = ?", tokenRecord.UserSessionID).Error; err != nil {
			return errRefreshSessionInvalid
		}

		if tokenRecord.UserID != session.UserID || tokenRecord.UserSessionID != session.ID {
			return errRefreshSessionInvalid
		}
		if refreshTokenWasReused(tokenRecord, session, incomingHash) {
			reuseDetected = true
			return h.revokeUserTokenFamily(tx, c, session.UserID, session.ID, now)
		}
		if !tokenRecord.ExpiresAt.After(now) || !session.ExpiresAt.After(now) {
			return errRefreshSessionInvalid
		}

		consumed := tx.Model(&model.UserSessionToken{}).
			Where("id = ? AND used_at IS NULL AND revoked_at IS NULL", tokenRecord.ID).
			Update("used_at", &now)
		if consumed.Error != nil {
			return consumed.Error
		}
		if consumed.RowsAffected != 1 {
			return errRefreshSessionInvalid
		}
		replacement := model.UserSessionToken{UserSessionID: session.ID, UserID: session.UserID, TokenHash: newHash, ExpiresAt: session.ExpiresAt}
		if err := tx.Create(&replacement).Error; err != nil {
			return err
		}
		updated := tx.Model(&model.UserSession{}).
			Where("id = ? AND refresh_hash = ? AND revoked_at IS NULL", session.ID, incomingHash).
			Updates(map[string]any{"refresh_hash": newHash, "last_active_at": now, "ip": truncateSecurityValue(c.ClientIP(), 64), "user_agent": truncateSecurityValue(c.Request.UserAgent(), 500)})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return errRefreshSessionInvalid
		}
		return nil
	})
	if reuseDetected && err == nil {
		// Keep the public response indistinguishable from an unknown token;
		// operators receive the high-severity SecurityEvent with full context.
		response.Error(c, 401, 40114, "error.session_invalid")
		return
	}
	if errors.Is(err, errRefreshSessionInvalid) || errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 401, 40114, "error.session_invalid")
		return
	}
	if err != nil {
		response.Error(c, 500, 50012, "error.session_refresh_failed")
		return
	}
	var user model.User
	if err := h.DB.Select("session_version").First(&user, "id = ?", session.UserID).Error; err != nil {
		response.Error(c, 500, 50012, "error.session_refresh_failed")
		return
	}
	accessToken, err := middleware.IssueUserToken(session.UserID.String(), h.Cfg.JWTSecret, session.ID.String(), user.SessionVersion, 15*time.Minute)
	if err != nil {
		response.Error(c, 500, 50012, "error.session_refresh_failed")
		return
	}
	response.OK(c, gin.H{"token": accessToken, "refresh_token": newToken, "expires_in": 900})
}

func refreshTokenWasReused(token model.UserSessionToken, session model.UserSession, incomingHash string) bool {
	return token.UsedAt != nil || token.RevokedAt != nil || session.RevokedAt != nil || session.RefreshHash != incomingHash
}

func (h Handler) revokeUserTokenFamily(tx *gorm.DB, c *gin.Context, userID, sessionID uuid.UUID, now time.Time) error {
	if err := tx.Model(&model.UserSession{}).Where("user_id = ? AND revoked_at IS NULL", userID).Update("revoked_at", &now).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.UserSessionToken{}).Where("user_id = ? AND revoked_at IS NULL", userID).Update("revoked_at", &now).Error; err != nil {
		return err
	}
	details, _ := json.Marshal(map[string]string{
		"reason":     "rotated_refresh_token_reused",
		"session_id": sessionID.String(),
		"request_id": truncateSecurityValue(c.GetString("request_id"), 64),
	})
	principalID := userID
	return tx.Create(&model.SecurityEvent{
		EventType:   "auth.refresh_token_reuse",
		Severity:    "critical",
		Realm:       "user",
		PrincipalID: &principalID,
		IP:          truncateSecurityValue(c.ClientIP(), 64),
		UserAgent:   truncateSecurityValue(c.Request.UserAgent(), 500),
		Details:     string(details),
	}).Error
}

func truncateSecurityValue(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func (h Handler) LogoutUserSession(c *gin.Context) {
	var req refreshRequest
	if c.ShouldBindJSON(&req) != nil || len(req.RefreshToken) < 40 || len(req.RefreshToken) > 100 {
		response.OK(c, gin.H{"revoked": false})
		return
	}
	now := time.Now()
	revoked := false
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		var tokenRecord model.UserSessionToken
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("token_hash = ?", refreshHash(req.RefreshToken)).First(&tokenRecord).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			result := tx.Model(&model.UserSession{}).Where("refresh_hash = ? AND revoked_at IS NULL", refreshHash(req.RefreshToken)).Update("revoked_at", &now)
			revoked = result.RowsAffected > 0
			return result.Error
		}
		if err != nil {
			return err
		}
		result := tx.Model(&model.UserSession{}).Where("id = ? AND revoked_at IS NULL", tokenRecord.UserSessionID).Update("revoked_at", &now)
		if result.Error != nil {
			return result.Error
		}
		revoked = result.RowsAffected > 0
		return tx.Model(&model.UserSessionToken{}).Where("user_session_id = ? AND revoked_at IS NULL", tokenRecord.UserSessionID).Update("revoked_at", &now).Error
	})
	if err != nil {
		response.Error(c, 500, 50013, "error.session_logout_failed")
		return
	}
	response.OK(c, gin.H{"revoked": revoked})
}

type forgotPasswordRequest struct {
	Email string `json:"email" binding:"required,email"`
}

func (h Handler) RequestPasswordReset(c *gin.Context) {
	var req forgotPasswordRequest
	if c.ShouldBindJSON(&req) != nil {
		response.Error(c, 422, 42213, "error.valid_email_required")
		return
	}
	email, valid := normalizeUserEmail(req.Email)
	if !valid {
		response.Error(c, 422, 42213, "error.valid_email_required")
		return
	}
	var user model.User
	if h.DB.Where("email = ? AND status = ?", email, "active").First(&user).Error == nil {
		token, hash, err := randomRefreshToken()
		if err == nil {
			reset := model.PasswordResetToken{Base: model.Base{ID: uuid.New()}, UserID: user.ID, TokenHash: hash, ExpiresAt: time.Now().Add(30 * time.Minute)}
			delivery := model.NotificationDelivery{Base: model.Base{ID: uuid.New()}, IdempotencyKey: "password-reset:" + reset.ID.String(), Channel: "email", Recipient: user.Email, Subject: "重置 LinLinQi 登录密码", Status: "queued"}
			body := h.Cfg.UserAppURL + "/auth/reset?token=" + token
			ciphertext, nonce, _, encryptErr := h.Vault.Encrypt(body, delivery.ID[:])
			if encryptErr == nil {
				delivery.BodyCipher, delivery.BodyNonce = ciphertext, nonce
				if txErr := h.DB.Transaction(func(tx *gorm.DB) error {
					if err := tx.Create(&reset).Error; err != nil {
						return err
					}
					return tx.Create(&delivery).Error
				}); txErr == nil {
					client := queue.NewClient(h.Cfg, h.DB)
					_, enqueueErr := client.Enqueue(queue.TypeNotificationDispatch, map[string]string{"delivery_id": delivery.ID.String()})
					_ = client.Close()
					if enqueueErr != nil {
						h.DB.Model(&delivery).Update("last_error", "initial queue enqueue unavailable; scheduler will retry")
					}
				}
			}
		}
	}
	response.OK(c, gin.H{"accepted": true, "message": i18n.Localize(c, "notice.password_reset_sent")})
}

type resetPasswordRequest struct {
	Token    string `json:"token" binding:"required"`
	Password string `json:"password" binding:"required,min=8,max=72"`
}

func (h Handler) ResetPassword(c *gin.Context) {
	var req resetPasswordRequest
	if c.ShouldBindJSON(&req) != nil || len(req.Token) < 40 || len(req.Token) > 100 {
		response.Error(c, 422, 42214, "error.reset_parameters_invalid")
		return
	}
	if validateUserPassword(req.Password) != nil {
		response.Error(c, 422, 42214, "error.password_policy_not_met")
		return
	}
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		var reset model.PasswordResetToken
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("token_hash = ? AND used_at IS NULL AND expires_at > ?", refreshHash(req.Token), time.Now()).First(&reset).Error; err != nil {
			return err
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		now := time.Now()
		if err := tx.Model(&model.User{}).Where("id = ?", reset.UserID).Updates(map[string]any{"password_hash": string(hash), "session_version": gorm.Expr("session_version + 1")}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.PasswordResetToken{}).Where("user_id = ? AND used_at IS NULL", reset.UserID).Update("used_at", &now).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.UserSession{}).Where("user_id = ? AND revoked_at IS NULL", reset.UserID).Update("revoked_at", &now).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.UserSessionToken{}).Where("user_id = ? AND revoked_at IS NULL", reset.UserID).Update("revoked_at", &now).Error; err != nil {
			return err
		}
		details, _ := json.Marshal(map[string]string{"reason": "password_reset", "request_id": truncateSecurityValue(c.GetString("request_id"), 64)})
		return tx.Create(&model.SecurityEvent{
			EventType: "auth.password_reset", Severity: "info", Realm: "user", PrincipalID: &reset.UserID,
			IP: truncateSecurityValue(c.ClientIP(), 64), UserAgent: truncateSecurityValue(c.Request.UserAgent(), 500),
			Details: string(details), Resolved: true, ResolvedAt: &now,
		}).Error
	})
	if err != nil {
		response.Error(c, 422, 42215, "error.reset_link_invalid_or_expired")
		return
	}
	response.OK(c, gin.H{"reset": true})
}
