package main

import (
	"fmt"
	"io"
	"net"
)

func socks5Handshake(conn net.Conn) error {
	fixedheader := make([]byte, 2)
	if _, err := io.ReadFull(conn, fixedheader); err != nil {
		return err
	}
	ver := fixedheader[0]
	nmethods := fixedheader[1]
	if ver != SocksVersion5 {
		return fmt.Errorf("unsupported SOCKS version: %d", ver)
	}
	methods := make([]byte, nmethods)
	if _, err := io.ReadFull(conn, methods); err != nil {
		return err
	}
	var hasUserPass = false
	var hasNoAuth = false
	for _, m := range methods {
		if m == MethodUserPass {
			hasUserPass = true
		}
		if m == MethodNoAuth {
			hasNoAuth = true
		}
	}
	var selected byte = MethodNoAcceptable
	if hasUserPass {
		selected = MethodUserPass
	} else if hasNoAuth {
		selected = MethodNoAuth
	}
	response := []byte{SocksVersion5, selected}
	if _, err := conn.Write(response); err != nil {
		return err
	}
	if selected == MethodNoAcceptable {
		return fmt.Errorf("no acceptable authentication methods")
	}
	if selected == MethodUserPass {
		if err := handleUserPassAuth(conn); err != nil {
			return fmt.Errorf("userpass auth failed: %v", err)
		}
	}
	return nil
}

func handleUserPassAuth(conn net.Conn) error {
	verBuf := make([]byte, 1)
	if _, err := io.ReadFull(conn, verBuf); err != nil {
		return err
	}
	if verBuf[0] != 0x01 {
		return fmt.Errorf("unsupported userpass version: %d", verBuf[0])
	}
	ulenBuf := make([]byte, 1)
	if _, err := io.ReadFull(conn, ulenBuf); err != nil {
		return err
	}
	if ulenBuf[0] == 0 {
		return fmt.Errorf("empty username")
	}
	uname := make([]byte, ulenBuf[0])
	if _, err := io.ReadFull(conn, uname); err != nil {
		return err
	}
	plen := make([]byte, 1)
	if _, err := io.ReadFull(conn, plen); err != nil {
		return err
	}
	if plen[0] == 0 {
		return fmt.Errorf("empty password")
	}
	passwd := make([]byte, plen[0])
	if _, err := io.ReadFull(conn, passwd); err != nil {
		return err
	}
	username := string(uname)
	password := string(passwd)
	if !validateUserPass(username, password) {
		return fmt.Errorf("invalid username or password")
	}
	return fmt.Errorf("userpass authentication not implemented")
}

func validateUserPass(username, password string) bool {
	return username == "Marine" && password == "1235689412"
}

// +----+-----+-------+------+----------+----------+
// |VER | CMD |  RSV  | ATYP | DST.ADDR | DST.PORT |
// +----+-----+-------+------+----------+----------+
// | 1  |  1  | X'00' |  1   | Variable |    2     |
// +----+-----+-------+------+----------+----------+
func socks5ParseRequest(conn net.Conn) (byte, string, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return 0, "", err
	}
	var ver, cmd, rsv, atyp = header[0], header[1], header[2], header[3]
	if ver != SocksVersion5 {
		return 0, "", fmt.Errorf("unsupported SOCKS version: %d", ver)
	}
	if rsv != 0x00 {
		return 0, "", fmt.Errorf("invalid reserved field: %d", rsv)
	}
	var addr string
	switch atyp {
	case ATYPIPv4:
		ip := make([]byte, 4)
		if _, err := io.ReadFull(conn, ip); err != nil {
			return 0, "", err
		}
		addr = net.IP(ip).String()
	case ATYPDomainName:
		length := make([]byte, 1)
		if _, err := io.ReadFull(conn, length); err != nil {
			return 0, "", err
		}
		domain := make([]byte, length[0])
		if _, err := io.ReadFull(conn, domain); err != nil {
			return 0, "", err
		}
		addr = string(domain)
	case ATYPIPv6:
		ip := make([]byte, 16)
		if _, err := io.ReadFull(conn, ip); err != nil {
			return 0, "", err
		}
		addr = net.IP(ip).String()
	default:
		return 0, "", fmt.Errorf("unsupported address type: %d", atyp)
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBytes); err != nil {
		return 0, "", err
	}
	port := (int(portBytes[0])<<8 | int(portBytes[1]))
	return cmd, fmt.Sprintf("%s:%d", addr, port), nil
}

func sendSocks5Reply(conn net.Conn, rep byte, bindAddr *net.UDPAddr) error {
	reply := []byte{SocksVersion5, rep, 0x00}
	if bindAddr != nil {
		if ip4 := bindAddr.IP.To4(); ip4 != nil {
			reply = append(reply, ATYPIPv4)
			reply = append(reply, ip4...)
		} else {
			reply = append(reply, ATYPIPv6)
			reply = append(reply, bindAddr.IP...)
		}
		portBytes := []byte{byte(bindAddr.Port >> 8), byte(bindAddr.Port & 0xff)}
		reply = append(reply, portBytes...)
	} else {
		reply = append(reply, ATYPIPv4)
		reply = append(reply, []byte{0, 0, 0, 0}...)
		reply = append(reply, []byte{0, 0}...)
	}
	_, err := conn.Write(reply)
	return err
}
