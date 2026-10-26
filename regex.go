package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	// maxRegexDomainExpansion bounds expansion into DOMAIN/DOMAIN-SUFFIX
	// rules. Surge indexes those rules, so a larger literal expansion is
	// preferred over a smaller set of linearly evaluated wildcard rules.
	maxRegexDomainExpansion = 200
	// maxRegexWildcardPatterns bounds the number of DOMAIN-WILDCARD patterns
	// produced for a single regex.
	maxRegexWildcardPatterns = 32
	// minWildcardSpecificChars is the number of literal characters, outside
	// the last label, required for a wildcard that matches more than the
	// original regex.
	minWildcardSpecificChars = 3
)

const (
	digitChars = "0123456789"
	wordChars  = "abcdefghijklmnopqrstuvwxyz0123456789_"
)

var errRegexExpansionLimit = errors.New("expansion exceeds limit")

// labelAnchorPrefixes match either the start of the hostname or a label
// boundary, which is the DOMAIN-SUFFIX semantics.
var labelAnchorPrefixes = []string{`(^|\.)`, `^(.+\.)*`, `^(.*\.)*`, `^(.+\.)?`, `^(.*\.)?`}

// surgeRulesForRegex converts a geosite regex, which is matched against the
// hostname, into Surge domain rules. Patterns that cannot be expressed as
// domain rules return an error and should be skipped: URL-REGEX matches the
// full request URL, so it is not a valid fallback.
func surgeRulesForRegex(pattern string) ([]string, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, errors.New("empty pattern")
	}

	re, err := parseHostRegex(pattern)
	if err != nil {
		return nil, err
	}
	if rules, ok := re.literalRules(maxRegexDomainExpansion); ok {
		return rules, nil
	}
	if rules, err := re.wildcardRules(false); err == nil {
		return rules, nil
	}
	return re.wildcardRules(true)
}

type regexStartAnchor int

const (
	startUnanchored regexStartAnchor = iota
	startHostname
	startLabel
)

type hostRegex struct {
	start regexStartAnchor
	end   bool
	body  []regexNode
}

type regexNodeKind int

const (
	nodeChar regexNodeKind = iota
	nodeAny
	nodeClass
	nodeNegatedClass
	nodeGroup
)

type regexNode struct {
	kind  regexNodeKind
	chars string
	alts  [][]regexNode
	min   int
	max   int // negative means unbounded
}

func parseHostRegex(pattern string) (*hostRegex, error) {
	re := &hostRegex{}
	body := pattern
	for _, prefix := range labelAnchorPrefixes {
		if strings.HasPrefix(body, prefix) {
			re.start = startLabel
			body = body[len(prefix):]
			break
		}
	}
	if re.start == startUnanchored && strings.HasPrefix(body, "^") {
		re.start = startHostname
		body = body[1:]
	}
	if strings.HasSuffix(body, "$") && !strings.HasSuffix(body, `\$`) {
		re.end = true
		body = body[:len(body)-1]
	}

	parser := regexParser{input: body}
	alts, err := parser.parseAlternation()
	if err != nil {
		return nil, err
	}
	if parser.pos != len(parser.input) {
		return nil, fmt.Errorf("unsupported regex syntax %q at offset %d", parser.input[parser.pos], parser.pos)
	}
	if len(alts) == 1 {
		re.body = alts[0]
		return re, nil
	}
	if re.start != startUnanchored || re.end {
		return nil, errors.New("top-level alternation with anchors is not supported")
	}
	re.body = []regexNode{{kind: nodeGroup, alts: alts, min: 1, max: 1}}
	return re, nil
}

type regexParser struct {
	input string
	pos   int
}

func (p *regexParser) parseAlternation() ([][]regexNode, error) {
	var alts [][]regexNode
	for {
		seq, err := p.parseSequence()
		if err != nil {
			return nil, err
		}
		alts = append(alts, seq)
		if p.pos >= len(p.input) || p.input[p.pos] != '|' {
			return alts, nil
		}
		p.pos++
	}
}

func (p *regexParser) parseSequence() ([]regexNode, error) {
	var seq []regexNode
	for p.pos < len(p.input) {
		if char := p.input[p.pos]; char == '|' || char == ')' {
			break
		}
		node, err := p.parseAtom()
		if err != nil {
			return nil, err
		}
		if err := p.parseQuantifier(&node); err != nil {
			return nil, err
		}
		seq = append(seq, node)
	}
	return seq, nil
}

