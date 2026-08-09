package client

import (
	"encoding/json"
	"regexp"
	"testing"
)

func TestIsPrivateAddress(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		// IPv4 RFC1918
		{"10.0.0.1", true},
		{"10.255.255.254", true},
		{"172.16.0.1", true},
		{"172.31.255.1", true},
		{"172.32.0.1", false},
		{"192.168.1.1", true},
		{"192.169.0.1", false},
		// IPv4 CGNAT (RFC6598), link-local (RFC3927), loopback
		{"100.64.0.1", true},
		{"100.127.255.1", true},
		{"100.128.0.1", false},
		{"169.254.10.1", true},
		{"127.0.0.1", true},
		// IPv4 public
		{"8.8.8.8", false},
		{"203.0.113.10", false},
		// IPv4-mapped IPv6
		{"::ffff:10.0.0.1", true},
		{"::ffff:8.8.8.8", false},
		// IPv6 ULA (RFC4193), link-local, loopback
		{"fc00::1", true},
		{"fd12:3456:789a::1", true},
		{"fe80::1", true},
		{"::1", true},
		// IPv6 global
		{"2001:db8::1", false},
		{"2a00:1450:4001::1", false},
		// Invalid input
		{"", false},
		{"not-an-ip", false},
	}
	for _, c := range cases {
		if got := isPrivateAddress(c.ip); got != c.want {
			t.Errorf("isPrivateAddress(%q) = %v, want %v", c.ip, got, c.want)
		}
	}
}

func TestFlexTypes(t *testing.T) {
	var payload struct {
		S1 flexString `json:"s1"`
		S2 flexString `json:"s2"`
		S3 flexString `json:"s3"`
		I1 flexInt    `json:"i1"`
		I2 flexInt    `json:"i2"`
		I3 flexInt    `json:"i3"`
	}
	raw := `{"s1": "text", "s2": 7, "s3": null, "i1": 24, "i2": "16", "i3": null}`
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if payload.S1 != "text" || payload.S2 != "7" || payload.S3 != "" {
		t.Errorf("flexString results: %q %q %q", payload.S1, payload.S2, payload.S3)
	}
	if payload.I1 != 24 || payload.I2 != 16 || payload.I3 != 0 {
		t.Errorf("flexInt results: %d %d %d", payload.I1, payload.I2, payload.I3)
	}
}

func TestLoopbackInterfaceName(t *testing.T) {
	cases := map[string]string{
		"100":   "loopback100",
		"0":     "loopback0",
		"lo100": "lo100",
		"":      "",
	}
	for key, want := range cases {
		if got := loopbackInterfaceName(key); got != want {
			t.Errorf("loopbackInterfaceName(%q) = %q, want %q", key, got, want)
		}
	}
}

