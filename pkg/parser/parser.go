package parser

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/opsmill/infrahub-terraform-provider-generator/pkg/schema"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
)

// errMissingQueryName is returned when a document has no parseable read query,
// and therefore no name from which to derive a Terraform component.
var errMissingQueryName = errors.New("parsing GraphQL query: missing query name")

// parseGraphQLQuery parses and classifies a GraphQL document into the
// intermediate representation the templates render. A leading mutation makes it
// a resource; a leading query makes it a data source.
func parseGraphQLQuery(query string, reg *schema.Registry) (*InputGraphQLQuery, error) {
	doc, err := parser.ParseQuery(&ast.Source{Name: "query.gql", Input: query})
	if err != nil {
		return nil, fmt.Errorf("parsing GraphQL query: %w", err)
	}

	resourceType, ok := detectResourceType(doc)
	if !ok {
		return nil, errors.New("parsing GraphQL query: document contains neither a query nor a mutation")
	}

	var result InputGraphQLQuery
	switch resourceType {
	case Resource:
		result, err = parseResourceInput(doc, reg)
	case DataSource:
		result, err = parseDataSourceInput(doc, reg)
	}
	if err != nil {
		return nil, err
	}

	result.ResourceType = resourceType
	return &result, nil
}

// parseResourceInput parses a resource document (create/upsert/delete mutations
// plus a single-result read query) into the IR. The struct fields, schema
// attributes and read-back all derive from the read query's selection; the
// create/upsert/delete operation names are taken verbatim from the mutations so
// the generated code calls the matching genqlient functions.
func parseResourceInput(doc *ast.QueryDocument, reg *schema.Registry) (InputGraphQLQuery, error) {
	objectName, required, varTypes, paths, err := collectFields(doc)
	if err != nil {
		return InputGraphQLQuery{}, err
	}
	queryName, readOp := queryNameAndOp(doc)
	if queryName == "" {
		return InputGraphQLQuery{}, errMissingQueryName
	}

	var genqlientFields, genqlientFieldsModify, genqlientFieldsReadOnly []GenqlientField
	var idFieldName string
	for _, fp := range paths {
		// Resources read the node's id through GetId() (the mutation response
		// types expose it as a method, not a field).
		field, readOnly := buildField(objectName, required, fp, true)
		if readOnly {
			genqlientFieldsReadOnly = append(genqlientFieldsReadOnly, field)
			// The node's own UUID is the read-only field whose human-readable
			// name is exactly "id"; related ids carry their relationship prefix.
			if field.HumanReadableName == "id" {
				idFieldName = field.Name
			}
		} else {
			// Stamp schema-derived type info only on configurable fields; the
			// read-only id is always a string UUID and must never be typed. A
			// nil registry or a miss leaves Kind="" (String) and Optional=true,
			// reproducing the untyped default.
			field.Optional = true
			if attr, ok := reg.Attribute(objectName, field.HumanReadableName); ok {
				field.Kind = attr.Kind
				field.Optional = attr.Optional
			}
			genqlientFieldsModify = append(genqlientFieldsModify, field)
		}
		genqlientFields = append(genqlientFields, field)
	}

	if idFieldName == "" {
		return InputGraphQLQuery{}, fmt.Errorf("parsing GraphQL query %q: a resource must select the node's own id", queryName)
	}

	createOp, upsertOp, deleteOp := operationNames(doc)
	// Fall back to the Infrahub <Kind><Op> convention when an operation name
	// could not be read from the document.
	if createOp == "" {
		createOp = objectName + "Create"
	}
	if upsertOp == "" {
		upsertOp = objectName + "Upsert"
	}
	if deleteOp == "" {
		deleteOp = objectName + "Delete"
	}
	if readOp == "" {
		readOp = ucFirst(queryName)
	}

	return InputGraphQLQuery{
		QueryName:               queryName,
		ObjectName:              objectName,
		Required:                required,
		RequiredField:           stampRequired(required, objectName, varTypes[required], reg),
		ReadOp:                  readOp,
		CreateOp:                createOp,
		UpsertOp:                upsertOp,
		DeleteOp:                deleteOp,
		IDFieldName:             idFieldName,
		GenqlientFields:         genqlientFields,
		genqlientFieldsModify:   genqlientFieldsModify,
		genqlientFieldsReadOnly: genqlientFieldsReadOnly,
	}, nil
}

