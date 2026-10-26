package main

import (
	"reflect"
	"strings"
	"testing"
)

const testDigitClass = "[0123456789]"

func TestSurgeRulesForRegex(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    []string
	}{
		{
			name:    "exact domain",
			pattern: `^github\.com$`,
			want:    []string{"DOMAIN,github.com"},
		},
		{
			name:    "domain suffix",
			pattern: `(^|\.)netflix\.com$`,
			want:    []string{"DOMAIN-SUFFIX,netflix.com"},
		},
		{
			name:    "repeated subdomain suffix",
			pattern: `^(.+\.)*example\.com$`,
			want:    []string{"DOMAIN-SUFFIX,example.com"},
		},
		{
			name:    "expanded character classes",
			pattern: `(^|\.)tt[1-2][0-1]\.tv$`,
			want: []string{
				"DOMAIN-SUFFIX,tt10.tv",
				"DOMAIN-SUFFIX,tt11.tv",
				"DOMAIN-SUFFIX,tt20.tv",
				"DOMAIN-SUFFIX,tt21.tv",
			},
		},
		{
			name:    "expanded alternatives instead of broad wildcard",
			pattern: `(^|\.)91porn\.(best|com|tw)$`,
			want: []string{
				"DOMAIN-SUFFIX,91porn.best",
				"DOMAIN-SUFFIX,91porn.com",
				"DOMAIN-SUFFIX,91porn.tw",
			},
		},
		{
			name:    "expanded lazy quantifier",
			pattern: `(^|\.)ab[1-2]{2}?\.xyz$`,
			want: []string{
				"DOMAIN-SUFFIX,ab11.xyz",
				"DOMAIN-SUFFIX,ab12.xyz",
				"DOMAIN-SUFFIX,ab21.xyz",
				"DOMAIN-SUFFIX,ab22.xyz",
			},
		},
		{
			name:    "wildcard keeps label anchored apex",
			pattern: `(^|\.)xv[0-9]{4}\.top$`,
			want: []string{
				"DOMAIN-WILDCARD,xv" + strings.Repeat(testDigitClass, 4) + ".top",
				"DOMAIN-WILDCARD,*.xv" + strings.Repeat(testDigitClass, 4) + ".top",
			},
		},
		{
			name:    "expanded optional group",
			pattern: `(^|\.)783([a-c][a-c]?)?\.tv$`,
			want: []string{
				"DOMAIN-SUFFIX,783.tv",
				"DOMAIN-SUFFIX,783a.tv",
				"DOMAIN-SUFFIX,783aa.tv",
				"DOMAIN-SUFFIX,783ab.tv",
				"DOMAIN-SUFFIX,783ac.tv",
				"DOMAIN-SUFFIX,783b.tv",
				"DOMAIN-SUFFIX,783ba.tv",
				"DOMAIN-SUFFIX,783bb.tv",
				"DOMAIN-SUFFIX,783bc.tv",
				"DOMAIN-SUFFIX,783c.tv",
				"DOMAIN-SUFFIX,783ca.tv",
				"DOMAIN-SUFFIX,783cb.tv",
				"DOMAIN-SUFFIX,783cc.tv",
			},
		},
		{
			name:    "wildcard with literal apex and class patterns",
			pattern: `(^|\.)fi11sm([0-9]{1,3})?\.com$`,
			want: []string{
				"DOMAIN-SUFFIX,fi11sm.com",
				"DOMAIN-WILDCARD,fi11sm" + testDigitClass + ".com",
				"DOMAIN-WILDCARD,*.fi11sm" + testDigitClass + ".com",
				"DOMAIN-WILDCARD,fi11sm" + strings.Repeat(testDigitClass, 2) + ".com",
				"DOMAIN-WILDCARD,*.fi11sm" + strings.Repeat(testDigitClass, 2) + ".com",
				"DOMAIN-WILDCARD,fi11sm" + strings.Repeat(testDigitClass, 3) + ".com",
				"DOMAIN-WILDCARD,*.fi11sm" + strings.Repeat(testDigitClass, 3) + ".com",
			},
		},
		{
			name:    "regex dot matches one character",
			pattern: `^nis.+\.10010\.com$`,
			want:    []string{"DOMAIN-WILDCARD,nis?*.10010.com"},
		},
		{
			name:    "regex dot before label",
			pattern: `^.+-mihayo\.akamaized\.net$`,
			want:    []string{"DOMAIN-WILDCARD,?*-mihayo.akamaized.net"},
		},
		{
			name:    "unanchored any-character prefix",
			pattern: `.+\.awsdns-[0-9][0-9]\.(co\.uk|com)$`,
			want: []string{
				"DOMAIN-WILDCARD,*.awsdns-" + strings.Repeat(testDigitClass, 2) + ".co.uk",
				"DOMAIN-WILDCARD,*.awsdns-" + strings.Repeat(testDigitClass, 2) + ".com",
			},
		},
		{
			name:    "repeat inside label keeps leading class",
			pattern: `(^|\.)[1-9]+aipai\.(com|net)$`,
			want: []string{
				"DOMAIN-WILDCARD,[123456789]*aipai.com",
				"DOMAIN-WILDCARD,*.[123456789]*aipai.com",
				"DOMAIN-WILDCARD,[123456789]*aipai.net",
				"DOMAIN-WILDCARD,*.[123456789]*aipai.net",
			},
		},
		{
			name:    "unanchored start",
			pattern: `javdb\d+\.com$`,
			want:    []string{"DOMAIN-WILDCARD,*javdb" + testDigitClass + "*.com"},
		},
		{
			name:    "digit escapes",
			pattern: `^cdn\d-epicgames-\d+\.file\.myqcloud\.com$`,
			want:    []string{"DOMAIN-WILDCARD,cdn" + testDigitClass + "-epicgames-" + testDigitClass + "*.file.myqcloud.com"},
		},
		{
			name:    "class with hyphen",
			pattern: `^a[x\-]\.example\.com$`,
			want: []string{
				"DOMAIN,ax.example.com",
				"DOMAIN,a-.example.com",
			},
		},
		{
			name:    "collapsed long repeat",
			pattern: `(^|\.)abc[0-9]{0,40}\.com$`,
			want: []string{
				"DOMAIN-WILDCARD,abc*.com",
				"DOMAIN-WILDCARD,*.abc*.com",
			},
		},
		{
			name:    "unanchored keyword",
			pattern: `google`,
			want:    []string{"DOMAIN-KEYWORD,google"},
		},
		{
			name:    "regex dot inside label",
			pattern: `(^|\.)91cg([1-9]|[0-9][1-9])?.com$`,
			want: []string{
				"DOMAIN-WILDCARD,91cg?com",
				"DOMAIN-WILDCARD,*.91cg?com",
				"DOMAIN-WILDCARD,91cg[123456789]?com",
				"DOMAIN-WILDCARD,*.91cg[123456789]?com",
				"DOMAIN-WILDCARD,91cg[0123456789][123456789]?com",
				"DOMAIN-WILDCARD,*.91cg[0123456789][123456789]?com",
			},
		},
		{
			name:    "unanchored any-character keyword",
			pattern: `.*\.com`,
			want:    []string{"DOMAIN-KEYWORD,.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := surgeRulesForRegex(tt.pattern)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("rules mismatch\n got: %#v\nwant: %#v", got, tt.want)
			}
		})
	}
}

