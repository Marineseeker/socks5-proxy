package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// 使用buffer pool 减少每次io.copy的申请内存的开销

// sync.Pool 是 Go 为你提供的一个“对象缓存池”。它的作用是：
// 缓存那些“用一下又要丢”的对象, 下次再用时直接复用，而不是重新创建

// 去池子里拿一个 64KB 缓冲区 如果没有，New() 会生成一个
// 对于池中的对象, 应该返回指向这个对象的指针以减少内存分配开销
var pool = sync.Pool{
	New: func() any {
		// New 方法现在返回 *[]byte 指针
		b := make([]byte, 64*1024)
		return &b
	},
}

type Mode string

const (
	ModeClient Mode = "client"
	ModeServer Mode = "server"
)

var (
	blockedTTL  time.Duration
	blockedFile string
	blocked     *blockedCache
)

type blockEntry struct {
	failures     int
	blockedUntil time.Time
	permanent    bool
}

type blockedCache struct {
	mu       sync.RWMutex
	m        map[string]*blockEntry
	ttl      time.Duration
	filePath string
}

func newBlockedCache(ttl time.Duration, filePath string) *blockedCache {
	bc := &blockedCache{
		m:        make(map[string]*blockEntry),
		ttl:      ttl,
		filePath: filePath,
	}
	bc.loadPermanent()
	return bc
}

func (bc *blockedCache) loadPermanent() {
	f, err := os.Open(bc.filePath)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		addr := strings.TrimSpace(scanner.Text())
		if addr != "" {
			bc.m[addr] = &blockEntry{permanent: true}
		}
	}
	log.Printf("[socks] loaded %d permanently blocked addresses", len(bc.m))
}

func (bc *blockedCache) savePermanent(addr string) {
	f, err := os.OpenFile(bc.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("failed to save blocked addr: %v", err)
		return
	}
	defer f.Close()
	if _, err := f.WriteString(addr + "\n"); err != nil {
		log.Printf("failed to write blocked addr into file: %v", err)
	}
}

func (bc *blockedCache) isBlocked(addr string) bool {
	bc.mu.RLock()
	e, ok := bc.m[addr]
	bc.mu.RUnlock()
	if !ok {
		return false
	}
	if e.permanent {
		return true
	}
	// 前三次失败不阻塞，仅计数
	if e.failures < 3 {
		return false
	}
	// 检查是否在临时封禁期内
	if time.Now().Before(e.blockedUntil) {
		return true
	}
	// 封禁期已过，允许重试
	return false
}

func (bc *blockedCache) markBlocked(addr string) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	entry, ok := bc.m[addr]
	if !ok {
		entry = &blockEntry{}
		bc.m[addr] = entry
	}
	if entry.permanent {
		return // 已经是永久封禁
	}
	entry.failures++
	if entry.failures <= 3 {
		entry.blockedUntil = time.Now().Add(bc.ttl)
		log.Printf("[socks] %s is temporarily blocked until %s", addr, entry.blockedUntil)
		return
	}
	if entry.failures > 3 {
		entry.permanent = true
		bc.savePermanent(addr)
		log.Printf("[socks] %s is permanently blocked after %d failures", addr, entry.failures)
	}
}

func (bc *blockedCache) unmarkBlocked(addr string) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	// 如果连接成功，且不是永久封禁，则清除失败记录
	if e, ok := bc.m[addr]; ok && !e.permanent {
		delete(bc.m, addr)
	}
}

func main() {
	var listenAddr string

	flag.StringVar(&listenAddr, "listen", ":1080", "local address to listen on")
	flag.DurationVar(&blockedTTL, "blocked-ttl", 10*time.Minute, "duration to block an address")
	flag.StringVar(&blockedFile, "blocked-file", "blocked.txt", "file to store permanently blocked addresses")
	flag.Parse()

	blocked = newBlockedCache(blockedTTL, blockedFile)
	runServer(listenAddr)
}

