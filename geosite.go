package main

import (
	"errors"
	"fmt"
	"io"
)

type domainType int

const (
	domainTypePlain domainType = iota
	domainTypeRegex
	domainTypeRoot
	domainTypeFull
)

type domain struct {
	Type       domainType
	Value      string
	Attributes []string
}

type geoSite struct {
	Code    string
	Domains []domain
}

type geoSiteList struct {
	Entries []geoSite
}

func readGeoSiteList(r io.Reader) (*geoSiteList, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return parseGeoSiteList(data)
}

func parseGeoSiteList(data []byte) (*geoSiteList, error) {
	var list geoSiteList
	for len(data) > 0 {
		field, wireType, rest, err := consumeTag(data)
		if err != nil {
			return nil, err
		}
		data = rest

		switch field {
		case 1:
			if wireType != wireTypeBytes {
				return nil, fmt.Errorf("geosite list field %d: expected bytes, got wire type %d", field, wireType)
			}
			msg, next, err := consumeBytes(data)
			if err != nil {
				return nil, err
			}
			data = next
			site, err := parseGeoSite(msg)
			if err != nil {
				return nil, err
			}
			list.Entries = append(list.Entries, site)
		default:
			next, err := skipValue(wireType, data)
			if err != nil {
				return nil, err
			}
			data = next
		}
	}
	return &list, nil
}

func parseGeoSite(data []byte) (geoSite, error) {
	var site geoSite
	for len(data) > 0 {
		field, wireType, rest, err := consumeTag(data)
		if err != nil {
			return geoSite{}, err
		}
		data = rest

		switch field {
		case 1:
			if wireType != wireTypeBytes {
				return geoSite{}, fmt.Errorf("geosite code field: expected bytes, got wire type %d", wireType)
			}
			value, next, err := consumeString(data)
			if err != nil {
				return geoSite{}, err
			}
			site.Code = value
			data = next
		case 2:
			if wireType != wireTypeBytes {
				return geoSite{}, fmt.Errorf("geosite domain field: expected bytes, got wire type %d", wireType)
			}
			msg, next, err := consumeBytes(data)
			if err != nil {
				return geoSite{}, err
			}
			d, err := parseDomain(msg)
			if err != nil {
				return geoSite{}, err
			}
			site.Domains = append(site.Domains, d)
			data = next
		default:
			next, err := skipValue(wireType, data)
			if err != nil {
				return geoSite{}, err
			}
			data = next
		}
	}
	if site.Code == "" {
		return geoSite{}, errors.New("geosite entry without country code")
	}
	return site, nil
}

func parseDomain(data []byte) (domain, error) {
	var d domain
	for len(data) > 0 {
		field, wireType, rest, err := consumeTag(data)
		if err != nil {
			return domain{}, err
		}
		data = rest

		switch field {
		case 1:
			if wireType != wireTypeVarint {
				return domain{}, fmt.Errorf("domain type field: expected varint, got wire type %d", wireType)
			}
			value, next, err := consumeVarint(data)
			if err != nil {
				return domain{}, err
			}
			d.Type = domainType(value)
			data = next
		case 2:
			if wireType != wireTypeBytes {
				return domain{}, fmt.Errorf("domain value field: expected bytes, got wire type %d", wireType)
			}
			value, next, err := consumeString(data)
			if err != nil {
				return domain{}, err
			}
			d.Value = value
			data = next
		case 3:
			if wireType != wireTypeBytes {
				return domain{}, fmt.Errorf("domain attribute field: expected bytes, got wire type %d", wireType)
			}
			msg, next, err := consumeBytes(data)
			if err != nil {
				return domain{}, err
			}
			key, err := parseAttributeKey(msg)
			if err != nil {
				return domain{}, err
			}
			if key != "" {
				d.Attributes = append(d.Attributes, key)
			}
			data = next
		default:
			next, err := skipValue(wireType, data)
			if err != nil {
				return domain{}, err
			}
			data = next
		}
	}
	return d, nil
}

func parseAttributeKey(data []byte) (string, error) {
	for len(data) > 0 {
		field, wireType, rest, err := consumeTag(data)
		if err != nil {
			return "", err
		}
		data = rest

		if field == 1 {
			if wireType != wireTypeBytes {
				return "", fmt.Errorf("attribute key field: expected bytes, got wire type %d", wireType)
			}
			value, _, err := consumeString(data)
			return value, err
		}
		next, err := skipValue(wireType, data)
		if err != nil {
			return "", err
		}
		data = next
	}
	return "", nil
}
