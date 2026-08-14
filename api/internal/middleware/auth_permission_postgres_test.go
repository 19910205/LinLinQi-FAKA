package middleware

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"linlinqi/api/internal/model"
)

func TestRequireAnyPermissionPostgreSQL(t *testing.T) {
	db := permissionPostgresTestDB(t)
	if err := db.AutoMigrate(
		&model.Admin{},
		&model.Role{},
		&model.Permission{},
		&model.AdminRole{},
		&model.RolePermission{},
	); err != nil {
		t.Fatalf("migrate permission models: %v", err)
	}

	permission := model.Permission{
		Code: "order.manage", Name: "Manage orders", Module: "order",
	}
	role := model.Role{Code: "order_manager", Name: "Order manager"}
	admin := model.Admin{
		Username: "permission-manager", PasswordHash: "not-used",
		Name: "Permission Manager", Role: role.Code, Status: "active",
	}
	if err := db.Create(&permission).Error; err != nil {
		t.Fatalf("create permission: %v", err)
	}
	if err := db.Create(&role).Error; err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if err := db.Create(&model.RolePermission{RoleID: role.ID, PermissionID: permission.ID}).Error; err != nil {
		t.Fatalf("grant permission: %v", err)
	}
	if err := db.Create(&model.AdminRole{AdminID: admin.ID, RoleID: role.ID}).Error; err != nil {
		t.Fatalf("assign role: %v", err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("subject", admin.ID.String())
		c.Next()
	})
	router.GET(
		"/product-options",
		RequireAnyPermission(db, "catalog.view", "order.manage", "marketing.manage"),
		func(c *gin.Context) { c.Status(http.StatusNoContent) },
	)
	router.GET(
		"/orders",
		RequireAnyPermission(db, "order.view", "payment.manage"),
		func(c *gin.Context) { c.Status(http.StatusNoContent) },
	)
	router.GET(
		"/supplier-options",
		RequireAnyPermission(db, "catalog.view", "supplier.view"),
		func(c *gin.Context) { c.Status(http.StatusNoContent) },
	)

	for _, test := range []struct {
		path string
		want int
	}{
		{path: "/product-options", want: http.StatusNoContent},
		// A manage grant also satisfies the matching view alternative.
		{path: "/orders", want: http.StatusNoContent},
		{path: "/supplier-options", want: http.StatusForbidden},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != test.want {
			t.Errorf("GET %s returned %d, want %d", test.path, response.Code, test.want)
		}
	}
}

func permissionPostgresTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("LINLINQI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("LINLINQI_TEST_DATABASE_URL is not set")
	}
	adminDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	schemaName := "linlinqi_permission_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := adminDB.Exec(`CREATE SCHEMA "` + schemaName + `"`).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_ = adminDB.Exec(`DROP SCHEMA "` + schemaName + `" CASCADE`).Error
		if sqlDB, dbErr := adminDB.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse PostgreSQL URL: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schemaName)
	parsed.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open isolated permission schema: %v", err)
	}
	return db
}
