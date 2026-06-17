package parser

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/opsmill/infrahub-terraform-provider-generator/pkg/schema"
)

// errMissingQueryName is returned when a document has no parseable read query,
// and therefore no name from which to derive a Terraform component.
var errMissingQueryName = errors.New("parsing GraphQL query: missing query name")

// parseGraphQLQuery classifies a GraphQL document and parses it into the
// intermediate representation the templates render. A leading mutation makes it
// a resource; a leading query makes it a data source.
func parseGraphQLQuery(query string, reg *schema.Registry) (*InputGraphQLQuery, error) {
	lines := normalizeScalarBlocks(strings.Split(query, "\n"))

	resourceType, ok := detectResourceType(lines)
	if !ok {
		return nil, errors.New("parsing GraphQL query: document contains neither a query nor a mutation")
	}

	var result InputGraphQLQuery
	var err error
	switch resourceType {
	case Resource:
		result, err = parseResourceInput(lines, reg)
	case DataSource:
		result, err = parseDataSourceInput(lines, reg)
	}
	if err != nil {
		return nil, err
	}

	result.ResourceType = resourceType
	return &result, nil
}

// detectResourceType classifies a document by its first operation keyword.
func detectResourceType(lines []string) (ResourceType, bool) {
	for _, line := range lines {
		switch trimmed := strings.TrimSpace(line); {
		case strings.HasPrefix(trimmed, "mutation "):
			return Resource, true
		case strings.HasPrefix(trimmed, "query "):
			return DataSource, true
		}
	}
	return 0, false
}

// parseResourceInput parses a resource document (create/upsert/delete mutations
// plus a single-result read query) into the IR. The struct fields, schema
// attributes and read-back all derive from the read query's selection; the
// create/upsert/delete operation names are taken verbatim from the mutations so
// the generated code calls the matching genqlient functions.
func parseResourceInput(lines []string, reg *schema.Registry) (InputGraphQLQuery, error) {
	objectName, required, varTypes, paths := collectFields(lines)
	queryName, readOp := queryNameAndOp(lines)
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

	createOp, upsertOp, deleteOp := operationNames(lines)
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
func parseDataSourceInput(lines []string, reg *schema.Registry) (InputGraphQLQuery, error) {
	objectName, required, varTypes, paths := collectFields(lines)
	queryName, readOp := queryNameAndOp(lines)
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

// collectFields walks the read query's selection set and returns the queried
// object kind, the filter/key variable (empty for an unfiltered list query),
// the GraphQL types of the query's declared variables, and every selected leaf
// field with its full relationship path. The walk uses an explicit prefix
// stack rather than string arithmetic, so relationship and attribute names that
// contain underscores are handled correctly.
func collectFields(lines []string) (objectName, required string, varTypes map[string]string, fields []fieldPath) {
	queryIdx := indexOfQueryLine(lines)
	if queryIdx == -1 {
		return "", "", nil, nil
	}
	varTypes = parseOperationVars(lines[queryIdx])

	var stack []string
	seenObject := false
	for _, raw := range lines[queryIdx+1:] {
		line := strings.TrimSpace(raw)
		switch {
		case line == "":
			continue
		case strings.HasSuffix(line, "{"):
			name := strings.TrimSpace(strings.TrimSuffix(line, "{"))
			if !seenObject {
				// The first block names the queried object and may carry the
				// filter variable; it is not part of any attribute's path.
				objectName, required = parseObjectLine(name)
				seenObject = true
				continue
			}
			stack = append(stack, name)
		case line == "}":
			if !seenObject {
				continue
			}
			if len(stack) == 0 {
				// Closing the object block ends the selection set.
				return objectName, required, varTypes, fields
			}
			stack = stack[:len(stack)-1]
		default:
			if !seenObject {
				continue
			}
			attr := strings.Fields(line)[0]
			parts := append(append([]string{}, stack...), attr)
			fields = append(fields, fieldPath{parts: parts})
		}
	}
	return objectName, required, varTypes, fields
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

// normalizeScalarBlocks collapses a multi-line scalar selection
//
//	fqdn {
//	  value
//	}
//
// into the single-line leaf form `fqdn`, so the brace-walk treats it as an
// attribute rather than a relationship prefix. A block qualifies only when it
// contains no nested block, which is exactly a scalar attribute's `{ value }`
// selection; relationship blocks (edges/node/…) always nest and are untouched.
func normalizeScalarBlocks(lines []string) []string {
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		name := strings.TrimSpace(strings.TrimSuffix(trimmed, "{"))
		if strings.HasSuffix(trimmed, "{") && name != "" {
			if end, ok := scalarBlockEnd(lines, i); ok {
				indent := lines[i][:len(lines[i])-len(strings.TrimLeft(lines[i], " \t"))]
				out = append(out, indent+name)
				i = end // skip the body and the closing brace
				continue
			}
		}
		out = append(out, lines[i])
	}
	return out
}

// scalarBlockEnd returns the index of the `}` closing the block opened at start,
// but only when the body contains no nested block; otherwise ok is false and
// the block is left intact. A nested block is any body line containing `{`,
// which covers both multi-line (`node {`) and single-line (`fqdn { value }`)
// sub-selections; a scalar selection's body is only bare meta fields (`value`).
func scalarBlockEnd(lines []string, start int) (int, bool) {
	for j := start + 1; j < len(lines); j++ {
		t := strings.TrimSpace(lines[j])
		switch {
		case strings.ContainsRune(t, '{'):
			return 0, false // nested block -> relationship, not a scalar
		case t == "}":
			return j, true
		}
	}
	return 0, false
}

// indexOfQueryLine returns the index of the line opening the read query, or -1.
func indexOfQueryLine(lines []string) int {
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "query ") {
			return i
		}
	}
	return -1
}

