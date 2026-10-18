// Package jsonschema builds JSON Schemas from Go types by reflection. It is
// the Go stand-in for Pydantic model_json_schema(), used for tool arguments
// and structured output.
//
// Supported struct tags:
//
//	json:"name,omitempty"   field name; omitempty (or a pointer) makes it optional
//	jsonschema:"description=...,enum=a|b|c,required,default=x,minimum=1,maximum=9"
//	description:"..."       shorthand for the description
package jsonschema

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Schema is a JSON Schema document.
type Schema struct {
	Type                 string             `json:"type,omitempty"`
	Description          string             `json:"description,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	Required             []string           `json:"required,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	Enum                 []any              `json:"enum,omitempty"`
	Default              any                `json:"default,omitempty"`
	Format               string             `json:"format,omitempty"`
	Minimum              *float64           `json:"minimum,omitempty"`
	Maximum              *float64           `json:"maximum,omitempty"`
	AdditionalProperties any                `json:"additionalProperties,omitempty"`
	Title                string             `json:"title,omitempty"`
}

// Map returns the schema as a generic map (what provider APIs want).
func (s *Schema) Map() map[string]any {
	b, _ := json.Marshal(s)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

// For reflects the schema of T.
func For[T any]() *Schema {
	return Reflect(reflect.TypeOf((*T)(nil)).Elem())
}

var timeType = reflect.TypeOf(time.Time{})

// Reflect builds a schema for t.
func Reflect(t reflect.Type) *Schema {
	return reflectType(t, map[reflect.Type]bool{})
}

func reflectType(t reflect.Type, seen map[reflect.Type]bool) *Schema {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == timeType {
		return &Schema{Type: "string", Format: "date-time"}
	}
	switch t.Kind() {
	case reflect.String:
		return &Schema{Type: "string"}
	case reflect.Bool:
		return &Schema{Type: "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &Schema{Type: "integer"}
	case reflect.Float32, reflect.Float64:
		return &Schema{Type: "number"}
	case reflect.Slice, reflect.Array:
		return &Schema{Type: "array", Items: reflectType(t.Elem(), seen)}
	case reflect.Map:
		return &Schema{Type: "object", AdditionalProperties: reflectType(t.Elem(), seen).Map()}
	case reflect.Interface:
		return &Schema{}
	case reflect.Struct:
		if seen[t] {
			return &Schema{Type: "object"}
		}
		seen[t] = true
		defer delete(seen, t)
		s := &Schema{Type: "object", Properties: map[string]*Schema{}, Title: t.Name()}
		addFields(s, t, seen)
		return s
	}
	return &Schema{}
}

func addFields(s *Schema, t reflect.Type, seen map[reflect.Type]bool) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if f.Anonymous && f.Type.Kind() == reflect.Struct && f.Tag.Get("json") == "" {
			addFields(s, f.Type, seen)
			continue
		}
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		fs := reflectType(f.Type, seen)
		optional := strings.Contains(opts, "omitempty") || f.Type.Kind() == reflect.Pointer
		if d := f.Tag.Get("description"); d != "" {
			fs.Description = d
		}
		for _, kv := range splitTag(f.Tag.Get("jsonschema")) {
			k, v, _ := strings.Cut(kv, "=")
			switch k {
			case "description":
				fs.Description = v
			case "required":
				optional = false
			case "optional":
				optional = true
			case "enum":
				for _, e := range strings.Split(v, "|") {
					fs.Enum = append(fs.Enum, parseScalar(fs.Type, e))
				}
			case "default":
				fs.Default = parseScalar(fs.Type, v)
			case "format":
				fs.Format = v
			case "minimum":
				f, _ := strconv.ParseFloat(v, 64)
				fs.Minimum = &f
			case "maximum":
				f, _ := strconv.ParseFloat(v, 64)
				fs.Maximum = &f
			}
		}
		s.Properties[name] = fs
		if !optional {
			s.Required = append(s.Required, name)
		}
	}
}

// splitTag splits on commas that are not escaped with a backslash, so
// descriptions may contain "\,".
func splitTag(tag string) []string {
	if tag == "" {
		return nil
	}
	var out []string
	var cur strings.Builder
	for i := 0; i < len(tag); i++ {
		if tag[i] == '\\' && i+1 < len(tag) && tag[i+1] == ',' {
			cur.WriteByte(',')
			i++
			continue
		}
		if tag[i] == ',' {
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(tag[i])
	}
	return append(out, cur.String())
}

func parseScalar(typ, v string) any {
	switch typ {
	case "integer":
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	case "number":
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	case "boolean":
		return v == "true"
	}
	return v
}

// Validate performs a light structural check of value (decoded JSON) against
// s: required keys, primitive types and enums. It returns the first problem.
func Validate(s *Schema, value any) error {
	return validate(s, value, "$")
}

func validate(s *Schema, v any, path string) error {
	if s == nil {
		return nil
	}
	if len(s.Enum) > 0 {
		ok := false
		for _, e := range s.Enum {
			if fmt.Sprint(e) == fmt.Sprint(v) {
				ok = true
			}
		}
		if !ok {
			return fmt.Errorf("%s: value %v not in enum %v", path, v, s.Enum)
		}
	}
	switch s.Type {
	case "object":
		m, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: expected object, got %T", path, v)
		}
		for _, r := range s.Required {
			if _, ok := m[r]; !ok {
				return fmt.Errorf("%s: missing required field %q", path, r)
			}
		}
		for k, sub := range s.Properties {
			if val, ok := m[k]; ok && val != nil {
				if err := validate(sub, val, path+"."+k); err != nil {
					return err
				}
			}
		}
	case "array":
		a, ok := v.([]any)
		if !ok {
			return fmt.Errorf("%s: expected array, got %T", path, v)
		}
		for i, e := range a {
			if err := validate(s.Items, e, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case "string":
		if _, ok := v.(string); !ok {
			return fmt.Errorf("%s: expected string, got %T", path, v)
		}
	case "integer", "number":
		if _, ok := v.(float64); !ok {
			if _, ok := v.(int); !ok {
				return fmt.Errorf("%s: expected number, got %T", path, v)
			}
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s: expected boolean, got %T", path, v)
		}
	}
	return nil
}
