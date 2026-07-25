package plex

import (
	"encoding/json"
	"testing"
)

func TestIsForeignLanguage(t *testing.T) {
	cases := []struct {
		name string
		item MediaItem
		want bool
	}{
		{"no signals at all", MediaItem{Title: "Heat"}, false},
		{"english audio tag", MediaItem{Title: "Heat", AudioLanguages: "en"}, false},
		{"english among others", MediaItem{Title: "Heat", AudioLanguages: "fr, en, de"}, false},
		{"iso 639-2 english", MediaItem{Title: "Heat", AudioLanguages: "eng"}, false},
		{"regional english", MediaItem{Title: "Heat", AudioLanguages: "en-US"}, false},
		{"display-name english", MediaItem{Title: "Heat", AudioLanguages: "English"}, false},
		{"foreign audio only", MediaItem{Title: "Oldboy", AudioLanguages: "ko"}, true},
		{"multiple foreign tracks", MediaItem{Title: "Amélie", AudioLanguages: "fr, de"}, true},
		{
			"audio known beats originalTitle",
			MediaItem{Title: "Hate", OriginalTitle: "La Haine", AudioLanguages: "en"},
			false,
		},
		{
			"originalTitle fallback flags foreign",
			MediaItem{Title: "Hate", OriginalTitle: "La Haine"},
			true,
		},
		{
			"identical originalTitle is not foreign",
			MediaItem{Title: "Roma", OriginalTitle: "Roma"},
			false,
		},
		{
			"originalTitle compare is case-insensitive",
			MediaItem{Title: "ROMA", OriginalTitle: "Roma"},
			false,
		},
	}
	for _, tc := range cases {
		if got := tc.item.IsForeignLanguage(); got != tc.want {
			t.Errorf("%s: IsForeignLanguage() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestAudioLanguagesExtraction(t *testing.T) {
	// A section item as returned with includeStreams=1: video stream first,
	// then audio tracks (one untagged), then a subtitle stream that must not
	// leak into the audio languages.
	payload := `{
		"key": "/library/metadata/1",
		"title": "Oldboy",
		"originalTitle": "올드보이",
		"Media": [{"Part": [{
			"file": "/media/oldboy.mkv",
			"Stream": [
				{"streamType": 1},
				{"streamType": 2, "language": "Korean", "languageTag": "ko", "languageCode": "kor"},
				{"streamType": 2, "language": "Korean", "languageTag": "ko", "languageCode": "kor"},
				{"streamType": 2},
				{"streamType": 3, "language": "English", "languageTag": "en"}
			]
		}]}]
	}`
	var meta sectionMetadata
	if err := json.Unmarshal([]byte(payload), &meta); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := audioLanguages(meta); got != "ko" {
		t.Errorf("audioLanguages = %q, want %q (dedup, audio-only, skip untagged)", got, "ko")
	}
	if meta.OriginalTitle == nil || *meta.OriginalTitle != "올드보이" {
		t.Errorf("originalTitle not parsed: %v", meta.OriginalTitle)
	}

	// No stream data (server ignored includeStreams) → empty, not a panic.
	var bare sectionMetadata
	if err := json.Unmarshal([]byte(`{"key":"/library/metadata/2","title":"Heat"}`), &bare); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := audioLanguages(bare); got != "" {
		t.Errorf("audioLanguages with no stream data = %q, want empty", got)
	}
}
