package client

// This file implements read-only access to the appliance inventory and
// deployment configuration of the Orchestrator. It combines four endpoints
// into one consolidated view that is consumed by the
// arubasdwan_appliance_deployments data source:
//
//   - GET /gms/rest/appliance
//     Appliance inventory (hostname, serial number, model, site, ...).
//   - GET /gms/rest/deployment?nePk=<nePk>&cached=<bool>
//     Deployment configuration: management interfaces (mgmt0, mgmt1, ...)
//     and datapath interfaces (wan0, lan0, VLAN sub-interfaces, ...).
//   - GET /gms/rest/virtualif/loopback?nePk=<nePk>&cached=<bool>
//     Loopback interface configuration.
//   - GET /gms/rest/tunnelsConfiguration/deployment
//     Orchestrator's resolved view of WAN interfaces, including the
//     discovered public IP address of interfaces behind NAT.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Interface type constants used in ApplianceInterface.Type.
const (
	InterfaceTypeMgmt     = "mgmt"
	InterfaceTypeWAN      = "wan"
	InterfaceTypeLAN      = "lan"
	InterfaceTypeLoopback = "loopback"
	InterfaceTypeOther    = "other"
)

// Appliance represents one appliance managed by the Orchestrator, as returned
// by GET /gms/rest/appliance.
type Appliance struct {
	NePk            string // Orchestrator primary key of the appliance (e.g. "3.NE")
	HostName        string // Appliance hostname
	Serial          string // Hardware serial number
	IP              string // Management IP address the Orchestrator uses to reach the appliance
	Model           string // Appliance model (e.g. "EC-S-B")
	Site            string // Site name the appliance is tagged with
	SoftwareVersion string // ECOS software version
	Mode            string // Deployment mode (e.g. "inline-router")
	NetworkRole     string // Network role as reported by the Orchestrator (e.g. "0" = spoke, "1" = hub)
}

// ApplianceInterface is a unified representation of a single configured IP
// interface of an appliance, regardless of whether it is a management,
// datapath (WAN/LAN), or loopback interface.
type ApplianceInterface struct {
	Name         string // Interface name (e.g. "mgmt0", "wan0", "wan0.100", "lan0", "loopback100")
	Type         string // One of "mgmt", "wan", "lan", "loopback", "other"
	IPAddress    string // Configured IP address (without prefix length)
	PrefixLength int    // Network mask as prefix length (e.g. 24)
	CIDR         string // IPAddress and PrefixLength combined (e.g. "10.1.2.3/24")
	Label        string // Interface label name (e.g. "INET1", "MPLS"); raw label ID if it cannot be resolved
	VLAN         string // VLAN ID for sub-interfaces, empty otherwise
	DHCP         bool   // True if the interface obtains its address dynamically (DHCP/SLAAC)
	BehindNAT    bool   // True if the Orchestrator considers this (WAN) interface to be behind a NAT device
	PublicIP     string // Public IP discovered by the Orchestrator for this (WAN) interface, empty if none
	IsPrivate    bool   // True if IPAddress is private / not globally routable (RFC1918, CGNAT, link-local, ULA, loopback)

	// Deployment settings assigned to the interface.
	VRFID                int    // VRF segment ID the interface is assigned to (0 = Default)
	VRFName              string // Resolved VRF segment name (e.g. "Default"); empty if it cannot be resolved
	ZoneID               int    // Security zone ID assigned to the interface (0 = none)
	ZoneName             string // Resolved security zone name; empty if no zone assigned or it cannot be resolved
	FirewallMode         string // Firewall mode of WAN interfaces: "allow-all", "hardened", "stateful", "stateful-snat"; empty for non-WAN interfaces
	MaxBandwidthOutbound int64  // Maximum outbound (LAN to WAN) bandwidth in Kbps; from the interface shaper, falling back to the Orchestrator's resolved view (0 = not set)
	MaxBandwidthInbound  int64  // Maximum inbound (WAN to LAN) bandwidth in Kbps; from the interface shaper, falling back to the Orchestrator's resolved view (0 = not set)

	// DHCPConfig carries the DHCP server or relay configuration of LAN
	// interfaces; nil when DHCP server/relay is not enabled.
	DHCPConfig *InterfaceDHCPConfig
}

// DHCPRange is one IP allocation range of a DHCP server configuration.
type DHCPRange struct {
	Start string // First IP address of the range
	End   string // Last IP address of the range
}

// DHCPReservation is one static IP reservation of a DHCP server
// configuration.
type DHCPReservation struct {
	Hostname string // Hostname the reservation is keyed by
	IP       string // Reserved IP address
	MAC      string // MAC address of the host
}

// InterfaceDHCPConfig holds the DHCP server or relay configuration of a LAN
// interface. Mode determines which of the field groups is populated.
type InterfaceDHCPConfig struct {
	Mode string // "server" or "relay"

	// Server mode settings.
	Prefix          string            // DHCP subnet in CIDR notation (e.g. "10.3.171.0/24")
	IPStart         string            // First allocatable IP address
	IPEnd           string            // Last allocatable IP address
	Ranges          []DHCPRange       // Additional IP allocation ranges
	Gateways        []string          // Gateway IP addresses handed out to clients
	DNSServers      []string          // DNS server IP addresses handed out to clients
	NTPServers      []string          // NTP server IP addresses handed out to clients
	NetbiosServers  []string          // NetBIOS name server IP addresses
	NetbiosNodeType string            // NetBIOS node type: "B", "P", "M", or "H"
	DefaultLease    int64             // Default lease time in seconds
	MaxLease        int64             // Maximum lease time in seconds
	Options         map[string]string // Additional DHCP options keyed by option ID
	Failover        bool              // True if DHCP failover is enabled
	Reservations    []DHCPReservation // Static IP reservations, sorted by hostname

	// Relay mode settings.
	DHCPServers    []string // Destination DHCP server IP addresses
	Option82       bool     // True if DHCP option 82 is enabled
	Option82Policy string   // Option 82 policy: "append", "replace", "forward", or "discard"
}

// ApplianceLicense holds the EC license of an appliance. Current EdgeConnect
// licensing is tier (throughput) based with Boost as the only add-on.
type ApplianceLicense struct {
	Tier           string // Tier (throughput) license name (e.g. "1 Gbps"); empty if none
	TierBandwidth  int64  // Tier bandwidth as reported, passed through unchanged (0 = not set)
	Boost          bool   // True if the WAN optimization (Boost) add-on is enabled
	BoostBandwidth int64  // Boost bandwidth as reported, passed through unchanged (0 = not set)
}

// OverlayRef identifies a Business Intent Overlay (BIO) an appliance is
// associated with.
type OverlayRef struct {
	ID   string // Numeric overlay ID as reported by the Orchestrator (e.g. "1")
	Name string // Overlay name (e.g. "RealTime"); empty if it cannot be resolved
}

// StaticRoute represents one locally configured (static) route of an
// appliance, taken from the appliance's subnet table (entries flagged as
// configured and not automatic).
type StaticRoute struct {
	Prefix    string // Destination prefix in CIDR notation (e.g. "10.20.0.0/16")
	NextHop   string // Next hop IP address; empty if not applicable
	Interface string // Egress interface name; empty if not applicable
	Metric    int    // Route metric (lower value = higher priority)
	VRFID     int    // VRF segment ID the route belongs to (0 = Default)
	VRFName   string // Resolved VRF segment name; empty if it cannot be resolved
	Advertise bool   // True if the route is advertised to SD-WAN peers
}

// ApplianceDeployment combines the appliance inventory data with all of its
// configured IP interfaces, SD-WAN assignments, and static routes.
type ApplianceDeployment struct {
	Appliance
	RegionID                int              // SD-WAN region ID the appliance belongs to (0 = Default)
	RegionName              string           // Resolved region name; empty if it cannot be resolved
	License                 ApplianceLicense // EC license configured in the deployment
	SystemBandwidthOutbound int64            // System maximum outbound bandwidth in Kbps from the deployment (0 = not set)
	SystemBandwidthInbound  int64            // System maximum inbound bandwidth in Kbps from the deployment (0 = not set)
	TemplateGroups          []string         // Names of the template groups applied to the appliance
	Overlays                []OverlayRef     // Business Intent Overlays the appliance is associated with
	StaticRoutes            []StaticRoute
	Interfaces              []ApplianceInterface
}

