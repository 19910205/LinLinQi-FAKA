package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestSupplierParameterMappingCanonicalValidation(t *testing.T) {
	encoded, err := EncodeSupplierParameterMapping(map[string]string{
		" account_email ": "Customer.Email",
		"region":          "region_code",
	})
	if err != nil {
		t.Fatalf("valid mapping rejected: %v", err)
	}
	if string(encoded) != `{"account_email":"Customer.Email","region":"region_code"}` {
		t.Fatalf("mapping was not canonicalized: %s", encoded)
	}
	decoded, err := DecodeSupplierParameterMapping(encoded)
	if err != nil || !reflect.DeepEqual(decoded, map[string]string{"account_email": "Customer.Email", "region": "region_code"}) {
		t.Fatalf("canonical mapping did not round trip: %#v %v", decoded, err)
	}
	localized, err := EncodeSupplierParameterMapping(map[string]string{"field_1234": "账号邮箱"})
	if err != nil || string(localized) != `{"field_1234":"账号邮箱"}` {
		t.Fatalf("documented localized upstream key was rejected: %s %v", localized, err)
	}

	tooMany := make(map[string]string, SupplierParameterMappingLimit+1)
	for index := 0; index <= SupplierParameterMappingLimit; index++ {
		tooMany[fmt.Sprintf("field_%d", index)] = fmt.Sprintf("remote_%d", index)
	}
	for name, mapping := range map[string]map[string]string{
		"too many":          tooMany,
		"unsafe local key":  {"../../account": "account"},
		"unsafe remote key": {"account": "../../account"},
		"oversized target":  {"account": "A" + strings.Repeat("x", 64)},
		"duplicate targets": {"account": "customer", "email": "customer"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := EncodeSupplierParameterMapping(mapping); !errors.Is(err, ErrSupplierParameterMappingInvalid) {
				t.Fatalf("invalid mapping accepted: %v", err)
			}
		})
	}
}

func TestSupplierParameterMappingRejectsAmbiguousJSON(t *testing.T) {
	for name, raw := range map[string]json.RawMessage{
		"null":             json.RawMessage(`null`),
		"array":            json.RawMessage(`[]`),
		"non-string value": json.RawMessage(`{"account":42}`),
		"duplicate source": json.RawMessage(`{"account":"first","account":"second"}`),
		"trailing data":    json.RawMessage(`{} {}`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeSupplierParameterMapping(raw); !errors.Is(err, ErrSupplierParameterMappingInvalid) {
				t.Fatalf("ambiguous JSON accepted: %v", err)
			}
		})
	}
}

func TestApplySupplierParameterMappingPreservesValuesAndRejectsCollisions(t *testing.T) {
	secret := "buyer-secret-value-that-must-not-enter-audit"
	mapped, err := ApplySupplierParameterMapping(
		map[string]string{"account_email": secret, "region": "cn-east"},
		json.RawMessage(`{"account_email":"Customer.Email"}`),
	)
	if err != nil {
		t.Fatalf("apply mapping: %v", err)
	}
	want := map[string]string{"Customer.Email": secret, "region": "cn-east"}
	if !reflect.DeepEqual(mapped, want) {
		t.Fatalf("mapped parameters mismatch: got %#v want %#v", mapped, want)
	}
	if _, err := ApplySupplierParameterMapping(
		map[string]string{"account_email": secret, "customer": "other"},
		json.RawMessage(`{"account_email":"customer"}`),
	); !errors.Is(err, ErrSupplierParameterMappingInvalid) {
		t.Fatalf("mapped/unmapped target collision accepted: %v", err)
	}
}
