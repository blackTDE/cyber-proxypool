package dialer

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
)

// EncodeTargetAddr encodes host:port into standard SOCKS5 address bytes
// [atyp: 1 byte][address: variable][port: 2 bytes]
func EncodeTargetAddr(target string) ([]byte, error) {
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return nil, fmt.Errorf("invalid target '%s': %w", target, err)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return nil, fmt.Errorf("invalid port in '%s'", target)
	}

	var buf []byte
	ip := net.ParseIP(host)
	if ip == nil {
		// Domain name
		if len(host) > 255 {
			return nil, fmt.Errorf("domain name too long: %s", host)
		}
		buf = make([]byte, 1+1+len(host)+2)
		buf[0] = 0x03 // Domain atyp
		buf[1] = byte(len(host))
		copy(buf[2:], host)
		binary.BigEndian.PutUint16(buf[2+len(host):], uint16(port))
		return buf, nil
	}

	if ipv4 := ip.To4(); ipv4 != nil {
		// IPv4
		buf = make([]byte, 1+4+2)
		buf[0] = 0x01 // IPv4 atyp
		copy(buf[1:5], ipv4)
		binary.BigEndian.PutUint16(buf[5:7], uint16(port))
		return buf, nil
	}

	// IPv6
	buf = make([]byte, 1+16+2)
	buf[0] = 0x04 // IPv6 atyp
	copy(buf[1:17], ip.To16())
	binary.BigEndian.PutUint16(buf[17:19], uint16(port))
	return buf, nil
}