// ===========================================================================
// Tolerant JSON scalar types
//
// The Orchestrator API is not always consistent about scalar types: fields
// documented as strings are sometimes returned as numbers (label IDs, VLAN
// IDs) and masks may be returned as either numbers or numeric strings.
// These helper types accept both representations.
// ===========================================================================

// flexString unmarshals a JSON string, number, or null into a Go string.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*f = ""
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = flexString(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err == nil {
		*f = flexString(n.String())
		return nil
	}
	return fmt.Errorf("cannot unmarshal %s into string", string(b))
}

// flexBool unmarshals a JSON bool, "true"/"false"/"1"/"0" string, number,
// or null into a Go bool.
type flexBool bool

func (f *flexBool) UnmarshalJSON(b []byte) error {
	switch string(b) {
	case "null", "false":
		*f = false
		return nil
	case "true":
		*f = true
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = flexBool(strings.EqualFold(s, "true") || s == "1" || strings.EqualFold(s, "yes") || strings.EqualFold(s, "enabled"))
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err == nil {
		v, _ := n.Float64()
		*f = flexBool(v != 0)
		return nil
	}
	return fmt.Errorf("cannot unmarshal %s into bool", string(b))
}

// flexInt unmarshals a JSON number, numeric string, or null into a Go int.
type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*f = 0
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err == nil {
		v, err := n.Float64()
		if err != nil {
			return fmt.Errorf("cannot unmarshal %s into int", string(b))
		}
		*f = flexInt(int(v))
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		if s == "" {
			*f = 0
			return nil
		}
		v, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return fmt.Errorf("cannot unmarshal %q into int", s)
		}
		*f = flexInt(v)
		return nil
	}
	return fmt.Errorf("cannot unmarshal %s into int", string(b))
}

// ===========================================================================
// Private (not globally routable) address detection
// ===========================================================================

// privatePrefixes holds the IPv4 and IPv6 ranges that are not globally
// routable. A WAN interface carrying one of these addresses is reachable
// from the outside only via NAT, which is when the Orchestrator's discovered
// public IP matters.
var privatePrefixes = []netip.Prefix{
	// IPv4
	netip.MustParsePrefix("10.0.0.0/8"),     // RFC1918 private
	netip.MustParsePrefix("172.16.0.0/12"),  // RFC1918 private
	netip.MustParsePrefix("192.168.0.0/16"), // RFC1918 private
	netip.MustParsePrefix("100.64.0.0/10"),  // RFC6598 carrier-grade NAT
	netip.MustParsePrefix("169.254.0.0/16"), // RFC3927 link-local
	netip.MustParsePrefix("127.0.0.0/8"),    // Loopback
	// IPv6
	netip.MustParsePrefix("fc00::/7"),  // RFC4193 unique local addresses (ULA)
	netip.MustParsePrefix("fe80::/10"), // Link-local
	netip.MustParsePrefix("::1/128"),   // Loopback
}

// isPrivateAddress reports whether ip is a private or otherwise not globally
// routable address: RFC1918, carrier-grade NAT (RFC6598), link-local, and
// loopback for IPv4; unique local (RFC4193), link-local, and loopback for
// IPv6. Returns false for invalid addresses.
func isPrivateAddress(ip string) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	// Normalize IPv4-mapped IPv6 addresses (::ffff:10.0.0.1) to plain IPv4
	// so they match the IPv4 prefixes.
	addr = addr.Unmap()
	for _, p := range privatePrefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// ===========================================================================
// GET /gms/rest/appliance — appliance inventory
// ===========================================================================

// applianceListItem is the wire format of one entry in the appliance list.
type applianceListItem struct {
	NePk            string     `json:"nePk"`
	ID              string     `json:"id"`
	HostName        string     `json:"hostName"`
	Serial          string     `json:"serial"`
	IP              string     `json:"IP"`
	Model           string     `json:"model"`
	Site            string     `json:"site"`
	SoftwareVersion string     `json:"softwareVersion"`
	Mode            string     `json:"mode"`
	NetworkRole     flexString `json:"networkRole"`
}

// toAppliance converts the wire format into the public Appliance model.
func (a applianceListItem) toAppliance() Appliance {
	nePk := a.NePk
	if nePk == "" {
		nePk = a.ID
	}
	return Appliance{
		NePk:            nePk,
		HostName:        a.HostName,
		Serial:          a.Serial,
		IP:              a.IP,
		Model:           a.Model,
		Site:            a.Site,
		SoftwareVersion: a.SoftwareVersion,
		Mode:            a.Mode,
		NetworkRole:     string(a.NetworkRole),
	}
}

// GetAppliances retrieves the complete appliance inventory from the
// Orchestrator, sorted by hostname (and nePk as tie-breaker) for
// deterministic ordering.
//
// API endpoint: GET /gms/rest/appliance
func (c *Client) GetAppliances() ([]Appliance, error) {
	respBody, statusCode, err := c.doRequest("GET", "/gms/rest/appliance", nil)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /appliance returned status %d: %s", statusCode, string(respBody))
	}

	// The endpoint returns a JSON array with one entry per appliance.
	var items []applianceListItem
	if err := json.Unmarshal(respBody, &items); err != nil {
		// Some Orchestrator versions return a single object when only one
		// appliance exists or when queried with a nePk filter.
		var single applianceListItem
		if err2 := json.Unmarshal(respBody, &single); err2 != nil {
			return nil, fmt.Errorf("error unmarshaling appliance list: %w", err)
		}
		items = []applianceListItem{single}
	}

	appliances := make([]Appliance, 0, len(items))
	for _, item := range items {
		a := item.toAppliance()
		if a.NePk == "" {
			continue // Skip malformed entries without a primary key.
		}
		appliances = append(appliances, a)
	}

	sort.Slice(appliances, func(i, j int) bool {
		if appliances[i].HostName != appliances[j].HostName {
			return appliances[i].HostName < appliances[j].HostName
		}
		return appliances[i].NePk < appliances[j].NePk
	})

	return appliances, nil
}

// ===========================================================================
// GET /gms/rest/deployment — deployment configuration per appliance
// ===========================================================================

// deploymentMgmtIf is the wire format of one management interface entry in
// the "mgmtIfData" object of the deployment response.
type deploymentMgmtIf struct {
	IP   string  `json:"ip"`
	Mask flexInt `json:"mask"`
	DHCP bool    `json:"dhcp"`
}

// deploymentApplianceIP is the wire format of one IP assignment on a
// datapath interface ("applianceIPs" array of a "modeIfs" entry).
type deploymentApplianceIP struct {
	IP        string     `json:"ip"`
	Mask      flexInt    `json:"mask"`
	Label     flexString `json:"label"`
	LanSide   bool       `json:"lanSide"`
	WanSide   bool       `json:"wanSide"`
	BehindNAT string     `json:"behindNAT"` // "auto" = behind NAT, "none"/"" = not behind NAT
	VLAN      flexString `json:"vlan"`
	Subif     flexString `json:"subif"`
	DHCP      bool       `json:"dhcp"`
	IntfMode  string     `json:"intf_mode"` // static, dhcpv4, dhcpv6, slaac, ...
	Version   flexInt    `json:"version"`   // IP version of this entry: 4 or 6
	VRF       flexInt    `json:"vrf"`       // VRF segment ID (0 = Default)
	Zone      flexInt    `json:"zone"`      // Security zone ID (0 = none)
	Harden    flexInt    `json:"harden"`    // Firewall mode: 0 = allow all, 1 = hardened, 2 = stateful, 3 = stateful+SNAT
	MaxBW     *struct {
		Inbound  flexInt `json:"inbound"`
		Outbound flexInt `json:"outbound"`
	} `json:"maxBW"` // Interface bandwidth limits in Kbps (WAN interfaces only)
	Dhcpd *deploymentDhcpd `json:"dhcpd"` // DHCP server/relay configuration (LAN interfaces only)
}

// sortNumericAware sorts keys numerically where possible (so "2" comes
// before "10"), falling back to string order for non-numeric keys.
func sortNumericAware(keys []string) {
	sort.Slice(keys, func(i, j int) bool {
		a, errA := strconv.Atoi(keys[i])
		b, errB := strconv.Atoi(keys[j])
		if errA == nil && errB == nil {
			return a < b
		}
		return keys[i] < keys[j]
	})
}

// flexStringList unmarshals a string list that the API may return as a JSON
// array, a single string, or an object keyed by index.
type flexStringList []string

