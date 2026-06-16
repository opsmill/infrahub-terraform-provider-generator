# Schema-Driven Attribute Typing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Type generated Terraform resource attributes from the live Infrahub schema — `Number → types.Int64`, `Boolean`/`Checkbox → types.Bool`, everything else `types.String` — and drive Terraform `Required` vs `Optional` from each attribute's schema `optional` flag, with the matching Infrahub SDK input wrappers.

**Architecture:** A new `pkg/schema` package fetches `/api/schema` once and exposes a `Registry` lookup `(nodeKind, attrName) → {Kind, Optional}`. `main.go` builds the registry from connection flags (nil when none given) and threads it into the parser. The parser stamps each `GenqlientField` with `Kind`/`Optional`; a single mapping table in `pkg/parser/types.go` plus `text/template` FuncMap helpers render type-correct schema attributes, struct fields, SDK input wrappers, and value accessors. A `nil` registry reproduces today's all-`String` behavior exactly.

**Tech Stack:** Go 1.23 (stdlib `net/http`, `encoding/json`, `text/template`, `httptest`), `terraform-plugin-framework/types`, genqlient-generated `infrahub-sdk-go`.

**Spec:** `docs/superpowers/specs/2026-06-16-schema-driven-typing-design.md`

**Out of band (separate repo, must land together):** `infrahub-sdk-go`'s `pkg/genqlient.yaml` must add `bindings: {BigInt: {type: int64}}` and be regenerated, so `NumberAttribute{Create,Update}.Value` is `int64`. This plan targets that post-binding SDK.

---

## File Structure

- **Create `pkg/schema/registry.go`** — `Attribute`, `Registry` types and the `Attribute(nodeKind, attrName)` lookup (pure, no I/O).
- **Create `pkg/schema/registry_test.go`** — registry + nil-registry behavior.
- **Create `pkg/schema/fetch.go`** — `Fetch(ctx, address, token, branch)`: HTTP GET, JSON decode of `{nodes:[{kind,attributes:[{name,kind,optional}]}]}`, build `Registry`.
- **Create `pkg/schema/fetch_test.go`** — `httptest` server: success, non-200, bad JSON.
- **Create `pkg/parser/types.go`** — kind normalization + template FuncMap helpers (`tfType`, `tfAttr`, `sdkCreate`, `sdkUpdate`, `writeAccessor`, `readCtor`).
- **Create `pkg/parser/types_test.go`** — helper unit tests.
- **Modify `pkg/parser/model.go`** — add `Kind string`, `Optional bool` to `GenqlientField`.
- **Modify `pkg/parser/parser.go`** — `parseGraphQLQuery(query, reg)`, sub-parsers stamp `Kind`/`Optional`.
- **Modify `pkg/parser/generators.go`** — thread `reg`; register FuncMap helpers.
- **Modify `pkg/templates/resource_template.go`** — typed struct fields, schema attrs, Required/Optional branch, typed create/update/read.
- **Modify `main.go`** — connection flags, fetch registry once, pass through.
- **Modify existing tests** that call `parseGraphQLQuery` / `ReadAndGenerateDataSourcesAndResources` to pass `nil`.

Note: the read-only-vs-configurable split (`valueSuffix == ""`) and the empty-value guards already shipped on `main` are preserved; this plan generalizes them to typed values.

---

## Task 1: Schema Registry (pure lookup)

**Files:**
- Create: `pkg/schema/registry.go`
- Test: `pkg/schema/registry_test.go`

- [ ] **Step 1: Write the failing test**

```go
package schema

import "testing"

func TestRegistryAttributeLookup(t *testing.T) {
	r := &Registry{nodes: map[string]map[string]Attribute{
		"DctVCenter": {
			"total_vcpu":   {Kind: "Number", Optional: true},
			"vcenter_name": {Kind: "Text", Optional: false},
		},
	}}

	got, ok := r.Attribute("DctVCenter", "total_vcpu")
	if !ok {
		t.Fatal("expected total_vcpu to be found")
	}
	if got.Kind != "Number" || !got.Optional {
		t.Errorf("got %+v, want {Number true}", got)
	}

	if _, ok := r.Attribute("DctVCenter", "missing"); ok {
		t.Error("expected missing attribute to report not-found")
	}
	if _, ok := r.Attribute("NoSuchNode", "x"); ok {
		t.Error("expected missing node to report not-found")
	}
}

func TestNilRegistryReportsNotFound(t *testing.T) {
	var r *Registry // nil
	if _, ok := r.Attribute("DctVCenter", "total_vcpu"); ok {
		t.Error("nil registry must report not-found, not panic")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/schema/ -run TestRegistry -v`
Expected: FAIL — `undefined: Registry` / `undefined: Attribute`.