// TestBuildInterfaces feeds representative Orchestrator JSON through the
// wire-format parsing and the interface assembly, covering management
// interfaces, static WAN with discovered public IP, DHCP WAN whose current
// address comes from the tunnels view, VLAN sub-interfaces, label
// resolution, and loopbacks.
func TestBuildInterfaces(t *testing.T) {
	deploymentJSON := `{
		"mgmtIfData": {
			"mgmt0": {"ip": "192.168.10.5", "mask": 24, "nexthop": "192.168.10.1", "dhcp": false},
			"mgmt1": {"ip": "", "mask": 0, "nexthop": "", "dhcp": false}
		},
		"modeIfs": [
			{
				"ifName": "wan0",
				"applianceIPs": [
					{"ip": "10.99.1.2", "mask": 30, "label": "1", "wanSide": true, "lanSide": false, "behindNAT": "auto", "wanNexthop": "10.99.1.1", "subif": "0", "vlan": "", "intf_mode": "static", "version": 4, "vrf": 0, "zone": 21, "harden": 3, "maxBW": {"outbound": 100000, "inbound": 500000}}
				]
			},
			{
				"ifName": "wan1",
				"applianceIPs": [
					{"ip": "203.0.113.10", "mask": 29, "label": "2", "wanSide": true, "lanSide": false, "behindNAT": "none", "wanNexthop": "203.0.113.9", "subif": "0", "vlan": "", "intf_mode": "static", "version": 4, "vrf": 0, "zone": 21, "harden": 2, "maxBW": {"outbound": 50000, "inbound": 50000}},
					{"ip": "fd00:99::2", "mask": 64, "label": "2", "wanSide": true, "lanSide": false, "behindNAT": "auto", "wanNexthop": "fd00:99::1", "subif": "0", "vlan": "", "intf_mode": "static", "version": 6, "vrf": 0, "zone": 21, "harden": 2, "maxBW": {"outbound": 50000, "inbound": 50000}}
				]
			},
			{
				"ifName": "wan2",
				"applianceIPs": [
					{"ip": "", "mask": 0, "label": "1", "wanSide": true, "lanSide": false, "behindNAT": "auto", "wanNexthop": "", "subif": "0", "vlan": "", "dhcp": true, "intf_mode": "dhcpv4", "version": 4, "vrf": 0, "zone": 0, "harden": 0}
				]
			},
			{
				"ifName": "lan0",
				"applianceIPs": [
					{"ip": "172.16.1.1", "mask": 24, "label": "10", "wanSide": false, "lanSide": true, "behindNAT": "", "wanNexthop": "", "subif": "0", "vlan": "100", "intf_mode": "static", "version": 4, "vrf": 1, "zone": 40,
					 "dhcpd": {"type": "server", "server": {
						"prefix": "172.16.1.0/24", "ipStart": "172.16.1.100", "ipEnd": "172.16.1.199",
						"ip_range": {"1": {"start": "172.16.1.220", "end": "172.16.1.230"}},
						"gw": ["172.16.1.1"], "dns": ["172.16.1.53", "9.9.9.9"], "ntpd": ["172.16.1.123"],
						"netbios": ["172.16.1.19"], "netbiosNodeType": "H",
						"defaultLease": 86400, "maxLease": "172800",
						"options": {"43": "aruba-ap", "150": "10.1.1.1"},
						"failover": true,
						"host": {"printer01": {"ip": "172.16.1.240", "mac": "aa:bb:cc:dd:ee:01"}, "cam01": {"ip": "172.16.1.241", "mac": "aa:bb:cc:dd:ee:02"}}
					 }}},
					{"ip": "172.16.2.1", "mask": 24, "label": "10", "wanSide": false, "lanSide": true, "behindNAT": "", "wanNexthop": "", "subif": "0", "vlan": "200", "intf_mode": "static", "version": 4, "vrf": 1, "zone": 40,
					 "dhcpd": {"type": "relay", "relay": {"dhcpserver": ["10.10.10.10", "10.10.10.11"], "option82": true, "option82_policy": "append"}}}
				]
			}
		],
		"sysConfig": {
			"mode": "router",
			"maxBW": 200000,
			"maxInBW": "400000",
			"licence": {"ecTier": "1 Gbps", "ecTierBW": 1000000, "ecBoost": true, "ecBoostBW": 100000, "ecMini": false, "ecPlus": true},
			"ifLabels": {
				"wan": [{"id": 1, "name": "INET1"}, {"id": 2, "name": "INET2"}],
				"lan": [{"id": 10, "name": "Data"}]
			}
		}
	}`

	loopbackJSON := `{
		"100": {"admin": true, "ipaddr": "10.255.0.1", "nmask": 32, "label": "10", "zone": 20, "vrf_id": 1},
		"101": {"admin": true, "ipaddr": "fd00:10:255::1", "nmask": 128, "label": "", "zone": 0}
	}`

	tunnelsJSON := `{
		"77.NE": {
			"mode": "INLINE_ROUTER",
			"wanInterfaces": [
				{"name": "wan0", "ipAddress": "10.99.1.2", "mask": 30, "behindNAT": true, "publicIpAddress": "198.51.100.77", "outboundBandwidth": 11111, "inboundBandwidth": 22222, "wanSide": true},
				{"name": "wan1", "ipAddress": "203.0.113.10", "mask": 29, "behindNAT": false, "publicIpAddress": "203.0.113.10", "wanSide": true},
				{"name": "wan1", "ipAddress": "2001:db8:f::2", "mask": 64, "behindNAT": true, "publicIpAddress": "2001:db8:f::77", "wanSide": true},
				{"name": "wan2", "ipAddress": "192.168.178.20", "mask": 24, "dhcp": true, "behindNAT": true, "publicIpAddress": "198.51.100.88", "wanNextHop": "192.168.178.1", "outboundBandwidth": 95000, "inboundBandwidth": 250000, "wanSide": true}
			],
			"lanInterfaces": []
		}
	}`

	var dep deploymentResponse
	if err := json.Unmarshal([]byte(deploymentJSON), &dep); err != nil {
		t.Fatalf("unmarshal deployment: %v", err)
	}
	var loops map[string]loopbackDetail
	if err := json.Unmarshal([]byte(loopbackJSON), &loops); err != nil {
		t.Fatalf("unmarshal loopbacks: %v", err)
	}
	var tunnels map[string]tunnelsDeploymentInfo
	if err := json.Unmarshal([]byte(tunnelsJSON), &tunnels); err != nil {
		t.Fatalf("unmarshal tunnels: %v", err)
	}

	names := nameMaps{
		wanLabels: mergeLabelMaps(nil, dep.SysConfig.IfLabels.Wan),
		lanLabels: mergeLabelMaps(nil, dep.SysConfig.IfLabels.Lan),
		segments:  map[int]string{0: "Default", 1: "Guest"},
		zones:     map[int]string{20: "LAN", 21: "WAN", 40: "GuestLAN"},
	}

	interfaces := buildInterfaces(&dep, loops, tunnels["77.NE"], names)

	byName := map[string]ApplianceInterface{}
	for _, iface := range interfaces {
		byName[iface.Name] = iface
	}

	// mgmt1 has no IP and must be skipped; expected: mgmt0, wan0, wan1
	// (IPv4 + IPv6 = two entries with the same name), wan2 (from tunnels
	// view), lan0.100, lan0.200, loopback100, loopback101.
	if len(interfaces) != 9 {
		t.Fatalf("expected 9 interfaces, got %d: %+v", len(interfaces), interfaces)
	}

	// System-level deployment settings: license and system bandwidth.
	if dep.SysConfig.Licence == nil || dep.SysConfig.Licence.EcTier != "1 Gbps" ||
		int(dep.SysConfig.Licence.EcTierBW) != 1000000 || !bool(dep.SysConfig.Licence.EcBoost) ||
		int(dep.SysConfig.Licence.EcBoostBW) != 100000 {
		t.Errorf("unexpected licence: %+v", dep.SysConfig.Licence)
	}
	if int(dep.SysConfig.MaxBW) != 200000 || int(dep.SysConfig.MaxInBW) != 400000 {
		t.Errorf("unexpected system bandwidth: out=%d in=%d", int(dep.SysConfig.MaxBW), int(dep.SysConfig.MaxInBW))
	}

	mgmt0 := byName["mgmt0"]
	if mgmt0.Type != InterfaceTypeMgmt || mgmt0.CIDR != "192.168.10.5/24" || !mgmt0.IsPrivate {
		t.Errorf("unexpected mgmt0: %+v", mgmt0)
	}
	if mgmt0.NextHop != "192.168.10.1" || !mgmt0.NextHopIsPrivate {
		t.Errorf("mgmt0 next hop: %+v", mgmt0)
	}

	// Static WAN with RFC1918 address behind NAT: discovered public IP set.
	wan0 := byName["wan0"]
	if wan0.Type != InterfaceTypeWAN {
		t.Errorf("wan0 type = %q", wan0.Type)
	}
	if wan0.Label != "INET1" {
		t.Errorf("wan0 label = %q, want INET1", wan0.Label)
	}
	if !wan0.BehindNAT || wan0.PublicIP != "198.51.100.77" {
		t.Errorf("wan0 NAT/public IP: behindNAT=%v publicIP=%q", wan0.BehindNAT, wan0.PublicIP)
	}
	if !wan0.IsPrivate || wan0.CIDR != "10.99.1.2/30" {
		t.Errorf("wan0 addressing: %+v", wan0)
	}
	if wan0.NextHop != "10.99.1.1" || !wan0.NextHopIsPrivate {
		t.Errorf("wan0 next hop: %+v", wan0)
	}
	if wan0.VRFID != 0 || wan0.VRFName != "Default" || wan0.ZoneID != 21 || wan0.ZoneName != "WAN" {
		t.Errorf("wan0 segment/zone: %+v", wan0)
	}
	// The per-interface shaper from the deployment wins over the resolved
	// tunnels view values (11111/22222).
	if wan0.FirewallMode != "stateful-snat" || wan0.MaxBandwidthOutbound != 100000 || wan0.MaxBandwidthInbound != 500000 {
		t.Errorf("wan0 firewall/bandwidth: %+v", wan0)
	}
	if wan0.DHCPConfig != nil {
		t.Errorf("wan0 must have no DHCP config: %+v", wan0.DHCPConfig)
	}

	// Dual-stack WAN: two entries share the name "wan1", one per address
	// family. Look them up by IP because the name is not unique.
	byIP := map[string]ApplianceInterface{}
	wan1Count := 0
	for _, iface := range interfaces {
		byIP[iface.IPAddress] = iface
		if iface.Name == "wan1" {
			wan1Count++
		}
	}
	if wan1Count != 2 {
		t.Errorf("expected 2 wan1 entries (dual-stack), got %d", wan1Count)
	}

	// IPv4 entry: public static address, not behind NAT.
	wan1v4 := byIP["203.0.113.10"]
	if wan1v4.IsPrivate || wan1v4.BehindNAT || wan1v4.Label != "INET2" || wan1v4.CIDR != "203.0.113.10/29" {
		t.Errorf("unexpected wan1 IPv4: %+v", wan1v4)
	}
	if wan1v4.FirewallMode != "stateful" || wan1v4.MaxBandwidthOutbound != 50000 {
		t.Errorf("wan1 IPv4 firewall/bandwidth: %+v", wan1v4)
	}
	if wan1v4.NextHop != "203.0.113.9" || wan1v4.NextHopIsPrivate {
		t.Errorf("wan1 IPv4 next hop: %+v", wan1v4)
	}

	// IPv6 entry: ULA behind NAT. Its IP is absent from the tunnels view
	// (which reports the global address), so matching falls back to the
	// name restricted to the IPv6 family — it must get the IPv6 discovered
	// IP, never the IPv4 one.
	wan1v6 := byIP["fd00:99::2"]
	if !wan1v6.IsPrivate || wan1v6.CIDR != "fd00:99::2/64" {
		t.Errorf("unexpected wan1 IPv6: %+v", wan1v6)
	}
	if !wan1v6.BehindNAT || wan1v6.PublicIP != "2001:db8:f::77" {
		t.Errorf("wan1 IPv6 NAT/public IP: behindNAT=%v publicIP=%q", wan1v6.BehindNAT, wan1v6.PublicIP)
	}
	if wan1v6.NextHop != "fd00:99::1" || !wan1v6.NextHopIsPrivate {
		t.Errorf("wan1 IPv6 next hop: %+v", wan1v6)
	}

	// DHCP WAN: no static IP in the deployment config, current address and
	// discovered public IP come from the tunnels view.
	wan2 := byName["wan2"]
	if wan2.CIDR != "192.168.178.20/24" || !wan2.DHCP || !wan2.IsPrivate {
		t.Errorf("unexpected wan2 addressing: %+v", wan2)
	}
	if !wan2.BehindNAT || wan2.PublicIP != "198.51.100.88" {
		t.Errorf("wan2 NAT/public IP: behindNAT=%v publicIP=%q", wan2.BehindNAT, wan2.PublicIP)
	}
	// No zone assigned; harden 0 = allow-all. The deployment has no
	// per-interface shaper, so the bandwidths fall back to the resolved
	// tunnels view.
	if wan2.ZoneID != 0 || wan2.ZoneName != "" || wan2.FirewallMode != "allow-all" {
		t.Errorf("wan2 zone/firewall: %+v", wan2)
	}
	if wan2.MaxBandwidthOutbound != 95000 || wan2.MaxBandwidthInbound != 250000 {
		t.Errorf("wan2 bandwidth fallback: %+v", wan2)
	}
	// The deployment has no static next hop for DHCP interfaces; the
	// current gateway comes from the resolved tunnels view.
	if wan2.NextHop != "192.168.178.1" || !wan2.NextHopIsPrivate {
		t.Errorf("wan2 next hop fallback: %+v", wan2)
	}

	// LAN VLAN sub-interface in a non-default VRF segment; no firewall mode
	// and no bandwidth limits on LAN interfaces.
	lanVlan := byName["lan0.100"]
	if lanVlan.Type != InterfaceTypeLAN || lanVlan.VLAN != "100" || lanVlan.Label != "Data" || lanVlan.CIDR != "172.16.1.1/24" {
		t.Errorf("unexpected lan0.100: %+v", lanVlan)
	}
	if lanVlan.VRFID != 1 || lanVlan.VRFName != "Guest" || lanVlan.ZoneID != 40 || lanVlan.ZoneName != "GuestLAN" {
		t.Errorf("lan0.100 segment/zone: %+v", lanVlan)
	}
	if lanVlan.FirewallMode != "" || lanVlan.MaxBandwidthOutbound != 0 || lanVlan.MaxBandwidthInbound != 0 {
		t.Errorf("lan0.100 firewall/bandwidth: %+v", lanVlan)
	}
	if lanVlan.NextHop != "" || lanVlan.NextHopIsPrivate {
		t.Errorf("lan0.100 must have no next hop: %+v", lanVlan)
	}

	// DHCP server configuration on lan0.100.
	dhcp := lanVlan.DHCPConfig
	if dhcp == nil || dhcp.Mode != "server" {
		t.Fatalf("lan0.100 DHCP config missing or wrong mode: %+v", dhcp)
	}
	if dhcp.Prefix != "172.16.1.0/24" || dhcp.IPStart != "172.16.1.100" || dhcp.IPEnd != "172.16.1.199" {
		t.Errorf("dhcp addressing: %+v", dhcp)
	}
	if len(dhcp.Ranges) != 1 || dhcp.Ranges[0].Start != "172.16.1.220" || dhcp.Ranges[0].End != "172.16.1.230" {
		t.Errorf("dhcp ranges: %+v", dhcp.Ranges)
	}
	if len(dhcp.Gateways) != 1 || len(dhcp.DNSServers) != 2 || dhcp.DNSServers[1] != "9.9.9.9" ||
		len(dhcp.NTPServers) != 1 || len(dhcp.NetbiosServers) != 1 || dhcp.NetbiosNodeType != "H" {
		t.Errorf("dhcp servers: %+v", dhcp)
	}
	if dhcp.DefaultLease != 86400 || dhcp.MaxLease != 172800 || !dhcp.Failover {
		t.Errorf("dhcp leases/failover: %+v", dhcp)
	}
	if dhcp.Options["43"] != "aruba-ap" || dhcp.Options["150"] != "10.1.1.1" {
		t.Errorf("dhcp options: %+v", dhcp.Options)
	}
	// Reservations sorted by hostname: cam01 before printer01.
	if len(dhcp.Reservations) != 2 || dhcp.Reservations[0].Hostname != "cam01" ||
		dhcp.Reservations[1].IP != "172.16.1.240" || dhcp.Reservations[1].MAC != "aa:bb:cc:dd:ee:01" {
		t.Errorf("dhcp reservations: %+v", dhcp.Reservations)
	}

	// DHCP relay configuration on lan0.200.
	lanRelay := byName["lan0.200"]
	relay := lanRelay.DHCPConfig
	if relay == nil || relay.Mode != "relay" {
		t.Fatalf("lan0.200 DHCP relay config missing: %+v", relay)
	}
	if len(relay.DHCPServers) != 2 || relay.DHCPServers[0] != "10.10.10.10" || !relay.Option82 || relay.Option82Policy != "append" {
		t.Errorf("dhcp relay: %+v", relay)
	}

	loop := byName["loopback100"]
	if loop.Type != InterfaceTypeLoopback || loop.CIDR != "10.255.0.1/32" || loop.Label != "Data" || !loop.IsPrivate {
		t.Errorf("unexpected loopback100: %+v", loop)
	}
	// vrf_id is reported by Orchestrator 9.7.0+ and resolves to the segment
	// name.
	if loop.ZoneID != 20 || loop.ZoneName != "LAN" || loop.VRFID != 1 || loop.VRFName != "Guest" {
		t.Errorf("loopback100 zone/segment: %+v", loop)
	}

	// IPv6 ULA loopback: also flagged as private. It has no vrf_id
	// (pre-9.7 form), so its segment stays unknown.
	loop6 := byName["loopback101"]
	if loop6.Type != InterfaceTypeLoopback || loop6.CIDR != "fd00:10:255::1/128" || !loop6.IsPrivate {
		t.Errorf("unexpected loopback101: %+v", loop6)
	}
	if loop6.VRFID != 0 || loop6.VRFName != "" {
		t.Errorf("loopback101 segment must stay unknown: %+v", loop6)
	}
}