// queryNameAndOp returns the lowercased component name derived from the read
// query's operation name and the operation name itself (used to call the
// matching genqlient function).
func queryNameAndOp(lines []string) (queryName, readOp string) {
	idx := indexOfQueryLine(lines)
	if idx == -1 {
		return "", ""
	}
	op := operationName(lines[idx])
	if op == "" {
		return "", ""
	}
	return lcFirst(op), op
}

// operationNames extracts the create/upsert/delete mutation operation names as
// written in the document.
func operationNames(lines []string) (createOp, upsertOp, deleteOp string) {
	for _, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "mutation ") {
			continue
		}
		switch op := operationName(line); {
		case strings.HasSuffix(op, "Create"):
			createOp = op
		case strings.HasSuffix(op, "Upsert"):
			upsertOp = op
		case strings.HasSuffix(op, "Delete"):
			deleteOp = op
		}
	}
	return createOp, upsertOp, deleteOp
}

// operationName returns the operation name from a `query`/`mutation` line,
// stripping any argument list and trailing brace, e.g.
// `mutation DctVCenterCreate($data: …) {` -> "DctVCenterCreate".
func operationName(line string) string {
	parts := strings.Fields(strings.TrimSpace(line))
	if len(parts) < 2 {
		return ""
	}
	op := parts[1]
	if i := strings.IndexByte(op, '('); i != -1 {
		op = op[:i]
	}
	return strings.TrimRight(op, "({")
}

// parseObjectLine extracts the queried object kind and, when present, the
// filter/key variable from a query's object selector, e.g.
// `DctVCenter(vcenter_name__value: $vcenter_name)` -> ("DctVCenter", "vcenter_name").
// An unfiltered list selector like `Device` yields an empty key.
func parseObjectLine(s string) (objectName, required string) {
	open := strings.IndexByte(s, '(')
	if open == -1 {
		return strings.TrimSpace(s), ""
	}
	return strings.TrimSpace(s[:open]), variableName(s[open:])
}

// parseOperationVars parses a query operation line's variable declarations,
// e.g. `query DctThingByName($asn: BigInt!, $x: String!)`, into a map of
// variable name to its GraphQL type (the leading scalar name, e.g. "BigInt").
func parseOperationVars(line string) map[string]string {
	open := strings.IndexByte(line, '(')
	closeIdx := strings.LastIndexByte(line, ')')
	if open == -1 || closeIdx <= open {
		return nil
	}
	vars := map[string]string{}
	for _, decl := range strings.Split(line[open+1:closeIdx], ",") {
		decl = strings.TrimSpace(decl)
		colon := strings.IndexByte(decl, ':')
		if colon == -1 || !strings.HasPrefix(decl, "$") {
			continue
		}
		name := scanIdentifier(decl[1:])
		typ := scanIdentifier(strings.TrimLeft(decl[colon+1:], " "))
		if name != "" && typ != "" {
			vars[name] = typ
		}
	}
	return vars
}

// variableName returns the identifier following the first '$' in s, or "".
func variableName(s string) string {
	i := strings.IndexByte(s, '$')
	if i == -1 {
		return ""
	}
	return scanIdentifier(s[i+1:])
}

// scanIdentifier returns the leading run of GraphQL identifier characters
// (letters, digits, underscore) of s.
func scanIdentifier(s string) string {
	for i, r := range s {
		isIdent := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !isIdent {
			return s[:i]
		}
	}
	return s
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
