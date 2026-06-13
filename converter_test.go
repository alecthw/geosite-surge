package main

import (
	"reflect"
	"testing"
)

func TestBuildRuleFilesExportsAttributesSeparately(t *testing.T) {
	idx := newGeoIndex(&geoSiteList{Entries: []geoSite{
		{
			Code: "steam",
			Domains: []domain{
				{Type: domainTypeRoot, Value: "steampowered.com"},
				{Type: domainTypeFull, Value: "steamchina.com", Attributes: []string{"cn"}},
			},
		},
	}})

	files, err := buildRuleFiles(idx, []string{"steam"})
	if err != nil {
		t.Fatal(err)
	}

	got := map[string][]string{}
	for _, file := range files {
		got[file.Name] = file.Rules
	}

	want := map[string][]string{
		"steam":    {"DOMAIN-SUFFIX,steampowered.com", "DOMAIN,steamchina.com"},
		"steam@cn": {"DOMAIN,steamchina.com"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("files mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestBuildRuleFilesResolvesIncludesRecursively(t *testing.T) {
	idx := newGeoIndex(&geoSiteList{Entries: []geoSite{
		{
			Code: "parent",
			Domains: []domain{
				{Type: domainTypePlain, Value: "include:child"},
			},
		},
		{
			Code: "child",
			Domains: []domain{
				{Type: domainTypePlain, Value: "include:leaf"},
			},
		},
		{
			Code: "leaf",
			Domains: []domain{
				{Type: domainTypeRoot, Value: "example.com"},
			},
		},
	}})

	files, err := buildRuleFiles(idx, []string{"parent"})
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	want := []string{"DOMAIN-SUFFIX,example.com"}
	if !reflect.DeepEqual(files[0].Rules, want) {
		t.Fatalf("rules mismatch\n got: %#v\nwant: %#v", files[0].Rules, want)
	}
}

func TestBuildRuleFilesSupportsIncludeAttributeFilters(t *testing.T) {
	idx := newGeoIndex(&geoSiteList{Entries: []geoSite{
		{
			Code: "parent",
			Domains: []domain{
				{Type: domainTypePlain, Value: "include:child @cn @-ads"},
			},
		},
		{
			Code: "child",
			Domains: []domain{
				{Type: domainTypeRoot, Value: "service.com", Attributes: []string{"cn"}},
				{Type: domainTypeRoot, Value: "ads.service.com", Attributes: []string{"cn", "ads"}},
				{Type: domainTypeRoot, Value: "plain.com"},
			},
		},
	}})

	files, err := buildRuleFiles(idx, []string{"parent"})
	if err != nil {
		t.Fatal(err)
	}

	got := map[string][]string{}
	for _, file := range files {
		got[file.Name] = file.Rules
	}
	want := map[string][]string{
		"parent":    {"DOMAIN-SUFFIX,service.com"},
		"parent@cn": {"DOMAIN-SUFFIX,service.com"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("files mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestBuildRuleFilesSupportsIncludeAttributeFiltersFromAttributes(t *testing.T) {
	idx := newGeoIndex(&geoSiteList{Entries: []geoSite{
		{
			Code: "parent",
			Domains: []domain{
				{Type: domainTypePlain, Value: "include:child", Attributes: []string{"cn", "-ads"}},
			},
		},
		{
			Code: "child",
			Domains: []domain{
				{Type: domainTypeRoot, Value: "service.com", Attributes: []string{"cn"}},
				{Type: domainTypeRoot, Value: "ads.service.com", Attributes: []string{"cn", "ads"}},
				{Type: domainTypeRoot, Value: "plain.com"},
			},
		},
	}})

	files, err := buildRuleFiles(idx, []string{"parent"})
	if err != nil {
		t.Fatal(err)
	}

	got := map[string][]string{}
	for _, file := range files {
		got[file.Name] = file.Rules
	}
	want := map[string][]string{
		"parent":    {"DOMAIN-SUFFIX,service.com"},
		"parent@cn": {"DOMAIN-SUFFIX,service.com"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("files mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestSurgeRulesExpandsFiniteRegexToDomainSuffixes(t *testing.T) {
	got := surgeRules(domain{Type: domainTypeRegex, Value: "(^|\\.)tt[1-2][0-1]\\.tv$"})
	want := []string{
		"DOMAIN-SUFFIX,tt10.tv",
		"DOMAIN-SUFFIX,tt11.tv",
		"DOMAIN-SUFFIX,tt20.tv",
		"DOMAIN-SUFFIX,tt21.tv",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rules mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestSurgeRulesExpandsFiniteRegexAlternativesInsteadOfBroadWildcard(t *testing.T) {
	got := surgeRules(domain{Type: domainTypeRegex, Value: "(^|\\.)91porn\\.(best|com|tw)$"})
	want := []string{
		"DOMAIN-SUFFIX,91porn.best",
		"DOMAIN-SUFFIX,91porn.com",
		"DOMAIN-SUFFIX,91porn.tw",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rules mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestSurgeRulesDoesNotExpandLargeFiniteRegex(t *testing.T) {
	got := surgeRules(domain{Type: domainTypeRegex, Value: "(^|\\.)xv[0-9]{4}\\.top$"})
	want := []string{"URL-REGEX,(^|\\.)xv[0-9]{4}\\.top$"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rules mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestSurgeRuleConvertsRegexToSpecificDomainRule(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    string
	}{
		{
			name:    "exact domain",
			pattern: `^github\.com$`,
			want:    "DOMAIN,github.com",
		},
		{
			name:    "domain suffix",
			pattern: `(^|\.)netflix\.com$`,
			want:    "DOMAIN-SUFFIX,netflix.com",
		},
		{
			name:    "repeated subdomain suffix",
			pattern: `^(.+\.)*example\.com$`,
			want:    "DOMAIN-SUFFIX,example.com",
		},
		{
			name:    "wildcard",
			pattern: `^cdn\d-epicgames-\d+\.file\.myqcloud\.com$`,
			want:    "DOMAIN-WILDCARD,cdn*-epicgames-*.file.myqcloud.com",
		},
		{
			name:    "unsupported fallback",
			pattern: `^a(?=b)\.example\.com$`,
			want:    `URL-REGEX,^a(?=b)\.example\.com$`,
		},
		{
			name:    "broad wildcard fallback",
			pattern: `.*\.com`,
			want:    `URL-REGEX,.*\.com`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := surgeRule(domain{Type: domainTypeRegex, Value: tt.pattern})
			if !ok {
				t.Fatal("surgeRule returned ok=false")
			}
			if got != tt.want {
				t.Fatalf("rule mismatch\n got: %s\nwant: %s", got, tt.want)
			}
		})
	}
}