// TestDhcpdWireTolerance verifies that the DHCP configuration parsing
// accepts all wire forms seen in the field: ip_range as documented array,
// as object keyed by range ID (with object or "start-end" string values),
// and string lists as arrays, objects, or single strings.
func TestDhcpdWireTolerance(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{
			name: "array form (API spec)",
			raw: `{"type": "server", "server": {
				"ip_range": [{"start": "10.0.0.10", "end": "10.0.0.20"}, {"start": "10.0.0.30", "end": "10.0.0.40"}],
				"gw": ["10.0.0.1"]
			}}`,
		},
		{
			name: "object form keyed by range ID (appliance reality)",
			raw: `{"type": "server", "server": {
				"ip_range": {"2": {"start": "10.0.0.30", "end": "10.0.0.40"}, "1": {"start": "10.0.0.10", "end": "10.0.0.20"}},
				"gw": {"1": "10.0.0.1"}
			}}`,
		},
		{
			name: "object form with start-end strings",
			raw: `{"type": "server", "server": {
				"ip_range": {"1": "10.0.0.10-10.0.0.20", "2": "10.0.0.30 - 10.0.0.40"},
				"gw": "10.0.0.1"
			}}`,
		},
	}
	for _, c := range cases {
		var d deploymentDhcpd
		if err := json.Unmarshal([]byte(c.raw), &d); err != nil {
			t.Errorf("%s: unmarshal failed: %v", c.name, err)
			continue
		}
		cfg := d.toDHCPConfig()
		if cfg == nil || cfg.Mode != "server" {
			t.Errorf("%s: unexpected config: %+v", c.name, cfg)
			continue
		}
		if len(cfg.Ranges) != 2 ||
			cfg.Ranges[0].Start != "10.0.0.10" || cfg.Ranges[0].End != "10.0.0.20" ||
			cfg.Ranges[1].Start != "10.0.0.30" || cfg.Ranges[1].End != "10.0.0.40" {
			t.Errorf("%s: unexpected ranges: %+v", c.name, cfg.Ranges)
		}
		if len(cfg.Gateways) != 1 || cfg.Gateways[0] != "10.0.0.1" {
			t.Errorf("%s: unexpected gateways: %+v", c.name, cfg.Gateways)
		}
	}
}

