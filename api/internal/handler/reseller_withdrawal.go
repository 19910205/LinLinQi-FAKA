package handler

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/service"
	"linlinqi/api/pkg/response"
)

type resellerWithdrawalRequest struct {
	Amount  int64  `json:"amount"`
	Method  string `json:"method"`
	Account string `json:"account"`
}

type resellerWithdrawalDTO struct {
	ID              uuid.UUID  `json:"id"`
	WithdrawalNo    string     `json:"withdrawal_no"`
	Amount          int64      `json:"amount"`
	Fee             int64      `json:"fee"`
	Method          string     `json:"method"`
	AccountPreview  string     `json:"account_preview"`
	Status          string     `json:"status"`
	PayoutReference string     `json:"payout_reference,omitempty"`
	Reason          string     `json:"reason,omitempty"`
	ProcessedAt     *time.Time `json:"processed_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func toResellerWithdrawalDTO(item model.ResellerWithdrawal) resellerWithdrawalDTO {
	return resellerWithdrawalDTO{
		ID: item.ID, WithdrawalNo: item.WithdrawalNo, Amount: item.Amount, Fee: item.Fee,
		Method: item.Method, AccountPreview: item.AccountPreview, Status: item.Status,
		PayoutReference: item.PayoutReference, Reason: item.Reason, ProcessedAt: item.ProcessedAt,
		CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func validPayoutAccount(value string) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 3 || utf8.RuneCountInString(value) > 255 {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func resellerWithdrawalMinimum(db *gorm.DB) int64 {
	minimum := int64(10_000)
	var setting model.Setting
	if db.Select("value").First(&setting, "key = ?", "reseller_withdrawal_minimum").Error == nil {
		if parsed, err := strconv.ParseInt(setting.Value, 10, 64); err == nil && parsed >= 100 && parsed <= 100_000_000 {
			minimum = parsed
		}
	}
	return minimum
}

func (h Handler) MyResellerWithdrawals(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	page, pageSize := pagination(c)
	var profile model.ResellerProfile
	if err := h.DB.Select("id").Where("user_id = ?", userID).First(&profile).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Page(c, []resellerWithdrawalDTO{}, 0, page, pageSize)
			return
		}
		response.Error(c, 500, 50124, "error.reseller_withdrawal_list_fetch_failed")
		return
	}
	query := h.DB.Model(&model.ResellerWithdrawal{}).Where("reseller_id = ?", profile.ID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50124, "error.reseller_withdrawal_list_fetch_failed")
		return
	}
	var rows []model.ResellerWithdrawal
	if err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		response.Error(c, 500, 50124, "error.reseller_withdrawal_list_fetch_failed")
		return
	}
	items := make([]resellerWithdrawalDTO, 0, len(rows))
	for _, item := range rows {
		items = append(items, toResellerWithdrawalDTO(item))
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) RequestResellerWithdrawal(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req resellerWithdrawalRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42340, "error.dealer_withdrawal_format_invalid")
		return
	}
	req.Method = strings.ToLower(strings.TrimSpace(req.Method))
	req.Account = strings.TrimSpace(req.Account)
	methodAllowed := req.Method == "alipay" || req.Method == "bank" || req.Method == "usdt"
	minimum := resellerWithdrawalMinimum(h.DB)
	if !methodAllowed || !validPayoutAccount(req.Account) || req.Amount < minimum || req.Amount > 100_000_000 {
		response.Error(c, 422, 42340, "error.withdrawal_details_invalid", map[string]interface{}{"Min": fmt.Sprintf("%.2f", float64(minimum)/100)})
		return
	}
	currencyCode, currencyErr := service.StoreCurrency(h.DB)
	if currencyErr != nil {
		response.Error(c, 500, 50125, "error.store_currency_fetch_failed")
		return
	}
	withdrawal := model.ResellerWithdrawal{
		Base:         model.Base{ID: uuid.New()},
		WithdrawalNo: "LQRW" + time.Now().UTC().Format("20060102150405") + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:8]),
		Amount:       req.Amount, Currency: currencyCode, Fee: 0, Method: req.Method, AccountPreview: previewPayoutAccount(req.Account), Status: "pending",
	}
	ciphertext, nonce, _, err := h.Vault.Encrypt(req.Account, withdrawal.ID[:])
	req.Account = ""
	if err != nil {
		response.Error(c, 500, 50125, "error.withdrawal_account_encrypt_failed")
		return
	}
	withdrawal.AccountCipher, withdrawal.AccountNonce = ciphertext, nonce
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var profile model.ResellerProfile
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&profile).Error; err != nil {
			return err
		}
		if profile.Status != "active" {
			return errors.New("reseller profile is not active")
		}
		var account model.WalletAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("owner_type = ? AND owner_id = ? AND currency = ?", "reseller", profile.ID, withdrawal.Currency).First(&account).Error; err != nil {
			return err
		}
		if account.Frozen < 0 || account.Balance < account.Frozen || account.Balance-account.Frozen < withdrawal.Amount {
			return service.ErrInsufficientBalance
		}
		withdrawal.ResellerID = profile.ID
		if err := tx.Model(&account).Updates(map[string]any{
			"frozen": gorm.Expr("frozen + ?", withdrawal.Amount), "version": gorm.Expr("version + 1"),
		}).Error; err != nil {
			return err
		}
		return tx.Create(&withdrawal).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40499, "error.dealer_account_or_wallet_not_found")
		return
	}
	if errors.Is(err, service.ErrInsufficientBalance) {
		response.Error(c, 409, 40999, "error.dealer_wallet_balance_insufficient")
		return
	}
	if err != nil {
		response.Error(c, 409, 40999, "error.dealer_withdrawal_not_allowed")
		return
	}
	response.Created(c, gin.H{"withdrawal": toResellerWithdrawalDTO(withdrawal)})
}

type adminResellerWithdrawalDTO struct {
	resellerWithdrawalDTO
	ResellerID   uuid.UUID `json:"reseller_id"`
	ResellerCode string    `json:"reseller_code"`
	ResellerName string    `json:"reseller_name"`
	UserEmail    string    `json:"user_email"`
}

type resellerWithdrawalRow struct {
	model.ResellerWithdrawal
	ResellerCode string
	ResellerName string
	UserEmail    string
}

func toAdminResellerWithdrawalDTO(row resellerWithdrawalRow) adminResellerWithdrawalDTO {
	return adminResellerWithdrawalDTO{
		resellerWithdrawalDTO: toResellerWithdrawalDTO(row.ResellerWithdrawal),
		ResellerID:            row.ResellerID, ResellerCode: row.ResellerCode,
		ResellerName: row.ResellerName, UserEmail: row.UserEmail,
	}
}

func (h Handler) AdminResellerWithdrawals(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Table("reseller_withdrawals rw").
		Select("rw.*, rp.code AS reseller_code, rp.name AS reseller_name, u.email AS user_email").
		Joins("JOIN reseller_profiles rp ON rp.id = rw.reseller_id AND rp.deleted_at IS NULL").
		Joins("JOIN users u ON u.id = rp.user_id AND u.deleted_at IS NULL").
		Where("rw.deleted_at IS NULL")
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		query = query.Where("rw.status = ?", status)
	}
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("rw.withdrawal_no ILIKE ? OR rp.code ILIKE ? OR rp.name ILIKE ? OR u.email ILIKE ?", like, like, like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50126, "error.reseller_withdrawal_operation_queue_fetch_failed")
		return
	}
	var rows []resellerWithdrawalRow
	if err := query.Order("rw.created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error; err != nil {
		response.Error(c, 500, 50126, "error.reseller_withdrawal_operation_queue_fetch_failed")
		return
	}
	items := make([]adminResellerWithdrawalDTO, 0, len(rows))
	for _, item := range rows {
		items = append(items, toAdminResellerWithdrawalDTO(item))
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) ResellerWithdrawalDetail(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42340, "error.withdrawal_record_id_invalid")
		return
	}
	var row resellerWithdrawalRow
	if err := h.DB.Table("reseller_withdrawals rw").
		Select("rw.*, rp.code AS reseller_code, rp.name AS reseller_name, u.email AS user_email").
		Joins("JOIN reseller_profiles rp ON rp.id = rw.reseller_id AND rp.deleted_at IS NULL").
		Joins("JOIN users u ON u.id = rp.user_id AND u.deleted_at IS NULL").
		Where("rw.deleted_at IS NULL AND rw.id = ?", id).Scan(&row).Error; err != nil || row.ID == uuid.Nil {
		response.Error(c, 404, 40499, "error.dealer_withdrawal_not_found")
		return
	}
	response.OK(c, gin.H{"withdrawal": toAdminResellerWithdrawalDTO(row)})
}

func (h Handler) RevealResellerWithdrawalAccount(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42340, "error.withdrawal_record_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "查看经销商提现账户")
	if !ok {
		return
	}
	var withdrawal model.ResellerWithdrawal
	if err := h.DB.First(&withdrawal, "id = ?", id).Error; err != nil {
		response.Error(c, 404, 40499, "error.dealer_withdrawal_not_found")
		return
	}
	if len(withdrawal.AccountCipher) == 0 || len(withdrawal.AccountNonce) == 0 {
		response.Error(c, 409, 40999, "error.withdrawal_payout_account_missing")
		return
	}
	account, err := h.Vault.Decrypt(withdrawal.AccountCipher, withdrawal.AccountNonce, withdrawal.ID[:])
	if err != nil {
		response.Error(c, 500, 50125, "error.withdrawal_account_decrypt_failed")
		return
	}
	h.audit(c, "reseller.withdrawal.account.reveal", "reseller_withdrawal", id.String(), reason)
	response.OK(c, gin.H{"account": account, "account_preview": withdrawal.AccountPreview})
}

type resellerWithdrawalUpdateRequest struct {
	Status          string `json:"status"`
	PayoutReference string `json:"payout_reference"`
}

func (h Handler) UpdateResellerWithdrawal(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42340, "error.withdrawal_record_id_invalid")
		return
	}
	adminID, err := uuid.Parse(c.GetString("subject"))
	if err != nil {
		response.Error(c, 401, 40140, "error.invalid_login_state")
		return
	}
	reason, ok := requireAdminChangeReason(c, "处理经销商提现")
	if !ok {
		return
	}
	var req resellerWithdrawalUpdateRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42340, "error.withdrawal_process_format_invalid")
		return
	}
	req.Status = strings.ToLower(strings.TrimSpace(req.Status))
	req.PayoutReference = strings.TrimSpace(req.PayoutReference)
	if (req.Status != "processing" && req.Status != "completed" && req.Status != "rejected") ||
		(req.Status == "completed" && (utf8.RuneCountInString(req.PayoutReference) < 4 || utf8.RuneCountInString(req.PayoutReference) > 160)) {
		response.Error(c, 422, 42340, "error.withdrawal_status_transaction_invalid")
		return
	}
	var withdrawal model.ResellerWithdrawal
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&withdrawal, "id = ?", id).Error; err != nil {
			return err
		}
		allowed := (withdrawal.Status == "pending" && (req.Status == "processing" || req.Status == "completed" || req.Status == "rejected")) ||
			(withdrawal.Status == "processing" && (req.Status == "completed" || req.Status == "rejected"))
		if !allowed {
			return errors.New("invalid reseller withdrawal transition")
		}
		var wallet model.WalletAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("owner_type = ? AND owner_id = ? AND currency = ?", "reseller", withdrawal.ResellerID, withdrawal.Currency).First(&wallet).Error; err != nil {
			return err
		}
		if wallet.Frozen < withdrawal.Amount || wallet.Balance < withdrawal.Amount {
			return errors.New("reseller frozen balance is inconsistent")
		}
		if req.Status == "completed" {
			if _, err := service.ApplyWalletMutation(tx, service.WalletMutation{
				EntryNo: "LQW-RW-" + withdrawal.ID.String(), AccountID: wallet.ID, Amount: -withdrawal.Amount,
				Type: "reseller_withdrawal", ReferenceType: "reseller_withdrawal", ReferenceID: &withdrawal.ID,
				Description: "经销商提现 " + withdrawal.WithdrawalNo,
			}); err != nil {
				return err
			}
			if err := tx.Model(&wallet).Updates(map[string]any{
				"frozen": gorm.Expr("frozen - ?", withdrawal.Amount), "version": gorm.Expr("version + 1"),
			}).Error; err != nil {
				return err
			}
		} else if req.Status == "rejected" {
			if err := tx.Model(&wallet).Updates(map[string]any{
				"frozen": gorm.Expr("frozen - ?", withdrawal.Amount), "version": gorm.Expr("version + 1"),
			}).Error; err != nil {
				return err
			}
		}
		updates := map[string]any{"status": req.Status, "processed_by": &adminID, "reason": reason}
		if req.PayoutReference != "" {
			updates["payout_reference"] = req.PayoutReference
		}
		if req.Status == "completed" || req.Status == "rejected" {
			now := time.Now()
			updates["processed_at"] = &now
		}
		return tx.Model(&withdrawal).Updates(updates).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40499, "error.dealer_withdrawal_not_found")
		return
	}
	if err != nil {
		response.Error(c, 409, 40999, "error.withdrawal_state_changed_refresh_retry")
		return
	}
	h.audit(c, "reseller.withdrawal.update", "reseller_withdrawal", id.String(), reason+"；status="+req.Status)
	response.OK(c, gin.H{"id": id, "status": req.Status})
}
