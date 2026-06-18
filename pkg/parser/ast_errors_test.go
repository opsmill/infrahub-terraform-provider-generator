package parser

import (
	"strings"
	"testing"
)

// TestAliasRejected guards FR-006: a field alias would make the parser's access
// path disagree with genqlient's generated field name, so it must be rejected.
func TestAliasRejected(t *testing.T) {
	const gql = `query Q($name: String!) {
  DctX(name__value: $name) { edges { node { id host: fqdn { value } } } }
}`
	_, err := parseGraphQLQuery(gql, nil)
	if err == nil {
		t.Fatal("expected an error for an aliased field, got nil")
	}
	if !strings.Contains(err.Error(), "alias") {
		t.Errorf("error %q should mention the unsupported alias", err)
	}
}

// TestInlineFragmentRejected guards FR-006: inline/type-condition fragments map
// to genqlient interface types, not flat paths, so they must be rejected.
func TestInlineFragmentRejected(t *testing.T) {
	const gql = `query Q($name: String!) {
  DctX(name__value: $name) { edges { node { id ... on DctX { fqdn { value } } } } }
}`
	_, err := parseGraphQLQuery(gql, nil)
	if err == nil {
		t.Fatal("expected an error for an inline fragment, got nil")
	}
	if !strings.Contains(err.Error(), "inline fragment") {
		t.Errorf("error %q should mention the unsupported inline fragment", err)
	}
}

// TestUndefinedFragmentRejected guards FR-006: a spread of a fragment the
// document does not define must be a clear error, not a dropped selection.
func TestUndefinedFragmentRejected(t *testing.T) {
	const gql = `query Q($name: String!) {
  DctX(name__value: $name) { edges { node { id ...Missing } } }
}`
	_, err := parseGraphQLQuery(gql, nil)
	if err == nil {
		t.Fatal("expected an error for an undefined fragment, got nil")
	}
	if !strings.Contains(err.Error(), "not defined") {
		t.Errorf("error %q should explain the fragment is not defined", err)
	}
}

// TestFieldDirectiveRejected guards FR-006: a directive on a field (e.g.
// @skip/@include) would silently drop or alter the selection, so it must be
// rejected rather than mis-generated.
func TestFieldDirectiveRejected(t *testing.T) {
	const gql = `query Q($name: String!) {
  DctX(name__value: $name) { edges { node { id fqdn @include(if: true) { value } } } }
}`
	_, err := parseGraphQLQuery(gql, nil)
	if err == nil {
		t.Fatal("expected an error for a field directive, got nil")
	}
	if !strings.Contains(err.Error(), "directive") {
		t.Errorf("error %q should mention the unsupported directive", err)
	}
}

// TestOperationDirectiveRejected guards FR-006 at the operation level.
func TestOperationDirectiveRejected(t *testing.T) {
	const gql = `query Q($name: String!) @someDirective {
  DctX(name__value: $name) { edges { node { id } } }
}`
	_, err := parseGraphQLQuery(gql, nil)
	if err == nil {
		t.Fatal("expected an error for an operation directive, got nil")
	}
	if !strings.Contains(err.Error(), "directive") {
		t.Errorf("error %q should mention the unsupported directive", err)
	}
}

// TestCyclicFragmentRejected guards against unbounded recursion: a cyclic
// fragment spread is syntactically valid (parser.ParseQuery accepts it) but
// would send the selection walk into a stack overflow, so it must be rejected
// with a clear error instead.
func TestCyclicFragmentRejected(t *testing.T) {
	const gql = `query Q($name: String!) {
  DctX(name__value: $name) { edges { node { id ...A } } }
}

fragment A on DctXNode { ...B }
fragment B on DctXNode { id ...A }`
	_, err := parseGraphQLQuery(gql, nil)
	if err == nil {
		t.Fatal("expected an error for a cyclic fragment, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error %q should mention the fragment cycle", err)
	}
}
