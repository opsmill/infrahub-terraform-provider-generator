package parser

import (
	"testing"

	"github.com/opsmill/infrahub-terraform-provider-generator/pkg/schema"
)

func TestParseStampsKindAndOptional(t *testing.T) {
	reg := schema.NewRegistry(map[string]map[string]schema.Attribute{
		"DctVCenter": {
			"total_vcpu": {Kind: "Number", Optional: true},
			"fqdn":       {Kind: "Text", Optional: false},
		},
	})

	parsed, err := parseGraphQLQuery(sampleResourceGQL, reg)
	if err != nil {
		t.Fatalf("parseGraphQLQuery returned error: %v", err)
	}

	byName := map[string]GenqlientField{}
	for _, f := range parsed.genqlientFieldsModify {
		byName[f.HumanReadableName] = f
	}

	if f, ok := byName["fqdn"]; !ok {
		t.Fatal("expected fqdn among configurable fields")
	} else if f.Kind != "Text" || f.Optional {
		t.Errorf("fqdn stamped %+v, want Kind=Text Optional=false", f)
	}
}

func TestParseNilRegistryDefaultsToOptionalString(t *testing.T) {
	parsed, err := parseGraphQLQuery(sampleResourceGQL, nil)
	if err != nil {
		t.Fatalf("parseGraphQLQuery returned error: %v", err)
	}
	for _, f := range parsed.genqlientFieldsModify {
		if f.Kind != "" || !f.Optional {
			t.Errorf("nil-registry field %q stamped %+v, want Kind=\"\" Optional=true", f.HumanReadableName, f)
		}
	}
}