func runServer(listenAddr string) {
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %s", listenAddr, err)
	}
	log.Printf("Socks5 server listening on %s", listenAddr)

	for {
		conn_from_client, err := ln.Accept()
		if err != nil {
			log.Printf("failed to accept: %s", err)
			continue
		}
		go handleConn(conn_from_client)
	}
}

func handleConn(conn_from_client net.Conn) {
	defer conn_from_client.Close()
	// 第 1 阶段：SOCKS5 握手
	if err := socks5Handshake(conn_from_client); err != nil {
		log.Printf("handshake failed: %v", err)
		return
	}

	// 第 2 阶段：解析请求
	cmd, targetAddr, err := socks5ParseRequest(conn_from_client)
	if err != nil {
		log.Printf("request parse failed: %v", err)
		sendSocks5Reply(conn_from_client, RepCommandNotSupported, nil)
		return
	}
	// 第 3 阶段：建立连接并转发
	switch cmd {
	case CmdConnect:
		if err := relay(conn_from_client, targetAddr); err != nil {
			log.Printf("[socks] relay failed: %v", err)
			// 此处的 err 可能是缓存拒绝或者无法 Dial 服务器
			sendSocks5Reply(conn_from_client, RepHostUnreachable, nil)
			return
		}
	case CmdUDPAssociate:
		handleUDPAssociate(conn_from_client)
	default:
		log.Printf("unsupported command: %v", err)
		sendSocks5Reply(conn_from_client, RepCommandNotSupported, nil)
	}
}

// UDP 数据报格式：
// +----+------+------+----------+----------+----------+
// |RSV | FRAG | ATYP | DST.ADDR | DST.PORT |   DATA   |
// +----+------+------+----------+----------+----------+
// | 2  |  1   |  1   | Variable |    2     | Variable |
// +----+------+------+----------+----------+----------+
//
// 客户端发送 UDP_ASSOCIATE 命令
// 服务器创建 UDP 监听端口并返回绑定地址
// 客户端向该 UDP 端口发送带 SOCKS5 头的数据报
// 服务器解析头部，转发数据到目标
// 目标响应后，服务器构造 SOCKS5 UDP 响应返回客户端
// TCP 连接断开时，UDP 会话结束
func handleUDPAssociate(conn_from_client net.Conn) {
	// 创建 UDP 监听地址, 表示让系统自动选择 IPv4 或 IPv6,
	// IP 地址为 nil → 绑定到所有可用网络接口（等价于 0.0.0.0 for IPv4 或 [::] for IPv6）
	udpConn, err := net.ListenUDP("udp", nil)
	if err != nil {
		log.Printf("[udp] failed to create udp linstening: %v", err)
		sendSocks5Reply(conn_from_client, RepServerFailure, nil)
		return
	}
	defer udpConn.Close()
	localAddr := udpConn.LocalAddr().(*net.UDPAddr)
	log.Printf("[udp] opened udp relay on %s", localAddr)
	if err := sendSocks5Reply(conn_from_client, RepSuccess, localAddr); err != nil {
		log.Printf("[udp] failed to send reply: %v", err)
		return
	}
	// 维持tcp连接, 直到断开
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
			log.Printf("[udp] TCP connection closed, stopping udp relay")
			return
		default:
		}
		n, clientAddr, err := udpConn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				udpConn.SetReadDeadline(time.Now().Add(5 * time.Minute))
				continue
			}
			log.Printf("[udp] read error: %v", err)
			return
		}
		udpConn.SetReadDeadline(time.Now().Add(5 * time.Minute))
		if n < 10 {
			log.Printf("[udp] packet too short: %d bytes", n)
			continue
		}
		go handleUDPPacket(udpConn, clientAddr, buf[:n])
	}
}

