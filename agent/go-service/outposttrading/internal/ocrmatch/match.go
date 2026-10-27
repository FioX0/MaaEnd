// Package ocrmatch 提供 OutpostTrading 各领域共享的严格规范化 OCR 匹配。
//
// 货品和干员名称使用分层严格匹配，不使用通用编辑距离：
//
//  1. 分隔符归一化（Tier A）：剥除空白、方括号、竖线、连字符、点号、顿号等常见分隔符并统
//     一大小写后要求严格相等。用于 EN 名在 OCR 里多出 `[` `]` `|` 的情况。
//  2. CJK 纯核归一化（Tier B）：在 Tier A 基础上再剔除 ASCII 字母 / 数字（这些是 CJK 名称
//     里的噪声）；候选做相同处理后要求严格相等。用于 "I紫晶质瓶" → "紫晶质瓶"；而「优质柑
//     实罐头」的 CJK 核心与「柑实罐头」不相等，天然不会被误匹配。
//  3. 拉丁名前导噪声归一化（Tier C）：仅当两侧都是拉丁名时生效。OCR 有时把稀有度色条读成
//     前导 `I`（实测 "ILC Wuling Battery" ← "LC Wuling Battery"）；Tier B 会把两侧 ASCII
//     字母全部剥掉而退化为空串，因此需要这一层显式处理。
//
// 三层均为严格相等比较，无相似度阈值。候选 EN 名自带 ASCII 字母时 Tier B 会同时剥掉两侧的
// 字母，对 EN 名退化为 Tier A 的等价形式，不会引入新风险。
package ocrmatch

import (
	"sort"
	"strings"
	"unicode"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"golang.org/x/text/unicode/norm"
)

// Item 表示一项 OCR 文本结果及其画面区域。
type Item struct {
	Text string
	Box  maa.Rect
}

// CollectResults 优先使用 Filtered 结果（OCR expected 过滤后的结果），无结果时读取 All。
// 结果保留相同文本的所有坐标：FindBest 会按 Y/X 排序选最靠上 / 靠左的
// box，去重会丢失同一文本在多个位置的候选 box。
func CollectResults(detail *maa.RecognitionDetail) []Item {
	if detail == nil || detail.Results == nil {
		return nil
	}

	for _, group := range [][]*maa.RecognitionResult{detail.Results.Filtered, detail.Results.All} {
		var items []Item
		for _, r := range group {
			if r == nil {
				continue
			}
			ocr, ok := r.AsOCR()
			if !ok {
				continue
			}
			text := strings.TrimSpace(ocr.Text)
			if text == "" {
				continue
			}
			items = append(items, Item{Text: text, Box: ocr.Box})
		}
		if len(items) > 0 {
			return items
		}
	}
	return nil
}

// Result 表示某一匹配层级选中的 OCR 项与候选项。
type Result struct {
	OCRText   string
	Candidate string
	Tier      string
	Box       maa.Rect
}

// FindBest 按 Tier A → Tier B 的顺序匹配，任一层命中即返回。
// OCR 结果按屏幕顺序排序，优先命中靠上 / 靠左的文本。Tier 划分见 package doc。
func FindBest(ocrItems []Item, candidates []string) *Result {
	tierACandidates := make([]string, len(candidates))
	tierBCandidates := make([]string, len(candidates))
	for i, c := range candidates {
		tierACandidates[i] = StripSeparators(c)
		tierBCandidates[i] = StripASCIIAlnum(tierACandidates[i])
	}

	sortedItems := SortItemsByPosition(ocrItems)

	for _, item := range sortedItems {
		ocrA := StripSeparators(item.Text)
		if ocrA == "" {
			continue
		}
		for i, candA := range tierACandidates {
			if candA != "" && ocrA == candA {
				return &Result{
					OCRText:   item.Text,
					Candidate: candidates[i],
					Tier:      "A",
					Box:       item.Box,
				}
			}
		}
	}

	for _, item := range sortedItems {
		ocrB := StripASCIIAlnum(StripSeparators(item.Text))
		if ocrB == "" {
			continue
		}
		for i, candB := range tierBCandidates {
			if candB == "" {
				continue
			}
			if ocrB == candB {
				return &Result{
					OCRText:   item.Text,
					Candidate: candidates[i],
					Tier:      "B",
					Box:       item.Box,
				}
			}
		}
	}

	// Tier C：仅拉丁名。OCR 把稀有度色条读成前导 I 时（"ILC Wuling Battery"）。
	// 只剥 OCR 侧的前导 i，候选侧保持原样，两侧仍要求严格相等。
	for _, item := range sortedItems {
		ocrC := latinNoiseKey(item.Text, true)
		if ocrC == "" {
			continue
		}
		for i, cand := range candidates {
			if !isLatinName(cand) {
				continue
			}
			if latinNoiseKey(cand, false) == ocrC {
				return &Result{
					OCRText:   item.Text,
					Candidate: candidates[i],
					Tier:      "C",
					Box:       item.Box,
				}
			}
		}
	}

	return nil
}