- [ ] **Step 3: Write minimal implementation**

```go
// Package schema fetches an Infrahub schema and answers attribute-type
// lookups used to generate type-correct Terraform provider code.
package schema

// Attribute is the subset of an Infrahub attribute schema the generator needs.
type Attribute struct {
	Kind     string // Infrahub AttributeKind, e.g. "Text", "Number", "Boolean".
	Optional bool   // true when the attribute may be omitted on create.
}

// Registry maps a node kind (namespace+name, e.g. "DctVCenter") and attribute
// name to its schema type information.
type Registry struct {
	nodes map[string]map[string]Attribute
}

// Attribute returns the schema info for an attribute, or ok=false when the node
// or attribute is unknown. A nil *Registry always returns ok=false, so callers
// can stay branch-free when no schema was fetched.
func (r *Registry) Attribute(nodeKind, attrName string) (Attribute, bool) {
	if r == nil {
		return Attribute{}, false
	}
	attrs, ok := r.nodes[nodeKind]
	if !ok {
		return Attribute{}, false
	}
	a, ok := attrs[attrName]
	return a, ok
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/schema/ -run TestRegistry -v && go test ./pkg/schema/ -run TestNilRegistry -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/schema/registry.go pkg/schema/registry_test.go
git commit -m "feat(schema): registry attribute lookup with nil-safe receiver"
```

---

## Task 2: Schema Fetch (`/api/schema`)

**Files:**
- Create: `pkg/schema/fetch.go`
- Test: `pkg/schema/fetch_test.go`

- [ ] **Step 1: Write the failing test**

```go
package schema

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const sampleSchemaJSON = `{
  "nodes": [
    {
      "kind": "DctVCenter",
      "attributes": [
        {"name": "vcenter_name", "kind": "Text", "optional": false},
        {"name": "total_vcpu", "kind": "Number", "optional": true},
        {"name": "is_active", "kind": "Boolean", "optional": true}
      ]
    }
  ],
  "generics": []
}`

func TestFetchBuildsRegistry(t *testing.T) {
	var gotPath, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotPath = req.URL.Path + "?" + req.URL.RawQuery
		gotKey = req.Header.Get("X-INFRAHUB-KEY")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleSchemaJSON))
	}))
	defer srv.Close()

	reg, err := Fetch(context.Background(), srv.URL, "tok-123", "main")
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if gotPath != "/api/schema?branch=main" {
		t.Errorf("requested %q, want /api/schema?branch=main", gotPath)
	}
	if gotKey != "tok-123" {
		t.Errorf("X-INFRAHUB-KEY = %q, want tok-123", gotKey)
	}

	a, ok := reg.Attribute("DctVCenter", "total_vcpu")
	if !ok || a.Kind != "Number" || !a.Optional {
		t.Errorf("total_vcpu = %+v ok=%v, want {Number true} true", a, ok)
	}
	if a, ok := reg.Attribute("DctVCenter", "vcenter_name"); !ok || a.Optional {
		t.Errorf("vcenter_name optional = %v ok=%v, want false true", a.Optional, ok)
	}
}

func TestFetchErrorsOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	if _, err := Fetch(context.Background(), srv.URL, "tok", "main"); err == nil {
		t.Fatal("expected an error on HTTP 401, got nil")
	}
}

func TestFetchErrorsOnBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	if _, err := Fetch(context.Background(), srv.URL, "tok", "main"); err == nil {
		t.Fatal("expected an error on invalid JSON, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/schema/ -run TestFetch -v`
Expected: FAIL — `undefined: Fetch`.

- [ ] **Step 3: Write minimal implementation**

```go
package schema

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// apiSchema mirrors the subset of the /api/schema response we consume. The
// endpoint inlines inherited (generic) attributes into each node's attributes
// list, so generics need not be merged here.
type apiSchema struct {
	Nodes []struct {
		Kind       string `json:"kind"`
		Attributes []struct {
			Name     string `json:"name"`
			Kind     string `json:"kind"`
			Optional bool   `json:"optional"`
		} `json:"attributes"`
	} `json:"nodes"`
}

// Fetch reads the schema for branch from an Infrahub instance at address,
// authenticating with token via the X-INFRAHUB-KEY header, and builds a
// Registry. It returns an error on any transport, status, or decode failure so
// callers never silently generate mistyped code.
func Fetch(ctx context.Context, address, token, branch string) (*Registry, error) {
	endpoint := strings.TrimRight(address, "/") + "/api/schema?" + url.Values{"branch": {branch}}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building schema request: %w", err)
	}
	req.Header.Set("X-INFRAHUB-KEY", token)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching schema from %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading schema response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("schema endpoint %s returned HTTP %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed apiSchema
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decoding schema response: %w", err)
	}

	reg := &Registry{nodes: make(map[string]map[string]Attribute, len(parsed.Nodes))}
	for _, n := range parsed.Nodes {
		attrs := make(map[string]Attribute, len(n.Attributes))
		for _, a := range n.Attributes {
			attrs[a.Name] = Attribute{Kind: a.Kind, Optional: a.Optional}
		}
		reg.nodes[n.Kind] = attrs
	}
	return reg, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/schema/ -v`
