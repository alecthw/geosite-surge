package main

import (
	"regexp"
	"strings"
)

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

func surgeRuleForRegex(pattern string) string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return ""
	}

	if match := exactDomainRegex.FindStringSubmatch(pattern); match != nil {
		return "DOMAIN," + unescapeRegexDomain(match[1])
	}
	if match := suffixDomainRegex.FindStringSubmatch(pattern); match != nil {
		return "DOMAIN-SUFFIX," + unescapeRegexDomain(match[1])
	}
	if match := repeatedSubdomainRegex.FindStringSubmatch(pattern); match != nil {
		return "DOMAIN-SUFFIX," + unescapeRegexDomain(match[1])
	}

	if advancedRegexTokens.MatchString(pattern) {
		return "URL-REGEX," + pattern
	}

	if wildcard := wildcardFromRegex(pattern); wildcard != "" {
		return "DOMAIN-WILDCARD," + wildcard
	}
	return "URL-REGEX," + pattern
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
