package parser

import (
	"strings"
	"testing"
)

// fieldByHuman returns the parsed field with the given human-readable name.
func fieldByHuman(fields []GenqlientField, name string) (GenqlientField, bool) {
	for _, f := range fields {
		if f.HumanReadableName == name {
			return f, true
		}
	}
	return GenqlientField{}, false
}

// TestMultiLineScalarSelectionParses guards the C3 fix: a scalar attribute whose
// `{ value }` selection is split across lines must parse identically to the
// single-line form, not be mistaken for a relationship prefix.
func TestMultiLineScalarSelectionParses(t *testing.T) {
	const gql = `query DctVCenterByName($vcenter_name: String!) {
  DctVCenter(vcenter_name__value: $vcenter_name) {
    edges {
      node {
        id
        fqdn {
          value
        }
      }
    }
  }
}
`
	parsed, err := parseGraphQLQuery(gql, nil)
	if err != nil {
		t.Fatalf("parseGraphQLQuery returned error: %v", err)
	}

	fqdn, ok := fieldByHuman(parsed.GenqlientFields, "fqdn")
	if !ok {
		t.Fatalf("expected an fqdn field, got %+v", parsed.GenqlientFields)
	}
	if want := "DctVCenter.Edges[0].Node.Fqdn.Value"; fqdn.Query != want {
		t.Errorf("multi-line scalar parsed to Query %q, want %q", fqdn.Query, want)
	}
	// The multi-line block must not leak `value` in as its own field.
	if _, leaked := fieldByHuman(parsed.GenqlientFields, "fqdn_value"); leaked {
		t.Errorf("multi-line scalar leaked a `value` field: %+v", parsed.GenqlientFields)
	}
}

// TestFilterVariableExtraction locks key-variable extraction: the first
// filter/key variable in the object selector becomes Required, regardless of
// argument layout (the GraphQL grammar handles whitespace).
func TestFilterVariableExtraction(t *testing.T) {
	cases := map[string]string{
		`query Q($name: String!) {
  DctX(name__value: $name) { edges { node { id } } }
}`: "name",
		`query Q($asn: BigInt!, $b: String!) {
  DctX(asn__value: $asn, b__value: $b) { edges { node { id } } }
}`: "asn", // first filter wins
	}
	for gql, want := range cases {
		parsed, err := parseGraphQLQuery(gql, nil)
		if err != nil {
			t.Fatalf("parseGraphQLQuery(%q): %v", gql, err)
		}
		if parsed.Required != want {
			t.Errorf("Required = %q, want %q", parsed.Required, want)
		}
	}
}

// TestResourceWithoutIDReturnsError guards the C1 fix: a resource whose read
// query omits the node's own id must return a clear error rather than panic
// downstream in the template (index out of range).
func TestResourceWithoutIDReturnsError(t *testing.T) {
	const gql = `mutation DctXCreate($data: DctXCreateInput!) {
  DctXCreate(data: $data) {
    object {
      name { value }
    }
  }
}

mutation DctXUpsert($data: DctXUpsertInput!) {
  DctXUpsert(data: $data) {
    object {
      name { value }
    }
  }
}

mutation DctXDelete($id: String!) {
  DctXDelete(data: { id: $id }) {
    ok
  }
}

query DctXByName($name: String!) {
  DctX(name__value: $name) {
    edges {
      node {
        name { value }
      }
    }
  }
}
`
	_, err := parseGraphQLQuery(gql, nil)
	if err == nil {
		t.Fatal("expected an error for a resource that does not select id, got nil")
	}
	if !strings.Contains(err.Error(), "own id") {
		t.Errorf("error %q should explain the resource must select the node's own id", err)
	}
}

// TestDocumentWithoutOperationReturnsError guards the I5 fix: a document with
// neither a query nor a mutation must return an error, not a zero-value IR.
func TestDocumentWithoutOperationReturnsError(t *testing.T) {
	_, err := parseGraphQLQuery("fragment Foo on Bar {\n  x\n}\n", nil)
	if err == nil {
		t.Fatal("expected an error for a document with no query or mutation, got nil")
	}
}

// TestParseDoesNotPanicOnMalformedInput is a broad guard that the parser returns
// an error (or a value) but never panics on a range of malformed documents.
func TestParseDoesNotPanicOnMalformedInput(t *testing.T) {
	inputs := []string{
		"",
		"{",
		"}",
		"query",
		"query {",
		"query Q(",
		"query Q($x) {",                     // missing type
		"query Q { Obj( ) { edges { node {", // unbalanced, missing id
		"mutation",
		"mutation M {",
	}
	for _, in := range inputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("parseGraphQLQuery panicked on %q: %v", in, r)
				}
			}()
			_, _ = parseGraphQLQuery(in, nil) // error is fine; a panic is not
		}()
	}
}
