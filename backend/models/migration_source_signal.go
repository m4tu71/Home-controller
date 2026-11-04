package models

// MigrationSourceSignal describes the historical on-disk IR signal format.
// It is retained only for migration and backup readback; it is not an API model.
type MigrationSourceSignal struct {
	Name       string      `json:"name"`
	Room       string      `json:"room"`
	Device     string      `json:"device"`
	Format     string      `json:"format"`
	CarrierKHz int         `json:"carrier_khz"`
	Count      int         `json:"count,omitempty"`
	TimingsUS  []uint      `json:"timings_us,omitempty"`
	Captures   []IRCapture `json:"captures,omitempty"`
}
