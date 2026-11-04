package ir

import (
	"errors"
	"reflect"
	"testing"
)

func TestEncodeCodeToRawPanasonic40Golden(t *testing.T) {
	timings, err := EncodeCodeToRaw(ProtocolPanasonic40, "344A90BC2C")
	if err != nil {
		t.Fatalf("EncodeCodeToRaw() error = %v", err)
	}

	// 0x344A90BC2C, written independently as 40 MSB-first bits.
	const goldenBits = "0011010001001010100100001011110000101100"
	want := []uint{3500, 1750}
	for _, bit := range goldenBits {
		want = append(want, 450)
		if bit == '1' {
			want = append(want, 1300)
		} else {
			want = append(want, 450)
		}
	}
	want = append(want, 450, 10000)

	if !reflect.DeepEqual(timings, want) {
		t.Fatalf("EncodeCodeToRaw() = %v, want %v", timings, want)
	}
	if len(timings) != 84 {
		t.Fatalf("len(EncodeCodeToRaw()) = %d, want 84", len(timings))
	}
	if len(timings) > MaxRawTimings {
		t.Fatalf("generated timings count = %d, maximum = %d", len(timings), MaxRawTimings)
	}
	if err := ValidateRawTimings(timings, len(timings)); err != nil {
		t.Fatalf("generated timings violate raw safety limits: %v", err)
	}
}

func TestEncodeCodeToRawNormalizesCode(t *testing.T) {
	got, err := EncodeCodeToRaw(ProtocolPanasonic40, " 344a90bc2c ")
	if err != nil {
		t.Fatalf("EncodeCodeToRaw() error = %v", err)
	}
	want, err := EncodeCodeToRaw(ProtocolPanasonic40, "344A90BC2C")
	if err != nil {
		t.Fatalf("EncodeCodeToRaw() canonical error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized timings = %v, want %v", got, want)
	}
}

func TestEncodeCodeToRawRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		code     string
		wantErr  error
	}{
		{name: "empty protocol", protocol: "", code: "344A90BC2C", wantErr: ErrUnsupportedCodeProtocol},
		{name: "unsupported protocol", protocol: "NEC", code: "344A90BC2C", wantErr: ErrUnsupportedCodeProtocol},
		{name: "empty code", protocol: ProtocolPanasonic40, code: "", wantErr: ErrInvalidIRCode},
		{name: "too short", protocol: ProtocolPanasonic40, code: "344A90BC2", wantErr: ErrInvalidIRCode},
		{name: "non hex", protocol: ProtocolPanasonic40, code: "344A90BC2G", wantErr: ErrInvalidIRCode},
		{name: "prefixed", protocol: ProtocolPanasonic40, code: "0x344A90BC2C", wantErr: ErrInvalidIRCode},
		{name: "40 bit overflow", protocol: ProtocolPanasonic40, code: "FFFFFFFFFFF", wantErr: ErrInvalidIRCode},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EncodeCodeToRaw(tt.protocol, tt.code)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("EncodeCodeToRaw() error = %v, want %v", err, tt.wantErr)
			}
			if got != nil {
				t.Fatalf("EncodeCodeToRaw() timings = %v, want nil", got)
			}
		})
	}
}

func TestEncodeCodeToRawPanasonic40Boundaries(t *testing.T) {
	tests := []struct {
		name      string
		code      string
		wantSpace uint
	}{
		{name: "minimum", code: "0000000000", wantSpace: 450},
		{name: "maximum", code: "FFFFFFFFFF", wantSpace: 1300},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			timings, err := EncodeCodeToRaw(ProtocolPanasonic40, tt.code)
			if err != nil {
				t.Fatalf("EncodeCodeToRaw() error = %v", err)
			}
			if len(timings) != 84 {
				t.Fatalf("len(EncodeCodeToRaw()) = %d, want 84", len(timings))
			}
			for i := 3; i < len(timings)-2; i += 2 {
				if timings[i] != tt.wantSpace {
					t.Fatalf("timings[%d] = %d, want %d", i, timings[i], tt.wantSpace)
				}
			}
		})
	}
}
