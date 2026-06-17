# GraphQL-Library-Based Query Parser Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the hand-rolled, line-based brace-walk in `pkg/parser` with a real GraphQL parser (`github.com/vektah/gqlparser/v2`), so the generator accepts any syntactically valid GraphQL document following Infrahub's conventions regardless of formatting — while producing byte-identical output for queries that work today.

**Architecture:** The change is confined to the **text → tuple** front-end of `pkg/parser`. `parseGraphQLQuery` parses the document into an AST once, then a recursive `walkSelection` flattens the read query's selection set into the same `(objectName, required, varTypes, []fieldPath)` tuple the old `collectFields` produced. Everything downstream — `buildField`, `parseResourceInput`/`parseDataSourceInput` assembly, the `InputGraphQLQuery` IR, and all templates — is **frozen**. The six existing golden files in `pkg/parser/testdata/` are the regression contract: zero output drift is the pass bar.

**Tech Stack:** Go 1.25, `github.com/vektah/gqlparser/v2` (`/parser` + `/ast`, parse-only, no schema validation), existing `text/template` rendering, table + golden tests.

---

## What stays frozen (do NOT touch)

These are the contract. If a change to any of these seems necessary, stop and re-check the walk — the bug is in the new code, not here.

- `pkg/parser/model.go` — the `InputGraphQLQuery` / `GenqlientField` / `fieldPath`-fed IR.
- `pkg/parser/generators.go` — template wiring, `ReadAndGenerate*`, `titleCaser`.
- `pkg/parser/types.go` — type-class helpers.
- `pkg/templates/*.gotmpl` — all templates.
- `pkg/parser/testdata/*.go.golden` — the six golden files. **Never run `go test -update`** during this work; that would mask drift instead of catching it.
- These functions in `parser.go` stay verbatim: `buildField`, `humanReadableName`, `gqlTypeToKind`, `stampRequired`, `lcFirst`, `ucFirst`, and the `fieldPath` type + `errMissingQueryName` var.

## File Structure

- **Create** `pkg/parser/ast_parse.go` — the AST-based extraction: `readQuery`, `detectResourceType`, `queryNameAndOp`, `operationNames`, `variableTypes`, `keyVariable`, `collectFields`, `walkSelection`, `isScalarSelection`, `appendPart`. One responsibility: turn an `*ast.QueryDocument` into the tuple the IR builders consume.
- **Create** `pkg/parser/ast_parse_test.go` — white-box unit tests for the walk (same `package parser`).
- **Modify** `pkg/parser/parser.go` — swap imports; rewrite `parseGraphQLQuery`, `parseResourceInput`, `parseDataSourceInput` to thread the AST doc; **delete** the 12 line-based helpers.
- **Modify** `pkg/parser/parser_robustness_test.go` — migrate `TestFilterVariableExtraction` (it calls the deleted `parseObjectLine`).
- **Modify** `go.mod` / `go.sum` — add the dependency.
- **Modify** `README.md` — document supported / unsupported GraphQL input.

---

## Task 1: Add the `gqlparser/v2` dependency

**Files:**
- Modify: `go.mod`, `go.sum`

This is a setup task (no behavior to test yet); it ends green when the module builds.

- [ ] **Step 1: Add the dependency**

Run:
```bash
go get github.com/vektah/gqlparser/v2@latest
go mod tidy
```
Expected: `go.mod` gains a `require github.com/vektah/gqlparser/v2 vX.Y.Z` line; `go.sum` gains its checksums.

- [ ] **Step 2: Verify the module still builds**

Run: `go build ./...`
Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "build(parser): add vektah/gqlparser/v2 dependency" \
  -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Replace the line-based front-end with an AST walk

**Files:**
- Create: `pkg/parser/ast_parse.go`
- Create: `pkg/parser/ast_parse_test.go`
- Modify: `pkg/parser/parser.go` (imports + `parseGraphQLQuery` + `parseResourceInput` + `parseDataSourceInput`; delete 12 helpers)
- Modify: `pkg/parser/parser_robustness_test.go` (migrate `TestFilterVariableExtraction`)

- [ ] **Step 1: Write the failing unit test for the AST walk**

Create `pkg/parser/ast_parse_test.go`:

