package main

import (
	"regexp"
	"strconv"
	"strings"
)

const maxRegexDomainExpansion = 200

var (
	exactDomainRegex       = regexp.MustCompile(`(?i)^\^([a-z0-9-]+(?:\\\.[a-z0-9-]+)+)\$$`)
	suffixDomainRegex      = regexp.MustCompile(`(?i)^\(\^\|\\\.\)([a-z0-9-]+(?:\\\.[a-z0-9-]+)+)\$$`)
	repeatedSubdomainRegex = regexp.MustCompile(`(?i)^\^\(\.\+\\\.\)\*([a-z0-9-]+(?:\\\.[a-z0-9-]+)+)\$$`)
	advancedRegexTokens    = regexp.MustCompile(`\(\?<?[=!]|\\[1-9]`)

	repeatedStarsRegex = regexp.MustCompile(`\*{2,}`)
	repeatedDotsRegex  = regexp.MustCompile(`\.{2,}`)
	leadingMarksRegex  = regexp.MustCompile(`^[*.]+`)
	trailingDotsRegex  = regexp.MustCompile(`^\.+|\.+$`)
)

type regexDomainRuleKind int

const (
	exactRegexDomainRule regexDomainRuleKind = iota
	suffixRegexDomainRule
)

func surgeRuleForRegex(pattern string) string {
	rules := surgeRulesForRegex(pattern)
	if len(rules) == 0 {
		return ""
	}
	return rules[0]
}

func surgeRulesForRegex(pattern string) []string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil
	}

	if match := exactDomainRegex.FindStringSubmatch(pattern); match != nil {
		return []string{"DOMAIN," + unescapeRegexDomain(match[1])}
	}
	if match := suffixDomainRegex.FindStringSubmatch(pattern); match != nil {
		return []string{"DOMAIN-SUFFIX," + unescapeRegexDomain(match[1])}
	}
	if match := repeatedSubdomainRegex.FindStringSubmatch(pattern); match != nil {
		return []string{"DOMAIN-SUFFIX," + unescapeRegexDomain(match[1])}
	}

	if advancedRegexTokens.MatchString(pattern) {
		return []string{"URL-REGEX," + pattern}
	}

	if rules := expandRegexToDomainRules(pattern, maxRegexDomainExpansion); len(rules) > 0 {
		return rules
	}

	if wildcard := wildcardFromRegex(pattern); wildcard != "" {
		return []string{"DOMAIN-WILDCARD," + wildcard}
	}
	return []string{"URL-REGEX," + pattern}
}

func expandRegexToDomainRules(pattern string, max int) []string {
	kind, body, ok := regexDomainExpansionBody(pattern)
	if !ok {
		return nil
	}

	values, ok := expandRegexDomainBody(body, max)
	if !ok || len(values) == 0 {
		return nil
	}

	prefix := "DOMAIN,"
	if kind == suffixRegexDomainRule {
		prefix = "DOMAIN-SUFFIX,"
	}

	seen := make(map[string]bool)
	rules := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(value)
		if !isValidExpandedDomain(value) {
			return nil
		}
		rule := prefix + value
		if seen[rule] {
			continue
		}
		seen[rule] = true
		rules = append(rules, rule)
	}
	return rules
}

func regexDomainExpansionBody(pattern string) (regexDomainRuleKind, string, bool) {
	if !strings.HasSuffix(pattern, "$") {
		return 0, "", false
	}
	pattern = strings.TrimSuffix(pattern, "$")
	if strings.HasPrefix(pattern, `(^|\.)`) {
		return suffixRegexDomainRule, pattern[len(`(^|\.)`):], true
	}
	if strings.HasPrefix(pattern, "^") {
		return exactRegexDomainRule, pattern[1:], true
	}
	return 0, "", false
}

func expandRegexDomainBody(body string, max int) ([]string, bool) {
	expander := regexDomainExpander{input: body, max: max}
	values, index, ok := expander.parseAlternation(0, 0)
	if !ok || index != len(body) {
		return nil, false
	}
	return values, true
}

type regexDomainExpander struct {
	input string
	max   int
}

