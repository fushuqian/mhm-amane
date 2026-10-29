// Package number 从文件名/路径或自由文本解析番号、内容类型与文件相位。
//
// 移植自 Amane 的 src/amane/parsing/file_info.py：规则顺序、词表与边界处理保持一致，
// 便于用同一批抓取样本做对拍。类型与相位分离：类型属于番号，相位属于这一份文件。
package number

import (
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/fushuqian/amane-lite/internal/model"
)

var (
	uncensoredPrefixes = []string{
		"BT-", "CT-", "EMP-", "CCDV-", "CWP-", "CWPBD-", "DSAM-", "DRC-", "DRG-",
		"DRGBD-", "GACHI-", "GACHIG-", "HEYDOUGA", "JAV-", "LAF-", "LAFBD-", "HEYZO-",
		"KTG-", "KP-", "KG-", "LLDV-", "MCDV-", "MKD-", "MKBD-", "MMDV-", "NIP-",
		"PB-", "PT-", "QE-", "RED-", "RHJ-", "S2M-", "SKY-", "SKYHD-", "SMD-",
		"SMBD-", "SMDV-", "SSDV-", "SSKP-", "TRG-", "TS-", "XXX-AV-", "YKB-",
		"BIRD", "BOUGA", "H4610-", "C0930-", "H0930-", "S2M", "MCB3D",
	}
	censoredPrefixes = []string{"R18-"}

	// surenLabels 是素人系标签 → 站内番号开头的数字段。
	surenLabels = map[string]string{
		"LUXU-": "259", "ARA-": "261", "MIUM-": "300", "NTK-": "300", "MAAN-": "300",
	}

	westernNames = map[string]string{
		"vixen": "Vixen", "blacked": "Blacked", "tushy": "Tushy", "sexart": "SexArt",
		"x-art": "X-Art", "nubilefilms": "NubileFilms", "babes": "Babes",
		"wgp": "WhenGirlsPlay", "twistys": "Twistys", "deeper": "Deeper",
		"slayed": "Slayed", "tushy raw": "TushyRaw", "blackedraw": "BlackedRaw",
	}

	escapeMarkers = []string{
		"4K", "4KS", "8K", "2160P", "1440P", "1080P", "720P", "480P", "HD", "LR",
		"VR", "DVD", "FULL", "HEVC", "H264", "H265", "X264", "X265", "AAC", "XXX", "PRT",
	}

	mosaicDirTokens = map[string]model.Mosaic{
		"uncensored": model.MosaicUncensored,
		"無碼":         model.MosaicUncensored,
		"无码":         model.MosaicUncensored,
		"cracked":    model.MosaicCracked,
		"破解":         model.MosaicCracked,
		"leaked":     model.MosaicLeaked,
		"流出":         model.MosaicLeaked,
	}
)

var (
	reCatalogNumber   = regexp.MustCompile(`[A-Z]{2,}-\d{2,}[Z]?`)
	reDigitPrefixCat  = regexp.MustCompile(`[A-Z]{1,4}\d{1,4}-\d{2,4}[A-Z]?`)
	reDMMConcat       = regexp.MustCompile(`([A-Z]{2,})00(\d{3})`)
	reTMA             = regexp.MustCompile(`T\d{2}-?\d{3,}`)
	reSixDigitCatalog = regexp.MustCompile(`\d{6}[-_]\d{2,4}`)
	reWesternProbe    = regexp.MustCompile(`([A-Z0-9_]{2,})[-.]2?0?(\d{2}[-.]\d{2}[-.]\d{2})`)
	reWesternParse    = regexp.MustCompile(`([A-Z0-9-]{2,})[-_.]2?0?(\d{2}[-.]\d{2}[-.]\d{2})`)

	reCatalogDate = regexp.MustCompile(`\d{4}[-_.]\d{1,2}[-_.]\d{1,2}`)
	reBracketDate = regexp.MustCompile(`[-\[\]]\d{2}[-_.]\d{2}[-_.]\d{2}\]?`)
	reCDStrip     = regexp.MustCompile(`[-_ .]CD\d{1,2}`)
	reTailStrip   = regexp.MustCompile(`[-_ .][A-Z0-9]\.$`)

	reCD        = regexp.MustCompile(`[-_.]CD(\d{1,2})`)
	rePart      = regexp.MustCompile(`[-_.]PART(\d{1,2})`)
	reAB        = regexp.MustCompile(`-([AB])(?:\.|$)`)
	reTrailing  = regexp.MustCompile(`-([1-9])$`)
	reCDDirName = regexp.MustCompile(`^(?:CD|PART)(\d{1,2})$`)

	definitionMarkers = []struct {
		value   string
		re      *regexp.Regexp
		hdGuard bool
	}{
		{"8K", regexp.MustCompile(`\b8K(?:\d+)?\b`), false},
		{"4K", regexp.MustCompile(`\b4K(?:\d+)?\b`), false},
		{"4K", regexp.MustCompile(`\b2160P(?:\d+)?\b`), false},
		{"1440p", regexp.MustCompile(`\b1440P(?:\d+)?\b`), false},
		{"1080p", regexp.MustCompile(`\b1080P(?:\d+)?\b`), false},
		{"720p", regexp.MustCompile(`\b720P(?:\d+)?\b`), false},
		{"480p", regexp.MustCompile(`\b480P(?:\d+)?\b`), false},
		{"HD", regexp.MustCompile(`\bHD\b`), true},
		{"SD", regexp.MustCompile(`\bSD\b`), true},
	}
)