// TestTemplateGroupList verifies that both wire forms of a template group
// association are accepted: a plain array of names and the templateIds
// object form.
func TestTemplateGroupList(t *testing.T) {
	var byNePk map[string]templateGroupList
	raw := `{
		"3.NE": ["Branch Baseline", "DNS"],
		"5.NE": {"templateIds": ["Hub Baseline"]},
		"7.NE": null
	}`
	if err := json.Unmarshal([]byte(raw), &byNePk); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(byNePk["3.NE"]) != 2 || byNePk["3.NE"][0] != "Branch Baseline" {
		t.Errorf("array form: %+v", byNePk["3.NE"])
	}
	if len(byNePk["5.NE"]) != 1 || byNePk["5.NE"][0] != "Hub Baseline" {
		t.Errorf("templateIds form: %+v", byNePk["5.NE"])
	}
	if byNePk["7.NE"] != nil {
		t.Errorf("null form: %+v", byNePk["7.NE"])
	}
}

// staticRouteEntries is the shared table content for the static route tests:
// segment 0 with one qualifying route among automatic/learned noise, and
// segment 1 with one qualifying IPv6 route.
const staticRouteSeg0 = `{"count": 4, "max": 60000, "entries": [
	{"state": {"version": 4, "prefix": "10.20.0.0/16", "nextHop": "10.99.1.1", "ifName": "wan0", "metric": 50, "configured": true, "automatic": false, "advert": true, "learned": false, "local": true}},
	{"state": {"version": 4, "prefix": "172.16.1.0/24", "nextHop": "0.0.0.0", "ifName": "lan0", "metric": 0, "configured": false, "automatic": true, "advert": true, "local": true}},
	{"state": {"version": 4, "prefix": "10.50.0.0/16", "nextHop": "172.30.1.99", "ifName": "", "metric": 60, "configured": true, "automatic": true, "advert": false}},
	{"state": {"version": 4, "prefix": "192.0.2.0/24", "nextHop": "10.8.0.1", "metric": 10, "configured": false, "automatic": false, "learned": true, "advert": true}}
]}`