Expected: PASS (all Task 1 + Task 2 tests).

- [ ] **Step 5: Commit**

```bash
git add pkg/schema/fetch.go pkg/schema/fetch_test.go
git commit -m "feat(schema): fetch /api/schema and build the registry"
```

---

## Task 3: Type mapping table + template helpers

**Files:**
- Create: `pkg/parser/types.go`
- Test: `pkg/parser/types_test.go`
- Modify: `pkg/parser/model.go` (add `Kind`, `Optional` to `GenqlientField`)

- [ ] **Step 1: Add the fields to `GenqlientField` in `pkg/parser/model.go`**

Change the struct (currently ends at `PlainObject string`):

```go
// GenqlientField augments a Field with the access paths used to read and write
// the value through the genqlient-generated SDK.
type GenqlientField struct {
	Field
	Query                  string
	QueryNoPrefixReplaceId string
	InputObjectNames       string
	PlainObject            string
	Kind                   string // Infrahub AttributeKind; "" means untyped (String).
	Optional               bool   // true unless the schema marks the attribute required.
}
```

- [ ] **Step 2: Write the failing test**

```go
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
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./pkg/parser/ -run TestTypeHelpers -v`
Expected: FAIL — `undefined: tfType` (and the rest).

- [ ] **Step 4: Write minimal implementation in `pkg/parser/types.go`**

```go
package parser

// typeClass is the normalized Terraform/SDK type family an Infrahub attribute
// maps to. Only Number and Boolean/Checkbox diverge from string.
type typeClass int

const (
	classString typeClass = iota
	classInt64
	classBool
)

// classOf maps an Infrahub AttributeKind to its type class. Unknown and
// not-yet-supported kinds (JSON, List, NumberPool, "") fall back to string so
// generation never breaks; callers warn separately for the lossy ones.
func classOf(f GenqlientField) typeClass {
	switch f.Kind {
	case "Number":
		return classInt64
	case "Boolean", "Checkbox":
		return classBool
	default:
		return classString
	}
}

// tfType is the terraform-plugin-framework value type for the resource struct
// field (e.g. "types.Int64").
func tfType(f GenqlientField) string {
	switch classOf(f) {
	case classInt64:
		return "types.Int64"
	case classBool:
		return "types.Bool"
	default:
		return "types.String"
	}
}

// tfAttr is the schema attribute type used in the Schema() block.
func tfAttr(f GenqlientField) string {
	switch classOf(f) {
	case classInt64:
		return "schema.Int64Attribute"
	case classBool:
		return "schema.BoolAttribute"
	default:
		return "schema.StringAttribute"
	}
}

// sdkCreate is the Infrahub SDK input wrapper used on create.
func sdkCreate(f GenqlientField) string {
	switch classOf(f) {
	case classInt64:
		return "infrahub_sdk.NumberAttributeCreate"
	case classBool:
		return "infrahub_sdk.CheckboxAttributeCreate"
	default:
		return "infrahub_sdk.TextAttributeCreate"
	}
}

// sdkUpdate is the Infrahub SDK input wrapper used on update/upsert.
func sdkUpdate(f GenqlientField) string {
	switch classOf(f) {
	case classInt64:
		return "infrahub_sdk.NumberAttributeUpdate"
	case classBool:
		return "infrahub_sdk.CheckboxAttributeUpdate"
	default:
		return "infrahub_sdk.TextAttributeUpdate"
	}
}

// writeAccessor is the typed getter on a types.* value, including the call
// parens, used to read the value out of the plan/state.
func writeAccessor(f GenqlientField) string {
	switch classOf(f) {
	case classInt64:
		return "ValueInt64()"
	case classBool:
		return "ValueBool()"
	default:
		return "ValueString()"
	}
}

// readCtor is the types.* constructor used to wrap an SDK response value back
// into a framework value. With BigInt bound to int64 in the SDK, the Number
// response value is already int64, so no conversion is needed.
func readCtor(f GenqlientField) string {
	switch classOf(f) {
	case classInt64:
		return "types.Int64Value"
	case classBool:
		return "types.BoolValue"
	default:
		return "types.StringValue"
	}
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./pkg/parser/ -run TestTypeHelpers -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/parser/types.go pkg/parser/types_test.go pkg/parser/model.go
git commit -m "feat(parser): kind->type mapping table and template helpers"
```

---

