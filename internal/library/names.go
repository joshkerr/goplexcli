package library

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// NameInfo is what a folder or file name tells us about a title.
type NameInfo struct {
	Title string // cleaned title, "" if none
	Year  int    // 0 if unknown
	IMDB  string // "tt1234567" or ""
	TMDB  int    // 0 if none
}

// EpisodeInfo is what an episode file name tells us.
type EpisodeInfo struct {
	Show         string // show name parsed from the file name before the SxxEyy tag, cleaned; may be ""
	Season       int
	Episode      int
	LastEpisode  int    // == Episode unless a multi-episode file (S01E01E02 / S01E01-E02)
	EpisodeTitle string // trailing " - Title" segment if present, else ""
	// SeasonKnown is false when the name carried only an episode number
	// ("E05", "Ep05"); callers should take the season from the folder.
	SeasonKnown bool
	// AirDate is set ("2023-06-09") for date-named daily episodes
	// ("Show.2023.06.09…"); Season is then the year and Episode is 0 until
	// the episode is resolved by air date.
	AirDate string
}

// nameMaxYear is the newest 4-digit number that is accepted as a release year
// in a bare (non-parenthesized) position. Anything later ("Blade Runner 2049")
// is part of the title.
var nameMaxYear = time.Now().Year() + 1

const nameMinYear = 1895

var (
	// {imdb-tt123} [tmdbid-456] {TMDB-789} ...
	nameIDTagRe = regexp.MustCompile(`(?i)[\[{]\s*(imdb|imdbid|tmdb|tmdbid)\s*[-:=]\s*([^\]}]*?)\s*[\]}]`)
	// {edition-Director's Cut}, {tvdb-123} and any other brace group.
	nameBraceGroupRe = regexp.MustCompile(`\{[^{}]*\}`)
	// [Group], [1080p], [tvdbid-1] and any other square-bracket group.
	nameSquareGroupRe = regexp.MustCompile(`\[[^\[\]]*\]`)
	// (2024) anywhere in a name.
	nameParenYearRe = regexp.MustCompile(`\((\d{4})\)`)
	// Token separators in scene names.
	nameTokenSepRe = regexp.MustCompile(`[.\s_]+`)

	// S01E04, s1e2, S01.E04, S01 E04, S01-E04. Group 1 = season, group 2 = episode.
	episodeSERe = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9])s(\d{1,4})[ ._-]?e(\d{1,4})`)
	// Continuations of a multi-episode tag: "E02", ".E02", "-E02", "-02".
	episodeSETailRe = regexp.MustCompile(`^(?:[ ._-]?[eE](\d{1,4})|-(\d{1,4}))`)
	// 1x02 with boundaries on both sides. Group 1 = season, group 2 = episode.
	episodeXRe = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9])(\d{1,2})x(\d{1,3})(?:[^A-Za-z0-9]|$)`)
	// Season 1 Episode 2 / Season.01.Episode.02.
	episodeWordsRe = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9])season[ ._-]*(\d{1,2})[ ._-]*episode[ ._-]*(\d{1,3})(?:[^A-Za-z0-9]|$)`)
	// Anime style "Show - 01" / "Show - 01v2" (after bracket groups are removed).
	// Group 1 = show, group 2 = episode.
	episodeAnimeRe = regexp.MustCompile(`^\s*(.+?)\s+-\s+(\d{1,3})(?:[vV]\d+)?(?:[^A-Za-z0-9]|$)`)
	// " - Episode Title" after an episode tag.
	episodeTitleRe = regexp.MustCompile(`^\s+-\s+(.+)$`)
	// Daily shows are named by air date: "Jeopardy!.2023.06.09.720p…".
	episodeDateRe = regexp.MustCompile(`(?:^|[^0-9])((?:19|20)\d\d)[ ._-](0[1-9]|1[0-2])[ ._-](0[1-9]|[12]\d|3[01])(?:[^0-9]|$)`)
	// Episode-only tags, optionally preceded by a season: "E01", "Ep20",
	// "Episode 20", "S08.Ep20".
	episodeOnlyRe = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9])(?:s(\d{1,4})[ ._-]?)?(?:episode|ep|e)[ ._-]?(\d{1,4})(?:[^0-9]|$)`)
	// A lone three-digit token inside a show folder is season+episode
	// ("mork.and.mindy.318" = S03E18).
	episodeCombinedRe = regexp.MustCompile(`(?:^|[^0-9A-Za-z])(\d)(\d{2})(?:[^0-9A-Za-z]|$)`)

	seasonFolderRe   = regexp.MustCompile(`(?i)^season[ ._-]*(\d{1,4})(?:[^0-9].*)?$`)
	seasonShortRe    = regexp.MustCompile(`(?i)^s(\d{1,4})$`)
	seasonSpecialsRe = regexp.MustCompile(`(?i)^specials?$`)
	trailingParenYr  = regexp.MustCompile(`^\(\d{4}\)$`)
)