const staticRouteSeg1 = `{"count": 1, "max": 60000, "entries": [
	{"state": {"version": 6, "prefix": "fd00:20::/64", "nextHop": "fd00:99::1", "ifName": "lan0.100", "metric": 30, "configured": true, "automatic": false, "advert": false}}
]}`

// TestExtractStaticRoutes verifies that all known subnets response shapes
// are recognized and that only locally configured, non-automatic routes
// survive filtering with resolved segment names.
func TestExtractStaticRoutes(t *testing.T) {
	shapes := []struct {
		name string
		raw  string
		want int // expected number of qualifying routes
	}{
		{
			name: "API spec shape: subnets wrapper with segment map of GetResponse",
			raw:  `{"subnets": {"0": {"subnets": ` + staticRouteSeg0 + `}, "1": {"subnets": ` + staticRouteSeg1 + `}}}`,
			want: 2,
		},
		{
			name: "segment map without outer wrapper",
			raw:  `{"0": {"subnets": ` + staticRouteSeg0 + `}, "1": {"subnets": ` + staticRouteSeg1 + `}}`,
			want: 2,
		},
		{
			name: "single table (GetResponse, e.g. /subnets fallback)",
			raw:  `{"moduleInfo": {}, "peers": [], "subnets": ` + staticRouteSeg0 + `}`,
			want: 1,
		},
		{
			name: "segment map with bare tables",
			raw:  `{"subnets": {"0": ` + staticRouteSeg0 + `, "1": ` + staticRouteSeg1 + `}}`,
			want: 2,
		},
	}

	for _, shape := range shapes {
		entries := map[string][]subnetEntryState{}
		tables := collectSubnetTables(json.RawMessage(shape.raw), "0", entries)
		if tables == 0 {
			t.Errorf("%s: response shape not recognized", shape.name)
			continue
		}
		routes := extractStaticRoutes(entries, map[int]string{0: "Default", 1: "Guest"})
		if len(routes) != shape.want {
			t.Errorf("%s: expected %d static routes, got %d: %+v", shape.name, shape.want, len(routes), routes)
			continue
		}
		r0 := routes[0]
		if r0.Prefix != "10.20.0.0/16" || r0.NextHop != "10.99.1.1" || r0.Interface != "wan0" ||
			r0.Metric != 50 || r0.VRFID != 0 || r0.VRFName != "Default" || !r0.Advertise {
			t.Errorf("%s: unexpected route 0: %+v", shape.name, r0)
		}
		if shape.want == 2 {
			r1 := routes[1]
			if r1.Prefix != "fd00:20::/64" || r1.VRFID != 1 || r1.VRFName != "Guest" || r1.Advertise {
				t.Errorf("%s: unexpected route 1: %+v", shape.name, r1)
			}
		}
	}

	// An unrecognized shape must report zero tables so the caller can fall
	// back to the other endpoint.
	entries := map[string][]subnetEntryState{}
	if got := collectSubnetTables(json.RawMessage(`{"error": "not found"}`), "0", entries); got != 0 {
		t.Errorf("unrecognized shape reported %d tables", got)
	}
}

