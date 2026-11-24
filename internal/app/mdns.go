package app

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	discoveryfeature "gohome/internal/features/discovery"
)

type dnsRecord struct {
	name   string
	typ    uint16
	port   uint16
	ptr    string
	target string
	ip     string
}

var mdnsServiceTypes = []string{
	"_http._tcp.local", "_https._tcp.local", "_hap._tcp.local",
	"_mqtt._tcp.local", "_matter._tcp.local", "_miio._udp.local",
	"_ewelink._tcp.local", "_services._dns-sd._udp.local",
}

func encodeDNSName(name string) []byte {
	var out []byte
	for _, p := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		if len(p) == 0 || len(p) > 63 {
			continue
		}
		out = append(out, byte(len(p)))
		out = append(out, []byte(p)...)
	}
	return append(out, 0)
}

func mdnsQuery(name string) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint16(b[4:6], 1)
	b = append(b, encodeDNSName(name)...)
	b = append(b, 0, 12, 0, 1) // PTR, IN
	return b
}

func readDNSName(msg []byte, off int) (string, int, bool) {
	if off < 0 || off >= len(msg) {
		return "", off, false
	}
	parts := make([]string, 0, 5)
	pos, next, jumped, hops := off, off, false, 0
	for pos < len(msg) && hops < 32 {
		hops++
		l := int(msg[pos])
		if l == 0 {
			if !jumped {
				next = pos + 1
			}
			return strings.Join(parts, "."), next, true
		}
		if l&0xc0 == 0xc0 {
			if pos+1 >= len(msg) {
				return "", off, false
			}
			ptr := int(msg[pos]&0x3f)<<8 | int(msg[pos+1])
			if !jumped {
				next = pos + 2
				jumped = true
			}
			if ptr >= len(msg) {
				return "", off, false
			}
			pos = ptr
			continue
		}
		if l&0xc0 != 0 || l > 63 || pos+1+l > len(msg) {
			return "", off, false
		}
		parts = append(parts, string(msg[pos+1:pos+1+l]))
		pos += 1 + l
		if !jumped {
			next = pos
		}
	}
	return "", off, false
}

func parseDNSRecords(msg []byte) []dnsRecord {
	if len(msg) < 12 {
		return nil
	}
	qd := int(binary.BigEndian.Uint16(msg[4:6]))
	an := int(binary.BigEndian.Uint16(msg[6:8]))
	ns := int(binary.BigEndian.Uint16(msg[8:10]))
	ar := int(binary.BigEndian.Uint16(msg[10:12]))
	off := 12
	for i := 0; i < qd; i++ {
		_, n, ok := readDNSName(msg, off)
		if !ok || n+4 > len(msg) {
			return nil
		}
		off = n + 4
	}
	count := an + ns + ar
	if count > 512 {
		count = 512
	}
	out := make([]dnsRecord, 0, count)
	for i := 0; i < count; i++ {
		name, n, ok := readDNSName(msg, off)
		if !ok || n+10 > len(msg) {
			break
		}
		off = n
		typ := binary.BigEndian.Uint16(msg[off : off+2])
		rdlen := int(binary.BigEndian.Uint16(msg[off+8 : off+10]))
		off += 10
		if off+rdlen > len(msg) {
			break
		}
		r := dnsRecord{name: name, typ: typ}
		switch typ {
		case 1:
			if rdlen == 4 {
				r.ip = net.IP(msg[off : off+4]).String()
			}
		case 12:
			if target, _, ok := readDNSName(msg, off); ok {
				r.ptr = target
			}
		case 33:
			if rdlen >= 7 {
				r.port = binary.BigEndian.Uint16(msg[off+4 : off+6])
				if target, _, ok := readDNSName(msg, off+6); ok {
					r.target = target
				}
			}
		case 28:
			if rdlen == 16 {
				r.ip = net.IP(msg[off : off+16]).String()
			}
		}
		out = append(out, r)
		off += rdlen
	}
	return out
}

func scanMDNS(ctx context.Context) ([]Discovery, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	all := make(map[string]Discovery)
	var firstErr error
	for _, iface := range ifs {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		conn, err := net.ListenMulticastUDP("udp4", &iface, &net.UDPAddr{IP: net.ParseIP("224.0.0.251"), Port: 5353})
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		_ = conn.SetReadBuffer(256 * 1024)
		_ = conn.SetWriteBuffer(64 * 1024)
		for _, service := range mdnsServiceTypes {
			_, _ = conn.WriteToUDP(mdnsQuery(service), &net.UDPAddr{IP: net.ParseIP("224.0.0.251"), Port: 5353})
		}
		deadline := time.Now().Add(1300 * time.Millisecond)
		_ = conn.SetReadDeadline(deadline)
		buf := make([]byte, 9000)
		var records []dnsRecord
		for {
			select {
			case <-ctx.Done():
				_ = conn.Close()
				return assembleDiscovery(records, iface.Name), nil
			default:
			}
			n, _, e := conn.ReadFromUDP(buf)
			if e != nil {
				if ne, ok := e.(net.Error); ok && ne.Timeout() {
					goto finished
				}
				if firstErr == nil {
					firstErr = e
				}
				goto finished
			}
			records = append(records, parseDNSRecords(append([]byte(nil), buf[:n]...))...)
		}
	finished:
		for _, d := range assembleDiscovery(records, iface.Name) {
			key := discoveryfeature.NormalizeService(d.Name + "|" + d.Address + fmt.Sprint(d.Port))
			all[key] = d
		}
		_ = conn.Close()
	}
	result := make([]Discovery, 0, len(all))
	for _, d := range all {
		result = append(result, d)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	if len(ifs) == 0 {
		return result, fmt.Errorf("没有可用网络接口")
	}
	if len(result) == 0 && firstErr != nil {
		return result, firstErr
	}
	return result, nil
}

func assembleDiscovery(records []dnsRecord, ifname string) []Discovery {
	ptrs := map[string]string{}
	srvs := map[string]dnsRecord{}
	ips := map[string]string{}
	for _, r := range records {
		switch r.typ {
		case 12:
			if r.ptr != "" {
				ptrs[r.ptr] = r.name
			}
		case 33:
			srvs[r.name] = r
		case 1, 28:
			if r.ip != "" {
				ips[r.name] = r.ip
			}
		}
	}
	out := make([]Discovery, 0, len(ptrs))
	for instance, service := range ptrs {
		srv, ok := srvs[instance]
		host := ""
		address := ""
		port := 0
		if ok {
			host = srv.target
			port = int(srv.port)
			address = ips[host]
		}
		out = append(out, Discovery{ID: newID(), Name: instance, Service: service, Host: host, Address: address, Port: port, Interface: ifname, FoundAt: time.Now()})
	}
	return out
}
