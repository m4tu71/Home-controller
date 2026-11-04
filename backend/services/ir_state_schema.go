package services

import (
	"math"
	"math/big"
	"sort"
	"strconv"

	"backend/models"
)

func schemaKeys(schema models.IRStateSchema) []string {
	keys := make([]string, 0, len(schema))
	for key := range schema {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func fieldPresence(f models.IRStateFieldSchema) models.IRStatePresence {
	if f.Presence == nil {
		return models.IRStateRequired
	}
	return *f.Presence
}
func extendedField(f models.IRStateFieldSchema) bool {
	return f.Presence != nil || f.Step != nil || f.AllowEmpty != nil || f.Cases != nil
}
func validPresence(p models.IRStatePresence) bool {
	return p == models.IRStateRequired || p == models.IRStateOptional || p == models.IRStateAbsent
}
func schemaIssueError(message, field string, index *int) error {
	return &ServiceError{Kind: ErrorInvalidRequest, Message: message, Payload: map[string]interface{}{
		"ok": false, "error": message, "issues": []models.IRStateSchemaIssue{{FieldID: field, Code: "invalid_schema", Message: message, CaseIndex: index}},
	}}
}
func validateIRStateSchema(schema models.IRStateSchema) error {
	if len(schema) == 0 {
		return schemaIssueError("ir state schema is required", "", nil)
	}
	for _, id := range schemaKeys(schema) {
		f := schema[id]
		if !validateIRStateFieldID(id) {
			return schemaIssueError("invalid ir state field id", id, nil)
		}
		if !validateIRStateFieldType(f.Type) {
			return schemaIssueError("invalid ir state field type", id, nil)
		}
		if err := validateSchemaConstraints(id, f, extendedField(f), nil); err != nil {
			return err
		}
		if f.Cases == nil {
			continue
		}
		for index, c := range *f.Cases {
			idx := index
			if !validPresence(c.Presence) {
				return schemaIssueError("invalid state field presence", id, &idx)
			}
			resolved := fieldFromCase(f, c)
			if err := validateSchemaConstraints(id, resolved, true, &idx); err != nil {
				return err
			}
			if len(c.When.All) == 0 {
				return schemaIssueError("case conditions are required", id, &idx)
			}
			seen := map[string]bool{}
			for _, selector := range c.When.All {
				source, exists := schema[selector.Field]
				if !exists || selector.Field == id || seen[selector.Field] || source.Cases != nil || fieldPresence(source) != models.IRStateRequired || (source.Type != models.IRStateFieldTypeEnum && source.Type != models.IRStateFieldTypeBoolean) {
					return schemaIssueError("invalid case selector", id, &idx)
				}
				seen[selector.Field] = true
				if len(selector.In) == 0 {
					return schemaIssueError("selector values are required", id, &idx)
				}
				values := map[interface{}]bool{}
				for _, v := range selector.In {
					valid := false
					switch source.Type {
					case models.IRStateFieldTypeEnum:
						str, ok := v.(string)
						valid = ok && containsString(source.Values, str)
					case models.IRStateFieldTypeBoolean:
						_, valid = v.(bool)
					}
					if !valid {
						return schemaIssueError("invalid selector value", id, &idx)
					}
					if values[v] {
						return schemaIssueError("duplicate selector value", id, &idx)
					}
					values[v] = true
				}
			}
			for earlier := 0; earlier < index; earlier++ {
				if casesOverlap((*f.Cases)[earlier], c) {
					return schemaIssueError("overlapping state field cases", id, &idx)
				}
			}
		}
	}
	return nil
}
func validateSchemaConstraints(id string, f models.IRStateFieldSchema, extended bool, index *int) error {
	fail := func(message string) error { return schemaIssueError(message, id, index) }
	for _, n := range []*float64{f.Min, f.Max, f.Step} {
		if n != nil && (math.IsNaN(*n) || math.IsInf(*n, 0)) {
			return fail("state field numbers must be finite")
		}
	}
	if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
		return fail("state field min must be less than or equal to max")
	}
	if !extended {
		if f.Type == models.IRStateFieldTypeEnum && len(f.Values) == 0 {
			return fail("enum state field values are required")
		}
		return nil
	}
	presence := fieldPresence(f)
	if !validPresence(presence) {
		return fail("invalid state field presence")
	}
	if presence == models.IRStateAbsent {
		if f.Values != nil || f.Min != nil || f.Max != nil || f.Step != nil || f.AllowEmpty != nil {
			return fail("absent state field must not have value constraints")
		}
		return nil
	}
	if (f.Type != models.IRStateFieldTypeEnum && f.Values != nil) || (f.Type != models.IRStateFieldTypeNumber && (f.Min != nil || f.Max != nil || f.Step != nil)) || (f.Type != models.IRStateFieldTypeString && f.AllowEmpty != nil) {
		return fail("state field constraints do not match type")
	}
	if f.Type == models.IRStateFieldTypeEnum {
		if len(f.Values) == 0 {
			return fail("enum state field values are required")
		}
		seen := map[string]bool{}
		for _, v := range f.Values {
			if seen[v] {
				return fail("duplicate enum state field value")
			}
			seen[v] = true
		}
	}
	if f.Step != nil {
		if *f.Step <= 0 {
			return fail("state field step must be positive")
		}
		// With a min, min itself lies on the grid. Without one, the grid
		// starts at zero; check that it has a finite point under a negative max.
		if f.Min == nil && f.Max != nil && *f.Max < 0 && !stepHasFinitePoint(*f.Max, *f.Step) {
			return fail("state field has no reachable numeric value")
		}
	}
	return nil
}
func casesOverlap(a, b models.IRStateFieldCase) bool {
	for _, left := range a.When.All {
		for _, right := range b.When.All {
			if left.Field != right.Field {
				continue
			}
			overlap := false
			for _, x := range left.In {
				for _, y := range right.In {
					if x == y {
						overlap = true
					}
				}
			}
			if !overlap {
				return false
			}
		}
	}
	return true
}
func fieldFromCase(base models.IRStateFieldSchema, c models.IRStateFieldCase) models.IRStateFieldSchema {
	f := models.IRStateFieldSchema{Type: base.Type, Presence: &c.Presence, Values: c.Values, Min: c.Min, Max: c.Max, Step: c.Step, AllowEmpty: c.AllowEmpty, Label: base.Label, Description: base.Description, Unit: base.Unit}
	if c.Label != nil {
		f.Label = c.Label
	}
	if c.Description != nil {
		f.Description = c.Description
	}
	if c.Unit != nil {
		f.Unit = c.Unit
	}
	return f
}

// resolveIRStateField assumes a valid Schema and valid unconditional selectors.
// Case constraints replace the base in full; only display attributes inherit.
func resolveIRStateField(f models.IRStateFieldSchema, state map[string]interface{}) (models.IRStateFieldSchema, *int) {
	if f.Cases != nil {
		for index, c := range *f.Cases {
			matches := true
			for _, selector := range c.When.All {
				value, exists := state[selector.Field]
				member := false
				for _, candidate := range selector.In {
					if value == candidate {
						member = true
					}
				}
				if !exists || !member {
					matches = false
					break
				}
			}
			if matches {
				idx := index
				return fieldFromCase(f, c), &idx
			}
		}
	}
	return f, nil
}
func decimalRat(n float64) *big.Rat {
	r, ok := new(big.Rat).SetString(strconv.FormatFloat(n, 'g', -1, 64))
	if !ok {
		panic("finite number must have a decimal representation")
	}
	return r
}
func matchesDecimalStep(value float64, min, step *float64) bool {
	if step == nil {
		return true
	}
	offset := decimalRat(value)
	if min != nil {
		offset.Sub(offset, decimalRat(*min))
	}
	offset.Quo(offset, decimalRat(*step))
	return offset.IsInt()
}
func stepHasFinitePoint(max, step float64) bool {
	// The greatest grid point <= max must not be below -MaxFloat64.
	q := new(big.Rat).Quo(decimalRat(max), decimalRat(step))
	integer := new(big.Int).Quo(q.Num(), q.Denom())
	if !q.IsInt() {
		integer.Sub(integer, big.NewInt(1))
	}
	point := new(big.Rat).Mul(new(big.Rat).SetInt(integer), decimalRat(step))
	return point.Cmp(decimalRat(-math.MaxFloat64)) >= 0
}
func stateValueIssue(value interface{}, f models.IRStateFieldSchema) string {
	switch f.Type {
	case models.IRStateFieldTypeEnum:
		str, ok := value.(string)
		if !ok {
			return "type"
		}
		if !containsString(f.Values, str) {
			return "enum"
		}
	case models.IRStateFieldTypeNumber:
		n, ok := value.(float64)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
			return "type"
		}
		if (f.Min != nil && n < *f.Min) || (f.Max != nil && n > *f.Max) {
			return "range"
		}
		if !matchesDecimalStep(n, f.Min, f.Step) {
			return "step"
		}
	case models.IRStateFieldTypeBoolean:
		if _, ok := value.(bool); !ok {
			return "type"
		}
	case models.IRStateFieldTypeString:
		str, ok := value.(string)
		if !ok {
			return "type"
		}
		if str == "" && f.AllowEmpty != nil && !*f.AllowEmpty {
			return "empty"
		}
	default:
		return "type"
	}
	return ""
}
func irStateSchemaIssues(state map[string]interface{}, schema models.IRStateSchema) []models.IRStateSchemaIssue {
	issues := []models.IRStateSchemaIssue{}
	if state == nil {
		return []models.IRStateSchemaIssue{{FieldID: "", Code: "type", Message: "state must be a JSON object"}}
	}
	add := func(field, code string, index *int) {
		issues = append(issues, models.IRStateSchemaIssue{FieldID: field, Code: code, Message: "state field violates " + code + " constraint", CaseIndex: index})
	}
	unknown := []string{}
	for id := range state {
		if _, exists := schema[id]; !exists {
			unknown = append(unknown, id)
		}
	}
	sort.Strings(unknown)
	for _, id := range unknown {
		add(id, "unknown_field", nil)
	}
	for _, id := range schemaKeys(schema) {
		base := schema[id]
		selectorsValid := true
		if base.Cases != nil {
			for _, c := range *base.Cases {
				for _, selector := range c.When.All {
					v, exists := state[selector.Field]
					if !exists || stateValueIssue(v, schema[selector.Field]) != "" {
						selectorsValid = false
					}
				}
			}
		}
		if !selectorsValid {
			continue
		} // Report the source field's error, not a guessed case.
		f, index := resolveIRStateField(base, state)
		value, exists := state[id]
		switch fieldPresence(f) {
		case models.IRStateAbsent:
			if exists {
				add(id, "absent", index)
			}
			continue
		case models.IRStateRequired:
			if !exists {
				add(id, "required", index)
				continue
			}
		case models.IRStateOptional:
			if !exists {
				continue
			}
		}
		if code := stateValueIssue(value, f); code != "" {
			add(id, code, index)
		}
	}
	return issues
}
func validateIRStateMatchesSchema(state map[string]interface{}, schema models.IRStateSchema) error {
	if err := validateIRStateSchema(schema); err != nil {
		return err
	}
	issues := irStateSchemaIssues(state, schema)
	if len(issues) == 0 {
		return nil
	}
	return &ServiceError{Kind: ErrorInvalidRequest, Message: irStateDoesNotMatchSchemaErrorMessage, Payload: map[string]interface{}{"ok": false, "error": irStateDoesNotMatchSchemaErrorMessage, "issues": issues}}
}
