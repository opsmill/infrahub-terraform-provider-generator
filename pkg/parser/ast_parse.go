package parser

import (
	"fmt"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
)

// readQuery returns the document's read-query operation (the single `query`
// operation), or nil when the document has none.
func readQuery(doc *ast.QueryDocument) *ast.OperationDefinition {
	for _, op := range doc.Operations {
		if op.Operation == ast.Query {
			return op
		}
	}
	return nil
}

// detectResourceType classifies a parsed document by its first operation: a
// mutation makes it a resource, a query makes it a data source.
func detectResourceType(doc *ast.QueryDocument) (ResourceType, bool) {
	for _, op := range doc.Operations {
		switch op.Operation {
		case ast.Mutation:
			return Resource, true
		case ast.Query:
			return DataSource, true
		}
	}
	return 0, false
}

// queryNameAndOp returns the lowercased component name derived from the read
// query's operation name and the operation name itself (used to call the
// matching genqlient function).
func queryNameAndOp(doc *ast.QueryDocument) (queryName, readOp string) {
	op := readQuery(doc)
	if op == nil || op.Name == "" {
		return "", ""
	}
	return lcFirst(op.Name), op.Name
}

// operationNames extracts the create/upsert/delete mutation operation names by
// the Infrahub <Kind><Op> suffix convention, taken verbatim from the document.
func operationNames(doc *ast.QueryDocument) (createOp, upsertOp, deleteOp string) {
	for _, op := range doc.Operations {
		if op.Operation != ast.Mutation {
			continue
		}
		switch {
		case strings.HasSuffix(op.Name, "Create"):
			createOp = op.Name
		case strings.HasSuffix(op.Name, "Upsert"):
			upsertOp = op.Name
		case strings.HasSuffix(op.Name, "Delete"):
			deleteOp = op.Name
		}
	}
	return createOp, upsertOp, deleteOp
}

// variableTypes maps each variable declared by the operation to its leading
// named GraphQL type (e.g. `$vcenter_name: String!` -> "String"), which is what
// determines the generated SDK function's parameter type. List and other
// non-named types have an empty NamedType and are skipped.
func variableTypes(op *ast.OperationDefinition) map[string]string {
	if op == nil || len(op.VariableDefinitions) == 0 {
		return nil
	}
	m := make(map[string]string, len(op.VariableDefinitions))
	for _, v := range op.VariableDefinitions {
		if v.Type != nil && v.Type.NamedType != "" {
			m[v.Variable] = v.Type.NamedType
		}
	}
	return m
}

// keyVariable returns the name of the first filter/key variable referenced in
// the object selector's arguments (e.g. `DctVCenter(vcenter_name__value:
// $vcenter_name)` -> "vcenter_name"), or "" for an unfiltered list query.
func keyVariable(f *ast.Field) string {
	for _, arg := range f.Arguments {
		if arg.Value != nil && arg.Value.Kind == ast.Variable {
			return arg.Value.Raw
		}
	}
	return ""
}

// collectFields walks the read query's selection set and returns the queried
// object kind, the filter/key variable (empty for an unfiltered list query),
// the GraphQL types of the query's declared variables, and every selected leaf
// field with its full relationship path. Named fragment spreads are inlined;
// inline fragments are rejected. The first top-level field names the queried
// object and is not part of any attribute's path.
func collectFields(doc *ast.QueryDocument) (objectName, required string, varTypes map[string]string, fields []fieldPath, err error) {
	op := readQuery(doc)
	if op == nil || len(op.SelectionSet) == 0 {
		return "", "", nil, nil, nil
	}
	varTypes = variableTypes(op)

	objField, ok := op.SelectionSet[0].(*ast.Field)
	if !ok {
		return "", "", varTypes, nil, fmt.Errorf("parsing GraphQL query %q: expected an object selection, got %T", op.Name, op.SelectionSet[0])
	}
	if len(op.Directives) > 0 {
		return "", "", varTypes, nil, fmt.Errorf("parsing GraphQL query %q: operation directive @%s is unsupported; remove it", op.Name, op.Directives[0].Name)
	}
	if len(objField.Directives) > 0 {
		return "", "", varTypes, nil, fmt.Errorf("parsing GraphQL query %q: directive @%s on %q is unsupported; remove it", op.Name, objField.Directives[0].Name, objField.Name)
	}
	objectName = objField.Name
	required = keyVariable(objField)

	fields, err = walkSelection(objField.SelectionSet, nil, doc)
	if err != nil {
		return "", "", varTypes, nil, err
	}
	return objectName, required, varTypes, fields, nil
}

// walkSelection recursively flattens a selection set into leaf field paths,
// carrying the relationship-prefix stack (e.g. ["edges", "node"]). A field with
// no sub-selection, or whose sub-selection is purely scalar meta fields
// (`{ value }`), is a leaf attribute; any other field is a relationship frame
// pushed onto the stack. Named fragment spreads are inlined at the current
// depth; inline fragments are unsupported.
func walkSelection(set ast.SelectionSet, stack []string, doc *ast.QueryDocument) ([]fieldPath, error) {
	var out []fieldPath
	for _, sel := range set {
		switch s := sel.(type) {
		case *ast.Field:
			if s.Alias != "" && s.Alias != s.Name {
				return nil, fmt.Errorf("parsing GraphQL query: alias %q on field %q is unsupported; remove the alias", s.Alias, s.Name)
			}
			if len(s.Directives) > 0 {
				return nil, fmt.Errorf("parsing GraphQL query: directive @%s on field %q is unsupported; remove it", s.Directives[0].Name, s.Name)
			}
			if len(s.SelectionSet) == 0 || isScalarSelection(s.SelectionSet, doc) {
				out = append(out, fieldPath{parts: appendPart(stack, s.Name)})
				continue
			}
			sub, err := walkSelection(s.SelectionSet, appendPart(stack, s.Name), doc)
			if err != nil {
				return nil, err
			}
			out = append(out, sub...)
		case *ast.FragmentSpread:
			frag := doc.Fragments.ForName(s.Name)
			if frag == nil {
				return nil, fmt.Errorf("parsing GraphQL query: fragment %q is referenced but not defined", s.Name)
			}
			sub, err := walkSelection(frag.SelectionSet, stack, doc)
			if err != nil {
				return nil, err
			}
			out = append(out, sub...)
		case *ast.InlineFragment:
			return nil, fmt.Errorf("parsing GraphQL query: inline fragment (... on %s) is unsupported; expand its fields inline", s.TypeCondition)
		}
	}
	return out, nil
}

// isScalarSelection reports whether every member of set (resolving named
// fragment spreads) is a leaf field with no sub-selection — i.e. an attribute's
// scalar `{ value }` meta-selection rather than a relationship block.
func isScalarSelection(set ast.SelectionSet, doc *ast.QueryDocument) bool {
	for _, sel := range set {
		switch s := sel.(type) {
		case *ast.Field:
			if len(s.SelectionSet) != 0 {
				return false
			}
		case *ast.FragmentSpread:
			frag := doc.Fragments.ForName(s.Name)
			if frag == nil || !isScalarSelection(frag.SelectionSet, doc) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// appendPart returns a new slice with name appended, never aliasing stack's
// backing array, so sibling recursions cannot corrupt each other's prefix.
func appendPart(stack []string, name string) []string {
	parts := make([]string, len(stack)+1)
	copy(parts, stack)
	parts[len(stack)] = name
	return parts
}
