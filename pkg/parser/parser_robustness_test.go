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

// TestCommentWithBracesParses guards FR-003: `#` comments anywhere — including
// ones containing braces, which mis-counted in the old brace-walk — are ignored.
func TestCommentWithBracesParses(t *testing.T) {
	const gql = `query DctVCenterByName($vcenter_name: String!) {
  # the next block selects a single { value } scalar
  DctVCenter(vcenter_name__value: $vcenter_name) {
    edges { node { id fqdn { value } } }  # fqdn { value } is one attribute
  }
}`
	parsed, err := parseGraphQLQuery(gql, nil)
	if err != nil {
		t.Fatalf("parseGraphQLQuery returned error: %v", err)
	}
	if _, ok := fieldByHuman(parsed.GenqlientFields, "fqdn"); !ok {
		t.Errorf("expected an fqdn field, got %+v", parsed.GenqlientFields)
	}
}

// TestNamedFragmentMatchesInline guards FR-004/SC-002: a query using a named
// fragment spread on the same type generates byte-identical source to the
// equivalent inlined query.
func TestNamedFragmentMatchesInline(t *testing.T) {
	const withFragment = `query DctVCenterByName($vcenter_name: String!) {
  DctVCenter(vcenter_name__value: $vcenter_name) {
    edges { node { ...NodeFields } }
  }
}

fragment NodeFields on DctVCenterNode {
  id
  fqdn { value }
}`
	const inline = `query DctVCenterByName($vcenter_name: String!) {
  DctVCenter(vcenter_name__value: $vcenter_name) {
    edges { node { id fqdn { value } } }
  }
}`

	fragCode, err := generateTerraformDataSource(parseOrFatal(t, withFragment))
	if err != nil {
		t.Fatalf("generate (fragment form): %v", err)
	}
	inlineCode, err := generateTerraformDataSource(parseOrFatal(t, inline))
	if err != nil {
		t.Fatalf("generate (inline form): %v", err)
	}
	if fragCode != inlineCode {
		t.Errorf("named-fragment query generated different source than the inline equivalent:\n--- fragment ---\n%s\n--- inline ---\n%s", fragCode, inlineCode)
	}
}

// parseOrFatal parses a query into the IR, failing the test on error.
func parseOrFatal(t *testing.T, gql string) *InputGraphQLQuery {
	t.Helper()
	parsed, err := parseGraphQLQuery(gql, nil)
	if err != nil {
		t.Fatalf("parseGraphQLQuery: %v", err)
	}
	return parsed
}

// TestResourceWithoutKeyReturnsError guards that a resource whose read query has
// no key/filter variable is rejected: the resource templates dereference the key
// field unconditionally, so an empty key would otherwise generate non-compiling
// Go. (An unfiltered list is valid for a data source, but not for a resource,
// which must read its object back by key.)
func TestResourceWithoutKeyReturnsError(t *testing.T) {
	const gql = `mutation DctXCreate($data: DctXCreateInput!) {
  DctXCreate(data: $data) { object { id name { value } } }
}

mutation DctXUpsert($data: DctXUpsertInput!) {
  DctXUpsert(data: $data) { object { id name { value } } }
}

mutation DctXDelete($id: String!) {
  DctXDelete(data: { id: $id }) { ok }
}

query DctXAll {
  DctX { edges { node { id name { value } } } }
}`
	_, err := parseGraphQLQuery(gql, nil)
	if err == nil {
		t.Fatal("expected an error for a resource whose read query has no key filter, got nil")
	}
	if !strings.Contains(err.Error(), "key variable") {
		t.Errorf("error %q should explain the resource needs a key filter", err)
	}
}

// TestHumanReadableNameStripsOnlyLeadingPrefix locks the prefix-strip to the
// leading edges_node_ only: a deeply nested relationship path keeps its inner
// frames so distinct paths do not collapse to the same attribute name.
func TestHumanReadableNameStripsOnlyLeadingPrefix(t *testing.T) {
	cases := map[string]string{
		"edges_node_fqdn":                 "fqdn",
		"id":                              "id",
		"edges_node_site_edges_node_name": "site_edges_node_name",
	}
	for in, want := range cases {
		if got := humanReadableName(in); got != want {
			t.Errorf("humanReadableName(%q) = %q, want %q", in, got, want)
		}
	}
}
