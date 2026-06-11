# Infrahub Terraform Provider Generator

This Go application generates a custom Terraform Provider for Infrahub. Supply it with GraphQL queries, and it will return their respective Data Sources or Resources.

```bash
go run github.com/opsmill/infrahub-terraform-provider-generator/cmd/generator --help
Usage of Generator:
  -gql-dir string
        Directory with GraphQL queries (default "gql")
  -provider-dir string
        Directory to write the generated Terraform Provider (default "internal/provider")

```