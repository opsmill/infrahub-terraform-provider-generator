package parser

import (
	"fmt"
	"strings"

	"github.com/opsmill/infrahub-terraform-provider-generator/pkg/schema"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

func parseGraphQLQuery(query string, reg *schema.Registry) (*InputGraphQLQuery, error) {
	var resourceType ResourceType
	var result InputGraphQLQuery
	var err error
	lines := strings.Split(query, "\n")

	for _, line := range lines {
		if strings.Contains(line, "mutation") {
			resourceType = Resource
			break
		} else if strings.Contains(line, "query") {
			resourceType = DataSource
			break
		}
	}

	if resourceType == DataSource {
		result, err = parseDataSourceInput(lines)
		result.ResourceType = DataSource
	} else if resourceType == Resource {
		result, err = parseResourceInput(lines, reg)
		result.ResourceType = Resource
	}

	if err != nil {
		return nil, err
	}

	return &result, nil
}

func parseResourceInput(lines []string, reg *schema.Registry) (InputGraphQLQuery, error) {
	var queryName, required, objectName, parentPrefix string
	var inBlock bool
	var prefixList, prefixListImmutable []string
	var fields []Field
	var genqlientFields, genqlientFieldsModify, genqlientFieldsReadOnly []GenqlientField

	// Capture the actual operation names as written in the .gql so the generated
	// code calls the matching genqlient functions. genqlient names each generated
	// function after the GraphQL operation, so the query/mutation operation names
	// (not the query alias or the object kind) are what must be used.
	var readOp, createOp, upsertOp, deleteOp string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		isQuery := strings.HasPrefix(trimmed, "query ")
		isMutation := strings.HasPrefix(trimmed, "mutation ")
		if !isQuery && !isMutation {
			continue
		}
		parts := strings.Fields(trimmed)
		if len(parts) < 2 {
			continue
		}
		opName := parts[1]
		if i := strings.IndexByte(opName, '('); i != -1 {
			opName = opName[:i]
		}
		opName = strings.TrimRight(strings.TrimSpace(opName), "{(")
		if opName == "" {
			continue
		}
		switch {
		case isQuery:
			readOp = opName
		case strings.HasSuffix(opName, "Create"):
			createOp = opName
		case strings.HasSuffix(opName, "Upsert"):
			upsertOp = opName
		case strings.HasSuffix(opName, "Delete"):
			deleteOp = opName
		}
	}

	index := 0
	for number, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "query ") {
			parts := strings.Fields(line)
			if len(parts) > 1 {
				containsBracket := strings.IndexByte(parts[1], '(')
				if containsBracket != -1 {
					queryName = parts[1][:containsBracket]
				} else {
					queryName = parts[1]
				}
				queryName = strings.ToLower(string(queryName[0])) + queryName[1:]
			}
			index = number
		} else if index != 0 && number == index+1 {
			// This identifies the required field (e.g., name__value: $device_name)
			if strings.Contains(line, ":") {
				parts := strings.Split(line, ":")
				required = parts[1][strings.Index(parts[1], "$")+1 : strings.Index(parts[1][strings.Index(parts[1], "$"):], " ")+strings.Index(parts[1], "$")]
				required = strings.TrimRight(required, ")")
				objectNameParts := strings.Split(parts[0], "(")
				objectName = objectNameParts[0]
			} else {
				parts := strings.Split(line, " ")
				objectName = parts[0]
			}
		}
	}

	for number, line := range lines[index:] {

		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "query ") || number == 1 {
		} else if strings.HasSuffix(line, " {") {
			inBlock = true
			prefix := line[:len(line)-2]
			prefixList = append(prefixList, prefix)
			if strings.Contains(prefix, "_") {
				prefixListImmutable = append(prefixListImmutable, prefix)
			}
			parentPrefix = parentPrefix + prefix + "_"
		} else if line == "}" {
			inBlock = false
			if strings.Count(parentPrefix, "_") < 2 {
				parentPrefix = ""
				break
			}
			// remove last _ and length of last prefix added, workaround for underscores in schema
			parentPrefix = parentPrefix[:len(parentPrefix)-1-len(prefixList[len(prefixList)-1])]
			prefixList = prefixList[:len(prefixList)-1]
		} else if inBlock {
			parts := strings.Fields(line)
			if len(parts) > 0 {
				fields = append(fields, Field{
					Name: parentPrefix + strings.TrimSpace(parts[0]),
					Type: "String",
				})
				if strings.Contains(parts[0], "_") {
					prefixListImmutable = append(prefixListImmutable, parts[0])
				}
			}
		}
	}

	customSplit := func(str string, exceptions []string) []string {
		var result []string
		var currentWord string

		for _, char := range str {
			if char == '_' {
				isException := false
				for _, exception := range exceptions {
					if strings.HasPrefix(exception, currentWord) {
						if len(currentWord) == len(exception) {
							break
						}
						isException = true
						break
					}
				}
				if !isException {
					result = append(result, currentWord)
					currentWord = ""
				} else {
					currentWord += string(char)
				}
			} else {
				currentWord += string(char)
			}
		}
		result = append(result, currentWord)
		return result
	}

	for _, entry := range fields {
		parts := customSplit(entry.Name, prefixListImmutable)

		// Capitalize each part except for the first one
		caser := cases.Title(language.English)
		var filtered, noPrefix, plain []string

		for i := range parts {
			// Capitalize the first letter of each part
			parts[i] = caser.String(parts[i])
			plain = append(plain, parts[i])
			if parts[i] != "Edges" && parts[i] != "Node" {
				noPrefix = append(noPrefix, parts[i])
				filtered = append(filtered, parts[i])
			}
			if required != "" {
				if parts[i] == "Edges" {
					parts[i] = "Edges[0]"
				}
			} else {
				if parts[i] == "Edges" {
					parts[i] = "Edges[i]"
				}
			}
		}

		for _, x := range [][]string{parts, noPrefix, plain} {
			if len(x) > 0 && x[len(x)-1] == "Id" {
				x[len(x)-1] = "GetId()"
			}
		}

		// Scalar attributes are selected as `attr { value }`, so genqlient nests
		// the string under a `.Value` field. The id is read through GetId() and
		// must not get a `.Value` suffix.
		valueSuffix := ".Value"
		if len(plain) > 0 && plain[len(plain)-1] == "GetId()" {
			valueSuffix = ""
		}

		newField := GenqlientField{
			Field: Field{
				Name: entry.Name,
				Type: entry.Type,
			},
			Query:                  objectName + "." + strings.Join(parts, ".") + valueSuffix,
			QueryNoPrefixReplaceId: strings.Join(noPrefix, "."),
			InputObjectNames:       strings.Join(filtered, "."),
			PlainObject:            strings.Join(plain[2:], ".") + valueSuffix,
		}

		// Stamp schema-derived type info. objectName is the node kind
		// (namespace+name); the attribute name is the field name without the
		// edges_node_ prefix. A nil registry or a miss leaves Kind="" (String)
		// and Optional=true, reproducing the untyped default.
		newField.Optional = true
		if attr, ok := reg.Attribute(objectName, humanReadableName(newField.Name)); ok {
			newField.Kind = attr.Kind
			newField.Optional = attr.Optional
		}

		// Only fields read through GetId() (the node's own server-assigned UUID,
		// and any related node's id) are read-only; they carry no `.Value`
		// suffix. Every other selected scalar ends in `.Value` and is a real
		// attribute the user can set, so it must be configurable. The earlier
		// heuristic counted the substring "id" in the access path and wrongly
		// forced any attribute whose name merely contained "id" (id_projet,
		// vlan_id, …) to be read-only.
		if valueSuffix == "" {
			genqlientFieldsReadOnly = append(genqlientFieldsReadOnly, newField)
		} else {
			genqlientFieldsModify = append(genqlientFieldsModify, newField)
		}
		genqlientFields = append(genqlientFields, newField)
	}

	if queryName == "" {
		return InputGraphQLQuery{}, fmt.Errorf("failed to parse GraphQL query: missing query name")
	}

	addHumanReadableField(genqlientFields)
	addHumanReadableField(genqlientFieldsReadOnly)
	addHumanReadableField(genqlientFieldsModify)

	// Fall back to the Infrahub naming convention (<Kind><Op>) when an operation
	// name could not be read from the .gql.
	if readOp == "" && queryName != "" {
		readOp = strings.ToUpper(queryName[:1]) + queryName[1:]
	}
	if createOp == "" {
		createOp = objectName + "Create"
	}
	if upsertOp == "" {
		upsertOp = objectName + "Upsert"
	}
	if deleteOp == "" {
		deleteOp = objectName + "Delete"
	}

	return InputGraphQLQuery{
		QueryName:               queryName,
		ObjectName:              objectName,
		Required:                required,
		ReadOp:                  readOp,
		CreateOp:                createOp,
		UpsertOp:                upsertOp,
		DeleteOp:                deleteOp,
		GenqlientFields:         genqlientFields,
		genqlientFieldsReadOnly: genqlientFieldsReadOnly,
		genqlientFieldsModify:   genqlientFieldsModify,
	}, nil
}

