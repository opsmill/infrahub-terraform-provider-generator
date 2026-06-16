package parser

// typeClass is the normalized Terraform/SDK type family an Infrahub attribute
// maps to. Only Number and Boolean/Checkbox diverge from string.
type typeClass int

const (
	classString typeClass = iota
	classInt64
	classBool
)

// classOf maps an Infrahub AttributeKind to its type class. Unknown and
// not-yet-supported kinds (JSON, List, NumberPool, "") fall back to string so
// generation never breaks; callers warn separately for the lossy ones.
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

// tfType is the terraform-plugin-framework value type for the resource struct
// field (e.g. "types.Int64").
func tfType(f GenqlientField) string {
	switch classOf(f) {
	case classInt64:
		return "types.Int64"
	case classBool:
		return "types.Bool"
	default:
		return "types.String"
	}
}

// tfAttr is the schema attribute type used in the Schema() block.
func tfAttr(f GenqlientField) string {
	switch classOf(f) {
	case classInt64:
		return "schema.Int64Attribute"
	case classBool:
		return "schema.BoolAttribute"
	default:
		return "schema.StringAttribute"
	}
}

// sdkCreate is the Infrahub SDK input wrapper used on create.
func sdkCreate(f GenqlientField) string {
	switch classOf(f) {
	case classInt64:
		return "infrahub_sdk.NumberAttributeCreate"
	case classBool:
		return "infrahub_sdk.CheckboxAttributeCreate"
	default:
		return "infrahub_sdk.TextAttributeCreate"
	}
}

// sdkUpdate is the Infrahub SDK input wrapper used on update/upsert.
func sdkUpdate(f GenqlientField) string {
	switch classOf(f) {
	case classInt64:
		return "infrahub_sdk.NumberAttributeUpdate"
	case classBool:
		return "infrahub_sdk.CheckboxAttributeUpdate"
	default:
		return "infrahub_sdk.TextAttributeUpdate"
	}
}

// writeAccessor is the typed getter on a types.* value, including the call
// parens, used to read the value out of the plan/state.
func writeAccessor(f GenqlientField) string {
	switch classOf(f) {
	case classInt64:
		return "ValueInt64()"
	case classBool:
		return "ValueBool()"
	default:
		return "ValueString()"
	}
}

// readCtor is the types.* constructor used to wrap an SDK response value back
// into a framework value. With BigInt bound to int64 in the SDK, the Number
// response value is already int64, so no conversion is needed.
func readCtor(f GenqlientField) string {
	switch classOf(f) {
	case classInt64:
		return "types.Int64Value"
	case classBool:
		return "types.BoolValue"
	default:
		return "types.StringValue"
	}
}
