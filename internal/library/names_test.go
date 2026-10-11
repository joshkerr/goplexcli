package library

import "testing"

func TestParseTitleFolder(t *testing.T) {
	tests := []struct {
		in   string
		want NameInfo
	}{
		{"A '90s Christmas (2024) [imdb-tt16270280]", NameInfo{Title: "A '90s Christmas", Year: 2024, IMDB: "tt16270280"}},
		{"The 'Burbs (1989)", NameInfo{Title: "The 'Burbs", Year: 1989}},
		{"The (Ex)perience of Love (2023) [imdb-tt27812279]", NameInfo{Title: "The (Ex)perience of Love", Year: 2023, IMDB: "tt27812279"}},
		{"-batteries not included (1987)", NameInfo{Title: "batteries not included", Year: 1987}},
		{"#1 Cheerleader Camp (2010) [imdb-tt1637976]", NameInfo{Title: "#1 Cheerleader Camp", Year: 2010, IMDB: "tt1637976"}},
		{"Blade Runner 2049 (2017) {tmdb-335984}", NameInfo{Title: "Blade Runner 2049", Year: 2017, TMDB: 335984}},
		{"Blade Runner 2049 (2017) [tmdbid-335984] [imdbid-tt1856101]", NameInfo{Title: "Blade Runner 2049", Year: 2017, TMDB: 335984, IMDB: "tt1856101"}},
		{"Blade Runner 2049 (2017) {IMDB-TT1856101} {TMDBID-335984}", NameInfo{Title: "Blade Runner 2049", Year: 2017, TMDB: 335984, IMDB: "tt1856101"}},
		{"Movie (2001) [imdb-1856101]", NameInfo{Title: "Movie", Year: 2001, IMDB: "tt1856101"}},
		{"Movie (2001) {edition-Director's Cut} {tmdb-12}", NameInfo{Title: "Movie", Year: 2001, TMDB: 12}},
		{"Some Movie", NameInfo{Title: "Some Movie"}},
		{"Blade Runner 2049", NameInfo{Title: "Blade Runner 2049"}},
		{"Adolescence (2025) [imdb-tt31806037]", NameInfo{Title: "Adolescence", Year: 2025, IMDB: "tt31806037"}},
		{"9-1-1 - Lone Star (2020) [imdb-tt10323338]", NameInfo{Title: "9-1-1 - Lone Star", Year: 2020, IMDB: "tt10323338"}},
		{"A Discovery of Witches", NameInfo{Title: "A Discovery of Witches"}},
		{"Accused (US) (2023) [imdb-tt7949212]", NameInfo{Title: "Accused (US)", Year: 2023, IMDB: "tt7949212"}},
		{"Accused (US)", NameInfo{Title: "Accused (US)"}},
		{"2012 (2009)", NameInfo{Title: "2012", Year: 2009}},
		{"1917 (2019)", NameInfo{Title: "1917", Year: 2019}},
		{"1883 (2021)", NameInfo{Title: "1883", Year: 2021}},
		{"[REC] (2007)", NameInfo{Title: "[REC]", Year: 2007}},
		{"Some Movie (2020) [1080p]", NameInfo{Title: "Some Movie", Year: 2020}},
		{"Some.Movie.With.Dots (2020)", NameInfo{Title: "Some Movie With Dots", Year: 2020}},
		{"  Padded Title  (1999) ", NameInfo{Title: "Padded Title", Year: 1999}},
		{"", NameInfo{}},
	}
	for _, tc := range tests {
		if got := ParseTitleFolder(tc.in); got != tc.want {
			t.Errorf("ParseTitleFolder(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

func TestParseReleaseName(t *testing.T) {
	tests := []struct {
		in   string
		want NameInfo
	}{
		{"A.'90s.Christmas.2024.PCOK.1080p.WEB-DL.EAC3.5.1.x264-Kitsune.mkv", NameInfo{Title: "A '90s Christmas", Year: 2024}},
		{"The.'Burbs.1989.1080p.BluRay.FLAC.2.0.x264-CtrlHD.mkv", NameInfo{Title: "The 'Burbs", Year: 1989}},
		{"Blade.Runner.2049.2017.2160p.UHD.BluRay.x265-TERMiNAL.mkv", NameInfo{Title: "Blade Runner 2049", Year: 2017}},
		{"-batteries.not.included.1987.1080p.BluRay.EAC3.5.1.x264-playHD.mkv", NameInfo{Title: "batteries not included", Year: 1987}},
		{"Movie Title (2001).mp4", NameInfo{Title: "Movie Title", Year: 2001}},
		{"Movie Title (2001) 1080p BluRay.mp4", NameInfo{Title: "Movie Title", Year: 2001}},
		{"Movie_Title_2001_720p.mkv", NameInfo{Title: "Movie Title", Year: 2001}},
		{"Some Movie.mkv", NameInfo{Title: "Some Movie"}},
		{"Some Movie", NameInfo{Title: "Some Movie"}},
		{"The.(Ex)perience.of.Love.2023.1080p.WEB.h264-EDITH.mkv", NameInfo{Title: "The (Ex)perience of Love", Year: 2023}},
		{"#1.Cheerleader.Camp.2010.DVDRip.XviD-GRP.avi", NameInfo{Title: "#1 Cheerleader Camp", Year: 2010}},
		// year-like numbers in titles
		{"2012.2009.720p.BluRay.x264.mkv", NameInfo{Title: "2012", Year: 2009}},
		{"1917.2019.1080p.BluRay.x264-SPARKS.mkv", NameInfo{Title: "1917", Year: 2019}},
		{"1883.2021.1080p.mkv", NameInfo{Title: "1883", Year: 2021}},
		{"1917.1080p.BluRay.mkv", NameInfo{Title: "1917"}},                                // only token before quality: keep as title, year unknown
		{"Blade.Runner.2049.2160p.mkv", NameInfo{Title: "Blade Runner 2049"}},             // 2049 is not a plausible release year
		{"Movie.Title.2001.mkv", NameInfo{Title: "Movie Title", Year: 2001}},              // trailing year, no quality tags
		{"Movie.Title.2001.Extended.Cut.mkv", NameInfo{Title: "Movie Title", Year: 2001}}, // weak tokens after the year are dropped
		// ambiguous tokens stay in the title when no year/strong token precedes them
		{"Max.Payne.2008.720p.mkv", NameInfo{Title: "Max Payne", Year: 2008}},
		{"Web.of.Lies.mkv", NameInfo{Title: "Web of Lies"}},
		{"TS.Spivet.2013.1080p.mkv", NameInfo{Title: "TS Spivet", Year: 2013}},
		{"Some.Movie.TS.mkv", NameInfo{Title: "Some Movie TS"}},
		// ids embedded in brackets
		{"Movie.Title.2001.1080p.x264-GRP [imdb-tt0000001].mkv", NameInfo{Title: "Movie Title", Year: 2001, IMDB: "tt0000001"}},
		{"Movie Title (2001) {tmdb-42}.mkv", NameInfo{Title: "Movie Title", Year: 2001, TMDB: 42}},
		// bracket groups are stripped
		{"[Group] Show - 01 [1080p].mkv", NameInfo{Title: "Show - 01"}},
		{"Movie Title [2001].mkv", NameInfo{Title: "Movie Title"}},
		// weird extensions are not stripped (only known video extensions are)
		{"Blade.Runner.2049.2017.2160p", NameInfo{Title: "Blade Runner 2049", Year: 2017}},
		{"Some.Movie.iso", NameInfo{Title: "Some Movie iso"}},
		{"Some.Movie.2001.iso", NameInfo{Title: "Some Movie", Year: 2001}}, // year still wins; trailing junk dropped
		// separators
		{"9-1-1.Lone.Star.2020.1080p.mkv", NameInfo{Title: "9-1-1 Lone Star", Year: 2020}},
		{"Show - Title - 2001 - 1080p.mkv", NameInfo{Title: "Show - Title", Year: 2001}},
		{"", NameInfo{}},
		{".mkv", NameInfo{}},
	}
	for _, tc := range tests {
		if got := ParseReleaseName(tc.in); got != tc.want {
			t.Errorf("ParseReleaseName(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

func TestParseEpisodeFile(t *testing.T) {
	tests := []struct {
		in   string
		want EpisodeInfo
		ok   bool
	}{
		{"Adolescence.S01E04.NF.1080p.WEB-DL.x264.EAC3.Atmos.5.1-FLUX.mkv", EpisodeInfo{Show: "Adolescence", Season: 1, Episode: 4, LastEpisode: 4}, true},
		{"Come.Dine.with.Me.S2023E38.ALL4.1080p.WEB-DL.x264.AAC.2.0-NioN.mkv", EpisodeInfo{Show: "Come Dine with Me", Season: 2023, Episode: 38, LastEpisode: 38}, true},
		{"Jeopardy!.2023.06.09.720p.HDTV.x264.AC3.5.1-NGP.mkv", EpisodeInfo{Show: "Jeopardy!", Season: 2023, AirDate: "2023-06-09"}, true},
		{"Love.Island.2020-02-16.1080p.WEBDL.h264-TEPES.mkv", EpisodeInfo{Show: "Love Island", Season: 2020, AirDate: "2020-02-16"}, true},
		{"S.G.N.T.E01.1080p.HDTV.x264-CHI.mkv", EpisodeInfo{Show: "S G N T", Episode: 1, LastEpisode: 1}, true},
		{"Family.Guy.S08.Ep20.Something.Something.2010.1080P.BluRay.mkv", EpisodeInfo{Show: "Family Guy", Season: 8, Episode: 20, LastEpisode: 20}, true},
		{"Show.Name.Episode.7.mkv", EpisodeInfo{Show: "Show Name", Episode: 7, LastEpisode: 7}, true},
		{"mork.and.mindy.318.dvdrip-dimension.avi", EpisodeInfo{Show: "mork and mindy", Season: 3, Episode: 18, LastEpisode: 18}, true},
		{"james.mays.toy.stories.104-ifh.avi", EpisodeInfo{Show: "james mays toy stories", Season: 1, Episode: 4, LastEpisode: 4}, true},
		{"The.Daily.Show.S2024E1234.mkv", EpisodeInfo{Show: "The Daily Show", Season: 2024, Episode: 1234, LastEpisode: 1234}, true},
		{"Show.S01E01E02.mkv", EpisodeInfo{Show: "Show", Season: 1, Episode: 1, LastEpisode: 2}, true},
		{"Show.S01E01-E02.mkv", EpisodeInfo{Show: "Show", Season: 1, Episode: 1, LastEpisode: 2}, true},
		{"Show.S01E01-02.mkv", EpisodeInfo{Show: "Show", Season: 1, Episode: 1, LastEpisode: 2}, true},
		{"Show.S01E01E02E03.mkv", EpisodeInfo{Show: "Show", Season: 1, Episode: 1, LastEpisode: 3}, true},
		{"Show.S01E05-720p.mkv", EpisodeInfo{Show: "Show", Season: 1, Episode: 5, LastEpisode: 5}, true}, // "-720" is not an episode continuation
		{"show.s1e2.mkv", EpisodeInfo{Show: "show", Season: 1, Episode: 2, LastEpisode: 2}, true},
		{"Show.S01.E02.mkv", EpisodeInfo{Show: "Show", Season: 1, Episode: 2, LastEpisode: 2}, true},
		{"Show - 1x02 - Title.mkv", EpisodeInfo{Show: "Show", Season: 1, Episode: 2, LastEpisode: 2, EpisodeTitle: "Title"}, true},
		{"show.1x02.720p.mkv", EpisodeInfo{Show: "show", Season: 1, Episode: 2, LastEpisode: 2}, true},
		{"Show.Name.2019.S02E10.720p.mkv", EpisodeInfo{Show: "Show Name", Season: 2, Episode: 10, LastEpisode: 10}, true},
		{"Show Name (2019) - S02E10 - Title.mkv", EpisodeInfo{Show: "Show Name", Season: 2, Episode: 10, LastEpisode: 10, EpisodeTitle: "Title"}, true},
		{"Show - S01E05 - Episode Title.mkv", EpisodeInfo{Show: "Show", Season: 1, Episode: 5, LastEpisode: 5, EpisodeTitle: "Episode Title"}, true},
		{"Show - S01E05 - Episode Title - 1080p.mkv", EpisodeInfo{Show: "Show", Season: 1, Episode: 5, LastEpisode: 5, EpisodeTitle: "Episode Title"}, true},
		{"Show - S01E05 - Episode Title 1080p WEB-DL.mkv", EpisodeInfo{Show: "Show", Season: 1, Episode: 5, LastEpisode: 5, EpisodeTitle: "Episode Title"}, true},
		{"Show - S01E05 - 1080p.mkv", EpisodeInfo{Show: "Show", Season: 1, Episode: 5, LastEpisode: 5}, true}, // quality token is not a title
		{"Show - S01E05 - WEB.mkv", EpisodeInfo{Show: "Show", Season: 1, Episode: 5, LastEpisode: 5}, true},   // weak tokens count after the tag
		{"Show.S01E05-FLUX.mkv", EpisodeInfo{Show: "Show", Season: 1, Episode: 5, LastEpisode: 5}, true},      // group tag is not a title
		{"9-1-1 - Lone Star - S03E01 - Pilot.mkv", EpisodeInfo{Show: "9-1-1 - Lone Star", Season: 3, Episode: 1, LastEpisode: 1, EpisodeTitle: "Pilot"}, true},
		{"Accused (US) - S01E01.mkv", EpisodeInfo{Show: "Accused (US)", Season: 1, Episode: 1, LastEpisode: 1}, true},
		{"1883.S01E01.1080p.mkv", EpisodeInfo{Show: "1883", Season: 1, Episode: 1, LastEpisode: 1}, true}, // lone year-like show name is kept
		{"S01E03.mkv", EpisodeInfo{Season: 1, Episode: 3, LastEpisode: 3}, true},
		{"[Group] Show S01E02 [1080p].mkv", EpisodeInfo{Show: "Show", Season: 1, Episode: 2, LastEpisode: 2}, true},
		{"Show.Season.1.Episode.2.mkv", EpisodeInfo{Show: "Show", Season: 1, Episode: 2, LastEpisode: 2}, true},
		{"Show Season 1 Episode 2 - Title.mkv", EpisodeInfo{Show: "Show", Season: 1, Episode: 2, LastEpisode: 2, EpisodeTitle: "Title"}, true},
		// anime style: season 1 only when no SxxEyy / NxNN tag is present
		{"[Group] Show - 01 [1080p].mkv", EpisodeInfo{Show: "Show", Season: 1, Episode: 1, LastEpisode: 1}, true},
		{"[Group] Show - 12v2 [1080p].mkv", EpisodeInfo{Show: "Show", Season: 1, Episode: 12, LastEpisode: 12}, true},
		{"Show - 03 - Title.mkv", EpisodeInfo{Show: "Show", Season: 1, Episode: 3, LastEpisode: 3, EpisodeTitle: "Title"}, true},
		{"[Group] Show - S02E03 - 01.mkv", EpisodeInfo{Show: "Show", Season: 2, Episode: 3, LastEpisode: 3, EpisodeTitle: "01"}, true},
		// not episodes
		{"Blade.Runner.2049.2017.2160p.UHD.BluRay.x265-TERMiNAL.mkv", EpisodeInfo{}, false},
		{"Movie Title (2001).mp4", EpisodeInfo{}, false},
		{"Movie - 1080p.mkv", EpisodeInfo{}, false},
		{"Some.Movie.1920x1080.mkv", EpisodeInfo{}, false},
		{"Shows01e02.mkv", EpisodeInfo{}, false},
		{"", EpisodeInfo{}, false},
	}
	for _, tc := range tests {
		got, ok := ParseEpisodeFile(tc.in)
		got.SeasonKnown = false // covered by TestParseEpisodeFileSeasonKnown
		if ok != tc.ok || got != tc.want {
			t.Errorf("ParseEpisodeFile(%q) = %+v, %v; want %+v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestParseSeasonFolder(t *testing.T) {
	tests := []struct {
		in     string
		season int
		ok     bool
	}{
		{"Season 01", 1, true},
		{"Season 2023", 2023, true},
		{"S2024", 2024, true},
		{"Season 1", 1, true},
		{"season 10", 10, true},
		{"SEASON 2", 2, true},
		{"Season.03", 3, true},
		{"Season_04", 4, true},
		{"Season01", 1, true},
		{"S01", 1, true},
		{"s2", 2, true},
		{"S10", 10, true},
		{"Specials", 0, true},
		{"specials", 0, true},
		{"Special", 0, true},
		{"Season 00", 0, true},
		{"Season 0", 0, true},
		{"Season 1 (1080p)", 1, true},
		{" Season 1 ", 1, true},
		{"Season", 0, false},
		{"Seasoning 1", 0, false},
		{"S01E01", 0, false},
		{"Extras", 0, false},
		{"Some Show", 0, false},
		{"Adolescence (2025) [imdb-tt31806037]", 0, false},
		{"", 0, false},
	}
	for _, tc := range tests {
		got, ok := ParseSeasonFolder(tc.in)
		if ok != tc.ok || got != tc.season {
			t.Errorf("ParseSeasonFolder(%q) = %d, %v; want %d, %v", tc.in, got, ok, tc.season, tc.ok)
		}
	}
}

func TestIsVideoFile(t *testing.T) {
	yes := []string{"a.mkv", "a.MKV", "a.mp4", "a.m4v", "a.avi", "a.mov", "a.wmv", "a.webm", "a.ts", "a.m2ts", "a.mpg", "a.mpeg", "a.flv", "Movie (2001).Mp4", "dir/with.dots/file.mkv"}
	no := []string{"a.iso", "a.srt", "a.nfo", "a.jpg", "a.txt", "mkv", "a.mkv.part", "a", "", ".mkv.bak"}
	for _, n := range yes {
		if !IsVideoFile(n) {
			t.Errorf("IsVideoFile(%q) = false, want true", n)
		}
	}
	for _, n := range no {
		if IsVideoFile(n) {
			t.Errorf("IsVideoFile(%q) = true, want false", n)
		}
	}
}

func TestIsIgnoredName(t *testing.T) {
	yes := []string{
		".DS_Store", ".hidden", "._Movie.mkv", "._", "sample", "Sample", "SAMPLES", "sample.mkv", "Samples",
		"Behind The Scenes", "behind the scenes", "Deleted Scenes", "Featurettes", "Interviews", "Scenes", "Shorts",
		"Trailers", "Other", "Extras", "extras",
		"Movie (2001)-trailer.mkv", "Movie-sample.mkv", "Movie-behindthescenes.mp4", "Movie-deleted.mkv",
		"Movie-featurette.mkv", "Movie-interview.mkv", "Movie-scene.mkv", "Movie-short.mkv", "Movie-other.mkv",
		"Some.Dotted.Name-trailer.mkv", "Some.Dotted.Name-trailer",
		"@eaDir", "@EADIR", "#recycle", "lost+found", "$RECYCLE.BIN", "System Volume Information", "",
	}
	no := []string{
		"Movie (2001).mkv", "Adolescence (2025) [imdb-tt31806037]", "Season 01", "Brother.mkv", "Sampler.mkv",
		"Samples of Life (2001).mkv", "The Trailer Park Boys (2001)", "Other People (2016).mkv", "Scenes from a Marriage (2021)",
		"Movie-trailers.mkv", "trailer.mkv", "Extras (2005)",
	}
	for _, n := range yes {
		if !IsIgnoredName(n) {
			t.Errorf("IsIgnoredName(%q) = false, want true", n)
		}
	}
	for _, n := range no {
		if IsIgnoredName(n) {
			t.Errorf("IsIgnoredName(%q) = true, want false", n)
		}
	}
}

func TestCleanTitle(t *testing.T) {
	tests := []struct{ in, want string }{
		{"A.'90s.Christmas", "A '90s Christmas"},
		{"The.'Burbs", "The 'Burbs"},
		{"Movie_Title", "Movie Title"},
		{"  lots   of \t space  ", "lots of space"},
		{"9-1-1 - Lone Star", "9-1-1 - Lone Star"},
		{"9-1-1.-.Lone.Star", "9-1-1 - Lone Star"},
		{"Show   -   Title", "Show - Title"},
		{"-batteries not included", "batteries not included"},
		{"Show - ", "Show"},
		{" - Show - ", "Show"},
		{"#1 Cheerleader Camp", "#1 Cheerleader Camp"},
		{"The (Ex)perience of Love", "The (Ex)perience of Love"},
		{"Spider-Man", "Spider-Man"},
		{"...", ""},
		{"", ""},
		{"-", ""},
	}
	for _, tc := range tests {
		if got := CleanTitle(tc.in); got != tc.want {
			t.Errorf("CleanTitle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTitleKey(t *testing.T) {
	tests := []struct{ in, want string }{
		{"A Discovery of Witches", "adiscoveryofwitches"},
		{"The 'Burbs", "theburbs"},
		{"9-1-1 - Lone Star", "911lonestar"},
		{"Accused (US)", "accusedus"},
		{"#1 Cheerleader Camp", "1cheerleadercamp"},
		{"Blade Runner 2049", "bladerunner2049"},
		{"An Education", "aneducation"},
		{"Amélie", "amélie"},
		{"   ", ""},
		{"", ""},
	}
	for _, tc := range tests {
		if got := TitleKey(tc.in); got != tc.want {
			t.Errorf("TitleKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// Folder and release forms of the same movie must agree.
	a := ParseTitleFolder("A '90s Christmas (2024) [imdb-tt16270280]")
	b := ParseReleaseName("A.'90s.Christmas.2024.PCOK.1080p.WEB-DL.EAC3.5.1.x264-Kitsune.mkv")
	if TitleKey(a.Title) != TitleKey(b.Title) || a.Year != b.Year {
		t.Errorf("folder/release mismatch: %+v vs %+v", a, b)
	}
}

func TestParseEpisodeFileSeasonKnown(t *testing.T) {
	cases := map[string]bool{
		"Show.S01E02.mkv":                     true,
		"Show - 1x02.mkv":                     true,
		"Jeopardy!.2023.06.09.720p.mkv":       true,
		"mork.and.mindy.318.avi":              true,
		"Family.Guy.S08.Ep20.mkv":             true,
		"S.G.N.T.E01.1080p.HDTV.x264-CHI.mkv": false,
		"Show.Name.Episode.7.mkv":             false,
		"[Group] Show - 01 [1080p].mkv":       false,
	}
	for in, want := range cases {
		got, ok := ParseEpisodeFile(in)
		if !ok || got.SeasonKnown != want {
			t.Errorf("ParseEpisodeFile(%q).SeasonKnown = %v (ok=%v); want %v", in, got.SeasonKnown, ok, want)
		}
	}
}