func (p *regexParser) parseAtom() (regexNode, error) {
	char := p.input[p.pos]
	switch {
	case char == '(':
		return p.parseGroup()
	case char == '[':
		return p.parseCharClass()
	case char == '\\':
		return p.parseEscape()
	case char == '.':
		p.pos++
		return regexNode{kind: nodeAny, min: 1, max: 1}, nil
	case isHostnameChar(char):
		p.pos++
		return charNode(lowerASCII(char)), nil
	default:
		return regexNode{}, fmt.Errorf("unsupported regex syntax %q at offset %d", char, p.pos)
	}
}

func (p *regexParser) parseGroup() (regexNode, error) {
	p.pos++
	if strings.HasPrefix(p.input[p.pos:], "?:") {
		p.pos += 2
	} else if strings.HasPrefix(p.input[p.pos:], "?") {
		return regexNode{}, fmt.Errorf("unsupported group syntax at offset %d", p.pos-1)
	}
	alts, err := p.parseAlternation()
	if err != nil {
		return regexNode{}, err
	}
	if p.pos >= len(p.input) || p.input[p.pos] != ')' {
		return regexNode{}, errors.New("unclosed group")
	}
	p.pos++
	return regexNode{kind: nodeGroup, alts: alts, min: 1, max: 1}, nil
}

func (p *regexParser) parseEscape() (regexNode, error) {
	if p.pos+1 >= len(p.input) {
		return regexNode{}, errors.New("trailing backslash")
	}
	char := p.input[p.pos+1]
	p.pos += 2
	switch char {
	case 'd':
		return classNode(digitChars), nil
	case 'w':
		return classNode(wordChars), nil
	case 'S':
		// Hostnames never contain whitespace, so \S matches any character.
		return regexNode{kind: nodeAny, min: 1, max: 1}, nil
	case 'D', 'W':
		return regexNode{kind: nodeNegatedClass, min: 1, max: 1}, nil
	case '.', '-', '_':
		return charNode(char), nil
	default:
		return regexNode{}, fmt.Errorf(`unsupported escape \%c`, char)
	}
}

func (p *regexParser) parseCharClass() (regexNode, error) {
	p.pos++
	negated := false
	if p.pos < len(p.input) && p.input[p.pos] == '^' {
		negated = true
		p.pos++
	}

	var chars []byte
	for {
		if p.pos >= len(p.input) {
			return regexNode{}, errors.New("unclosed character class")
		}
		char := p.input[p.pos]
		if char == ']' {
			p.pos++
			break
		}
		if char == '\\' {
			if p.pos+1 >= len(p.input) {
				return regexNode{}, errors.New("trailing backslash")
			}
			char = p.input[p.pos+1]
			p.pos += 2
			switch char {
			case 'd':
				chars = append(chars, digitChars...)
				continue
			case 'w':
				chars = append(chars, wordChars...)
				continue
			case '.', '-', '_':
			default:
				return regexNode{}, fmt.Errorf(`unsupported escape \%c in character class`, char)
			}
		} else {
			p.pos++
		}

		if p.pos+1 < len(p.input) && p.input[p.pos] == '-' && p.input[p.pos+1] != ']' {
			end := p.input[p.pos+1]
			p.pos += 2
			rangeChars, ok := charRange(char, end)
			if !ok {
				return regexNode{}, fmt.Errorf("unsupported character range %c-%c", char, end)
			}
			chars = append(chars, rangeChars...)
			continue
		}
		if !isHostnameChar(char) && char != '.' {
			return regexNode{}, fmt.Errorf("unsupported character %q in character class", char)
		}
		chars = append(chars, lowerASCII(char))
	}

	if negated {
		return regexNode{kind: nodeNegatedClass, min: 1, max: 1}, nil
	}
	if len(chars) == 0 {
		return regexNode{}, errors.New("empty character class")
	}
	return classNode(string(chars)), nil
}

func (p *regexParser) parseQuantifier(node *regexNode) error {
	if p.pos >= len(p.input) {
		return nil
	}
	switch p.input[p.pos] {
	case '?':
		node.min, node.max = 0, 1
		p.pos++
	case '*':
		node.min, node.max = 0, -1
		p.pos++
	case '+':
		node.min, node.max = 1, -1
		p.pos++
	case '{':
		end := strings.IndexByte(p.input[p.pos:], '}')
		if end == -1 {
			return errors.New("unclosed repeat quantifier")
		}
		lo, hi, ok := parseRegexRepeatBounds(p.input[p.pos+1 : p.pos+end])
		if !ok {
			return fmt.Errorf("unsupported repeat quantifier %q", p.input[p.pos:p.pos+end+1])
		}
		node.min, node.max = lo, hi
		p.pos += end + 1
	default:
		return nil
	}
	// A lazy quantifier matches the same set of hostnames.
	if p.pos < len(p.input) && p.input[p.pos] == '?' {
		p.pos++
	}
	return nil
}

