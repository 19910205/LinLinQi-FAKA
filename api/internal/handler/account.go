package handler

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/i18n"
	"linlinqi/api/internal/media"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/service"
	"linlinqi/api/pkg/response"
)

type userGiftCardDTO struct {
	ID             uuid.UUID  `json:"id"`
	CodePreview    string     `json:"code_preview"`
	InitialBalance int64      `json:"initial_balance"`
	Balance        int64      `json:"balance"`
	Currency       string     `json:"currency"`
	Status         string     `json:"status"`
	RedeemedAt     *time.Time `json:"redeemed_at"`
	ExpiresAt      *time.Time `json:"expires_at"`
	CreatedAt      time.Time  `json:"created_at"`
}

func toUserGiftCardDTO(card model.GiftCard) userGiftCardDTO {
	return userGiftCardDTO{
		ID: card.ID, CodePreview: card.CodePreview, InitialBalance: card.InitialBalance,
		Balance: card.Balance, Currency: card.Currency, Status: card.Status,
		RedeemedAt: card.RedeemedAt, ExpiresAt: card.ExpiresAt, CreatedAt: card.CreatedAt,
	}
}

type userGiftCardEntryDTO struct {
	ID           uuid.UUID `json:"id"`
	GiftCardID   uuid.UUID `json:"gift_card_id"`
	Amount       int64     `json:"amount"`
	BalanceAfter int64     `json:"balance_after"`
	Type         string    `json:"type"`
	CreatedAt    time.Time `json:"created_at"`
}

func toUserGiftCardEntryDTO(entry model.GiftCardEntry) userGiftCardEntryDTO {
	return userGiftCardEntryDTO{
		ID: entry.ID, GiftCardID: entry.GiftCardID, Amount: entry.Amount,
		BalanceAfter: entry.BalanceAfter, Type: entry.Type, CreatedAt: entry.CreatedAt,
	}
}

type userAffiliateProfileDTO struct {
	ID                   uuid.UUID  `json:"id"`
	ReferralCode         string     `json:"referral_code"`
	CommissionBasisPoint int        `json:"commission_basis_point"`
	Status               string     `json:"status"`
	TotalCommission      int64      `json:"total_commission"`
	AvailableCommission  int64      `json:"available_commission"`
	FrozenCommission     int64      `json:"frozen_commission"`
	Currency             string     `json:"currency"`
	AppliedAt            time.Time  `json:"applied_at"`
	ApprovedAt           *time.Time `json:"approved_at,omitempty"`
	RejectedAt           *time.Time `json:"rejected_at,omitempty"`
}

