package parser

import "testing"

func TestTypeHelpers(t *testing.T) {
	cases := []struct {
		kind                                                       string
		tfType, tfAttr, sdkCreate, sdkUpdate, writeAcc, readCtorFn string
	}{
		{"Number", "types.Int64", "schema.Int64Attribute",
			"infrahub_sdk.NumberAttributeCreate", "infrahub_sdk.NumberAttributeUpdate",
			"ValueInt64()", "types.Int64Value"},
		{"Boolean", "types.Bool", "schema.BoolAttribute",
			"infrahub_sdk.CheckboxAttributeCreate", "infrahub_sdk.CheckboxAttributeUpdate",
			"ValueBool()", "types.BoolValue"},
		{"Checkbox", "types.Bool", "schema.BoolAttribute",
			"infrahub_sdk.CheckboxAttributeCreate", "infrahub_sdk.CheckboxAttributeUpdate",
			"ValueBool()", "types.BoolValue"},
		{"Text", "types.String", "schema.StringAttribute",
			"infrahub_sdk.TextAttributeCreate", "infrahub_sdk.TextAttributeUpdate",
			"ValueString()", "types.StringValue"},
		{"Dropdown", "types.String", "schema.StringAttribute",
			"infrahub_sdk.TextAttributeCreate", "infrahub_sdk.TextAttributeUpdate",
			"ValueString()", "types.StringValue"},
		{"", "types.String", "schema.StringAttribute",
			"infrahub_sdk.TextAttributeCreate", "infrahub_sdk.TextAttributeUpdate",
			"ValueString()", "types.StringValue"},
	}
	for _, c := range cases {
		f := GenqlientField{Kind: c.kind}
		if got := tfType(f); got != c.tfType {
			t.Errorf("tfType(%q) = %q, want %q", c.kind, got, c.tfType)
		}
		if got := tfAttr(f); got != c.tfAttr {
			t.Errorf("tfAttr(%q) = %q, want %q", c.kind, got, c.tfAttr)
		}
		if got := sdkCreate(f); got != c.sdkCreate {
			t.Errorf("sdkCreate(%q) = %q, want %q", c.kind, got, c.sdkCreate)
		}
		if got := sdkUpdate(f); got != c.sdkUpdate {
			t.Errorf("sdkUpdate(%q) = %q, want %q", c.kind, got, c.sdkUpdate)
		}
		if got := writeAccessor(f); got != c.writeAcc {
			t.Errorf("writeAccessor(%q) = %q, want %q", c.kind, got, c.writeAcc)
		}
		if got := readCtor(f); got != c.readCtorFn {
			t.Errorf("readCtor(%q) = %q, want %q", c.kind, got, c.readCtorFn)
		}
	}
}