// TestPortalLicenseMerge verifies parsing of the portal license response
// (tier from the granted licenses or the license request, boost from any of
// the fx/metered blocks) and the merge with the deployment license block.
func TestPortalLicenseMerge(t *testing.T) {
	granted := `{
		"id": "3.NE", "serial": "SN123", "hostname": "branch01",
		"licenses": {
			"fx": {
				"boost": {"enable": true, "bandwidth": 100000},
				"tier": {"display": "1 Gbps", "bandwidth": 1000000}
			},
			"metered": {"boost": {"enable": false, "bandwidth": 0}}
		}
	}`
	var item portalLicenseItem
	if err := json.Unmarshal([]byte(granted), &item); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if item.Licenses.Fx.Tier.Display != "1 Gbps" || int(item.Licenses.Fx.Tier.Bandwidth) != 1000000 ||
		!bool(item.Licenses.Fx.Boost.Enable) || int(item.Licenses.Fx.Boost.Bandwidth) != 100000 {
		t.Errorf("unexpected granted license: %+v", item.Licenses)
	}

	// Tier only present in the license request (e.g. metered accounts).
	requested := `{
		"id": "5.NE", "serial": "SN999",
		"licenses": {"fx": {"tier": {"display": "", "bandwidth": 0}}, "metered": {"boost": {"enable": true, "bandwidth": 50000}}},
		"licenseRequest": {"fx": {"tier": {"display": "500M", "bandwidth": 500000}}}
	}`
	if err := json.Unmarshal([]byte(requested), &item); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if item.LicenseRequest.Fx.Tier.Display != "500M" || !bool(item.Licenses.Metered.Boost.Enable) {
		t.Errorf("unexpected requested license: %+v", item)
	}

	// The endpoint answers in different shapes across Orchestrator versions:
	// the documented array, an object keyed by nePk, or a wrapped list. All
	// must yield the same items.
	itemJSON := `{"serial": "SN123", "licenses": {"fx": {"tier": {"display": "1 Gbps", "bandwidth": 1000000}}}}`
	shapes := []struct {
		name string
		raw  string
	}{
		{"documented array", `[` + itemJSON + `]`},
		{"object keyed by nePk", `{"3.NE": ` + itemJSON + `}`},
		{"wrapped list", `{"appliances": [` + itemJSON + `]}`},
		{"appliance2 data wrapper with timestamp", `{"data": [` + itemJSON + `], "timestamp": 1735689600000}`},
	}
	for _, shape := range shapes {
		parsed, err := parsePortalLicenseItems([]byte(shape.raw))
		if err != nil {
			t.Errorf("%s: parse failed: %v", shape.name, err)
			continue
		}
		if len(parsed) != 1 || parsed[0].Licenses.Fx.Tier.Display != "1 Gbps" || parsed[0].Serial != "SN123" {
			t.Errorf("%s: unexpected items: %+v", shape.name, parsed)
			continue
		}
		if shape.name == "object keyed by nePk" && parsed[0].ID != "3.NE" {
			t.Errorf("%s: map key not used as ID: %+v", shape.name, parsed[0])
		}
	}

	// Merge behavior: portal fills what the deployment omits, deployment wins
	// where present.
	portal := ApplianceLicense{Tier: "1 Gbps", TierBandwidth: 1000000, Boost: true, BoostBandwidth: 100000}
	merged := mergeLicense(ApplianceLicense{}, portal)
	if merged != portal {
		t.Errorf("merge with empty deployment: %+v", merged)
	}
	merged = mergeLicense(ApplianceLicense{Tier: "500M", TierBandwidth: 500000}, portal)
	if merged.Tier != "500M" || merged.TierBandwidth != 500000 || !merged.Boost || merged.BoostBandwidth != 100000 {
		t.Errorf("merge with deployment tier: %+v", merged)
	}
}