type userAffiliateCommissionDTO struct {
	ID             uuid.UUID  `json:"id"`
	OrderID        uuid.UUID  `json:"order_id"`
	OrderAmount    int64      `json:"order_amount"`
	Commission     int64      `json:"commission"`
	Currency       string     `json:"currency"`
	ReversedAmount int64      `json:"reversed_amount"`
	Status         string     `json:"status"`
	SettlesAt      time.Time  `json:"settles_at"`
	SettledAt      *time.Time `json:"settled_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

type userAffiliateWithdrawalDTO struct {
	ID              uuid.UUID  `json:"id"`
	WithdrawalNo    string     `json:"withdrawal_no"`
	Amount          int64      `json:"amount"`
	Fee             int64      `json:"fee"`
	Currency        string     `json:"currency"`
	Method          string     `json:"method"`
	AccountPreview  string     `json:"account_preview"`
	Status          string     `json:"status"`
	PayoutReference string     `json:"payout_reference,omitempty"`
	Reason          string     `json:"reason,omitempty"`
	ProcessedAt     *time.Time `json:"processed_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

type userSessionDTO struct {
	ID           uuid.UUID `json:"id"`
	Device       string    `json:"device"`
	IP           string    `json:"ip"`
	UserAgent    string    `json:"user_agent"`
	LastActiveAt time.Time `json:"last_active_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
}

type userAPICredentialDTO struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Key         string     `json:"key"`
	Permissions string     `json:"permissions"`
	Status      string     `json:"status"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

func toUserAPICredentialDTO(item model.APICredential) userAPICredentialDTO {
	return userAPICredentialDTO{
		ID: item.ID, Name: item.Name, Key: item.Key, Permissions: item.Permissions,
		Status: item.Status, LastUsedAt: item.LastUsedAt, RevokedAt: item.RevokedAt, CreatedAt: item.CreatedAt,
	}
}

type userWebhookEndpointDTO struct {
	ID           uuid.UUID  `json:"id"`
	URL          string     `json:"url"`
	Events       string     `json:"events"`
	Enabled      bool       `json:"enabled"`
	FailureCount int        `json:"failure_count"`
	DisabledAt   *time.Time `json:"disabled_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

func toUserWebhookEndpointDTO(item model.WebhookEndpoint) userWebhookEndpointDTO {
	return userWebhookEndpointDTO{
		ID: item.ID, URL: item.URL, Events: item.Events, Enabled: item.Enabled,
		FailureCount: item.FailureCount, DisabledAt: item.DisabledAt, CreatedAt: item.CreatedAt,
	}
}

type userAccountDTO struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	Nickname    string    `json:"nickname"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
	Balance     int64     `json:"balance"`
	Status      string    `json:"status"`
	LastLoginAt time.Time `json:"last_login_at"`
	CreatedAt   time.Time `json:"created_at"`
}

func toUserAccountDTO(user model.User) userAccountDTO {
	return userAccountDTO{
		ID: user.ID, Email: user.Email, Nickname: user.Nickname, AvatarURL: user.AvatarURL, Balance: user.Balance,
		Status: user.Status, LastLoginAt: user.LastLoginAt, CreatedAt: user.CreatedAt,
	}
}

type userMemberLevelDTO struct {
	ID                 uuid.UUID `json:"id"`
	Code               string    `json:"code"`
	Name               string    `json:"name"`
	Currency           string    `json:"currency"`
	MinimumSpend       int64     `json:"minimum_spend"`
	DiscountBasisPoint int       `json:"discount_basis_point"`
}

func toUserMemberLevelDTO(level model.MemberLevel) userMemberLevelDTO {
	return userMemberLevelDTO{
		ID: level.ID, Code: level.Code, Name: level.Name, Currency: level.Currency, MinimumSpend: level.MinimumSpend,
		DiscountBasisPoint: level.DiscountBasisPoint,
	}
}

type userWalletAccountDTO struct {
	ID              uuid.UUID `json:"id"`
	Currency        string    `json:"currency"`
	Balance         int64     `json:"balance"`
	Frozen          int64     `json:"frozen"`
	Available       int64     `json:"available"`
	MinorUnit       int       `json:"minor_unit"`
	Symbol          string    `json:"symbol"`
	CurrencyEnabled bool      `json:"currency_enabled"`
}

type userWalletEntryDTO struct {
	ID            uuid.UUID `json:"id"`
	EntryNo       string    `json:"entry_no"`
	Type          string    `json:"type"`
	Amount        int64     `json:"amount"`
	BalanceAfter  int64     `json:"balance_after"`
	Currency      string    `json:"currency"`
	ReferenceType string    `json:"reference_type,omitempty"`
	Description   string    `json:"description"`
	CreatedAt     time.Time `json:"created_at"`
}

func currentUserID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.GetString("subject"))
	if err != nil {
		response.Error(c, 401, 40140, "error.invalid_login_state")
		return uuid.Nil, false
	}
	return id, true
}

type updateMyProfileRequest struct {
	Nickname        string  `json:"nickname"`
	Email           *string `json:"email"`
	CurrentPassword string  `json:"current_password"`
	AvatarURL       *string `json:"avatar_url"`
}

