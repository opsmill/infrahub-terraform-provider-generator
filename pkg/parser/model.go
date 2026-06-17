// Package parser reads Infrahub GraphQL queries and renders the Go source for
// a Terraform provider (the provider entrypoint plus its data sources and
// resources) from the templates in pkg/templates.
package parser

// ResourceType is the kind of Terraform component a GraphQL operation maps to.
type ResourceType int

// The kinds of Terraform component a parsed GraphQL operation can produce.
const (
	DataSource ResourceType = iota
	Resource
)

// InputGraphQLQuery is the intermediate representation produced by parsing a
// single GraphQL query, holding everything the templates need to render.
type InputGraphQLQuery struct {
	QueryName               string
	ObjectName              string
	Required                string
	RequiredField           GenqlientField
	ReadOp                  string
	CreateOp                string
	UpsertOp                string
	DeleteOp                string
	IDFieldName             string // struct field name of the node's own id (resources only)
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
	Query            string // read path, e.g. "DctVCenter.Edges[0].Node.Fqdn.Value"
	InputObjectNames string // mutation input field path, with edges/node removed
	PlainObject      string // mutation response path, with edges/node removed
	Kind             string // Infrahub AttributeKind; "" means untyped (String).
	Optional         bool   // true unless the schema marks the attribute required.
}

// DataSourceTemplateData is the data passed to the data source template.
type DataSourceTemplateData struct {
	QueryName       string
	ObjectName      string
	Required        string
	RequiredField   GenqlientField
	ReadOp          string
	StructName      string
	GenqlientFields []GenqlientField
}

// ResourceTemplateData is the data passed to the resource template.
type ResourceTemplateData struct {
	QueryName               string
	ObjectName              string
	Required                string
	RequiredField           GenqlientField
	ReadOp                  string
	CreateOp                string
	UpsertOp                string
	DeleteOp                string
	IDFieldName             string
	StructName              string
	GenqlientFields         []GenqlientField
	GenqlientFieldsModify   []GenqlientField
	GenqlientFieldsReadOnly []GenqlientField
}

// ProviderSourceTemplateData is the data passed to the provider template.
type ProviderSourceTemplateData struct {
	DataSources []string
	Resources   []string
}

// TerraformComponents lists the names of the data sources and resources the
// provider should register.
type TerraformComponents struct {
	DataSources []string
	Resources   []string
}