// DefinitionValues 按优先级排列（2160p 归一为 4K）。
var DefinitionValues = []string{"8K", "4K", "1440p", "1080p", "720p", "480p", "HD", "SD"}

// VR_TAGS 是站点 genre 的惯用写法，判定要求整体相等。
var VRTags = map[string]struct{}{
	"vr": {}, "vr専用": {}, "8kvr": {}, "ハイクオリティvr": {}, "vr専用作品": {}, "vr作品": {},
}

type prepared struct{ escaped, dated, catalog string }

// ParsePath 解析一条文件路径：文件名优先，目录段可补番号，目录关键词可覆盖类型。
func ParsePath(path string, escape []string) model.FileInfo {
	base := filepath.Base(path)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	dirs := dirNames(path)
	number, ctype := identify(stem, dirs, escape, true)
	if override := classifyFromPath(stem, dirs); override != "" {
		ctype = override
	}
	upper := strings.ToUpper(stem)
	cd := detectCD(upper)
	if cd == 0 && len(dirs) > 0 {
		cd = detectCDFromParent(dirs[len(dirs)-1])
	}
	mosaic := detectMosaic(upper)
	if mosaic == "" {
		mosaic = detectMosaicFromDirs(dirs)
	}
	return model.FileInfo{
		Number:            number,
		ContentType:       ctype,
		Prefix:            prefix(number),
		CD:                cd,
		HasSubtitleInName: detectSubtitle(upper),
		HasSubtitle:       detectSubtitle(upper),
		Mosaic:            mosaic,
		Definition:        detectDefinition(upper),
	}
}

// ParseText 解析自由文本（RSS 标题、用户输入），未命中时 Number 为空，不把原文当番号。
func ParseText(text string, escape []string) model.FileInfo {
	number, ctype := identify(text, nil, escape, false)
	stem := strings.TrimSuffix(filepath.Base(text), filepath.Ext(text))
	upper := strings.ToUpper(stem)
	subtitle := detectSubtitle(upper)
	return model.FileInfo{
		Number:            number,
		ContentType:       ctype,
		Prefix:            prefix(number),
		CD:                detectCD(upper),
		HasSubtitleInName: subtitle,
		HasSubtitle:       subtitle,
		Mosaic:            detectMosaic(upper),
		Definition:        detectDefinition(upper),
	}
}

// ExtractNumber 只返回番号；未命中返回空串。
func ExtractNumber(text string) string { return ParseText(text, nil).Number }

// IsVR 只依据标签判定，番号不参与。
func IsVR(tags []string) bool {
	for _, tag := range tags {
		if _, ok := VRTags[strings.ToLower(strings.TrimSpace(tag))]; ok {
			return true
		}
	}
	return false
}

func identify(stem string, dirs []string, escape []string, fallback bool) (string, model.ContentType) {
	if n, t, ok := match(stem, escape, true); ok {
		if n == "" {
			return "", t
		}
		return n, t
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if n, t, ok := match(dirs[i], escape, false); ok && n != "" {
			return n, t
		}
	}
	if !fallback {
		return "", model.Western
	}
	return fallbackName(stem, escape)
}

