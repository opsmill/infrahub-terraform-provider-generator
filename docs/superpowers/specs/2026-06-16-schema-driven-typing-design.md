# Schema-driven attribute typing — design

Date: 2026-06-16
Status: Approved (pending written-spec review)

## Problem

The generator builds a Terraform provider from Infrahub `.gql` files alone. The
`.gql` carries no attribute type information, so every attribute is generated as
`types.String` and every input value is wrapped in `infrahub_sdk.TextAttributeCreate`
/ `TextAttributeUpdate`.

This breaks non-text attributes. The Minotaur customer's `DctVCenter` has
`Number` attributes (`total_vcpu`, `total_memory_mb`, …). Infrahub rejects a
`Number` field that receives a text/empty value with `Expected type 'BigInt'`.

A prior fix (already on `main`, working tree) stopped *unset* optional
attributes from being sent as empty strings, which unblocked creation when the
numeric fields are left blank. This design covers the remaining gap: typing
attributes correctly so a numeric value can actually be **set**, and using the
schema's `optional` flag to drive Terraform `Required` vs `Optional`.

## Research findings (load-bearing facts)

From `opsmill/infrahub-sdk-go` (private; genqlient-generated) and Infrahub docs:

- The SDK is regenerated against the customer's schema with vanilla genqlient
  (`go run github.com/Khan/genqlient`); its `genqlient.yaml` has **no scalar
  bindings**.
- There are exactly **5** attribute input types in the schema:
  - `TextAttributeCreate` / `…Update` — `value: String`
  - `NumberAttributeCreate` / `…Update` — `value: BigInt`
  - `CheckboxAttributeCreate` / `…Update` — `value: Boolean`
  - `JSONAttributeCreate` / `…Update`
  - `ListAttributeCreate` / `…Update`
- A `Dropdown` attribute uses `TextAttributeCreate`. So `Dropdown`, `IPHost`,
  `DateTime`, `TextArea`, `Text` all map to **String**; only `Number` and
  `Boolean`/`Checkbox` diverge.
- genqlient auto-maps only the 5 standard scalars (`Int`/`Float`/`String`/
  `Boolean`/`ID`). `BigInt` is a **custom, unbound** scalar and currently
  resolves to Go `string`.
- Infrahub schema: `kind = namespace + name`, so the `objectName` parsed from a
  `.gql` (e.g. `DctVCenter`) **is** the node kind — usable directly as the
  registry key. Attributes carry `name`, `kind`, `optional`. Auth header is
  `X-INFRAHUB-KEY`.

### `/api/schema` shape (confirmed against the installed `infrahub_sdk`)

Verified from the Python SDK bundled in the customer poc venv
(`infrahub_sdk/schema/`), so no live instance was needed:

- Read endpoint: `GET {address}/api/schema?branch={branch}` (an optional
  repeated `namespaces=` param exists but we don't need it). Non-200 raises in
  the SDK — we mirror that by erroring out.
- Response envelope (`SchemaRootAPI`): `{"nodes": [...], "generics": [...]}`.
- Each node (`NodeSchemaAPI`) has a `kind` (= namespace+name) and
  `attributes: [AttributeSchemaAPI]`. Each attribute carries `name`,
  `kind` (the `AttributeKind` enum), `optional: bool`, `read_only: bool`, and
  **`inherited: bool`**.
- **Inheritance is pre-resolved:** attributes inherited from generics are
  **already inlined** into each node's `attributes` list (flagged
  `inherited: true`). The decoder reads `nodes[].attributes[]` directly and does
  **not** need to follow `inherit_from` / merge generics.
- `AttributeKind` values: `ID, Text, String, TextArea, DateTime, Number,
  NumberPool, Dropdown, Email, Password, HashedPassword, URL, File, MacAddress,
  Color, Bandwidth, IPHost, IPNetwork, Boolean, Checkbox, List, JSON, Any`.

### Dependency: BigInt binding (decided)

Real numeric values require `infrahub-sdk-go` to bind `BigInt` to a numeric Go
type. Decision: **bind `BigInt → int64`** in the SDK's `genqlient.yaml`:

```yaml
bindings:
  BigInt:
    type: int64
```

This is a small, separate change in the SDK repo, owned by OpsMill. This
generator targets the post-binding SDK: `NumberAttribute{Create,Update}.Value`
is `int64`, and reads of a `Number` attribute's `.Value` are `int64`.

