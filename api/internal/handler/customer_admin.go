package handler

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/service"
	"linlinqi/api/pkg/response"
)

type adminCustomerListItem struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	Nickname    string    `json:"nickname"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
	Status      string    `json:"status"`
	LastLoginAt time.Time `json:"last_login_at"`
	CreatedAt   time.Time `json:"created_at"`
	OrderCount  int64     `json:"order_count"`
	NetSpend    int64     `json:"net_spend"`
}

type adminCustomerUserDTO struct {
	ID              uuid.UUID `json:"id"`
	Email           string    `json:"email"`
	Nickname        string    `json:"nickname"`
	AvatarURL       string    `json:"avatar_url,omitempty"`
	Status          string    `json:"status"`
	PreferredLocale string    `json:"preferred_locale"`
	LastLoginAt     time.Time `json:"last_login_at"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func toAdminCustomerUserDTO(user model.User) adminCustomerUserDTO {
	return adminCustomerUserDTO{
		ID: user.ID, Email: user.Email, Nickname: user.Nickname, AvatarURL: user.AvatarURL,
		Status: user.Status, PreferredLocale: user.PreferredLocale, LastLoginAt: user.LastLoginAt,
		CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt,
	}
}

// adminWalletCustomerListItem intentionally exposes only the identity needed
// to select a wallet owner plus wallet balances. A wallet-only operator must
// not receive order, membership, session or login-audit data from this route.
type adminWalletCustomerListItem struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Nickname  string    `json:"nickname"`
	AvatarURL string    `json:"avatar_url,omitempty"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	Balance   int64     `json:"balance"`
	Frozen    int64     `json:"frozen"`
	Currency  string    `json:"currency"`
}

type adminWalletUserSummary struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Nickname  string    `json:"nickname"`
	AvatarURL string    `json:"avatar_url,omitempty"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func applyAdminCustomerFilters(query *gorm.DB, c *gin.Context) *gorm.DB {
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + strings.ToLower(keyword) + "%"
		query = query.Where("LOWER(u.email) LIKE ? OR LOWER(u.nickname) LIKE ?", like, like)
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		query = query.Where("u.status = ?", status)
	}
	return query
}

func adminCustomerStatisticsSQL() string {
	refunds := `SELECT order_id, COALESCE(SUM(order_amount), 0) AS refunded FROM refunds WHERE deleted_at IS NULL AND status = 'succeeded' GROUP BY order_id`
	return `SELECT o.user_id, COUNT(*) AS order_count, COALESCE(SUM(CASE WHEN o.payment_status IN ('paid','partially_refunded','refunded') THEN GREATEST(o.total - COALESCE(r.refunded, 0), 0) ELSE 0 END), 0) AS net_spend FROM orders o LEFT JOIN (` + refunds + `) r ON r.order_id = o.id WHERE o.deleted_at IS NULL AND o.user_id IS NOT NULL AND o.currency = ? GROUP BY o.user_id`
}

func selectAdminCustomerList(query *gorm.DB, currencyCode string) *gorm.DB {
	return query.Select("u.id, u.email, u.nickname, u.avatar_url, u.status, u.last_login_at, u.created_at, COALESCE(os.order_count, 0) AS order_count, COALESCE(os.net_spend, 0) AS net_spend").
		Joins("LEFT JOIN ("+adminCustomerStatisticsSQL()+") os ON os.user_id = u.id", currencyCode)
}

