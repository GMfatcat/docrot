package fuzzy

import (
	"reflect"
	"testing"
)

func TestDistance(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want int
	}{
		{"equal", "abc", "abc", 0},
		{"empty both", "", "", 0},
		{"empty a", "", "abc", 3},
		{"empty b", "abc", "", 3},
		{"substitution", "kitten", "sitten", 1},
		{"levenshtein classic", "kitten", "sitting", 3},
		{"insertion", "flaw", "lawn", 2},
		{"transposition", "ca", "ac", 1},
		{"transposition inside", "WriteJSON", "WriteJOSN", 1},
		{"osa not unrestricted", "ca", "abc", 3},
		{"case matters", "abc", "ABC", 3},
		{"runes not bytes", "文件", "文檔", 1},
		{"cjk vs ascii", "說明", "ab", 2},
		{"real symbols", "httpx.WriteJSON", "httpx.WriteData", 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Distance(tt.a, tt.b); got != tt.want {
				t.Errorf("Distance(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
			if got := Distance(tt.b, tt.a); got != tt.want {
				t.Errorf("Distance(%q, %q) = %d, want %d (not symmetric)", tt.b, tt.a, got, tt.want)
			}
		})
	}
}

func TestDefaultMaxDist(t *testing.T) {
	tests := []struct {
		q    string
		want int
	}{
		{"", 2},
		{"abc", 2},
		{"abcdefgh", 2},
		{"abcdefghijkl", 3},
		{"httpx.WriteJSONResponse", 5},
	}
	for _, tt := range tests {
		if got := DefaultMaxDist(tt.q); got != tt.want {
			t.Errorf("DefaultMaxDist(%q) = %d, want %d", tt.q, got, tt.want)
		}
	}
}

func TestRank(t *testing.T) {
	pool := []string{"WriteData", "WriteJSON", "WriteText", "ReadJSON", "Close"}
	tests := []struct {
		name    string
		q       string
		pool    []string
		n       int
		maxDist int
		want    []Candidate
	}{
		{
			name: "empty pool",
			q:    "x",
			pool: nil,
			n:    3,
			want: nil,
		},
		{
			name: "exact match scores zero",
			q:    "WriteJSON",
			pool: pool,
			n:    1,
			want: []Candidate{{Text: "WriteJSON", Score: 0}},
		},
		{
			name: "case insensitive",
			q:    "writejson",
			pool: pool,
			n:    1,
			want: []Candidate{{Text: "WriteJSON", Score: 0}},
		},
		{
			name: "default maxDist drops far candidates",
			q:    "WriteJSOM",
			pool: pool,
			n:    5,
			want: []Candidate{{Text: "WriteJSON", Score: 1}},
		},
		{
			name:    "maxDist filters",
			q:       "WriteJSOM",
			pool:    pool,
			n:       5,
			maxDist: 1,
			want:    []Candidate{{Text: "WriteJSON", Score: 1}},
		},
		{
			name: "nothing close enough",
			q:    "zzzzzzzz",
			pool: pool,
			n:    3,
			want: nil,
		},
		{
			name: "n limits results",
			q:    "WriteXata",
			pool: pool,
			n:    1,
			want: []Candidate{{Text: "WriteData", Score: 1}},
		},
		{
			name: "n<=0 means all",
			q:    "Clse",
			pool: pool,
			n:    0,
			want: []Candidate{{Text: "Close", Score: 1}},
		},
		{
			name: "duplicates collapse",
			q:    "abc",
			pool: []string{"abd", "abd", "abd"},
			n:    5,
			want: []Candidate{{Text: "abd", Score: 1}},
		},
		{
			name: "alphabetical after prefix tie",
			q:    "ab",
			pool: []string{"zb", "xb", "yb"},
			n:    3,
			want: []Candidate{{Text: "xb", Score: 1}, {Text: "yb", Score: 1}, {Text: "zb", Score: 1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Rank(tt.q, tt.pool, tt.n, tt.maxDist)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Rank(%q, %v, %d, %d) = %v, want %v", tt.q, tt.pool, tt.n, tt.maxDist, got, tt.want)
			}
		})
	}
}

func TestRankPrefersLongerSharedPrefix(t *testing.T) {
	// "docs/contract.md" is one deletion away from both candidates, so the
	// shared prefix must decide.
	got := Rank("docs/contract.md", []string{"docs/contracts.md", "dots/contract.md"}, 1, 0)
	if len(got) != 1 || got[0].Text != "docs/contracts.md" {
		t.Fatalf("Rank = %v, want docs/contracts.md first", got)
	}
}

func TestBest(t *testing.T) {
	tests := []struct {
		name   string
		q      string
		pool   []string
		want   string
		wantOK bool
	}{
		{"hit", "WriteJSON", []string{"WriteData", "WriteJSONx"}, "WriteJSONx", true},
		{"miss", "WriteJSON", []string{"Close"}, "", false},
		{"empty pool", "x", nil, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Best(tt.q, tt.pool)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("Best(%q, %v) = (%q, %v), want (%q, %v)", tt.q, tt.pool, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
