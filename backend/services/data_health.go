package services

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"backend/models"
	"backend/repositories"
)

type DataHealthService struct {
	Repository *repositories.DataHealthRepository
}

func NewDataHealthService(repo *repositories.DataHealthRepository) *DataHealthService {
	return &DataHealthService{Repository: repo}
}

func (s *DataHealthService) Scan() ([]models.DataHealthIssue, error) {
	nodes, err := s.Repository.Scan()
	if err != nil {
		return nil, err
	}
	byPath := make(map[string]repositories.DataHealthNode, len(nodes))
	for _, n := range nodes {
		byPath[n.Relative] = n
	}
	var issues []models.DataHealthIssue
	add := func(kind, severity, message string, n repositories.DataHealthNode, destructive bool) {
		issues = append(issues, models.DataHealthIssue{
			ID: kind + ":" + n.Relative, Severity: severity,
			Path: path.Join("data", n.Relative), CanonicalPath: n.Absolute,
			Hash: n.Hash, Message: message, HasDestructiveActions: destructive,
		})
	}
	valid := func(rel string) bool {
		n, ok := byPath[rel]
		return ok && !n.Directory && !n.Symlink && len(n.Bytes) > 0 && json.Valid(n.Bytes)
	}
	for _, n := range nodes {
		parts := strings.Split(n.Relative, "/")
		if n.Symlink {
			add("unsafe_symlink", "error", "symbolic link is not followed", n, false)
			continue
		}
		if n.Directory {
			if len(n.Names) == 0 {
				add("empty_directory", "warning", "directory is empty", n, true)
			}
			if len(parts) == 2 && parts[0] == "rooms" {
				if _, ok := byPath[n.Relative+"/room.json"]; !ok {
					add("missing_room_json", "warning", "room directory exists but room.json is missing", n, true)
				}
				if !valid(n.Relative + "/room.json") {
					add("orphan_room", "warning", "room has no readable room.json", n, true)
				}
			}
			if len(parts) == 4 && parts[0] == "rooms" && parts[2] == "devices" {
				if _, ok := byPath[n.Relative+"/device.json"]; !ok {
					add("missing_device_json", "warning", "device directory exists but device.json is missing", n, true)
				}
				if !valid(path.Join("rooms", parts[1], "room.json")) || !valid(n.Relative+"/device.json") {
					add("orphan_device", "warning", "device has no readable parent room or device.json", n, true)
				}
			}
			if len(parts) == 6 && parts[0] == "rooms" && parts[2] == "devices" && parts[4] == "adapters" {
				if !valid(path.Join("rooms", parts[1], "room.json")) || !valid(path.Join("rooms", parts[1], "devices", parts[3], "device.json")) {
					add("orphan_adapter", "warning", "adapter has no readable parent room or device", n, true)
				}
			}
			continue
		}
		if !strings.HasSuffix(n.Relative, ".json") {
			continue
		}
		if len(n.Bytes) == 0 {
			add("empty_json", "error", "JSON file is empty", n, true)
			continue
		}
		if !json.Valid(n.Bytes) {
			add("invalid_json", "error", "JSON file is invalid", n, true)
			continue
		}
		if len(parts) >= 7 && parts[0] == "rooms" && parts[2] == "devices" && parts[4] == "adapters" && parts[5] == "ir" {
			if len(parts) == 7 && parts[6] == "state_schema.json" {
				var schema models.IRStateSchema
				err := json.Unmarshal(n.Bytes, &schema)
				if err == nil {
					err = validateIRStateSchema(schema)
				}
				if err != nil {
					add("invalid_state_schema", "error", "State Schema cannot be decoded or violates its contract: "+err.Error(), n, false)
				}
			}
			if len(parts) == 8 && parts[6] == "states" {
				var state models.IRState
				schemaNode, exists := byPath[path.Join(parts[:6]...)+"/state_schema.json"]
				var schema models.IRStateSchema
				if !exists || schemaNode.Symlink || json.Unmarshal(schemaNode.Bytes, &schema) != nil || validateIRStateSchema(schema) != nil {
					add("state_schema_unavailable", "error", "State cannot be validated: State Schema is missing or invalid", n, false)
				} else if err := json.Unmarshal(n.Bytes, &state); err != nil {
					add("invalid_state", "error", "State cannot be decoded", n, false)
				} else if issues := irStateSchemaIssues(state.State, schema); len(issues) > 0 {
					reasons := []string{}
					for _, issue := range issues {
						reasons = append(reasons, issue.FieldID+":"+issue.Code)
					}
					add("state_schema_mismatch", "error", "State violates Schema: "+strings.Join(reasons, ", "), n, false)
				}
			}
		}
		if !formatSupported(n) {
			add("unsupported_format", "warning", fmt.Sprintf("unsupported format in %s", n.Relative), n, true)
		}
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].ID < issues[j].ID })
	return issues, nil
}

func formatSupported(n repositories.DataHealthNode) bool {
	parts := strings.Split(n.Relative, "/")
	var format struct {
		Format string `json:"format"`
	}
	if err := json.Unmarshal(n.Bytes, &format); err != nil {
		return true
	}
	if len(parts) == 2 && parts[0] == "ir-signals" {
		return format.Format == "raw_samples"
	}
	if len(parts) == 8 && parts[0] == "rooms" && parts[2] == "devices" && parts[4] == "adapters" && parts[5] == "ir" && (parts[6] == "commands" || parts[6] == "states") {
		switch models.OperationFormat(format.Format) {
		case models.OperationFormatRawSamples, models.OperationFormatRaw, models.OperationFormatCode, models.OperationFormatPresetCommand:
			return true
		default:
			return false
		}
	}
	return true
}