## Task 4: Stamp Kind/Optional during parsing

**Files:**
- Modify: `pkg/parser/parser.go` (`parseGraphQLQuery`, `parseResourceInput`, `parseDataSourceInput` signatures + stamping)
- Modify: `pkg/parser/generators.go` (`ReadAndGenerateDataSourcesAndResources` signature)
- Modify: `pkg/parser/parser_test.go`, `pkg/parser/parser_resource_test.go` (pass `nil` to `parseGraphQLQuery`/`ReadAndGenerateDataSourcesAndResources`)
- Test: `pkg/parser/parser_typing_test.go` (new)

This task changes signatures, so it updates existing call sites in the same commit to keep the build green.

- [ ] **Step 1: Write the failing test in `pkg/parser/parser_typing_test.go`**

```go
package parser

import (
	"testing"

	"github.com/opsmill/infrahub-terraform-provider-generator/pkg/schema"
)

func TestParseStampsKindAndOptional(t *testing.T) {
	reg := schema.NewRegistry(map[string]map[string]schema.Attribute{
		"DctVCenter": {
			"total_vcpu": {Kind: "Number", Optional: true},
			"fqdn":       {Kind: "Text", Optional: false},
		},
	})

	parsed, err := parseGraphQLQuery(sampleResourceGQL, reg)
	if err != nil {
		t.Fatalf("parseGraphQLQuery returned error: %v", err)
	}

	byName := map[string]GenqlientField{}
	for _, f := range parsed.genqlientFieldsModify {
		byName[f.HumanReadableName] = f
	}

	if f, ok := byName["fqdn"]; !ok {
		t.Fatal("expected fqdn among configurable fields")
	} else if f.Kind != "Text" || f.Optional {
		t.Errorf("fqdn stamped %+v, want Kind=Text Optional=false", f)
	}
}

func TestParseNilRegistryDefaultsToOptionalString(t *testing.T) {
	parsed, err := parseGraphQLQuery(sampleResourceGQL, nil)
	if err != nil {
		t.Fatalf("parseGraphQLQuery returned error: %v", err)
	}
	for _, f := range parsed.genqlientFieldsModify {
		if f.Kind != "" || !f.Optional {
			t.Errorf("nil-registry field %q stamped %+v, want Kind=\"\" Optional=true", f.HumanReadableName, f)
		}
	}
}
```

- [ ] **Step 2: Add the `NewRegistry` constructor to `pkg/schema/registry.go`**

(The test injects a registry without going through HTTP.)

```go
// NewRegistry builds a Registry directly from a node→attr→Attribute map. It is
// used by tests and any caller that already has schema data in hand.
func NewRegistry(nodes map[string]map[string]Attribute) *Registry {
	return &Registry{nodes: nodes}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./pkg/parser/ -run TestParse -v`
Expected: FAIL — `parseGraphQLQuery` takes 1 arg, not 2 / `schema.NewRegistry` undefined until Step 2 built. After Step 2, FAIL is the arity mismatch on `parseGraphQLQuery`.

- [ ] **Step 4: Thread the registry and stamp fields in `pkg/parser/parser.go`**

Change `parseGraphQLQuery` signature and pass-through:

```go
func parseGraphQLQuery(query string, reg *schema.Registry) (*InputGraphQLQuery, error) {
	var resourceType ResourceType
	var result InputGraphQLQuery
	var err error
	lines := strings.Split(query, "\n")

	for _, line := range lines {
		if strings.Contains(line, "mutation") {
			resourceType = Resource
			break
		} else if strings.Contains(line, "query") {
			resourceType = DataSource
			break
		}
	}

	if resourceType == DataSource {
		result, err = parseDataSourceInput(lines, reg)
		result.ResourceType = DataSource
	} else if resourceType == Resource {
		result, err = parseResourceInput(lines, reg)
		result.ResourceType = Resource
	}

	if err != nil {
		return nil, err
	}
	return &result, nil
}
```

Add the import at the top of `parser.go`:

```go
import (
	"fmt"
	"strings"

	"github.com/opsmill/infrahub-terraform-provider-generator/pkg/schema"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)
```

Change `parseResourceInput` signature to `func parseResourceInput(lines []string, reg *schema.Registry) (InputGraphQLQuery, error)`. Then stamp each field where `newField` is constructed (immediately before the read-only/modify classification at the `if valueSuffix == ""` block added by the prior fix). Insert:

```go
		// Stamp schema-derived type info. objectName is the node kind
		// (namespace+name); HumanReadableName is the attribute name without the
		// edges_node_ prefix. A nil registry or a miss leaves Kind="" (String)
		// and Optional=true, reproducing the untyped default.
		newField.Optional = true
		if attr, ok := reg.Attribute(objectName, strings.ReplaceAll(newField.Name, "edges_node_", "")); ok {
			newField.Kind = attr.Kind
			newField.Optional = attr.Optional
		}
```

