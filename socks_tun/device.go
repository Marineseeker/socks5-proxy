package socks_tun

import (
	"errors"
	"fmt"
	"os/exec"

	"go.uber.org/zap"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wintun"
)

type TunDevice struct {
	// ringBufSize 是 Wintun Session 的环形缓冲区（Ring Buffer）大小，
	// 用于在 wintun 与 socks5 之间缓存数据包
	ringBufSize uint32
	adapter     *wintun.Adapter
	session     *wintun.Session
}

func (t *TunDevice) Close() {
	if t.session != nil {
		t.session.End()
		t.session = nil
	}

	if t.adapter != nil {
		t.adapter.Close()
		t.adapter = nil
	}
}

func NewTunDevice(ringBufSize uint32) *TunDevice {
	if ringBufSize == 0 {
		ringBufSize = 0x400000
	}

	return &TunDevice{
		ringBufSize: ringBufSize,
	}
}

func (t *TunDevice) Start(tunName string) error {
	adapter, err := wintun.OpenAdapter(tunName)
	if err != nil {
		adapter, err = wintun.CreateAdapter(
			tunName,
			"Wintun",
			nil,
		)
		if err != nil {
			zap.S().Errorf("Create adapter %q failed: %v", tunName, err)
			return err
		}
	}

	// startSession会在内部创建一个环形缓冲区，并返回一个Session对象，
	// Session对象提供了ReceivePacket和SendPacket方法来接收和发送数据包

	// `adapter.StartSession()` 返回的 `session`，
	// 可以理解为 __"在这张虚拟网卡上开辟的一段用户态↔内核态共享 Ring Buffer 的读写句柄"__。
	// Adapter 决定"网卡是否存在"，Session 决定"这张网卡上的数据流是否打开"
	// ——前者是设备，后者是通道。
	session, err := adapter.StartSession(t.ringBufSize)
	if err != nil {
		adapter.Close()
		zap.S().Errorf("Start session failed: %v", err)
		return err
	}
	t.adapter = adapter
	t.session = &session

	if err := t.configureIP(tunName); err != nil {
		t.Close()
		return err
	}

	go t.handleSession()

	return nil
}

func (t *TunDevice) configureIP(name string) error {
	cmd := exec.Command(
		"netsh",
		"interface",
		"ipv4",
		"set",
		"address",
		fmt.Sprintf(`name=%s`, name),
		"static",
		"10.0.0.1",
		"255.255.255.0",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"configure tun ip failed: %v: %s",
			err,
			string(out),
		)
	}
	zap.S().Infof("Configured tun IP: %s", string(out))
	return nil
}

func (t *TunDevice) handleSession() {
	defer t.Close()
	readWaitEvent := t.session.ReadWaitEvent()

	for {
		packet, err := t.session.ReceivePacket()
		if err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_ITEMS) {
				_, waitErr := windows.WaitForSingleObject(
					readWaitEvent,
					1000,
				)
				if waitErr != nil {
					zap.S().Errorf("Wait for packet failed: %v", waitErr)
				}
				continue
			}

			zap.S().Errorf("Receive Packet failed: %v", err)
			continue
		}

		zap.S().Infof("Received packet: %d", len(packet))
		version := packet[0] >> 4
		switch version {
		case 4:
			// IPv4
			header, err := parseIPv4Header(packet)
			if err != nil {
				zap.S().Errorf("Failed to parse IPv4 header: %v", err)
				continue
			}
			zap.S().Infof("Received IPv4 packet: %v", header)
			payload := packet[int(header.IHL)*4:]
			switch header.Protocol {
			case 6:
				tcpHeader, err := parseTCPHeader(payload)
				if err != nil {
					zap.S().Errorf("Failed to parse TCP header: %v", err)
					continue
				}
				zap.S().Infof(
					"TCP %s:%d -> %s:%d flags=%s",
					header.SrcIP,
					tcpHeader.SrcPort,
					header.DstIP,
					tcpHeader.DstPort,
					tcpHeader.FlagString(),
				)
			case 17:
				// todo 解析 UDP 包头，目前先不处理 UDP 包
				err = parseUDPHeader(payload)
				if err != nil {
					zap.S().Errorf("Failed to parse UDP header: %v", err)
				}
			}
		case 6:
			// IPv6 packet
			zap.S().Infof("Received IPv6 packet")
		default:
			// Unknown version
			zap.S().Warnf("Received packet with unknown IP version: %d", version)
		}
		t.session.ReleaseReceivePacket(packet)
	}
}