// TestApplianceFilterMatches verifies the AND semantics of the appliance
// filter: include lists, exclude lists, and hostname patterns.
func TestApplianceFilterMatches(t *testing.T) {
	branch := Appliance{NePk: "3.NE", HostName: "br-berlin01", Model: "EC-S-B", Site: "Berlin"}
	hub := Appliance{NePk: "1.NE", HostName: "hub-fra01", Model: "EC-L-B", Site: "Frankfurt"}
	lab := Appliance{NePk: "9.NE", HostName: "lab-test01", Model: "EC-V", Site: ""}

	mustRe := regexp.MustCompile

	cases := []struct {
		name   string
		filter ApplianceFilter
		want   map[string]bool // hostname -> expected match
	}{
		{
			name:   "no filter matches everything",
			filter: ApplianceFilter{},
			want:   map[string]bool{"br-berlin01": true, "hub-fra01": true, "lab-test01": true},
		},
		{
			name:   "exclude models (case-insensitive)",
			filter: ApplianceFilter{ExcludeModels: []string{"ec-v"}},
			want:   map[string]bool{"br-berlin01": true, "hub-fra01": true, "lab-test01": false},
		},
		{
			name:   "include models",
			filter: ApplianceFilter{Models: []string{"EC-S-B", "EC-L-B"}},
			want:   map[string]bool{"br-berlin01": true, "hub-fra01": true, "lab-test01": false},
		},
		{
			name:   "include sites",
			filter: ApplianceFilter{Sites: []string{"berlin"}},
			want:   map[string]bool{"br-berlin01": true, "hub-fra01": false, "lab-test01": false},
		},
		{
			name:   "exclude sites",
			filter: ApplianceFilter{ExcludeSites: []string{"Frankfurt"}},
			want:   map[string]bool{"br-berlin01": true, "hub-fra01": false, "lab-test01": true},
		},
		{
			name:   "hostname regex include",
			filter: ApplianceFilter{HostnameRegex: mustRe("^br-")},
			want:   map[string]bool{"br-berlin01": true, "hub-fra01": false, "lab-test01": false},
		},
		{
			name:   "hostname regex exclude",
			filter: ApplianceFilter{ExcludeHostnameRegex: mustRe("(?i)^(lab|test)-")},
			want:   map[string]bool{"br-berlin01": true, "hub-fra01": true, "lab-test01": false},
		},
		{
			name: "combined criteria are ANDed",
			filter: ApplianceFilter{
				ExcludeModels:        []string{"EC-V"},
				ExcludeHostnameRegex: mustRe("^hub-"),
			},
			want: map[string]bool{"br-berlin01": true, "hub-fra01": false, "lab-test01": false},
		},
		{
			name:   "ne_pk restricts to one appliance",
			filter: ApplianceFilter{NePk: "1.NE"},
			want:   map[string]bool{"br-berlin01": false, "hub-fra01": true, "lab-test01": false},
		},
	}

	for _, c := range cases {
		for _, a := range []Appliance{branch, hub, lab} {
			if got := c.filter.matches(a); got != c.want[a.HostName] {
				t.Errorf("%s: matches(%s) = %v, want %v", c.name, a.HostName, got, c.want[a.HostName])
			}
		}
	}
}