// parseDataSourceInput parses a read query into a data source. Data source
// fields are read-only and rendered as strings, so it takes no schema registry;
// add one here if typed data-source reads are ever needed.
func parseDataSourceInput(lines []string) (InputGraphQLQuery, error) {
	var queryName, required, objectName, parentPrefix, readOp string
	var fields []Field
	var genqlientFields []GenqlientField
	var inBlock bool
	var prefixList, prefixListImmutable []string

	for number, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "query ") {
			parts := strings.Fields(line)
			if len(parts) > 1 {
				containsBracket := strings.IndexByte(parts[1], '(')
				if containsBracket != -1 {
					queryName = parts[1][:containsBracket]
				} else {
					queryName = parts[1]
				}
				// Keep the operation name as written so the generated code
				// calls the matching genqlient function; genqlient names each
				// function after the GraphQL operation, not the lowercased alias.
				readOp = queryName
				queryName = strings.ToLower(string(queryName[0])) + queryName[1:]
			}
		} else if number == 1 {
			// This identifies the required field (e.g., name__value: $device_name)
			if strings.Contains(line, ":") {
				parts := strings.Split(line, ":")
				required = parts[1][strings.Index(parts[1], "$")+1 : strings.Index(parts[1][strings.Index(parts[1], "$"):], " ")+strings.Index(parts[1], "$")]
				required = strings.TrimRight(required, ")")
				objectNameParts := strings.Split(parts[0], "(")
				objectName = objectNameParts[0]
			} else {
				parts := strings.Split(line, " ")
				objectName = parts[0]
			}
		} else if strings.HasSuffix(line, " {") {
			inBlock = true
			prefix := line[:len(line)-2]
			prefixList = append(prefixList, prefix)
			if strings.Contains(prefix, "_") {
				prefixListImmutable = append(prefixListImmutable, prefix)
			}
			parentPrefix = parentPrefix + prefix + "_"
		} else if line == "}" {
			inBlock = false
			if strings.Count(parentPrefix, "_") < 2 {
				parentPrefix = ""
				break
			}
			// remove last _ and length of last prefix added, workaround for underscores in schema
			parentPrefix = parentPrefix[:len(parentPrefix)-1-len(prefixList[len(prefixList)-1])]
			prefixList = prefixList[:len(prefixList)-1]
		} else if inBlock {
			parts := strings.Fields(line)
			if len(parts) > 0 {
				fields = append(fields, Field{
					Name: parentPrefix + strings.TrimSpace(parts[0]),
					Type: "String",
				})
				if strings.Contains(parts[0], "_") {
					prefixListImmutable = append(prefixListImmutable, parts[0])
				}
			}
		}
	}

	customSplit := func(str string, exceptions []string) []string {
		var result []string
		var currentWord string

		for _, char := range str {
			if char == '_' {
				isException := false
				for _, exception := range exceptions {
					if strings.HasPrefix(exception, currentWord) {
						if len(currentWord) == len(exception) {
							break
						}
						isException = true
						break
					}
				}
				if !isException {
					result = append(result, currentWord)
					currentWord = ""
				} else {
					currentWord += string(char)
				}
			} else {
				currentWord += string(char)
			}
		}
		result = append(result, currentWord)
		return result
	}

	for _, entry := range fields {
		parts := customSplit(entry.Name, prefixListImmutable)

		// Capitalize each part except for the first one
		caser := cases.Title(language.English)
		for i := range parts {
			// Capitalize the first letter of each part
			parts[i] = caser.String(parts[i])
			if required != "" {
				if parts[i] == "Edges" {
					parts[i] = "Edges[0]"
				}
			} else {
				if parts[i] == "Edges" {
					parts[i] = "Edges[i]"
				}
			}
		}

		// Join the parts using a dot separator
		genqlientFields = append(genqlientFields, GenqlientField{
			Field: Field{
				Name: entry.Name,
				Type: entry.Type,
			},
			Query: objectName + "." + strings.Join(parts, "."),
		})
	}

	if queryName == "" {
		return InputGraphQLQuery{}, fmt.Errorf("failed to parse GraphQL query: missing query name")
	}

	addHumanReadableField(genqlientFields)

	// Fall back to the Infrahub naming convention when the operation name could
	// not be read from the .gql.
	if readOp == "" && queryName != "" {
		readOp = strings.ToUpper(queryName[:1]) + queryName[1:]
	}

	return InputGraphQLQuery{
		QueryName:       queryName,
		ObjectName:      objectName,
		Required:        required,
		ReadOp:          readOp,
		GenqlientFields: genqlientFields,
	}, nil
}

// humanReadableName is the attribute name as exposed in Terraform: the parsed
// field name with the GraphQL edges_node_ prefix removed. It is also the key
// used to look the attribute up in the schema registry.
func humanReadableName(fieldName string) string {
	return strings.ReplaceAll(fieldName, "edges_node_", "")
}

func addHumanReadableField(fields []GenqlientField) {

	for i, field := range fields {
		fields[i].HumanReadableName = humanReadableName(field.Name)
	}
}