func match(basename string, escape []string, generic bool) (string, model.ContentType, bool) {
	t := prepare(basename, escape)
	c, escaped, dated := t.catalog, t.escaped, t.dated

	if strings.Contains(c, "MYWIFE") {
		if m := regexp.MustCompile(`NO\.\d*`).FindString(c); m != "" {
			return "Mywife No." + m[2:], model.Censored, true
		}
	}
	if m := regexp.MustCompile(`CW3D2D?BD-\d{2,}`).FindString(c); m != "" {
		return m, typeOfCatalogID(m), true
	}
	if m := regexp.MustCompile(`MMR-?[A-Z]{2,}-?\d+[A-Z]*`).FindString(c); m != "" {
		return strings.Replace(m, "MMR-", "MMR", 1), typeOfCatalogID(m), true
	}
	// 国产 MD 必须用 escaped：catalog 会删除 MD0165-1 的 -1。
	if m := regexp.MustCompile(`([^A-Z]|^)(MD[A-Z-]*\d{4,}(-\d)?)`).FindStringSubmatch(escaped); m != nil && !strings.Contains(escaped, "MDVR") {
		return m[2], model.Chinese, true
	}
	// 欧美日期号用 dated（日期尚未剥除）。
	if reWesternProbe.MatchString(dated) {
		if res := reWesternParse.FindStringSubmatch(dated); res != nil {
			short := strings.Trim(res[1], "-")
			short = strings.ToLower(short)
			full, ok := westernNames[short]
			if !ok {
				full = short
			}
			name := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(full, "-", ""), ".", ""))
			date := strings.ReplaceAll(res[2], "-", ".")
			return capitalize(name + "." + date), model.Western, true
		}
	}
	if m := regexp.MustCompile(`XXX-AV-\d{4,}`).FindString(c); m != "" {
		return m, model.Uncensored, true
	}
	if m := regexp.MustCompile(`MKY-[A-Z]+-\d{3,}`).FindString(c); m != "" {
		return m, model.Chinese, true
	}
	if strings.Contains(c, "FC2") {
		fc2 := strings.NewReplacer("PPV", "", "_", "-", "--", "-").Replace(c)
		if m := regexp.MustCompile(`FC2-\d{5,}`).FindString(fc2); m != "" {
			return m, model.FC2, true
		}
		if m := regexp.MustCompile(`FC2\d{5,}`).FindString(fc2); m != "" {
			return strings.Replace(m, "FC2", "FC2-", 1), model.FC2, true
		}
	}
	if strings.Contains(c, "HEYZO") {
		hz := strings.NewReplacer("_", "-", "--", "-").Replace(c)
		if m := regexp.MustCompile(`HEYZO-\d{3,}`).FindString(hz); m != "" {
			return m, model.Uncensored, true
		}
		if m := regexp.MustCompile(`HEYZO\d{3,}`).FindString(hz); m != "" {
			return strings.Replace(m, "HEYZO", "HEYZO-", 1), model.Uncensored, true
		}
	}
	if strings.Contains(c, "HEYDOUGA") {
		hd := regexp.MustCompile(`[-_. ]+`).ReplaceAllString(c, "-")
		if m := regexp.MustCompile(`HEYDOUGA-?(\d{2,4})-(\d{1,4})`).FindStringSubmatch(hd); m != nil {
			return "HEYDOUGA-" + m[1] + "-" + m[2], model.Uncensored, true
		}
	}
	if m := regexp.MustCompile(`(H4610|C0930|H0930)-[A-Z]+\d{4,}`).FindString(c); m != "" {
		return m, model.Uncensored, true
	}
	if m := regexp.MustCompile(`KIN8(TENGOKU)?-?\d{3,}`).FindString(c); m != "" {
		return strings.NewReplacer("TENGOKU", "-", "--", "-").Replace(m), model.Western, true
	}
	if m := regexp.MustCompile(`S2M[BD]*-\d{3,}`).FindString(c); m != "" {
		return m, model.Uncensored, true
	} else if m := regexp.MustCompile(`MCB3D[BD]*-\d{2,}`).FindString(c); m != "" {
		return m, model.Uncensored, true
	}
	if m := reTMA.FindString(c); m != "" {
		return regexp.MustCompile(`T(\d{2})00`).ReplaceAllString(m, "T$1-"), model.Censored, true
	}
	if m := regexp.MustCompile(`TH101-\d{3,}-\d{5,}`).FindString(c); m != "" {
		return strings.ToLower(m), model.Uncensored, true
	}
	if m := reDMMConcat.FindStringSubmatch(c); m != nil {
		n := m[1] + "-" + m[2]
		return n, typeOfCatalogID(n), true
	}
	if m := regexp.MustCompile(`\d{2,}[A-Z]{2,}-\d{2,}[A-Z]?`).FindString(c); m != "" {
		return m, model.Amateur, true
	}
	if m := reCatalogNumber.FindString(c); m != "" {
		n := m
		for label, digits := range surenLabels {
			if strings.HasPrefix(n, label) {
				n = digits + n
				break
			}
		}
		return n, typeOfCatalogID(n), true
	}
	if n, ok := sixDigitCatalog(c); ok {
		return n, model.Uncensored, true
	}
	if m := reDigitPrefixCat.FindString(c); m != "" {
		return m, typeOfCatalogID(m), true
	}
	if generic {
		if m := regexp.MustCompile(`[A-Z]+-[A-Z]\d+`).FindString(c); m != "" {
			return m, typeOfCatalogID(m), true
		}
		if m := regexp.MustCompile(`\d{2,}[-_]\d{2,}`).FindString(c); m != "" {
			return m, typeOfCatalogID(m), true
		}
		if m := regexp.MustCompile(`\d{3,}-[A-Z]{3,}`).FindString(c); m != "" {
			return m, typeOfCatalogID(m), true
		}
	}
	if m := regexp.MustCompile(`([^A-Z]|^)([NK]\d{4})(\D|$)`).FindStringSubmatch(c); m != nil {
		return strings.ToLower(m[2]), model.Uncensored, true
	}
	if m := regexp.MustCompile(`H_\d{3,}([A-Z]{2,})(\d{2,})`).FindStringSubmatch(c); m != nil {
		n := m[1] + "-" + m[2]
		return n, typeOfCatalogID(n), true
	}
	if generic {
		if m := regexp.MustCompile(`([A-Z]{3,}).*?(\d{2,})`).FindStringSubmatch(c); m != nil {
			n := m[1] + "-" + m[2]
			return n, typeOfCatalogID(n), true
		}
		if m := regexp.MustCompile(`([A-Z]{2,}).*?(\d{3,})`).FindStringSubmatch(c); m != nil {
			n := m[1] + "-" + m[2]
			return n, typeOfCatalogID(n), true
		}
	}
	return "", model.Western, false
}

