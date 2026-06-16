package parser

import (
	"strings"
	"testing"

	"github.com/opsmill/infrahub-terraform-provider-generator/pkg/schema"
)

// sampleResourceGQL mirrors the documented resource layout: the mutation
// operations use the <Kind><Op> naming while the lookup query uses a
// different alias (…ByName), and the key attribute (vcenter_name) is only a
// filter variable, not a selected node field.
const sampleResourceGQL = `mutation DctVCenterCreate($data: DctVCenterCreateInput!) {
  DctVCenterCreate(data: $data) {
    object {
      id
      vcenter_name { value }
      fqdn { value }
      version { value }
    }
  }
}

mutation DctVCenterUpsert($data: DctVCenterUpsertInput!) {
  DctVCenterUpsert(data: $data) {
    object {
      id
      vcenter_name { value }
      fqdn { value }
      version { value }
    }
  }
}

mutation DctVCenterDelete($id: String!) {
  DctVCenterDelete(data: { id: $id }) {
    ok
  }
}

query DctVCenterByName($vcenter_name: String!) {
  DctVCenter(vcenter_name__value: $vcenter_name) {
    edges {
      node {
        id
        fqdn { value }
        version { value }
      }
    }
  }
}
`

func TestGenerateTerraformResource(t *testing.T) {
	parsed, err := parseGraphQLQuery(sampleResourceGQL, nil)
	if err != nil {
		t.Fatalf("parseGraphQLQuery returned error: %v", err)
	}

	code, err := generateTerraformResource(parsed)
	if err != nil {
		t.Fatalf("generateTerraformResource returned error: %v", err)
	}

	mustContain := []string{
		// Bug 1: the key attribute must be a struct field and a required schema attribute.
		"Vcenter_name types.String `tfsdk:\"vcenter_name\"`",
		"\"vcenter_name\": schema.StringAttribute{",
		// Bug 2: inputs must be wrapped in the Infrahub input types, not raw strings.
		"infrahub_sdk.TextAttributeCreate{Value: plan.Vcenter_name.ValueString()}",
		"infrahub_sdk.TextAttributeCreate{Value: plan.Edges_node_fqdn.ValueString()}",
		// Update carries the prior value forward via plan/state guards (no setDefault).
		"infrahub_sdk.TextAttributeUpdate{Value: plan.Edges_node_fqdn.ValueString()}",
		"} else if !state.Edges_node_fqdn.IsNull() {",
		"infrahub_sdk.TextAttributeUpdate{Value: state.Edges_node_fqdn.ValueString()}",
		// Bug 3: mutation responses must read the nested .Value.
		"response.DctVCenterCreate.Object.Fqdn.Value",
		"response.DctVCenterUpsert.Object.Version.Value",
		// Function names must match the genqlient functions (the real operation names).
		"infrahub_sdk.DctVCenterByName(ctx,",
		"infrahub_sdk.DctVCenterCreate(ctx,",
		"infrahub_sdk.DctVCenterUpsert(ctx,",
		"infrahub_sdk.DctVCenterDelete(ctx,",
	}
	for _, want := range mustContain {
		if !strings.Contains(code, want) {
			t.Errorf("generated resource is missing expected fragment:\n%s", want)
		}
	}

	mustNotContain := []string{
		// The old derivation appended Create/Upsert/Delete to the query alias,
		// producing functions that genqlient never generates.
		"DctVCenterByNameCreate",
		"DctVCenterByNameUpsert",
		"DctVCenterByNameDelete",
		// Raw string assignments to the typed input fields.
		"= plan.Edges_node_fqdn.ValueString()\n",
	}
	for _, unwanted := range mustNotContain {
		if strings.Contains(code, unwanted) {
			t.Errorf("generated resource still contains regression fragment:\n%s", unwanted)
		}
	}
}

// sampleResourceWithIDAttrGQL mirrors a DctProject resource whose node selects a
// regular scalar attribute (id_projet) whose name merely contains the substring
// "id". Such attributes must remain configurable; only the node's own UUID
// (read through GetId()) is server-assigned and read-only.
const sampleResourceWithIDAttrGQL = `mutation DctProjectCreate($data: DctProjectCreateInput!) {
  DctProjectCreate(data: $data) {
    object {
      id
      id_projet { value }
    }
  }
}

mutation DctProjectUpsert($data: DctProjectUpsertInput!) {
  DctProjectUpsert(data: $data) {
    object {
      id
      id_projet { value }
    }
  }
}

mutation DctProjectDelete($id: String!) {
  DctProjectDelete(data: { id: $id }) {
    ok
  }
}

query DctProjectByName($nom: String!) {
  DctProject(nom__value: $nom) {
    edges {
      node {
        id
        id_projet { value }
      }
    }
  }
}
`

