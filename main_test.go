package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRunExportsSelectedCodesCoveringRuleAndAttributeTypes(t *testing.T) {
	tmp := t.TempDir()
	geositePath := filepath.Join(tmp, "geosite.dat")
	outDir := filepath.Join(tmp, "rules")

	data := mainTestGeoSiteList(
		geoSite{
			Code: "google",
			Domains: []domain{
				{Type: domainTypeRoot, Value: "google.com"},
				{Type: domainTypeFull, Value: "www.google.com"},
				{Type: domainTypePlain, Value: "googleapis"},
				{Type: domainTypeRegex, Value: `^github\.com$`},
				{Type: domainTypeRegex, Value: `(^|\.)netflix\.com$`},
				{Type: domainTypeFull, Value: "play.google.com", Attributes: []string{"!cn"}},
			},
		},
		geoSite{
			Code: "steam",
			Domains: []domain{
				{Type: domainTypeRoot, Value: "steampowered.com"},
				{Type: domainTypeFull, Value: "steamchina.com", Attributes: []string{"cn"}},
				{Type: domainTypeRoot, Value: "steamads.com", Attributes: []string{"cn", "ads"}},
			},
		},
		geoSite{
			Code: "epicgames",
			Domains: []domain{
				{Type: domainTypeRegex, Value: `^cdn\d-epicgames-\d+\.file\.myqcloud\.com$`},
			},
		},
		geoSite{
			Code: "private",
			Domains: []domain{
				{Type: domainTypeRegex, Value: `^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`},
			},
		},
		geoSite{
			Code: "ccb",
			Domains: []domain{
				{Type: domainTypeRoot, Value: "ccb.com", Attributes: []string{"!cn"}},
			},
		},
		geoSite{
			Code: "category-ads",
			Domains: []domain{
				{Type: domainTypePlain, Value: "include:steam @cn @-ads"},
			},
		},
	)
	if err := os.WriteFile(geositePath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-geosite", geositePath,
		"-out", outDir,
		"-codes", "google,steam,epicgames,private,ccb,category-ads",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run failed: %v\nstderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Generated 11 Surge rule files") {
		t.Fatalf("unexpected stdout: %s", stdout.String())
	}

	assertRules(t, outDir, "google.list", []string{
		"DOMAIN-SUFFIX,google.com",
		"DOMAIN,www.google.com",
		"DOMAIN-KEYWORD,googleapis",
		"DOMAIN,github.com",
		"DOMAIN-SUFFIX,netflix.com",
		"DOMAIN,play.google.com",
	})
	assertRules(t, outDir, "google@!cn.list", []string{
		"DOMAIN,play.google.com",
	})
	assertRules(t, outDir, "steam.list", []string{
		"DOMAIN-SUFFIX,steampowered.com",
		"DOMAIN,steamchina.com",
		"DOMAIN-SUFFIX,steamads.com",
	})
	assertRules(t, outDir, "steam@ads.list", []string{
		"DOMAIN-SUFFIX,steamads.com",
	})
	assertRules(t, outDir, "steam@cn.list", []string{
		"DOMAIN,steamchina.com",
		"DOMAIN-SUFFIX,steamads.com",
	})
	assertRules(t, outDir, "epicgames.list", []string{
		"DOMAIN-WILDCARD,cdn*-epicgames-*.file.myqcloud.com",
	})
	assertRules(t, outDir, "private.list", []string{
		`URL-REGEX,^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`,
	})
	assertRules(t, outDir, "ccb.list", []string{
		"DOMAIN-SUFFIX,ccb.com",
	})
	assertRules(t, outDir, "ccb@!cn.list", []string{
		"DOMAIN-SUFFIX,ccb.com",
	})
	assertRules(t, outDir, "category-ads.list", []string{
		"DOMAIN,steamchina.com",
	})
	assertRules(t, outDir, "category-ads@cn.list", []string{
		"DOMAIN,steamchina.com",
	})

	if _, err := os.Stat(filepath.Join(outDir, "category-ads@ads.list")); !os.IsNotExist(err) {
		t.Fatalf("category-ads@ads.list should not exist, stat err: %v", err)
	}
}

func assertRules(t *testing.T, outDir, name string, want []string) {
	t.Helper()

	path := filepath.Join(outDir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(data)), "\n")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s rules mismatch\n got: %#v\nwant: %#v", name, got, want)
	}
}

func mainTestGeoSiteList(sites ...geoSite) []byte {
	var fields [][]byte
	for _, site := range sites {
		fields = append(fields, mainTestFieldBytes(1, mainTestGeoSite(site)))
	}
	return mainTestMessage(fields...)
}

func mainTestGeoSite(site geoSite) []byte {
	fields := [][]byte{mainTestFieldString(1, site.Code)}
	for _, d := range site.Domains {
		fields = append(fields, mainTestFieldBytes(2, mainTestDomain(d)))
	}
	return mainTestMessage(fields...)
}

func mainTestDomain(d domain) []byte {
	fields := [][]byte{
		mainTestFieldVarint(1, uint64(d.Type)),
		mainTestFieldString(2, d.Value),
	}
	for _, attr := range d.Attributes {
		fields = append(fields, mainTestFieldBytes(3, mainTestAttribute(attr)))
	}
	return mainTestMessage(fields...)
}

func mainTestAttribute(key string) []byte {
	return mainTestMessage(mainTestFieldString(1, key))
}

func mainTestMessage(fields ...[]byte) []byte {
	var out []byte
	for _, field := range fields {
		out = append(out, field...)
	}
	return out
}

func mainTestFieldVarint(number int, value uint64) []byte {
	return append(mainTestVarint(uint64(number<<3)|uint64(wireTypeVarint)), mainTestVarint(value)...)
}

func mainTestFieldString(number int, value string) []byte {
	return mainTestFieldBytes(number, []byte(value))
}

func mainTestFieldBytes(number int, value []byte) []byte {
	out := mainTestVarint(uint64(number<<3) | uint64(wireTypeBytes))
	out = append(out, mainTestVarint(uint64(len(value)))...)
	out = append(out, value...)
	return out
}

func mainTestVarint(value uint64) []byte {
	var out []byte
	for value >= 0x80 {
		out = append(out, byte(value)|0x80)
		value >>= 7
	}
	out = append(out, byte(value))
	return out
}
