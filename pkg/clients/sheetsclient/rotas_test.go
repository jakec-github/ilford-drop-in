package sheetsclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateTabTitle(t *testing.T) {
	tests := []struct {
		name       string
		startDate  string
		shiftCount int
		want       string
		wantErr    bool
	}{
		{
			name:       "single shift",
			startDate:  "2025-01-05",
			shiftCount: 1,
			want:       "Sun Jan 05 2025 - Sun Jan 05 2025",
			wantErr:    false,
		},
		{
			name:       "multiple shifts",
			startDate:  "2025-08-24",
			shiftCount: 12,
			want:       "Sun Aug 24 2025 - Sun Nov 09 2025",
			wantErr:    false,
		},
		{
			name:       "two shifts",
			startDate:  "2025-01-05",
			shiftCount: 2,
			want:       "Sun Jan 05 2025 - Sun Jan 12 2025",
			wantErr:    false,
		},
		{
			name:       "invalid date",
			startDate:  "invalid",
			shiftCount: 1,
			want:       "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := generateTabTitle(tt.startDate, tt.shiftCount)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFindColumnIndex(t *testing.T) {
	header := []interface{}{"Date", "Team lead", "Volunteer 1", "Volunteer 2", "Hot food", "Collection"}

	tests := []struct {
		name       string
		columnName string
		want       int
	}{
		{
			name:       "find date column",
			columnName: "Date",
			want:       0,
		},
		{
			name:       "find team lead column",
			columnName: "Team lead",
			want:       1,
		},
		{
			name:       "find hot food column",
			columnName: "Hot food",
			want:       4,
		},
		{
			name:       "column not found",
			columnName: "Missing",
			want:       -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findColumnIndex(header, tt.columnName)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFindVolunteerColumns(t *testing.T) {
	tests := []struct {
		name   string
		header []interface{}
		want   []int
	}{
		{
			name:   "multiple volunteer columns",
			header: []interface{}{"Date", "Team lead", "Volunteer 1", "Volunteer 2", "Volunteer 3", "Hot food"},
			want:   []int{2, 3, 4},
		},
		{
			name:   "single volunteer column",
			header: []interface{}{"Date", "Team lead", "Volunteer 1", "Hot food"},
			want:   []int{2},
		},
		{
			name:   "no volunteer columns",
			header: []interface{}{"Date", "Team lead", "Hot food"},
			want:   nil,
		},
		{
			name:   "volunteer columns with gaps",
			header: []interface{}{"Date", "Volunteer 1", "Team lead", "Volunteer 2"},
			want:   []int{1, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findVolunteerColumns(tt.header)
			assert.Equal(t, tt.want, got)
		})
	}
}