```go
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./pkg/parser -run TestCollectFieldsBuildsRelationshipPaths -v`
Expected: compile failure — `collectFields` currently has signature `func(lines []string) (string, string, map[string]string, []fieldPath)` (no error return) and is defined in `parser.go`. It will not match this call. That mismatch is the red.

- [ ] **Step 3: Create the AST extraction file**

Create `pkg/parser/ast_parse.go`:

```go
package parser

import (
	"fmt"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
)

// readQuery returns the document's read-query operation (the single `query`
// operation), or nil when the document has none.
func readQuery(doc *ast.QueryDocument) *ast.OperationDefinition {
	for _, op := range doc.Operations {
		if op.Operation == ast.Query {
			return op
		}
	}
	return nil
}

// detectResourceType classifies a parsed document by its first operation: a
// mutation makes it a resource, a query makes it a data source.
func detectResourceType(doc *ast.QueryDocument) (ResourceType, bool) {
	for _, op := range doc.Operations {
		switch op.Operation {
		case ast.Mutation:
			return Resource, true
		case ast.Query:
			return DataSource, true
		}
	}
	return 0, false
}

// queryNameAndOp returns the lowercased component name derived from the read
// query's operation name and the operation name itself (used to call the
// matching genqlient function).
func queryNameAndOp(doc *ast.QueryDocument) (queryName, readOp string) {
	op := readQuery(doc)
	if op == nil || op.Name == "" {
		return "", ""
	}
	return lcFirst(op.Name), op.Name
}

// operationNames extracts the create/upsert/delete mutation operation names by
// the Infrahub <Kind><Op> suffix convention, taken verbatim from the document.
func operationNames(doc *ast.QueryDocument) (createOp, upsertOp, deleteOp string) {
	for _, op := range doc.Operations {
		if op.Operation != ast.Mutation {
			continue
		}
		switch {
		case strings.HasSuffix(op.Name, "Create"):
			createOp = op.Name
		case strings.HasSuffix(op.Name, "Upsert"):
			upsertOp = op.Name
		case strings.HasSuffix(op.Name, "Delete"):
			deleteOp = op.Name
		}
	}
	return createOp, upsertOp, deleteOp
}

// variableTypes maps each variable declared by the operation to its leading
// named GraphQL type (e.g. `$vcenter_name: String!` -> "String"), which is what
// determines the generated SDK function's parameter type. List and other
// non-named types have an empty NamedType and are skipped.
func variableTypes(op *ast.OperationDefinition) map[string]string {
	if op == nil || len(op.VariableDefinitions) == 0 {
		return nil
	}
	m := make(map[string]string, len(op.VariableDefinitions))
	for _, v := range op.VariableDefinitions {
		if v.Type != nil && v.Type.NamedType != "" {
			m[v.Variable] = v.Type.NamedType
		}
	}
	return m
}

// keyVariable returns the name of the first filter/key variable referenced in
// the object selector's arguments (e.g. `DctVCenter(vcenter_name__value:
// $vcenter_name)` -> "vcenter_name"), or "" for an unfiltered list query.
func keyVariable(f *ast.Field) string {
	for _, arg := range f.Arguments {
		if arg.Value != nil && arg.Value.Kind == ast.Variable {
			return arg.Value.Raw
		}
	}
	return ""
}

// collectFields walks the read query's selection set and returns the queried
// object kind, the filter/key variable (empty for an unfiltered list query),
// the GraphQL types of the query's declared variables, and every selected leaf
// field with its full relationship path. Named fragment spreads are inlined;
// inline fragments are rejected. The first top-level field names the queried
// object and is not part of any attribute's path.
func collectFields(doc *ast.QueryDocument) (objectName, required string, varTypes map[string]string, fields []fieldPath, err error) {
	op := readQuery(doc)
	if op == nil || len(op.SelectionSet) == 0 {
		return "", "", nil, nil, nil
	}
	varTypes = variableTypes(op)

	objField, ok := op.SelectionSet[0].(*ast.Field)
	if !ok {
		return "", "", varTypes, nil, fmt.Errorf("parsing GraphQL query %q: expected an object selection, got %T", op.Name, op.SelectionSet[0])
	}
	objectName = objField.Name
	required = keyVariable(objField)

	fields, err = walkSelection(objField.SelectionSet, nil, doc)
	if err != nil {
		return "", "", varTypes, nil, err
	}
	return objectName, required, varTypes, fields, nil
}

// walkSelection recursively flattens a selection set into leaf field paths,
// carrying the relationship-prefix stack (e.g. ["edges", "node"]). A field with
// no sub-selection, or whose sub-selection is purely scalar meta fields
// (`{ value }`), is a leaf attribute; any other field is a relationship frame
// pushed onto the stack. Named fragment spreads are inlined at the current
// depth; inline fragments are unsupported.
func walkSelection(set ast.SelectionSet, stack []string, doc *ast.QueryDocument) ([]fieldPath, error) {
	var out []fieldPath
	for _, sel := range set {
		switch s := sel.(type) {
		case *ast.Field:
			if len(s.SelectionSet) == 0 || isScalarSelection(s.SelectionSet, doc) {
				out = append(out, fieldPath{parts: appendPart(stack, s.Name)})
				continue
			}
			sub, err := walkSelection(s.SelectionSet, appendPart(stack, s.Name), doc)
			if err != nil {
				return nil, err
			}
			out = append(out, sub...)
		case *ast.FragmentSpread:
			frag := doc.Fragments.ForName(s.Name)
			if frag == nil {
				return nil, fmt.Errorf("parsing GraphQL query: fragment %q is referenced but not defined", s.Name)
			}
			sub, err := walkSelection(frag.SelectionSet, stack, doc)
			if err != nil {
				return nil, err
			}
			out = append(out, sub...)
		case *ast.InlineFragment:
			return nil, fmt.Errorf("parsing GraphQL query: inline fragment (... on %s) is unsupported; expand its fields inline", s.TypeCondition)
		}
	}
	return out, nil
}

// isScalarSelection reports whether every member of set (resolving named
// fragment spreads) is a leaf field with no sub-selection — i.e. an attribute's
// scalar `{ value }` meta-selection rather than a relationship block.
func isScalarSelection(set ast.SelectionSet, doc *ast.QueryDocument) bool {
	for _, sel := range set {
		switch s := sel.(type) {
		case *ast.Field:
			if len(s.SelectionSet) != 0 {
				return false
			}
		case *ast.FragmentSpread:
			frag := doc.Fragments.ForName(s.Name)
			if frag == nil || !isScalarSelection(frag.SelectionSet, doc) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// appendPart returns a new slice with name appended, never aliasing stack's
// backing array, so sibling recursions cannot corrupt each other's prefix.
func appendPart(stack []string, name string) []string {
	parts := make([]string, len(stack)+1)
	copy(parts, stack)
	parts[len(stack)] = name
	return parts
}
```

- [ ] **Step 4: Rewire `parser.go` and delete the line-based helpers**

In `pkg/parser/parser.go`:

(a) Replace the import block (lines 3-10) with:

```go
import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/opsmill/infrahub-terraform-provider-generator/pkg/schema"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
)
```

(b) Replace `parseGraphQLQuery` (lines 16-41) with:

```go
// parseGraphQLQuery parses and classifies a GraphQL document into the
// intermediate representation the templates render. A leading mutation makes it
// a resource; a leading query makes it a data source.
func parseGraphQLQuery(query string, reg *schema.Registry) (*InputGraphQLQuery, error) {
	doc, err := parser.ParseQuery(&ast.Source{Name: "query.gql", Input: query})
	if err != nil {
		return nil, fmt.Errorf("parsing GraphQL query: %w", err)
	}

	resourceType, ok := detectResourceType(doc)
	if !ok {
		return nil, errors.New("parsing GraphQL query: document contains neither a query nor a mutation")
	}

	var result InputGraphQLQuery
	switch resourceType {
	case Resource:
		result, err = parseResourceInput(doc, reg)
	case DataSource:
		result, err = parseDataSourceInput(doc, reg)
	}
	if err != nil {
		return nil, err
	}

	result.ResourceType = resourceType
	return &result, nil
}
```

(c) In `parseResourceInput`, change the signature and the first two calls. Replace lines 61-66:

```go
func parseResourceInput(lines []string, reg *schema.Registry) (InputGraphQLQuery, error) {
	objectName, required, varTypes, paths := collectFields(lines)
	queryName, readOp := queryNameAndOp(lines)
	if queryName == "" {
		return InputGraphQLQuery{}, errMissingQueryName
	}
```

with:

```go
func parseResourceInput(doc *ast.QueryDocument, reg *schema.Registry) (InputGraphQLQuery, error) {
	objectName, required, varTypes, paths, err := collectFields(doc)
	if err != nil {
		return InputGraphQLQuery{}, err
	}
	queryName, readOp := queryNameAndOp(doc)
	if queryName == "" {
		return InputGraphQLQuery{}, errMissingQueryName
	}
```

Then change the `operationNames(lines)` call (line 100) to `operationNames(doc)`. Everything else in `parseResourceInput` is unchanged.

(d) In `parseDataSourceInput`, change the signature and the first two calls. Replace lines 134-139:

```go
func parseDataSourceInput(lines []string, reg *schema.Registry) (InputGraphQLQuery, error) {
	objectName, required, varTypes, paths := collectFields(lines)
	queryName, readOp := queryNameAndOp(lines)
	if queryName == "" {
		return InputGraphQLQuery{}, errMissingQueryName
	}
```

with:

```go
func parseDataSourceInput(doc *ast.QueryDocument, reg *schema.Registry) (InputGraphQLQuery, error) {
	objectName, required, varTypes, paths, err := collectFields(doc)
	if err != nil {
		return InputGraphQLQuery{}, err
	}
	queryName, readOp := queryNameAndOp(doc)
	if queryName == "" {
		return InputGraphQLQuery{}, errMissingQueryName
	}
```

Everything else in `parseDataSourceInput` is unchanged.

(e) **Delete** these now-superseded functions from `parser.go` (their replacements live in `ast_parse.go`, or they are no longer needed):

- `detectResourceType` (old line-based version, ~lines 43-54)
- `collectFields` (old line-based version, ~lines 174-223)
- `normalizeScalarBlocks` (~lines 280-306)
- `scalarBlockEnd` (~lines 308-324)
- `indexOfQueryLine` (~lines 326-334)
- `queryNameAndOp` (old line-based version, ~lines 336-349)
- `operationNames` (old line-based version, ~lines 351-368)
- `operationName` (~lines 370-383)
- `parseObjectLine` (~lines 385-395)
- `parseOperationVars` (~lines 397-420)
- `variableName` (~lines 422-429)
- `scanIdentifier` (~lines 431-441)

Keep `buildField`, `gqlTypeToKind`, `humanReadableName`, `stampRequired`, `lcFirst`, `ucFirst`, the `fieldPath` type, and `errMissingQueryName`.

- [ ] **Step 5: Migrate `TestFilterVariableExtraction` (it calls the deleted `parseObjectLine`)**

In `pkg/parser/parser_robustness_test.go`, replace the whole `TestFilterVariableExtraction` function (lines 53-71) with a version that asserts the extracted key at the `parseGraphQLQuery` level — the formatting variants the old test guarded (no space before `{`, variable ending the line) are now handled by the grammar, so the meaningful behavior to lock is key extraction and first-filter-wins:

```go
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
```

If `strings` is now unused in `parser_robustness_test.go` after this edit, remove it from that file's imports. (`TestResourceWithoutIDReturnsError` and `TestDocumentWithoutOperationReturnsError` still use `strings.Contains`, so it most likely stays — let `go test` tell you.)

- [ ] **Step 6: Run the new unit test — verify it passes**

Run: `go test ./pkg/parser -run TestCollectFieldsBuildsRelationshipPaths -v`
Expected: PASS.

- [ ] **Step 7: Run the FULL package test suite — golden files must not drift**

Run: `go test ./pkg/parser -v`
Expected: PASS, including every `TestGolden*`, `TestMultiLineScalarSelectionParses`, `TestFilterVariableExtraction`, `TestResourceWithoutIDReturnsError`, `TestDocumentWithoutOperationReturnsError`, and `TestParseDoesNotPanicOnMalformedInput`. **Do not pass `-update`.** A golden diff here means the AST walk produced different paths than the brace-walk — fix the walk, do not rewrite the golden.

- [ ] **Step 8: Vet, format, build**

Run:
```bash
go vet ./...
gofmt -l pkg/parser
go build ./...
```
Expected: no output from any command (gofmt printing a filename means it needs formatting — run `gofmt -w pkg/parser`).

- [ ] **Step 9: Commit**

```bash
git add pkg/parser/ast_parse.go pkg/parser/ast_parse_test.go pkg/parser/parser.go pkg/parser/parser_robustness_test.go
git commit -m "refactor(parser): parse .gql via gqlparser AST, drop brace-walk" \
  -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Prove comment and named-fragment robustness (additive)

These inputs should already work with the AST front-end (the lexer skips `#` comments; `walkSelection` inlines named fragment spreads). This task locks that behavior with tests; if a test fails, the fix is in `walkSelection`/`isScalarSelection`, not in the templates.

**Files:**
- Modify: `pkg/parser/parser_robustness_test.go` (add two tests)

- [ ] **Step 1: Write the failing tests**

Append to `pkg/parser/parser_robustness_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests**

Run: `go test ./pkg/parser -run 'TestCommentWithBracesParses|TestNamedFragmentMatchesInline' -v`
Expected: PASS. If `TestNamedFragmentMatchesInline` fails, inspect the diff — `walkSelection` should inline the fragment's `id` and `fqdn` at the `edges.node` depth, yielding identical paths to the inline form.

- [ ] **Step 3: Full suite + vet/format**

Run:
```bash
go test ./pkg/parser
go vet ./... && gofmt -l pkg/parser
```
Expected: PASS; no gofmt output.

- [ ] **Step 4: Commit**

```bash
git add pkg/parser/parser_robustness_test.go
git commit -m "test(parser): lock comment tolerance and named-fragment equivalence" \
  -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Reject unsupported constructs loudly

Per FR-006 / P2, unsupported constructs must fail with a clear, file-naming error rather than silently mis-generate. `walkSelection` already errors on inline fragments and undefined fragments (Task 2). This task adds the **alias** guard (genuine red-green) and pins all three with negative tests.

**Files:**
- Modify: `pkg/parser/ast_parse.go` (add the alias guard)
- Create: `pkg/parser/ast_errors_test.go`

- [ ] **Step 1: Write the failing tests**

Create `pkg/parser/ast_errors_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify the alias case fails**

Run: `go test ./pkg/parser -run 'TestAliasRejected|TestInlineFragmentRejected|TestUndefinedFragmentRejected' -v`
Expected: `TestInlineFragmentRejected` and `TestUndefinedFragmentRejected` PASS (their guards exist from Task 2); `TestAliasRejected` FAILS — with no alias guard, the aliased field `host: fqdn` is treated as a leaf named `fqdn` and parses without error.

- [ ] **Step 3: Add the alias guard to `walkSelection`**

In `pkg/parser/ast_parse.go`, in `walkSelection`, add the alias check as the first statement of the `case *ast.Field:` branch (gqlparser sets `Alias == Name` when no alias is written, so an alias is `Alias != "" && Alias != Name`):

```go
		case *ast.Field:
			if s.Alias != "" && s.Alias != s.Name {
				return nil, fmt.Errorf("parsing GraphQL query: alias %q on field %q is unsupported; remove the alias", s.Alias, s.Name)
			}
			if len(s.SelectionSet) == 0 || isScalarSelection(s.SelectionSet, doc) {
				out = append(out, fieldPath{parts: appendPart(stack, s.Name)})
				continue
			}
			sub, err := walkSelection(s.SelectionSet, appendPart(stack, s.Name), doc)
			if err != nil {
				return nil, err
			}
			out = append(out, sub...)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./pkg/parser -run 'TestAliasRejected|TestInlineFragmentRejected|TestUndefinedFragmentRejected' -v`
Expected: all three PASS.

- [ ] **Step 5: Full suite + vet/format**

Run:
```bash
go test ./pkg/parser
go vet ./... && gofmt -l pkg/parser
```
Expected: PASS; no gofmt output. (Confirm `TestParseDoesNotPanicOnMalformedInput` still passes — all those malformed inputs now produce gqlparser parse errors, which is fine; the test only forbids panics.)

- [ ] **Step 6: Commit**

```bash
git add pkg/parser/ast_parse.go pkg/parser/ast_errors_test.go
git commit -m "feat(parser): reject aliases, inline fragments, undefined fragments" \
  -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Document supported / unsupported GraphQL input

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Add a "GraphQL input support" section to the README**

Open `README.md` and add the following section just after the Quick Start table (adjust the surrounding heading level to match the file). Do not invent a different matrix — this is the one the parser enforces:

```markdown
## GraphQL input support

The generator parses each `.gql` file with a GraphQL grammar, so formatting is
free: fields may share a line, selections may be inlined, indentation is
irrelevant, and `#` comments are ignored anywhere (including comments that
contain braces).

**Supported**

- Any syntactically valid GraphQL document following Infrahub's conventions
  (`edges`/`node` nesting, `{ value }` scalar selections, the
  `<Kind>Create`/`Upsert`/`Delete` mutation-name convention).
- Named fragment spreads on the queried type, e.g. `...NodeFields` with a
  matching `fragment NodeFields on <Kind> { ... }`. These are flattened into the
  selection and generate the same source as writing the fields inline.

**Unsupported (rejected with a clear, file-naming error — never silently
mis-generated)**

- Field aliases (`alias: field`) — the alias would rename the genqlient Go
  field and break the generated access path.
- Inline / type-condition fragments (`... on Kind`) — these map to genqlient
  interface types and type assertions, not flat paths.
- Custom or built-in directives (`@include`, `@skip`, …).

> **genqlient compatibility (maintainers):** named-fragment support assumes the
> SDK's genqlient (in `infrahub-terraform-provider-template`) inlines the same
> fragment spreads to the same Go field names this generator does. This holds
> for same-type spreads. When adding a query that uses a fragment, verify once
> that the generated provider compiles against the genqlient-built SDK.
```

- [ ] **Step 2: Verify the doc builds / lints (if the docs site is wired)**

Run: `git diff --stat README.md`
Expected: README.md shows the added lines. (`gqlparser` is already in `.vale/styles/spelling-exceptions.txt`, so Vale will not flag it.)

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: document supported and unsupported GraphQL query input" \
  -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Final Verification

- [ ] **Step 1: Full build, test, vet, lint**

Run:
```bash
make build
make test
make vet
make lint
```
Expected: `make build` produces `bin/`; `make test` passes (all golden files unchanged); `make vet` clean; `make lint` clean (golangci-lint v2, errcheck disabled per `.golangci.yaml`).

- [ ] **Step 2: Confirm zero golden drift explicitly**

Run: `git status --porcelain pkg/parser/testdata`
Expected: **no output** — the six golden files are untouched. If any golden changed, the refactor altered generated output for an existing input; investigate the walk before proceeding.

---

## Self-Review (run after writing, before handoff)

**1. Spec coverage**

| Brief item | Task |
| --- | --- |
| FR-001 byte-identical output (golden frozen) | Task 2 Step 7 + Final Verification Step 2 |
| FR-002 parse via AST, not string-walk | Task 2 (delete brace-walk, add gqlparser) |
| FR-003 ignore comments (incl. braces) | Task 3 (`TestCommentWithBracesParses`) |
| FR-004 flatten named fragment spreads | Task 2 (`walkSelection` FragmentSpread) + Task 3 equivalence test |
| FR-005 paths match genqlient field names | Task 5 documented manual check (open question resolution) |
| FR-006 loud errors, no partial output | Task 4 (alias/inline/undefined guards + tests) |
| P1 varied-format generates correctly | Task 2 unit test + Task 3 |
| P2 unsupported fails loudly | Task 4 |
| SC-001 100% existing goldens pass | Task 2 Step 7; Final Step 2 |
| SC-002 equivalence corpus matches canonical | Task 3 (`TestNamedFragmentMatchesInline`) |
| SC-003 unsupported → non-zero/clear error | Task 4 |
| New-dependency governance gate | Task 1 |

**2. Placeholder scan:** every code step contains complete code; no TBD/TODO/"add error handling". ✅

**3. Type/signature consistency:** `collectFields(doc *ast.QueryDocument) (string, string, map[string]string, []fieldPath, error)` is defined in Task 2 and called by `parseResourceInput`/`parseDataSourceInput` with the matching 5-value signature; `walkSelection(ast.SelectionSet, []string, *ast.QueryDocument) ([]fieldPath, error)`, `isScalarSelection(ast.SelectionSet, *ast.QueryDocument) bool`, `appendPart([]string, string) []string`, `detectResourceType(doc)`, `queryNameAndOp(doc)`, `operationNames(doc)` are all defined once in `ast_parse.go` and referenced consistently. `parser.ParseQuery(&ast.Source{...})` and `ast.Query`/`ast.Mutation`/`ast.Variable` match the gqlparser/v2 API. ✅
