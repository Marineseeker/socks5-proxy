package socks_tun

import (
	"encoding/binary"
	"fmt"
	"net"
)

type IPv4Header struct {
	Version        uint8
	IHL            uint8 // Internet Header Length
	TOS            uint8
	TotalLength    uint16
	Identification uint16
	Flags          uint8
	FragmentOffset uint16
	TTL            uint8
	Protocol       uint8
	Checksum       uint16
	SrcIP          net.IP
	DstIP          net.IP
}

func parseIPv4Header(packet []byte) (*IPv4Header, error) {
	if len(packet) < 20 {
		return nil, fmt.Errorf("invalid ipv4 packet")
	}
	version := packet[0] >> 4

	ihl := packet[0] & 0x0F
	headerLen := int(ihl) * 4
	if len(packet) < headerLen {
		return nil, fmt.Errorf("invalid ipv4 packet: header length exceeds packet length")
	}
	flagsAndOffset := binary.BigEndian.Uint16(packet[6:8])

	header := &IPv4Header{
		Version:        version,
		IHL:            ihl,
		TOS:            packet[1],
		TotalLength:    binary.BigEndian.Uint16(packet[2:4]),
		Identification: binary.BigEndian.Uint16(packet[4:6]),

		Flags:          uint8(flagsAndOffset >> 13),
		FragmentOffset: flagsAndOffset & 0x1FFF,

		TTL:      packet[8],
		Protocol: packet[9],
		Checksum: binary.BigEndian.Uint16(packet[10:12]),

		SrcIP: net.IP(packet[12:16]),
		DstIP: net.IP(packet[16:20]),
	}

	return header, nil
}