// nameStrongStops are tokens that unambiguously mark the end of a title
// (resolutions, sources, codecs, audio) wherever they appear.
var nameStrongStops = map[string]bool{
	// resolutions
	"480p": true, "576p": true, "720p": true, "1080p": true, "1080i": true, "2160p": true, "4k": true, "uhd": true,
	// sources
	"bluray": true, "blu-ray": true, "bdrip": true, "brrip": true, "web-dl": true, "webdl": true, "webrip": true,
	"hdtv": true, "dvdrip": true, "hdrip": true, "telesync": true, "hdcam": true, "remux": true,
	// codecs
	"x264": true, "x265": true, "h264": true, "h265": true, "hevc": true, "avc": true, "xvid": true, "divx": true,
	"av1": true, "10bit": true, "hdr": true, "hdr10": true, "dovi": true,
	// audio
	"aac": true, "ac3": true, "eac3": true, "dts": true, "truehd": true, "atmos": true, "flac": true, "mp3": true,
	"dd5": true, "ddp5": true, "ddp": true, "dd": true,
}

// nameWeakStops are tokens that may legitimately appear inside a title, so
// they only end a title once a year or a strong stop token has been seen.
var nameWeakStops = map[string]bool{
	"web": true, "dvd": true, "cam": true, "ts": true, "amzn": true, "nf": true, "pcok": true, "dsnp": true,
	"hmax": true, "atvp": true, "max": true, "hulu": true, "itunes": true, "dv": true,
	"proper": true, "repack": true, "internal": true, "limited": true, "extended": true, "unrated": true,
	"remastered": true, "directors": true, "cut": true, "multi": true, "dubbed": true, "subbed": true,
	"complete": true, "season": true,
	"5": true, "7": true, "2": true, "1": true, "0": true, // pieces of 5.1 / 7.1 / 2.0
}

var videoExtensions = map[string]bool{
	".mkv": true, ".mp4": true, ".m4v": true, ".avi": true, ".mov": true, ".wmv": true, ".webm": true,
	".ts": true, ".m2ts": true, ".mpg": true, ".mpeg": true, ".flv": true,
}

var ignoredExactNames = map[string]bool{
	"sample": true, "samples": true,
	"behind the scenes": true, "deleted scenes": true, "featurettes": true, "interviews": true,
	"scenes": true, "shorts": true, "trailers": true, "other": true, "extras": true,
	"@eadir": true, "#recycle": true, "lost+found": true, "$recycle.bin": true, "system volume information": true,
}

var ignoredSuffixes = []string{
	"-trailer", "-sample", "-behindthescenes", "-deleted", "-featurette", "-interview", "-scene", "-short", "-other",
}

var simpleExtRe = regexp.MustCompile(`^\.[A-Za-z0-9]{1,5}$`)

// ParseTitleFolder parses a Plex-style "Title (Year) [imdb-tt…] {tmdb-…}" folder name.
//
// Only a parenthesized year counts for folders, so "Blade Runner 2049" without a
// "(2017)" keeps the number in the title. The last "(YYYY)" wins, which keeps
// "Accused (US) (2023)" and "2012 (2009)" correct.
func ParseTitleFolder(name string) NameInfo {
	info, rest := extractNameIDs(name)
	rest = nameBraceGroupRe.ReplaceAllString(rest, " ")

	title := rest
	if locs := nameParenYearRe.FindAllStringSubmatchIndex(rest, -1); len(locs) > 0 {
		last := locs[len(locs)-1]
		if y, _ := strconv.Atoi(rest[last[2]:last[3]]); y >= 1800 && y <= 2999 {
			info.Year = y
			title = rest[:last[0]]
		}
	}
	info.Title = CleanTitle(stripSquareGroups(title))
	return info
}

