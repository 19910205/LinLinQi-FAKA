package handler

import (
	"errors"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"linlinqi/api/internal/model"
	"linlinqi/api/pkg/response"
)

// adminSessionProfile is deliberately narrower than model.Admin. Authentication
// responses must never grow into a transport for password hashes, session
// versions, TOTP material, or any other persistence-only field.
type adminSessionProfile struct {
	ID          uuid.UUID `json:"id"`
	Username    string    `json:"username"`
	Name        string    `json:"name"`
	Role        string    `json:"role"`
	Permissions []string  `json:"permissions"`
}

func loadAdminSessionProfile(db *gorm.DB, adminID uuid.UUID) (adminSessionProfile, error) {
	var admin model.Admin
	if err := db.Select("id", "username", "name", "role").First(&admin, "id = ? AND status = ?", adminID, "active").Error; err != nil {
		return adminSessionProfile{}, err
	}
	var permissions []string
	if err := db.Table("admin_roles ar").
		Distinct("p.code").
		Joins("JOIN roles r ON r.id = ar.role_id AND r.deleted_at IS NULL").
		Joins("JOIN role_permissions rp ON rp.role_id = r.id").
		Joins("JOIN permissions p ON p.id = rp.permission_id AND p.deleted_at IS NULL").
		Where("ar.admin_id = ?", adminID).
		Pluck("p.code", &permissions).Error; err != nil {
		return adminSessionProfile{}, err
	}
	for index := range permissions {
		permissions[index] = strings.TrimSpace(permissions[index])
	}
	sort.Strings(permissions)
	return adminSessionProfile{
		ID: admin.ID, Username: admin.Username, Name: admin.Name,
		Role: admin.Role, Permissions: permissions,
	}, nil
}

// AdminSession refreshes the browser's permission-aware navigation after a
// reload. JWT and session-version middleware already authenticate this route;
// it intentionally has no business permission requirement so every active
// administrator can discover only the modules their roles grant.
func (h Handler) AdminSession(c *gin.Context) {
	adminID, err := uuid.Parse(c.GetString("subject"))
	if err != nil || adminID == uuid.Nil {
		response.Error(c, 401, 40103, "error.invalid_admin_identity")
		return
	}
	profile, err := loadAdminSessionProfile(h.DB, adminID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Error(c, 401, 40103, "error.invalid_admin_identity")
			return
		}
		response.Error(c, 500, 50020, "error.login_session_create_failed")
		return
	}
	response.OK(c, profile)
}
