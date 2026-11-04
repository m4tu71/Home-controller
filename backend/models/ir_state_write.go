package models

// A null State object must not be interpreted as an omitted partial update.
// Empty objects remain present and are validated against the saved Schema.
func (r *IRStateWriteRequest) UnmarshalJSON(data []byte) error {
	type plain IRStateWriteRequest
	var request plain
	if err := decodeIRSchemaObject(data, &request, []string{"state"}, false); err != nil {
		return err
	}
	*r = IRStateWriteRequest(request)
	return nil
}