// foldLatinAccents 把带附加符号的拉丁字母折叠为基本字母（"Jincão" → "Jincao"）。
// OCR 在英文名上偶尔会多出重音；CJK 等非拉丁文字不受影响。
func foldLatinAccents(s string) string {
	decomposed := norm.NFD.String(s)
	var b strings.Builder
	b.Grow(len(decomposed))
	for _, r := range decomposed {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// isLatinName 判断文本是否为纯拉丁名（折叠重音后只含 ASCII 字母、数字与符号）。
// CJK / 西里尔等非 ASCII 字母一律返回 false，避免 Tier C 影响其它语言。
func isLatinName(s string) bool {
	hasLetter := false
	for _, r := range foldLatinAccents(s) {
		if r > unicode.MaxASCII {
			if unicode.IsLetter(r) {
				return false
			}
			continue
		}
		if unicode.IsLetter(r) {
			hasLetter = true
		}
	}
	return hasLetter
}

// latinNoiseKey 生成 Tier C 的比较键：折叠重音 → 做 Tier A 归一化 →（仅 OCR 侧）剥除稀有度
// 色条读出的前导 I。
//
// 色条噪声可能紧跟名称（"ILC Wuling Battery"）也可能被空格分开（"I Xiranite"），因此允许
// 中间有空白；但要求其后紧跟大写字母。真实以 I 开头的单词后接小写（"Iron Part"），不会被
// 误剥。
func latinNoiseKey(s string, dropLeadingGlyph bool) string {
	if !isLatinName(s) {
		return ""
	}
	folded := foldLatinAccents(strings.TrimSpace(s))
	dropGlyph := false
	if dropLeadingGlyph {
		runes := []rune(folded)
		if len(runes) >= 2 && (runes[0] == 'I' || runes[0] == 'i') {
			next := 1
			for next < len(runes) && unicode.IsSpace(runes[next]) {
				next++
			}
			if next < len(runes) && unicode.IsUpper(runes[next]) {
				dropGlyph = true
			}
		}
	}
	key := StripSeparators(folded)
	if key == "" {
		return ""
	}
	if dropGlyph && len(key) > 1 && key[0] == 'i' {
		key = key[1:]
	}
	return key
}

func SortItemsByPosition(items []Item) []Item {
	sortedItems := make([]Item, len(items))
	copy(sortedItems, items)
	sort.SliceStable(sortedItems, func(i, j int) bool {
		if sortedItems[i].Box.Y() != sortedItems[j].Box.Y() {
			return sortedItems[i].Box.Y() < sortedItems[j].Box.Y()
		}
		return sortedItems[i].Box.X() < sortedItems[j].Box.X()
	})
	return sortedItems
}

// StripSeparators 剥除允许差异的分隔字符并统一 ASCII 大小写，保留字母 / 数字 / CJK。
func StripSeparators(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '[', ']', '|', '(', ')', '-', '_', '.', ',', '、', '·', '/', '\\',
			'：', ':', '；', ';':
			continue
		}
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// StripASCIIAlnum 在 StripSeparators 基础上再剥除 ASCII 字母与数字，用于 Tier B。
func StripASCIIAlnum(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x80 {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				continue
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}