func (h Handler) AdminCustomers(c *gin.Context) {
	currencyCode, err := service.StoreCurrency(h.DB)
	if err != nil {
		response.Error(c, 500, 50041, "error.store_currency_fetch_failed")
		return
	}
	page, pageSize := pagination(c)
	query := applyAdminCustomerFilters(h.DB.Table("users u").Where("u.deleted_at IS NULL"), c)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50041, "error.customer_count_fetch_failed")
		return
	}
	var items []adminCustomerListItem
	err = selectAdminCustomerList(query, currencyCode).
		Order("u.created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&items).Error
	if err != nil {
		response.Error(c, 500, 50041, "error.customer_list_fetch_failed")
		return
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) AdminWalletCustomers(c *gin.Context) {
	currencyCode, err := service.StoreCurrency(h.DB)
	if err != nil {
		response.Error(c, 500, 50041, "error.store_currency_fetch_failed")
		return
	}
	page, pageSize := pagination(c)
	query := applyAdminCustomerFilters(h.DB.Table("users u").Where("u.deleted_at IS NULL"), c)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50056, "error.wallet_fetch_failed")
		return
	}
	items := make([]adminWalletCustomerListItem, 0)
	err = query.
		Select("u.id, u.email, u.nickname, u.avatar_url, u.status, u.created_at, COALESCE(wa.balance, 0) AS balance, COALESCE(wa.frozen, 0) AS frozen").
		Joins("LEFT JOIN wallet_accounts wa ON wa.owner_type = ? AND wa.owner_id = u.id AND wa.currency = ? AND wa.deleted_at IS NULL", "user", currencyCode).
		Order("u.created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&items).Error
	if err != nil {
		response.Error(c, 500, 50056, "error.wallet_fetch_failed")
		return
	}
	for index := range items {
		items[index].Currency = currencyCode
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) AdminWalletCustomerDetail(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42250, "error.customer_number_invalid")
		return
	}
	var user adminWalletUserSummary
	if err := h.DB.Table("users").
		Select("id, email, nickname, avatar_url, status, created_at").
		Where("id = ? AND deleted_at IS NULL", userID).Scan(&user).Error; err != nil {
		response.Error(c, 500, 50056, "error.wallet_fetch_failed")
		return
	}
	if user.ID == uuid.Nil {
		response.Error(c, 404, 40441, "error.customer_not_found")
		return
	}
	currencyCode, err := service.StoreCurrency(h.DB)
	if err != nil {
		response.Error(c, 500, 50041, "error.store_currency_fetch_failed")
		return
	}
	var wallet model.WalletAccount
	err = h.DB.Where("owner_type = ? AND owner_id = ? AND currency = ?", "user", userID, currencyCode).First(&wallet).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 500, 50056, "error.wallet_fetch_failed")
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		wallet = model.WalletAccount{OwnerType: "user", OwnerID: userID, Currency: currencyCode}
	}
	ledgerPage, ledgerPageSize := pagination(c)
	entries := make([]model.WalletEntry, 0)
	var ledgerTotal int64
	if wallet.ID != uuid.Nil {
		if err := h.DB.Model(&model.WalletEntry{}).Where("account_id = ?", wallet.ID).Count(&ledgerTotal).Error; err != nil {
			response.Error(c, 500, 50056, "error.wallet_fetch_failed")
			return
		}
		if err := h.DB.Where("account_id = ?", wallet.ID).Order("created_at DESC").Offset((ledgerPage - 1) * ledgerPageSize).Limit(ledgerPageSize).Find(&entries).Error; err != nil {
			response.Error(c, 500, 50056, "error.wallet_fetch_failed")
			return
		}
	}
	response.OK(c, gin.H{
		"user":           user,
		"wallet":         wallet,
		"wallet_entries": gin.H{"items": entries, "total": ledgerTotal, "page": ledgerPage, "page_size": ledgerPageSize},
	})
}