// TestResourceIDLikeAttributeIsConfigurable guards against the regression where
// any attribute whose name contained "id" (id_projet, vlan_id, …) was forced
// read-only (Computed) and could not be set on create.
func TestResourceIDLikeAttributeIsConfigurable(t *testing.T) {
	parsed, err := parseGraphQLQuery(sampleResourceWithIDAttrGQL, nil)
	if err != nil {
		t.Fatalf("parseGraphQLQuery returned error: %v", err)
	}

	code, err := generateTerraformResource(parsed)
	if err != nil {
		t.Fatalf("generateTerraformResource returned error: %v", err)
	}

	// A configurable attribute is assigned into the create input; a read-only
	// one never is. This assignment proves id_projet is settable.
	if want := "plan.Edges_node_id_projet.ValueString()"; !strings.Contains(code, want) {
		t.Errorf("id_projet should be configurable (assigned on create), missing %q:\n%s", want, code)
	}

	// The node's own id must stay read-only: it is never assigned on create.
	if bad := "plan.Edges_node_id.ValueString()"; strings.Contains(code, bad) {
		t.Errorf("the node id must stay read-only, but %q appears on create:\n%s", bad, code)
	}

	assertGofmt(t, code)
}

// TestCreateSkipsUnsetOptionalAttributes guards against unset optional
// attributes being sent to Infrahub as empty values, which the API rejects for
// non-text kinds (e.g. a Number attribute fails with "Expected type 'BigInt'").
func TestCreateSkipsUnsetOptionalAttributes(t *testing.T) {
	parsed, err := parseGraphQLQuery(sampleResourceGQL, nil)
	if err != nil {
		t.Fatalf("parseGraphQLQuery returned error: %v", err)
	}

	code, err := generateTerraformResource(parsed)
	if err != nil {
		t.Fatalf("generateTerraformResource returned error: %v", err)
	}

	// fqdn is optional; on create it must only be sent when the user set it.
	if want := "if !plan.Edges_node_fqdn.IsNull()"; !strings.Contains(code, want) {
		t.Errorf("create should guard unset optional attributes, missing %q:\n%s", want, code)
	}

	assertGofmt(t, code)
}

// TestUpdateSkipsEmptyOptionalAttributes guards against the same empty-value
// rejection on the upsert path: an optional attribute that is set in neither
// the plan nor the prior state must not be sent at all.
func TestUpdateSkipsEmptyOptionalAttributes(t *testing.T) {
	parsed, err := parseGraphQLQuery(sampleResourceGQL, nil)
	if err != nil {
		t.Fatalf("parseGraphQLQuery returned error: %v", err)
	}

	code, err := generateTerraformResource(parsed)
	if err != nil {
		t.Fatalf("generateTerraformResource returned error: %v", err)
	}

	// The plan value is sent only if set; otherwise the prior state value is
	// sent only if set; if neither is set, nothing is sent (empty never goes out).
	for _, want := range []string{
		"if !plan.Edges_node_fqdn.IsNull() {",
		"} else if !state.Edges_node_fqdn.IsNull() {",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("update should guard empty optional attributes, missing %q:\n%s", want, code)
		}
	}

	assertGofmt(t, code)
}

const sampleTypedResourceGQL = `mutation DctVCenterCreate($data: DctVCenterCreateInput!) {
  DctVCenterCreate(data: $data) {
    object {
      id
      vcenter_name { value }
      total_vcpu { value }
      is_active { value }
    }
  }
}

mutation DctVCenterUpsert($data: DctVCenterUpsertInput!) {
  DctVCenterUpsert(data: $data) {
    object {
      id
      vcenter_name { value }
      total_vcpu { value }
      is_active { value }
    }
  }
}

mutation DctVCenterDelete($id: String!) {
  DctVCenterDelete(data: { id: $id }) {
    ok
  }
}

query DctVCenterByName($vcenter_name: String!) {
  DctVCenter(vcenter_name__value: $vcenter_name) {
    edges {
      node {
        id
        total_vcpu { value }
        is_active { value }
      }
    }
  }
}
`

func typedVCenterRegistry() *schema.Registry {
	return schema.NewRegistry(map[string]map[string]schema.Attribute{
		"DctVCenter": {
			"vcenter_name": {Kind: "Text", Optional: false},
			"total_vcpu":   {Kind: "Number", Optional: true},
			"is_active":    {Kind: "Boolean", Optional: false},
		},
	})
}

func TestResourceSchemaUsesTypedAttributes(t *testing.T) {
	parsed, err := parseGraphQLQuery(sampleTypedResourceGQL, typedVCenterRegistry())
	if err != nil {
		t.Fatalf("parseGraphQLQuery returned error: %v", err)
	}
	code, err := generateTerraformResource(parsed)
	if err != nil {
		t.Fatalf("generateTerraformResource returned error: %v", err)
	}

	mustContain := []string{
		// Number -> Int64, optional -> Optional+Computed.
		"\"total_vcpu\": schema.Int64Attribute{",
		"Edges_node_total_vcpu types.Int64 `tfsdk:\"total_vcpu\"`",
		// Boolean -> Bool, schema-required -> Required (no Computed/Optional).
		"\"is_active\": schema.BoolAttribute{",
		"Edges_node_is_active types.Bool `tfsdk:\"is_active\"`",
	}
	for _, want := range mustContain {
		if !strings.Contains(code, want) {
			t.Errorf("typed schema missing fragment:\n%s\n---\n%s", want, code)
		}
	}
	assertGofmt(t, code)
}