func (l *flexStringList) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*l = nil
		return nil
	}
	var arr []string
	if err := json.Unmarshal(b, &arr); err == nil {
		*l = arr
		return nil
	}
	var single string
	if err := json.Unmarshal(b, &single); err == nil {
		if single != "" {
			*l = []string{single}
		}
		return nil
	}
	var byKey map[string]string
	if err := json.Unmarshal(b, &byKey); err != nil {
		return fmt.Errorf("cannot unmarshal string list: %s", string(b))
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sortNumericAware(keys)
	for _, key := range keys {
		if byKey[key] != "" {
			*l = append(*l, byKey[key])
		}
	}
	return nil
}

// dhcpRangeList unmarshals the DHCP IP allocation ranges. The API spec
// documents an array of {start, end} objects, but appliances return an
// object keyed by range ID whose values are either {start, end} objects or
// "start-end" strings; all three forms are accepted.
type dhcpRangeList []DHCPRange

func (l *dhcpRangeList) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*l = nil
		return nil
	}
	type rangeObj struct {
		Start string `json:"start"`
		End   string `json:"end"`
	}
	appendRange := func(r rangeObj) {
		if r.Start != "" || r.End != "" {
			*l = append(*l, DHCPRange{Start: r.Start, End: r.End})
		}
	}
	var arr []rangeObj
	if err := json.Unmarshal(b, &arr); err == nil {
		for _, r := range arr {
			appendRange(r)
		}
		return nil
	}
	var byID map[string]json.RawMessage
	if err := json.Unmarshal(b, &byID); err != nil {
		return fmt.Errorf("cannot unmarshal ip_range: %s", string(b))
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sortNumericAware(ids)
	for _, id := range ids {
		var obj rangeObj
		if err := json.Unmarshal(byID[id], &obj); err == nil {
			appendRange(obj)
			continue
		}
		var s string
		if err := json.Unmarshal(byID[id], &s); err == nil && s != "" {
			r := rangeObj{Start: strings.TrimSpace(s)}
			if idx := strings.Index(s, "-"); idx >= 0 {
				r.Start = strings.TrimSpace(s[:idx])
				r.End = strings.TrimSpace(s[idx+1:])
			}
			appendRange(r)
		}
	}
	return nil
}

// deploymentDhcpd is the wire format of the DHCP server/relay configuration
// of a LAN interface.
type deploymentDhcpd struct {
	Type   string `json:"type"` // "server", "relay", or "none"
	Server *struct {
		Prefix          string                `json:"prefix"`
		IPStart         string                `json:"ipStart"`
		IPEnd           string                `json:"ipEnd"`
		IPRange         dhcpRangeList         `json:"ip_range"`
		GW              flexStringList        `json:"gw"`
		DNS             flexStringList        `json:"dns"`
		NTPD            flexStringList        `json:"ntpd"`
		Netbios         flexStringList        `json:"netbios"`
		NetbiosNodeType string                `json:"netbiosNodeType"`
		DefaultLease    flexInt               `json:"defaultLease"`
		MaxLease        flexInt               `json:"maxLease"`
		Options         map[string]flexString `json:"options"`
		Failover        bool                  `json:"failover"`
		Host            map[string]struct {
			IP  string `json:"ip"`
			MAC string `json:"mac"`
		} `json:"host"`
	} `json:"server"`
	Relay *struct {
		DHCPServer     flexStringList `json:"dhcpserver"`
		Option82       bool           `json:"option82"`
		Option82Policy string         `json:"option82_policy"`
	} `json:"relay"`
}

// toDHCPConfig converts the wire format into the public model. Returns nil
// when neither DHCP server nor relay is enabled on the interface.
func (d *deploymentDhcpd) toDHCPConfig() *InterfaceDHCPConfig {
	if d == nil {
		return nil
	}
	switch d.Type {
	case "server":
		cfg := &InterfaceDHCPConfig{Mode: "server"}
		if s := d.Server; s != nil {
			cfg.Prefix = s.Prefix
			cfg.IPStart = s.IPStart
			cfg.IPEnd = s.IPEnd
			cfg.Ranges = []DHCPRange(s.IPRange)
			cfg.Gateways = []string(s.GW)
			cfg.DNSServers = []string(s.DNS)
			cfg.NTPServers = []string(s.NTPD)
			cfg.NetbiosServers = []string(s.Netbios)
			cfg.NetbiosNodeType = s.NetbiosNodeType
			cfg.DefaultLease = int64(s.DefaultLease)
			cfg.MaxLease = int64(s.MaxLease)
			if len(s.Options) > 0 {
				cfg.Options = make(map[string]string, len(s.Options))
				for id, value := range s.Options {
					cfg.Options[id] = string(value)
				}
			}
			cfg.Failover = s.Failover
			hostnames := make([]string, 0, len(s.Host))
			for hostname := range s.Host {
				hostnames = append(hostnames, hostname)
			}
			sort.Strings(hostnames)
			for _, hostname := range hostnames {
				cfg.Reservations = append(cfg.Reservations, DHCPReservation{
					Hostname: hostname,
					IP:       s.Host[hostname].IP,
					MAC:      s.Host[hostname].MAC,
				})
			}
		}
		return cfg
	case "relay":
		cfg := &InterfaceDHCPConfig{Mode: "relay"}
		if r := d.Relay; r != nil {
			cfg.DHCPServers = []string(r.DHCPServer)
			cfg.Option82 = r.Option82
			cfg.Option82Policy = r.Option82Policy
		}
		return cfg
	default:
		return nil
	}
}

// deploymentModeIf is the wire format of one datapath interface with its IP
// assignments (main IP plus optional VLAN sub-interface IPs).
type deploymentModeIf struct {
	IfName       string                  `json:"ifName"`
	ApplianceIPs []deploymentApplianceIP `json:"applianceIPs"`
}

// deploymentIfLabel is one interface label definition from
// sysConfig.ifLabels of the deployment response.
type deploymentIfLabel struct {
	ID   flexString `json:"id"`
	Name string     `json:"name"`
}

// deploymentLicence is the wire format of the EC license block in the
// deployment configuration.
type deploymentLicence struct {
	EcTier    string   `json:"ecTier"`
	EcTierBW  flexInt  `json:"ecTierBW"`
	EcBoost   flexBool `json:"ecBoost"`
	EcBoostBW flexInt  `json:"ecBoostBW"`
}

// deploymentResponse is the subset of the GET /deployment response that this
// provider consumes.
type deploymentResponse struct {
	MgmtIfData map[string]deploymentMgmtIf `json:"mgmtIfData"`
	ModeIfs    []deploymentModeIf          `json:"modeIfs"`
	SysConfig  struct {
		Mode    string  `json:"mode"`
		MaxBW   flexInt `json:"maxBW"`   // System maximum outbound bandwidth in Kbps
		MaxInBW flexInt `json:"maxInBW"` // System maximum inbound bandwidth in Kbps
		// The license block is documented as "licence" but has appeared
		// under both spellings; both are accepted.
		Licence    *deploymentLicence `json:"licence"`
		LicenceAlt *deploymentLicence `json:"license"`
		IfLabels   struct {
			Wan []deploymentIfLabel `json:"wan"`
			Lan []deploymentIfLabel `json:"lan"`
		} `json:"ifLabels"`
	} `json:"sysConfig"`
}

// licence returns the EC license block of the deployment regardless of the
// spelling used by the Orchestrator; nil when absent.
func (d *deploymentResponse) licence() *deploymentLicence {
	if d.SysConfig.Licence != nil {
		return d.SysConfig.Licence
	}
	return d.SysConfig.LicenceAlt
}

// getDeployment retrieves the deployment configuration of a single appliance.
//
// API endpoint: GET /gms/rest/deployment?nePk=<nePk>&cached=<bool>
//
// With cached=true the data is served from the Orchestrator database; with
// cached=false the Orchestrator queries the appliance directly (slower, and
// it fails for unreachable appliances).
func (c *Client) getDeployment(nePk string, cached bool) (*deploymentResponse, error) {
	path := fmt.Sprintf("/gms/rest/deployment?nePk=%s&cached=%t", url.QueryEscape(nePk), cached)
	respBody, statusCode, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /deployment returned status %d: %s", statusCode, string(respBody))
	}

	var dep deploymentResponse
	if err := json.Unmarshal(respBody, &dep); err != nil {
		return nil, fmt.Errorf("error unmarshaling deployment response: %w", err)
	}
	return &dep, nil
}

// ===========================================================================
// GET /gms/rest/virtualif/loopback — loopback interfaces per appliance
// ===========================================================================