// parseDataSourceInput parses a read query into a data source, stamping each
// field's Kind/Optional from the schema registry so reads are type-correct.
func parseDataSourceInput(doc *ast.QueryDocument, reg *schema.Registry) (InputGraphQLQuery, error) {
	objectName, required, varTypes, paths, err := collectFields(doc)
	if err != nil {
		return InputGraphQLQuery{}, err
	}
	queryName, readOp := queryNameAndOp(doc)
	if queryName == "" {
		return InputGraphQLQuery{}, errMissingQueryName
	}

	var genqlientFields []GenqlientField
	for _, fp := range paths {
		// Data sources read the node's id as a plain `.Id` field (the query
		// response types expose it directly), not through GetId().
		field, _ := buildField(objectName, required, fp, false)
		field.Optional = true
		if attr, ok := reg.Attribute(objectName, field.HumanReadableName); ok {
			field.Kind = attr.Kind
			field.Optional = attr.Optional
		}
		genqlientFields = append(genqlientFields, field)
	}

	if readOp == "" {
		readOp = ucFirst(queryName)
	}

	return InputGraphQLQuery{
		QueryName:       queryName,
		ObjectName:      objectName,
		Required:        required,
		RequiredField:   stampRequired(required, objectName, varTypes[required], reg),
		ReadOp:          readOp,
		GenqlientFields: genqlientFields,
	}, nil
}

// fieldPath is a selected leaf attribute together with its relationship prefix
// stack (e.g. ["edges", "node"]) captured during the brace-walk.
type fieldPath struct {
	parts []string // prefix frames followed by the attribute name
}

// buildField turns a captured field path into the GenqlientField the templates
// consume, computing every access path they need. The node's own UUID and
// related ids are selected as `id` and carry no `.Value` suffix; every other
// scalar is read as `attr { value }` and so carries `.Value`. When idAsGetID is
// set, an id is read through GetId() (mutation responses) instead of the `.Id`
// field (query responses). readOnly reports whether the field is an id, which
// is server-assigned and never configurable.
func buildField(objectName, required string, fp fieldPath, idAsGetID bool) (field GenqlientField, readOnly bool) {
	titled := make([]string, len(fp.parts))
	for i, p := range fp.parts {
		titled[i] = titleCaser.String(p)
	}
	readOnly = len(titled) > 0 && titled[len(titled)-1] == "Id"

	// Edges are indexed: a single keyed lookup reads Edges[0]; an unfiltered
	// list reads Edges[i].
	edge := "Edges[i]"
	if required != "" {
		edge = "Edges[0]"
	}

	queryParts := make([]string, len(titled))
	var noEdgeNode []string // path with Edges/Node frames removed (input + mutation response)
	for i, p := range titled {
		if p == "Edges" {
			queryParts[i] = edge
		} else {
			queryParts[i] = p
		}
		if p != "Edges" && p != "Node" {
			noEdgeNode = append(noEdgeNode, p)
		}
	}

	valueSuffix := ".Value"
	if readOnly {
		valueSuffix = ""
		if idAsGetID {
			queryParts[len(queryParts)-1] = "GetId()"
			if n := len(noEdgeNode); n > 0 {
				noEdgeNode[n-1] = "GetId()"
			}
		}
	}

	name := strings.Join(fp.parts, "_")
	field = GenqlientField{
		Field:            Field{Name: name, HumanReadableName: humanReadableName(name)},
		Query:            objectName + "." + strings.Join(queryParts, ".") + valueSuffix,
		InputObjectNames: strings.Join(noEdgeNode, "."),
		PlainObject:      strings.Join(noEdgeNode, ".") + valueSuffix,
	}
	return field, readOnly
}

// gqlTypeToKind maps a GraphQL scalar type to the equivalent Infrahub
// AttributeKind understood by classOf, so the key attribute is typed from the
// .gql variable declaration (which determines the generated SDK signature)
// rather than guessed. String, ID and unknown scalars map to "" (string).
func gqlTypeToKind(gqlType string) string {
	switch gqlType {
	case "Int", "BigInt", "Float", "Number":
		return "Number"
	case "Boolean":
		return "Boolean"
	default:
		return ""
	}
}

// humanReadableName is the attribute name as exposed in Terraform: the parsed
// field name with the GraphQL edges_node_ prefix removed. It is also the key
// used to look the attribute up in the schema registry.
func humanReadableName(fieldName string) string {
	return strings.ReplaceAll(fieldName, "edges_node_", "")
}

// stampRequired builds the typed GenqlientField for the filter/key attribute.
// The key type is taken from the .gql query-variable declaration (gqlType),
// which is what determines the generated SDK function's parameter type and thus
// what compiles; the schema registry only supplies Optional and a cross-check.
// A class mismatch between the declared variable type and the schema kind is
// warned about, since it usually means the .gql will not compile against the
// generated SDK.
func stampRequired(required, objectName, gqlType string, reg *schema.Registry) GenqlientField {
	f := GenqlientField{Field: Field{Name: required, HumanReadableName: required}, Optional: true}
	if required == "" {
		return f
	}
	f.Kind = gqlTypeToKind(gqlType)
	if attr, ok := reg.Attribute(objectName, required); ok {
		f.Optional = attr.Optional
		if classOf(GenqlientField{Kind: attr.Kind}) != classOf(f) {
			fmt.Fprintf(os.Stderr, "warning: key %q is declared %q in the .gql but %q in the schema; "+
				"the generated lookup may not compile — align the .gql variable type with the schema\n",
				required, gqlType, attr.Kind)
		}
	}
	return f
}

// lcFirst lower-cases the first rune of s.
func lcFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// ucFirst upper-cases the first rune of s.
func ucFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
