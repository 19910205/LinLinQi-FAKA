package response

import (
	"github.com/gin-gonic/gin"

	"linlinqi/api/internal/i18n"
)

type Envelope struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func OK(c *gin.Context, data interface{}) {
	c.JSON(200, Envelope{Code: 0, Message: i18n.Localize(c, "ok"), Data: data})
}

func Created(c *gin.Context, data interface{}) {
	c.JSON(201, Envelope{Code: 0, Message: i18n.Localize(c, "created"), Data: data})
}

// Error writes a localized error envelope. The message argument is an i18n
// key (e.g. "error.admin_list_fetch_failed"); the per-request localizer negotiated from the
// Accept-Language header translates it into the response message. Optional
// templateData supports Go template placeholders such as {{ .Min }} in
// parameterized messages. When the key is missing from the negotiated
// locale the key itself is returned, keeping messages stable for debugging.
func Error(c *gin.Context, status, code int, msgKey string, templateData ...interface{}) {
	msg := i18n.Localize(c, msgKey, templateData...)
	c.AbortWithStatusJSON(status, Envelope{Code: code, Message: msg})
}

func Page(c *gin.Context, items interface{}, total int64, page, pageSize int) {
	OK(c, gin.H{"items": items, "total": total, "page": page, "page_size": pageSize})
}
