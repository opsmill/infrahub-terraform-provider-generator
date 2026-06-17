# AGENTS.md

## Project Overview

The Infrahub Terraform Provider Generator is a Go CLI that builds a custom
Terraform provider for Infrahub from a directory of GraphQL queries. It parses
each `.gql` file into an intermediate representation (`pkg/parser`), then renders
the provider's Go source — the provider entrypoint, data sources, and resources —
from the templates in `pkg/templates`. Queries become data sources and mutations
become resources. When `-infrahub-address` and `-api-token` are set, `pkg/schema`
fetches the live Infrahub schema and types each attribute from its Infrahub kind;
otherwise every attribute is generated as a Terraform `String`. The CLI entrypoint
lives in `cmd/generator`.

## Common Development Commands

- `make build` - Build the generator binary into `bin/`
- `make test` - Run all tests
- `make test-coverage` - Generate coverage report (outputs coverage.html)
- `make lint` - Run golangci-lint (note: errcheck is disabled in .golangci.yaml)
- `make fmt` - Format code with gofmt
- `make vet` - Run go vet
- `make deps` - Download dependencies

## Architecture

- **cmd/generator** - CLI entrypoint; walks `-gql-dir`, optionally fetches the
  schema, and drives generation into `-provider-dir`.
- **pkg/parser** - Reads `.gql` queries and mutations and builds the model the
  templates render from, including the operation names used to call the Infrahub
  SDK.
- **pkg/templates** - Go source templates for the provider entrypoint, data
  sources, and resources.
- **pkg/schema** - Fetches an Infrahub schema and answers attribute-type lookups
  so generated attributes can be typed (`types.Int64` / `types.Bool` /
  `types.String`).

## Error Handling

The codebase uses explicit error wrapping with `fmt.Errorf("...: %w", err)` for
context. Errors propagate up to `main`, which is the only place that calls
`log.Fatalf`; library packages never call `os.Exit` or `panic`.