// ParseReleaseName parses a scene-style or plain file name (extension optional) into title + year
// (+ids if embedded in brackets).
//
// A "(YYYY)" wins outright. Otherwise the title ends at the first unambiguous
// quality token (resolution/source/codec/audio) and the year is the LAST
// plausible 4-digit token before that point, so "Blade.Runner.2049.2017.2160p"
// yields year 2017 and "2012.2009.720p" yields title "2012", year 2009. A name
// that is only a year-like number ("1917.1080p") keeps it as the title with
// year 0. Ambiguous tokens such as "ts", "nf", "web" or "max" never cut a title
// on their own.
func ParseReleaseName(name string) NameInfo {
	name = stripVideoExt(name)
	info, rest := extractNameIDs(name)
	rest = nameBraceGroupRe.ReplaceAllString(rest, " ")
	rest = stripSquareGroups(rest)

	if locs := nameParenYearRe.FindAllStringSubmatchIndex(rest, -1); len(locs) > 0 {
		last := locs[len(locs)-1]
		if y, _ := strconv.Atoi(rest[last[2]:last[3]]); y >= 1800 && y <= 2999 {
			info.Year = y
			info.Title = CleanTitle(rest[:last[0]])
			return info
		}
	}

	toks := splitNameTokens(rest)
	end := len(toks)
	for i, t := range toks {
		if isStrongStopToken(t) {
			end = i
			break
		}
	}
	for i := end - 1; i > 0; i-- {
		if y, ok := bareYear(toks[i]); ok {
			info.Year = y
			end = i
			break
		}
	}
	info.Title = CleanTitle(strings.Join(toks[:end], " "))
	return info
}

// ParseEpisodeFile parses an episode file name. ok is false if no season/episode tag is found.
//
// Recognized tags, in order of preference: SxxEyy (with E02 / -E02 / -02
// continuations for multi-episode files), NxNN, "Season N Episode M", and
// finally the anime-style "Show - 01" form, which is treated as season 1 only
// when none of the other tags is present. Show drops a trailing year
// ("Show.Name.2019.S02E10" -> "Show Name") so it groups with the show folder.
func ParseEpisodeFile(name string) (EpisodeInfo, bool) {
	name = stripVideoExt(name)
	var ep EpisodeInfo

	if m := episodeSERe.FindStringSubmatchIndex(name); m != nil {
		ep.Season, _ = strconv.Atoi(name[m[2]:m[3]])
		ep.Episode, _ = strconv.Atoi(name[m[4]:m[5]])
		ep.LastEpisode = ep.Episode
		pos := m[5]
		for {
			t := episodeSETailRe.FindStringSubmatchIndex(name[pos:])
			if t == nil {
				break
			}
			var numStart, numEnd int
			if t[2] >= 0 {
				numStart, numEnd = t[2], t[3]
			} else {
				numStart, numEnd = t[4], t[5]
			}
			if !atTokenBoundary(name, pos+t[1]) && !episodeSETailRe.MatchString(name[pos+t[1]:]) {
				break
			}
			n, _ := strconv.Atoi(name[pos+numStart : pos+numEnd])
			if n < ep.Episode {
				break
			}
			ep.LastEpisode = n
			pos += t[1]
		}
		if !atTokenBoundary(name, pos) {
			return EpisodeInfo{}, false
		}
		ep.Show = cleanShowName(name[:m[0]])
		ep.EpisodeTitle = trailingEpisodeTitle(name[pos:])
		ep.SeasonKnown = true
		return ep, true
	}

	if m := episodeXRe.FindStringSubmatchIndex(name); m != nil {
		ep.Season, _ = strconv.Atoi(name[m[2]:m[3]])
		ep.Episode, _ = strconv.Atoi(name[m[4]:m[5]])
		ep.LastEpisode = ep.Episode
		ep.Show = cleanShowName(name[:m[0]])
		ep.EpisodeTitle = trailingEpisodeTitle(name[m[5]:])
		ep.SeasonKnown = true
		return ep, true
	}

	if m := episodeWordsRe.FindStringSubmatchIndex(name); m != nil {
		ep.Season, _ = strconv.Atoi(name[m[2]:m[3]])
		ep.Episode, _ = strconv.Atoi(name[m[4]:m[5]])
		ep.LastEpisode = ep.Episode
		ep.Show = cleanShowName(name[:m[0]])
		ep.EpisodeTitle = trailingEpisodeTitle(name[m[5]:])
		ep.SeasonKnown = true
		return ep, true
	}

	if m := episodeDateRe.FindStringSubmatchIndex(name); m != nil {
		ep.Season, _ = strconv.Atoi(name[m[2]:m[3]])
		ep.AirDate = name[m[2]:m[3]] + "-" + name[m[4]:m[5]] + "-" + name[m[6]:m[7]]
		ep.Show = cleanShowName(name[:m[0]])
		ep.SeasonKnown = true
		return ep, true
	}

	if m := episodeOnlyRe.FindStringSubmatchIndex(name); m != nil {
		if m[2] >= 0 {
			ep.Season, _ = strconv.Atoi(name[m[2]:m[3]])
			ep.SeasonKnown = true
		}
		ep.Episode, _ = strconv.Atoi(name[m[4]:m[5]])
		ep.LastEpisode = ep.Episode
		ep.Show = cleanShowName(name[:m[0]])
		ep.EpisodeTitle = trailingEpisodeTitle(name[m[5]:])
		return ep, true
	}

	stripped := nameBraceGroupRe.ReplaceAllString(nameSquareGroupRe.ReplaceAllString(name, " "), " ")
	if m := episodeAnimeRe.FindStringSubmatchIndex(stripped); m != nil {
		ep.Season = 1
		ep.Episode, _ = strconv.Atoi(stripped[m[4]:m[5]])
		ep.LastEpisode = ep.Episode
		ep.Show = cleanShowName(stripped[m[2]:m[3]])
		ep.EpisodeTitle = trailingEpisodeTitle(stripped[m[5]:])
		return ep, true
	}

	if m := episodeCombinedRe.FindStringSubmatchIndex(name); m != nil {
		ep.Season, _ = strconv.Atoi(name[m[2]:m[3]])
		ep.Episode, _ = strconv.Atoi(name[m[4]:m[5]])
		ep.LastEpisode = ep.Episode
		ep.Show = cleanShowName(name[:m[0]])
		ep.SeasonKnown = true
		return ep, true
	}

	return EpisodeInfo{}, false
}

