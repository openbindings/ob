// Package codegen generates typed client code from OpenBindings interfaces.
package codegen

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// TypeKind classifies a TypeRef.
type TypeKind int

const (
	KindPrimitive TypeKind = iota
	KindNamed
	KindArray
	KindMap
	KindUnion
	KindUnknown
)

// TypeRef is a language-neutral reference to a type.
type TypeRef struct {
	Kind      TypeKind
	Name      string    // KindNamed: the type name (e.g. "MenuItem")
	Primitive string    // KindPrimitive: "string", "number", "integer", "boolean"
	Items     *TypeRef  // KindArray: element type
	Values    *TypeRef  // KindMap: value type (key always string)
	Variants  []TypeRef // KindUnion: oneOf/anyOf variants
	Enum      []any     // for enums (string, integer, or mixed)
	Nullable  bool
	Const     any       // for const values (discriminated union narrowing)
}

// TypeDef is a named object type (struct / interface).
type TypeDef struct {
	Name        string
	Description string
	Fields      []Field
}

// Field is a single property in a TypeDef.
type Field struct {
	Name        string  // PascalCase (Go) / camelCase (TS) — language-neutral PascalCase stored here
	JSONName    string  // original JSON property name
	Type        TypeRef
	Required    bool
	Description string
	Default     any     // JSON Schema default value, for doc comments
}

// OperationSig describes one operation for code generation.
type OperationSig struct {
	Key         string
	Description string
	Deprecated  bool
	Input       *TypeRef // nil = no input
	Output      *TypeRef // nil = unknown output
	Tags        []string
}

// CodegenResult is everything an emitter needs to produce a typed client.
type CodegenResult struct {
	InterfaceName string
	Description   string
	Types         []TypeDef
	Operations    []OperationSig
	RawOBI        []byte // minified contract JSON (operations + schemas only)
}

// ---------- JSON Schema → IR conversion ----------

// schemaConverter converts JSON Schema (map[string]any) into IR types.
type schemaConverter struct {
	// root is the containing OBI document for resolving #/schemas/Foo refs.
	root map[string]any
	// registry tracks already-converted $ref targets by ref string.
	registry map[string]*TypeDef
	// converting tracks refs currently being converted (cycle detection).
	converting map[string]bool
	// types collects all generated TypeDefs.
	types []TypeDef
}

func newSchemaConverter(root map[string]any) *schemaConverter {
	return &schemaConverter{
		root:       root,
		registry:   make(map[string]*TypeDef),
		converting: make(map[string]bool),
	}
}

// convert turns a JSON Schema into a TypeRef, generating TypeDefs as needed.
// path is used for naming anonymous inline objects (e.g. "PlaceOrderInput").
func (c *schemaConverter) convert(schema map[string]any, path string) TypeRef {
	if schema == nil || len(schema) == 0 {
		return TypeRef{Kind: KindUnknown}
	}

	// Handle $ref first.
	if ref, ok := schema["$ref"].(string); ok {
		return c.resolveRef(ref)
	}

	// Handle const.
	if cv, ok := schema["const"]; ok {
		ref := c.convertWithoutConst(schema, path)
		ref.Const = cv
		return ref
	}

	return c.convertWithoutConst(schema, path)
}

