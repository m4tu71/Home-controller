package models

type Device struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	RoomID         string   `json:"room_id"`
	DeviceType     string   `json:"device_type"`
	PrimaryAdapter string   `json:"primary_adapter"`
	Capabilities   []string `json:"capabilities,omitempty"`
}
