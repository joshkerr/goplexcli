package plex

import "strings"

// audioStreamType is Plex's streamType value for audio tracks.
const audioStreamType = 2

// audioLanguages collects the distinct language tags of an item's audio
// streams across all media versions and parts, comma-separated in encounter
// order. Untagged streams contribute nothing, so the result is "" when the
// server omitted stream data entirely or no audio track carries a language.
func audioLanguages(metadata sectionMetadata) string {
	var langs []string
	seen := map[string]bool{}
	for _, media := range metadata.Media {
		for _, part := range media.Part {
			for _, s := range part.Stream {
				if s.StreamType == nil || *s.StreamType != audioStreamType {
					continue
				}
				lang := valueOrEmpty(s.LanguageTag)
				if lang == "" {
					lang = valueOrEmpty(s.LanguageCode)
				}
				if lang == "" {
					lang = valueOrEmpty(s.Language)
				}
				lang = strings.TrimSpace(lang)
				key := strings.ToLower(lang)
				if lang == "" || seen[key] {
					continue
				}
				seen[key] = true
				langs = append(langs, lang)
			}
		}
	}
	return strings.Join(langs, ", ")
}

// IsForeignLanguage reports whether the item looks like a non-English-language
// film. Tagged audio tracks are the authoritative signal: if any track is
// English the item is not foreign. When no track languages are known it falls
// back to the metadata agent's originalTitle, which Plex sets when a film's
// native title differs from its display title — overwhelmingly foreign
// releases. Items with neither signal are treated as not foreign, so the
// filter never hides a film it knows nothing about.
func (m *MediaItem) IsForeignLanguage() bool {
	if m.AudioLanguages != "" {
		for _, lang := range strings.Split(m.AudioLanguages, ",") {
			if isEnglishLang(lang) {
				return false
			}
		}
		return true
	}
	return m.OriginalTitle != "" && !strings.EqualFold(m.OriginalTitle, m.Title)
}

// isEnglishLang matches the forms an English audio tag takes across Plex's
// language fields: BCP-47 ("en", "en-US"), ISO 639-2 ("eng"), or the display
// name ("English").
func isEnglishLang(lang string) bool {
	l := strings.ToLower(strings.TrimSpace(lang))
	return l == "en" || l == "eng" || l == "english" || strings.HasPrefix(l, "en-")
}