func (c *schemaConverter) convertWithoutConst(schema map[string]any, path string) TypeRef {
	// Handle allOf — flatten then convert.
	if allOf, ok := schemaSlice(schema, "allOf"); ok && len(allOf) > 0 {
		merged := c.flattenAllOf(allOf, path)
		return c.convert(merged, path)
	}

	// Handle oneOf / anyOf → Union, but only when the variants define structural
	// types. If the variants are purely constraint-based (e.g., oneOf with only
	// "required" fields to express "location OR content"), and the parent schema
	// has properties or a type, skip the union and let the parent schema drive.
	if variants, ok := schemaSlice(schema, "oneOf"); ok && len(variants) > 0 {
		if !isConstraintOnlyVariants(variants) || !hasStructuralType(schema) {
			return c.convertUnion(variants, path)
		}
	}
	if variants, ok := schemaSlice(schema, "anyOf"); ok && len(variants) > 0 {
		if !isConstraintOnlyVariants(variants) || !hasStructuralType(schema) {
			return c.convertUnion(variants, path)
		}
	}

	// Determine type.
	typ := schemaType(schema)

	// Handle nullable (type array like ["string", "null"] or nullable: true).
	nullable := false
	if types, ok := schema["type"].([]any); ok {
		filtered := make([]any, 0, len(types))
		for _, t := range types {
			if s, ok := t.(string); ok && s == "null" {
				nullable = true
			} else {
				filtered = append(filtered, t)
			}
		}
		if len(filtered) == 1 {
			if s, ok := filtered[0].(string); ok {
				typ = s
			}
		}
	}
	if nb, ok := schema["nullable"].(bool); ok && nb {
		nullable = true
	}

	// Handle enum.
	var enumVals []any
	if ev, ok := schema["enum"].([]any); ok {
		enumVals = ev
	}

	switch typ {
	case "string":
		ref := TypeRef{Kind: KindPrimitive, Primitive: "string", Nullable: nullable, Enum: enumVals}
		return ref
	case "number":
		return TypeRef{Kind: KindPrimitive, Primitive: "number", Nullable: nullable, Enum: enumVals}
	case "integer":
		return TypeRef{Kind: KindPrimitive, Primitive: "integer", Nullable: nullable, Enum: enumVals}
	case "boolean":
		return TypeRef{Kind: KindPrimitive, Primitive: "boolean", Nullable: nullable}
	case "array":
		itemRef := TypeRef{Kind: KindUnknown}
		if items, ok := schema["items"].(map[string]any); ok {
			itemRef = c.convert(items, path+"Item")
		}
		return TypeRef{Kind: KindArray, Items: &itemRef, Nullable: nullable}
	case "object":
		return c.convertObject(schema, path, nullable)
	default:
		// No type specified — check if it has properties (implicit object).
		if _, hasProps := schema["properties"]; hasProps {
			return c.convertObject(schema, path, nullable)
		}
		return TypeRef{Kind: KindUnknown, Nullable: nullable}
	}
}

func (c *schemaConverter) convertObject(schema map[string]any, path string, nullable bool) TypeRef {
	props, hasProps := schema["properties"].(map[string]any)

	// Map type: {"type": "object", "additionalProperties": <schema>}
	if !hasProps {
		if ap, ok := schema["additionalProperties"].(map[string]any); ok {
			valRef := c.convert(ap, path+"Value")
			return TypeRef{Kind: KindMap, Values: &valRef, Nullable: nullable}
		}
		// Plain object with no properties — unknown.
		return TypeRef{Kind: KindUnknown, Nullable: nullable}
	}

	// Named object type.
	name := path
	if name == "" {
		name = "AnonymousObject"
	}

	reqSet := requiredSet(schema)
	var fields []Field
	// Sort property keys for deterministic output.
	propKeys := sortedKeys(props)
	for _, key := range propKeys {
		propSchema, ok := props[key].(map[string]any)
		if !ok {
			continue
		}
		fieldPath := path + toPascalCase(key)
		fieldType := c.convert(propSchema, fieldPath)
		f := Field{
			Name:     toPascalCase(key),
			JSONName: key,
			Type:     fieldType,
			Required: reqSet[key],
		}
		if desc, ok := propSchema["description"].(string); ok {
			f.Description = desc
		}
		if def, ok := propSchema["default"]; ok {
			f.Default = def
		}
		fields = append(fields, f)
	}

	td := TypeDef{
		Name:   name,
		Fields: fields,
	}
	if desc, ok := schema["description"].(string); ok {
		td.Description = desc
	}
	c.types = append(c.types, td)

	return TypeRef{Kind: KindNamed, Name: name, Nullable: nullable}
}

func (c *schemaConverter) convertUnion(variants []map[string]any, path string) TypeRef {
	var refs []TypeRef
	for i, v := range variants {
		variantPath := fmt.Sprintf("%sVariant%d", path, i)
		refs = append(refs, c.convert(v, variantPath))
	}
	return TypeRef{Kind: KindUnion, Variants: refs}
}

// resolveRef resolves a $ref string (e.g. "#/schemas/Foo") into a TypeRef.
func (c *schemaConverter) resolveRef(ref string) TypeRef {
	// Already converted?
	if td, ok := c.registry[ref]; ok {
		return TypeRef{Kind: KindNamed, Name: td.Name}
	}

	// Cycle detection.
	if c.converting[ref] {
		// Return a forward reference — the name is derived from the ref.
		name := refName(ref)
		return TypeRef{Kind: KindNamed, Name: name}
	}

	// Resolve the target schema from the root document.
	target := resolveJSONPointer(c.root, ref)
	if target == nil {
		return TypeRef{Kind: KindUnknown}
	}

	name := refName(ref)

	// Place a placeholder in the registry to break cycles.
	placeholder := &TypeDef{Name: name}
	c.registry[ref] = placeholder
	c.converting[ref] = true

	// Convert the target schema.
	result := c.convert(target, name)
	delete(c.converting, ref)

	// If the conversion produced a Named type pointing to a TypeDef, update
	// the placeholder so the registry entry is the real definition.
	if result.Kind == KindNamed {
		// Find the TypeDef that was generated and update the placeholder.
		for i := range c.types {
			if c.types[i].Name == result.Name {
				*placeholder = c.types[i]
				// Remove the duplicate — the registry entry IS the canonical one.
				c.types = append(c.types[:i], c.types[i+1:]...)
				break
			}
		}
		c.registry[ref] = placeholder
		return TypeRef{Kind: KindNamed, Name: placeholder.Name}
	}

	// The ref resolved to a non-object type (e.g. a string enum).
	// Remove the placeholder from registry since there's no TypeDef to register.
	delete(c.registry, ref)
	return result
}

