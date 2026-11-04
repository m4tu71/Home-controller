package repositories

import (
	"encoding/json"
	"os"
	"path/filepath"

	"backend/models"
)

type IRAdapterRepository struct {
	roomsDir string
	writer   JSONWriter
}

func NewIRAdapterRepository() IRAdapterRepository {
	return NewIRAdapterRepositoryWithRoomsDir(defaultRoomsDir)
}

func NewIRAdapterRepositoryWithRoomsDir(roomsDir string) IRAdapterRepository {
	return IRAdapterRepository{roomsDir: roomsDir, writer: NewAtomicJSONWriter()}
}

func (r IRAdapterRepository) LoadConfig(roomID string, deviceID string) (models.IRConfig, error) {
	data, err := os.ReadFile(r.configPath(roomID, deviceID))
	if err != nil {
		return models.IRConfig{}, err
	}

	var config models.IRConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return models.IRConfig{}, err
	}

	return config, nil
}

func (r IRAdapterRepository) SaveConfig(roomID string, deviceID string, config models.IRConfig) error {
	if err := r.EnsureIRAdapterDir(roomID, deviceID); err != nil {
		return err
	}

	return r.writer.WriteJSON(r.configPath(roomID, deviceID), config, 0644)
}

func (r IRAdapterRepository) ConfigExists(roomID string, deviceID string) (bool, error) {
	_, err := os.Stat(r.configPath(roomID, deviceID))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (r IRAdapterRepository) ListCommands(roomID string, deviceID string) ([]models.IRCommand, error) {
	entries, err := os.ReadDir(r.commandsDir(roomID, deviceID))
	if err != nil {
		return nil, err
	}

	commands := []models.IRCommand{}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(r.commandsDir(roomID, deviceID), entry.Name()))
		if err != nil {
			continue
		}

		var command models.IRCommand
		if err := json.Unmarshal(data, &command); err != nil {
			continue
		}

		commands = append(commands, command)
	}

	return commands, nil
}

func (r IRAdapterRepository) LoadCommand(roomID string, deviceID string, commandID string) (models.IRCommand, error) {
	data, err := os.ReadFile(r.commandPath(roomID, deviceID, commandID))
	if err != nil {
		return models.IRCommand{}, err
	}

	var command models.IRCommand
	if err := json.Unmarshal(data, &command); err != nil {
		return models.IRCommand{}, err
	}

	return command, nil
}

func (r IRAdapterRepository) SaveCommand(roomID string, deviceID string, command models.IRCommand) error {
	if err := os.MkdirAll(r.commandsDir(roomID, deviceID), 0755); err != nil {
		return err
	}

	return r.writer.WriteJSON(r.commandPath(roomID, deviceID, command.ID), command, 0644)
}

func (r IRAdapterRepository) DeleteCommand(roomID string, deviceID string, commandID string) error {
	return os.Remove(r.commandPath(roomID, deviceID, commandID))
}

func (r IRAdapterRepository) CommandExists(roomID string, deviceID string, commandID string) (bool, error) {
	_, err := os.Stat(r.commandPath(roomID, deviceID, commandID))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (r IRAdapterRepository) LoadStateSchema(roomID string, deviceID string) (models.IRStateSchema, error) {
	data, err := os.ReadFile(r.stateSchemaPath(roomID, deviceID))
	if err != nil {
		return nil, err
	}

	var schema models.IRStateSchema
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, err
	}

	return schema, nil
}

func (r IRAdapterRepository) SaveStateSchema(roomID string, deviceID string, schema models.IRStateSchema) error {
	if err := r.EnsureIRAdapterDir(roomID, deviceID); err != nil {
		return err
	}

	return r.writer.WriteJSON(r.stateSchemaPath(roomID, deviceID), schema, 0644)
}

func (r IRAdapterRepository) StateSchemaExists(roomID string, deviceID string) (bool, error) {
	_, err := os.Stat(r.stateSchemaPath(roomID, deviceID))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (r IRAdapterRepository) ListStates(roomID string, deviceID string) ([]models.IRState, error) {
	return r.listStates(roomID, deviceID, false)
}

// ListStatesStrict is used before Schema replacement: unreadable records must
// stop the write, rather than silently excluding an existing State.
func (r IRAdapterRepository) ListStatesStrict(roomID string, deviceID string) ([]models.IRState, error) {
	return r.listStates(roomID, deviceID, true)
}

func (r IRAdapterRepository) listStates(roomID string, deviceID string, strict bool) ([]models.IRState, error) {
	entries, err := os.ReadDir(r.statesDir(roomID, deviceID))
	if err != nil {
		if strict && os.IsNotExist(err) {
			return []models.IRState{}, nil
		}
		return nil, err
	}

	states := []models.IRState{}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(r.statesDir(roomID, deviceID), entry.Name()))
		if err != nil {
			if strict {
				return nil, err
			}
			continue
		}

		var state models.IRState
		if err := json.Unmarshal(data, &state); err != nil {
			if strict {
				return nil, err
			}
			continue
		}

		states = append(states, state)
	}

	return states, nil
}

func (r IRAdapterRepository) LoadState(roomID string, deviceID string, stateID string) (models.IRState, error) {
	data, err := os.ReadFile(r.statePath(roomID, deviceID, stateID))
	if err != nil {
		return models.IRState{}, err
	}

	var state models.IRState
	if err := json.Unmarshal(data, &state); err != nil {
		return models.IRState{}, err
	}

	return state, nil
}

func (r IRAdapterRepository) SaveState(roomID string, deviceID string, state models.IRState) error {
	if err := os.MkdirAll(r.statesDir(roomID, deviceID), 0755); err != nil {
		return err
	}

	return r.writer.WriteJSON(r.statePath(roomID, deviceID, state.ID), state, 0644)
}

func (r IRAdapterRepository) DeleteState(roomID string, deviceID string, stateID string) error {
	return os.Remove(r.statePath(roomID, deviceID, stateID))
}

func (r IRAdapterRepository) StateExists(roomID string, deviceID string, stateID string) (bool, error) {
	_, err := os.Stat(r.statePath(roomID, deviceID, stateID))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (r IRAdapterRepository) EnsureIRAdapterDir(roomID string, deviceID string) error {
	return os.MkdirAll(r.irAdapterDir(roomID, deviceID), 0755)
}

func (r IRAdapterRepository) irAdapterDir(roomID string, deviceID string) string {
	return filepath.Join(r.roomsDir, roomID, "devices", deviceID, "adapters", "ir")
}

func (r IRAdapterRepository) configPath(roomID string, deviceID string) string {
	return filepath.Join(r.irAdapterDir(roomID, deviceID), "ir_config.json")
}

func (r IRAdapterRepository) commandsDir(roomID string, deviceID string) string {
	return filepath.Join(r.irAdapterDir(roomID, deviceID), "commands")
}

func (r IRAdapterRepository) commandPath(roomID string, deviceID string, commandID string) string {
	return filepath.Join(r.commandsDir(roomID, deviceID), commandID+".json")
}

func (r IRAdapterRepository) stateSchemaPath(roomID string, deviceID string) string {
	return filepath.Join(r.irAdapterDir(roomID, deviceID), "state_schema.json")
}

func (r IRAdapterRepository) statesDir(roomID string, deviceID string) string {
	return filepath.Join(r.irAdapterDir(roomID, deviceID), "states")
}

func (r IRAdapterRepository) statePath(roomID string, deviceID string, stateID string) string {
	return filepath.Join(r.statesDir(roomID, deviceID), stateID+".json")
}
