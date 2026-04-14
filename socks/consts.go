package socks

// SOCKS5 Version
const (
	SocksVersion5 = 0x05
)

// Auth methods
const (
	MethodNoAuth       = 0x00
	MethodGSSAPI       = 0x01
	MethodUserPass     = 0x02
	MethodNoAcceptable = 0xFF
)

// Command fields
const (
	CmdConnect      = 0x01
	CmdBind         = 0x02
	CmdUDPAssociate = 0x03
)

// Address types
const (
	ATYPIPv4       = 0x01
	ATYPDomainName = 0x03
	ATYPIPv6       = 0x04
)

// Reply codes
const (
	RepSuccess               = 0x00
	RepServerFailure         = 0x01
	RepNotAllowed            = 0x02
	RepNetworkUnreachable    = 0x03
	RepHostUnreachable       = 0x04
	RepConnectionRefused     = 0x05
	RepTTLExpired            = 0x06
	RepCommandNotSupported   = 0x07
	RepAddressTypeNotSupport = 0x08
)