// sixDigitCatalog 等价于 Python 的 (?<!\d)\d{6}[-_]\d{2,4}(?!\d)：RE2 不支持环视，改为手工校验相邻字符。
func sixDigitCatalog(s string) (string, bool) {
	for _, loc := range reSixDigitCatalog.FindAllStringIndex(s, -1) {
		start, end := loc[0], loc[1]
		if start > 0 && s[start-1] >= '0' && s[start-1] <= '9' {
			continue
		}
		if end < len(s) && s[end] >= '0' && s[end] <= '9' {
			continue
		}
		return s[start:end], true
	}
	return "", false
}

func typeOfCatalogID(number string) model.ContentType {
	if regexp.MustCompile(`^\d{6}[-_]\d{2,4}$`).MatchString(number) {
		return model.Uncensored
	}
	if regexp.MustCompile(`^n\d{4}`).MatchString(number) || regexp.MustCompile(`[^.]+\.\d{2}\.\d{2}\.\d{2}`).MatchString(number) {
		return model.Uncensored
	}
	upper := strings.ToUpper(number)
	for _, p := range uncensoredPrefixes {
		if strings.HasPrefix(upper, strings.ToUpper(p)) {
			return model.Uncensored
		}
	}
	for _, p := range censoredPrefixes {
		if strings.HasPrefix(upper, strings.ToUpper(p)) {
			return model.Censored
		}
	}
	if strings.Contains(upper, "SIRO") || regexp.MustCompile(`\d{3,}[A-Z]+-\d{2}`).MatchString(upper) {
		return model.Amateur
	}
	for label := range surenLabels {
		if strings.HasPrefix(upper, strings.ToUpper(label)) {
			return model.Amateur
		}
	}
	if regexp.MustCompile(`([^A-Z]|^)MD[A-Z-]*\d{4,}`).MatchString(upper) && !strings.Contains(upper, "MDVR") {
		return model.Chinese
	}
	if regexp.MustCompile(`MKY-[A-Z]+-\d{3,}`).MatchString(upper) {
		return model.Chinese
	}
	if reCatalogNumber.MatchString(upper) {
		return model.Censored
	}
	return model.Western
}

