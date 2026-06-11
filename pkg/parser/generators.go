package parser

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"text/template"

	"github.com/opsmill/infrahub-terraform-provider-generator/pkg/templates"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// titleCaser title-cases identifiers inside templates. It is created once and
// shared because a cases.Caser is safe for concurrent use.
var titleCaser = cases.Title(language.English)

// renderTemplate parses content as a text/template and executes it with data.
// text/template (not html/template) is used because the rendered output is Go
// source code, which must never be HTML-escaped.
func renderTemplate(name, content string, data any) (string, error) {
	tmpl, err := template.New(name).Funcs(template.FuncMap{
		"title": titleCaser.String,
	}).Parse(content)
	if err != nil {
		return "", fmt.Errorf("parsing %s template: %w", name, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing %s template: %w", name, err)
	}

	return buf.String(), nil
}

// writeGeneratedFile gofmt-formats Go source and writes it to path. If the
// rendered source cannot be parsed by gofmt it is written unformatted, with a
// warning, so the output is still available for inspection.
func writeGeneratedFile(path, source string) error {
	formatted, err := format.Source([]byte(source))
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not gofmt %s, writing unformatted: %v\n", path, err)
		formatted = []byte(source)
	}

	if err := os.WriteFile(path, formatted, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}

// ReadAndGenerateProvider renders the Terraform provider entrypoint for the
// given components and writes it to provider.go in providerDirectory.
func ReadAndGenerateProvider(components TerraformComponents, providerDirectory string) error {
	code, err := generateTerraformProvider(components)
	if err != nil {
		return err
	}

	return writeGeneratedFile(filepath.Join(providerDirectory, "provider.go"), code)
}

// ReadAndGenerateDataSourcesAndResources parses a single GraphQL query and
// writes the matching Terraform data source or resource into providerDirectory.
// It returns the generated data source name or resource name (exactly one is
// non-empty on success) along with any error.
func ReadAndGenerateDataSourcesAndResources(graphqlQuery, providerDirectory string) (dataSourceName, resourceName string, err error) {
	parsedQuery, err := parseGraphQLQuery(graphqlQuery)
	if err != nil {
		return "", "", fmt.Errorf("parsing GraphQL query: %w", err)
	}

	switch parsedQuery.ResourceType {
	case DataSource:
		code, err := generateTerraformDataSource(parsedQuery)
		if err != nil {
			return "", "", fmt.Errorf("generating data source: %w", err)
		}
		path := filepath.Join(providerDirectory, parsedQuery.QueryName+"_data_source.go")
		if err := writeGeneratedFile(path, code); err != nil {
			return "", "", err
		}
		return parsedQuery.QueryName, "", nil
	case Resource:
		code, err := generateTerraformResource(parsedQuery)
		if err != nil {
			return "", "", fmt.Errorf("generating resource: %w", err)
		}
		path := filepath.Join(providerDirectory, parsedQuery.QueryName+"_resource.go")
		if err := writeGeneratedFile(path, code); err != nil {
			return "", "", err
		}
		return "", parsedQuery.QueryName, nil
	default:
		return "", "", fmt.Errorf("query %q is neither a resource nor a data source", parsedQuery.QueryName)
	}
}

func generateTerraformProvider(components TerraformComponents) (string, error) {
	data := ProviderSourceTemplateData{
		DataSources: components.DataSources,
		Resources:   components.Resources,
	}

	return renderTemplate("provider", templates.ProviderTemplateContent, data)
}

func generateTerraformDataSource(parsedQuery *InputGraphQLQuery) (string, error) {
	data := DataSourceTemplateData{
		QueryName:       parsedQuery.QueryName,
		ObjectName:      parsedQuery.ObjectName,
		Required:        parsedQuery.Required,
		ReadOp:          parsedQuery.ReadOp,
		StructName:      parsedQuery.QueryName + "DataSource",
		Fields:          parsedQuery.Fields,
		GenqlientFields: parsedQuery.GenqlientFields,
	}

	return renderTemplate("datasource", templates.DatasourceTemplateContent, data)
}

func generateTerraformResource(parsedQuery *InputGraphQLQuery) (string, error) {
	data := ResourceTemplateData{
		QueryName:               parsedQuery.QueryName,
		ObjectName:              parsedQuery.ObjectName,
		Required:                parsedQuery.Required,
		ReadOp:                  parsedQuery.ReadOp,
		CreateOp:                parsedQuery.CreateOp,
		UpsertOp:                parsedQuery.UpsertOp,
		DeleteOp:                parsedQuery.DeleteOp,
		StructName:              parsedQuery.QueryName + "Resource",
		Fields:                  parsedQuery.Fields,
		GenqlientFields:         parsedQuery.GenqlientFields,
		GenqlientFieldsModify:   parsedQuery.genqlientFieldsModify,
		GenqlientFieldsReadOnly: parsedQuery.genqlientFieldsReadOnly,
	}

	return renderTemplate("resource", templates.ResourceTemplateContent, data)
}

// GenerateArtifactDatasource writes the static artifact data source, used to
// fetch generated artifacts from Infrahub's storage API, into providerDirectory.
func GenerateArtifactDatasource(providerDirectory string) error {
	code, err := renderTemplate("artifact", templates.ArtifactTemplateContent, "")
	if err != nil {
		return err
	}

	return writeGeneratedFile(filepath.Join(providerDirectory, "artifact_data_source.go"), code)
}