func validUserNickname(value string) bool {
	runes := []rune(value)
	if len(runes) < 2 || len(runes) > 80 {
		return false
	}
	for _, character := range runes {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func normalizeUserEmail(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > 190 {
		return "", false
	}
	address, err := mail.ParseAddress(value)
	if err != nil || !strings.EqualFold(address.Address, value) {
		return "", false
	}
	return value, true
}

func (h Handler) UpdateMyProfile(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req updateMyProfileRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42255, "error.account_profile_invalid")
		return
	}
	req.Nickname = strings.TrimSpace(req.Nickname)
	if !validUserNickname(req.Nickname) {
		response.Error(c, 422, 42255, "error.nickname_invalid")
		return
	}
	updates := map[string]any{"nickname": req.Nickname}
	if req.AvatarURL != nil {
		avatarURL := strings.TrimSpace(*req.AvatarURL)
		if avatarURL != "" && (!strings.HasPrefix(avatarURL, "https://") || len(avatarURL) > 1000) {
			response.Error(c, 422, 42255, "error.account_avatar_invalid")
			return
		}
		updates["avatar_url"] = avatarURL
	}
	var requestedEmail *string
	if req.Email != nil {
		email, valid := normalizeUserEmail(*req.Email)
		if !valid {
			response.Error(c, 422, 42258, "error.account_email_invalid")
			return
		}
		requestedEmail = &email
	}
	var emailChanged bool
	updateErr := h.DB.Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND status = ?", userID, "active").First(&user).Error; err != nil {
			return err
		}
		if requestedEmail != nil && !strings.EqualFold(user.Email, *requestedEmail) {
			if req.CurrentPassword == "" || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)) != nil {
				return errCurrentPasswordInvalid
			}
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "user-email:"+*requestedEmail).Error; err != nil {
				return err
			}
			var duplicate int64
			if err := tx.Unscoped().Model(&model.User{}).Where("LOWER(email) = ? AND id <> ?", *requestedEmail, userID).Count(&duplicate).Error; err != nil {
				return err
			}
			if duplicate > 0 {
				return gorm.ErrDuplicatedKey
			}
			updates["email"] = *requestedEmail
			emailChanged = true
		}
		if err := tx.Model(&user).Updates(updates).Error; err != nil {
			return err
		}
		if emailChanged {
			details, _ := json.Marshal(gin.H{
				"email_changed": true,
				"request_id":    c.GetString("request_id"),
			})
			if err := tx.Create(&model.SecurityEvent{
				EventType: "auth.email_changed", Severity: "info", Realm: "user", PrincipalID: &userID,
				IP: truncateSecurityValue(c.ClientIP(), 64), UserAgent: truncateSecurityValue(c.Request.UserAgent(), 500),
				Details: string(details), Resolved: true,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(updateErr, errCurrentPasswordInvalid) {
		response.Error(c, 422, 42259, "error.current_password_incorrect")
		return
	}
	if errors.Is(updateErr, gorm.ErrDuplicatedKey) {
		response.Error(c, 409, 40910, "error.email_already_registered")
		return
	}
	if errors.Is(updateErr, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40441, "error.user_not_found")
		return
	}
	if updateErr != nil {
		response.Error(c, 500, 50056, "error.account_profile_update_failed")
		return
	}
	var user model.User
	if err := h.DB.First(&user, "id = ?", userID).Error; err != nil {
		response.Error(c, 500, 50056, "error.account_profile_fetch_failed")
		return
	}
	response.OK(c, gin.H{"user": toUserAccountDTO(user)})
}

// UploadMyAvatar accepts a validated image and stores it in content-addressed
// public media storage. The profile keeps only the immutable public URL.
func (h Handler) UploadMyAvatar(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.Cfg.MediaMaxImageBytes+(1<<20))
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.Error(c, 422, 42256, "error.account_avatar_file_required")
		return
	}
	defer file.Close()
	stored, err := media.New(h.Cfg).PutImage(file, header.Filename)
	if err != nil {
		response.Error(c, 422, 42257, "error.account_avatar_image_invalid")
		return
	}
	if err := h.DB.Model(&model.User{}).Where("id = ? AND status = ?", userID, "active").Update("avatar_url", stored.PublicURL).Error; err != nil {
		response.Error(c, 500, 50056, "error.account_avatar_update_failed")
		return
	}
	var user model.User
	if err := h.DB.First(&user, "id = ?", userID).Error; err != nil {
		response.Error(c, 500, 50056, "error.account_profile_fetch_failed")
		return
	}
	response.OK(c, gin.H{"user": toUserAccountDTO(user), "avatar_url": stored.PublicURL})
}

type changeMyPasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

var errCurrentPasswordInvalid = errors.New("current password invalid")

