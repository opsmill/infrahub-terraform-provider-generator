package parser

import (
	"strings"
	"testing"
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
	parsed, err := parseGraphQLQuery(sampleResourceGQL)
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
		"infrahub_sdk.TextAttributeUpdate{Value: setDefault(plan.Edges_node_fqdn.ValueString()",
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