## Scope

In scope:

1. `Number → types.Int64` (+ `NumberAttribute{Create,Update}{Value: int64}`),
   `Boolean`/`Checkbox → types.Bool` (+ `CheckboxAttribute{…}{Value: bool}`),
   everything else `types.String` (+ `TextAttribute{…}`), unchanged.
2. Drive Terraform `Required` vs `Optional` from the schema's `optional` flag.

Out of scope (fall back to String + a `stderr` warning):

- `JSON` and `List` attribute kinds.

## Approach (A — chosen)

Dedicated schema package + parser annotation + template helpers. Matches the
existing `parse → build IR → render` architecture; isolates the only new
concern (talking to Infrahub) in its own package; each unit is independently
testable.

## Architecture

### New: `pkg/schema/`

```
Fetch(ctx, address, token, branch) (*Registry, error)
    GET {address}/api/schema?branch={branch}
    header X-INFRAHUB-KEY: {token}
    non-200 -> error
    decode {nodes:[{kind, attributes:[{name, kind, optional}]}]}
    -> Registry   (read nodes[].attributes[] directly; inherited attrs
                   are already inlined, so generics need not be merged)

type Attribute struct { Kind string; Optional bool }
type Registry struct { /* map[nodeKind]map[attrName]Attribute */ }
func (r *Registry) Attribute(nodeKind, attrName string) (Attribute, bool)
```

Pure stdlib (`net/http`, `encoding/json`). A `nil` `*Registry` is a valid value
whose `Attribute` always returns `(_, false)` — lets callers stay
branch-free.

### New: `pkg/parser/types.go`

Single source of truth for the kind mapping, plus the `text/template` FuncMap
helpers that consume it:

| Infrahub kind        | TF attribute       | TF type        | SDK wrapper                | write accessor   | read constructor                  |
| -------------------- | ------------------ | -------------- | -------------------------- | ---------------- | --------------------------------- |
| `Number`             | `Int64Attribute`   | `types.Int64`  | `NumberAttribute{C,U}`     | `.ValueInt64()`  | `types.Int64Value(int64(v))`      |
| `Boolean`/`Checkbox` | `BoolAttribute`    | `types.Bool`   | `CheckboxAttribute{C,U}`   | `.ValueBool()`   | `types.BoolValue(v)`              |
| else (incl. unknown) | `StringAttribute`  | `types.String` | `TextAttribute{C,U}`       | `.ValueString()` | `types.StringValue(v)`            |

"else" covers `Text, String, TextArea, DateTime, Dropdown, Email, Password,
HashedPassword, URL, File, MacAddress, Color, Bandwidth, IPHost, IPNetwork, Any`
(all `String`-valued in the SDK). `NumberPool`, `JSON`, and `List` also fall
into "else" (String) for now, each with a `stderr` warning since their real
wrapper differs — see out-of-scope note.

Helpers (registered in `generators.go`): `tfType`, `tfAttr`, `sdkCreate`,
`sdkUpdate`, `writeAccessor`, `readCtor` — each takes a `GenqlientField` (or its
`Kind`) and returns the corresponding fragment.

### Changed: `pkg/parser/model.go`

`GenqlientField` gains `Kind string` and `Optional bool`. `ResourceTemplateData`
/ `DataSourceTemplateData` already carry the field slices, so no new top-level
template fields beyond what the helpers read off each field.

### Changed: `pkg/parser/parser.go`

`parseGraphQLQuery(query string, reg *schema.Registry)` and the two sub-parsers
stamp each `GenqlientField`:

- `attrName` = field name with the `edges_node_` prefix removed (the existing
  `HumanReadableName` derivation).
- `reg.Attribute(objectName, attrName)` hit → set `Kind`, `Optional`.
- miss / `reg == nil` → `Kind = ""` (→ String), `Optional = true` (today's
  behavior).

