// Package schema fetches an Infrahub schema and answers attribute-type
// lookups used to generate type-correct Terraform provider code.
package schema

// Attribute is the subset of an Infrahub attribute schema the generator needs.
type Attribute struct {
	Kind     string // Infrahub AttributeKind, e.g. "Text", "Number", "Boolean".
	Optional bool   // true when the attribute may be omitted on create.
}

// Registry maps a node kind (namespace+name, e.g. "DctVCenter") and attribute
// name to its schema type information.
type Registry struct {
	nodes map[string]map[string]Attribute
}

// Attribute returns the schema info for an attribute, or ok=false when the node
// or attribute is unknown. A nil *Registry always returns ok=false, so callers
// can stay branch-free when no schema was fetched.
func (r *Registry) Attribute(nodeKind, attrName string) (Attribute, bool) {
	if r == nil {
		return Attribute{}, false
	}
	attrs, ok := r.nodes[nodeKind]
	if !ok {
		return Attribute{}, false
	}
	a, ok := attrs[attrName]
	return a, ok
}
