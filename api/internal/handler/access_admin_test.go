package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"linlinqi/api/internal/model"
)

func TestAccessPasswordPolicy(t *testing.T) {
	for _, password := range []string{"M7#oranges-Cobalt", "V4!RiverStone-Moon"} {
		if err := validateAccessPassword(password, "ops.manager"); err != nil {
			t.Fatalf("strong password rejected: %q: %v", password, err)
		}
	}
	for _, password := range []string{
		"Short7!a",
		"alllowercase7!",
		"ALLUPPERCASE7!",
		"NoNumbersHere!",
		"NoSymbolsHere7",
		"Has WhiteSpace7!",
		"Ops.Manager7!Secure",
		"Password7!StillWeak",
		strings.Repeat("A", 70) + "a7!",
	} {
		if err := validateAccessPassword(password, "ops.manager"); err == nil {
			t.Fatalf("weak password accepted: %q", password)
		}
	}
}

func TestAccessAdminAndRoleRequestNormalization(t *testing.T) {
	roleID := uuid.New()
	adminRequest := accessAdminCreateRequest{
		Username: " OPS.Manager ", Name: " 运营主管 ", Password: "M7#oranges-Cobalt", Status: " ACTIVE ", RoleIDs: []uuid.UUID{roleID},
	}
	if err := adminRequest.normalizeAndValidate(); err != nil {
		t.Fatalf("valid administrator request rejected: %v", err)
	}
	if adminRequest.Username != "ops.manager" || adminRequest.Status != "active" || adminRequest.Name != "运营主管" {
		t.Fatalf("administrator request was not normalized: %#v", adminRequest)
	}

	permissionIDs := []uuid.UUID{uuid.New(), uuid.New()}
	roleRequest := accessRoleRequest{Code: " SUPPORT_LEAD ", Name: " 支持主管 ", Description: " 工单与售后权限 ", PermissionIDs: permissionIDs}
	if err := roleRequest.normalizeAndValidate(); err != nil {
		t.Fatalf("valid role request rejected: %v", err)
	}
	if roleRequest.Code != "support_lead" || roleRequest.Name != "支持主管" {
		t.Fatalf("role request was not normalized: %#v", roleRequest)
	}

	duplicate := accessRoleRequest{Code: "duplicate_role", Name: "重复权限", PermissionIDs: []uuid.UUID{permissionIDs[0], permissionIDs[0]}}
	if err := duplicate.normalizeAndValidate(); err == nil {
		t.Fatal("role request accepted duplicate permission identifiers")
	}
}

func TestAccessRequestsRejectServerOwnedFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name   string
		method string
		body   string
		target any
	}{
		{
			name: "administrator hash", method: http.MethodPost,
			body:   `{"username":"operator","name":"Operator","password":"M7#oranges-Cobalt","status":"active","role_ids":[],"password_hash":"attacker"}`,
			target: &accessAdminCreateRequest{},
		},
		{
			name: "administrator session version", method: http.MethodPatch,
			body:   `{"status":"active","session_version":0}`,
			target: &accessAdminUpdateRequest{},
		},
		{
			name: "system role flag", method: http.MethodPost,
			body:   `{"code":"operator","name":"Operator","description":"","permission_ids":[],"system":true}`,
			target: &accessRoleRequest{},
		},
		{
			name: "password target hash", method: http.MethodPost,
			body:   `{"current_password":"current","new_password":"M7#oranges-Cobalt","password_hash":"attacker"}`,
			target: &accessAdminPasswordRequest{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(test.method, "/admin/v1/access", strings.NewReader(test.body))
			if err := decodeStrictJSON(context, test.target); err == nil {
				t.Fatal("request accepted a server-owned field")
			}
		})
	}
}

func TestAccessAdminDTONeverSerializesCredentials(t *testing.T) {
	admin := model.Admin{
		Base:     model.Base{ID: uuid.New(), CreatedAt: time.Now(), UpdatedAt: time.Now()},
		Username: "operator", Name: "Operator", Status: "active", PasswordHash: "bcrypt-secret", SessionVersion: 42,
	}
	payload, err := json.Marshal(toAccessAdminDTO(admin, []accessNamedRoleDTO{{ID: uuid.New(), Code: "operator", Name: "运营员"}}, true))
	if err != nil {
		t.Fatalf("marshal safe administrator DTO: %v", err)
	}
	body := string(payload)
	for _, forbidden := range []string{"bcrypt-secret", "password_hash", "session_version", "secret_cipher", "recovery_hashes"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("safe administrator DTO leaked %q: %s", forbidden, body)
		}
	}
	for _, expected := range []string{`"username":"operator"`, `"totp_enabled":true`, `"role_ids"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("safe administrator DTO omitted %q: %s", expected, body)
		}
	}
}

func TestParseAccessAuditTime(t *testing.T) {
	start, err := parseAccessAuditTime("2026-08-09", false)
	if err != nil {
		t.Fatalf("parse audit start: %v", err)
	}
	end, err := parseAccessAuditTime("2026-08-09", true)
	if err != nil {
		t.Fatalf("parse audit end: %v", err)
	}
	if end.Sub(start) != 24*time.Hour {
		t.Fatalf("date-only audit filter should cover one day, got %s", end.Sub(start))
	}
	instant, err := parseAccessAuditTime("2026-08-09T12:30:00+08:00", true)
	if err != nil || !instant.Equal(time.Date(2026, 8, 9, 4, 30, 0, 0, time.UTC)) {
		t.Fatalf("RFC3339 audit time not normalized to UTC: %s, %v", instant, err)
	}
}

func TestActiveAccessSystemManagerCountPostgreSQL(t *testing.T) {
	db := authTestPostgreSQL(t)
	if err := db.AutoMigrate(&model.Admin{}, &model.Role{}, &model.Permission{}, &model.AdminRole{}, &model.RolePermission{}); err != nil {
		t.Fatalf("migrate access control models: %v", err)
	}
	permission := model.Permission{Code: "system.manage", Name: "管理系统", Module: "system"}
	if err := db.Create(&permission).Error; err != nil {
		t.Fatalf("create permission: %v", err)
	}
	roles := []model.Role{
		{Code: "manager_one", Name: "管理员一"},
		{Code: "manager_two", Name: "管理员二"},
	}
	if err := db.Create(&roles).Error; err != nil {
		t.Fatalf("create roles: %v", err)
	}
	admin := model.Admin{Username: "root.manager", PasswordHash: "not-used", Name: "Root", Role: roles[0].Code, Status: "active"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create administrator: %v", err)
	}
	for _, role := range roles {
		if err := db.Create(&model.RolePermission{RoleID: role.ID, PermissionID: permission.ID}).Error; err != nil {
			t.Fatalf("grant system permission: %v", err)
		}
		if err := db.Create(&model.AdminRole{AdminID: admin.ID, RoleID: role.ID}).Error; err != nil {
			t.Fatalf("assign administrator role: %v", err)
		}
	}
	count, err := activeAccessSystemManagerCount(db)
	if err != nil {
		t.Fatalf("count active system managers: %v", err)
	}
	if count != 1 {
		t.Fatalf("one administrator with two system roles must count once, got %d", count)
	}
	if err := db.Model(&admin).Update("status", "disabled").Error; err != nil {
		t.Fatalf("disable administrator: %v", err)
	}
	count, err = activeAccessSystemManagerCount(db)
	if err != nil || count != 0 {
		t.Fatalf("disabled administrator must not satisfy invariant, count=%d err=%v", count, err)
	}
}