func parseRegexRepeatBounds(value string) (int, int, bool) {
	lowText, highText, hasComma := strings.Cut(value, ",")
	lo, err := strconv.Atoi(lowText)
	if err != nil || lo < 0 {
		return 0, 0, false
	}
	if !hasComma {
		return lo, lo, true
	}
	if highText == "" {
		return lo, -1, true
	}
	hi, err := strconv.Atoi(highText)
	if err != nil || hi < lo {
		return 0, 0, false
	}
	return lo, hi, true
}

func charNode(char byte) regexNode {
	return regexNode{kind: nodeChar, chars: string(char), min: 1, max: 1}
}

// classNode deduplicates chars while keeping their first-seen order, so
// literal expansion follows the order written in the regex.
func classNode(chars string) regexNode {
	var unique []byte
	for index := 0; index < len(chars); index++ {
		if strings.IndexByte(string(unique), chars[index]) == -1 {
			unique = append(unique, chars[index])
		}
	}
	if len(unique) == 1 {
		return charNode(unique[0])
	}
	return regexNode{kind: nodeClass, chars: string(unique), min: 1, max: 1}
}

func charRange(start, end byte) ([]byte, bool) {
	start = lowerASCII(start)
	end = lowerASCII(end)
	if end < start {
		return nil, false
	}
	if !(isASCIIDigit(start) && isASCIIDigit(end)) && !(isASCIILetter(start) && isASCIILetter(end)) {
		return nil, false
	}
	chars := make([]byte, 0, int(end-start)+1)
	for char := start; char <= end; char++ {
		chars = append(chars, char)
	}
	return chars, true
}

type globTokenKind int

const (
	globChar globTokenKind = iota
	globAny
	globClass
	globStar
)

type globToken struct {
	kind  globTokenKind
	chars string
}

type globPattern []globToken

func (p globPattern) String() string {
	var b strings.Builder
	for _, token := range p {
		switch token.kind {
		case globChar:
			b.WriteString(token.chars)
		case globAny:
			b.WriteByte('?')
		case globClass:
			// Classes are enumerated instead of written as ranges: the Surge
			// manual documents [...] classes but not range syntax.
			b.WriteByte('[')
			b.WriteString(orderClassChars(token.chars))
			b.WriteByte(']')
		case globStar:
			b.WriteByte('*')
		}
	}
	return b.String()
}

func (p globPattern) isLiteral() bool {
	for _, token := range p {
		if token.kind != globChar {
			return false
		}
	}
	return true
}

// canMatchDot reports whether any position of the pattern can match ".".
// Patterns that cannot only match single-label hostnames.
func (p globPattern) canMatchDot() bool {
	for _, token := range p {
		switch token.kind {
		case globAny, globStar:
			return true
		case globChar, globClass:
			if strings.IndexByte(token.chars, '.') != -1 {
				return true
			}
		}
	}
	return false
}

// keyword returns the literal text of a *text* pattern, which is exactly a
// DOMAIN-KEYWORD rule.
func (p globPattern) keyword() (string, bool) {
	if len(p) < 3 || p[0].kind != globStar || p[len(p)-1].kind != globStar {
		return "", false
	}
	inner := p[1 : len(p)-1]
	if !inner.isLiteral() {
		return "", false
	}
	return inner.String(), true
}

// specificChars counts literal hostname characters outside the last label.
func (p globPattern) specificChars() int {
	lastDot := -1
	for index, token := range p {
		if token.kind == globChar && token.chars == "." {
			lastDot = index
		}
	}
	count := 0
	for _, token := range p[:max(lastDot, 0)] {
		if token.kind == globChar && isHostnameChar(token.chars[0]) {
			count++
		}
	}
	return count
}

func appendGlobToken(p globPattern, token globToken) globPattern {
	if token.kind == globStar && len(p) > 0 && p[len(p)-1].kind == globStar {
		return p
	}
	return append(p, token)
}

func concatGlobPatterns(prefix, suffix globPattern) globPattern {
	out := make(globPattern, 0, len(prefix)+len(suffix))
	out = append(out, prefix...)
	for _, token := range suffix {
		out = appendGlobToken(out, token)
	}
	return out
}

