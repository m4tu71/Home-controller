package models

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestIRStateSchemaCodecPreservesOmittedAndExplicitValues(t *testing.T) {
	for _, raw := range []string{
		`{"choice":{"type":"enum","values":[""]}}`,
		`{"choice":{"type":"enum","presence":"required","values":[""]}}`,
		`{"n":{"type":"number","values":[]}}`,
		`{"note":{"type":"string","min":0,"label":""},"n":{"type":"number","values":["unused"]}}`,
		`{"note":{"type":"string","presence":"optional","allow_empty":false,"cases":[],"label":""}}`,
		`{"enabled":{"type":"boolean"},"n":{"type":"number","presence":"absent","cases":[{"when":{"all":[{"field":"enabled","in":[false]}]},"presence":"required","min":0,"step":0.1,"unit":""}]}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			var schema IRStateSchema
			if err := json.Unmarshal([]byte(raw), &schema); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(schema)
			if err != nil {
				t.Fatal(err)
			}
			var before, after interface{}
			if err := json.Unmarshal([]byte(raw), &before); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(encoded, &after); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("round trip: %s => %s", raw, encoded)
			}
		})
	}
}
func TestIRStateSchemaCodecRejectsUnknownAndNullExtensions(t *testing.T) {
	for _, raw := range []string{
		`{"n":null}`, `{"n":{"type":"number","presence":null}}`,
		`{"n":{"type":"number","cases":null}}`, `{"n":{"type":"number","label":null}}`,
		`{"n":{"type":"number","allow_empty":null}}`, `{"n":{"type":"number","step":null}}`,
		`{"n":{"type":"number","unknown":1}}`,
		`{"n":{"type":"number","cases":[{"when":{"all":[{"field":"p","in":[true],"extra":1}]},"presence":"required"}]}}`,
		`{"n":{"type":"number","cases":[{"when":{"all":[]},"presence":"required","unknown":1}]}}`,
		`{"n":{"type":"number","cases":[{"when":{"all":[]},"presence":"required","min":null}]}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			var schema IRStateSchema
			if json.Unmarshal([]byte(raw), &schema) == nil {
				t.Fatal("accepted invalid schema JSON")
			}
		})
	}
}

func TestIRStateSchemaCodecRejectsNonStringExtendedEnumCandidates(t *testing.T) {
	for _, candidate := range []string{"null", "true", "1", "{}", "[]"} {
		for _, definition := range []string{
			`{"choice":{"type":"enum","presence":"required","values":[` + candidate + `]}}`,
			`{"profile":{"type":"enum","values":["normal"]},"choice":{"type":"enum","values":["a"],"cases":[{"when":{"all":[{"field":"profile","in":["normal"]}]},"presence":"required","values":[` + candidate + `]}]}}`,
		} {
			t.Run(definition, func(t *testing.T) {
				var schema IRStateSchema
				if json.Unmarshal([]byte(definition), &schema) == nil {
					t.Fatal("accepted non-string enum candidate")
				}
			})
		}
	}
}

func TestIRStateSchemaCodecRejectsMixedCaseNullAttributes(t *testing.T) {
	for _, name := range []string{"Presence", "PRESENCE", "Step", "Cases", "Allow_Empty", "Label", "Description", "Unit"} {
		raw := `{"n":{"type":"number","` + name + `":null}}`
		t.Run(name, func(t *testing.T) {
			var schema IRStateSchema
			if json.Unmarshal([]byte(raw), &schema) == nil {
				t.Fatal("accepted null additional attribute")
			}
		})
	}
	for _, name := range []string{"When", "Presence", "Values", "Min", "Max", "Step", "Allow_Empty", "Label", "Description", "Unit"} {
		raw := `{"n":{"type":"number","cases":[{"when":{"all":[]},"presence":"required","` + name + `":null}]}}`
		t.Run("case/"+name, func(t *testing.T) {
			var schema IRStateSchema
			if json.Unmarshal([]byte(raw), &schema) == nil {
				t.Fatal("accepted null case attribute")
			}
		})
	}
}
