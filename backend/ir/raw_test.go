package ir

import (
	"errors"
	"reflect"
	"strconv"
	"testing"

	"backend/models"
)

func TestValidateRawTimingsBoundaries(t *testing.T) {
	for _, test := range []struct {
		name    string
		timings []uint
		count   int
	}{
		{name: "single timing maximum", timings: []uint{MaxRawTimingUS}, count: 1},
		{name: "duration maximum", timings: []uint{100000, 100000, 100000, 100000, 100000, 100000, 100000, 100000, 100000, 100000}, count: 10},
		{name: "timing count maximum", timings: make([]uint, MaxRawTimings), count: MaxRawTimings},
	} {
		t.Run(test.name, func(t *testing.T) {
			for i := range test.timings {
				if test.timings[i] == 0 {
					test.timings[i] = 1
				}
			}
			if err := ValidateRawTimings(test.timings, test.count); err != nil {
				t.Fatalf("ValidateRawTimings() error = %v", err)
			}
		})
	}
}

func TestValidateRawTimingsRejectsUnsafeValues(t *testing.T) {
	tests := []struct {
		name    string
		timings []uint
		count   int
		want    error
	}{
		{name: "empty", timings: nil, count: 0, want: ErrInvalidRawTimings},
		{name: "count mismatch", timings: []uint{1, 2}, count: 1, want: ErrInvalidRawTimings},
		{name: "zero timing", timings: []uint{1, 0}, count: 2, want: ErrInvalidRawTimings},
		{name: "too many timings", timings: make([]uint, MaxRawTimings+1), count: MaxRawTimings + 1, want: ErrRawTimingsTooLong},
		{name: "timing over maximum", timings: []uint{MaxRawTimingUS + 1}, count: 1, want: ErrRawTimingExceedsMaximum},
		{name: "duration over maximum", timings: []uint{100000, 100000, 100000, 100000, 100000, 100000, 100000, 100000, 100000, 100000, 1}, count: 11, want: ErrRawDurationExceedsMaximum},
	}
	for i := range tests[3].timings {
		tests[3].timings[i] = 1
	}
	if strconv.IntSize > 32 {
		tests = append(tests, struct {
			name    string
			timings []uint
			count   int
			want    error
		}{name: "Pico unsigned int overflow", timings: []uint{uint(uint64(^uint32(0)) + 1)}, count: 1, want: ErrInvalidRawTimings})
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateRawTimings(test.timings, test.count); !errors.Is(err, test.want) {
				t.Fatalf("ValidateRawTimings() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestValidateCapturedTimings(t *testing.T) {
	valid := []uint{340, 500, 340, 500, 340, 500, 340, 500, 340, 500, 340, 500, 340, 500, 340, 500, 340, 500, 340, 500, 340, 500, 340, 500}
	if !ValidateCapturedTimings(valid) {
		t.Fatal("expected timings to be valid")
	}

	if ValidateCapturedTimings(valid[:9]) {
		t.Fatal("short timings must be invalid")
	}
	if ValidateCapturedTimings([]uint{100, 1200, 1200, 1200, 1200, 1200, 1200, 1200, 1200, 1200}) {
		t.Fatal("timings with a short leading mark must be invalid")
	}
	unsafe := append([]uint(nil), valid...)
	unsafe[5] = MaxRawTimingUS + 1
	if ValidateCapturedTimings(unsafe) {
		t.Fatal("captured timings over the shared safety maximum must be invalid")
	}
}

func TestInferRepresentativeTimingsUsesSharedIRCapture(t *testing.T) {
	captures := []models.IRCapture{
		{Attempt: 1, Valid: true, Count: 4, TimingsUS: []uint{100, 200, 300, 400}},
		{Attempt: 2, Valid: true, Count: 4, TimingsUS: []uint{110, 210, 310, 410}},
		{Attempt: 3, Valid: true, Count: 4, TimingsUS: []uint{120, 220, 320, 420}},
		{Attempt: 4, Valid: true, Count: 20, TimingsUS: []uint{1, 2, 3, 4}},
		{Attempt: 5, Valid: false, Count: 4, TimingsUS: []uint{999, 999, 999, 999}},
	}

	timings, err := InferRepresentativeTimings(captures)
	if err != nil {
		t.Fatalf("infer timings: %v", err)
	}
	want := []uint{110, 210, 310, 410}
	if !reflect.DeepEqual(timings, want) {
		t.Fatalf("timings = %v, want %v", timings, want)
	}
}

func TestCaptureHelpersUseSharedIRCapture(t *testing.T) {
	captures := []models.IRCapture{
		{Valid: true, Count: 4},
		{Valid: false, Count: 4},
		{Valid: true, Count: 6},
	}
	if got := CountValidCaptures(captures); got != 2 {
		t.Fatalf("valid capture count = %d, want 2", got)
	}
	want := map[int]int{4: 1, 6: 1}
	if got := BuildCountDistribution(captures); !reflect.DeepEqual(got, want) {
		t.Fatalf("distribution = %v, want %v", got, want)
	}
}