func (h Handler) ExportAdminCustomers(c *gin.Context) {
	reason, ok := requireAdminChangeReason(c, "导出客户")
	if !ok {
		return
	}
	query := applyAdminCustomerFilters(h.DB.Table("users u").Where("u.deleted_at IS NULL"), c)
	currencyCode, err := service.StoreCurrency(h.DB)
	if err != nil {
		response.Error(c, 500, 50041, "error.store_currency_fetch_failed")
		return
	}
	var items []adminCustomerListItem
	if err := selectAdminCustomerList(query, currencyCode).Order("u.created_at DESC").Limit(50001).Scan(&items).Error; err != nil {
		response.Error(c, 500, 50041, "error.customer_export_fetch_failed")
		return
	}
	truncated := len(items) > 50000
	if truncated {
		items = items[:50000]
	}
	var currencyDefinition model.CurrencyDefinition
	if err := h.DB.Where("code = ?", currencyCode).First(&currencyDefinition).Error; err != nil {
		response.Error(c, 500, 50041, "error.currency_definition_missing")
		return
	}
	h.audit(c, "customer.export", "user", "", fmt.Sprintf("%s；rows=%d；truncated=%t", reason, len(items), truncated))
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="linlinqi-customers-`+time.Now().UTC().Format("20060102T150405Z")+`.csv"`)
	c.Header("Cache-Control", "no-store")
	if truncated {
		c.Header("X-Export-Truncated", "true")
	}
	c.Status(http.StatusOK)
	_, _ = c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(c.Writer)
	_ = writer.Write([]string{"客户邮箱", "昵称", "状态", "订单数", "净消费", "币种", "注册时间", "最近登录"})
	for _, item := range items {
		_ = writer.Write([]string{
			protectCSVCell(item.Email), protectCSVCell(item.Nickname), item.Status,
			fmt.Sprintf("%d", item.OrderCount), formatCSVAmount(item.NetSpend, currencyDefinition.MinorUnit), currencyCode,
			item.CreatedAt.UTC().Format(time.RFC3339), item.LastLoginAt.UTC().Format(time.RFC3339),
		})
	}
	writer.Flush()
}

// ExportAdminWalletCustomers is intentionally separate from the customer
// operations export. It preserves wallet operations without granting
// wallet.view access to order, login, membership or session data.
func (h Handler) ExportAdminWalletCustomers(c *gin.Context) {
	reason, ok := requireAdminChangeReason(c, "导出客户钱包")
	if !ok {
		return
	}
	currencyCode, err := service.StoreCurrency(h.DB)
	if err != nil {
		response.Error(c, 500, 50041, "error.store_currency_fetch_failed")
		return
	}
	query := applyAdminCustomerFilters(h.DB.Table("users u").Where("u.deleted_at IS NULL"), c)
	items := make([]adminWalletCustomerListItem, 0)
	if err := query.
		Select("u.id, u.email, u.nickname, u.avatar_url, u.status, u.created_at, COALESCE(wa.balance, 0) AS balance, COALESCE(wa.frozen, 0) AS frozen").
		Joins("LEFT JOIN wallet_accounts wa ON wa.owner_type = ? AND wa.owner_id = u.id AND wa.currency = ? AND wa.deleted_at IS NULL", "user", currencyCode).
		Order("u.created_at DESC").Limit(50001).Scan(&items).Error; err != nil {
		response.Error(c, 500, 50056, "error.wallet_fetch_failed")
		return
	}
	truncated := len(items) > 50000
	if truncated {
		items = items[:50000]
	}
	var currencyDefinition model.CurrencyDefinition
	if err := h.DB.Where("code = ?", currencyCode).First(&currencyDefinition).Error; err != nil {
		response.Error(c, 500, 50041, "error.currency_definition_missing")
		return
	}
	h.audit(c, "wallet.export", "wallet_account", "", fmt.Sprintf("%s；rows=%d；truncated=%t", reason, len(items), truncated))
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="linlinqi-wallets-`+time.Now().UTC().Format("20060102T150405Z")+`.csv"`)
	c.Header("Cache-Control", "no-store")
	if truncated {
		c.Header("X-Export-Truncated", "true")
	}
	c.Status(http.StatusOK)
	_, _ = c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(c.Writer)
	_ = writer.Write([]string{"客户邮箱", "昵称", "状态", "钱包余额", "冻结金额", "币种", "注册时间"})
	for _, item := range items {
		_ = writer.Write([]string{
			protectCSVCell(item.Email), protectCSVCell(item.Nickname), item.Status,
			formatCSVAmount(item.Balance, currencyDefinition.MinorUnit), formatCSVAmount(item.Frozen, currencyDefinition.MinorUnit),
			currencyCode, item.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	writer.Flush()
}

func (h Handler) AdminCustomerDetail(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42250, "error.customer_number_invalid")
		return
	}
	var user model.User
	if err := h.DB.First(&user, "id = ?", userID).Error; err != nil {
		response.Error(c, 404, 40441, "error.customer_not_found")
		return
	}
	if _, _, err := service.ReconcileUserMembership(h.DB, userID, time.Now().UTC()); err != nil {
		response.Error(c, 500, 50605, "error.customer_membership_recalculation_failed")
		return
	}
	orders := make([]model.Order, 0)
	sessions := make([]model.UserSession, 0)
	loginEvents := make([]model.LoginEvent, 0)
	h.DB.Preload("Items").Where("user_id = ?", user.ID).Order("created_at DESC").Limit(20).Find(&orders)
	h.DB.Where("user_id = ?", user.ID).Order("last_active_at DESC").Limit(20).Find(&sessions)
	h.DB.Where("realm = ? AND principal_id = ?", "user", user.ID).Order("created_at DESC").Limit(30).Find(&loginEvents)
	var membership model.UserLevelMembership
	var memberLevel model.MemberLevel
	if h.DB.Where("user_id = ?", user.ID).First(&membership).Error == nil {
		h.DB.First(&memberLevel, "id = ?", membership.MemberLevelID)
	}
	var affiliate model.AffiliateProfile
	var reseller model.ResellerProfile
	h.DB.Where("user_id = ?", user.ID).First(&affiliate)
	h.DB.Where("user_id = ?", user.ID).First(&reseller)
	var orderCount, ticketCount int64
	var netSpend int64
	h.DB.Model(&model.Order{}).Where("user_id = ?", user.ID).Count(&orderCount)
	h.DB.Model(&model.SupportTicket{}).Where("user_id = ?", user.ID).Count(&ticketCount)
	if calculated, spendErr := service.UserNetSpend(h.DB, user.ID); spendErr == nil {
		netSpend = calculated
	} else {
		response.Error(c, 500, 50041, "error.customer_statistics_fetch_failed")
		return
	}
	response.OK(c, gin.H{
		"user":          toAdminCustomerUserDTO(user),
		"recent_orders": orders, "sessions": sessions, "login_events": loginEvents,
		"membership": membership, "member_level": memberLevel, "affiliate": affiliate, "reseller": reseller,
		"statistics": gin.H{"order_count": orderCount, "ticket_count": ticketCount, "net_spend": netSpend},
	})
}

