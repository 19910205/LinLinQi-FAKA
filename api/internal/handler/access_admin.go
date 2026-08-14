package handler

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
	"linlinqi/api/pkg/response"
)

const accessMutationLockKey int64 = 0x4c696e4c51616363

var (
	accessAdminUsernamePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{1,78}[a-z0-9]$`)
	accessRoleCodePattern      = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,78}[a-z0-9]$`)
)

type accessNamedRoleDTO struct {
	ID   uuid.UUID `json:"id"`
	Code string    `json:"code"`
	Name string    `json:"name"`
}

type accessAdminDTO struct {
	ID          uuid.UUID            `json:"id"`
	Username    string               `json:"username"`
	Name        string               `json:"name"`
	Status      string               `json:"status"`
	RoleIDs     []uuid.UUID          `json:"role_ids"`
	Roles       []accessNamedRoleDTO `json:"roles"`
	TOTPEnabled bool                 `json:"totp_enabled"`
	LastLoginAt *time.Time           `json:"last_login_at"`
	CreatedAt   time.Time            `json:"created_at"`
	UpdatedAt   time.Time            `json:"updated_at"`
}

type accessRoleDTO struct {
	ID            uuid.UUID   `json:"id"`
	Code          string      `json:"code"`
	Name          string      `json:"name"`
	Description   string      `json:"description"`
	System        bool        `json:"system"`
	PermissionIDs []uuid.UUID `json:"permission_ids"`
	AdminCount    int64       `json:"admin_count"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
}

type accessPermissionDTO struct {
	ID          uuid.UUID `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Module      string    `json:"module"`
	Description string    `json:"description"`
}

type accessAuditLogDTO struct {
	ID            uuid.UUID  `json:"id"`
	AdminID       *uuid.UUID `json:"admin_id"`
	AdminName     string     `json:"admin_name"`
	AdminUsername string     `json:"admin_username"`
	Action        string     `json:"action"`
	Resource      string     `json:"resource"`
	ResourceID    string     `json:"resource_id"`
	IP            string     `json:"ip"`
	Detail        string     `json:"detail"`
	CreatedAt     time.Time  `json:"created_at"`
}

type accessAdminCreateRequest struct {
	Username string      `json:"username"`
	Name     string      `json:"name"`
	Password string      `json:"password"`
	Status   string      `json:"status"`
	RoleIDs  []uuid.UUID `json:"role_ids"`
}

type accessAdminUpdateRequest struct {
	Name    *string      `json:"name"`
	Status  *string      `json:"status"`
	RoleIDs *[]uuid.UUID `json:"role_ids"`
}

type accessAdminPasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type accessRoleRequest struct {
	Code          string      `json:"code"`
	Name          string      `json:"name"`
	Description   string      `json:"description"`
	PermissionIDs []uuid.UUID `json:"permission_ids"`
}

type accessOperationError struct {
	status  int
	code    int
	message string
	cause   error
}

func (e *accessOperationError) Error() string {
	if e.cause != nil {
		return e.message + ": " + e.cause.Error()
	}
	return e.message
}

func (e *accessOperationError) Unwrap() error { return e.cause }

func accessFailure(status, code int, message string, cause error) error {
	return &accessOperationError{status: status, code: code, message: message, cause: cause}
}

func respondAccessFailure(c *gin.Context, err error, fallback string) {
	var operationError *accessOperationError
	if errors.As(err, &operationError) {
		response.Error(c, operationError.status, operationError.code, operationError.message)
		return
	}
	response.Error(c, 500, 50080, fallback)
}

func isAccessUniqueViolation(err error) bool {
	return errors.Is(err, gorm.ErrDuplicatedKey) || (err != nil && strings.Contains(err.Error(), "SQLSTATE 23505"))
}

func normalizeAccessUUIDs(values []uuid.UUID, minimum, maximum int) ([]uuid.UUID, error) {
	if len(values) < minimum || len(values) > maximum {
		return nil, errors.New("invalid reference count")
	}
	seen := make(map[uuid.UUID]struct{}, len(values))
	result := append([]uuid.UUID(nil), values...)
	for _, value := range result {
		if value == uuid.Nil {
			return nil, errors.New("nil reference")
		}
		if _, exists := seen[value]; exists {
			return nil, errors.New("duplicate reference")
		}
		seen[value] = struct{}{}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].String() < result[j].String() })
	return result, nil
}

func normalizeAccessAdminName(value string) (string, error) {
	value = strings.TrimSpace(value)
	length := len([]rune(value))
	if length < 1 || length > 80 {
		return "", errors.New("invalid administrator name")
	}
	return value, nil
}

func validateAccessPassword(password, username string) error {
	if !utf8.ValidString(password) || len([]rune(password)) < 12 || len(password) > 72 {
		return errors.New("password length must be 12 to 72 characters and at most 72 bytes")
	}
	var lower, upper, digit, symbol bool
	for _, character := range password {
		if unicode.IsSpace(character) || unicode.IsControl(character) {
			return errors.New("password contains whitespace or control characters")
		}
		switch {
		case unicode.IsLower(character):
			lower = true
		case unicode.IsUpper(character):
			upper = true
		case unicode.IsDigit(character):
			digit = true
		case unicode.IsPunct(character) || unicode.IsSymbol(character):
			symbol = true
		}
	}
	if !lower || !upper || !digit || !symbol {
		return errors.New("password must contain lower-case, upper-case, numeric and symbol characters")
	}
	lowerPassword := strings.ToLower(password)
	lowerUsername := strings.ToLower(strings.TrimSpace(username))
	if len(lowerUsername) >= 3 && strings.Contains(lowerPassword, lowerUsername) {
		return errors.New("password contains administrator username")
	}
	for _, weak := range []string{"password", "qwerty", "123456", "linlinqi"} {
		if strings.Contains(lowerPassword, weak) {
			return errors.New("password contains a common weak phrase")
		}
	}
	return nil
}