func (h Handler) ChangeMyPassword(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req changeMyPasswordRequest
	if decodeStrictJSON(c, &req) != nil || req.CurrentPassword == "" || validateUserPassword(req.NewPassword) != nil || req.CurrentPassword == req.NewPassword {
		response.Error(c, 422, 42256, "error.password_change_invalid")
		return
	}
	now := time.Now()
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND status = ?", userID, "active").First(&user).Error; err != nil {
			return err
		}
		if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)) != nil {
			return errCurrentPasswordInvalid
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		if err := tx.Model(&user).Updates(map[string]any{"password_hash": string(hash), "session_version": gorm.Expr("session_version + 1")}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.UserSession{}).Where("user_id = ? AND revoked_at IS NULL", userID).Update("revoked_at", &now).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.UserSessionToken{}).Where("user_id = ? AND revoked_at IS NULL", userID).Update("revoked_at", &now).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.PasswordResetToken{}).Where("user_id = ? AND used_at IS NULL", userID).Update("used_at", &now).Error; err != nil {
			return err
		}
		details, _ := json.Marshal(gin.H{"reason": "authenticated_password_change", "request_id": c.GetString("request_id")})
		return tx.Create(&model.SecurityEvent{
			EventType: "auth.password_changed", Severity: "info", Realm: "user", PrincipalID: &userID,
			IP: truncateSecurityValue(c.ClientIP(), 64), UserAgent: truncateSecurityValue(c.Request.UserAgent(), 500),
			Details: string(details), Resolved: true, ResolvedAt: &now,
		}).Error
	})
	if errors.Is(err, errCurrentPasswordInvalid) {
		response.Error(c, 422, 42257, "error.current_password_incorrect")
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40441, "error.user_not_found")
		return
	}
	if err != nil {
		response.Error(c, 500, 50057, "error.password_change_failed")
		return
	}
	response.OK(c, gin.H{"changed": true, "sessions_revoked": true})
}

func (h Handler) MyGiftCards(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var cards []model.GiftCard
	if err := h.DB.Where("redeemed_by = ?", userID).Order("created_at DESC").Find(&cards).Error; err != nil {
		response.Error(c, 500, 50056, "error.gift_card_record_fetch_failed")
		return
	}
	items := make([]userGiftCardDTO, 0, len(cards))
	for _, card := range cards {
		items = append(items, toUserGiftCardDTO(card))
	}
	response.OK(c, items)
}

func (h Handler) MyAffiliate(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var profile model.AffiliateProfile
	if err := h.DB.Where("user_id = ?", userID).First(&profile).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			response.Error(c, 500, 50056, "error.affiliate_account_fetch_failed")
			return
		}
		response.OK(c, gin.H{"profile": nil, "commissions": []any{}, "withdrawals": []any{}, "referral_count": int64(0)})
		return
	}
	storeCurrency, err := service.StoreCurrency(h.DB)
	if err != nil {
		response.Error(c, 500, 50056, "error.store_currency_fetch_failed")
		return
	}
	var commissions []model.AffiliateCommission
	var withdrawals []model.AffiliateWithdrawal
	var balances []model.AffiliateBalance
	var referralCount int64
	if err := h.DB.Where("affiliate_id = ?", profile.ID).Order("created_at DESC").Limit(100).Find(&commissions).Error; err != nil {
		response.Error(c, 500, 50056, "error.commission_record_fetch_failed")
		return
	}
	if err := h.DB.Where("affiliate_id = ?", profile.ID).Order("created_at DESC").Limit(100).Find(&withdrawals).Error; err != nil {
		response.Error(c, 500, 50056, "error.withdrawal_record_fetch_failed")
		return
	}
	if err := h.DB.Where("affiliate_id = ?", profile.ID).Order("currency ASC").Find(&balances).Error; err != nil {
		response.Error(c, 500, 50056, "error.affiliate_balance_fetch_failed")
		return
	}
	if err := h.DB.Model(&model.AffiliateReferral{}).Where("affiliate_id = ?", profile.ID).Count(&referralCount).Error; err != nil {
		response.Error(c, 500, 50056, "error.affiliate_stats_fetch_failed")
		return
	}
	commissionItems := make([]userAffiliateCommissionDTO, 0, len(commissions))
	for _, item := range commissions {
		commissionItems = append(commissionItems, userAffiliateCommissionDTO{
			ID: item.ID, OrderID: item.OrderID, OrderAmount: item.OrderAmount, Commission: item.Commission,
			Currency:       item.Currency,
			ReversedAmount: item.ReversedAmount, Status: item.Status, SettlesAt: item.SettlesAt,
			SettledAt: item.SettledAt, CreatedAt: item.CreatedAt,
		})
	}
	withdrawalItems := make([]userAffiliateWithdrawalDTO, 0, len(withdrawals))
	for _, item := range withdrawals {
		withdrawalItems = append(withdrawalItems, userAffiliateWithdrawalDTO{
			ID: item.ID, WithdrawalNo: item.WithdrawalNo, Amount: item.Amount, Fee: item.Fee,
			Currency: item.Currency,
			Method:   item.Method, AccountPreview: item.AccountPreview, Status: item.Status,
			PayoutReference: item.PayoutReference, Reason: item.Reason, ProcessedAt: item.ProcessedAt,
			CreatedAt: item.CreatedAt,
		})
	}
	currentBalance := model.AffiliateBalance{AffiliateID: profile.ID, Currency: storeCurrency}
	for _, balance := range balances {
		if balance.Currency == storeCurrency {
			currentBalance = balance
			break
		}
	}
	response.OK(c, gin.H{
		"profile": userAffiliateProfileDTO{
			ID: profile.ID, ReferralCode: profile.ReferralCode, CommissionBasisPoint: profile.CommissionBasisPoint,
			Status: profile.Status, TotalCommission: currentBalance.TotalCommission,
			AvailableCommission: currentBalance.AvailableCommission, FrozenCommission: currentBalance.FrozenCommission,
			Currency:  storeCurrency,
			AppliedAt: profile.AppliedAt, ApprovedAt: profile.ApprovedAt, RejectedAt: profile.RejectedAt,
		},
		"commissions": commissionItems, "withdrawals": withdrawalItems, "balances": balances,
		"referral_count": referralCount,
		"referral_link":  strings.TrimRight(h.Cfg.UserAppURL, "/") + "/auth/register?ref=" + profile.ReferralCode,
	})
}

