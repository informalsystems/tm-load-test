package loadtest

import (
	"fmt"
	"net"
	"os"
	"strings"
)

const hostIDCharWhitelist = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz_"

func sanitizeHostID(id string) string {
	result := ""
	for _, r := range id {
		if strings.ContainsRune(hostIDCharWhitelist, r) {
			result += string(r)
		} else {
			result += "_"
		}
	}
	return result
}

func getOrGenerateHostID(suffix string) string {
	hostname, err := os.Hostname()
	if err == nil {
		hostname = hostname + "_"
	}
	return sanitizeHostID(hostname + suffix)
}

// resolveBindAddr will take the given address, attempt to listen on it, and
// then return a "host:port" string with the precise host/port details in it.
func resolveBindAddr(addr string) (string, error) {
	a, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return "", err
	}
	l, err := net.ListenTCP("tcp", a)
	if err != nil {
		return "", err
	}
	defer l.Close()
	tcpAddr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return "", fmt.Errorf("failed to obtain TCPAddr from bind address: %s", addr)
	}
	return fmt.Sprintf("%s:%d", tcpAddr.IP, tcpAddr.Port), nil
}
