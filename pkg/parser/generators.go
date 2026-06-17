package parser

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"text/template"

	"github.com/opsmill/infrahub-terraform-provider-generator/pkg/schema"
	"github.com/opsmill/infrahub-terraform-provider-generator/pkg/templates"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// titleCaser title-cases identifiers inside templates. It is created once and
// shared because a cases.Caser is safe for concurrent use.
var titleCaser = cases.Title(language.English)

// templateFuncs are the helpers available to every template.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"title":         titleCaser.String,
		"tfType":        tfType,
		"tfAttr":        tfAttr,
		"sdkCreate":     sdkCreate,
		"sdkUpdate":     sdkUpdate,
		"writeAccessor": writeAccessor,
		"readCtor":      readCtor,
		"dict":          dict,
	}
}

// dict builds a map from alternating key/value arguments, letting a template
// pass a small struct of values to a shared partial (e.g. the Configure block).
func dict(kv ...any) (map[string]any, error) {
	if len(kv)%2 != 0 {
		return nil, fmt.Errorf("dict: got %d arguments, want an even number", len(kv))
	}
	m := make(map[string]any, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		key, ok := kv[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict: key %d is %T, want string", i, kv[i])
		}
		m[key] = kv[i+1]
	}
	return m, nil
}

// renderTemplate parses the named template file (alongside the shared base
// partials) from the embedded template FS and executes it with data.
// text/template (not html/template) is used because the rendered output is Go
// source code, which must never be HTML-escaped.
func renderTemplate(file string, data any) (string, error) {
	tmpl, err := template.New(templates.Base).Funcs(templateFuncs()).ParseFS(templates.FS, templates.Base, file)
	if err != nil {
		return "", fmt.Errorf("parsing %s template: %w", file, err)
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, file, data); err != nil {
		return "", fmt.Errorf("executing %s template: %w", file, err)
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
func ReadAndGenerateDataSourcesAndResources(graphqlQuery, providerDirectory string, reg *schema.Registry) (dataSourceName, resourceName string, err error) {
	parsedQuery, err := parseGraphQLQuery(graphqlQuery, reg)
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

	return renderTemplate(templates.Provider, data)
}

func generateTerraformDataSource(parsedQuery *InputGraphQLQuery) (string, error) {
	data := DataSourceTemplateData{
		QueryName:       parsedQuery.QueryName,
		ObjectName:      parsedQuery.ObjectName,
		Required:        parsedQuery.Required,
		RequiredField:   parsedQuery.RequiredField,
		ReadOp:          parsedQuery.ReadOp,
		StructName:      parsedQuery.QueryName + "DataSource",
		GenqlientFields: parsedQuery.GenqlientFields,
	}

	return renderTemplate(templates.DataSource, data)
}

func generateTerraformResource(parsedQuery *InputGraphQLQuery) (string, error) {
	data := ResourceTemplateData{
		QueryName:               parsedQuery.QueryName,
		ObjectName:              parsedQuery.ObjectName,
		Required:                parsedQuery.Required,
		RequiredField:           parsedQuery.RequiredField,
		ReadOp:                  parsedQuery.ReadOp,
		CreateOp:                parsedQuery.CreateOp,
		UpsertOp:                parsedQuery.UpsertOp,
		DeleteOp:                parsedQuery.DeleteOp,
		IDFieldName:             parsedQuery.IDFieldName,
		StructName:              parsedQuery.QueryName + "Resource",
		GenqlientFields:         parsedQuery.GenqlientFields,
		GenqlientFieldsModify:   parsedQuery.genqlientFieldsModify,
		GenqlientFieldsReadOnly: parsedQuery.genqlientFieldsReadOnly,
	}

	return renderTemplate(templates.Resource, data)
}

// GenerateArtifactDatasource writes the static artifact data source, used to
// fetch generated artifacts from Infrahub's storage API, into providerDirectory.
func GenerateArtifactDatasource(providerDirectory string) error {
	code, err := renderTemplate(templates.Artifact, nil)
	if err != nil {
		return err
	}

	return writeGeneratedFile(filepath.Join(providerDirectory, "artifact_data_source.go"), code)
}
