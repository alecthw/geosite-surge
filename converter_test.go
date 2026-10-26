package main

import (
	"reflect"
	"strings"
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

func TestNewRuleFilePrunesRulesCoveredBySuffix(t *testing.T) {
	file := newRuleFile("test", []resolvedDomain{
		{domain: domain{Type: domainTypeFull, Value: "www.example.com"}},
		{domain: domain{Type: domainTypeRoot, Value: "cdn.example.com"}},
		{domain: domain{Type: domainTypeRoot, Value: "example.com"}},
		{domain: domain{Type: domainTypeFull, Value: "example.com"}},
		{domain: domain{Type: domainTypeFull, Value: "example.org"}},
		{domain: domain{Type: domainTypeRoot, Value: "notexample.com"}},
		{domain: domain{Type: domainTypePlain, Value: "example"}},
	})

	want := []string{
		"DOMAIN-SUFFIX,example.com",
		"DOMAIN,example.org",
		"DOMAIN-SUFFIX,notexample.com",
		"DOMAIN-KEYWORD,example",
	}
	if !reflect.DeepEqual(file.Rules, want) {
		t.Fatalf("rules mismatch\n got: %#v\nwant: %#v", file.Rules, want)
	}
}

func TestNewRuleFileRecordsSkippedRegex(t *testing.T) {
	file := newRuleFile("private", []resolvedDomain{
		{domain: domain{Type: domainTypeRegex, Value: `^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`}, from: "private"},
		{domain: domain{Type: domainTypeRoot, Value: "localhost"}, from: "private"},
	})

	if want := []string{"DOMAIN-SUFFIX,localhost"}; !reflect.DeepEqual(file.Rules, want) {
		t.Fatalf("rules mismatch\n got: %#v\nwant: %#v", file.Rules, want)
	}
	if len(file.Skipped) != 1 || file.Skipped[0].From != "private" || !strings.Contains(file.Skipped[0].Reason, "single-label") {
		t.Fatalf("unexpected skipped entries: %#v", file.Skipped)
	}
}
