package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type geoIndex struct {
	order []string
	sites map[string]*geoSite
}

type resolvedDomain struct {
	domain domain
	from   string
}

type includeRef struct {
	code      string
	mustAttrs []string
	banAttrs  []string
}

func newGeoIndex(list *geoSiteList) *geoIndex {
	idx := &geoIndex{sites: make(map[string]*geoSite)}
	for _, site := range list.Entries {
		key := normalizeCode(site.Code)
		if key == "" {
			continue
		}
		existing := idx.sites[key]
		if existing == nil {
			copySite := geoSite{Code: key}
			idx.sites[key] = &copySite
			idx.order = append(idx.order, key)
			existing = &copySite
		}
		existing.Domains = append(existing.Domains, site.Domains...)
	}
	return idx
}

func (idx *geoIndex) codes() []string {
	return append([]string(nil), idx.order...)
}

func (idx *geoIndex) resolve(code, attr string) ([]resolvedDomain, error) {
	state := resolveState{idx: idx, stack: make(map[string]bool)}
	domains, err := state.resolve(normalizeCode(code))
	if err != nil {
		return nil, err
	}
	attr = normalizeAttr(attr)
	if attr == "" {
		return domains, nil
	}
	return filterResolvedDomains(domains, attr), nil
}

type resolveState struct {
	idx   *geoIndex
	stack map[string]bool
}

func (s *resolveState) resolve(code string) ([]resolvedDomain, error) {
	key := code
	if s.stack[key] {
		return nil, fmt.Errorf("cyclic geosite include detected at %s", key)
	}

	site := s.idx.sites[code]
	if site == nil {
		return nil, fmt.Errorf("geosite code %q not found", code)
	}

	s.stack[key] = true
	defer delete(s.stack, key)

	var out []resolvedDomain
	for _, d := range site.Domains {
		include, ok := parseInclude(d)
		if ok {
			children, err := s.resolve(include.code)
			if err != nil {
				return nil, err
			}
			for _, child := range children {
				if matchesIncludeFilters(child.domain, include) {
					out = append(out, child)
				}
			}
			continue
		}
		out = append(out, resolvedDomain{domain: d, from: code})
	}
	return out, nil
}

func filterResolvedDomains(domains []resolvedDomain, attr string) []resolvedDomain {
	var out []resolvedDomain
	for _, item := range domains {
		if hasAttribute(item.domain, attr) {
			out = append(out, item)
		}
	}
	return out
}

func buildRuleFiles(idx *geoIndex, codes []string) ([]ruleFile, error) {
	var files []ruleFile
	for _, code := range codes {
		baseCode, explicitAttr := splitCodeAttr(code)
		baseCode = normalizeCode(baseCode)
		explicitAttr = strings.ToLower(explicitAttr)
		if baseCode == "" {
			continue
		}

		if explicitAttr != "" {
			domains, err := idx.resolve(baseCode, explicitAttr)
			if err != nil {
				return nil, err
			}
			files = append(files, newRuleFile(baseCode+"@"+explicitAttr, domains))
			continue
		}

		allDomains, err := idx.resolve(baseCode, "")
		if err != nil {
			return nil, err
		}
		files = append(files, newRuleFile(baseCode, allDomains))

		for _, attr := range collectAttributes(allDomains) {
			attrDomains, err := idx.resolve(baseCode, attr)
			if err != nil {
				return nil, err
			}
			files = append(files, newRuleFile(baseCode+"@"+attr, attrDomains))
		}
	}
	return files, nil
}

func collectAttributes(domains []resolvedDomain) []string {
	seen := make(map[string]bool)
	for _, item := range domains {
		for _, attr := range item.domain.Attributes {
			attr = normalizeAttr(attr)
			if attr != "" && !strings.HasPrefix(attr, "-") {
				seen[attr] = true
			}
		}
	}
	attrs := make([]string, 0, len(seen))
	for attr := range seen {
		attrs = append(attrs, attr)
	}
	sort.Strings(attrs)
	return attrs
}

