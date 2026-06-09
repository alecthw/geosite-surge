package main

import (
	"encoding/binary"
	"errors"
	"fmt"
)

type protoWireType uint64

const (
	wireTypeVarint     protoWireType = 0
	wireTypeFixed64    protoWireType = 1
	wireTypeBytes      protoWireType = 2
	wireTypeStartGroup protoWireType = 3
	wireTypeEndGroup   protoWireType = 4
	wireTypeFixed32    protoWireType = 5
)

func consumeTag(data []byte) (int, protoWireType, []byte, error) {
	tag, rest, err := consumeVarint(data)
	if err != nil {
		return 0, 0, nil, err
	}
	field := int(tag >> 3)
	wireType := protoWireType(tag & 0x7)
	if field == 0 {
		return 0, 0, nil, errors.New("invalid protobuf field 0")
	}
	return field, wireType, rest, nil
}

func consumeVarint(data []byte) (uint64, []byte, error) {
	value, n := binary.Uvarint(data)
	if n == 0 {
		return 0, nil, errors.New("truncated protobuf varint")
	}
	if n < 0 {
		return 0, nil, errors.New("overflowing protobuf varint")
	}
	return value, data[n:], nil
}

func consumeBytes(data []byte) ([]byte, []byte, error) {
	length, rest, err := consumeVarint(data)
	if err != nil {
		return nil, nil, err
	}
	if length > uint64(len(rest)) {
		return nil, nil, fmt.Errorf("truncated protobuf bytes: want %d, have %d", length, len(rest))
	}
	return rest[:length], rest[length:], nil
}

func consumeString(data []byte) (string, []byte, error) {
	value, rest, err := consumeBytes(data)
	if err != nil {
		return "", nil, err
	}
	return string(value), rest, nil
}

func skipValue(wireType protoWireType, data []byte) ([]byte, error) {
	switch wireType {
	case wireTypeVarint:
		_, rest, err := consumeVarint(data)
		return rest, err
	case wireTypeFixed64:
		if len(data) < 8 {
			return nil, errors.New("truncated protobuf fixed64")
		}
		return data[8:], nil
	case wireTypeBytes:
		_, rest, err := consumeBytes(data)
		return rest, err
	case wireTypeFixed32:
		if len(data) < 4 {
			return nil, errors.New("truncated protobuf fixed32")
		}
		return data[4:], nil
	case wireTypeStartGroup, wireTypeEndGroup:
		return nil, fmt.Errorf("unsupported protobuf group wire type %d", wireType)
	default:
		return nil, fmt.Errorf("unknown protobuf wire type %d", wireType)
	}
}