func handleUDPPacket(udpConn *net.UDPConn, clientAddr *net.UDPAddr, packet []byte) {
	// +----+------+------+----------+----------+----------+
	// |RSV | FRAG | ATYP | DST.ADDR | DST.PORT |   DATA   |
	// +----+------+------+----------+----------+----------+
	// | 2  |  1   |  1   | Variable |    2     | Variable |
	// +----+------+------+----------+----------+----------+
	if len(packet) < 10 {
		return
	}
	frag := packet[2]
	if frag != 0x00 {
		log.Printf("[udp] fragmentation not supported")
		return
	}
	atyp := packet[3]
	var dstAddr string
	var dataOffset int
	switch atyp {
	// 如果是 ipv4 地址, 那么各个字段依次占用 2, 1, 1, 4, 2, dataoffset = 10
	case ATYPIPv4:
		if len(packet) < 10 {
			return
		}
		ip := net.IP(packet[4:8])
		port := binary.BigEndian.Uint16(packet[8:10])
		dstAddr = fmt.Sprintf("%s:%d", ip.String(), port)
		dataOffset = 10
	// 如果是域名, 那么各个字段依次占用 2, 1, 1, 1+域名长度, 2, dataoffset = 5 + 域名长度 + 2
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
	// 如果是 ipv6 地址, 那么各个字段一次占用 2, 1, 1, 16, 2, dataoffset = 22
	case ATYPIPv6:
		// 如果 packet 长度小于22个字节, data段为空, 无意义
		if len(packet) < 22 {
			return
		}
		ip := net.IP(packet[4:20])
		port := binary.BigEndian.Uint16(packet[20:22])
		dstAddr = fmt.Sprintf("%s:%d", ip.String(), port)
		dataOffset = 22
	default:
		log.Printf("[udp] unsupported address type: %d", atyp)
		return
	}
	data := packet[dataOffset:]
	log.Printf("[udp] relaying %d bytes from %s to %s", len(data), clientAddr, dstAddr)

	if blocked != nil && blocked.isBlocked(dstAddr) {
		log.Printf("[udp] target %s is blocked", dstAddr)
		return
	}
	targetConn, err := net.DialTimeout("udp", dstAddr, 6*time.Second)
	if err != nil {
		log.Printf("[udp] failed to dial %s: %v", dstAddr, err)
		if blocked != nil {
			blocked.markBlocked(dstAddr)
			log.Printf("[socks] marking %s as blocked for %s", dstAddr, blockedTTL)
		}
		return
	}
	defer targetConn.Close()
	if blocked != nil {
		blocked.unmarkBlocked(dstAddr)
	}
	if _, err := targetConn.Write(data); err != nil {
		log.Printf("[udp] failed to write to target: %v", err)
		return
	}
	// targetConn.SetReadDeadline(time.Now().Add(10 * time.Second))
	targetConn.SetReadDeadline(time.Now().Add(10 * time.Second))
	respBuf := make([]byte, 65535)
	n, err := targetConn.Read(respBuf)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			log.Printf("[udp] target response timeout for %s", dstAddr)
		} else {
			log.Printf("[udp] failed to read from target: %v", err)
		}
		return
	}
	response := buildUDPResponse(dstAddr, respBuf[:n])
	if _, err := udpConn.WriteToUDP(response, clientAddr); err != nil {
		log.Printf("[udp] failed to write back to client: %v", err)
	} else {
		log.Printf("[udp] repyed %d bytes back to client", n)
	}
}

func buildUDPResponse(addr string, data []byte) []byte {
	// +----+------+------+----------+----------+----------+
	// |RSV | FRAG | ATYP | DST.ADDR | DST.PORT |   DATA   |
	// +----+------+------+----------+----------+----------+
	// | 2  |  1   |  1   | Variable |    2     | Variable |
	// +----+------+------+----------+----------+----------+
	host, postStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil
	}
	port := 0
	fmt.Sscanf(postStr, "%d", &port)
	var header []byte
	header = append(header, 0, 0, 0) // RSV(2), FRAG(1)
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
	// 以大端序将 port 写入 portBytes 切片
	binary.BigEndian.PutUint16(portBytes, uint16(port))
	header = append(header, portBytes...)
	header = append(header, data...)
	return header
}