type adminCustomerStatusRequest struct {
	Status string `json:"status"`
}

func (h Handler) UpdateAdminCustomerStatus(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42250, "error.customer_number_invalid")
		return
	}
	var req adminCustomerStatusRequest
	if decodeStrictJSON(c, &req) != nil || (req.Status != "active" && req.Status != "disabled") {
		response.Error(c, 422, 42251, "error.customer_status_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "变更客户状态")
	if !ok {
		return
	}
	adminID, _ := uuid.Parse(c.GetString("subject"))
	now := time.Now()
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", userID).Error; err != nil {
			return err
		}
		if user.Status == req.Status {
			return fmt.Errorf("customer status is unchanged")
		}
		if err := tx.Model(&user).Update("status", req.Status).Error; err != nil {
			return err
		}
		if req.Status == "disabled" {
			if err := tx.Model(&model.UserSession{}).Where("user_id = ? AND revoked_at IS NULL", user.ID).Update("revoked_at", &now).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.UserSessionToken{}).Where("user_id = ? AND revoked_at IS NULL", user.ID).Update("revoked_at", &now).Error; err != nil {
				return err
			}
		}
		details, _ := json.Marshal(gin.H{"status": req.Status, "reason": reason})
		return tx.Create(&model.SecurityEvent{EventType: "customer.status_changed", Severity: "warning", Realm: "user", PrincipalID: &user.ID, Details: string(details), Resolved: true, ResolvedBy: &adminID, ResolvedAt: &now}).Error
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Error(c, 404, 40441, "error.customer_not_found")
		} else {
			response.Error(c, 409, 40944, "error.customer_status_unchanged")
		}
		return
	}
	h.audit(c, "customer.status.update", "user", userID.String(), reason+"；status="+req.Status)
	response.OK(c, gin.H{"status": req.Status, "sessions_revoked": req.Status == "disabled"})
}