func (r *accessAdminCreateRequest) normalizeAndValidate() error {
	r.Username = strings.ToLower(strings.TrimSpace(r.Username))
	r.Name = strings.TrimSpace(r.Name)
	r.Status = strings.ToLower(strings.TrimSpace(r.Status))
	if !accessAdminUsernamePattern.MatchString(r.Username) {
		return errors.New("invalid administrator username")
	}
	name, err := normalizeAccessAdminName(r.Name)
	if err != nil {
		return err
	}
	r.Name = name
	if r.Status != "active" && r.Status != "disabled" {
		return errors.New("invalid administrator status")
	}
	if err := validateAccessPassword(r.Password, r.Username); err != nil {
		return err
	}
	r.RoleIDs, err = normalizeAccessUUIDs(r.RoleIDs, 1, 100)
	return err
}

func (r *accessAdminUpdateRequest) normalizeAndValidate() error {
	if r.Name == nil && r.Status == nil && r.RoleIDs == nil {
		return errors.New("empty administrator update")
	}
	if r.Name != nil {
		name, err := normalizeAccessAdminName(*r.Name)
		if err != nil {
			return err
		}
		r.Name = &name
	}
	if r.Status != nil {
		status := strings.ToLower(strings.TrimSpace(*r.Status))
		if status != "active" && status != "disabled" {
			return errors.New("invalid administrator status")
		}
		r.Status = &status
	}
	if r.RoleIDs != nil {
		roleIDs, err := normalizeAccessUUIDs(*r.RoleIDs, 1, 100)
		if err != nil {
			return err
		}
		r.RoleIDs = &roleIDs
	}
	return nil
}

func (r *accessRoleRequest) normalizeAndValidate() error {
	r.Code = strings.ToLower(strings.TrimSpace(r.Code))
	r.Name = strings.TrimSpace(r.Name)
	r.Description = strings.TrimSpace(r.Description)
	if !accessRoleCodePattern.MatchString(r.Code) || len([]rune(r.Name)) < 2 || len([]rune(r.Name)) > 120 || len([]rune(r.Description)) > 500 {
		return errors.New("invalid role identity")
	}
	permissionIDs, err := normalizeAccessUUIDs(r.PermissionIDs, 1, 200)
	if err != nil {
		return err
	}
	r.PermissionIDs = permissionIDs
	return nil
}

func accessTimePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	value = value.UTC()
	return &value
}

func toAccessAdminDTO(admin model.Admin, roles []accessNamedRoleDTO, totpEnabled bool) accessAdminDTO {
	if roles == nil {
		roles = []accessNamedRoleDTO{}
	}
	roleIDs := make([]uuid.UUID, 0, len(roles))
	for _, role := range roles {
		roleIDs = append(roleIDs, role.ID)
	}
	return accessAdminDTO{
		ID: admin.ID, Username: admin.Username, Name: admin.Name, Status: admin.Status,
		RoleIDs: roleIDs, Roles: roles, TOTPEnabled: totpEnabled,
		LastLoginAt: accessTimePointer(admin.LastLoginAt), CreatedAt: admin.CreatedAt.UTC(), UpdatedAt: admin.UpdatedAt.UTC(),
	}
}

