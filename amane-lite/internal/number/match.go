// 番号比对与检索词变体，移植自 Amane 的 crawlers/parsing.py。
package number

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// FoldNumber 折叠大小写、短横线与空格；下划线不折叠（010115_001 与 010115-001 是两部）。
func FoldNumber(number string) string {
	s := strings.ToLower(number)
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, " ", "")
	return s
}

var reFC2Number = regexp.MustCompile(`^fc2(?:_?ppv)?_?(\d+)$`)
var reLeadingDigits = regexp.MustCompile(`^\d+`)

// FC2ArticleID 返回 FC2 商品 ID；不是 FC2 形态时为空。
func FC2ArticleID(number string) string {
	m := reFC2Number.FindStringSubmatch(FoldNumber(number))
	if m == nil {
		return ""
	}
	return m[1]
}

// FC2Number 按官方市场（ppv）或索引站写法生成番号。
func FC2Number(articleID string, ppv bool) string {
	if ppv {
		return "FC2-PPV-" + articleID
	}
	return "FC2-" + articleID
}

// NumberVariants 返回站点上可能的检索词，原样写法在前，去重保序。
func NumberVariants(number string) []string {
	variants := []string{number}
	if id := FC2ArticleID(number); id != "" {
		variants = append(variants, FC2Number(id, true), FC2Number(id, false))
	} else if loc := reLeadingDigits.FindStringIndex(number); loc != nil && loc[1] < len(number) && isLetter(number[loc[1]]) {
		variants = append(variants, number[loc[1]:])
	} else {
		upper := strings.ToUpper(number)
		for label, prefix := range surenLabels {
			if strings.HasPrefix(upper, label) {
				variants = append(variants, prefix+number)
				break
			}
		}
	}
	return dedupe(variants)
}

func splitLeadingDigits(number string) (string, string, bool) {
	folded := FoldNumber(number)
	i := 0
	for i < len(folded) && folded[i] >= '0' && folded[i] <= '9' {
		i++
	}
	if i == 0 || i >= len(folded) {
		return "", folded, false
	}
	if folded[i] < 'a' || folded[i] > 'z' {
		return "", folded, false
	}
	return folded[:i], folded[i:], true
}

// MatchQuality 是站内番号与检索番号的同一性，声明顺序即精确度。
type MatchQuality int

const (
	MatchNone MatchQuality = iota
	MatchWithoutLeadingDigits
	MatchExact
)

var reWesternNumber = regexp.MustCompile(
	`^(.+?)([._-])(\d{4}|\d{2})([._-])(\d{2})([._-])(\d{2})$`,
)

type westernNumber struct {
	studio string
	date   string
}

func parseWesternNumber(number string) (westernNumber, bool) {
	m := reWesternNumber.FindStringSubmatch(strings.TrimSpace(number))
	if m == nil {
		return westernNumber{}, false
	}
	year := m[3]
	if len(year) == 2 {
		year = "20" + year
	}
	date := year + "-" + m[5] + "-" + m[7]
	if !validDate(year, m[5], m[7]) {
		return westernNumber{}, false
	}
	return westernNumber{studio: foldStudio(m[1]), date: date}, true
}

func foldStudio(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(norm.NFKC.String(name)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// NumberMatch 判定站内番号与检索番号是否同一部。
func NumberMatch(found, query string) MatchQuality {
	if found == "" || query == "" {
		return MatchNone
	}
	if FoldNumber(found) == FoldNumber(query) {
		return MatchExact
	}
	if id := FC2ArticleID(found); id != "" && id == FC2ArticleID(query) {
		return MatchExact
	}
	if a, ok := parseWesternNumber(found); ok {
		if b, ok2 := parseWesternNumber(query); ok2 && a == b {
			return MatchExact
		}
	}
	foundDigits, foundRest, _ := splitLeadingDigits(found)
	queryDigits, queryRest, _ := splitLeadingDigits(query)
	if foundRest == queryRest && !(foundDigits != "" && queryDigits != "") {
		return MatchWithoutLeadingDigits
	}
	return MatchNone
}

// SelectNumberIndex 在候选中找同一部：精确优先，退而只在唯一「仅差数字段」时取它。
func SelectNumberIndex(found []string, query string) int {
	exact := -1
	loose := []int{}
	for i, item := range found {
		switch NumberMatch(item, query) {
		case MatchExact:
			if exact < 0 {
				exact = i
			}
		case MatchWithoutLeadingDigits:
			loose = append(loose, i)
		}
	}
	if exact >= 0 {
		return exact
	}
	if len(loose) == 1 {
		return loose[0]
	}
	return -1
}

func isLetter(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }

func validDate(year, month, day string) bool {
	y := atoi(year)
	mo := atoi(month)
	d := atoi(day)
	if y < 1900 || mo < 1 || mo > 12 || d < 1 || d > 31 {
		return false
	}
	days := []int{31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}[mo-1]
	if mo == 2 && !leap(y) {
		days = 28
	}
	return d <= days
}

func leap(y int) bool { return y%4 == 0 && (y%100 != 0 || y%400 == 0) }

func dedupe(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