// loopbackDetail is the wire format of one loopback interface entry. The
// response is a JSON object keyed by loopback interface ID.
type loopbackDetail struct {
	Admin  bool       `json:"admin"`
	IPAddr string     `json:"ipaddr"`
	NMask  flexInt    `json:"nmask"`
	Label  flexString `json:"label"`
	Zone   flexInt    `json:"zone"`   // Security zone ID (0 = none)
	VRF    *flexInt   `json:"vrf_id"` // VRF segment ID; reported by Orchestrator 9.7.0 and later, absent before
}

// getLoopbackInterfaces retrieves the loopback interface configuration of a
// single appliance.
//
// API endpoint: GET /gms/rest/virtualif/loopback?nePk=<nePk>&cached=<bool>
func (c *Client) getLoopbackInterfaces(nePk string, cached bool) (map[string]loopbackDetail, error) {
	path := fmt.Sprintf("/gms/rest/virtualif/loopback?nePk=%s&cached=%t", url.QueryEscape(nePk), cached)
	respBody, statusCode, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /virtualif/loopback returned status %d: %s", statusCode, string(respBody))
	}

	var loops map[string]loopbackDetail
	if err := json.Unmarshal(respBody, &loops); err != nil {
		return nil, fmt.Errorf("error unmarshaling loopback response: %w", err)
	}
	return loops, nil
}

// ===========================================================================
// GET /gms/rest/tunnelsConfiguration/deployment — discovered public IPs
// ===========================================================================

// tunnelsInterface is the wire format of one interface entry in the tunnels
// deployment response. publicIpAddress carries the public IP the Orchestrator
// discovered for WAN interfaces behind NAT.
type tunnelsInterface struct {
	Name              string  `json:"name"`
	IPAddress         string  `json:"ipAddress"`
	Mask              flexInt `json:"mask"`
	DHCP              bool    `json:"dhcp"`
	BehindNAT         bool    `json:"behindNAT"`
	PublicIPAddress   string  `json:"publicIpAddress"`
	InboundBandwidth  flexInt `json:"inboundBandwidth"`  // Inbound shaping in Kbps, 0 if not configured
	OutboundBandwidth flexInt `json:"outboundBandwidth"` // Outbound shaping in Kbps, 0 if not configured
}

// tunnelsDeploymentInfo groups the WAN and LAN interface views of one
// appliance in the tunnels deployment response.
type tunnelsDeploymentInfo struct {
	Mode          string             `json:"mode"`
	WanInterfaces []tunnelsInterface `json:"wanInterfaces"`
	LanInterfaces []tunnelsInterface `json:"lanInterfaces"`
}

// getTunnelsDeployments retrieves the Orchestrator's resolved deployment view
// for all appliances in a single call. The response is keyed by nePk.
//
// API endpoint: GET /gms/rest/tunnelsConfiguration/deployment
func (c *Client) getTunnelsDeployments() (map[string]tunnelsDeploymentInfo, error) {
	respBody, statusCode, err := c.doRequest("GET", "/gms/rest/tunnelsConfiguration/deployment", nil)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /tunnelsConfiguration/deployment returned status %d: %s", statusCode, string(respBody))
	}

	var info map[string]tunnelsDeploymentInfo
	if err := json.Unmarshal(respBody, &info); err != nil {
		return nil, fmt.Errorf("error unmarshaling tunnelsConfiguration/deployment response: %w", err)
	}
	return info, nil
}

// ===========================================================================
// GET /gms/rest/gms/interfaceLabels — global interface label names
// ===========================================================================

// interfaceLabelEntry is the wire format of one label definition in the
// global label map (keyed by label ID).
type interfaceLabelEntry struct {
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

// getInterfaceLabelMap retrieves the global interface label definitions of
// the given type ("wan" or "lan") as a map from label ID to label name.
//
// API endpoint: GET /gms/rest/gms/interfaceLabels?type=<type>
func (c *Client) getInterfaceLabelMap(ifType string) (map[string]string, error) {
	path := fmt.Sprintf("/gms/rest/gms/interfaceLabels?type=%s", url.QueryEscape(ifType))
	respBody, statusCode, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /gms/interfaceLabels returned status %d: %s", statusCode, string(respBody))
	}

	var raw map[string]interfaceLabelEntry
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("error unmarshaling interfaceLabels response: %w", err)
	}

	labels := make(map[string]string, len(raw))
	for id, entry := range raw {
		if entry.Name != "" {
			labels[id] = entry.Name
		}
	}
	return labels, nil
}

// ===========================================================================
// GET /gms/rest/regions and /gms/rest/regions/appliances — SD-WAN regions
// ===========================================================================

// regionAssociation is the wire format of one appliance-to-region assignment.
type regionAssociation struct {
	NePk       string  `json:"nePk"`
	RegionID   flexInt `json:"regionId"`
	RegionName string  `json:"regionName"`
}

// getRegionAssociations retrieves the region assignment of all appliances as
// a map keyed by nePk. The endpoint has returned both an array and an object
// keyed by nePk across Orchestrator versions, so both forms are accepted.
//
// API endpoint: GET /gms/rest/regions/appliances
func (c *Client) getRegionAssociations() (map[string]regionAssociation, error) {
	respBody, statusCode, err := c.doRequest("GET", "/gms/rest/regions/appliances", nil)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /regions/appliances returned status %d: %s", statusCode, string(respBody))
	}

	assoc := make(map[string]regionAssociation)
	var list []regionAssociation
	if err := json.Unmarshal(respBody, &list); err == nil {
		for _, a := range list {
			if a.NePk != "" {
				assoc[a.NePk] = a
			}
		}
		return assoc, nil
	}
	var byNePk map[string]regionAssociation
	if err := json.Unmarshal(respBody, &byNePk); err != nil {
		return nil, fmt.Errorf("error unmarshaling regions/appliances response: %w", err)
	}
	for nePk, a := range byNePk {
		if a.NePk == "" {
			a.NePk = nePk
		}
		assoc[a.NePk] = a
	}
	return assoc, nil
}

// getRegionNames retrieves the region definitions as a map from region ID to
// region name. Used to fill in names missing from the association response.
//
// API endpoint: GET /gms/rest/regions
func (c *Client) getRegionNames() (map[int]string, error) {
	respBody, statusCode, err := c.doRequest("GET", "/gms/rest/regions", nil)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /regions returned status %d: %s", statusCode, string(respBody))
	}

	type regionModel struct {
		RegionID   flexInt `json:"regionId"`
		RegionName string  `json:"regionName"`
	}
	names := make(map[int]string)
	var list []regionModel
	if err := json.Unmarshal(respBody, &list); err == nil {
		for _, r := range list {
			names[int(r.RegionID)] = r.RegionName
		}
		return names, nil
	}
	var byID map[string]regionModel
	if err := json.Unmarshal(respBody, &byID); err != nil {
		return nil, fmt.Errorf("error unmarshaling regions response: %w", err)
	}
	for idStr, r := range byID {
		id := int(r.RegionID)
		if id == 0 {
			if parsed, err := strconv.Atoi(idStr); err == nil {
				id = parsed
			}
		}
		names[id] = r.RegionName
	}
	return names, nil
}

// ===========================================================================
// GET /gms/rest/template/applianceAssociation — applied template groups
// ===========================================================================

// templateGroupList accepts both wire forms of a template group association
// value: a plain array of group names, or an object with a templateIds array
// (as used by the single-appliance variant of the endpoint).
type templateGroupList []string

