package page

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		size    int32
		want    int
		wantLim int32
	}{
		{name: "none named", size: 0, want: DefaultSize, wantLim: DefaultSize + 1},
		{name: "named", size: 3, want: 3, wantLim: 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := New(tc.size)
			if p.Size != tc.want || p.Lim() != tc.wantLim {
				t.Errorf("New(%d) = size %d lim %d, want size %d lim %d", tc.size, p.Size, p.Lim(), tc.want, tc.wantLim)
			}
		})
	}
}

func TestCut(t *testing.T) {
	tests := []struct {
		name     string
		rows     []int
		want     []int
		wantMore bool
	}{
		{name: "short", rows: []int{1}, want: []int{1}},
		{name: "full", rows: []int{1, 2}, want: []int{1, 2}},
		{name: "over", rows: []int{1, 2, 3}, want: []int{1, 2}, wantMore: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, more := Cut(New(2), tc.rows)
			if diff := cmp.Diff(tc.want, got); diff != "" || more != tc.wantMore {
				t.Errorf("Cut(2, %v) = %v more %v, want %v more %v", tc.rows, got, more, tc.want, tc.wantMore)
			}
		})
	}
}

func TestID(t *testing.T) {
	id := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	tests := []struct {
		name    string
		token   string
		want    *uuid.UUID
		wantErr bool
	}{
		{name: "first page", token: ""},
		{name: "a token", token: id.String(), want: &id},
		{name: "malformed", token: "not-a-uuid", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ID(tc.token)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ID(%q) error = %v, wantErr %v", tc.token, err, tc.wantErr)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("ID(%q) mismatch (-want +got):\n%s", tc.token, diff)
			}
		})
	}
}

func TestOrdinal(t *testing.T) {
	seven := int32(7)
	tests := []struct {
		name    string
		token   string
		want    *int32
		wantErr bool
	}{
		{name: "first page", token: ""},
		{name: "a token", token: "7", want: &seven},
		{name: "malformed", token: "seven", wantErr: true},
		{name: "out of range", token: "4294967296", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Ordinal(tc.token)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Ordinal(%q) error = %v, wantErr %v", tc.token, err, tc.wantErr)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Ordinal(%q) mismatch (-want +got):\n%s", tc.token, diff)
			}
		})
	}
}
