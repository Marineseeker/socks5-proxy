package socks_tun

import (
	"encoding/binary"
	"fmt"
	"strings"
)

type TCPHeader struct {
	SrcPort uint16
	DstPort uint16

	Seq uint32
	Ack uint32

	DataOffset uint8

	Flags uint8

	Window uint16

	Checksum uint16

	Urgent uint16
}

func parseTCPHeader(data []byte) (*TCPHeader, error) {
	if len(data) < 20 {
		return nil, fmt.Errorf(
			"invalid tcp packet: length=%d",
			len(data),
		)
	}

	header := &TCPHeader{
		SrcPort: binary.BigEndian.Uint16(data[0:2]),
		DstPort: binary.BigEndian.Uint16(data[2:4]),

		Seq: binary.BigEndian.Uint32(data[4:8]),
		Ack: binary.BigEndian.Uint32(data[8:12]),

		DataOffset: data[12] >> 4,

		Flags: data[13],

		Window: binary.BigEndian.Uint16(data[14:16]),

		Checksum: binary.BigEndian.Uint16(data[16:18]),

		Urgent: binary.BigEndian.Uint16(data[18:20]),
	}

	headerLen := int(header.DataOffset) * 4

	if headerLen < 20 {
		return nil, fmt.Errorf(
			"invalid tcp header length: %d",
			headerLen,
		)
	}

	if len(data) < headerLen {
		return nil, fmt.Errorf(
			"tcp header exceeds packet length",
		)
	}

	return header, nil
}

func (h *TCPHeader) FlagString() string {
	flags := make([]string, 0)

	if h.Flags&0x01 != 0 {
		flags = append(flags, "FIN")
	}

	if h.Flags&0x02 != 0 {
		flags = append(flags, "SYN")
	}

	if h.Flags&0x04 != 0 {
		flags = append(flags, "RST")
	}

	if h.Flags&0x08 != 0 {
		flags = append(flags, "PSH")
	}

	if h.Flags&0x10 != 0 {
		flags = append(flags, "ACK")
	}

	if h.Flags&0x20 != 0 {
		flags = append(flags, "URG")
	}

	return strings.Join(flags, "|")
}
