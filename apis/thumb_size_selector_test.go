package apis

import "testing"

func TestSelectThumbSizePhotosClosestLargerOrLargest(t *testing.T) {
	available := []string{"400x0", "1200x0", "2000x0"}
	tests := []struct {
		requested string
		want      string
	}{
		{"400x0", "400x0"},
		{"401x0", "1200x0"},
		{"800x0", "1200x0"},
		{"1800x0", "2000x0"},
		{"2400x0", "2000x0"},
	}
	for _, tt := range tests {
		t.Run(tt.requested, func(t *testing.T) {
			got, ok := selectThumbSize("photos", tt.requested, available)
			if !ok {
				t.Fatalf("selectThumbSize() ok = false, want true")
			}
			if got != tt.want {
				t.Fatalf("selectThumbSize() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSelectThumbSizeNonPhotosExactMatchOnly(t *testing.T) {
	available := []string{"400x0", "1200x0", "2000x0"}
	if got, ok := selectThumbSize("demo1", "800x0", available); ok {
		t.Fatalf("selectThumbSize() = %q, true; want no thumbnail selection", got)
	}
	got, ok := selectThumbSize("demo1", "400x0", available)
	if !ok || got != "400x0" {
		t.Fatalf("selectThumbSize() = %q, %v; want 400x0, true", got, ok)
	}
}

func TestSelectThumbSizePhotosSuffixAndZeroDimensionRanking(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		available []string
		want      string
	}{
		{
			name:      "prefers same suffix when available",
			requested: "100x100t",
			available: []string{"120x120", "150x150t", "200x200t"},
			want:      "150x150t",
		},
		{
			name:      "falls back across suffixes when none match",
			requested: "100x100t",
			available: []string{"120x120", "150x150f"},
			want:      "120x120",
		},
		{
			name:      "height only chooses closest larger height",
			requested: "0x401",
			available: []string{"0x400", "0x1200", "0x2000"},
			want:      "0x1200",
		},
		{
			name:      "height only falls back to largest height",
			requested: "0x2400",
			available: []string{"0x400", "0x1200", "0x2000"},
			want:      "0x2000",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := selectThumbSize("photos", tt.requested, tt.available)
			if !ok {
				t.Fatalf("selectThumbSize() ok = false, want true")
			}
			if got != tt.want {
				t.Fatalf("selectThumbSize() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSelectThumbSizeInvalidPhotoRequestDoesNotSelectClosest(t *testing.T) {
	if got, ok := selectThumbSize("photos", "invalid", []string{"400x0"}); ok {
		t.Fatalf("selectThumbSize() = %q, true; want no thumbnail selection", got)
	}
	if got, ok := selectThumbSize("photos", "0x0", []string{"400x0"}); ok {
		t.Fatalf("selectThumbSize() = %q, true; want no thumbnail selection", got)
	}
}