func (e regexDomainExpander) parseAlternation(index int, stop byte) ([]string, int, bool) {
	var values []string
	for {
		part, next, ok := e.parseSequence(index, stop)
		if !ok {
			return nil, 0, false
		}
		var appendOK bool
		values, appendOK = appendLimitedExpansions(values, part, e.max)
		if !appendOK {
			return nil, 0, false
		}
		index = next
		if index >= len(e.input) || e.input[index] != '|' {
			break
		}
		index++
	}

	if stop != 0 {
		if index >= len(e.input) || e.input[index] != stop {
			return nil, 0, false
		}
		index++
	}
	return values, index, true
}

func (e regexDomainExpander) parseSequence(index int, stop byte) ([]string, int, bool) {
	values := []string{""}
	for index < len(e.input) {
		char := e.input[index]
		if char == '|' || (stop != 0 && char == stop) {
			break
		}

		atom, next, ok := e.parseAtom(index)
		if !ok {
			return nil, 0, false
		}
		atom, next, ok = e.applyQuantifier(atom, next)
		if !ok {
			return nil, 0, false
		}

		values, ok = combineExpansions(values, atom, e.max)
		if !ok {
			return nil, 0, false
		}
		index = next
	}
	return values, index, true
}

func (e regexDomainExpander) parseAtom(index int) ([]string, int, bool) {
	if index >= len(e.input) {
		return nil, 0, false
	}

	char := e.input[index]
	switch {
	case char == '\\':
		return e.parseEscapedAtom(index)
	case char == '[':
		return e.parseCharClass(index)
	case char == '(':
		return e.parseAlternation(index+1, ')')
	case isDomainRegexChar(char):
		return []string{strings.ToLower(string(char))}, index + 1, true
	default:
		return nil, 0, false
	}
}

func (e regexDomainExpander) parseEscapedAtom(index int) ([]string, int, bool) {
	nextIndex := index + 1
	if nextIndex >= len(e.input) {
		return nil, 0, false
	}
	next := e.input[nextIndex]
	switch {
	case next == '.' || next == '-':
		return []string{string(next)}, nextIndex + 1, true
	case next == 'd':
		return byteRangeExpansions('0', '9'), nextIndex + 1, true
	default:
		return nil, 0, false
	}
}

func (e regexDomainExpander) parseCharClass(index int) ([]string, int, bool) {
	close := findRegexCharClassEnd(e.input, index+1)
	if close == -1 {
		return nil, 0, false
	}

	body := e.input[index+1 : close]
	if body == "" || strings.HasPrefix(body, "^") {
		return nil, 0, false
	}

	var values []string
	for pos := 0; pos < len(body); pos++ {
		char := body[pos]
		if char == '\\' {
			if pos+1 >= len(body) {
				return nil, 0, false
			}
			next := body[pos+1]
			if next == 'd' {
				var ok bool
				values, ok = appendLimitedExpansions(values, byteRangeExpansions('0', '9'), e.max)
				if !ok {
					return nil, 0, false
				}
				pos++
				continue
			}
			if next != '-' {
				return nil, 0, false
			}
			char = next
			pos++
		}

		if pos+2 < len(body) && body[pos+1] == '-' {
			end := body[pos+2]
			rangeValues, ok := charRangeExpansions(char, end)
			if !ok {
				return nil, 0, false
			}
			values, ok = appendLimitedExpansions(values, rangeValues, e.max)
			if !ok {
				return nil, 0, false
			}
			pos += 2
			continue
		}

		if !isDomainRegexChar(char) {
			return nil, 0, false
		}
		var ok bool
		values, ok = appendUniqueExpansion(values, strings.ToLower(string(char)), e.max)
		if !ok {
			return nil, 0, false
		}
	}
	return values, close + 1, true
}

func (e regexDomainExpander) applyQuantifier(values []string, index int) ([]string, int, bool) {
	if index >= len(e.input) {
		return values, index, true
	}

	switch e.input[index] {
	case '?':
		out := []string{""}
		var ok bool
		out, ok = appendLimitedExpansions(out, values, e.max)
		return out, index + 1, ok
	case '+', '*':
		return nil, 0, false
	case '{':
		close := strings.IndexByte(e.input[index+1:], '}')
		if close == -1 {
			return nil, 0, false
		}
		close += index + 1
		min, maxRepeat, ok := parseRegexRepeatBounds(e.input[index+1 : close])
		if !ok {
			return nil, 0, false
		}
		repeated, ok := repeatExpansions(values, min, maxRepeat, e.max)
		return repeated, close + 1, ok
	default:
		return values, index, true
	}
}