func relay(conn_from_client net.Conn, targetAddr string) error {
	if blocked != nil && blocked.isBlocked(targetAddr) {
		return fmt.Errorf("refuse to dial blocked cache")
	}
	conn_from_server, err := net.DialTimeout("tcp", targetAddr, 6*time.Second)
	if err != nil {
		if blocked != nil {
			blocked.markBlocked(targetAddr)
			log.Printf("[socks] marking %s as blocked for %s", targetAddr, blockedTTL)
		}
		return err
	}
	if blocked != nil {
		blocked.unmarkBlocked(targetAddr)
	}
	// 接收缓冲（RcvBuf）和发送缓冲（SndBuf）位于内核，
	// 用于临时存放尚未由应用读取或尚未被对端确认的数据。
	// 直接将tcp缓存区设置为64 * 1024只是权宜之计,
	// 军工级别的优化方案是根据BDP动态设置缓存
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
	if err := sendSocks5Reply(conn_from_client, RepSuccess, nil); err != nil {
		log.Printf("[socks] failed to send reply : %v", err)
		return err
	}

	var wg sync.WaitGroup
	wg.Add(2)

	// 转发函数
	transfer := func(dst io.Writer, src io.Reader) {
		defer wg.Done()
		// pool 的三个方法
		// Get 是从仓库取对象，Put 是把对象放回仓库，New 是仓库空的时候用来生产新对象的工厂
		bufPtr := pool.Get().(*[]byte)
		buf := *bufPtr
		defer pool.Put(bufPtr)
		_, err := io.CopyBuffer(dst, src, buf)
		if err != nil && !isIgnorableError(err) {
			log.Printf("[relay] err during copy %v", err)
		}
		if err == nil || err == io.EOF {
			// 正常结束的 copy，表示 src 方向发送 FIN
			if tcpDst, ok := dst.(*net.TCPConn); ok {
				tcpDst.CloseWrite()
			}
		} else {
			// 如果不是 TCPConn，尽量也尝试直接关闭目标写端（保底）
			if closer, ok := dst.(interface{ Close() error }); ok {
				// 不能立刻 Close，否则会中断另一端。这里一般不执行。
				_ = closer
			}
		}
	}
	// client → target
	go transfer(conn_from_server, conn_from_client)
	// target → client
	go transfer(conn_from_client, conn_from_server)
	// 等待两个完成信号 (即等待两个 io.Copy goroutine 都已退出)
	wg.Wait()
	// 关闭连接
	conn_from_client.Close()
	conn_from_server.Close()
	return nil
}

func isIgnorableError(err error) bool {
	if err == nil {
		return true
	}
	// 常见的正常退出错误
	if errors.Is(err, io.EOF) {
		return true
	}
	msg := err.Error()
	if strings.Contains(msg, "use of closed network connection") {
		return true
	}
	if strings.Contains(msg, "connection reset by peer") {
		return true
	}
	if strings.Contains(msg, "broken pipe") {
		return true
	}
	if strings.Contains(msg, "i/o timeout") {
		return true
	}
	return false
}

func socks5Handshake(conn net.Conn) error {
	// app greeting
	// +----+----------+----------+
	// |VER | NMETHODS | METHODS  |
	// +----+----------+----------+
	// | 1  |    1     | 1~255    |
	// +----+----------+----------+
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
	// server regreeting
	// +----+--------+
	// |VER | METHOD |
	// +----+--------+
	// | 1  |   1    |
	// +----+--------+
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
	// subnegotiation request:
	// +----+------+----------+------+----------+
	// |VER | ULEN |  UNAME   | PLEN |  PASSWD  |
	// +----+------+----------+------+----------+
	// | 1  |  1   | 1~255    |  1   | 1~255    |
	// +----+------+----------+------+----------+
	//
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
	if strings.TrimSpace(username) == "" {
		return false
	}
	return false
}

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
	port := binary.BigEndian.Uint16(portBytes)
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
		portBytes := make([]byte, 2)
		binary.BigEndian.PutUint16(portBytes, uint16(bindAddr.Port))
		reply = append(reply, portBytes...)
	} else {
		reply = append(reply, ATYPIPv4)
		reply = append(reply, []byte{0, 0, 0, 0}...)
		reply = append(reply, []byte{0, 0}...)
	}
	_, err := conn.Write(reply)
	return err
}
