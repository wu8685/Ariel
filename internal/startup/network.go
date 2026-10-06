package startup

import (
	"context"
	"errors"
	"net/netip"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type commandOutput func(context.Context, string, ...string) (string, error)

var networkInterfacePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

func systemCommandOutput(ctx context.Context, command string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, command, args...).Output()
	if err != nil {
		return "", errors.New("network discovery command failed")
	}
	return string(output), nil
}

func discoverPrivateLANIPv4(ctx context.Context, run commandOutput) (string, error) {
	route, err := run(ctx, "/sbin/route", "-n", "get", "default")
	if err != nil {
		return "", errors.New("cannot determine the default network interface")
	}
	var networkInterface string
	for _, line := range strings.Split(route, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "interface:" {
			networkInterface = fields[1]
			break
		}
	}
	if !networkInterfacePattern.MatchString(networkInterface) {
		return "", errors.New("default network interface is missing or invalid")
	}
	value, err := run(ctx, "/usr/sbin/ipconfig", "getifaddr", networkInterface)
	if err != nil {
		return "", errors.New("default network interface has no IPv4 address")
	}
	address, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil || !address.Is4() || !privateIPv4(address) {
		return "", errors.New("default network interface must have an RFC1918 private IPv4 address")
	}
	return address.String(), nil
}

func privateIPv4(address netip.Addr) bool {
	return netip.MustParsePrefix("10.0.0.0/8").Contains(address) ||
		netip.MustParsePrefix("172.16.0.0/12").Contains(address) ||
		netip.MustParsePrefix("192.168.0.0/16").Contains(address)
}
