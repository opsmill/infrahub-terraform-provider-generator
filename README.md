# Infrahub Terraform Provider Generator

The Infrahub Terraform Provider Generator is a Go tool that builds a custom Terraform provider for Infrahub directly from your GraphQL queries. Point it at a directory of `.gql` files and it generates the provider's resources and data sources for you — no hand-written provider boilerplate.

---

## What You Can Do With It

Generate a provider tailored to your Infrahub schema, then manage Infrahub the way you manage the rest of your infrastructure:

- **Generate a Terraform provider from GraphQL** — drop your Infrahub queries into a directory and get provider source with the boilerplate written for you
- **Manage Infrahub objects as Terraform resources** — every mutation set becomes a resource with full create, read, update, and delete support
- **Read Infrahub data into Terraform** — every read query becomes a data source, either a single-object lookup by key or a list
- **Pull rendered artifacts into your config** — enable the artifact data source to fetch generated artifacts from Infrahub's storage API
- **Regenerate as your model evolves** — add or remove a `.gql` file and re-run; the provider, resources, and data sources are rewritten to match

---

## Who This Is For

**Infrahub users adopting Terraform:** You already manage infrastructure with Terraform and want Infrahub objects in the same workflow. Supply the GraphQL queries for the objects you care about and generate a provider scoped to your schema. → Start with [Quick Start](#quick-start).

**Provider builders and contributors:** You want a reference for how an Infrahub Terraform provider is structured, or you want to extend the generator's templates and parser. → See [What's Included](#whats-included) and dig into `pkg/`.

---

## Prerequisites

- [Go](https://go.dev) 1.25+ to run the generator
- A directory of Infrahub GraphQL queries (`.gql` files) — queries become data sources, mutations become resources
- A running [Infrahub](https://github.com/opsmill/infrahub) instance and an API key for the generated provider to use at apply time

---

## Quick Start

```bash
# See available options
go run github.com/opsmill/infrahub-terraform-provider-generator/cmd/generator --help

# Generate a provider from a directory of .gql queries
go run github.com/opsmill/infrahub-terraform-provider-generator/cmd/generator \
  -gql-dir gql \
  -provider-dir internal/provider \
  -artifacts
```

| Flag | Default | Purpose |
| --- | --- | --- |
| `-gql-dir` | `gql` | Directory to scan for `.gql` query files |
| `-provider-dir` | `internal/provider` | Directory to write the generated provider source into |
| `-artifacts` | `false` | Also generate the artifact data source |
| `-infrahub-address` | `$INFRAHUB_ADDRESS` | Infrahub base URL; with `-api-token`, attribute types are read from the live schema |
| `-api-token` | `$INFRAHUB_API_TOKEN` | API token, sent as the `X-INFRAHUB-KEY` header |
| `-branch` | `main` | Infrahub branch to read the schema from |

### GraphQL file layout

The parser is line-oriented: each selected field must sit on its own line, and a
relationship block opens with `<name> {` on its own line. A scalar attribute may
be selected on a single line (`fqdn { value }`) or across lines:

```graphql
fqdn {
  value
}
```

A resource document is one create, one upsert and one delete mutation followed
by a single-result read query, and its read query **must select the node's own
`id`**. Inline single-line documents (the whole query on one line) are not
supported.

### Attribute typing from the schema

By default every attribute is generated as a Terraform `String`. When you pass
`-infrahub-address` and `-api-token`, the generator reads the schema for the
given `-branch` and types each attribute from its Infrahub kind:

- `Number` → `types.Int64`
- `Boolean` / `Checkbox` → `types.Bool`
- everything else → `types.String`

The filter/key attribute is typed the same way; for a non-text key the `.gql`
lookup query must declare a matching variable type (e.g. `$asn: BigInt!`).
Attributes the schema marks required (`optional: false`) are generated as
Terraform `Required` and always sent; optional attributes are sent only when
set. Data sources are typed too. Without these flags the generator stays fully
offline and types every attribute as `String`, exactly as before.

> **The SDK's scalar bindings must match the generated types.** The generated
> provider talks to Infrahub through a genqlient-built SDK. In the
> [provider template](https://github.com/opsmill/infrahub-terraform-provider-template)
> that SDK is built locally from `sdk/genqlient.yaml`, whose `bindings:` must
> line up with the types this generator emits:
>
> - `BigInt: int64` — `Number` attributes use `ValueInt64()` / `types.Int64Value`
> - `DateTime: string` — `DateTime` is rendered as `types.String` (the plugin
>   framework has no DateTime type)
>
> The template ships these defaults as `string` / `time.Time`, which will not
> compile against typed output. Update them and run `make generate_sdk`.
>
> No custom marshaler is needed: Infrahub's `BigInt` is graphene's built-in
> scalar (`serialize = coerce_int`), so it is sent and received as a JSON
> number, which genqlient maps cleanly to `int64`; `DateTime` is an ISO-8601
> string. (`int64` covers values up to ~9.2×10¹⁸, matching Terraform's
> `types.Int64`.)

---

## What You'll See

When you run the generator against a directory of queries:

1. It walks `-gql-dir` and reads every `.gql` file it finds
2. Each **query** (read operation) is written as `<name>_data_source.go`
3. Each **mutation** set (create / upsert / delete) is written as `<name>_resource.go`
4. With `-artifacts`, it also writes `artifact_data_source.go`
5. Finally it writes `provider.go`, registering every generated resource and data source

The result is a set of Go source files under `-provider-dir`, ready to be built into a Terraform provider that talks to Infrahub over GraphQL.

---

## What's Included

The repository is the generator itself — the tool and the templates it renders:

- **Generator CLI** (`cmd/generator`) — walks a directory of GraphQL queries and writes the provider source
- **Query parser** (`pkg/parser`) — reads `.gql` queries and mutations and builds the model the templates render from, including the operation names used to call the Infrahub SDK
- **Code templates** (`pkg/templates`) — the Go templates for the provider entrypoint, resources, data sources, and the artifact data source

The generated provider is built on the [Terraform Plugin Framework](https://developer.hashicorp.com/terraform/plugin/framework) and calls Infrahub through the [Infrahub Go SDK](https://github.com/opsmill/infrahub-sdk-go).

**Note:** This tool generates the provider's source code. Compiling, versioning, and publishing the provider are separate steps in your own build pipeline.

---

## Status

This is a newer, actively maintained rebuild. It is inspired by the original work of **Marco Martinez** (`marcomartinez`); we are rebuilding it as a version we will own and maintain going forward.

---

## Going Deeper

|  |  |
| --- | --- |
| **Run the generator** | [Quick Start](#quick-start) |
| **Understand the components** | [What's Included](#whats-included) |
| **Terraform provider internals** | [Terraform Plugin Framework](https://developer.hashicorp.com/terraform/plugin/framework) |
| **How the Infrahub SDK is generated** | [genqlient](https://github.com/Khan/genqlient) |
| **Infrahub core docs** | [Infrahub on GitHub](https://github.com/opsmill/infrahub) |

---

## About Infrahub

[Infrahub](https://github.com/opsmill/infrahub) is an open source infrastructure data management and automation platform (Apache 2.0), developed by [OpsMill](https://opsmill.com). It gives infrastructure and network teams a unified, schema-driven source of truth for all infrastructure data — devices, topology, IP space, configuration — with built-in version control, a generator framework for automation, and native integrations with Git, Ansible, Terraform, and CI/CD pipelines.