// flattenAllOf merges all allOf branches into a single schema.
func (c *schemaConverter) flattenAllOf(branches []map[string]any, path string) map[string]any {
	merged := map[string]any{}
	var mergedProps map[string]any
	var mergedRequired []any

	for _, branch := range branches {
		// Resolve $ref in branch.
		resolved := branch
		if ref, ok := branch["$ref"].(string); ok {
			target := resolveJSONPointer(c.root, ref)
			if target != nil {
				resolved = target
			}
		}

		// Recursively flatten nested allOf.
		if nested, ok := schemaSlice(resolved, "allOf"); ok && len(nested) > 0 {
			resolved = c.flattenAllOf(nested, path)
		}

		// Merge type (intersection).
		if t, ok := resolved["type"]; ok {
			if existing, ok := merged["type"]; ok {
				merged["type"] = intersectTypes(existing, t)
			} else {
				merged["type"] = t
			}
		}

		// Merge properties (union of keys, recursive merge for overlapping).
		if props, ok := resolved["properties"].(map[string]any); ok {
			if mergedProps == nil {
				mergedProps = make(map[string]any)
			}
			for k, v := range props {
				if existing, ok := mergedProps[k]; ok {
					// Recursive merge: wrap in allOf.
					existMap, eOk := existing.(map[string]any)
					vMap, vOk := v.(map[string]any)
					if eOk && vOk {
						mergedProps[k] = c.flattenAllOf([]map[string]any{existMap, vMap}, path+toPascalCase(k))
					}
				} else {
					mergedProps[k] = v
				}
			}
		}

		// Merge required (union).
		if req, ok := resolved["required"].([]any); ok {
			mergedRequired = append(mergedRequired, req...)
		}

		// Merge additionalProperties.
		if ap, ok := resolved["additionalProperties"]; ok {
			if apBool, ok := ap.(bool); ok && !apBool {
				merged["additionalProperties"] = false
			} else if existing, ok := merged["additionalProperties"]; ok {
				// Recursive merge if both are schemas.
				existMap, eOk := existing.(map[string]any)
				apMap, aOk := ap.(map[string]any)
				if eOk && aOk {
					merged["additionalProperties"] = c.flattenAllOf([]map[string]any{existMap, apMap}, path+"AdditionalProperties")
				}
			} else {
				merged["additionalProperties"] = ap
			}
		}

		// Merge enum (intersection).
		if ev, ok := resolved["enum"].([]any); ok {
			if existing, ok := merged["enum"].([]any); ok {
				merged["enum"] = intersectEnums(existing, ev)
			} else {
				merged["enum"] = ev
			}
		}

		// Merge items (recursive).
		if items, ok := resolved["items"].(map[string]any); ok {
			if existing, ok := merged["items"].(map[string]any); ok {
				merged["items"] = c.flattenAllOf([]map[string]any{existing, items}, path+"Item")
			} else {
				merged["items"] = items
			}
		}

		// Carry forward description.
		if desc, ok := resolved["description"].(string); ok {
			if _, has := merged["description"]; !has {
				merged["description"] = desc
			}
		}
	}

	if mergedProps != nil {
		merged["properties"] = mergedProps
	}
	if len(mergedRequired) > 0 {
		merged["required"] = uniqueStrings(mergedRequired)
	}

	return merged
}

// ---------- Helpers ----------

// resolveJSONPointer resolves a JSON Pointer fragment (e.g. "#/schemas/Foo")
// against a root document.
func resolveJSONPointer(root map[string]any, ref string) map[string]any {
	if !strings.HasPrefix(ref, "#/") {
		return nil
	}
	parts := strings.Split(ref[2:], "/")
	var current any = root
	for _, part := range parts {
		// Unescape JSON Pointer (RFC 6901).
		part = strings.ReplaceAll(part, "~1", "/")
		part = strings.ReplaceAll(part, "~0", "~")
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current, ok = m[part]
		if !ok {
			return nil
		}
	}
	if m, ok := current.(map[string]any); ok {
		return m
	}
	return nil
}

