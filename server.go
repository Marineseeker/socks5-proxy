package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/socks5-proxy/metrics"
	"go.uber.org/zap"
)

func runServer(listenAddr string) {
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		zap.S().Fatalf("failed to listen on %s: %s", listenAddr, err)
	}
	zap.S().Infof("Socks5 server listening on %s", listenAddr)

	for {
		conn_from_client, err := ln.Accept()
		if err != nil {
			zap.S().Infof("failed to accept: %s", err)
			continue
		}
		go handleConn(conn_from_client)
	}
}

func handleConn(conn_from_client net.Conn) {
	defer conn_from_client.Close()
	if err := socks5Handshake(conn_from_client); err != nil {
		zap.S().Infof("handshake failed: %v", err)
		return
	}
	// 从 socks5ParseRequest 中拿到 cmd 与 targetAddr,
	cmd, targetAddr, err := socks5ParseRequest(conn_from_client)
	if err != nil {
		zap.S().Infof("request parse failed: %v", err)
		sendSocks5Reply(conn_from_client, RepCommandNotSupported, nil)
		return
	}
	// 根据 cmd 的值选择进行 TCP 握手还是 UDP 传包
	switch cmd {
	case CmdConnect:
		// 在 relay 中进行双向 TCP 转发
		if err := relay(conn_from_client, targetAddr); err != nil {
			zap.S().Infof("[socks] relay failed: %v", err)
			sendSocks5Reply(conn_from_client, RepHostUnreachable, nil)
			return
		}
	case CmdUDPAssociate:
		handleUDPAssociate(conn_from_client)
	default:
		zap.S().Infof("unsupported command: %v", err)
		sendSocks5Reply(conn_from_client, RepCommandNotSupported, nil)
	}
}

func handleUDPAssociate(conn_from_client net.Conn) {
	udpConn, err := net.ListenUDP("udp", nil)
	if err != nil {
		zap.S().Infof("[udp] failed to create udp linstening: %v", err)
		sendSocks5Reply(conn_from_client, RepServerFailure, nil)
		return
	}
	defer udpConn.Close()
	localAddr := udpConn.LocalAddr().(*net.UDPAddr)
	zap.S().Infof("[udp] opened udp relay on %s", localAddr)
	if err := sendSocks5Reply(conn_from_client, RepSuccess, localAddr); err != nil {
		zap.S().Infof("[udp] failed to send reply: %v", err)
		return
	}
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 1)
		conn_from_client.Read(buf)
		close(done)
	}()

	buf := make([]byte, 65535)
	udpConn.SetReadDeadline(time.Now().Add(5 * time.Minute))
	for {
		select {
		case <-done:
			zap.S().Infof("[udp] TCP connection closed, stopping udp relay")
			return
		default:
		}
		n, clientAddr, err := udpConn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				udpConn.SetReadDeadline(time.Now().Add(5 * time.Minute))
				continue
			}
			zap.S().Infof("[udp] read error: %v", err)
			return
		}
		udpConn.SetReadDeadline(time.Now().Add(5 * time.Minute))
		if n < 10 {
			zap.S().Infof("[udp] packet too short: %d bytes", n)
			continue
		}
		go handleUDPPacket(udpConn, clientAddr, buf[:n])
	}
}

func handleUDPPacket(udpConn *net.UDPConn, clientAddr *net.UDPAddr, packet []byte) {
	if len(packet) < 10 {
		return
	}
	frag := packet[2]
	if frag != 0x00 {
		zap.S().Infof("[udp] fragmentation not supported")
		return
	}
	atyp := packet[3]
	var dstAddr string
	var dataOffset int
	switch atyp {
	case ATYPIPv4:
		if len(packet) < 10 {
			return
		}
		ip := net.IP(packet[4:8])
		port := binary.BigEndian.Uint16(packet[8:10])
		dstAddr = fmt.Sprintf("%s:%d", ip.String(), port)
		dataOffset = 10
	case ATYPDomainName:
		if len(packet) < 5 {
			return
		}
		domainLen := int(packet[4])
		if len(packet) < 5+domainLen+2 {
			return
		}
		domain := string(packet[5 : 5+domainLen])
		port := binary.BigEndian.Uint16(packet[5+domainLen : 5+domainLen+2])
		dstAddr = fmt.Sprintf("%s:%d", domain, port)
		dataOffset = 5 + domainLen + 2
	case ATYPIPv6:
		if len(packet) < 22 {
			return
		}
		ip := net.IP(packet[4:20])
		port := binary.BigEndian.Uint16(packet[20:22])
		dstAddr = fmt.Sprintf("%s:%d", ip.String(), port)
		dataOffset = 22
	default:
		zap.S().Infof("[udp] unsupported address type: %d", atyp)
		return
	}
	data := packet[dataOffset:]
	zap.S().Infof("[udp] relaying %d bytes from %s to %s", len(data), clientAddr, dstAddr)

	if blocked != nil && blocked.isBlocked(dstAddr) {
		zap.S().Infof("[udp] target %s is blocked", dstAddr)
		return
	}
	targetConn, err := net.DialTimeout("udp", dstAddr, 6*time.Second)
	if err != nil {
		zap.S().Infof("[udp] failed to dial %s: %v", dstAddr, err)
		if blocked != nil {
			blocked.markBlocked(dstAddr)
			zap.S().Infof("[socks] marking %s as blocked for %s", dstAddr, blockedTTL)
		}
		return
	}
	defer targetConn.Close()
	if blocked != nil {
		blocked.unmarkBlocked(dstAddr)
	}
	if _, err := targetConn.Write(data); err != nil {
		zap.S().Infof("[udp] failed to write to target: %v", err)
		return
	}
	targetConn.SetReadDeadline(time.Now().Add(10 * time.Second))
	respBuf := make([]byte, 65535)
	n, err := targetConn.Read(respBuf)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			zap.S().Infof("[udp] target response timeout for %s", dstAddr)
		} else {
			zap.S().Infof("[udp] failed to read from target: %v", err)
		}
		return
	}
	response := buildUDPResponse(dstAddr, respBuf[:n])
	if _, err := udpConn.WriteToUDP(response, clientAddr); err != nil {
		zap.S().Infof("[udp] failed to write back to client: %v", err)
	} else {
		zap.S().Infof("[udp] repyed %d bytes back to client", n)
	}
}