func prefix(number string) string {
	upper := strings.ToUpper(number)
	if m := regexp.MustCompile(`([A-Za-z0-9-.]{3,})[-_. ]\d{2}\.\d{2}\.\d{2}`).FindStringSubmatch(number); m != nil {
		return strings.ToUpper(m[1])
	}
	if m := regexp.MustCompile(`^(T\d{2})`).FindStringSubmatch(upper); m != nil {
		return m[1]
	}
	for _, p := range []string{"FC2", "MYWIFE", "KIN8", "S2M", "TH101", "XXX-AV"} {
		if strings.HasPrefix(upper, p) {
			return p
		}
	}
	if m := regexp.MustCompile(`(MKY-[A-Z]+)-\d{3,}`).FindStringSubmatch(upper); m != nil {
		return m[1]
	}
	if m := regexp.MustCompile(`(H4610|C0930|H0930)`).FindStringSubmatch(upper); m != nil {
		return m[1]
	}
	if m := regexp.MustCompile(`^([A-Z]{1,4}\d{1,3})[-_. ]`).FindStringSubmatch(upper); m != nil {
		return m[1]
	}
	if m := regexp.MustCompile(`(\d*[A-Za-z]+)\d*`).FindStringSubmatch(number); m != nil {
		return strings.ToUpper(m[1])
	}
	return upper
}

func prepare(basename string, escape []string) prepared {
	realName := strings.TrimSpace(basename) + "."
	escaped := removeEscapeStrings(realName, escape) + "."
	dated := strings.NewReplacer(
		"-C.", ".",
		".PART", "-CD",
		"-PART", "-CD",
		" EP.", ".EP",
		"-CD-", "",
	).Replace(escaped)
	dated = reCDStrip.ReplaceAllString(dated, "")
	dated = reTailStrip.ReplaceAllString(dated, "")
	dated = strings.ReplaceAll(dated, " ", "-")
	dated = strings.Trim(dated, "-_ .")
	catalog := reCatalogDate.ReplaceAllString(dated, "")
	catalog = reBracketDate.ReplaceAllString(catalog, "")
	catalog = strings.NewReplacer(
		"FC2-PPV", "FC2-",
		"FC2PPV", "FC2-",
		"--", "-",
		"GACHIPPV", "GACHI",
	).Replace(catalog)
	return prepared{escaped: escaped, dated: dated, catalog: catalog}
}

func removeEscapeStrings(filename string, escape []string) string {
	upper := strings.ToUpper(filename)
	for _, s := range escape {
		if s != "" {
			upper = strings.ReplaceAll(upper, strings.ToUpper(s), "")
		}
	}
	for _, marker := range escapeMarkers {
		re := regexp.MustCompile(`[-_ .\[]` + marker + `[-_ .\]]`)
		upper = re.ReplaceAllString(upper, "-")
	}
	upper = strings.ReplaceAll(upper, "--", "-")
	return strings.Trim(upper, "-_ .")
}

func fallbackName(stem string, escape []string) (string, model.ContentType) {
	t := prepare(stem, escape)
	if strings.Contains(t.catalog, "FC2") {
		return strings.NewReplacer("PPV", "", "_", "-", "--", "-").Replace(t.catalog), model.FC2
	}
	if strings.Contains(t.catalog, "HEYZO") {
		return strings.NewReplacer("_", "-", "--", "-").Replace(t.catalog), model.Uncensored
	}
	name := regexp.MustCompile(`[【(（\[].+?[]）)】]`).ReplaceAllString(t.escaped, "")
	name = strings.Trim(name, "@. ")
	name = norm.NFC.String(name)
	// Amane 另有一处 cp932→shift_jis 的乱码修补；Go 标准库无等价编码器，暂略。
	name = strings.Trim(name, "-_ .")
	if strings.HasPrefix(name, "FC-") {
		return strings.Replace(name, "FC-", "FC2-", 1), model.FC2
	}
	return name, model.Western
}