// ParseSeasonFolder parses "Season 01" / "S01" / "Specials"; ok=false if the name is not a season folder.
func ParseSeasonFolder(name string) (season int, ok bool) {
	name = strings.TrimSpace(name)
	if seasonSpecialsRe.MatchString(name) {
		return 0, true
	}
	for _, re := range []*regexp.Regexp{seasonFolderRe, seasonShortRe} {
		if m := re.FindStringSubmatch(name); m != nil {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				return 0, false
			}
			return n, true
		}
	}
	return 0, false
}

// IsVideoFile reports whether the name has a known video extension
// (.mkv .mp4 .m4v .avi .mov .wmv .webm .ts .m2ts .mpg .mpeg .flv; .iso is NOT included).
func IsVideoFile(name string) bool {
	return videoExtensions[strings.ToLower(filepath.Ext(name))]
}

// IsIgnoredName reports whether a file or directory base name should be skipped while scanning:
// dotfiles, names starting with "._", "sample"/"samples" files and dirs, Plex extras folders
// (behind the scenes, deleted scenes, featurettes, interviews, scenes, shorts, trailers, other, extras),
// file names ending in "-trailer", "-sample", "-behindthescenes", "-deleted", "-featurette",
// "-interview", "-scene", "-short", "-other" (before the extension), and "@eaDir", "#recycle",
// "lost+found", "$RECYCLE.BIN", "System Volume Information".
func IsIgnoredName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || strings.HasPrefix(name, ".") {
		return true
	}
	lower := strings.ToLower(name)
	if ignoredExactNames[lower] {
		return true
	}
	stem := lower
	if ext := filepath.Ext(lower); simpleExtRe.MatchString(ext) {
		stem = strings.TrimSuffix(lower, ext)
	}
	if ignoredExactNames[stem] {
		return true
	}
	for _, suf := range ignoredSuffixes {
		if strings.HasSuffix(stem, suf) {
			return true
		}
	}
	return false
}

// CleanTitle turns dots/underscores into spaces, collapses whitespace, trims separators, and
// preserves apostrophes and intentional punctuation like "9-1-1" (a hyphen between digits/letters
// without surrounding spaces stays; " - " stays as " - ").
func CleanTitle(s string) string {
	s = strings.NewReplacer(".", " ", "_", " ").Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	return strings.Trim(s, " -")
}

