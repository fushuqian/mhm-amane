// 视频路径模板渲染：沿用 Amane 原版语法（占位符、可选组、双选组、值映射）。
//
// 模板只写相对片库根的部分，片库根由调用方拼接；渲染结果逐段清理后作为相对路径。
package organize

import (
	"errors"
	"fmt"
	"strings"
)

// knownPlaceholders 是支持的原版占位符（不含花括号）。
var knownPlaceholders = map[string]struct{}{
	"number": {}, "prefix": {}, "suffix": {}, "title": {},
	"actor": {}, "actors": {}, "actress": {}, "actresses": {},
	"studio": {}, "publisher": {}, "series": {}, "year": {}, "release": {},
	"content_type": {}, "ext": {},
	"cd?": {}, "sub?": {}, "mosaic?": {}, "def?": {},
	"raw_name": {}, "raw_dir": {},
	"video_dir": {}, "video_name": {}, "video_relpath": {},
}

// ValidateTemplate 在保存设置前校验语法与占位符。
func ValidateTemplate(template string) error {
	if strings.TrimSpace(template) == "" {
		return errors.New("视频路径模板不能为空")
	}
	if strings.HasPrefix(strings.TrimSpace(template), "/") {
		return errors.New("模板不要以 / 开头：片库路径由程序自动拼接")
	}
	_, _, _, err := renderText(template, nil, true)
	return err
}

// Render 渲染模板；未知占位符输出 Unknown（不报错）。
func Render(template string, vals map[string]string) (string, error) {
	out, _, _, err := renderText(template, vals, false)
	return out, err
}

// renderText 逐个字符扫描：占位符、单层可选组 [..]、双层可选组 [[..]]。
// 返回：渲染结果、本层是否有非空占位符、本层是否出现占位符（用于可选组省略规则）。
func renderText(s string, vals map[string]string, strict bool) (string, bool, bool, error) {
	var b strings.Builder
	nonEmpty, hasAny := false, false
	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], "[["):
			end, err := groupEnd(s, i+2)
			if err != nil {
				return "", false, false, err
			}
			inner := s[i+2 : end]
			out, _, _, err := renderText(inner, vals, strict)
			if err != nil {
				return "", false, false, err
			}
			if out != "" {
				b.WriteString(out)
			}
			i = end + 2
		case s[i] == '[':
			end, err := groupEnd(s, i+1)
			if err != nil {
				return "", false, false, err
			}
			inner := s[i+1 : end]
			out, innerNonEmpty, innerHasAny, err := renderText(inner, vals, strict)
			if err != nil {
				return "", false, false, err
			}
			// 组内直接占位符全空则整组省略（连同样板里的分隔符一起去掉）。
			if innerNonEmpty || !innerHasAny {
				b.WriteString(out)
			}
			i = end + 1
		case s[i] == '{':
			end := strings.IndexByte(s[i:], '}')
			if end < 0 {
				return "", false, false, errors.New("占位符缺少右花括号")
			}
			name := s[i+1 : i+end]
			value, err := evalPlaceholder(name, vals, strict)
			if err != nil {
				return "", false, false, err
			}
			hasAny = true
			if value != "" {
				nonEmpty = true
			}
			b.WriteString(value)
			i += end + 1
		default:
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String(), nonEmpty, hasAny, nil
}

// groupEnd 从 start 开始找配对的右括号，返回右括号起始下标。
func groupEnd(s string, start int) (int, error) {
	depth := 1
	for i := start; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], "[["):
			depth++
			i += 2
		case strings.HasPrefix(s[i:], "]]"):
			depth--
			if depth == 0 {
				return i, nil
			}
			i += 2
		case s[i] == '[':
			depth++
			i++
		case s[i] == ']':
			depth--
			if depth == 0 {
				return i, nil
			}
			i++
		default:
			i++
		}
	}
	return 0, errors.New("可选组缺少右方括号")
}

func evalPlaceholder(expr string, vals map[string]string, strict bool) (string, error) {
	name := strings.TrimSpace(expr)
	mapping := ""
	if idx := strings.IndexByte(expr, '|'); idx >= 0 {
		name = strings.TrimSpace(expr[:idx])
		mapping = expr[idx+1:]
	}
	if _, ok := knownPlaceholders[name]; !ok {
		if strict {
			return "", fmt.Errorf("未知占位符 {%s}", name)
		}
		return "Unknown", nil
	}
	raw := ""
	if vals != nil {
		raw = vals[name]
	}
	if strings.TrimSpace(mapping) == "" {
		return raw, nil
	}
	pairs, err := parseMapping(mapping)
	if err != nil {
		return "", err
	}
	if out, ok := pairs[raw]; ok {
		return out, nil
	}
	if out, ok := pairs[""]; ok && raw == "" {
		return out, nil
	}
	return raw, nil
}

// parseMapping 解析 {name|原值=输出,原值=输出}；空键表示缺省值。
func parseMapping(mapping string) (map[string]string, error) {
	out := map[string]string{}
	for _, item := range strings.Split(mapping, ",") {
		if strings.TrimSpace(item) == "" {
			continue
		}
		idx := strings.IndexByte(item, '=')
		if idx < 0 {
			return nil, fmt.Errorf("占位符映射缺少等号：%s", strings.TrimSpace(item))
		}
		out[strings.TrimSpace(item[:idx])] = strings.TrimSpace(item[idx+1:])
	}
	return out, nil
}