func parseRegexRepeatBounds(value string) (int, int, bool) {
	if value == "" {
		return 0, 0, false
	}
	if !strings.Contains(value, ",") {
		count, err := strconv.Atoi(value)
		if err != nil || count < 0 {
			return 0, 0, false
		}
		return count, count, true
	}

	parts := strings.SplitN(value, ",", 2)
	if parts[0] == "" || parts[1] == "" {
		return 0, 0, false
	}
	min, minErr := strconv.Atoi(parts[0])
	max, maxErr := strconv.Atoi(parts[1])
	if minErr != nil || maxErr != nil || min < 0 || max < min {
		return 0, 0, false
	}
	return min, max, true
}

func repeatExpansions(values []string, min, max, limit int) ([]string, bool) {
	var out []string
	for count := min; count <= max; count++ {
		current := []string{""}
		var ok bool
		for repeat := 0; repeat < count; repeat++ {
			current, ok = combineExpansions(current, values, limit)
			if !ok {
				return nil, false
			}
		}
		out, ok = appendLimitedExpansions(out, current, limit)
		if !ok {
			return nil, false
		}
	}
	return out, true
}

func combineExpansions(prefixes, suffixes []string, limit int) ([]string, bool) {
	if len(prefixes) == 0 || len(suffixes) == 0 {
		return nil, false
	}

	out := make([]string, 0, len(prefixes)*len(suffixes))
	for _, prefix := range prefixes {
		for _, suffix := range suffixes {
			var ok bool
			out, ok = appendUniqueExpansion(out, prefix+suffix, limit)
			if !ok {
				return nil, false
			}
		}
	}
	return out, true
}

func appendLimitedExpansions(dst, values []string, limit int) ([]string, bool) {
	for _, value := range values {
		var ok bool
		dst, ok = appendUniqueExpansion(dst, value, limit)
		if !ok {
			return nil, false
		}
	}
	return dst, true
}

func appendUniqueExpansion(values []string, value string, limit int) ([]string, bool) {
	for _, existing := range values {
		if existing == value {
			return values, true
		}
	}
	if len(values) >= limit {
		return nil, false
	}
	return append(values, value), true
}

func charRangeExpansions(start, end byte) ([]string, bool) {
	start = lowerASCII(start)
	end = lowerASCII(end)
	if !isDomainRegexChar(start) || !isDomainRegexChar(end) || end < start {
		return nil, false
	}
	if (isASCIIDigit(start) && !isASCIIDigit(end)) || (isASCIILetter(start) && !isASCIILetter(end)) {
		return nil, false
	}
	return byteRangeExpansions(start, end), true
}

func byteRangeExpansions(start, end byte) []string {
	values := make([]string, 0, int(end-start)+1)
	for char := start; char <= end; char++ {
		values = append(values, string(char))
	}
	return values
}

func lowerASCII(char byte) byte {
	if char >= 'A' && char <= 'Z' {
		return char + ('a' - 'A')
	}
	return char
}

func isASCIIDigit(char byte) bool {
	return char >= '0' && char <= '9'
}

func isASCIILetter(char byte) bool {
	char = lowerASCII(char)
	return char >= 'a' && char <= 'z'
}

func isValidExpandedDomain(value string) bool {
	if value == "" || !strings.Contains(value, ".") || strings.Contains(value, "..") || len(value) > 253 {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for index := 0; index < len(label); index++ {
			if !isDomainRegexChar(label[index]) {
				return false
			}
		}
	}
	return true
}

