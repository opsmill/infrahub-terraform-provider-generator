package parser

// typeClass is the normalized Terraform/SDK type family an Infrahub attribute
// maps to. Only Number and Boolean/Checkbox diverge from string.
type typeClass int

const (
	classString typeClass = iota
	classInt64
	classBool
)

// typeInfo holds every code fragment the templates need for one type class: the
// Terraform value type and schema attribute, the SDK input wrappers, and the
// accessor/constructor used to write and read the attribute value.
type typeInfo struct {
	tfType        string
	tfAttr        string
	sdkCreate     string
	sdkUpdate     string
	writeAccessor string
	readCtor      string
}

// classInfo maps each type class to its fragments. Adding support for a new
// type class is one row here rather than an edit across six functions.
var classInfo = map[typeClass]typeInfo{
	classString: {"types.String", "schema.StringAttribute", "infrahub_sdk.TextAttributeCreate", "infrahub_sdk.TextAttributeUpdate", "ValueString()", "types.StringValue"},
	classInt64:  {"types.Int64", "schema.Int64Attribute", "infrahub_sdk.NumberAttributeCreate", "infrahub_sdk.NumberAttributeUpdate", "ValueInt64()", "types.Int64Value"},
	classBool:   {"types.Bool", "schema.BoolAttribute", "infrahub_sdk.CheckboxAttributeCreate", "infrahub_sdk.CheckboxAttributeUpdate", "ValueBool()", "types.BoolValue"},
}

// classOf maps an Infrahub AttributeKind to its type class. Unknown and
// not-yet-supported kinds (JSON, List, NumberPool, "") fall back to string so
// generation never breaks.
func classOf(f GenqlientField) typeClass {
	switch f.Kind {
	case "Number":
		return classInt64
	case "Boolean", "Checkbox":
		return classBool
	default:
		return classString
	}
}

// infoFor returns the typeInfo for a field's class. classOf only ever returns a
// key present in classInfo, so the lookup always hits.
func infoFor(f GenqlientField) typeInfo { return classInfo[classOf(f)] }

// The helpers below are registered as template functions; each returns one
// fragment of the field's typeInfo.

// tfType is the terraform-plugin-framework value type for the struct field.
func tfType(f GenqlientField) string { return infoFor(f).tfType }

// tfAttr is the schema attribute type used in the Schema() block.
func tfAttr(f GenqlientField) string { return infoFor(f).tfAttr }

// sdkCreate is the Infrahub SDK input wrapper used on create.
func sdkCreate(f GenqlientField) string { return infoFor(f).sdkCreate }

// sdkUpdate is the Infrahub SDK input wrapper used on update/upsert.
func sdkUpdate(f GenqlientField) string { return infoFor(f).sdkUpdate }

// writeAccessor is the typed getter (with call parens) used to read the value
// out of the plan/state.
func writeAccessor(f GenqlientField) string { return infoFor(f).writeAccessor }

// readCtor is the types.* constructor used to wrap an SDK response value back
// into a framework value. With BigInt bound to int64 in the SDK, the Number
// response value is already int64, so no conversion is needed.
func readCtor(f GenqlientField) string { return infoFor(f).readCtor }