func (h Handler) AdminAccessOverview(c *gin.Context) {
	var admins []model.Admin
	if err := h.DB.Select("id", "username", "name", "status", "last_login_at", "created_at", "updated_at").Order("created_at ASC, username ASC").Find(&admins).Error; err != nil {
		response.Error(c, 500, 50081, "error.admin_list_fetch_failed")
		return
	}
	var roles []model.Role
	if err := h.DB.Select("id", "code", "name", "description", "system", "created_at", "updated_at").Order("system DESC, code ASC").Find(&roles).Error; err != nil {
		response.Error(c, 500, 50082, "error.role_list_fetch_failed")
		return
	}
	var permissions []model.Permission
	if err := h.DB.Select("id", "code", "name", "module", "description").Order("module ASC, code ASC").Find(&permissions).Error; err != nil {
		response.Error(c, 500, 50083, "error.permission_list_fetch_failed")
		return
	}

	type adminRoleRow struct {
		AdminID uuid.UUID
		RoleID  uuid.UUID
		Code    string
		Name    string
	}
	var adminRoleRows []adminRoleRow
	if err := h.DB.Table("admin_roles ar").
		Select("ar.admin_id, r.id AS role_id, r.code, r.name").
		Joins("JOIN admins a ON a.id = ar.admin_id AND a.deleted_at IS NULL").
		Joins("JOIN roles r ON r.id = ar.role_id AND r.deleted_at IS NULL").
		Order("ar.admin_id ASC, r.code ASC").Scan(&adminRoleRows).Error; err != nil {
		response.Error(c, 500, 50084, "error.admin_role_fetch_failed")
		return
	}
	adminRoles := make(map[uuid.UUID][]accessNamedRoleDTO, len(admins))
	roleAdminCounts := make(map[uuid.UUID]int64, len(roles))
	for _, row := range adminRoleRows {
		adminRoles[row.AdminID] = append(adminRoles[row.AdminID], accessNamedRoleDTO{ID: row.RoleID, Code: row.Code, Name: row.Name})
		roleAdminCounts[row.RoleID]++
	}

	type rolePermissionRow struct {
		RoleID       uuid.UUID
		PermissionID uuid.UUID
	}
	var rolePermissionRows []rolePermissionRow
	if err := h.DB.Table("role_permissions rp").
		Select("rp.role_id, rp.permission_id").
		Joins("JOIN roles r ON r.id = rp.role_id AND r.deleted_at IS NULL").
		Joins("JOIN permissions p ON p.id = rp.permission_id AND p.deleted_at IS NULL").
		Order("rp.role_id ASC, rp.permission_id ASC").Scan(&rolePermissionRows).Error; err != nil {
		response.Error(c, 500, 50085, "error.role_permission_fetch_failed")
		return
	}
	rolePermissions := make(map[uuid.UUID][]uuid.UUID, len(roles))
	for _, row := range rolePermissionRows {
		rolePermissions[row.RoleID] = append(rolePermissions[row.RoleID], row.PermissionID)
	}

	var enabledTOTPIDs []uuid.UUID
	if err := h.DB.Model(&model.TOTPDevice{}).Where("realm = ? AND enabled = ?", "admin", true).Pluck("principal_id", &enabledTOTPIDs).Error; err != nil {
		response.Error(c, 500, 50086, "error.two_factor_status_fetch_failed")
		return
	}
	totpEnabled := make(map[uuid.UUID]bool, len(enabledTOTPIDs))
	for _, id := range enabledTOTPIDs {
		totpEnabled[id] = true
	}

	adminItems := make([]accessAdminDTO, 0, len(admins))
	for _, admin := range admins {
		adminItems = append(adminItems, toAccessAdminDTO(admin, adminRoles[admin.ID], totpEnabled[admin.ID]))
	}
	roleItems := make([]accessRoleDTO, 0, len(roles))
	for _, role := range roles {
		permissionIDs := rolePermissions[role.ID]
		if permissionIDs == nil {
			permissionIDs = []uuid.UUID{}
		}
		roleItems = append(roleItems, accessRoleDTO{
			ID: role.ID, Code: role.Code, Name: role.Name, Description: role.Description, System: role.System,
			PermissionIDs: permissionIDs, AdminCount: roleAdminCounts[role.ID], CreatedAt: role.CreatedAt.UTC(), UpdatedAt: role.UpdatedAt.UTC(),
		})
	}
	permissionItems := make([]accessPermissionDTO, 0, len(permissions))
	for _, permission := range permissions {
		permissionItems = append(permissionItems, accessPermissionDTO{
			ID: permission.ID, Code: permission.Code, Name: permission.Name, Module: permission.Module, Description: permission.Description,
		})
	}
	response.OK(c, gin.H{"admins": adminItems, "roles": roleItems, "permissions": permissionItems, "current_admin_id": c.GetString("subject")})
}

func parseAccessAuditTime(value string, endOfDate bool) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC(), nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, err
	}
	if endOfDate {
		parsed = parsed.AddDate(0, 0, 1)
	}
	return parsed.UTC(), nil
}

func (h Handler) AdminAccessAuditLogs(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Table("audit_logs al").Joins("LEFT JOIN admins a ON a.id = al.admin_id")
	q := strings.TrimSpace(c.Query("q"))
	if len([]rune(q)) > 100 {
		response.Error(c, 422, 42280, "error.search_keyword_too_long")
		return
	}
	if q != "" {
		like := "%" + strings.ToLower(q) + "%"
		query = query.Where("LOWER(COALESCE(al.action, '') || ' ' || COALESCE(al.resource, '') || ' ' || COALESCE(al.resource_id, '') || ' ' || COALESCE(al.detail, '') || ' ' || COALESCE(a.username, '') || ' ' || COALESCE(a.name, '')) LIKE ?", like)
	}
	for column, value := range map[string]string{"al.action": strings.TrimSpace(c.Query("action")), "al.resource": strings.TrimSpace(c.Query("resource"))} {
		if len([]rune(value)) > 100 {
			response.Error(c, 422, 42281, "error.audit_filter_too_long")
			return
		}
		if value != "" {
			query = query.Where(column+" = ?", value)
		}
	}
	adminFilter := strings.TrimSpace(c.Query("admin_id"))
	if adminFilter == "" {
		adminFilter = strings.TrimSpace(c.Query("admin"))
	}
	if adminFilter != "" {
		if parsed, err := uuid.Parse(adminFilter); err == nil {
			query = query.Where("al.admin_id = ?", parsed)
		} else {
			if len([]rune(adminFilter)) > 80 {
				response.Error(c, 422, 42282, "error.admin_filter_invalid")
				return
			}
			query = query.Where("LOWER(COALESCE(a.username, '')) = ? OR LOWER(COALESCE(a.name, '')) = ?", strings.ToLower(adminFilter), strings.ToLower(adminFilter))
		}
	}

	var from, to time.Time
	var err error
	if date := strings.TrimSpace(c.Query("date")); date != "" {
		from, err = parseAccessAuditTime(date, false)
		if err == nil {
			to, err = parseAccessAuditTime(date, true)
		}
	} else {
		from, err = parseAccessAuditTime(c.Query("from"), false)
		if err == nil {
			to, err = parseAccessAuditTime(c.Query("to"), true)
		}
	}
	if err != nil || (!from.IsZero() && !to.IsZero() && (!to.After(from) || to.Sub(from) > 366*24*time.Hour)) {
		response.Error(c, 422, 42283, "error.audit_date_range_invalid")
		return
	}
	if !from.IsZero() {
		query = query.Where("al.created_at >= ?", from)
	}
	if !to.IsZero() {
		query = query.Where("al.created_at < ?", to)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50087, "error.audit_log_stats_failed")
		return
	}
	var items []accessAuditLogDTO
	if err := query.Select("al.id, al.admin_id, COALESCE(a.name, '') AS admin_name, COALESCE(a.username, '') AS admin_username, al.action, al.resource, al.resource_id, al.ip, al.detail, al.created_at").
		Order("al.created_at DESC, al.id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&items).Error; err != nil {
		response.Error(c, 500, 50088, "error.audit_log_list_fetch_failed")
		return
	}
	response.Page(c, items, total, page, pageSize)
}