func wildcardFromRegex(pattern string) string {
	var out strings.Builder

	for index := 0; index < len(pattern); index++ {
		char := pattern[index]
		switch {
		case char == '^' || char == '$':
			continue
		case strings.HasPrefix(pattern[index:], `(^|\.)`):
			out.WriteString(`*.`)
			index += len(`(^|\.)`) - 1
		case char == '\\':
			nextIndex := index + 1
			if nextIndex >= len(pattern) {
				out.WriteByte('*')
				continue
			}
			next := pattern[nextIndex]
			index = nextIndex
			switch {
			case next == '.' || next == '-':
				out.WriteByte(next)
			case next == 'd' || next == 'w' || next == 's' || next == 'S' || next == 'D' || next == 'W':
				out.WriteByte('*')
			case isDomainRegexChar(next):
				out.WriteByte(next)
			default:
				out.WriteByte('*')
			}
		case char == '[':
			close := findRegexCharClassEnd(pattern, index+1)
			if close == -1 {
				return ""
			}
			index = consumeRegexQuantifier(pattern, close)
			out.WriteByte('*')
		case char == '(':
			close := findRegexGroupEnd(pattern, index+1)
			if close == -1 {
				return ""
			}
			index = consumeRegexQuantifier(pattern, close)
			out.WriteByte('*')
		case char == '{':
			close := strings.IndexByte(pattern[index+1:], '}')
			if close == -1 {
				return ""
			}
			index += close + 1
			out.WriteByte('*')
		case char == '|' || char == '?' || char == '+' || char == '*':
			out.WriteByte('*')
		case char == '.' || isDomainRegexChar(char):
			out.WriteByte(char)
		default:
			out.WriteByte('*')
		}
	}

	wildcard := normalizeWildcard(out.String())
	if wildcard == "" || !strings.Contains(wildcard, ".") || !containsAlphaNum(wildcard) {
		return ""
	}
	wildcard = strings.ToLower(wildcard)
	if !isSpecificWildcard(wildcard) {
		return ""
	}
	return wildcard
}

func normalizeWildcard(value string) string {
	value = repeatedStarsRegex.ReplaceAllString(value, "*")
	value = strings.ReplaceAll(value, "?*", "*")
	value = strings.ReplaceAll(value, "*?", "*")
	value = repeatedDotsRegex.ReplaceAllString(value, ".")
	value = leadingMarksRegex.ReplaceAllStringFunc(value, func(match string) string {
		if strings.Contains(match, ".") {
			return "*."
		}
		return "*"
	})
	value = trailingDotsRegex.ReplaceAllString(value, "")
	return value
}

func findRegexCharClassEnd(input string, start int) int {
	escaped := false
	for index := start; index < len(input); index++ {
		char := input[index]
		if escaped {
			escaped = false
			continue
		}
		if char == '\\' {
			escaped = true
			continue
		}
		if char == ']' {
			return index
		}
	}
	return -1
}

func findRegexGroupEnd(input string, start int) int {
	escaped := false
	depth := 1
	for index := start; index < len(input); index++ {
		char := input[index]
		if escaped {
			escaped = false
			continue
		}
		if char == '\\' {
			escaped = true
			continue
		}
		if char == '(' {
			depth++
			continue
		}
		if char == ')' {
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func consumeRegexQuantifier(input string, endIndex int) int {
	nextIndex := endIndex + 1
	if nextIndex >= len(input) {
		return endIndex
	}
	next := input[nextIndex]
	if next == '?' || next == '+' || next == '*' {
		return nextIndex
	}
	if next != '{' {
		return endIndex
	}
	quantifierEnd := strings.IndexByte(input[nextIndex+1:], '}')
	if quantifierEnd == -1 {
		return endIndex
	}
	return nextIndex + quantifierEnd + 1
}

func unescapeRegexDomain(input string) string {
	return strings.ToLower(strings.ReplaceAll(input, `\.`, "."))
}

func containsAlphaNum(input string) bool {
	for index := 0; index < len(input); index++ {
		char := input[index]
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') {
			return true
		}
	}
	return false
}

func isSpecificWildcard(input string) bool {
	labels := strings.Split(strings.Trim(input, "."), ".")
	if len(labels) < 2 {
		return false
	}

	specificChars := 0
	for _, label := range labels[:len(labels)-1] {
		for index := 0; index < len(label); index++ {
			if isDomainRegexChar(label[index]) {
				specificChars++
			}
		}
	}
	return specificChars >= 3
}

func isDomainRegexChar(char byte) bool {
	return (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-'
}