type adminWalletAdjustmentRequest struct {
	Amount         int64  `json:"amount"`
	Description    string `json:"description"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (h Handler) CreateAdminWalletAdjustment(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42250, "error.customer_number_invalid")
		return
	}
	var req adminWalletAdjustmentRequest
	if decodeStrictJSON(c, &req) != nil || req.Amount == 0 || req.Amount < -100_000_000 || req.Amount > 100_000_000 {
		response.Error(c, 422, 42253, "error.ledger_amount_limit_exceeded")
		return
	}
	req.Description = strings.TrimSpace(req.Description)
	if len([]rune(req.Description)) < 4 || len([]rune(req.Description)) > 500 {
		response.Error(c, 422, 42253, "error.ledger_summary_length")
		return
	}
	idempotencyID, err := uuid.Parse(strings.TrimSpace(req.IdempotencyKey))
	if err != nil {
		response.Error(c, 422, 42253, "error.idempotency_key_uuid_required")
		return
	}
	reason, ok := requireAdminChangeReason(c, "调整客户钱包")
	if !ok {
		return
	}
	adminID, _ := uuid.Parse(c.GetString("subject"))
	currencyCode, err := service.StoreCurrency(h.DB)
	if err != nil {
		response.Error(c, 500, 50041, "error.store_currency_fetch_failed")
		return
	}
	entryNo := "LQWA" + strings.ToUpper(strings.ReplaceAll(idempotencyID.String(), "-", ""))
	var entry *model.WalletEntry
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Select("id").First(&user, "id = ?", userID).Error; err != nil {
			return err
		}
		candidate := model.WalletAccount{Base: model.Base{ID: uuid.New()}, OwnerType: "user", OwnerID: user.ID, Currency: currencyCode}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&candidate).Error; err != nil {
			return err
		}
		var account model.WalletAccount
		if err := tx.Where("owner_type = ? AND owner_id = ? AND currency = ?", "user", user.ID, currencyCode).First(&account).Error; err != nil {
			return err
		}
		var mutationErr error
		entry, mutationErr = service.ApplyWalletMutation(tx, service.WalletMutation{
			EntryNo: entryNo, AccountID: account.ID, Amount: req.Amount, Type: "admin_adjustment",
			ReferenceType: "admin", ReferenceID: &adminID, Description: req.Description,
		})
		return mutationErr
	})
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			response.Error(c, 404, 40441, "error.customer_not_found")
		case errors.Is(err, service.ErrInsufficientBalance):
			response.Error(c, 409, 40945, "error.balance_cannot_be_negative")
		case errors.Is(err, service.ErrIdempotencyConflict):
			response.Error(c, 409, 40946, "error.idempotency_key_used_for_different_ledger")
		default:
			response.Error(c, 500, 50042, "error.customer_wallet_adjust_failed")
		}
		return
	}
	h.audit(c, "wallet.admin_adjustment", "wallet_entry", entry.ID.String(), reason+"；entry="+entry.EntryNo)
	response.Created(c, entry)
}