func (h Handler) MySessions(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var sessions []model.UserSession
	if err := h.DB.Where("user_id = ? AND revoked_at IS NULL AND expires_at > ?", userID, time.Now()).Order("last_active_at DESC").Find(&sessions).Error; err != nil {
		response.Error(c, 500, 50056, "error.login_session_fetch_failed")
		return
	}
	items := make([]userSessionDTO, 0, len(sessions))
	for _, session := range sessions {
		items = append(items, userSessionDTO{
			ID: session.ID, Device: session.Device, IP: session.IP, UserAgent: session.UserAgent,
			LastActiveAt: session.LastActiveAt, ExpiresAt: session.ExpiresAt, CreatedAt: session.CreatedAt,
		})
	}
	response.OK(c, items)
}

func (h Handler) RevokeMySession(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	sessionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42253, "error.session_id_invalid")
		return
	}
	now := time.Now()
	revoked := false
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.UserSession{}).Where("id = ? AND user_id = ? AND revoked_at IS NULL", sessionID, userID).Update("revoked_at", &now)
		if result.Error != nil {
			return result.Error
		}
		revoked = result.RowsAffected > 0
		return tx.Model(&model.UserSessionToken{}).Where("user_session_id = ? AND user_id = ? AND revoked_at IS NULL", sessionID, userID).Update("revoked_at", &now).Error
	})
	if err != nil {
		response.Error(c, 500, 50056, "error.session_revoke_failed")
		return
	}
	response.OK(c, gin.H{"revoked": revoked})
}

func (h Handler) MyAPICredentials(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var items []model.APICredential
	if err := h.DB.Where("owner_type = ? AND owner_id = ?", "user", userID).Order("created_at DESC").Find(&items).Error; err != nil {
		response.Error(c, 500, 50056, "error.api_credential_fetch_failed")
		return
	}
	result := make([]userAPICredentialDTO, 0, len(items))
	for _, item := range items {
		result = append(result, toUserAPICredentialDTO(item))
	}
	response.OK(c, result)
}

type apiCredentialRequest struct {
	Name string `json:"name"`
}

const maxUserAPICredentials = 10

var (
	errAPICredentialLimit      = errors.New("API credential limit reached")
	errAPICredentialNameExists = errors.New("API credential name already exists")
	errAPICredentialRevoked    = errors.New("API credential is permanently revoked")
)

func normalizeAPICredentialName(value string) (string, bool) {
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", false
		}
	}
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	runes := []rune(value)
	if len(runes) < 2 || len(runes) > 100 {
		return "", false
	}
	return value, true
}

func lockUserAPICredentials(tx *gorm.DB, userID uuid.UUID) error {
	if tx.Dialector.Name() != "postgres" {
		return nil
	}
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "user-api-credentials:"+userID.String()).Error
}

