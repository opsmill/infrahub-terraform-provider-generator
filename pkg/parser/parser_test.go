package parser

import (
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sampleDataSourceGQL is the single-result lookup form: a key attribute
// (vcenter_name) filters the query and the response is expected to contain
// exactly one edge.
const sampleDataSourceGQL = `query DctVCenterByName($vcenter_name: String!) {
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

// sampleListDataSourceGQL is the unfiltered form that returns a list of
// objects, exercising the template's range-over-edges branch.
const sampleListDataSourceGQL = `query Devices {
  Device {
    edges {
      node {
        id
        name { value }
      }
    }
  }
}
`

func assertGofmt(t *testing.T, code string) {
	t.Helper()
	if _, err := format.Source([]byte(code)); err != nil {
		t.Fatalf("generated code is not valid gofmt-able Go: %v\n---\n%s", err, code)
	}
}

func TestGenerateTerraformDataSource(t *testing.T) {
	parsed, err := parseGraphQLQuery(sampleDataSourceGQL, nil)
	if err != nil {
		t.Fatalf("parseGraphQLQuery returned error: %v", err)
	}

	code, err := generateTerraformDataSource(parsed)
	if err != nil {
		t.Fatalf("generateTerraformDataSource returned error: %v", err)
	}

	mustContain := []string{
		// The key attribute is both a required schema attribute and a struct field.
		"\"vcenter_name\": schema.StringAttribute{",
		"Vcenter_name types.String `tfsdk:\"vcenter_name\"`",
		// The SDK lookup uses the real operation name.
		"infrahub_sdk.DctVCenterByName(ctx,",
		// Scalar values are read from the single edge.
		"response.DctVCenter.Edges[0].Node.Id",
	}
	for _, want := range mustContain {
		if !strings.Contains(code, want) {
			t.Errorf("generated data source is missing expected fragment:\n%s", want)
		}
	}

	assertGofmt(t, code)
}

func TestGenerateTerraformResourceIsGofmtClean(t *testing.T) {
	parsed, err := parseGraphQLQuery(sampleResourceGQL, nil)
	if err != nil {
		t.Fatalf("parseGraphQLQuery returned error: %v", err)
	}

	code, err := generateTerraformResource(parsed)
	if err != nil {
		t.Fatalf("generateTerraformResource returned error: %v", err)
	}

	assertGofmt(t, code)
}

func TestListDataSourceRangesByIndex(t *testing.T) {
	parsed, err := parseGraphQLQuery(sampleListDataSourceGQL, nil)
	if err != nil {
		t.Fatalf("parseGraphQLQuery returned error: %v", err)
	}

	code, err := generateTerraformDataSource(parsed)
	if err != nil {
		t.Fatalf("generateTerraformDataSource returned error: %v", err)
	}

	if !strings.Contains(code, "for i := range response.Device.Edges {") {
		t.Errorf("list data source should range by index, got:\n%s", code)
	}
	if strings.Contains(code, "for i, _ := range") {
		t.Errorf("generated code still uses the non-idiomatic `for i, _ := range` form")
	}
}

func TestReadAndGenerateWritesResource(t *testing.T) {
	dir := t.TempDir()

	dataSourceName, resourceName, err := ReadAndGenerateDataSourcesAndResources(sampleResourceGQL, dir, nil)
	if err != nil {
		t.Fatalf("ReadAndGenerateDataSourcesAndResources returned error: %v", err)
	}
	if dataSourceName != "" {
		t.Errorf("expected no data source name, got %q", dataSourceName)
	}
	if resourceName == "" {
		t.Fatal("expected a resource name, got none")
	}

	written, err := os.ReadFile(filepath.Join(dir, resourceName+"_resource.go"))
	if err != nil {
		t.Fatalf("expected generated resource file: %v", err)
	}
	assertGofmt(t, string(written))
}

// TestReadAndGenerateReturnsErrorOnInvalidQuery guards against a regression to
// the previous behaviour, where a parse failure called os.Exit(1) and killed
// the caller's process instead of returning an error.
func TestReadAndGenerateReturnsErrorOnInvalidQuery(t *testing.T) {
	_, _, err := ReadAndGenerateDataSourcesAndResources("this is not a valid graphql document", t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected an error for an unparseable query, got nil")
	}
}