Place this AFTER `newField` is built and BEFORE the `if valueSuffix == ""` classification, so the stamped value lands in whichever bucket the field falls into. (`HumanReadableName` is set later by `addHumanReadableField`, so derive the attr name inline from `newField.Name` as shown.)

Change `parseDataSourceInput` signature to `func parseDataSourceInput(lines []string, reg *schema.Registry) (InputGraphQLQuery, error)`. Data sources are read-only, so no stamping is required there; the `reg` parameter is accepted for signature symmetry and future use. Add `_ = reg` at the top of the function body to satisfy the compiler if `reg` is otherwise unused.

- [ ] **Step 5: Thread the registry through `pkg/parser/generators.go`**

Change the exported entry point:

```go
func ReadAndGenerateDataSourcesAndResources(graphqlQuery, providerDirectory string, reg *schema.Registry) (dataSourceName, resourceName string, err error) {
	parsedQuery, err := parseGraphQLQuery(graphqlQuery, reg)
	if err != nil {
		return "", "", fmt.Errorf("parsing GraphQL query: %w", err)
	}
	// ...unchanged below...
```

Add the import to `generators.go`:

```go
	"github.com/opsmill/infrahub-terraform-provider-generator/pkg/schema"
```

- [ ] **Step 6: Update existing call sites to pass `nil`**

In `pkg/parser/parser_test.go` and `pkg/parser/parser_resource_test.go`, every `parseGraphQLQuery(x)` becomes `parseGraphQLQuery(x, nil)`, and `ReadAndGenerateDataSourcesAndResources(sampleResourceGQL, dir)` becomes `ReadAndGenerateDataSourcesAndResources(sampleResourceGQL, dir, nil)`.

Run to find them all:

```bash
grep -rn 'parseGraphQLQuery(\|ReadAndGenerateDataSourcesAndResources(' pkg/parser/*_test.go
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./pkg/parser/ -v 2>&1 | tail -30`
Expected: PASS — new typing tests pass, all pre-existing parser tests still pass (nil-registry path unchanged).

- [ ] **Step 8: Commit**

```bash
git add pkg/parser/parser.go pkg/parser/generators.go pkg/schema/registry.go \
        pkg/parser/parser_typing_test.go pkg/parser/parser_test.go pkg/parser/parser_resource_test.go
git commit -m "feat(parser): stamp Kind/Optional from schema registry"
```

---

## Task 5: Render typed attributes + schema-driven Required/Optional

**Files:**
- Modify: `pkg/parser/generators.go` (register helpers in the FuncMap)
- Modify: `pkg/templates/resource_template.go` (struct fields, Schema block)
- Test: extend `pkg/parser/parser_resource_test.go`

- [ ] **Step 1: Register the helpers in `renderTemplate` (`pkg/parser/generators.go`)**

Replace the `FuncMap` construction:

```go
	tmpl, err := template.New(name).Funcs(template.FuncMap{
		"title":         titleCaser.String,
		"tfType":        tfType,
		"tfAttr":        tfAttr,
		"sdkCreate":     sdkCreate,
		"sdkUpdate":     sdkUpdate,
		"writeAccessor": writeAccessor,
		"readCtor":      readCtor,
	}).Parse(content)
```

- [ ] **Step 2: Write the failing test (extend `pkg/parser/parser_resource_test.go`)**

Add a fixture and test. The fixture selects a Number and a Boolean attribute plus a required one.

```go
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
```