func (t *templateGroupList) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*t = nil
		return nil
	}
	var names []string
	if err := json.Unmarshal(b, &names); err == nil {
		*t = names
		return nil
	}
	var obj struct {
		TemplateIds []string `json:"templateIds"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return fmt.Errorf("cannot unmarshal template group association: %s", string(b))
	}
	*t = obj.TemplateIds
	return nil
}

// getTemplateAssociations retrieves the map of appliance nePk to the names
// of the template groups applied to it.
//
// API endpoint: GET /gms/rest/template/applianceAssociation
func (c *Client) getTemplateAssociations() (map[string][]string, error) {
	respBody, statusCode, err := c.doRequest("GET", "/gms/rest/template/applianceAssociation", nil)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /template/applianceAssociation returned status %d: %s", statusCode, string(respBody))
	}

	var raw map[string]templateGroupList
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("error unmarshaling template association response: %w", err)
	}

	assoc := make(map[string][]string, len(raw))
	for nePk, groups := range raw {
		sorted := append([]string(nil), groups...)
		sort.Strings(sorted)
		assoc[nePk] = sorted
	}
	return assoc, nil
}

// ===========================================================================
// Business Intent Overlays — /gms/rest/gms/overlays/*
// ===========================================================================

// getOverlayAssociations retrieves the overlay membership for all appliances.
// The API returns a map of overlay ID to the nePks in that overlay; this
// method inverts it into a map of nePk to sorted overlay IDs.
//
// API endpoint: GET /gms/rest/gms/overlays/association
func (c *Client) getOverlayAssociations() (map[string][]string, error) {
	respBody, statusCode, err := c.doRequest("GET", "/gms/rest/gms/overlays/association", nil)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /gms/overlays/association returned status %d: %s", statusCode, string(respBody))
	}

	var byOverlay map[string][]string
	if err := json.Unmarshal(respBody, &byOverlay); err != nil {
		return nil, fmt.Errorf("error unmarshaling overlay association response: %w", err)
	}

	byAppliance := make(map[string][]string)
	for overlayID, nePks := range byOverlay {
		for _, nePk := range nePks {
			byAppliance[nePk] = append(byAppliance[nePk], overlayID)
		}
	}
	// Sort overlay IDs numerically where possible for deterministic output.
	for _, ids := range byAppliance {
		sort.Slice(ids, func(i, j int) bool {
			a, errA := strconv.Atoi(ids[i])
			b, errB := strconv.Atoi(ids[j])
			if errA == nil && errB == nil {
				return a < b
			}
			return ids[i] < ids[j]
		})
	}
	return byAppliance, nil
}

// getOverlayNames retrieves the overlay definitions keyed by overlay ID and
// region ID, and returns a map from overlay ID to overlay name. Region "0"
// carries the global overlay configuration and is preferred; any other
// region entry serves as fallback.
//
// API endpoint: GET /gms/rest/gms/overlays/config/regions
func (c *Client) getOverlayNames() (map[string]string, error) {
	respBody, statusCode, err := c.doRequest("GET", "/gms/rest/gms/overlays/config/regions", nil)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /gms/overlays/config/regions returned status %d: %s", statusCode, string(respBody))
	}

	var raw map[string]map[string]struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("error unmarshaling overlay config response: %w", err)
	}

	names := make(map[string]string, len(raw))
	for overlayID, regions := range raw {
		if global, ok := regions["0"]; ok && global.Name != "" {
			names[overlayID] = global.Name
			continue
		}
		regionIDs := make([]string, 0, len(regions))
		for regionID := range regions {
			regionIDs = append(regionIDs, regionID)
		}
		sort.Strings(regionIDs)
		for _, regionID := range regionIDs {
			if regions[regionID].Name != "" {
				names[overlayID] = regions[regionID].Name
				break
			}
		}
	}
	return names, nil
}

// resolveOverlays maps sorted overlay IDs to OverlayRef entries with names.
func resolveOverlays(ids []string, names map[string]string) []OverlayRef {
	refs := make([]OverlayRef, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, OverlayRef{ID: id, Name: names[id]})
	}
	return refs
}

// ===========================================================================
// GET /gms/rest/subnets/all — static routes from the subnet table
// ===========================================================================

// subnetEntryState is the wire format of one route entry in the appliance's
// subnet table. Only the fields needed to report static routes are parsed.
type subnetEntryState struct {
	Prefix     string  `json:"prefix"`
	NextHop    string  `json:"nextHop"`
	IfName     string  `json:"ifName"`
	Metric     flexInt `json:"metric"`
	Configured bool    `json:"configured"` // Locally configured route
	Automatic  bool    `json:"automatic"`  // Automatically added by the system
	Advert     bool    `json:"advert"`     // Advertised to SD-WAN peers
}

// collectSubnetTables recursively walks a subnets response and collects the
// route entries per VRF segment ID. It accepts every response shape seen or
// documented for the subnets endpoints:
//
//   - {"subnets": {"<segId>": {"subnets": {"entries": [...]}, ...}}}  (API spec)
//   - {"<segId>": {"subnets": {"entries": [...]}, ...}}               (no outer wrapper)
//   - {"subnets": {"entries": [...]}, "peers": [...]}                 (single table)
//
// The segID parameter carries the segment context ("0" at the top level).
// The return value is the number of entry tables found; zero means the
// response shape was not recognized.
func collectSubnetTables(raw json.RawMessage, segID string, entries map[string][]subnetEntryState) int {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return 0
	}

	// A table: an object carrying an "entries" array of route states.
	if entriesRaw, ok := obj["entries"]; ok {
		var list []struct {
			State subnetEntryState `json:"state"`
		}
		if err := json.Unmarshal(entriesRaw, &list); err == nil {
			for _, e := range list {
				entries[segID] = append(entries[segID], e.State)
			}
			return 1
		}
		return 0
	}

	// Descend into a "subnets" wrapper without changing the segment context.
	if sub, ok := obj["subnets"]; ok {
		if found := collectSubnetTables(sub, segID, entries); found > 0 {
			return found
		}
	}

	// Otherwise treat numeric keys as VRF segment IDs.
	found := 0
	for key, val := range obj {
		if _, err := strconv.Atoi(key); err == nil {
			found += collectSubnetTables(val, key, entries)
		}
	}
	return found
}

// extractStaticRoutes filters the collected subnet entries of all VRF
// segments down to the locally configured (static) routes: entries flagged
// as configured and not automatically generated by the system.
func extractStaticRoutes(entriesBySegment map[string][]subnetEntryState, segmentNames map[int]string) []StaticRoute {
	var routes []StaticRoute
	for segmentID, states := range entriesBySegment {
		vrfID, err := strconv.Atoi(segmentID)
		if err != nil {
			continue
		}
		for _, state := range states {
			if !state.Configured || state.Automatic || state.Prefix == "" {
				continue
			}
			routes = append(routes, StaticRoute{
				Prefix:    state.Prefix,
				NextHop:   state.NextHop,
				Interface: state.IfName,
				Metric:    int(state.Metric),
				VRFID:     vrfID,
				VRFName:   segmentNames[vrfID],
				Advertise: state.Advert,
			})
		}
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].VRFID != routes[j].VRFID {
			return routes[i].VRFID < routes[j].VRFID
		}
		if routes[i].Prefix != routes[j].Prefix {
			return routes[i].Prefix < routes[j].Prefix
		}
		return routes[i].NextHop < routes[j].NextHop
	})
	return routes
}

// getStaticRoutes retrieves the static routes of a single appliance from its
// subnet table across all VRF segments. It falls back to the single-segment
// endpoint (Default VRF) when the all-VRFs endpoint is unavailable or its
// response shape is not recognized.
//
// API endpoints: GET /gms/rest/subnets/all?nePk=<nePk>&cached=<bool>
//
//	GET /gms/rest/subnets?nePk=<nePk>&cached=<bool>
func (c *Client) getStaticRoutes(nePk string, cached bool, segmentNames map[int]string) ([]StaticRoute, error) {
	path := fmt.Sprintf("/gms/rest/subnets/all?nePk=%s&cached=%t", url.QueryEscape(nePk), cached)
	respBody, statusCode, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	if statusCode == http.StatusOK {
		entries := map[string][]subnetEntryState{}
		if collectSubnetTables(respBody, "0", entries) > 0 {
			return extractStaticRoutes(entries, segmentNames), nil
		}
		// Unrecognized shape: fall through to the single-segment endpoint.
	}

	path = fmt.Sprintf("/gms/rest/subnets?nePk=%s&cached=%t", url.QueryEscape(nePk), cached)
	respBody, statusCode, err = c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /subnets returned status %d: %s", statusCode, string(respBody))
	}
	entries := map[string][]subnetEntryState{}
	if collectSubnetTables(respBody, "0", entries) == 0 {
		snippet := string(respBody)
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		return nil, fmt.Errorf("unrecognized subnets response format: %s", snippet)
	}
	return extractStaticRoutes(entries, segmentNames), nil
}

// ===========================================================================
// GET /gms/rest/license/portal/appliance — assigned EC licenses
// ===========================================================================

// portalLicensePart is one license component in the portal license response.
type portalLicensePart struct {
	Enable    flexBool `json:"enable"`
	Bandwidth flexInt  `json:"bandwidth"`
	Display   string   `json:"display"`
}

// portalLicenseItem is the wire format of one appliance's portal license
// assignment.
type portalLicenseItem struct {
	ID       string `json:"id"`
	Serial   string `json:"serial"`
	Licenses struct {
		Fx struct {
			Boost portalLicensePart `json:"boost"`
			Tier  portalLicensePart `json:"tier"`
		} `json:"fx"`
		Metered struct {
			Boost portalLicensePart `json:"boost"`
		} `json:"metered"`
	} `json:"licenses"`
	// licenseRequest carries the requested/assigned license and serves as
	// fallback when the granted licenses block is empty.
	LicenseRequest struct {
		Fx struct {
			Boost portalLicensePart `json:"boost"`
			Tier  portalLicensePart `json:"tier"`
		} `json:"fx"`
		Metered struct {
			Boost portalLicensePart `json:"boost"`
		} `json:"metered"`
	} `json:"licenseRequest"`
}

// parsePortalLicenseItems accepts every wire form the portal license
// endpoint has been seen to return: the documented array of items, an
// object keyed by appliance (nePk) with item values, and an object wrapping
// item arrays.
func parsePortalLicenseItems(body []byte) ([]portalLicenseItem, error) {
	var items []portalLicenseItem
	if err := json.Unmarshal(body, &items); err == nil {
		return items, nil
	}

	var byKey map[string]json.RawMessage
	if err := json.Unmarshal(body, &byKey); err != nil {
		snippet := string(body)
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		return nil, fmt.Errorf("unrecognized portal license response format: %s", snippet)
	}
	for key, raw := range byKey {
		var list []portalLicenseItem
		if err := json.Unmarshal(raw, &list); err == nil {
			items = append(items, list...)
			continue
		}
		var item portalLicenseItem
		if err := json.Unmarshal(raw, &item); err == nil {
			if item.ID == "" {
				item.ID = key
			}
			items = append(items, item)
		}
	}
	return items, nil
}

// getPortalLicenses retrieves the EC license assignments of all appliances
// from the Orchestrator's cached portal data. The result is keyed by both
// the appliance primary key and the serial number so callers can match
// either way.
//
// API endpoint: GET /gms/rest/license/portal/appliance?cached=true
func (c *Client) getPortalLicenses() (map[string]ApplianceLicense, error) {
	respBody, statusCode, err := c.doRequest("GET", "/gms/rest/license/portal/appliance?cached=true", nil)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /license/portal/appliance returned status %d: %s", statusCode, string(respBody))
	}

	items, err := parsePortalLicenseItems(respBody)
	if err != nil {
		return nil, err
	}
	return portalLicenseMap(items), nil
}

// portalLicenseMap converts portal license items into the lookup map used
// during assembly, keyed by appliance primary key and serial number.
func portalLicenseMap(items []portalLicenseItem) map[string]ApplianceLicense {
	licenses := make(map[string]ApplianceLicense, len(items)*2)
	for _, item := range items {
		license := ApplianceLicense{
			Tier:          item.Licenses.Fx.Tier.Display,
			TierBandwidth: int64(item.Licenses.Fx.Tier.Bandwidth),
		}
		if license.Tier == "" {
			license.Tier = item.LicenseRequest.Fx.Tier.Display
		}
		if license.TierBandwidth == 0 {
			license.TierBandwidth = int64(item.LicenseRequest.Fx.Tier.Bandwidth)
		}
		for _, boost := range []portalLicensePart{
			item.Licenses.Fx.Boost, item.Licenses.Metered.Boost,
			item.LicenseRequest.Fx.Boost, item.LicenseRequest.Metered.Boost,
		} {
			if bool(boost.Enable) {
				license.Boost = true
				if license.BoostBandwidth == 0 {
					license.BoostBandwidth = int64(boost.Bandwidth)
				}
			}
		}
		if item.ID != "" {
			licenses[item.ID] = license
		}
		if item.Serial != "" {
			licenses[item.Serial] = license
		}
	}
	return licenses
}

// mergeLicense fills the fields missing from the deployment's license block
// with the portal license assignment.
func mergeLicense(deployment, portal ApplianceLicense) ApplianceLicense {
	merged := deployment
	if merged.Tier == "" {
		merged.Tier = portal.Tier
	}
	if merged.TierBandwidth == 0 {
		merged.TierBandwidth = portal.TierBandwidth
	}
	merged.Boost = merged.Boost || portal.Boost
	if merged.BoostBandwidth == 0 {
		merged.BoostBandwidth = portal.BoostBandwidth
	}
	return merged
}

// ===========================================================================
// Assembly of the consolidated deployment view
// ===========================================================================

// nameMaps bundles the lookup tables used to resolve numeric IDs from the
// deployment configuration into display names. Any map may be empty when the
// corresponding source could not be fetched; resolution then degrades to raw
// IDs (labels) or empty names (segments, zones).
type nameMaps struct {
	wanLabels map[string]string // WAN interface label ID -> name
	lanLabels map[string]string // LAN interface label ID -> name
	segments  map[int]string    // VRF segment ID -> name
	zones     map[int]string    // Security zone ID -> name (zone IDs are unique across VRFs)
}

// firewallModeName translates the numeric "harden" value of a WAN interface
// into the mode name shown in the Orchestrator UI. Unknown values are
// returned as their decimal string so no information is lost.
func firewallModeName(harden int) string {
	switch harden {
	case 0:
		return "allow-all"
	case 1:
		return "hardened"
	case 2:
		return "stateful"
	case 3:
		return "stateful-snat"
	default:
		return strconv.Itoa(harden)
	}
}

// resolveLabel maps a label ID to its name using the given label maps in
// order. The raw ID is returned unchanged when no map knows the ID. Empty and
// "0" (= no label assigned) resolve to the empty string.
func resolveLabel(id string, maps ...map[string]string) string {
	if id == "" || id == "0" {
		return ""
	}
	for _, m := range maps {
		if name, ok := m[id]; ok && name != "" {
			return name
		}
	}
	return id
}

// datapathInterfaceName builds the effective interface name of an IP
// assignment: the base interface name, extended by the VLAN ID (preferred)
// or sub-interface ID for sub-interfaces (e.g. "wan0.100").
func datapathInterfaceName(ifName string, ip deploymentApplianceIP) string {
	if v := string(ip.VLAN); v != "" && v != "0" {
		return ifName + "." + v
	}
	if s := string(ip.Subif); s != "" && s != "0" {
		return ifName + "." + s
	}
	return ifName
}

// loopbackInterfaceName normalizes a loopback map key to an interface name.
// The Orchestrator keys loopback interfaces by their numeric ID; a purely
// numeric key is prefixed with "loopback" (e.g. "100" -> "loopback100").
// Keys that already look like interface names are used unchanged.
func loopbackInterfaceName(key string) string {
	if key == "" {
		return key
	}
	if _, err := strconv.Atoi(key); err == nil {
		return "loopback" + key
	}
	return key
}

// classifyDatapathInterface determines the interface type of an IP
// assignment from its wanSide/lanSide flags, falling back to the interface
// name prefix for entries where neither flag is set (e.g. bridge interfaces).
func classifyDatapathInterface(ifName string, ip deploymentApplianceIP) string {
	switch {
	case ip.WanSide:
		return InterfaceTypeWAN
	case ip.LanSide:
		return InterfaceTypeLAN
	case strings.HasPrefix(ifName, "wan"):
		return InterfaceTypeWAN
	case strings.HasPrefix(ifName, "lan"):
		return InterfaceTypeLAN
	default:
		return InterfaceTypeOther
	}
}

// buildInterfaces assembles the unified interface list of one appliance from
// the deployment configuration, the loopback configuration, and the tunnels
// deployment view (for discovered public IPs). names provides the lookup
// tables for label, segment, and zone name resolution.
func buildInterfaces(dep *deploymentResponse, loops map[string]loopbackDetail, tunnels tunnelsDeploymentInfo, names nameMaps) []ApplianceInterface {
	var interfaces []ApplianceInterface

	// Management interfaces (mgmt0, mgmt1, ...), sorted by name because the
	// API returns them as a JSON object.
	if dep != nil {
		mgmtNames := make([]string, 0, len(dep.MgmtIfData))
		for name := range dep.MgmtIfData {
			mgmtNames = append(mgmtNames, name)
		}
		sort.Strings(mgmtNames)
		for _, name := range mgmtNames {
			mgmt := dep.MgmtIfData[name]
			if mgmt.IP == "" {
				continue // Interface without an IP address configured.
			}
			interfaces = append(interfaces, ApplianceInterface{
				Name:         name,
				Type:         InterfaceTypeMgmt,
				IPAddress:    mgmt.IP,
				PrefixLength: int(mgmt.Mask),
				CIDR:         fmt.Sprintf("%s/%d", mgmt.IP, int(mgmt.Mask)),
				DHCP:         mgmt.DHCP,
				IsPrivate:    isPrivateAddress(mgmt.IP),
			})
		}
	}

	// Index the tunnels deployment view by interface name and by IP address
	// so datapath interfaces can be matched robustly. Names are not unique:
	// a dual-stack interface has one entry per address family, so the name
	// index keeps all of them and matching selects by family.
	tunnelsByName := make(map[string][]tunnelsInterface)
	tunnelsByIP := make(map[string]tunnelsInterface)
	for _, set := range [][]tunnelsInterface{tunnels.WanInterfaces, tunnels.LanInterfaces} {
		for _, ti := range set {
			if ti.Name != "" {
				tunnelsByName[ti.Name] = append(tunnelsByName[ti.Name], ti)
			}
			if ti.IPAddress != "" {
				tunnelsByIP[ti.IPAddress] = ti
			}
		}
	}

	// matchTunnels finds the tunnels view entry for an interface: by IP
	// address first (unambiguous), then by name restricted to the same
	// address family (so the IPv6 entry of a dual-stack interface never
	// picks up the IPv4 discovered IP, and vice versa), and finally by an
	// address-less entry of the same name (flags only).
	matchTunnels := func(name, ipAddr string, wantV6 bool) (tunnelsInterface, bool) {
		if ipAddr != "" {
			if ti, ok := tunnelsByIP[ipAddr]; ok {
				return ti, true
			}
		}
		for _, ti := range tunnelsByName[name] {
			if ti.IPAddress != "" && strings.Contains(ti.IPAddress, ":") == wantV6 {
				return ti, true
			}
		}
		for _, ti := range tunnelsByName[name] {
			if ti.IPAddress == "" {
				return ti, true
			}
		}
		return tunnelsInterface{}, false
	}

	// Datapath interfaces (wan0, lan0, VLAN sub-interfaces, ...). The API
	// returns these as arrays, so the configured order is preserved.
	if dep != nil {
		for _, modeIf := range dep.ModeIfs {
			for _, ip := range modeIf.ApplianceIPs {
				name := datapathInterfaceName(modeIf.IfName, ip)
				ifType := classifyDatapathInterface(modeIf.IfName, ip)
				dhcp := ip.DHCP || strings.HasPrefix(ip.IntfMode, "dhcp") || ip.IntfMode == "slaac"

				// Match the tunnels deployment entry for this interface.
				// The address family of this entry comes from the configured
				// IP when present, otherwise from the version field (DHCP
				// entries have no static IP).
				wantV6 := strings.Contains(ip.IP, ":") || (ip.IP == "" && int(ip.Version) == 6)
				ti, tiOK := matchTunnels(name, ip.IP, wantV6)

				// DHCP interfaces have no static IP in the deployment
				// configuration; use the current address the Orchestrator
				// sees in the tunnels view instead.
				ipAddr := ip.IP
				mask := int(ip.Mask)
				if ipAddr == "" && tiOK && ti.IPAddress != "" {
					ipAddr = ti.IPAddress
					mask = int(ti.Mask)
				}
				if ipAddr == "" {
					continue // Interface slot without any IP address.
				}

				iface := ApplianceInterface{
					Name:         name,
					Type:         ifType,
					IPAddress:    ipAddr,
					PrefixLength: mask,
					CIDR:         fmt.Sprintf("%s/%d", ipAddr, mask),
					VLAN:         string(ip.VLAN),
					DHCP:         dhcp,
					BehindNAT:    ip.BehindNAT == "auto",
					IsPrivate:    isPrivateAddress(ipAddr),
					VRFID:        int(ip.VRF),
					VRFName:      names.segments[int(ip.VRF)],
					ZoneID:       int(ip.Zone),
				}
				if iface.ZoneID != 0 {
					iface.ZoneName = names.zones[iface.ZoneID]
				}
				if ifType == InterfaceTypeWAN {
					iface.FirewallMode = firewallModeName(int(ip.Harden))
				}
				if ip.MaxBW != nil {
					iface.MaxBandwidthOutbound = int64(ip.MaxBW.Outbound)
					iface.MaxBandwidthInbound = int64(ip.MaxBW.Inbound)
				}
				iface.DHCPConfig = ip.Dhcpd.toDHCPConfig()

				// Resolve the label ID against the type-specific label set
				// first, then the other set as fallback.
				if ifType == InterfaceTypeWAN {
					iface.Label = resolveLabel(string(ip.Label), names.wanLabels, names.lanLabels)
				} else {
					iface.Label = resolveLabel(string(ip.Label), names.lanLabels, names.wanLabels)
				}

				// Enrich with the Orchestrator's discovered public IP
				// (relevant when a WAN interface has a private address
				// behind NAT). The resolved view also carries the effective
				// shaping bandwidths, which fills the gap when the
				// deployment configuration has no per-interface shaper.
				if tiOK {
					iface.DHCP = iface.DHCP || ti.DHCP
					iface.BehindNAT = iface.BehindNAT || ti.BehindNAT
					if ti.PublicIPAddress != "" {
						iface.PublicIP = ti.PublicIPAddress
					}
					if iface.MaxBandwidthOutbound == 0 {
						iface.MaxBandwidthOutbound = int64(ti.OutboundBandwidth)
					}
					if iface.MaxBandwidthInbound == 0 {
						iface.MaxBandwidthInbound = int64(ti.InboundBandwidth)
					}
				}

				interfaces = append(interfaces, iface)
			}
		}
	}

	// Loopback interfaces, sorted by key because the API returns them as a
	// JSON object.
	loopKeys := make([]string, 0, len(loops))
	for key := range loops {
		loopKeys = append(loopKeys, key)
	}
	sort.Strings(loopKeys)
	for _, key := range loopKeys {
		loop := loops[key]
		if loop.IPAddr == "" {
			continue
		}
		iface := ApplianceInterface{
			Name:         loopbackInterfaceName(key),
			Type:         InterfaceTypeLoopback,
			IPAddress:    loop.IPAddr,
			PrefixLength: int(loop.NMask),
			CIDR:         fmt.Sprintf("%s/%d", loop.IPAddr, int(loop.NMask)),
			Label:        resolveLabel(string(loop.Label), names.lanLabels, names.wanLabels),
			IsPrivate:    isPrivateAddress(loop.IPAddr),
			ZoneID:       int(loop.Zone),
		}
		if iface.ZoneID != 0 {
			iface.ZoneName = names.zones[iface.ZoneID]
		}
		// Orchestrator 9.7.0 and later report the loopback's VRF segment;
		// older versions omit the field and the segment stays unknown.
		if loop.VRF != nil {
			iface.VRFID = int(*loop.VRF)
			iface.VRFName = names.segments[iface.VRFID]
		}
		interfaces = append(interfaces, iface)
	}

	return interfaces
}

// ApplianceFilter restricts which appliances GetApplianceDeployments fetches
// and returns. All criteria are combined with AND; zero values impose no
// restriction. Filtering happens before any per-appliance API call, so
// excluded appliances are never queried.
type ApplianceFilter struct {
	NePk                 string         // Exact appliance primary key (e.g. "3.NE")
	Models               []string       // Only these models (case-insensitive exact match)
	ExcludeModels        []string       // Never these models (case-insensitive exact match)
	Sites                []string       // Only these sites (case-insensitive exact match)
	ExcludeSites         []string       // Never these sites (case-insensitive exact match)
	HostnameRegex        *regexp.Regexp // Only hostnames matching this pattern
	ExcludeHostnameRegex *regexp.Regexp // Never hostnames matching this pattern
}

// containsFold reports whether list contains value, compared case-insensitively.
func containsFold(list []string, value string) bool {
	for _, entry := range list {
		if strings.EqualFold(entry, value) {
			return true
		}
	}
	return false
}

// matches reports whether an appliance passes all filter criteria.
func (f ApplianceFilter) matches(a Appliance) bool {
	if f.NePk != "" && a.NePk != f.NePk {
		return false
	}
	if len(f.Models) > 0 && !containsFold(f.Models, a.Model) {
		return false
	}
	if containsFold(f.ExcludeModels, a.Model) {
		return false
	}
	if len(f.Sites) > 0 && !containsFold(f.Sites, a.Site) {
		return false
	}
	if containsFold(f.ExcludeSites, a.Site) {
		return false
	}
	if f.HostnameRegex != nil && !f.HostnameRegex.MatchString(a.HostName) {
		return false
	}
	if f.ExcludeHostnameRegex != nil && f.ExcludeHostnameRegex.MatchString(a.HostName) {
		return false
	}
	return true
}

// GetApplianceDeployments retrieves the consolidated deployment view of the
// appliances passing the filter: inventory data plus all configured
// management, WAN, LAN, and loopback interfaces, with discovered public IPs
// for WAN interfaces behind NAT.
//
// The cached flag is passed through to the per-appliance endpoints: true
// serves data from the Orchestrator database (fast, works for unreachable
// appliances), false queries each appliance directly.
//
// Failures of the appliance inventory or deployment endpoints abort the
// operation with an error. Failures of the auxiliary endpoints (loopback
// interfaces, discovered public IPs, label names) are reported as warnings
// so that a partially degraded Orchestrator does not break reads entirely;
// the affected fields are then empty.
func (c *Client) GetApplianceDeployments(filter ApplianceFilter, cached bool) ([]ApplianceDeployment, []string, error) {
	var warnings []string

	appliances, err := c.GetAppliances()
	if err != nil {
		return nil, nil, fmt.Errorf("error fetching appliance inventory: %w", err)
	}

	filtered := make([]Appliance, 0, len(appliances))
	for _, a := range appliances {
		if filter.matches(a) {
			filtered = append(filtered, a)
		}
	}
	if filter.NePk != "" && len(filtered) == 0 {
		return nil, nil, fmt.Errorf("no appliance with nePk %q found (or it is excluded by the filter)", filter.NePk)
	}
	appliances = filtered

	// One call resolves the discovered public IPs for all appliances.
	tunnels, err := c.getTunnelsDeployments()
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("discovered public IPs unavailable: %v", err))
		tunnels = map[string]tunnelsDeploymentInfo{}
	}

	// Global label maps translate label IDs to names. Best effort: on error
	// the raw label IDs are returned instead.
	wanLabels, err := c.getInterfaceLabelMap("wan")
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("WAN interface label names unavailable: %v", err))
		wanLabels = map[string]string{}
	}
	lanLabels, err := c.getInterfaceLabelMap("lan")
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("LAN interface label names unavailable: %v", err))
		lanLabels = map[string]string{}
	}

	// VRF segment names, resolved once for all appliances. Best effort: on
	// error the interfaces carry only the numeric segment ID.
	segmentNames := map[int]string{}
	if segments, err := c.GetVRFSegments(); err != nil {
		warnings = append(warnings, fmt.Sprintf("VRF segment names unavailable: %v", err))
	} else {
		for _, s := range segments {
			segmentNames[s.ID] = s.Name
		}
	}

	// Security zone names. Zone IDs are unique across VRF segments, so a
	// single ID-to-name map covers all interfaces. The plain zones endpoint
	// (Default VRF) serves as fallback when the VRF mapping endpoint fails.
	zoneNames := map[int]string{}
	mappings, mapErr := c.GetVRFZoneMappings()
	if mapErr == nil {
		for _, m := range mappings {
			zoneNames[m.ZoneID] = m.ZoneName
		}
	}
	zones, zonesErr := c.GetZones()
	if zonesErr == nil {
		for _, z := range zones {
			if _, ok := zoneNames[z.ID]; !ok {
				zoneNames[z.ID] = z.Name
			}
		}
	}
	if mapErr != nil && zonesErr != nil {
		warnings = append(warnings, fmt.Sprintf("security zone names unavailable: %v; %v", mapErr, zonesErr))
	} else if mapErr != nil {
		warnings = append(warnings, fmt.Sprintf("VRF zone mapping unavailable (%v); zone names resolved from the Default VRF zone list only", mapErr))
	}

	// SD-WAN region assignments and definitions. Best effort: on error the
	// appliances report region 0 with an empty name.
	regionAssoc, err := c.getRegionAssociations()
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("region assignments unavailable: %v", err))
		regionAssoc = map[string]regionAssociation{}
	}
	regionNames, err := c.getRegionNames()
	if err != nil {
		if len(regionAssoc) > 0 {
			warnings = append(warnings, fmt.Sprintf("region definitions unavailable (%v); region names taken from the assignment response only", err))
		}
		regionNames = map[int]string{}
	}

	// Applied template groups per appliance.
	templateAssoc, err := c.getTemplateAssociations()
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("template group associations unavailable: %v", err))
		templateAssoc = map[string][]string{}
	}

	// Business Intent Overlay membership and names.
	overlayAssoc, err := c.getOverlayAssociations()
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("overlay associations unavailable: %v", err))
		overlayAssoc = map[string][]string{}
	}
	overlayNames, err := c.getOverlayNames()
	if err != nil {
		if len(overlayAssoc) > 0 {
			warnings = append(warnings, fmt.Sprintf("overlay names unavailable: %v", err))
		}
		overlayNames = map[string]string{}
	}

	// EC license assignments from the portal data; used to fill fields the
	// deployment configuration does not report.
	portalLicenses, err := c.getPortalLicenses()
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("portal license data unavailable: %v", err))
		portalLicenses = map[string]ApplianceLicense{}
	}

	deployments := make([]ApplianceDeployment, 0, len(appliances))
	for _, appliance := range appliances {
		dep, err := c.getDeployment(appliance.NePk, cached)
		if err != nil {
			return nil, nil, fmt.Errorf("error fetching deployment of appliance %s (%s): %w", appliance.HostName, appliance.NePk, err)
		}

		// Merge the per-appliance label definitions over the global ones —
		// they are authoritative for this appliance.
		names := nameMaps{
			wanLabels: mergeLabelMaps(wanLabels, dep.SysConfig.IfLabels.Wan),
			lanLabels: mergeLabelMaps(lanLabels, dep.SysConfig.IfLabels.Lan),
			segments:  segmentNames,
			zones:     zoneNames,
		}

		loops, err := c.getLoopbackInterfaces(appliance.NePk, cached)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("loopback interfaces of appliance %s (%s) unavailable: %v", appliance.HostName, appliance.NePk, err))
			loops = map[string]loopbackDetail{}
		}

		staticRoutes, err := c.getStaticRoutes(appliance.NePk, cached, segmentNames)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("static routes of appliance %s (%s) unavailable: %v", appliance.HostName, appliance.NePk, err))
			staticRoutes = nil
		}

		// Region assignment: the association response carries the region
		// name; fall back to the region definitions when it is missing.
		region := regionAssoc[appliance.NePk]
		regionName := region.RegionName
		if regionName == "" {
			regionName = regionNames[int(region.RegionID)]
		}

		// EC license from the deployment configuration, filled in with the
		// portal license assignment for fields the deployment omits.
		var license ApplianceLicense
		if lic := dep.licence(); lic != nil {
			license = ApplianceLicense{
				Tier:           lic.EcTier,
				TierBandwidth:  int64(lic.EcTierBW),
				Boost:          bool(lic.EcBoost),
				BoostBandwidth: int64(lic.EcBoostBW),
			}
		}
		if portal, ok := portalLicenses[appliance.NePk]; ok {
			license = mergeLicense(license, portal)
		} else if portal, ok := portalLicenses[appliance.Serial]; ok {
			license = mergeLicense(license, portal)
		}

		deployments = append(deployments, ApplianceDeployment{
			Appliance:               appliance,
			RegionID:                int(region.RegionID),
			RegionName:              regionName,
			License:                 license,
			SystemBandwidthOutbound: int64(dep.SysConfig.MaxBW),
			SystemBandwidthInbound:  int64(dep.SysConfig.MaxInBW),
			TemplateGroups:          templateAssoc[appliance.NePk],
			Overlays:                resolveOverlays(overlayAssoc[appliance.NePk], overlayNames),
			StaticRoutes:            staticRoutes,
			Interfaces:              buildInterfaces(dep, loops, tunnels[appliance.NePk], names),
		})
	}

	return deployments, warnings, nil
}

// mergeLabelMaps copies the global label map and overlays the per-appliance
// label definitions from the deployment response.
func mergeLabelMaps(global map[string]string, local []deploymentIfLabel) map[string]string {
	merged := make(map[string]string, len(global)+len(local))
	for id, name := range global {
		merged[id] = name
	}
	for _, l := range local {
		if string(l.ID) != "" && l.Name != "" {
			merged[string(l.ID)] = l.Name
		}
	}
	return merged
}