func TestFlexBool(t *testing.T) {
	var payload struct {
		A flexBool `json:"a"`
		B flexBool `json:"b"`
		C flexBool `json:"c"`
		D flexBool `json:"d"`
		E flexBool `json:"e"`
	}
	raw := `{"a": true, "b": "true", "c": "1", "d": 0, "e": null}`
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if !bool(payload.A) || !bool(payload.B) || !bool(payload.C) || bool(payload.D) || bool(payload.E) {
		t.Errorf("flexBool results: %+v", payload)
	}
}

// TestDeploymentLicenceSpelling verifies that both the "licence" and
// "license" spellings of the deployment license block are accepted.
func TestDeploymentLicenceSpelling(t *testing.T) {
	var british deploymentResponse
	if err := json.Unmarshal([]byte(`{"sysConfig": {"licence": {"ecTier": "1 Gbps"}}}`), &british); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if british.licence() == nil || british.licence().EcTier != "1 Gbps" {
		t.Errorf("licence spelling not parsed: %+v", british.licence())
	}
	var american deploymentResponse
	if err := json.Unmarshal([]byte(`{"sysConfig": {"license": {"ecTier": "500M", "ecBoost": "true"}}}`), &american); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if american.licence() == nil || american.licence().EcTier != "500M" || !bool(american.licence().EcBoost) {
		t.Errorf("license spelling not parsed: %+v", american.licence())
	}
}

// TestResolveOverlays verifies overlay ID to name resolution.
func TestResolveOverlays(t *testing.T) {
	refs := resolveOverlays([]string{"1", "3"}, map[string]string{"1": "RealTime", "2": "CriticalApps"})
	if len(refs) != 2 {
		t.Fatalf("expected 2 overlay refs, got %d", len(refs))
	}
	if refs[0].ID != "1" || refs[0].Name != "RealTime" {
		t.Errorf("unexpected ref 0: %+v", refs[0])
	}
	// Unknown overlay IDs keep the ID with an empty name.
	if refs[1].ID != "3" || refs[1].Name != "" {
		t.Errorf("unexpected ref 1: %+v", refs[1])
	}
}

func TestNormalizeNextHop(t *testing.T) {
	cases := map[string]string{
		"10.0.0.1": "10.0.0.1",
		"fd00::1":  "fd00::1",
		"0.0.0.0":  "",
		"::":       "",
		"":         "",
	}
	for in, want := range cases {
		if got := normalizeNextHop(in); got != want {
			t.Errorf("normalizeNextHop(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFirewallModeName(t *testing.T) {
	cases := map[int]string{
		0: "allow-all",
		1: "hardened",
		2: "stateful",
		3: "stateful-snat",
		7: "7", // unknown values pass through as decimal string
	}
	for harden, want := range cases {
		if got := firewallModeName(harden); got != want {
			t.Errorf("firewallModeName(%d) = %q, want %q", harden, got, want)
		}
	}
}

func TestResolveLabel(t *testing.T) {
	wan := map[string]string{"1": "INET1"}
	lan := map[string]string{"10": "Data"}
	if got := resolveLabel("1", wan, lan); got != "INET1" {
		t.Errorf("resolveLabel(1) = %q", got)
	}
	if got := resolveLabel("10", wan, lan); got != "Data" {
		t.Errorf("resolveLabel(10) = %q", got)
	}
	if got := resolveLabel("99", wan, lan); got != "99" {
		t.Errorf("resolveLabel(99) = %q, want raw id", got)
	}
	if got := resolveLabel("0", wan, lan); got != "" {
		t.Errorf("resolveLabel(0) = %q, want empty", got)
	}
	if got := resolveLabel("", wan, lan); got != "" {
		t.Errorf("resolveLabel('') = %q, want empty", got)
	}
}