func classifyFromPath(stem string, dirs []string) model.ContentType {
	names := append([]string{stem}, dirs...)
	for _, name := range names {
		lower := strings.ToLower(name)
		if lower == "getchu" {
			return model.Hentai
		}
		if strings.HasPrefix(name, "里番") || strings.HasPrefix(name, "裏番") {
			return model.Hentai
		}
		if strings.HasPrefix(name, "欧美") {
			return model.Western
		}
	}
	return ""
}

func detectCD(basename string) int {
	if m := reCD.FindStringSubmatch(basename); m != nil {
		return atoi(m[1])
	}
	if m := rePart.FindStringSubmatch(basename); m != nil {
		return atoi(m[1])
	}
	if m := reAB.FindStringSubmatch(basename); m != nil {
		return int(m[1][0]-'A') + 1
	}
	if m := reTrailing.FindStringSubmatch(basename); m != nil {
		return atoi(m[1])
	}
	return 0
}

func detectCDFromParent(parent string) int {
	if m := reCDDirName.FindStringSubmatch(strings.TrimSpace(parent)); m != nil {
		if n := atoi(m[1]); n >= 1 {
			return n
		}
	}
	return 0
}

func detectSubtitle(basename string) bool {
	re := regexp.MustCompile(`-U?C`)
	for _, loc := range re.FindAllStringIndex(basename, -1) {
		end := loc[1]
		if end < len(basename) && isAlnum(basename[end]) {
			continue
		}
		return true
	}
	return strings.ContainsAny(basename, "字幕中文")
}

func detectMosaic(basename string) model.Mosaic {
	if strings.Contains(basename, "破解") || strings.Contains(basename, "克破") || strings.Contains(basename, "CRACKED") {
		return model.MosaicCracked
	}
	re := regexp.MustCompile(`-U(C)?`)
	for _, loc := range re.FindAllStringIndex(basename, -1) {
		end := loc[1]
		if end < len(basename) && isAlnum(basename[end]) {
			continue
		}
		return model.MosaicCracked
	}
	if strings.Contains(basename, "流出") || strings.Contains(basename, "LEAKED") {
		return model.MosaicLeaked
	}
	if strings.Contains(basename, "無碼") || strings.Contains(basename, "无码") || strings.Contains(basename, "UNCENSORED") {
		return model.MosaicUncensored
	}
	return ""
}

func detectMosaicFromDirs(dirs []string) model.Mosaic {
	for i := len(dirs) - 1; i >= 0; i-- {
		token := strings.Trim(strings.TrimSpace(dirs[i]), "[]【】()（）")
		if v, ok := mosaicDirTokens[strings.ToLower(token)]; ok {
			return v
		}
	}
	return ""
}

func detectDefinition(basename string) string {
	normalized := normalizeMarkers(basename)
	for _, marker := range definitionMarkers {
		loc := marker.re.FindStringIndex(normalized)
		if loc == nil {
			continue
		}
		if marker.hdGuard && loc[1] < len(normalized) {
			// HD/SD 后接 . 或 - 再接数字时是番号前缀（HD-123），不当作分辨率。
			if normalized[loc[1]] == '.' || normalized[loc[1]] == '-' {
				if loc[1]+1 < len(normalized) && isDigit(normalized[loc[1]+1]) {
					continue
				}
			}
		}
		return marker.value
	}
	return ""
}

func normalizeMarkers(text string) string {
	var b strings.Builder
	for _, r := range text {
		if r == '_' || r > 0x7f {
			b.WriteByte('.')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func dirNames(path string) []string {
	dir := filepath.Dir(path)
	volume := filepath.VolumeName(dir)
	dir = strings.TrimPrefix(dir, volume)
	parts := strings.FieldsFunc(dir, func(r rune) bool { return r == '/' || r == '\\' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" || p == "." || p == "/" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func capitalize(s string) string {
	lower := strings.ToLower(s)
	if lower == "" {
		return ""
	}
	return strings.ToUpper(lower[:1]) + lower[1:]
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func isAlnum(b byte) bool {
	return isDigit(b) || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}
