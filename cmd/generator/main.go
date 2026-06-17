package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/opsmill/infrahub-terraform-provider-generator/pkg/parser"
	"github.com/opsmill/infrahub-terraform-provider-generator/pkg/schema"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("generator: %v", err)
	}
}

func run() error {
	graphqlDirectory := flag.String("gql-dir", "gql", "Directory with GraphQL queries")
	providerDirectory := flag.String("provider-dir", "internal/provider", "Directory to write the generated Terraform Provider")
	artifactDataSource := flag.Bool("artifacts", false, "Set flag to be able to query artifacts")
	infrahubAddress := flag.String("infrahub-address", "", "Infrahub base URL; when set with -api-token, attribute types are read from the live schema (env INFRAHUB_ADDRESS)")
	apiToken := flag.String("api-token", "", "Infrahub API token for the X-INFRAHUB-KEY header (env INFRAHUB_API_TOKEN)")
	branch := flag.String("branch", "main", "Infrahub branch to read the schema from")

	flag.Parse()

	// Fall back to the environment only after parsing, so a token is never
	// printed as a flag default in --help output.
	address := *infrahubAddress
	if address == "" {
		address = os.Getenv("INFRAHUB_ADDRESS")
	}
	token := *apiToken
	if token == "" {
		token = os.Getenv("INFRAHUB_API_TOKEN")
	}

	var reg *schema.Registry
	switch {
	case address != "" && token != "":
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		fetched, err := schema.Fetch(ctx, address, token, *branch)
		if err != nil {
			return fmt.Errorf("fetching Infrahub schema: %w", err)
		}
		reg = fetched
		fmt.Printf("Fetched schema from %s (branch %s)\n", address, *branch)
	case address == "" && token == "":
		fmt.Println("No -infrahub-address/-api-token set; generating untyped (string) attributes")
	default:
		return errors.New("-infrahub-address and -api-token must be set together to read the live schema")
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
	if walkErr != nil {
		return walkErr
	}

	if *artifactDataSource {
		if err := parser.GenerateArtifactDatasource(*providerDirectory); err != nil {
			return fmt.Errorf("generating artifact data source: %w", err)
		}
		dataSources = append(dataSources, "Artifact")
		fmt.Println("Generated Artifact data source")
	}

	if err := parser.ReadAndGenerateProvider(parser.TerraformComponents{
		DataSources: dataSources,
		Resources:   resources,
	}, *providerDirectory); err != nil {
		return fmt.Errorf("generating provider: %w", err)
	}
	fmt.Println("Generated provider.go")

	return nil
}
