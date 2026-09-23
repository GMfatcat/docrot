package pairs

import (
	"reflect"
	"testing"
)

func TestStripLangSegments(t *testing.T) {
	got := stripLangSegments([]string{
		"https://fastapi.tiangolo.com/ja/tutorial/",
		"https://fastapi.tiangolo.com/tutorial/",
		"https://fastapi.tiangolo.com/zh",
		"https://fastapi.tiangolo.com/pt-br/async/#in-a-hurry",
		"https://example.com/en-US/docs",
		"docs/guide.md",
	})
	want := []string{
		"https://fastapi.tiangolo.com/tutorial",
		"https://fastapi.tiangolo.com",
		"https://fastapi.tiangolo.com/async/#in-a-hurry",
		"https://example.com/docs",
		"docs/guide.md",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}
