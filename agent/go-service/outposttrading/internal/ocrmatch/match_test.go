package ocrmatch

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestFindBestEnglishLeadingRarityGlyph(t *testing.T) {
	// Observed live on the trading screen: OCR reads "ILC Wuling Battery".
	box := maa.Rect{100, 100, 120, 20}
	items := []Item{{Text: "ILC Wuling Battery", Box: box}}
	res := FindBest(items, []string{"LC Wuling Battery"})
	if res == nil || res.Tier != "C" || res.Box != box {
		t.Fatalf("want Tier C match on %v, got %+v", box, res)
	}
}

func TestFindBestExactStillWinsOverNoisy(t *testing.T) {
	noisy := maa.Rect{10, 10, 120, 20}
	exact := maa.Rect{300, 10, 120, 20}
	items := []Item{
		{Text: "IHeavy Xiranite", Box: noisy},
		{Text: "Heavy Xiranite", Box: exact},
	}
	res := FindBest(items, []string{"Heavy Xiranite"})
	if res == nil || res.Tier != "A" || res.Box != exact {
		t.Fatalf("exact match must win, got %+v", res)
	}
}

func TestFindBestCJKMatchingUnchanged(t *testing.T) {
	box := maa.Rect{50, 50, 90, 20}
	items := []Item{{Text: "I甲货", Box: box}}
	res := FindBest(items, []string{"甲货"})
	if res == nil || res.Tier != "B" || res.Box != box {
		t.Fatalf("JCK core match changed, got %+v", res)
	}
}

func TestFindBestRejectsPartialLatinName(t *testing.T) {
	items := []Item{{Text: "ILC Wuling Battery", Box: maa.Rect{1, 1, 10, 10}}}
	if res := FindBest(items, []string{"Wuling Battery"}); res != nil {
		t.Fatalf("partial name must not match, got %+v", res)
	}
}

func TestFindBestDoesNotStripInsideName(t *testing.T) {
	// A leading "I" is only tolerated as an added glyph; a genuine "I..." name must stay distinct.
	items := []Item{{Text: "Iron Part", Box: maa.Rect{1, 1, 10, 10}}}
	if res := FindBest(items, []string{"Ron Part"}); res != nil {
		t.Fatalf("must not match after stripping a genuine leading letter, got %+v", res)
	}
}

func TestLatinNoiseKeyGuards(t *testing.T) {
	if latinNoiseKey("甲货", true) != "" {
		t.Fatal("CJK text must not produce a latin key")
	}
	if latinNoiseKey("i", true) != "i" {
		t.Fatal("must not strip when nothing remains")
	}
}

func TestFindBestSpaceSeparatedRarityGlyph(t *testing.T) {
	// Observed live: "I Xiranite" and "I Jincao Drink".
	for _, tc := range []struct{ ocr, cand string }{
		{"I Xiranite", "Xiranite"},
		{"I Jincao Drink", "Jincao Drink"},
	} {
		items := []Item{{Text: tc.ocr, Box: maa.Rect{5, 5, 50, 20}}}
		if res := FindBest(items, []string{tc.cand}); res == nil || res.Tier != "C" {
			t.Fatalf("%q should match %q via Tier C, got %+v", tc.ocr, tc.cand, res)
		}
	}
}

func TestFindBestFoldsLatinAccents(t *testing.T) {
	// Observed live: "Jincão Tea" for candidate "Jincao Tea".
	items := []Item{{Text: "Jincão Tea", Box: maa.Rect{7, 7, 60, 20}}}
	if res := FindBest(items, []string{"Jincao Tea"}); res == nil || res.Tier != "C" {
		t.Fatalf("accent noise should fold to a match, got %+v", res)
	}
}

func TestFindBestCJKStillUsesTierBAfterAccentFolding(t *testing.T) {
	// Accent folding must not move CJK matching off Tier B (leading "I" is the CJK path's noise).
	items := []Item{{Text: "I甲货", Box: maa.Rect{9, 9, 40, 20}}}
	if res := FindBest(items, []string{"甲货"}); res == nil || res.Tier != "B" {
		t.Fatalf("CJK path must stay Tier B, got %+v", res)
	}
}