// refName extracts a type name from a $ref string.
// "#/schemas/MenuItem" → "MenuItem", "#/$defs/Foo" → "Foo".
func refName(ref string) string {
	parts := strings.Split(ref, "/")
	if len(parts) > 0 {
		return toPascalCase(parts[len(parts)-1])
	}
	return "Unknown"
}

// schemaType returns the singular type string from a JSON Schema.
// isConstraintOnlyVariants returns true if all oneOf/anyOf variants contain
// only constraint keywords (like "required") and no structural type definitions
// (no "type", "properties", "$ref", "items", etc.). Such variants express
// validation rules (e.g., "must have location OR content"), not distinct types.
func isConstraintOnlyVariants(variants []map[string]any) bool {
	structural := map[string]bool{
		"type": true, "properties": true, "$ref": true, "items": true,
		"additionalProperties": true, "allOf": true, "oneOf": true, "anyOf": true,
		"enum": true, "const": true,
	}
	for _, v := range variants {
		for k := range v {
			if structural[k] {
				return false
			}
		}
	}
	return true
}

// hasStructuralType returns true if the schema has "type" or "properties",
// indicating it defines a structural type beyond just union variants.
func hasStructuralType(schema map[string]any) bool {
	if _, ok := schema["type"]; ok {
		return true
	}
	if _, ok := schema["properties"]; ok {
		return true
	}
	return false
}

func schemaType(schema map[string]any) string {
	switch t := schema["type"].(type) {
	case string:
		return t
	case []any:
		// Filter out "null" and return first remaining.
		for _, v := range t {
			if s, ok := v.(string); ok && s != "null" {
				return s
			}
		}
	}
	return ""
}

// schemaSlice extracts a key that holds an array of schemas.
func schemaSlice(schema map[string]any, key string) ([]map[string]any, bool) {
	arr, ok := schema[key].([]any)
	if !ok {
		return nil, false
	}
	var result []map[string]any
	for _, item := range arr {
		if m, ok := item.(map[string]any); ok {
			result = append(result, m)
		}
	}
	return result, len(result) > 0
}

// requiredSet returns the set of required property names from a schema.
func requiredSet(schema map[string]any) map[string]bool {
	req, ok := schema["required"].([]any)
	if !ok {
		return nil
	}
	set := make(map[string]bool, len(req))
	for _, v := range req {
		if s, ok := v.(string); ok {
			set[s] = true
		}
	}
	return set
}

// sortedKeys returns the keys of a map in sorted order.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// toPascalCase converts a string to PascalCase.
// "placeOrder" → "PlaceOrder", "order_id" → "OrderId", "get-menu" → "GetMenu".
func toPascalCase(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	upper := true
	for _, r := range s {
		switch {
		case r == '_' || r == '-' || r == '.' || r == ' ':
			upper = true
		case upper:
			b.WriteRune(unicode.ToUpper(r))
			upper = false
		default:
			b.WriteRune(r)
		}
	}
	result := b.String()
	if result == "" {
		return "X"
	}
	// Ensure first character is uppercase.
	runes := []rune(result)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// toCamelCase converts a string to camelCase.
func toCamelCase(s string) string {
	p := toPascalCase(s)
	if p == "" {
		return ""
	}
	runes := []rune(p)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

// intersectTypes computes the intersection of two JSON Schema type values.
func intersectTypes(a, b any) any {
	aSet := typeSet(a)
	bSet := typeSet(b)
	var result []any
	for _, t := range aSet {
		for _, u := range bSet {
			if t == u {
				result = append(result, t)
			}
			// integer ⊆ number: if one is "integer" and other is "number", keep "integer".
			if (t == "integer" && u == "number") || (t == "number" && u == "integer") {
				found := false
				for _, r := range result {
					if r == "integer" {
						found = true
						break
					}
				}
				if !found {
					result = append(result, "integer")
				}
			}
		}
	}
	if len(result) == 1 {
		return result[0]
	}
	return result
}

func typeSet(t any) []string {
	switch v := t.(type) {
	case string:
		return []string{v}
	case []any:
		var s []string
		for _, item := range v {
			if str, ok := item.(string); ok {
				s = append(s, str)
			}
		}
		return s
	}
	return nil
}

func intersectEnums(a, b []any) []any {
	bSet := make(map[any]bool, len(b))
	for _, v := range b {
		bSet[v] = true
	}
	var result []any
	for _, v := range a {
		if bSet[v] {
			result = append(result, v)
		}
	}
	return result
}

func uniqueStrings(arr []any) []any {
	seen := make(map[string]bool)
	var result []any
	for _, v := range arr {
		if s, ok := v.(string); ok && !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}