func currentAccessAdminID(c *gin.Context) (uuid.UUID, bool) {
	adminID, err := uuid.Parse(c.GetString("subject"))
	if err != nil {
		response.Error(c, 401, 40180, "error.invalid_admin_identity")
		return uuid.Nil, false
	}
	return adminID, true
}

// PostgreSQL's transaction-scoped advisory lock serializes the small set of
// RBAC mutations. Row locks are still taken in UUID order below, but the single
// lock makes the "at least one active system manager" invariant race-safe.
func acquireAccessMutationLock(tx *gorm.DB) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(?)", accessMutationLockKey).Error
}

func lockAccessAdmins(tx *gorm.DB, ids ...uuid.UUID) (map[uuid.UUID]model.Admin, error) {
	if len(ids) < 1 || len(ids) > 10 {
		return nil, errors.New("invalid administrator lock set")
	}
	seen := make(map[uuid.UUID]struct{}, len(ids))
	normalized := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			return nil, errors.New("invalid administrator lock id")
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].String() < normalized[j].String() })
	var admins []model.Admin
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", normalized).Order("id ASC").Find(&admins).Error; err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID]model.Admin, len(admins))
	for _, admin := range admins {
		result[admin.ID] = admin
	}
	return result, nil
}

func hasAccessSystemPermission(tx *gorm.DB, adminID uuid.UUID) (bool, error) {
	var count int64
	err := tx.Table(accessTableName(tx, "AdminRole", "ar")).
		Joins("JOIN "+accessTableName(tx, "Role", "r")+" ON r.id = ar.role_id AND r.deleted_at IS NULL").
		Joins("JOIN "+accessTableName(tx, "RolePermission", "rp")+" ON rp.role_id = r.id").
		Joins("JOIN "+accessTableName(tx, "Permission", "p")+" ON p.id = rp.permission_id AND p.deleted_at IS NULL").
		Where("ar.admin_id = ? AND p.code = ?", adminID, "system.manage").Count(&count).Error
	return count > 0, err
}

func validateAccessCaller(tx *gorm.DB, caller model.Admin) error {
	if caller.ID == uuid.Nil || caller.Status != "active" {
		return accessFailure(403, 40380, "error.admin_disabled", nil)
	}
	allowed, err := hasAccessSystemPermission(tx, caller.ID)
	if err != nil {
		return err
	}
	if !allowed {
		return accessFailure(403, 40381, "error.admin_privileges_removed", nil)
	}
	return nil
}

// accessTableName keeps invariant queries compatible with GORM table prefixes
// used by isolated PostgreSQL schemas. With the production naming strategy it
// resolves to the same public table names as before.
func accessTableName(tx *gorm.DB, modelName, alias string) string {
	return tx.NamingStrategy.TableName(modelName) + " " + alias
}

func activeAccessSystemManagerCount(tx *gorm.DB) (int64, error) {
	var count int64
	err := tx.Table(accessTableName(tx, "Admin", "a")).
		Joins("JOIN "+accessTableName(tx, "AdminRole", "ar")+" ON ar.admin_id = a.id").
		Joins("JOIN "+accessTableName(tx, "Role", "r")+" ON r.id = ar.role_id AND r.deleted_at IS NULL").
		Joins("JOIN "+accessTableName(tx, "RolePermission", "rp")+" ON rp.role_id = r.id").
		Joins("JOIN "+accessTableName(tx, "Permission", "p")+" ON p.id = rp.permission_id AND p.deleted_at IS NULL").
		Where("a.deleted_at IS NULL AND a.status = ? AND p.code = ?", "active", "system.manage").
		Distinct("a.id").Count(&count).Error
	return count, err
}

