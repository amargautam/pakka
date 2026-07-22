package commitgate

import "testing"

func TestClassifyMarker(t *testing.T) {
	const now = int64(1_000_000)
	const maxAge = int64(300)
	const hashA = "aaaa"
	const hashB = "bbbb"

	freshJSON := `{"ts":999900,"diffSHA256":"aaaa","verdict":"passed"}`     // age 100s
	staleJSON := `{"ts":999000,"diffSHA256":"aaaa","verdict":"passed"}`     // age 1000s
	mismatchJSON := `{"ts":999900,"diffSHA256":"cccc","verdict":"passed"}`  // fresh, wrong hash
	notPassedJSON := `{"ts":999900,"diffSHA256":"aaaa","verdict":"failed"}` // fresh, not passed
	noHashJSON := `{"ts":999900,"verdict":"passed"}`                        // missing diffSHA256

	cases := []struct {
		name    string
		content string
		curHash string
		want    MarkerClass
	}{
		{"fresh matching JSON passes", freshJSON, hashA, MarkerPass},
		{"fresh mismatch JSON", mismatchJSON, hashA, MarkerMismatch},
		{"fresh JSON but diff now differs", freshJSON, hashB, MarkerMismatch},
		{"stale JSON", staleJSON, hashA, MarkerStale},
		{"not-passed verdict ignored", notPassedJSON, hashA, MarkerNone},
		{"missing diffSHA256 ignored", noHashJSON, hashA, MarkerNone},
		{"legacy bare epoch", "1780439832", hashA, MarkerLegacy},
		{"legacy bare epoch with whitespace", "  1780439832\n", hashA, MarkerLegacy},
		{"legacy rfc3339", "2026-07-22T10:00:00Z", hashA, MarkerLegacy},
		{"empty", "", hashA, MarkerNone},
		{"whitespace only", "   \n", hashA, MarkerNone},
		{"garbage", "not-a-marker", hashA, MarkerNone},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ClassifyMarker(c.content, c.curHash, now, maxAge)
			if got != c.want {
				t.Fatalf("ClassifyMarker(%q, %q) = %d; want %d", c.content, c.curHash, got, c.want)
			}
		})
	}
}
