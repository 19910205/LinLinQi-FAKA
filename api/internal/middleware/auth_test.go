package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func TestIssueVersionedTokenCarriesAdminSessionVersion(t *testing.T) {
	const secret = "admin-session-version-test-secret"
	tokenString, err := IssueVersionedToken("admin-id", "admin", "super_admin", secret, 7, time.Minute)
	if err != nil {
		t.Fatalf("issue versioned token: %v", err)
	}
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(_ *jwt.Token) (any, error) {
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !token.Valid {
		t.Fatalf("parse versioned token: %v", err)
	}
	claims, ok := token.Claims.(*Claims)
	if !ok {
		t.Fatal("unexpected claims type")
	}
	if claims.Realm != "admin" || claims.Role != "super_admin" || claims.SessionVersion != 7 {
		t.Fatalf("unexpected claims: %#v", claims)
	}
}

func TestJWTMiddlewareRejectsSignedTokenWithoutExpiration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const secret = "required-expiration-test-secret"
	tokenString, err := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		Realm: "user",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:  "user-id",
			IssuedAt: jwt.NewNumericDate(time.Now()),
		},
	}).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign token without expiration: %v", err)
	}
	for name, auth := range map[string]gin.HandlerFunc{
		"required":         JWT(secret, "user"),
		"optional-present": OptionalJWT(secret, "user"),
	} {
		t.Run(name, func(t *testing.T) {
			router := gin.New()
			router.Use(auth)
			router.GET("/protected", func(c *gin.Context) { c.Status(http.StatusNoContent) })
			request := httptest.NewRequest(http.MethodGet, "/protected", nil)
			request.Header.Set("Authorization", "Bearer "+tokenString)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("token without exp returned HTTP %d", recorder.Code)
			}
		})
	}
}

func TestRequiredOpenAPIPermissionProtectsBalanceAsBillingData(t *testing.T) {
	for _, test := range []struct {
		method, path, want string
	}{
		{"GET", "/openapi/v1/products", "products:read"},
		{"POST", "/openapi/v1/orders", "orders:write"},
		{"GET", "/openapi/v1/orders/LQ-1", "orders:read"},
		{"GET", "/openapi/v1/account/balance", "orders:write"},
	} {
		if got := requiredOpenAPIPermission(test.method, test.path); got != test.want {
			t.Fatalf("%s %s permission = %s, want %s", test.method, test.path, got, test.want)
		}
	}
}
