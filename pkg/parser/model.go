// Package parser reads Infrahub GraphQL queries and renders the Go source for
// a Terraform provider (the provider entrypoint plus its data sources and
// resources) from the templates in pkg/templates.
package parser

// ResourceType classifies a parsed GraphQL operation.
const (
	DataSource ResourceType = iota
	Resource
	Function
)

// ResourceType is the kind of Terraform component a GraphQL query maps to.
type ResourceType int

// InputGraphQLQuery is the intermediate representation produced by parsing a
// single GraphQL query, holding everything the templates need to render.
type InputGraphQLQuery struct {
	QueryName               string
	ObjectName              string
	Required                string
	ReadOp                  string
	CreateOp                string
	UpsertOp                string
	DeleteOp                string
	Fields                  []Field
	GenqlientFields         []GenqlientField
	genqlientFieldsModify   []GenqlientField
	genqlientFieldsReadOnly []GenqlientField
	ResourceType            ResourceType
}

// Field is a single attribute selected by a query.
type Field struct {
	Name              string
	HumanReadableName string
	Type              string
}

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

// DataSourceTemplateData is the data passed to the data source template.
type DataSourceTemplateData struct {
	QueryName       string
	ObjectName      string
	Required        string
	ReadOp          string
	StructName      string
	Fields          []Field
	GenqlientFields []GenqlientField
}

// ResourceTemplateData is the data passed to the resource template.
type ResourceTemplateData struct {
	QueryName               string
	ObjectName              string
	Required                string
	ReadOp                  string
	CreateOp                string
	UpsertOp                string
	DeleteOp                string
	StructName              string
	Fields                  []Field
	GenqlientFields         []GenqlientField
	GenqlientFieldsModify   []GenqlientField
	GenqlientFieldsReadOnly []GenqlientField
}

// ProviderSourceTemplateData is the data passed to the provider template.
type ProviderSourceTemplateData struct {
	DataSources []string
	Resources   []string
	Functions   []string
}

// TerraformComponents lists the names of the data sources and resources the
// provider should register.
type TerraformComponents struct {
	DataSources []string
	Resources   []string
}
