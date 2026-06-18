package parser

import (
	"testing"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
)

// mustParseDoc parses a GraphQL document for tests, failing on a syntax error.
func mustParseDoc(t *testing.T, gql string) *ast.QueryDocument {
	t.Helper()
	doc, err := parser.ParseQuery(&ast.Source{Name: "test.gql", Input: gql})
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	return doc
}

// TestCollectFieldsBuildsRelationshipPaths checks the AST walk produces the
// same (objectName, required, varTypes, paths) tuple the old brace-walk did,
// from a query written with several selections per line (formatting the old
// parser could not handle).
func TestCollectFieldsBuildsRelationshipPaths(t *testing.T) {
	doc := mustParseDoc(t, `query DctVCenterByName($vcenter_name: String!) {
  DctVCenter(vcenter_name__value: $vcenter_name) {
    edges { node { id fqdn { value } } }
  }
}`)

	objectName, required, varTypes, paths, err := collectFields(doc)
	if err != nil {
		t.Fatalf("collectFields: %v", err)
	}
	if objectName != "DctVCenter" {
		t.Errorf("objectName = %q, want %q", objectName, "DctVCenter")
	}
	if required != "vcenter_name" {
		t.Errorf("required = %q, want %q", required, "vcenter_name")
	}
	if got := varTypes["vcenter_name"]; got != "String" {
		t.Errorf("varTypes[vcenter_name] = %q, want %q", got, "String")
	}

	want := [][]string{{"edges", "node", "id"}, {"edges", "node", "fqdn"}}
	if len(paths) != len(want) {
		t.Fatalf("got %d paths, want %d: %+v", len(paths), len(want), paths)
	}
	for i, w := range want {
		if len(paths[i].parts) != len(w) {
			t.Fatalf("path %d = %v, want %v", i, paths[i].parts, w)
		}
		for j, p := range w {
			if paths[i].parts[j] != p {
				t.Errorf("path %d part %d = %q, want %q", i, j, paths[i].parts[j], p)
			}
		}
	}
}