type ruleFile struct {
	Name  string
	Rules []string
}

func newRuleFile(name string, domains []resolvedDomain) ruleFile {
	seen := make(map[string]bool)
	rules := make([]string, 0, len(domains))
	for _, item := range domains {
		rule, ok := surgeRule(item.domain)
		if !ok {
			continue
		}
		if seen[rule] {
			continue
		}
		seen[rule] = true
		rules = append(rules, rule)
	}
	return ruleFile{Name: sanitizeFileStem(name), Rules: rules}
}

func surgeRule(d domain) (string, bool) {
	value := strings.TrimSpace(d.Value)
	if value == "" {
		return "", false
	}
	switch d.Type {
	case domainTypePlain:
		return "DOMAIN-KEYWORD," + value, true
	case domainTypeRegex:
		rule := surgeRuleForRegex(value)
		return rule, rule != ""
	case domainTypeRoot:
		return "DOMAIN-SUFFIX," + value, true
	case domainTypeFull:
		return "DOMAIN," + value, true
	default:
		return "", false
	}
}

func parseInclude(d domain) (includeRef, bool) {
	value := strings.TrimSpace(d.Value)
	if !strings.HasPrefix(strings.ToLower(value), "include:") {
		return includeRef{}, false
	}
	target := strings.TrimSpace(value[len("include:"):])
	if target == "" {
		return includeRef{}, false
	}

	parts := strings.Fields(target)
	code, inlineAttr := splitCodeAttr(parts[0])
	ref := includeRef{code: normalizeCode(code)}
	if inlineAttr != "" {
		ref.addFilterAttr(inlineAttr)
	}
	for _, part := range parts[1:] {
		if strings.HasPrefix(part, "@") {
			ref.addFilterAttr(part[1:])
		}
	}
	for _, attr := range d.Attributes {
		ref.addFilterAttr(attr)
	}
	return ref, ref.code != ""
}

func splitCodeAttr(value string) (code, attr string) {
	value = strings.TrimSpace(value)
	if at := strings.LastIndexByte(value, '@'); at >= 0 {
		return value[:at], value[at+1:]
	}
	return value, ""
}

func (r *includeRef) addFilterAttr(attr string) {
	attr = normalizeAttr(attr)
	if attr == "" {
		return
	}
	if strings.HasPrefix(attr, "-") {
		banAttr := normalizeAttr(strings.TrimPrefix(attr, "-"))
		if banAttr != "" {
			r.banAttrs = appendUniqueString(r.banAttrs, banAttr)
		}
		return
	}
	r.mustAttrs = appendUniqueString(r.mustAttrs, attr)
}

func matchesIncludeFilters(d domain, include includeRef) bool {
	if len(include.mustAttrs) == 0 && len(include.banAttrs) == 0 {
		return true
	}
	if len(d.Attributes) == 0 {
		return len(include.mustAttrs) == 0
	}
	for _, attr := range include.mustAttrs {
		if !hasAttribute(d, attr) {
			return false
		}
	}
	for _, attr := range include.banAttrs {
		if hasAttribute(d, attr) {
			return false
		}
	}
	return true
}

func hasAttribute(d domain, attr string) bool {
	attr = normalizeAttr(attr)
	for _, item := range d.Attributes {
		if normalizeAttr(item) == attr {
			return true
		}
	}
	return false
}

func appendUniqueString(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func normalizeAttr(attr string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(attr, "@")))
}

func normalizeCode(code string) string {
	return strings.ToLower(strings.TrimSpace(code))
}

func sanitizeFileStem(name string) string {
	name = normalizeCode(name)
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.' || r == '@' || r == '!' || r == '+':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "unnamed"
	}
	return b.String()
}

func ruleFilePath(outDir string, f ruleFile) string {
	return filepath.Join(outDir, f.Name+".list")
}