You'll need `"github.com/opsmill/infrahub-terraform-provider-generator/pkg/schema"` imported in this test file.

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./pkg/parser/ -run TestResourceSchemaUsesTypedAttributes -v`
Expected: FAIL — output still has `schema.StringAttribute` and `types.String`.

- [ ] **Step 4: Update the struct field types in `pkg/templates/resource_template.go`**

Replace the struct field block (currently lines ~31-36):

```go
type {{.QueryName }}Resource struct {
	client         *graphql.Client
	{{- if .Required }}
	{{ .Required | title }} types.String ` + "`tfsdk:\"{{ .Required }}\"`" + `
	{{- end }}
	{{- range .GenqlientFields }}
	{{ .Name | title }} {{ tfType . }} ` + "`tfsdk:\"{{ .HumanReadableName }}\"`" + `
	{{- end }}
}
```

(Only the `.GenqlientFields` line changes: `types.String` → `{{ tfType . }}`. The `.Required` filter field stays `types.String`.)

- [ ] **Step 5: Update the Schema() attribute block (currently lines ~47-64)**

```go
		Attributes: map[string]schema.Attribute{
			{{- if .Required }}
			"{{ .Required }}": schema.StringAttribute{
				Required: true,
			},
			{{- end }}
			{{- range .GenqlientFieldsReadOnly }}
			"{{ .HumanReadableName }}": schema.StringAttribute{
				Computed: true,
			},
			{{- end }}
			{{- range .GenqlientFieldsModify }}
			"{{ .HumanReadableName }}": {{ tfAttr . }}{
				{{- if .Optional }}
				Computed: true,
				Optional: true,
				{{- else }}
				Required: true,
				{{- end }}
			},
			{{- end }}
		},
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./pkg/parser/ -run TestResourceSchemaUsesTypedAttributes -v`
Expected: PASS.

- [ ] **Step 7: Run the full parser suite (no regressions)**

Run: `go test ./pkg/parser/ -v 2>&1 | tail -30`
Expected: PASS — existing `TestGenerateTerraformResource` etc. still green (nil registry → all String).

- [ ] **Step 8: Commit**

```bash
git add pkg/parser/generators.go pkg/templates/resource_template.go pkg/parser/parser_resource_test.go
git commit -m "feat(templates): typed struct fields and schema-driven Required/Optional"
```

---

## Task 6: Typed create/update/read (SDK wrappers + accessors)

**Files:**
- Modify: `pkg/templates/resource_template.go` (Create, Update, Read assignment blocks)
- Test: extend `pkg/parser/parser_resource_test.go`

- [ ] **Step 1: Write the failing test (extend `pkg/parser/parser_resource_test.go`)**

```go
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
		// Number optional: guarded create with Int64 accessor + Number wrapper.
		"if !plan.Edges_node_total_vcpu.IsNull() {",
		"infrahub_sdk.NumberAttributeCreate{Value: plan.Edges_node_total_vcpu.ValueInt64()}",
		// Boolean required: always sent with Bool accessor + Checkbox wrapper.
		"infrahub_sdk.CheckboxAttributeCreate{Value: plan.Edges_node_is_active.ValueBool()}",
		// Update: typed wrappers, empty guard only applies to optional string-ish;
		// Number optional uses the typed update wrapper.
		"infrahub_sdk.NumberAttributeUpdate{Value: plan.Edges_node_total_vcpu.ValueInt64()}",
		// Read: typed constructors back into framework values.
		"state.Edges_node_total_vcpu = types.Int64Value(response.",
		"state.Edges_node_is_active = types.BoolValue(response.",
	}
	for _, want := range mustContain {
		if !strings.Contains(code, want) {
			t.Errorf("typed create/update/read missing fragment:\n%s\n---\n%s", want, code)
		}
	}
	assertGofmt(t, code)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/parser/ -run TestResourceCreateUpdateReadAreTyped -v`
Expected: FAIL — create/update still emit `TextAttributeCreate{... ValueString()}`, read emits `types.StringValue`.

- [ ] **Step 3: Update the Create assignment block (currently the guarded `.GenqlientFieldsModify` range)**

```go
	{{- range .GenqlientFieldsModify }}
	{{- if .Optional }}
	// Only send attributes the user actually set; sending an unset optional
	// attribute as an empty value makes Infrahub reject non-text kinds (e.g. a
	// Number attribute fails with "Expected type 'BigInt'").
	if !plan.{{ .Name | title }}.IsNull() {
		default{{$defaultCreate}}.{{ .InputObjectNames }} = {{ sdkCreate . }}{Value: plan.{{ .Name | title }}.{{ writeAccessor . }}}
	}
	{{- else }}
	default{{$defaultCreate}}.{{ .InputObjectNames }} = {{ sdkCreate . }}{Value: plan.{{ .Name | title }}.{{ writeAccessor . }}}
	{{- end }}
	{{- end }}
```

- [ ] **Step 4: Update the Create response assignment (the `.GenqlientFields` range after the mutation call)**

```go
	{{- $defaultCreateObject :=  .ObjectName }}
	{{- range .GenqlientFields }}
	plan.{{ .Name | title }} = {{ readCtor . }}(response.{{ $defaultCreateObject }}Create.Object.{{ .PlainObject }})
	{{- end }}
```

Note: `PlainObject` for typed fields ends in `.Value` (a scalar), so `readCtor` wraps the scalar directly. The `id` field (read via `GetId()`, no `.Value`) is in `GenqlientFieldsReadOnly`, not in this typed write-back where it would need Int64/Bool — it stays String via the default class since its `Kind` is "".

- [ ] **Step 5: Update the Update assignment block (the `.GenqlientFieldsModify` range using `setDefault`)**

```go
	{{- range .GenqlientFieldsModify }}
	{{- if .Optional }}
	// Carry forward the prior value when the plan omits it, but never send an
	// empty value: Infrahub rejects it for non-text kinds.
	if !plan.{{ .Name | title }}.IsNull() {
		updateInput.{{ .InputObjectNames }} = {{ sdkUpdate . }}{Value: plan.{{ .Name | title }}.{{ writeAccessor . }}}
	} else if !state.{{ .Name | title }}.IsNull() {
		updateInput.{{ .InputObjectNames }} = {{ sdkUpdate . }}{Value: state.{{ .Name | title }}.{{ writeAccessor . }}}
	}
	{{- else }}
	updateInput.{{ .InputObjectNames }} = {{ sdkUpdate . }}{Value: plan.{{ .Name | title }}.{{ writeAccessor . }}}
	{{- end }}
	{{- end }}
```

This replaces the `setDefault(...)` call. After this task, `setDefault` is unused for typed fields. Because a `nil` registry leaves every field `Optional=true` with `Kind=""` (String), the optional branch with String accessors reproduces the prior carry-forward behavior **without** `setDefault`. The `setDefault` helper in `pkg/templates/provider_template.go` is now unused; remove it in Step 6 to avoid an "unused function" in generated providers (Go would not complain about an unused top-level func, but it is dead code — remove for cleanliness) — see Step 6.

- [ ] **Step 6: Update the Read assignment block + drop `setDefault`**

Read block (the `.GenqlientFields` range in `Read`):

```go
	{{- $defaultObject :=  .ObjectName }}
	{{- range .GenqlientFields }}
	state.{{ .Name | title }} = {{ readCtor . }}(response.{{ .Query }})
	{{- end }}
```

Remove the now-unused `setDefault` function from `pkg/templates/provider_template.go` (the `func setDefault(value, defaultValue string) string { ... }` block and its preceding comment).

- [ ] **Step 7: Update the existing `TestGenerateTerraformResource` assertion that referenced `setDefault`**

The prior fix added `"setDefault(plan.Edges_node_fqdn.ValueString(), state.Edges_node_fqdn.ValueString())"` to `mustContain`. With `setDefault` gone, that resource (parsed with `nil` registry → optional String) now emits the carry-forward `if/else if`. Replace that one `mustContain` entry with:

```go
		"if !plan.Edges_node_fqdn.IsNull() {",
		"} else if !state.Edges_node_fqdn.IsNull() {",
		"infrahub_sdk.TextAttributeUpdate{Value: state.Edges_node_fqdn.ValueString()}",
```

and remove the now-stale `mustNotContain` entry `"= plan.Edges_node_fqdn.ValueString()\n"` if it conflicts (verify by running; the guarded assignment is now indented inside an `if`, so the raw-assignment regression check may need to become `"= plan.Edges_node_fqdn.ValueString()}"` minus guard — simplest: drop that single `mustNotContain` line since the typed guard supersedes it).

- [ ] **Step 8: Run the full parser suite**

Run: `go test ./pkg/parser/ -v 2>&1 | tail -40`
Expected: PASS — typed test passes; existing resource tests pass with the updated assertions; all `assertGofmt` checks pass.

- [ ] **Step 9: Commit**

```bash
git add pkg/templates/resource_template.go pkg/templates/provider_template.go pkg/parser/parser_resource_test.go
git commit -m "feat(templates): typed create/update/read wrappers; drop setDefault"
```

---

## Task 7: Wire connection flags in `main.go`

**Files:**
- Modify: `main.go`

- [ ] **Step 1: Add flags and fetch the registry once**

Replace the flag block + add fetch before the walk. New imports: `context`, `github.com/opsmill/infrahub-terraform-provider-generator/pkg/schema`.

```go
func run() error {
	graphqlDirectory := flag.String("gql-dir", "gql", "Directory with GraphQL queries")
	providerDirectory := flag.String("provider-dir", "internal/provider", "Directory to write the generated Terraform Provider")
	artifactDataSource := flag.Bool("artifacts", false, "Set flag to be able to query artifacts")
	infrahubAddress := flag.String("infrahub-address", os.Getenv("INFRAHUB_ADDRESS"), "Infrahub base URL; when set with -api-token, attribute types are read from the live schema")
	apiToken := flag.String("api-token", os.Getenv("INFRAHUB_API_TOKEN"), "Infrahub API token (X-INFRAHUB-KEY)")
	branch := flag.String("branch", "main", "Infrahub branch to read the schema from")

	flag.Parse()

	var reg *schema.Registry
	if *infrahubAddress != "" && *apiToken != "" {
		var err error
		reg, err = schema.Fetch(context.Background(), *infrahubAddress, *apiToken, *branch)
		if err != nil {
			return fmt.Errorf("fetching Infrahub schema: %w", err)
		}
		fmt.Printf("Fetched schema from %s (branch %s)\n", *infrahubAddress, *branch)
	} else {
		fmt.Println("No -infrahub-address/-api-token set; generating untyped (string) attributes")
	}

	var dataSources, resources []string

	walkErr := filepath.Walk(*graphqlDirectory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Ext(path) != ".gql" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		dataSourceName, resourceName, err := parser.ReadAndGenerateDataSourcesAndResources(string(data), *providerDirectory, reg)
		if err != nil {
			return fmt.Errorf("processing %s: %w", path, err)
		}
		// ...unchanged switch + appends...
		switch {
		case dataSourceName != "":
			dataSources = append(dataSources, dataSourceName)
			fmt.Printf("Generated data source %q from %s\n", dataSourceName, path)
		case resourceName != "":
			resources = append(resources, resourceName)
			fmt.Printf("Generated resource %q from %s\n", resourceName, path)
		}
		return nil
	})
	// ...rest of run() unchanged...
```

- [ ] **Step 2: Verify the whole module builds and vets**

Run: `go build ./... && go vet ./...`
Expected: no output (success).

- [ ] **Step 3: Run the full test suite**

Run: `go test ./... 2>&1 | tail -10`
Expected: `ok` for `pkg/parser` and `pkg/schema`; no failures.

- [ ] **Step 4: End-to-end smoke test (untyped path unchanged)**

```bash
TMP=$(mktemp -d); mkdir -p "$TMP/gql" "$TMP/out"
cp <a representative .gql or the inline DctProject fixture> "$TMP/gql/"   # or reuse the manual fixture from the session
go run ./cmd/generator -gql-dir "$TMP/gql" -provider-dir "$TMP/out"
gofmt -l "$TMP/out"   # expect empty (generated code is gofmt-clean)
rm -rf "$TMP"
```

Expected: prints "No -infrahub-address... untyped", generates gofmt-clean code identical to today's behavior (no schema → all String).

- [ ] **Step 5: Commit**

```bash
git add main.go
git commit -m "feat(cli): -infrahub-address/-api-token/-branch flags to type attributes from live schema"
```

---

## Task 8: Docs + SDK coordination note

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Document the new flags and the SDK BigInt requirement**

Add to the flags table in `README.md`:

| Flag | Default | Purpose |
| --- | --- | --- |
| `-infrahub-address` | `$INFRAHUB_ADDRESS` | Infrahub base URL; with `-api-token`, types attributes from the live schema |
| `-api-token` | `$INFRAHUB_API_TOKEN` | API token (`X-INFRAHUB-KEY`) |
| `-branch` | `main` | Schema branch to read |

Add a short note: numeric attributes require `infrahub-sdk-go` to bind the `BigInt` scalar to `int64` (`bindings: {BigInt: {type: int64}}` in its `genqlient.yaml`) before regenerating the SDK; otherwise `Number` values are not representable as integers.

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: document schema-typing flags and BigInt binding requirement"
```

---

## Self-Review

**Spec coverage:**
- Number/Bool/String typing → Tasks 3 (mapping), 5 (struct+schema), 6 (create/update/read). ✓
- Required vs Optional from schema → Task 5 (Schema block branch), Task 6 (always-send vs guarded). ✓
- `/api/schema` fetch + envelope + inlined inherited attrs → Task 2. ✓
- nil-registry backward compatibility → Tasks 1, 4 (tests), 5/6 (existing suites stay green). ✓
- JSON/List/NumberPool fallback to String → Task 3 `classOf` default. (Warning emission deferred — see note below.) ✓
- Connection flags + env defaults + error-on-fetch-failure → Tasks 2, 7. ✓
- SDK BigInt binding dependency → documented Tasks 7 (note), 8. ✓

**Deferred from spec (intentional, low-risk):** the `stderr` warning for JSON/List/NumberPool fallback is not yet wired (the spec lists it under out-of-scope handling). If desired, add a one-line `fmt.Fprintf(os.Stderr, ...)` in `parseResourceInput` when `attr.Kind` is one of `JSON`/`List`/`NumberPool`; it needs no new structure. Flagged here rather than silently dropped.

**Type consistency:** helper names (`tfType`, `tfAttr`, `sdkCreate`, `sdkUpdate`, `writeAccessor`, `readCtor`) are defined in Task 3 and used verbatim in Tasks 5–6 templates and registered in Task 5 Step 1. `schema.NewRegistry`/`schema.Attribute`/`Registry.Attribute` consistent across Tasks 1, 2, 4, 5. `GenqlientField.Kind`/`.Optional` added in Task 3, stamped in Task 4, read in Tasks 5–6.

**Placeholder scan:** Task 7 Step 4 contains a literal `<...>` placeholder for the smoke-test fixture path — this is an operator choice (which `.gql` to smoke-test), not missing plan content; the inline DctProject fixture from the session works.