// TitleKey returns a normalized key for grouping/comparing titles: lowercase alphanumerics only
// with "the"/"a"/"an" leading articles kept (do not drop them), e.g. "A Discovery of Witches" → "adiscoveryofwitches".
func TitleKey(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// extractNameIDs pulls [imdb-tt…]/{tmdb-…} style tags out of name, returning the
// ids found and the name with those tags replaced by spaces.
func extractNameIDs(name string) (NameInfo, string) {
	var info NameInfo
	rest := nameIDTagRe.ReplaceAllStringFunc(name, func(tag string) string {
		m := nameIDTagRe.FindStringSubmatch(tag)
		kind := strings.ToLower(m[1])
		val := strings.ToLower(strings.TrimSpace(m[2]))
		switch kind {
		case "imdb", "imdbid":
			if info.IMDB == "" {
				switch {
				case isAllDigits(strings.TrimPrefix(val, "tt")) && strings.HasPrefix(val, "tt"):
					info.IMDB = val
				case isAllDigits(val) && val != "":
					info.IMDB = "tt" + val
				}
			}
		case "tmdb", "tmdbid":
			if info.TMDB == 0 {
				if n, err := strconv.Atoi(val); err == nil && n > 0 {
					info.TMDB = n
				}
			}
		}
		return " "
	})
	return info, rest
}

// stripSquareGroups removes [bracketed] groups unless doing so would leave
// nothing at all (so "[REC]" keeps its brackets).
func stripSquareGroups(s string) string {
	out := nameSquareGroupRe.ReplaceAllString(s, " ")
	if strings.TrimSpace(out) == "" {
		return s
	}
	return out
}

func stripVideoExt(name string) string {
	if IsVideoFile(name) {
		return strings.TrimSuffix(name, filepath.Ext(name))
	}
	return name
}

func splitNameTokens(s string) []string {
	var toks []string
	for _, t := range nameTokenSepRe.Split(s, -1) {
		if t != "" {
			toks = append(toks, t)
		}
	}
	return toks
}

// isStrongStopToken reports whether tok (optionally carrying a "-Group" suffix,
// as in "x264-Kitsune") is an unambiguous quality marker.
func isStrongStopToken(tok string) bool {
	lower := strings.ToLower(tok)
	if nameStrongStops[lower] {
		return true
	}
	if i := strings.IndexByte(lower, '-'); i > 0 && nameStrongStops[lower[:i]] {
		return true
	}
	return false
}

// isAnyStopToken reports whether tok is a strong or weak quality marker; it is
// only consulted after a year or an episode tag has already been passed.
func isAnyStopToken(tok string) bool {
	if isStrongStopToken(tok) {
		return true
	}
	lower := strings.ToLower(tok)
	if nameWeakStops[lower] {
		return true
	}
	if i := strings.IndexByte(lower, '-'); i > 0 && nameWeakStops[lower[:i]] {
		return true
	}
	return false
}

func bareYear(tok string) (int, bool) {
	if len(tok) != 4 || !isAllDigits(tok) {
		return 0, false
	}
	y, _ := strconv.Atoi(tok)
	if y < nameMinYear || y > nameMaxYear {
		return 0, false
	}
	return y, true
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// atTokenBoundary reports whether position i in s is the end of the string or
// is followed by a non-alphanumeric character.
func atTokenBoundary(s string, i int) bool {
	if i >= len(s) {
		return true
	}
	c := s[i]
	return !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z')
}

// cleanShowName cleans the text before an episode tag and drops a trailing
// year ("Show.Name.2019" / "Show Name (2019)") unless that would leave nothing.
func cleanShowName(s string) string {
	_, s = extractNameIDs(s)
	s = nameBraceGroupRe.ReplaceAllString(s, " ")
	s = stripSquareGroups(s)
	s = CleanTitle(s)
	fields := strings.Fields(s)
	if len(fields) > 1 {
		last := fields[len(fields)-1]
		if _, ok := bareYear(last); ok || trailingParenYr.MatchString(last) {
			s = CleanTitle(strings.Join(fields[:len(fields)-1], " "))
		}
	}
	return s
}

// trailingEpisodeTitle extracts an " - Episode Title" segment that follows an
// episode tag, stopping at the next " - " segment or the first quality token.
func trailingEpisodeTitle(rest string) string {
	rest = nameBraceGroupRe.ReplaceAllString(nameSquareGroupRe.ReplaceAllString(rest, " "), " ")
	m := episodeTitleRe.FindStringSubmatch(rest)
	if m == nil {
		return ""
	}
	cand := m[1]
	if i := strings.Index(cand, " - "); i >= 0 {
		cand = cand[:i]
	}
	toks := splitNameTokens(cand)
	end := len(toks)
	for i, t := range toks {
		if isAnyStopToken(t) {
			end = i
			break
		}
	}
	return CleanTitle(strings.Join(toks[:end], " "))
}
