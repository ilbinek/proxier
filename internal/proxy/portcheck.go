package proxy

import (
	"net"
	"strconv"

	"proxier/internal/model"
)

// CheckPortFree tries to bind proto on ip:port and immediately releases it.
func CheckPortFree(proto model.Protocol, ip string, port int) error {
	addr := net.JoinHostPort(ip, strconv.Itoa(port))
	if proto == model.ProtoTCP {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		return ln.Close()
	}
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		return err
	}
	return pc.Close()
}
