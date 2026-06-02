package plex

import "testing"

func TestConvertToRclonePath(t *testing.T) {
	tests := []struct {
		name     string
		mappings []PathMapping
		filePath string
		want     string
	}{
		{
			name:     "empty path",
			filePath: "",
			want:     "",
		},
		{
			name:     "no mappings falls back to legacy heuristic",
			filePath: "/home/joshkerr/plexcloudservers2/Media/TV/Show/ep.mkv",
			want:     "plexcloudservers2:Media/TV/Show/ep.mkv",
		},
		{
			name: "single mapping applied",
			mappings: []PathMapping{
				{Prefix: "/mnt/media/", Remote: "gdrive:"},
			},
			filePath: "/mnt/media/Movies/Film (2020)/film.mkv",
			want:     "gdrive:Movies/Film (2020)/film.mkv",
		},
		{
			name: "longest prefix wins over shorter overlapping prefix",
			mappings: []PathMapping{
				{Prefix: "/mnt/", Remote: "root:"},
				{Prefix: "/mnt/media/tv/", Remote: "tv:"},
			},
			filePath: "/mnt/media/tv/Show/ep.mkv",
			want:     "tv:Show/ep.mkv",
		},
		{
			name: "unmatched mapping falls back to legacy",
			mappings: []PathMapping{
				{Prefix: "/srv/", Remote: "srv:"},
			},
			filePath: "/home/joshkerr/remote1/Media/x.mkv",
			want:     "remote1:Media/x.mkv",
		},
		{
			name: "empty prefix mapping is ignored",
			mappings: []PathMapping{
				{Prefix: "", Remote: "bogus:"},
				{Prefix: "/data/", Remote: "data:"},
			},
			filePath: "/data/Movies/x.mkv",
			want:     "data:Movies/x.mkv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{pathMappings: tt.mappings}
			got := c.convertToRclonePath(tt.filePath)
			if got != tt.want {
				t.Errorf("convertToRclonePath(%q) = %q, want %q", tt.filePath, got, tt.want)
			}
		})
	}
}
