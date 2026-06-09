package main

import "testing"

func TestParseGeoSiteListReadsDomainAttributes(t *testing.T) {
	data := message(
		fieldBytes(1, message(
			fieldString(1, "steam"),
			fieldBytes(2, message(
				fieldVarint(1, uint64(domainTypeFull)),
				fieldString(2, "steamchina.com"),
				fieldBytes(3, message(
					fieldString(1, "cn"),
					fieldVarint(2, 1),
				)),
			)),
		)),
	)

	list, err := parseGeoSiteList(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(list.Entries))
	}
	domains := list.Entries[0].Domains
	if len(domains) != 1 {
		t.Fatalf("got %d domains, want 1", len(domains))
	}
	if got := domains[0].Attributes; len(got) != 1 || got[0] != "cn" {
		t.Fatalf("attributes = %#v, want [cn]", got)
	}
}

func message(fields ...[]byte) []byte {
	var out []byte
	for _, field := range fields {
		out = append(out, field...)
	}
	return out
}

func fieldVarint(number int, value uint64) []byte {
	return append(varint(uint64(number<<3)|uint64(wireTypeVarint)), varint(value)...)
}

func fieldString(number int, value string) []byte {
	return fieldBytes(number, []byte(value))
}

func fieldBytes(number int, value []byte) []byte {
	out := varint(uint64(number<<3) | uint64(wireTypeBytes))
	out = append(out, varint(uint64(len(value)))...)
	out = append(out, value...)
	return out
}

func varint(value uint64) []byte {
	var out []byte
	for value >= 0x80 {
		out = append(out, byte(value)|0x80)
		value >>= 7
	}
	out = append(out, byte(value))
	return out
}
