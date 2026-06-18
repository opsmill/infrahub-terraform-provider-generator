package parser

import (
	"flag"
	"go/format"
	"os"
	"path/filepath"
	"testing"

	"github.com/opsmill/infrahub-terraform-provider-generator/pkg/schema"
)

// update regenerates the golden files instead of comparing against them:
//
//	go test ./pkg/parser -run TestGolden -update
//
// Golden files are the gofmt-formatted source the generator writes, so a
// template change shows up as a reviewable diff under testdata/.
var update = flag.Bool("update", false, "update golden files")

// assertGolden compares the gofmt-formatted generated source against the
// committed golden file, or rewrites it when -update is set.
func assertGolden(t *testing.T, name, code string) {
	t.Helper()

	formatted, err := format.Source([]byte(code))
	if err != nil {
		t.Fatalf("generated %s is not valid Go: %v\n---\n%s", name, err, code)
	}

	path := filepath.Join("testdata", name+".go.golden")
	if *update {
		if err := os.WriteFile(path, formatted, 0o644); err != nil {
			t.Fatalf("writing golden %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden %s (run `go test ./pkg/parser -run TestGolden -update`): %v", path, err)
	}
	if string(formatted) != string(want) {
		t.Errorf("generated %s differs from golden %s; if the change is intentional, re-run with -update", name, path)
	}
}

func mustParse(t *testing.T, gql string, reg *schema.Registry) *InputGraphQLQuery {
	t.Helper()
	parsed, err := parseGraphQLQuery(gql, reg)
	if err != nil {
		t.Fatalf("parseGraphQLQuery: %v", err)
	}
	return parsed
}

func TestGoldenResourceKeyed(t *testing.T) {
	code, err := generateTerraformResource(mustParse(t, sampleResourceGQL, nil))
	if err != nil {
		t.Fatalf("generateTerraformResource: %v", err)
	}
	assertGolden(t, "resource_keyed", code)
}

func TestGoldenResourceTyped(t *testing.T) {
	code, err := generateTerraformResource(mustParse(t, sampleTypedResourceGQL, typedVCenterRegistry()))
	if err != nil {
		t.Fatalf("generateTerraformResource: %v", err)
	}
	assertGolden(t, "resource_typed", code)
}

func TestGoldenDataSourceKeyed(t *testing.T) {
	code, err := generateTerraformDataSource(mustParse(t, sampleDataSourceGQL, nil))
	if err != nil {
		t.Fatalf("generateTerraformDataSource: %v", err)
	}
	assertGolden(t, "datasource_keyed", code)
}

func TestGoldenDataSourceList(t *testing.T) {
	code, err := generateTerraformDataSource(mustParse(t, sampleListDataSourceGQL, nil))
	if err != nil {
		t.Fatalf("generateTerraformDataSource: %v", err)
	}
	assertGolden(t, "datasource_list", code)
}

func TestGoldenProvider(t *testing.T) {
	code, err := generateTerraformProvider(TerraformComponents{
		DataSources: []string{"dctVCenterByName", "Artifact"},
		Resources:   []string{"dctVCenterByName"},
	})
	if err != nil {
		t.Fatalf("generateTerraformProvider: %v", err)
	}
	assertGolden(t, "provider", code)
}

func TestGoldenArtifact(t *testing.T) {
	code, err := renderTemplate("artifact.gotmpl", nil)
	if err != nil {
		t.Fatalf("rendering artifact: %v", err)
	}
	assertGolden(t, "artifact", code)
}