func enforceActiveAccessSystemManager(tx *gorm.DB) error {
	count, err := activeAccessSystemManagerCount(tx)
	if err != nil {
		return err
	}
	if count < 1 {
		return accessFailure(409, 40980, "error.at_least_one_system_admin_required", nil)
	}
	return nil
}

func loadAccessRoles(tx *gorm.DB, ids []uuid.UUID) ([]model.Role, error) {
	var roles []model.Role
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", ids).Order("id ASC").Find(&roles).Error; err != nil {
		return nil, err
	}
	if len(roles) != len(ids) {
		return nil, accessFailure(422, 42284, "error.role_not_found", nil)
	}
	return roles, nil
}

func loadAccessPermissions(tx *gorm.DB, ids []uuid.UUID) ([]model.Permission, error) {
	var permissions []model.Permission
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", ids).Order("id ASC").Find(&permissions).Error; err != nil {
		return nil, err
	}
	if len(permissions) != len(ids) {
		return nil, accessFailure(422, 42285, "error.permission_not_found", nil)
	}
	return permissions, nil
}

func accessLegacyRoleCode(roles []model.Role) string {
	codes := make([]string, 0, len(roles))
	for _, role := range roles {
		codes = append(codes, role.Code)
	}
	sort.Strings(codes)
	if len(codes) == 0 {
		return "operator"
	}
	return codes[0]
}

func replaceAccessAdminRoles(tx *gorm.DB, adminID uuid.UUID, roles []model.Role) error {
	if err := tx.Where("admin_id = ?", adminID).Delete(&model.AdminRole{}).Error; err != nil {
		return err
	}
	links := make([]model.AdminRole, 0, len(roles))
	for _, role := range roles {
		links = append(links, model.AdminRole{AdminID: adminID, RoleID: role.ID})
	}
	return tx.Create(&links).Error
}

func accessAdminRoleIDs(tx *gorm.DB, adminID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := tx.Model(&model.AdminRole{}).Where("admin_id = ?", adminID).Order("role_id ASC").Pluck("role_id", &ids).Error
	return ids, err
}

func sameAccessUUIDs(left, right []uuid.UUID) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func createAccessAudit(tx *gorm.DB, c *gin.Context, callerID uuid.UUID, action, resource, resourceID, reason, detail string) error {
	metadata := []string{"reason=" + reason}
	if detail = strings.TrimSpace(detail); detail != "" {
		metadata = append(metadata, detail)
	}
	if requestID := strings.TrimSpace(c.GetString("request_id")); requestID != "" {
		metadata = append(metadata, "request_id="+requestID)
	}
	return tx.Create(&model.AuditLog{
		AdminID: &callerID, Action: action, Resource: resource, ResourceID: resourceID,
		IP: c.ClientIP(), Detail: strings.Join(metadata, "; "),
	}).Error
}

func (h Handler) CreateAccessAdmin(c *gin.Context) {
	callerID, ok := currentAccessAdminID(c)
	if !ok {
		return
	}
	var req accessAdminCreateRequest
	if err := decodeStrictJSON(c, &req); err != nil || req.normalizeAndValidate() != nil {
		response.Error(c, 422, 42286, "error.admin_profile_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "创建管理员")
	if !ok {
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		response.Error(c, 500, 50089, "error.admin_password_generate_failed")
		return
	}
	var created model.Admin
	var createdRoles []model.Role
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := acquireAccessMutationLock(tx); err != nil {
			return err
		}
		locked, err := lockAccessAdmins(tx, callerID)
		if err != nil {
			return err
		}
		if err := validateAccessCaller(tx, locked[callerID]); err != nil {
			return err
		}
		createdRoles, err = loadAccessRoles(tx, req.RoleIDs)
		if err != nil {
			return err
		}
		var existing int64
		if err := tx.Unscoped().Model(&model.Admin{}).Where("username = ?", req.Username).Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return accessFailure(409, 40981, "error.admin_account_exists", nil)
		}
		created = model.Admin{Username: req.Username, PasswordHash: string(hash), Name: req.Name, Role: accessLegacyRoleCode(createdRoles), Status: req.Status}
		if err := tx.Create(&created).Error; err != nil {
			if isAccessUniqueViolation(err) {
				return accessFailure(409, 40981, "error.admin_account_exists", err)
			}
			return err
		}
		if err := replaceAccessAdminRoles(tx, created.ID, createdRoles); err != nil {
			return err
		}
		if err := enforceActiveAccessSystemManager(tx); err != nil {
			return err
		}
		return createAccessAudit(tx, c, callerID, "access.admin.create", "admin", created.ID.String(), reason, fmt.Sprintf("username=%s; status=%s; roles=%d", created.Username, created.Status, len(createdRoles)))
	})
	if err != nil {
		respondAccessFailure(c, err, "error.admin_create_failed")
		return
	}
	namedRoles := make([]accessNamedRoleDTO, 0, len(createdRoles))
	for _, role := range createdRoles {
		namedRoles = append(namedRoles, accessNamedRoleDTO{ID: role.ID, Code: role.Code, Name: role.Name})
	}
	response.Created(c, toAccessAdminDTO(created, namedRoles, false))
}