func (h Handler) CreateMyAPICredential(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req apiCredentialRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42254, "error.credential_name_required")
		return
	}
	name, valid := normalizeAPICredentialName(req.Name)
	if !valid {
		response.Error(c, 422, 42254, "error.credential_name_required")
		return
	}
	secret, _, err := randomRefreshToken()
	if err != nil {
		response.Error(c, 500, 50055, "error.credential_generate_failed")
		return
	}
	credential := model.APICredential{Base: model.Base{ID: uuid.New()}, OwnerType: "user", OwnerID: &userID, Name: name, Key: fmt.Sprintf("linlinqi_%s", strings.ReplaceAll(uuid.NewString(), "-", "")), Permissions: "products:read,orders:write,orders:read", Status: "pending"}
	ciphertext, nonce, _, err := h.Vault.Encrypt(secret, credential.ID[:])
	if err != nil {
		response.Error(c, 500, 50055, "error.credential_generate_failed")
		return
	}
	credential.SecretCipher, credential.SecretNonce = ciphertext, nonce
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockUserAPICredentials(tx, userID); err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&model.APICredential{}).
			Where("owner_type = ? AND owner_id = ? AND status <> ?", "user", userID, "revoked").
			Count(&count).Error; err != nil {
			return err
		}
		if count >= maxUserAPICredentials {
			return errAPICredentialLimit
		}
		var duplicate int64
		if err := tx.Model(&model.APICredential{}).
			Where("owner_type = ? AND owner_id = ? AND status <> ? AND LOWER(name) = LOWER(?)", "user", userID, "revoked", name).
			Count(&duplicate).Error; err != nil {
			return err
		}
		if duplicate > 0 {
			return errAPICredentialNameExists
		}
		if err := tx.Create(&credential).Error; err != nil {
			return err
		}
		details, _ := json.Marshal(gin.H{"credential_id": credential.ID, "name": credential.Name, "request_id": c.GetString("request_id")})
		now := time.Now()
		return tx.Create(&model.SecurityEvent{
			EventType: "api_credential.created", Severity: "info", Realm: "user", PrincipalID: &userID,
			IP: truncateSecurityValue(c.ClientIP(), 64), UserAgent: truncateSecurityValue(c.Request.UserAgent(), 500),
			Details: string(details), Resolved: true, ResolvedAt: &now,
		}).Error
	})
	if errors.Is(err, errAPICredentialLimit) {
		response.Error(c, 409, 40954, "error.api_credential_limit_reached")
		return
	}
	if errors.Is(err, errAPICredentialNameExists) {
		response.Error(c, 409, 40955, "error.api_credential_name_exists")
		return
	}
	if err != nil {
		response.Error(c, 500, 50055, "error.credential_save_failed")
		return
	}
	_ = h.createOperationalNotifications(h.DB, "openapi.credential.created", credential.ID.String(), map[string]string{"entity_id": credential.ID.String(), "status": credential.Status, "email": "", "channel": "openapi", "summary": "用户创建了 OpenAPI 凭证"})
	response.Created(c, gin.H{"credential": toUserAPICredentialDTO(credential), "secret": secret, "notice": i18n.Localize(c, "notice.secret_once")})
}

func (h Handler) RevokeMyAPICredential(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	credentialID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42255, "error.api_credential_id_invalid")
		return
	}
	var credential model.APICredential
	changed := false
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockUserAPICredentials(tx, userID); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND owner_type = ? AND owner_id = ?", credentialID, "user", userID).
			First(&credential).Error; err != nil {
			return err
		}
		if credential.Status == "revoked" {
			return nil
		}
		cipherTombstone := make([]byte, len(credential.SecretCipher))
		nonceTombstone := make([]byte, len(credential.SecretNonce))
		if _, err := rand.Read(cipherTombstone); err != nil {
			return err
		}
		if _, err := rand.Read(nonceTombstone); err != nil {
			return err
		}
		now := time.Now()
		if err := tx.Model(&credential).Updates(map[string]any{
			"status": "revoked", "revoked_at": &now,
			"secret_cipher": cipherTombstone, "secret_nonce": nonceTombstone,
		}).Error; err != nil {
			return err
		}
		credential.Status, credential.RevokedAt = "revoked", &now
		changed = true
		details, _ := json.Marshal(gin.H{"credential_id": credential.ID, "name": credential.Name, "request_id": c.GetString("request_id")})
		return tx.Create(&model.SecurityEvent{
			EventType: "api_credential.revoked", Severity: "info", Realm: "user", PrincipalID: &userID,
			IP: truncateSecurityValue(c.ClientIP(), 64), UserAgent: truncateSecurityValue(c.Request.UserAgent(), 500),
			Details: string(details), Resolved: true, ResolvedAt: &now,
		}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40456, "error.api_credential_not_found")
		return
	}
	if err != nil {
		response.Error(c, 500, 50055, "error.api_credential_revoke_failed")
		return
	}
	response.OK(c, gin.H{"credential": toUserAPICredentialDTO(credential), "revoked": true, "changed": changed})
}
