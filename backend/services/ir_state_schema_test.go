package services

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"backend/models"
	"backend/repositories"
)

func schemaFromJSON(t *testing.T, raw string) models.IRStateSchema {
	t.Helper()
	var schema models.IRStateSchema
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		t.Fatal(err)
	}
	return schema
}
func stateFromJSON(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	var state map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		t.Fatal(err)
	}
	return state
}
func TestSchemaLegacyCompatibilityAndExplicitExtension(t *testing.T) {
	for _, tc := range []struct {
		schema, state string
		valid         bool
	}{
		{`{"note":{"type":"string","min":0}}`, `{"note":"abc"}`, true},
		{`{"n":{"type":"number","values":["unused"]}}`, `{"n":1}`, true},
		{`{"note":{"type":"string","min":2,"max":1}}`, `{"note":"abc"}`, false},
		{`{"note":{"type":"string","min":0,"label":"Note"}}`, `{"note":"abc"}`, true},
		{`{"note":{"type":"string","min":0,"presence":"required"}}`, `{"note":"abc"}`, false},
		{`{"note":{"type":"string","presence":"required"}}`, `{"note":"abc"}`, true},
		{`{"choice":{"type":"enum","values":[""]}}`, `{"choice":""}`, true},
		{`{"note":{"type":"string","min":0},"eco":{"type":"boolean","presence":"optional"}}`, `{"note":"abc","eco":false}`, true},
	} {
		t.Run(tc.schema, func(t *testing.T) {
			s := schemaFromJSON(t, tc.schema)
			err := validateIRStateSchema(s)
			if (err == nil) != tc.valid {
				t.Fatalf("schema validity: %v", err)
			}
			if tc.valid {
				if err := validateIRStateMatchesSchema(stateFromJSON(t, tc.state), s); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
func TestSchemaCasesReplaceAllConstraints(t *testing.T) {
	for _, tc := range []struct {
		field          string
		schemaValid    bool
		accept, reject string
	}{
		{`"type":"enum","values":["a"],"presence":"required","cases":[{"when":{"all":[{"field":"profile","in":["special"]}]},"presence":"required"}]`, false, "", ""},
		{`"type":"enum","values":["a"],"cases":[{"when":{"all":[{"field":"profile","in":["special"]}]},"presence":"required","values":[""]}]`, true, `""`, `"a"`},
		{`"type":"number","min":0,"max":10,"step":2,"cases":[{"when":{"all":[{"field":"profile","in":["special"]}]},"presence":"required","min":-2}]`, true, `10.5`, `-3`},
		{`"type":"string","allow_empty":false,"cases":[{"when":{"all":[{"field":"profile","in":["special"]}]},"presence":"required"}]`, true, `""`, `false`},
		{`"type":"number","min":0,"cases":[{"when":{"all":[{"field":"profile","in":["special"]}]},"presence":"absent"}]`, true, "", `0`},
	} {
		t.Run(tc.field, func(t *testing.T) {
			s := schemaFromJSON(t, `{"profile":{"type":"enum","values":["normal","special"]},"value":{`+tc.field+`}}`)
			if err := validateIRStateSchema(s); (err == nil) != tc.schemaValid {
				t.Fatalf("schema: %v", err)
			}
			if !tc.schemaValid {
				return
			}
			raw := `{"profile":"special"`
			if tc.accept != "" {
				raw += `,"value":` + tc.accept
			}
			raw += `}`
			if err := validateIRStateMatchesSchema(stateFromJSON(t, raw), s); err != nil {
				t.Fatal(err)
			}
			if err := validateIRStateMatchesSchema(stateFromJSON(t, `{"profile":"special","value":`+tc.reject+`}`), s); err == nil {
				t.Fatal("accepted invalid case value")
			}
		})
	}
	// Explicit empty display values clear the base while omitted ones inherit.
	s := schemaFromJSON(t, `{"profile":{"type":"enum","values":["special"]},"value":{"type":"number","label":"Value","unit":"U","cases":[{"when":{"all":[{"field":"profile","in":["special"]}]},"presence":"optional","unit":""}]}}`)
	f, index := resolveIRStateField(s["value"], stateFromJSON(t, `{"profile":"special"}`))
	if index == nil || *index != 0 || f.Label == nil || *f.Label != "Value" || f.Unit == nil || *f.Unit != "" {
		t.Fatalf("display resolution: %#v", f)
	}
}
func TestSchemaRejectsInvalidConditionsAndConstraints(t *testing.T) {
	base := `"profile":{"type":"enum","values":["normal","special"]},"enabled":{"type":"boolean"},`
	for _, field := range []string{
		`"type":"number","presence":"unknown"`,
		`"type":"number","presence":"absent","min":0`,
		`"type":"boolean","presence":"optional","values":["a"]`,
		`"type":"number","step":0`, `"type":"number","step":-1`,
		`"type":"enum","presence":"optional","values":["a","a"]`,
		`"type":"number","cases":[{"when":{"all":[]},"presence":"required"}]`,
		`"type":"number","cases":[{"when":{"all":[{"field":"unknown","in":["normal"]}]},"presence":"required"}]`,
		`"type":"number","cases":[{"when":{"all":[{"field":"value","in":[0]}]},"presence":"required"}]`,
		`"type":"number","cases":[{"when":{"all":[{"field":"profile","in":[]}]},"presence":"required"}]`,
		`"type":"number","cases":[{"when":{"all":[{"field":"profile","in":["missing"]}]},"presence":"required"}]`,
		`"type":"number","cases":[{"when":{"all":[{"field":"profile","in":[true]}]},"presence":"required"}]`,
		`"type":"number","cases":[{"when":{"all":[{"field":"enabled","in":["false"]}]},"presence":"required"}]`,
		`"type":"number","cases":[{"when":{"all":[{"field":"profile","in":["normal","normal"]}]},"presence":"required"}]`,
		`"type":"number","cases":[{"when":{"all":[{"field":"profile","in":["normal"]},{"field":"profile","in":["special"]}]},"presence":"required"}]`,
		`"type":"number","cases":[{"when":{"all":[{"field":"profile","in":["normal"]}]}}]`,
		`"type":"number","cases":[{"when":{"all":[{"field":"profile","in":["normal"]}]},"presence":"required"},{"when":{"all":[{"field":"enabled","in":[true]}]},"presence":"required"}]`,
		`"type":"number","cases":[{"when":{"all":[{"field":"profile","in":["normal"]}]},"presence":"absent","min":0}]`,
	} {
		t.Run(field, func(t *testing.T) {
			if err := validateIRStateSchema(schemaFromJSON(t, `{`+base+`"value":{`+field+`}}`)); err == nil {
				t.Fatal("accepted invalid schema")
			}
		})
	}
	for _, selector := range []string{
		`{"type":"enum","values":["normal"],"presence":"optional"}`,
		`{"type":"enum","values":["normal"],"cases":[]}`,
		`{"type":"number"}`,
	} {
		t.Run(selector, func(t *testing.T) {
			s := schemaFromJSON(t, `{"profile":`+selector+`,"value":{"type":"number","cases":[{"when":{"all":[{"field":"profile","in":["normal"]}]},"presence":"required"}]}}`)
			if validateIRStateSchema(s) == nil {
				t.Fatal("accepted conditional/optional/nonfinite selector")
			}
		})
	}
	n := math.Inf(1)
	if validateIRStateSchema(models.IRStateSchema{"n": {Type: models.IRStateFieldTypeNumber, Min: &n}}) == nil {
		t.Fatal("accepted infinite bound")
	}
}
func TestSchemaDecimalStepBoundaries(t *testing.T) {
	for _, tc := range []struct {
		constraints    string
		accept, reject []float64
	}{
		{`"min":0,"max":1,"step":0.1`, []float64{0, 0.3, 1, 3e-1}, []float64{0.31, 0.30000000000000004}},
		{`"min":-2,"max":2,"step":0.5`, []float64{-2, -0.5, 0, 2}, []float64{-2.5, 0.25}},
		{`"min":0.05,"max":0.35,"step":0.1`, []float64{0.05, 0.15, 0.35}, []float64{0.1, 0.36}},
		{`"max":2,"step":0.5`, []float64{-1, 0, 1.5}, []float64{0.25}},
	} {
		t.Run(tc.constraints, func(t *testing.T) {
			s := schemaFromJSON(t, `{"n":{"type":"number",`+tc.constraints+`}}`)
			if err := validateIRStateSchema(s); err != nil {
				t.Fatal(err)
			}
			for _, n := range tc.accept {
				if err := validateIRStateMatchesSchema(map[string]interface{}{"n": n}, s); err != nil {
					t.Fatalf("%g: %v", n, err)
				}
			}
			for _, n := range tc.reject {
				if validateIRStateMatchesSchema(map[string]interface{}{"n": n}, s) == nil {
					t.Fatalf("accepted %g", n)
				}
			}
		})
	}
}

const nonACSchemaFixture = `{"enabled":{"type":"boolean"},"profile":{"type":"enum","values":["normal","boost"]},"level":{"type":"number","presence":"absent","cases":[{"when":{"all":[{"field":"enabled","in":[true]},{"field":"profile","in":["normal"]}]},"presence":"required","min":0,"max":5,"step":1},{"when":{"all":[{"field":"enabled","in":[true]},{"field":"profile","in":["boost"]}]},"presence":"required","min":6,"max":10,"step":1}]},"eco":{"type":"boolean","presence":"optional"},"note":{"type":"string","presence":"optional","allow_empty":false}}`

func TestSchemaNonACAndOptionalFixtures(t *testing.T) {
	s := schemaFromJSON(t, nonACSchemaFixture)
	if err := validateIRStateSchema(s); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"enabled":false,"profile":"normal"}`, `{"enabled":true,"profile":"normal","level":0,"eco":false}`, `{"enabled":true,"profile":"boost","level":6}`} {
		if err := validateIRStateMatchesSchema(stateFromJSON(t, raw), s); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
	}
	for _, raw := range []string{`{"enabled":false,"profile":"normal","level":0}`, `{"enabled":true,"profile":"normal"}`, `{"enabled":true,"profile":"boost","level":5}`, `{"enabled":false,"profile":"normal","note":""}`, `{"enabled":"false","profile":"normal"}`, `{"enabled":false,"profile":"normal","eco":null}`, `{"enabled":false,"profile":"normal","extra":0}`} {
		if validateIRStateMatchesSchema(stateFromJSON(t, raw), s) == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	optional := schemaFromJSON(t, `{"note":{"type":"string","presence":"optional"}}`)
	for _, raw := range []string{`{}`, `{"note":""}`} {
		if err := validateIRStateMatchesSchema(stateFromJSON(t, raw), optional); err != nil {
			t.Fatal(err)
		}
	}
}
func TestSchemaPersistenceAndConflictKeepsLearningBytes(t *testing.T) {
	service := newStateTestService(t, models.IRRemoteTypeStateful, true)
	rooms := filepath.Join(t.TempDir(), "rooms")
	service.irAdapterRepository = repositories.NewIRAdapterRepositoryWithRoomsDir(rooms)
	if err := service.irAdapterRepository.SaveConfig("room", "device", models.IRConfig{IRRemoteType: models.IRRemoteTypeStateful, CarrierKHz: 38}); err != nil {
		t.Fatal(err)
	}
	legacy := schemaFromJSON(t, `{"note":{"type":"string","min":0},"n":{"type":"number","values":["unused"]},"choice":{"type":"enum","values":[""]}}`)
	if _, err := service.SaveStateSchema("room", "device", legacy); err != nil {
		t.Fatal(err)
	}
	state := models.IRState{ID: "learned", Name: "Learned", State: stateFromJSON(t, `{"note":"abc","n":1,"choice":""}`), Format: models.OperationFormatRawSamples, Count: 2, TimingsUS: []uint{9000, 4500}, Captures: []models.IRCapture{{Count: 2, TimingsUS: []uint{9000, 4500}}}}
	if err := service.irAdapterRepository.SaveState("room", "device", state); err != nil {
		t.Fatal(err)
	}
	read := func(name string) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(rooms, "room", "devices", "device", "adapters", "ir", name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	beforeSchema := read("state_schema.json")
	beforeState := read("states/learned.json")
	loaded, err := service.GetStateSchema("room", "device")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(legacy, loaded) {
		t.Fatal("legacy readback changed schema")
	}
	for _, modified := range []string{
		`{"note":{"type":"string","min":0,"presence":"required"},"n":{"type":"number","values":["unused"]},"choice":{"type":"enum","values":[""]}}`,
		`{"note":{"type":"string","allow_empty":false},"n":{"type":"number","presence":"absent"},"choice":{"type":"enum","values":[""]}}`,
	} {
		_, err := service.SaveStateSchema("room", "device", schemaFromJSON(t, modified))
		var se *ServiceError
		if !errors.As(err, &se) {
			t.Fatalf("expected rejection: %v", err)
		}
		actual, _ := service.GetStateSchema("room", "device")
		if !reflect.DeepEqual(legacy, actual) {
			t.Fatal("rejection replaced schema")
		}
		after, err := service.irAdapterRepository.LoadState("room", "device", "learned")
		if err != nil {
			t.Fatal(err)
		}

		if !reflect.DeepEqual(state, after) {
			t.Fatal("rejection changed state")
		}
		if string(read("states/learned.json")) != string(beforeState) || string(read("state_schema.json")) != string(beforeSchema) {
			t.Fatal("rejection changed file bytes")
		}

	}
}

// This fixture is shared with the forthcoming Frontend resolver tests. Values
// are synthetic; they are not a completed configuration for the real appliance.
func TestSchemaSharedContractFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/ir_state_schema_contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures map[string]struct {
		Schema models.IRStateSchema `json:"schema"`
		States []struct {
			Value map[string]interface{} `json:"value"`
			Valid bool                   `json:"valid"`
			Code  string                 `json:"code"`
		} `json:"states"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for name, fixture := range fixtures {
		t.Run(name, func(t *testing.T) {
			if err := validateIRStateSchema(fixture.Schema); err != nil {
				t.Fatal(err)
			}
			for _, state := range fixture.States {
				err := validateIRStateMatchesSchema(state.Value, fixture.Schema)
				if (err == nil) != state.Valid {
					t.Fatalf("%v: valid=%v error=%v", state.Value, state.Valid, err)
				}
				if !state.Valid {
					issues := irStateSchemaIssues(state.Value, fixture.Schema)
					found := false
					for _, issue := range issues {
						if issue.Code == state.Code {
							found = true
						}
					}
					if !found {
						t.Fatalf("%v: want code %s, issues=%v", state.Value, state.Code, issues)
					}
				}
			}
		})
	}
}

func TestSchemaNilStateAndFiniteGridReachability(t *testing.T) {
	optional := schemaFromJSON(t, `{"note":{"type":"string","presence":"optional"}}`)
	if validateIRStateMatchesSchema(nil, optional) == nil {
		t.Fatal("null State accepted")
	}
	for _, raw := range []string{
		`{"n":{"type":"number","max":-1.7976931348623157e308,"step":1e308}}`,
		`{"n":{"type":"number","cases":[{"when":{"all":[{"field":"p","in":[true]}]},"presence":"required","max":-1.7976931348623157e308,"step":1e308}]},"p":{"type":"boolean"}}`,
	} {
		if validateIRStateSchema(schemaFromJSON(t, raw)) == nil {
			t.Fatal("unreachable grid accepted")
		}
	}
}