The read-only vs configurable split is unchanged from the shipped fix
(`valueSuffix == ""` ⇒ the node's `GetId()` ⇒ read-only).

### Changed: `pkg/parser/generators.go`

Thread `reg` through `ReadAndGenerateDataSourcesAndResources`; register the
FuncMap helpers in `renderTemplate`.

### Changed: `main.go`

New flags (all optional):

| Flag                 | Env                    | Default | Purpose                          |
| -------------------- | ---------------------- | ------- | -------------------------------- |
| `-infrahub-address`  | `INFRAHUB_ADDRESS`     | (unset) | Base URL of the Infrahub API     |
| `-api-token`         | `INFRAHUB_API_TOKEN`   | (unset) | API token (`X-INFRAHUB-KEY`)     |
| `-branch`            | —                      | `main`  | Schema branch to read            |

If address + token are set → `schema.Fetch` once → registry passed to every
`.gql`. Otherwise registry is `nil` and the tool behaves exactly as today.

## Data flow

```
main:
  reg = nil
  if address && token: reg = schema.Fetch(ctx, address, token, branch)   // once
  walk gql-dir:
    ReadAndGenerateDataSourcesAndResources(gql, providerDir, reg)
        parseGraphQLQuery(gql, reg)        // stamps Kind/Optional per field
        generateTerraformResource(...)     // helpers render type-correct code
```

The `.gql` still decides *what* is generated and *which* fields appear; the
schema supplies only *types* and *optionality*.

## Required / Optional / read-only rules

- Node `id` (`GetId()` field): `Computed` `String`. Unchanged.
- Filter/key attribute (the GraphQL query variable): `Required` `String` — it is
  always a `String` variable regardless of the underlying attribute.
- Non-id attribute, schema `optional: false`: **`Required`**, always sent on
  create and update.
- Non-id attribute, schema `optional: true` (or registry miss): **`Optional` +
  `Computed`**; sent on create only when set, carried forward on update.

## Create / Update generation (kind-uniform)

Replaces the string-only `setDefault` helper with type-correct, null-guarded
assignment for all three types.

- Required & filter:
  `input.X = <Wrapper>{Value: plan.X.<accessor>}`  (always).
- Optional, create:
  `if !plan.X.IsNull() { input.X = <Create>{Value: plan.X.<accessor>} }`.
- Optional, update (carry-forward, no empty send):
  `if !plan.X.IsNull() { …plan… } else if !state.X.IsNull() { …state… }`.

This generalizes the empty-value guard already shipped (currently String-only,
using `setDefault`) to Int64 and Bool, and removes `setDefault`.

## Error handling / fallback

- Address configured but fetch fails (network / auth / decode): **return an
  error** — never emit wrong types silently.
- No connection configured: registry `nil` → today's all-String behavior;
  offline runs unaffected.
- Attribute absent from registry: String + Optional default, no error.
- `JSON` / `List` kind: String fallback + a `stderr` warning naming the
  attribute, so the user knows the wrapper is approximate.

## Testing

- `pkg/schema`: `httptest.Server` returning canned schema JSON → assert
  `Attribute` lookups (Number / Bool / Text, optional true/false) and
  miss-fallback; assert `Fetch` errors on non-200 / bad JSON; assert a `nil`
  `*Registry` returns `(_, false)`.
- `pkg/parser` (annotation): inject a fake registry → assert `Kind` / `Optional`
  stamping and the Required-vs-Optional bucketing; nil-registry path keeps the
  existing tests green.
- `pkg/parser` (generate): extend the resource tests —
  - Number optional → `Int64Attribute` Optional+Computed, create guarded by
    `if !plan.X.IsNull()` with `NumberAttributeCreate{Value: plan.X.ValueInt64()}`,
    read via `types.Int64Value`.
  - Number required → always sent, rendered `Required`.
  - Boolean → `BoolAttribute` + `CheckboxAttributeCreate{Value: plan.X.ValueBool()}`.
  - nil-registry backward-compat → all String, matches current output.
  - all generated output gofmt-clean (existing `assertGofmt`).

## Resolved during design (was: assumptions to verify)

Both prior open items are now confirmed (see "`/api/schema` shape" above):

- Response envelope is `{"nodes": [...], "generics": [...]}`; read endpoint is
  `GET /api/schema?branch={branch}`.
- Inherited generic attributes are **already inlined** per node (flagged
  `inherited`), so the decoder reads `nodes[].attributes[]` directly and skips
  `inherit_from` resolution.

Nothing in the approach changes; the decoder struct shape is now pinned down.

## Out of band (separate repo)

`infrahub-sdk-go`'s `genqlient.yaml` must add `bindings: {BigInt: {type:
int64}}` and be regenerated. This generator targets the post-binding SDK and
must land together with it. Tracked here so it isn't lost.