func TestSurgeRulesForRegexExpandsUpToLimit(t *testing.T) {
	got, err := surgeRulesForRegex(`(^|\.)18jmttios[0-9]{2}\.com$`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 100 || got[0] != "DOMAIN-SUFFIX,18jmttios00.com" || got[99] != "DOMAIN-SUFFIX,18jmttios99.com" {
		t.Fatalf("unexpected expansion: %d rules, first %q", len(got), got[0])
	}
}

func TestSurgeRulesForRegexSkipsUnsupportedPatterns(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		reason  string
	}{
		{
			name:    "lookahead",
			pattern: `^a(?=b)\.example\.com$`,
			reason:  "unsupported group syntax",
		},
		{
			name:    "backreference",
			pattern: `^(a)\1\.example\.com$`,
			reason:  `unsupported escape \1`,
		},
		{
			name:    "single-label hostname",
			pattern: `^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`,
			reason:  "single-label",
		},
		{
			name:    "too broad approximation",
			pattern: `^[a-z]+\.com$`,
			reason:  "too broad",
		},
		{
			name:    "anchored top-level alternation",
			pattern: `^a\.com|b\.com$`,
			reason:  "top-level alternation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := surgeRulesForRegex(tt.pattern)
			if err == nil {
				t.Fatalf("expected error, got rules %#v", got)
			}
			if !strings.Contains(err.Error(), tt.reason) {
				t.Fatalf("error %q does not contain %q", err, tt.reason)
			}
		})
	}
}
