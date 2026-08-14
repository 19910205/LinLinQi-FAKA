package service

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"linlinqi/api/internal/supply"
)

func TestNormalizeSupplierInputFieldsPreservesLocalizedExternalKey(t *testing.T) {
	fields, mapping, err := NormalizeSupplierInputFields(uuid.New(), []supply.ProductInputField{
		{Key: "field_1234", ExternalKey: "账号邮箱", Label: "账号邮箱", InputType: "email", Required: true, Sensitive: true, MaxLength: 190},
		{Key: "region", ExternalKey: "region", Label: "地区", InputType: "select", Options: []string{"US", "JP"}, MaxLength: 20},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 2 || mapping["field_1234"] != "账号邮箱" || !fields[0].PassToSupplier || !fields[0].Sensitive {
		t.Fatalf("normalized supplier fields mismatch: %#v %#v", fields, mapping)
	}
	var options []string
	if json.Unmarshal(fields[1].Options, &options) != nil || len(options) != 2 {
		t.Fatalf("select options were lost: %s", fields[1].Options)
	}
}

func TestNormalizeSupplierInputFieldsRejectsUnsafeSchema(t *testing.T) {
	for name, field := range map[string]supply.ProductInputField{
		"path target":  {Key: "account", ExternalKey: "../../account", Label: "Account", InputType: "text", MaxLength: 20},
		"bad pattern":  {Key: "account", ExternalKey: "account", Label: "Account", InputType: "text", ValidationPattern: "(?=x)", MaxLength: 20},
		"empty select": {Key: "account", ExternalKey: "account", Label: "Account", InputType: "select", MaxLength: 20},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := NormalizeSupplierInputFields(uuid.New(), []supply.ProductInputField{field}); err == nil {
				t.Fatal("unsafe supplier field accepted")
			}
		})
	}
}