func (h Handler) UpdateAccessAdmin(c *gin.Context) {
	callerID, ok := currentAccessAdminID(c)
	if !ok {
		return
	}
	targetID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42287, "error.admin_id_invalid")
		return
	}
	var req accessAdminUpdateRequest
	if err := decodeStrictJSON(c, &req); err != nil || req.normalizeAndValidate() != nil {
		response.Error(c, 422, 42288, "error.admin_update_profile_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "更新管理员")
	if !ok {
		return
	}
	var updated model.Admin
	var updatedRoles []accessNamedRoleDTO
	updatedTOTPEnabled := false
	securityContextRevoked := false
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := acquireAccessMutationLock(tx); err != nil {
			return err
		}
		locked, err := lockAccessAdmins(tx, callerID, targetID)
		if err != nil {
			return err
		}
		caller, callerExists := locked[callerID]
		target, targetExists := locked[targetID]
		if !callerExists {
			return accessFailure(403, 40380, "error.admin_disabled", nil)
		}
		if !targetExists {
			return accessFailure(404, 40480, "error.admin_not_found", nil)
		}
		if err := validateAccessCaller(tx, caller); err != nil {
			return err
		}
		updates := map[string]any{}
		if req.Name != nil && *req.Name != target.Name {
			updates["name"] = *req.Name
			target.Name = *req.Name
		}
		securityChanged := false
		if req.Status != nil && *req.Status != target.Status {
			updates["status"] = *req.Status
			target.Status = *req.Status
			securityChanged = true
		}
		currentRoleIDs, err := accessAdminRoleIDs(tx, targetID)
		if err != nil {
			return err
		}
		sort.Slice(currentRoleIDs, func(i, j int) bool { return currentRoleIDs[i].String() < currentRoleIDs[j].String() })
		if req.RoleIDs != nil {
			roles, err := loadAccessRoles(tx, *req.RoleIDs)
			if err != nil {
				return err
			}
			if !sameAccessUUIDs(currentRoleIDs, *req.RoleIDs) {
				if err := replaceAccessAdminRoles(tx, targetID, roles); err != nil {
					return err
				}
				updates["role"] = accessLegacyRoleCode(roles)
				securityChanged = true
			}
			currentRoleIDs = *req.RoleIDs
		}
		if len(currentRoleIDs) == 0 {
			return accessFailure(422, 42289, "error.admin_role_required", nil)
		}
		if securityChanged {
			updates["session_version"] = gorm.Expr("session_version + 1")
			securityContextRevoked = true
		}
		if len(updates) > 0 {
			if err := tx.Model(&model.Admin{}).Where("id = ?", targetID).Updates(updates).Error; err != nil {
				return err
			}
		}
		if targetID == callerID {
			allowed, err := hasAccessSystemPermission(tx, callerID)
			if err != nil {
				return err
			}
			if target.Status != "active" || !allowed {
				return accessFailure(409, 40982, "error.cannot_disable_current_admin", nil)
			}
		}
		if err := enforceActiveAccessSystemManager(tx); err != nil {
			return err
		}
		if err := tx.Select("id", "username", "name", "status", "last_login_at", "created_at", "updated_at").First(&updated, "id = ?", targetID).Error; err != nil {
			return err
		}
		var roleRows []model.Role
		if err := tx.Table("roles r").Select("r.id", "r.code", "r.name").Joins("JOIN admin_roles ar ON ar.role_id = r.id").Where("ar.admin_id = ? AND r.deleted_at IS NULL", targetID).Order("r.code ASC").Scan(&roleRows).Error; err != nil {
			return err
		}
		updatedRoles = make([]accessNamedRoleDTO, 0, len(roleRows))
		for _, role := range roleRows {
			updatedRoles = append(updatedRoles, accessNamedRoleDTO{ID: role.ID, Code: role.Code, Name: role.Name})
		}
		var totpCount int64
		if err := tx.Model(&model.TOTPDevice{}).Where("realm = ? AND principal_id = ? AND enabled = ?", "admin", targetID, true).Count(&totpCount).Error; err != nil {
			return err
		}
		updatedTOTPEnabled = totpCount > 0
		return createAccessAudit(tx, c, callerID, "access.admin.update", "admin", targetID.String(), reason, fmt.Sprintf("status=%s; roles=%d; sessions_revoked=%t", target.Status, len(currentRoleIDs), securityChanged))
	})
	if err != nil {
		respondAccessFailure(c, err, "error.admin_update_failed")
		return
	}
	response.OK(c, gin.H{"admin": toAccessAdminDTO(updated, updatedRoles, updatedTOTPEnabled), "security_context_revoked": securityContextRevoked})
}

