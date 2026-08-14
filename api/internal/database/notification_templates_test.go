package database

import (
	"fmt"
	"strings"
	"testing"
)

func TestDetailedNotificationTemplateCatalogCoverage(t *testing.T) {
	if len(notificationTemplateLocales) != 8 {
		t.Fatalf("expected 8 locales, got %d", len(notificationTemplateLocales))
	}
	seen := map[string]bool{}
	total := 0
	for _, locale := range notificationTemplateLocales {
		copy, ok := notificationLocaleCopies[locale]
		if !ok || copy.AdminBody == "" || copy.UserBody == "" || copy.Subject == "" {
			t.Fatalf("locale %s does not have complete notification copy", locale)
		}
		if got := len(notificationLocalizedEventNames[locale]); got != len(notificationAdminEventCodes) {
			t.Fatalf("locale %s has %d event names, want %d", locale, got, len(notificationAdminEventCodes))
		}
		for _, eventCode := range notificationAdminEventCodes {
			eventName := localizedNotificationEventName(locale, eventCode)
			if eventName == eventCode {
				t.Fatalf("locale %s is missing a localized name for %s", locale, eventCode)
			}
			body := fmt.Sprintf(copy.AdminBody, eventName)
			if len([]rune(body)) < 250 || strings.Contains(body, "%!") {
				t.Fatalf("locale %s admin template %s is incomplete", locale, eventCode)
			}
			for _, channel := range notificationAdminChannels {
				code := notificationTemplateCode("admin", eventCode, channel, locale)
				if seen[code] {
					t.Fatalf("duplicate template code %s", code)
				}
				seen[code] = true
				total++
			}
		}
		for _, eventCode := range notificationUserEventCodes {
			eventName := localizedNotificationEventName(locale, eventCode)
			body := fmt.Sprintf(copy.UserBody, eventName)
			if len([]rune(body)) < 200 || strings.Contains(body, "%!") {
				t.Fatalf("locale %s user template %s is incomplete", locale, eventCode)
			}
			for _, channel := range notificationUserChannels {
				code := notificationTemplateCode("user", eventCode, channel, locale)
				if seen[code] {
					t.Fatalf("duplicate template code %s", code)
				}
				seen[code] = true
				total++
			}
		}
	}
	want := len(notificationTemplateLocales) * (len(notificationAdminEventCodes)*len(notificationAdminChannels) + len(notificationUserEventCodes)*len(notificationUserChannels))
	if total != want || total != 1072 {
		t.Fatalf("generated %d templates, want %d", total, want)
	}
}