// normalizeGlobPattern rewrites each run of "*" and "?" that contains a "*"
// as the same number of "?" followed by one "*", then rewrites a leading
// "?*." as "*.": hostnames never start with a dot, so both match the same
// hostnames.
func normalizeGlobPattern(p globPattern) globPattern {
	out := make(globPattern, 0, len(p))
	for index := 0; index < len(p); {
		if p[index].kind != globStar && p[index].kind != globAny {
			out = append(out, p[index])
			index++
			continue
		}
		anys, hasStar := 0, false
		for ; index < len(p) && (p[index].kind == globStar || p[index].kind == globAny); index++ {
			if p[index].kind == globStar {
				hasStar = true
			} else {
				anys++
			}
		}
		for ; anys > 0; anys-- {
			out = append(out, globToken{kind: globAny})
		}
		if hasStar {
			out = append(out, globToken{kind: globStar})
		}
	}
	if len(out) >= 3 && out[0].kind == globAny && out[1].kind == globStar && out[2].kind == globChar && out[2].chars == "." {
		return out[1:]
	}
	return out
}

type globPatternSet struct {
	limit int
	items []globPattern
	seen  map[string]bool
}

func newGlobPatternSet(limit int) *globPatternSet {
	return &globPatternSet{limit: limit, seen: make(map[string]bool)}
}

func (s *globPatternSet) add(p globPattern) error {
	key := p.String()
	if s.seen[key] {
		return nil
	}
	if len(s.items) >= s.limit {
		return errRegexExpansionLimit
	}
	s.seen[key] = true
	s.items = append(s.items, p)
	return nil
}

// globExpander expands a parsed regex into glob patterns. In literal mode
// every pattern is a plain hostname. Otherwise character classes are kept as
// glob classes and unbounded repeats become "*". When collapse is set,
// variable-length repeats also become "*" to stay within the limit.
type globExpander struct {
	limit    int
	literal  bool
	collapse bool
	// approx is set once any pattern may match more than the regex.
	approx bool
}