// sampleNumericKeyResourceGQL has a numeric filter key (asn) declared as BigInt
// in the lookup query, with a separate text attribute selected in the node.
const sampleNumericKeyResourceGQL = `mutation DctThingCreate($data: DctThingCreateInput!) {
  DctThingCreate(data: $data) {
    object {
      id
      label { value }
    }
  }
}

mutation DctThingUpsert($data: DctThingUpsertInput!) {
  DctThingUpsert(data: $data) {
    object {
      id
      label { value }
    }
  }
}

mutation DctThingDelete($id: String!) {
  DctThingDelete(data: { id: $id }) {
    ok
  }
}

query DctThingByName($asn: BigInt!) {
  DctThing(asn__value: $asn) {
    edges {
      node {
        id
        label { value }
      }
    }
  }
}
`

// TestRequiredKeyAttributeIsTyped verifies the filter/key attribute is typed
// from the schema rather than always being a string. For a Number key the
// struct field, schema attribute, create/update wrapper, and the lookup
// accessor must all use the Int64 forms.
func TestRequiredKeyAttributeIsTyped(t *testing.T) {
	reg := schema.NewRegistry(map[string]map[string]schema.Attribute{
		"DctThing": {
			"asn":   {Kind: "Number", Optional: false},
			"label": {Kind: "Text", Optional: true},
		},
	})

	parsed, err := parseGraphQLQuery(sampleNumericKeyResourceGQL, reg)
	if err != nil {
		t.Fatalf("parseGraphQLQuery returned error: %v", err)
	}
	code, err := generateTerraformResource(parsed)
	if err != nil {
		t.Fatalf("generateTerraformResource returned error: %v", err)
	}

	mustContain := []string{
		"Asn types.Int64 `tfsdk:\"asn\"`",
		"\"asn\": schema.Int64Attribute{",
		"infrahub_sdk.NumberAttributeCreate{Value: plan.Asn.ValueInt64()}",
		"infrahub_sdk.NumberAttributeUpdate{Value: plan.Asn.ValueInt64()}",
		// The lookup passes the key with its typed accessor (the .gql declares $asn: BigInt!).
		"infrahub_sdk.DctThingByName(ctx, *r.client, state.Asn.ValueInt64())",
	}
	for _, want := range mustContain {
		if !strings.Contains(code, want) {
			t.Errorf("typed key attribute missing fragment:\n%s\n---\n%s", want, code)
		}
	}

	// The key must not be sent as a text attribute.
	if strings.Contains(code, "TextAttributeCreate{Value: plan.Asn.ValueString()}") {
		t.Errorf("numeric key still sent as TextAttributeCreate:\n%s", code)
	}

	assertGofmt(t, code)
}

func TestResourceCreateUpdateReadAreTyped(t *testing.T) {
	parsed, err := parseGraphQLQuery(sampleTypedResourceGQL, typedVCenterRegistry())
	if err != nil {
		t.Fatalf("parseGraphQLQuery returned error: %v", err)
	}
	code, err := generateTerraformResource(parsed)
	if err != nil {
		t.Fatalf("generateTerraformResource returned error: %v", err)
	}

	mustContain := []string{
		// Number optional: guarded create with the Int64 accessor + Number wrapper.
		"if !plan.Edges_node_total_vcpu.IsNull() {",
		"infrahub_sdk.NumberAttributeCreate{Value: plan.Edges_node_total_vcpu.ValueInt64()}",
		// Boolean required: always sent with the Bool accessor + Checkbox wrapper.
		"infrahub_sdk.CheckboxAttributeCreate{Value: plan.Edges_node_is_active.ValueBool()}",
		// Update: typed wrappers; optional Number carries forward via plan/state.
		"infrahub_sdk.NumberAttributeUpdate{Value: plan.Edges_node_total_vcpu.ValueInt64()}",
		"} else if !state.Edges_node_total_vcpu.IsNull() {",
		"infrahub_sdk.NumberAttributeUpdate{Value: state.Edges_node_total_vcpu.ValueInt64()}",
		// Read: typed constructors back into framework values.
		"state.Edges_node_total_vcpu = types.Int64Value(response.",
		"state.Edges_node_is_active = types.BoolValue(response.",
		// Create and Upsert response write-backs are typed too.
		"plan.Edges_node_total_vcpu = types.Int64Value(response.DctVCenterCreate.Object.",
		"plan.Edges_node_total_vcpu = types.Int64Value(response.DctVCenterUpsert.Object.",
	}
	for _, want := range mustContain {
		if !strings.Contains(code, want) {
			t.Errorf("typed create/update/read missing fragment:\n%s\n---\n%s", want, code)
		}
	}

	// setDefault is gone; no field should reference it.
	if strings.Contains(code, "setDefault(") {
		t.Errorf("generated code still references the removed setDefault helper:\n%s", code)
	}

	assertGofmt(t, code)
}
