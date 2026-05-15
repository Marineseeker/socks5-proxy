package socks

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/socks5-proxy/metrics"
	"github.com/socks5-proxy/utils"
	"go.uber.org/zap"
)

// RunServer 启动 socks5 TCP 服务器并接受连接
func RunServer(listenAddr string) {
	s := NewServer(listenAddr, BlockedCache)
	s.Start()
}

type Server struct {
	ListenAddr string
	Blocked    *blockedCache
}

func NewServer(listenAddr string, blocked *blockedCache) *Server {
	return &Server{ListenAddr: listenAddr, Blocked: blocked}
}

func (s *Server) Start() {
	ln, err := net.Listen("tcp", s.ListenAddr)
	if err != nil {
		zap.S().Fatalf("failed to listen on %s: %s", s.ListenAddr, err)
	}
	zap.S().Infof("Socks5 server listening on %s", s.ListenAddr)

	for {
		conn_from_client, err := ln.Accept()
		if err != nil {
			zap.S().Errorf("failed to accept: %s", err)
			continue
		}
		go s.handleConn(conn_from_client)
	}
}

func (s *Server) handleConn(conn_from_client net.Conn) {
	defer conn_from_client.Close()
	if err := Socks5Handshake(conn_from_client); err != nil {
		zap.S().Errorf("handshake failed: %v", err)
		return
	}
	// 从 socks5ParseRequest 中拿到 cmd 与 targetAddr,
	cmd, targetAddr, err := Socks5ParseRequest(conn_from_client)
	if err != nil {
		zap.S().Errorf("request parse failed: %v", err)
		SendSocks5Reply(conn_from_client, RepCommandNotSupported, nil)
		return
	}
	// 根据 cmd 的值选择进行 TCP 握手还是 UDP 传包
	switch cmd {
	case CmdConnect:
		// 在 relay 中进行双向 TCP 转发
		if err := s.relay(conn_from_client, targetAddr); err != nil {
			SendSocks5Reply(conn_from_client, RepHostUnreachable, nil)
			return
		}
	case CmdUDPAssociate:
		s.handleUDPAssociate(conn_from_client)
	default:
		zap.S().Infof("unsupported command: %v", cmd)
		SendSocks5Reply(conn_from_client, RepCommandNotSupported, nil)
	}
}

func (s *Server) handleUDPAssociate(conn_from_client net.Conn) {
	udpConn, err := net.ListenUDP("udp", nil)
	if err != nil {
		zap.S().Errorf("[udp] failed to create udp linstening: %v", err)
		SendSocks5Reply(conn_from_client, RepServerFailure, nil)
		return
	}
	defer udpConn.Close()
	localAddr := udpConn.LocalAddr().(*net.UDPAddr)
	zap.S().Infof("[udp] opened udp relay on %s", localAddr)
	if err := SendSocks5Reply(conn_from_client, RepSuccess, localAddr); err != nil {
		zap.S().Errorf("[udp] failed to send reply: %v", err)
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
			zap.S().Errorf("[udp] read error: %v", err)
			return
		}
		udpConn.SetReadDeadline(time.Now().Add(5 * time.Minute))
		if n < 10 {
			zap.S().Infof("[udp] packet too short: %d bytes", n)
			continue
		}
		go s.handleUDPPacket(udpConn, clientAddr, buf[:n])
	}
}

func (s *Server) handleUDPPacket(udpConn *net.UDPConn, clientAddr *net.UDPAddr, packet []byte) {
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

	if s.Blocked != nil && s.Blocked.isBlocked(dstAddr) {
		zap.S().Infof("[udp] target %s is blocked", dstAddr)
		return
	}
	targetConn, err := net.DialTimeout("udp", dstAddr, 6*time.Second)
	if err != nil {
		zap.S().Errorf("[udp] failed to dial %s: %v", dstAddr, err)
		if s.Blocked != nil {
			s.Blocked.markBlocked(dstAddr)
			zap.S().Infof("[socks] marking %s as blocked for %s", dstAddr, s.Blocked.ttl)
		}
		return
	}
	defer targetConn.Close()
	if s.Blocked != nil {
		s.Blocked.unmarkBlocked(dstAddr)
	}
	if _, err := targetConn.Write(data); err != nil {
		zap.S().Errorf("[udp] failed to write to target: %v", err)
		return
	}
	targetConn.SetReadDeadline(time.Now().Add(10 * time.Second))
	respBuf := make([]byte, 65535)
	n, err := targetConn.Read(respBuf)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			zap.S().Infof("[udp] target response timeout for %s", dstAddr)
		} else {
			zap.S().Errorf("[udp] failed to read from target: %v", err)
		}
		return
	}
	response := s.buildUDPResponse(dstAddr, respBuf[:n])
	if _, err := udpConn.WriteToUDP(response, clientAddr); err != nil {
		zap.S().Errorf("[udp] failed to write back to client: %v", err)
	} else {
		zap.S().Infof("[udp] repyed %d bytes back to client", n)
	}
}

func (s *Server) buildUDPResponse(addr string, data []byte) []byte {
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

func (s *Server) relay(conn_from_client net.Conn, targetAddr string) error {
	// --- 检查目标地址是否在阻止列表中 ---
	if s.Blocked != nil && s.Blocked.isBlocked(targetAddr) {
		return fmt.Errorf("refuse to dial blocked cache")
	}
	// 尝试进行 TCP 握手
	conn_from_server, err := net.DialTimeout("tcp", targetAddr, 6*time.Second)
	// dial 失败, 封禁 BlockedCache.ttl 时间
	if err != nil {
		if s.Blocked != nil {
			s.Blocked.markBlocked(targetAddr)
			zap.S().Infof("[socks] marking %s as blocked for %s", targetAddr, s.Blocked.ttl)
		}
		return err
	}
	// dial成功, 解除封禁
	if s.Blocked != nil {
		s.Blocked.unmarkBlocked(targetAddr)
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
	// 调用工具方法 SendSocks5Reply() 发送成功响应给客户端
	if err := SendSocks5Reply(conn_from_client, RepSuccess, nil); err != nil {
		zap.S().Errorf("[socks] failed to send reply : %v", err)
		return err
	}

	var wg sync.WaitGroup
	wg.Add(2)

	transfer := func(dst io.Writer, src io.Reader, isUpload bool) {
		defer wg.Done()
		bufPtr := utils.Pool.Get().(*[]byte)
		buf := *bufPtr
		defer utils.Pool.Put(bufPtr)
		clientIP := conn_from_client.RemoteAddr().String()
		var localCounter uint64 // 每个协程私有的计数器

		for {
			n, err := src.Read(buf)
			if n > 0 {
				if _, werr := dst.Write(buf[:n]); werr != nil {
					zap.S().Errorf("[relay] write err: %v", werr)
					break
				}
				localCounter += uint64(n)
				if localCounter >= 16*1024 {
					metrics.BroadcastRawTraffic(&metrics.RawTrafficEvent{
						Timestamp: time.Now().Unix(),
						ClientIP:  clientIP,
						IsUpload:  isUpload,
						ByteCount: localCounter,
					})
					localCounter = 0
				}
				metrics.AddToTotals(isUpload, uint64(n))
			}
			if err != nil {
				if !utils.IsIgnorableError(err) && err != io.EOF {
					zap.S().Errorf("[relay] read err: %v", err)
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