func (e *globExpander) sequence(nodes []regexNode) ([]globPattern, error) {
	out := []globPattern{{}}
	for _, node := range nodes {
		part, err := e.node(node)
		if err != nil {
			return nil, err
		}
		if out, err = e.concat(out, part); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (e *globExpander) node(node regexNode) ([]globPattern, error) {
	base, err := e.atom(node)
	if err != nil {
		return nil, err
	}
	if node.max < 0 {
		return e.unbounded(node, base)
	}
	if e.collapse && node.max-node.min > 1 {
		e.approx = true
		return e.unbounded(node, base)
	}

	set := newGlobPatternSet(e.limit)
	current := []globPattern{{}}
	for count := 0; ; count++ {
		if count >= node.min {
			for _, p := range current {
				if err := set.add(p); err != nil {
					return nil, err
				}
			}
		}
		if count == node.max {
			return set.items, nil
		}
		if current, err = e.concat(current, base); err != nil {
			return nil, err
		}
	}
}

// unbounded repeats base at least node.min times followed by "*". Only an
// any-character repeat such as .+ converts exactly; other repeats are
// approximated because a glob "*" also matches dots.
func (e *globExpander) unbounded(node regexNode, base []globPattern) ([]globPattern, error) {
	if e.literal {
		return nil, errors.New("unbounded repeat cannot be expanded")
	}
	if node.kind != nodeAny {
		e.approx = true
	}
	current := []globPattern{{}}
	var err error
	for count := 0; count < node.min; count++ {
		if current, err = e.concat(current, base); err != nil {
			return nil, err
		}
	}
	return e.concat(current, []globPattern{{{kind: globStar}}})
}

func (e *globExpander) atom(node regexNode) ([]globPattern, error) {
	switch node.kind {
	case nodeChar:
		return []globPattern{{{kind: globChar, chars: node.chars}}}, nil
	case nodeAny, nodeNegatedClass:
		if e.literal {
			return nil, errors.New("wildcard character cannot be expanded")
		}
		if node.kind == nodeNegatedClass {
			e.approx = true
		}
		return []globPattern{{{kind: globAny}}}, nil
	case nodeClass:
		if !e.literal {
			return []globPattern{{{kind: globClass, chars: node.chars}}}, nil
		}
		set := newGlobPatternSet(e.limit)
		for index := 0; index < len(node.chars); index++ {
			if err := set.add(globPattern{{kind: globChar, chars: node.chars[index : index+1]}}); err != nil {
				return nil, err
			}
		}
		return set.items, nil
	case nodeGroup:
		set := newGlobPatternSet(e.limit)
		for _, alt := range node.alts {
			patterns, err := e.sequence(alt)
			if err != nil {
				return nil, err
			}
			for _, p := range patterns {
				if err := set.add(p); err != nil {
					return nil, err
				}
			}
		}
		return set.items, nil
	default:
		return nil, errors.New("unknown regex node")
	}
}

func (e *globExpander) concat(prefixes, suffixes []globPattern) ([]globPattern, error) {
	set := newGlobPatternSet(e.limit)
	for _, prefix := range prefixes {
		for _, suffix := range suffixes {
			if err := set.add(concatGlobPatterns(prefix, suffix)); err != nil {
				return nil, err
			}
		}
	}
	return set.items, nil
}

// literalRules expands fully anchored finite patterns into indexed
// DOMAIN/DOMAIN-SUFFIX rules.
func (re *hostRegex) literalRules(limit int) ([]string, bool) {
	if re.start == startUnanchored || !re.end {
		return nil, false
	}
	expander := &globExpander{limit: limit, literal: true}
	patterns, err := expander.sequence(re.body)
	if err != nil || len(patterns) == 0 {
		return nil, false
	}

	prefix := "DOMAIN,"
	if re.start == startLabel {
		prefix = "DOMAIN-SUFFIX,"
	}
	rules := make([]string, 0, len(patterns))
	for _, p := range patterns {
		value := p.String()
		if !isValidExpandedDomain(value) {
			return nil, false
		}
		rules = append(rules, prefix+value)
	}
	return rules, true
}

// wildcardRules converts the regex into glob patterns. Literal patterns still
// become DOMAIN/DOMAIN-SUFFIX rules; a label anchor produces both "x" and
// "*.x" because "*.x" does not match "x" itself.
func (re *hostRegex) wildcardRules(collapse bool) ([]string, error) {
	expander := &globExpander{limit: maxRegexWildcardPatterns, collapse: collapse}
	patterns, err := expander.sequence(re.body)
	if err != nil {
		return nil, err
	}

	var rules []string
	seen := make(map[string]bool)
	add := func(rule string) {
		if !seen[rule] {
			seen[rule] = true
			rules = append(rules, rule)
		}
	}
	for _, p := range patterns {
		p = re.anchorGlobPattern(p)

		if p.isLiteral() {
			value := p.String()
			if !isValidExpandedDomain(value) {
				return nil, fmt.Errorf("expanded value %q is not a valid domain", value)
			}
			if re.start == startLabel {
				add("DOMAIN-SUFFIX," + value)
			} else {
				add("DOMAIN," + value)
			}
			continue
		}
		if keyword, ok := p.keyword(); ok && !expander.approx {
			add("DOMAIN-KEYWORD," + keyword)
			continue
		}
		if !p.canMatchDot() {
			return nil, fmt.Errorf("pattern %q only matches single-label hostnames", p.String())
		}
		if expander.approx && p.specificChars() < minWildcardSpecificChars {
			return nil, fmt.Errorf("approximate wildcard %q is too broad", p.String())
		}

		value := p.String()
		add("DOMAIN-WILDCARD," + value)
		if re.start == startLabel && p[0].kind != globStar {
			add("DOMAIN-WILDCARD,*." + value)
		}
	}
	return rules, nil
}

func (re *hostRegex) anchorGlobPattern(p globPattern) globPattern {
	star := globPattern{{kind: globStar}}
	if re.start == startUnanchored {
		p = concatGlobPatterns(star, p)
	}
	if !re.end {
		p = concatGlobPatterns(p, star)
	}
	return normalizeGlobPattern(p)
}

// orderClassChars moves "-" to the end of a class, where it is literal in
// any glob class syntax.
func orderClassChars(chars string) string {
	if strings.IndexByte(chars, '-') == -1 {
		return chars
	}
	return strings.ReplaceAll(chars, "-", "") + "-"
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

func isHostnameChar(char byte) bool {
	return isASCIILetter(char) || isASCIIDigit(char) || char == '-' || char == '_'
}

func isValidExpandedDomain(value string) bool {
	if value == "" || !strings.Contains(value, ".") || len(value) > 253 {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for index := 0; index < len(label); index++ {
			if !isHostnameChar(label[index]) {
				return false
			}
		}
	}
	return true
}
