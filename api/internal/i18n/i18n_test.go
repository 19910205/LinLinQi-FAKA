package i18n

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMessageTablesLoadSemanticKeys(t *testing.T) {
	keys := []string{
		"error.admin_list_fetch_failed",
		"error.notification_subscription_filter_invalid",
		"error.notification_subscription_list_failed",
		"error.notification_subscription_fields_invalid",
		"error.notification_subscription_not_found",
		"error.notification_subscription_conflict",
		"error.notification_subscription_id_invalid",
		"error.notification_connector_list_failed",
		"error.notification_connector_fields_invalid",
		"error.notification_connector_save_failed",
	}
	for _, locale := range []string{LocaleZH, LocaleTW, LocaleEN} {
		for _, key := range keys {
			msg := T(locale, key)
			if msg == "" || msg == key {
				t.Fatalf("%s returned raw semantic key: %q", locale, msg)
			}
		}
	}
}

func TestMessageTablesHaveIdenticalSemanticKeySets(t *testing.T) {
	for _, locale := range []string{LocaleTW, LocaleEN} {
		for key := range messages[LocaleZH] {
			if _, exists := messages[locale][key]; !exists {
				t.Fatalf("%s is missing semantic key %s", locale, key)
			}
		}
		for key := range messages[locale] {
			if _, exists := messages[LocaleZH][key]; !exists {
				t.Fatalf("%s has extra semantic key %s", locale, key)
			}
		}
	}
}

func TestLocalize(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	c.Set(CtxLocale, LocaleZH)
	const key = "error.admin_list_fetch_failed"
	got := Localize(c, key)
	if got == "" || got == key {
		t.Fatalf("localize failed: %q", got)
	}
	t.Logf("localize: %s", got)
}

func TestTemplateLocalize(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	c.Set(CtxLocale, LocaleZH)
	const key = "error.withdrawal_min_amount"
	got := Localize(c, key, map[string]interface{}{"Min": "100.00"})
	if got == "" || got == key || got == T(LocaleZH, key) {
		t.Fatalf("template localize failed: %q", got)
	}
	t.Logf("template: %s", got)
}
