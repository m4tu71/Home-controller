package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type IRStatePresence string

const (
	IRStateRequired IRStatePresence = "required"
	IRStateOptional IRStatePresence = "optional"
	IRStateAbsent   IRStatePresence = "absent"
)

type IRStateFieldCase struct {
	When        IRStateCondition `json:"when"`
	Presence    IRStatePresence  `json:"presence"`
	Values      []string         `json:"values,omitempty"`
	Min         *float64         `json:"min,omitempty"`
	Max         *float64         `json:"max,omitempty"`
	Step        *float64         `json:"step,omitempty"`
	AllowEmpty  *bool            `json:"allow_empty,omitempty"`
	Label       *string          `json:"label,omitempty"`
	Description *string          `json:"description,omitempty"`
	Unit        *string          `json:"unit,omitempty"`
}
type IRStateCondition struct {
	All []IRStateSelector `json:"all"`
}
type IRStateSelector struct {
	Field string        `json:"field"`
	In    []interface{} `json:"in"`
}
type IRStateSchemaIssue struct {
	FieldID   string `json:"field_id"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	CaseIndex *int   `json:"case_index,omitempty"`
	StateID   string `json:"state_id,omitempty"`
}

// Pointers preserve omitted defaults, explicit false, empty labels and empty cases.
// Strict decoding is also used by repository readback, so unknown attributes are
// never silently dropped by a GET followed by PUT.
func (f *IRStateFieldSchema) UnmarshalJSON(data []byte) error {
	type plain IRStateFieldSchema
	var value plain
	if err := decodeIRSchemaObject(data, &value, []string{"presence", "label", "description", "unit", "step", "allow_empty", "cases"}, false); err != nil {
		return err
	}
	*f = IRStateFieldSchema(value)
	return nil
}
func (c *IRStateFieldCase) UnmarshalJSON(data []byte) error {
	type plain IRStateFieldCase
	var value plain
	if err := decodeIRSchemaObject(data, &value, []string{"when", "presence", "values", "min", "max", "step", "allow_empty", "label", "description", "unit"}, true); err != nil {
		return err
	}
	*c = IRStateFieldCase(value)
	return nil
}
func decodeIRSchemaObject(data []byte, out interface{}, nonNull []string, strictValues bool) error {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return err
	}
	if keys == nil {
		return fmt.Errorf("schema definition must be an object")
	}
	// Match attributes exactly as Go's JSON decoder does, including case
	// variants. Check every raw key so aliases cannot hide a null value.
	for key, raw := range keys {
		for _, attribute := range nonNull {
			if strings.EqualFold(key, attribute) && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				return fmt.Errorf("schema attribute %s must not be null", key)
			}
		}
	}

	// []string decoding maps null elements to "". Validate raw candidates
	// before decoding extended fields and all cases, preserving legacy decoding.
	for key := range keys {
		for _, behavior := range []string{"presence", "step", "allow_empty", "cases"} {
			if strings.EqualFold(key, behavior) {
				strictValues = true
			}
		}
	}
	if strictValues {
		for key, raw := range keys {
			if !strings.EqualFold(key, "values") {
				continue
			}
			var candidates []json.RawMessage
			if err := json.Unmarshal(raw, &candidates); err != nil {
				return err
			}
			if candidates == nil {
				return fmt.Errorf("schema values must be an array of strings")
			}
			for index, candidate := range candidates {
				trimmed := bytes.TrimSpace(candidate)
				if len(trimmed) == 0 || trimmed[0] != '"' {
					return fmt.Errorf("schema values[%d] must be a string", index)
				}
			}
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(out)
}

// Preserve an explicitly empty legacy values array even on non-enum fields.
func (f IRStateFieldSchema) MarshalJSON() ([]byte, error) {
	type plain IRStateFieldSchema
	var values *[]string
	if f.Values != nil {
		values = &f.Values
	}
	return json.Marshal(struct {
		plain
		Values *[]string `json:"values,omitempty"`
	}{plain: plain(f), Values: values})
}
