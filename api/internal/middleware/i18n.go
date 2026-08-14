package middleware

import (
	"github.com/gin-gonic/gin"

	appI18N "linlinqi/api/internal/i18n"
)

// I18N resolves the response language from the request (lang query param,
// X-Lang header, or Accept-Language header) and stores the resolved locale
// in the request context so every response.Error call can translate its
// message. It must be registered before any handler or auth middleware that
// emits messages.
func I18N() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(appI18N.CtxLocale, appI18N.ResolveLocale(c))
		c.Next()
	}
}
