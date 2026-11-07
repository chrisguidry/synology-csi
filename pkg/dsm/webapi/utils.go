// Copyright 2022 Synology Inc.

package webapi

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/SynologyOpenSource/synology-csi/pkg/utils"
)

func (dsm *DSM) IsUC() bool {
	return strings.Contains(dsm.FirmwareVer, "DSM UC")
}

// GetAnotherController finds a reachable address of the other controller
// of a UC pair. The DSM it returns, and the requests made to find it, carry
// ctx, so they stop when the caller's CSI call ends.
func (dsm *DSM) GetAnotherController(ctx context.Context) (*DSM, error) {
	dsm = dsm.WithContext(ctx)
	anotherDsm := &DSM{
		ctx:                ctx,
		Port:               dsm.Port,
		Username:           dsm.Username,
		Password:           dsm.Password,
		Https:              dsm.Https,
		TLSCACert:          dsm.TLSCACert,
		TLSServerName:      dsm.TLSServerName,
		InsecureSkipVerify: dsm.InsecureSkipVerify,
	}

	netListA, err := dsm.NetworkInterfaceList("node0")
	if err != nil {
		return nil, fmt.Errorf("Failed to get DSM network list of controller A. %v", err)
	}

	netListB, err := dsm.NetworkInterfaceList("node1")
	if err != nil {
		return nil, fmt.Errorf("Failed to get DSM network list of controller B. %v", err)
	}

	ips, err := utils.LookupIPv4(dsm.Ip) // because dsm.Ip may be a domain
	if err != nil {
		return nil, err
	}

	// Concurrent CSI calls share dsm, so only the new DSM records which
	// controller it is.
	anotherDsm.Controller = "B"
	anotherList := netListB
	for _, netIf := range netListB {
		if netIf.Ip == ips[0] {
			anotherDsm.Controller = "A"
			anotherList = netListA
			break
		}
	}

	ipPrefix := ips[0]
	for i := 0; i < 4; i++ {
		dotPos := strings.LastIndex(ipPrefix, ".")

		if dotPos == -1 {
			ipPrefix = ""
		} else {
			ipPrefix = ipPrefix[:dotPos]
		}

		for _, netIf := range anotherList {
			if ipPrefix != "" && !strings.HasPrefix(netIf.Ip, ipPrefix) {
				continue
			}

			if netIf.Status == "connected" && CheckIpReachable(ctx, netIf.Ip, anotherDsm.Port) {
				anotherDsm.Ip = netIf.Ip
				return anotherDsm, nil
			}
		}
	}

	return nil, fmt.Errorf("Failed to get reachable network of another controller.")
}

func CheckIpReachable(ctx context.Context, ip string, port int) bool {
	seconds := 5
	dialer := net.Dialer{Timeout: time.Duration(seconds) * time.Second}

	conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", ip, port))
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