func buildUDPResponse(addr string, data []byte) []byte {
	host, postStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil
	}
	port := 0
	fmt.Sscanf(postStr, "%d", &port)
	var header []byte
	header = append(header, 0, 0, 0)
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			header = append(header, ATYPIPv4)
			header = append(header, ip4...)
		} else {
			header = append(header, ATYPIPv6)
			header = append(header, ip...)
		}
	} else {
		header = append(header, ATYPDomainName)
		header = append(header, byte(len(host)))
		header = append(header, []byte(host)...)
	}
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(port))
	header = append(header, portBytes...)
	header = append(header, data...)
	return header
}

func relay(conn_from_client net.Conn, targetAddr string) error {
	// --- 检查目标地址是否在阻止列表中 ---
	if blocked != nil && blocked.isBlocked(targetAddr) {
		return fmt.Errorf("refuse to dial blocked cache")
	}
	// 尝试进行 TCP 握手
	conn_from_server, err := net.DialTimeout("tcp", targetAddr, 6*time.Second)
	// dial 失败, 封禁 blockedTTL 时间
	if err != nil {
		if blocked != nil {
			blocked.markBlocked(targetAddr)
			zap.S().Infof("[socks] marking %s as blocked for %s", targetAddr, blockedTTL)
		}
		return err
	}
	// dial成功, 解除封禁
	if blocked != nil {
		blocked.unmarkBlocked(targetAddr)
	}

	// 设置较大的读写缓冲区和禁用 Nagle 算法
	if cc, ok := conn_from_client.(*net.TCPConn); ok {
		cc.SetReadBuffer(64 * 1024)
		cc.SetWriteBuffer(64 * 1024)
		cc.SetNoDelay(true)
	}
	if cs, ok := conn_from_server.(*net.TCPConn); ok {
		cs.SetReadBuffer(64 * 1024)
		cs.SetWriteBuffer(64 * 1024)
		cs.SetNoDelay(true)
	}
	// 调用工具方法 sendSocks5Reply() 发送成功响应给客户端
	if err := sendSocks5Reply(conn_from_client, RepSuccess, nil); err != nil {
		zap.S().Infof("[socks] failed to send reply : %v", err)
		return err
	}

	var wg sync.WaitGroup
	wg.Add(2)

	transfer := func(dst io.Writer, src io.Reader, isUpload bool) {
		defer wg.Done()
		bufPtr := pool.Get().(*[]byte)
		buf := *bufPtr
		defer pool.Put(bufPtr)

		var localCounter uint64 // 每个协程私有的计数器

		for {
			n, err := src.Read(buf)
			if n > 0 {
				if _, werr := dst.Write(buf[:n]); werr != nil {
					zap.S().Infof("[relay] write err: %v", werr)
					break
				}
				localCounter += uint64(n)
				// 增加全局计数，由全局聚合器负责周期性上报到 user
				// todo 阈值可配置化, 这个阈值可以调整，过小可能增加上下文切换开销，过大可能增加内存占用和上报延迟
				if localCounter >= 16*1024 {
					metrics.AddToTotals(isUpload, localCounter)
					localCounter = 0
				}
			}
			if err != nil {
				if !isIgnorableError(err) && err != io.EOF {
					zap.S().Infof("[relay] read err: %v", err)
				}
				if localCounter > 0 {
					metrics.AddToTotals(isUpload, localCounter)
				}
				break
			}
		}

		if tcpDst, ok := dst.(*net.TCPConn); ok {
			tcpDst.CloseWrite()
		}
	}

	// client -> server 为 upload，server -> client 为 download
	go transfer(conn_from_server, conn_from_client, true)
	go transfer(conn_from_client, conn_from_server, false)

	wg.Wait()

	conn_from_client.Close()
	conn_from_server.Close()
	return nil
}