func (h Handler) ChangeAccessAdminPassword(c *gin.Context) {
	callerID, ok := currentAccessAdminID(c)
	if !ok {
		return
	}
	targetID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42290, "error.admin_id_invalid")
		return
	}
	var req accessAdminPasswordRequest
	if err := decodeStrictJSON(c, &req); err != nil || req.CurrentPassword == "" || len(req.CurrentPassword) > 72 || validateAccessPassword(req.NewPassword, "") != nil {
		response.Error(c, 422, 42291, "error.password_profile_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "重置管理员密码")
	if !ok {
		return
	}
	var preliminary model.Admin
	if err := h.DB.Select("id", "password_hash", "status").First(&preliminary, "id = ?", callerID).Error; err != nil || bcrypt.CompareHashAndPassword([]byte(preliminary.PasswordHash), []byte(req.CurrentPassword)) != nil {
		response.Error(c, 401, 40181, "error.operator_password_check_failed")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		response.Error(c, 500, 50090, "error.admin_password_generate_failed")
		return
	}
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := acquireAccessMutationLock(tx); err != nil {
			return err
		}
		locked, err := lockAccessAdmins(tx, callerID, targetID)
		if err != nil {
			return err
		}
		caller, callerExists := locked[callerID]
		target, targetExists := locked[targetID]
		if !callerExists || bcrypt.CompareHashAndPassword([]byte(caller.PasswordHash), []byte(req.CurrentPassword)) != nil {
			return accessFailure(401, 40181, "error.operator_password_check_failed", nil)
		}
		if !targetExists {
			return accessFailure(404, 40480, "error.admin_not_found", nil)
		}
		if err := validateAccessCaller(tx, caller); err != nil {
			return err
		}
		if err := validateAccessPassword(req.NewPassword, target.Username); err != nil {
			return accessFailure(422, 42291, "error.weak_password_rejected", nil)
		}
		if bcrypt.CompareHashAndPassword([]byte(target.PasswordHash), []byte(req.NewPassword)) == nil {
			return accessFailure(409, 40983, "error.new_password_same_as_current", nil)
		}
		if err := tx.Model(&model.Admin{}).Where("id = ?", targetID).Updates(map[string]any{
			"password_hash": string(hash), "session_version": gorm.Expr("session_version + 1"),
		}).Error; err != nil {
			return err
		}
		return createAccessAudit(tx, c, callerID, "access.admin.password", "admin", targetID.String(), reason, "sessions_revoked=true")
	})
	if err != nil {
		respondAccessFailure(c, err, "error.admin_password_reset_failed")
		return
	}
	response.OK(c, gin.H{"changed": true, "sessions_revoked": true})
}

func accessPermissionIDs(tx *gorm.DB, roleID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := tx.Model(&model.RolePermission{}).Where("role_id = ?", roleID).Order("permission_id ASC").Pluck("permission_id", &ids).Error
	return ids, err
}

func replaceAccessRolePermissions(tx *gorm.DB, roleID uuid.UUID, permissions []model.Permission) error {
	if err := tx.Where("role_id = ?", roleID).Delete(&model.RolePermission{}).Error; err != nil {
		return err
	}
	links := make([]model.RolePermission, 0, len(permissions))
	for _, permission := range permissions {
		links = append(links, model.RolePermission{RoleID: roleID, PermissionID: permission.ID})
	}
	return tx.Create(&links).Error
}

func (h Handler) CreateAccessRole(c *gin.Context) {
	callerID, ok := currentAccessAdminID(c)
	if !ok {
		return
	}
	var req accessRoleRequest
	if err := decodeStrictJSON(c, &req); err != nil || req.normalizeAndValidate() != nil {
		response.Error(c, 422, 42292, "error.role_profile_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "创建角色")
	if !ok {
		return
	}
	var role model.Role
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := acquireAccessMutationLock(tx); err != nil {
			return err
		}
		locked, err := lockAccessAdmins(tx, callerID)
		if err != nil {
			return err
		}
		if err := validateAccessCaller(tx, locked[callerID]); err != nil {
			return err
		}
		permissions, err := loadAccessPermissions(tx, req.PermissionIDs)
		if err != nil {
			return err
		}
		var existing int64
		if err := tx.Unscoped().Model(&model.Role{}).Where("code = ?", req.Code).Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return accessFailure(409, 40984, "error.role_slug_exists", nil)
		}
		role = model.Role{Code: req.Code, Name: req.Name, Description: req.Description, System: false}
		if err := tx.Create(&role).Error; err != nil {
			if isAccessUniqueViolation(err) {
				return accessFailure(409, 40984, "error.role_slug_exists", err)
			}
			return err
		}
		if err := replaceAccessRolePermissions(tx, role.ID, permissions); err != nil {
			return err
		}
		if err := validateAccessCaller(tx, locked[callerID]); err != nil {
			return err
		}
		if err := enforceActiveAccessSystemManager(tx); err != nil {
			return err
		}
		return createAccessAudit(tx, c, callerID, "access.role.create", "role", role.ID.String(), reason, fmt.Sprintf("code=%s; permissions=%d", role.Code, len(permissions)))
	})
	if err != nil {
		respondAccessFailure(c, err, "error.role_create_failed")
		return
	}
	response.Created(c, accessRoleDTO{ID: role.ID, Code: role.Code, Name: role.Name, Description: role.Description, System: false, PermissionIDs: req.PermissionIDs, AdminCount: 0, CreatedAt: role.CreatedAt.UTC(), UpdatedAt: role.UpdatedAt.UTC()})
}

func (h Handler) UpdateAccessRole(c *gin.Context) {
	callerID, ok := currentAccessAdminID(c)
	if !ok {
		return
	}
	roleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42293, "error.role_id_invalid")
		return
	}
	var req accessRoleRequest
	if err := decodeStrictJSON(c, &req); err != nil || req.normalizeAndValidate() != nil {
		response.Error(c, 422, 42292, "error.role_profile_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "更新角色")
	if !ok {
		return
	}
	var updated model.Role
	var adminCount int64
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := acquireAccessMutationLock(tx); err != nil {
			return err
		}
		lockedAdmins, err := lockAccessAdmins(tx, callerID)
		if err != nil {
			return err
		}
		if err := validateAccessCaller(tx, lockedAdmins[callerID]); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&updated, "id = ?", roleID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return accessFailure(404, 40481, "error.role_does_not_exist", nil)
			}
			return err
		}
		if updated.System {
			return accessFailure(409, 40985, "error.system_role_not_modifiable", nil)
		}
		permissions, err := loadAccessPermissions(tx, req.PermissionIDs)
		if err != nil {
			return err
		}
		oldPermissionIDs, err := accessPermissionIDs(tx, roleID)
		if err != nil {
			return err
		}
		sort.Slice(oldPermissionIDs, func(i, j int) bool { return oldPermissionIDs[i].String() < oldPermissionIDs[j].String() })
		permissionsChanged := !sameAccessUUIDs(oldPermissionIDs, req.PermissionIDs)
		oldCode := updated.Code
		codeChanged := oldCode != req.Code
		if codeChanged {
			var conflicting int64
			if err := tx.Unscoped().Model(&model.Role{}).Where("code = ? AND id <> ?", req.Code, roleID).Count(&conflicting).Error; err != nil {
				return err
			}
			if conflicting > 0 {
				return accessFailure(409, 40984, "error.role_slug_exists", nil)
			}
		}
		if err := tx.Model(&updated).Updates(map[string]any{"code": req.Code, "name": req.Name, "description": req.Description}).Error; err != nil {
			if isAccessUniqueViolation(err) {
				return accessFailure(409, 40984, "error.role_slug_exists", err)
			}
			return err
		}
		if permissionsChanged {
			if err := replaceAccessRolePermissions(tx, roleID, permissions); err != nil {
				return err
			}
		}
		if permissionsChanged || codeChanged {
			adminIDsQuery := tx.Model(&model.AdminRole{}).Select("admin_id").Where("role_id = ?", roleID)
			if err := tx.Model(&model.Admin{}).Where("id IN (?)", adminIDsQuery).UpdateColumn("session_version", gorm.Expr("session_version + 1")).Error; err != nil {
				return err
			}
		}
		if codeChanged {
			if err := tx.Model(&model.Admin{}).Where("role = ? AND id IN (?)", oldCode, tx.Model(&model.AdminRole{}).Select("admin_id").Where("role_id = ?", roleID)).Update("role", req.Code).Error; err != nil {
				return err
			}
		}
		if err := validateAccessCaller(tx, lockedAdmins[callerID]); err != nil {
			return err
		}
		if err := enforceActiveAccessSystemManager(tx); err != nil {
			return err
		}
		if err := tx.Model(&model.AdminRole{}).Where("role_id = ?", roleID).Count(&adminCount).Error; err != nil {
			return err
		}
		if err := createAccessAudit(tx, c, callerID, "access.role.update", "role", roleID.String(), reason, fmt.Sprintf("code=%s; permissions=%d; sessions_revoked=%t", req.Code, len(permissions), permissionsChanged || codeChanged)); err != nil {
			return err
		}
		return tx.Select("id", "code", "name", "description", "system", "created_at", "updated_at").First(&updated, "id = ?", roleID).Error
	})
	if err != nil {
		respondAccessFailure(c, err, "error.role_update_failed")
		return
	}
	response.OK(c, accessRoleDTO{ID: updated.ID, Code: updated.Code, Name: updated.Name, Description: updated.Description, System: updated.System, PermissionIDs: req.PermissionIDs, AdminCount: adminCount, CreatedAt: updated.CreatedAt.UTC(), UpdatedAt: updated.UpdatedAt.UTC()})
}

func (h Handler) DeleteAccessRole(c *gin.Context) {
	callerID, ok := currentAccessAdminID(c)
	if !ok {
		return
	}
	roleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42293, "error.role_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "删除角色")
	if !ok {
		return
	}
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := acquireAccessMutationLock(tx); err != nil {
			return err
		}
		lockedAdmins, err := lockAccessAdmins(tx, callerID)
		if err != nil {
			return err
		}
		if err := validateAccessCaller(tx, lockedAdmins[callerID]); err != nil {
			return err
		}
		var role model.Role
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&role, "id = ?", roleID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return accessFailure(404, 40481, "error.role_does_not_exist", nil)
			}
			return err
		}
		if role.System {
			return accessFailure(409, 40986, "error.system_role_not_deletable", nil)
		}
		var adminCount int64
		if err := tx.Model(&model.AdminRole{}).Where("role_id = ?", roleID).Count(&adminCount).Error; err != nil {
			return err
		}
		if adminCount > 0 {
			return accessFailure(409, 40987, "error.role_in_use_cannot_delete", nil)
		}
		if err := tx.Where("role_id = ?", roleID).Delete(&model.RolePermission{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&role).Error; err != nil {
			return err
		}
		if err := validateAccessCaller(tx, lockedAdmins[callerID]); err != nil {
			return err
		}
		if err := enforceActiveAccessSystemManager(tx); err != nil {
			return err
		}
		return createAccessAudit(tx, c, callerID, "access.role.delete", "role", roleID.String(), reason, "code="+role.Code)
	})
	if err != nil {
		respondAccessFailure(c, err, "error.role_delete_failed")
		return
	}
	response.OK(c, gin.H{"deleted": true, "id": roleID})
}
