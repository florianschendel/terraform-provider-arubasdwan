package client

// This file implements read-only access to the VRRP instances configured on
// the appliances. The data is consumed by the arubasdwan_vrrp_instances data
// source and combines two endpoints:
//
//   - GET /gms/rest/appliance
//     Appliance inventory (hostname, serial number, model, site, ...).
//   - GET /gms/rest/vrrp?nePk=<nePk>&cached=<bool>
//     VRRP instances of one appliance: configuration (group ID, interface,
//     virtual IP, priority, timers) and operational state (Master/Backup,
//     current master IP, transitions, uptime).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// VRRPInstance represents one VRRP instance configured on an appliance, as
// returned by GET /gms/rest/vrrp.
type VRRPInstance struct {
	GroupID     int    // VRID shared by the two peers of the group (1-255)
	Interface   string // Interface the instance is peering on (e.g. "lan0")
	VirtualIP   string // Virtual IP address of the group ("vipaddr")
	Priority    int    // VRRP priority (1-254); the higher priority is the master
	Enabled     bool   // True if the instance is administratively up ("enable" == "Up")
	Preempt     bool   // True if the higher-priority peer takes over when it comes back online
	Holddown    int    // Holddown timer in seconds
	AdvTimer    int    // Advertisement interval in seconds ("adv_timer")
	Description string // Description string of the instance
	Auth        string // Authentication string; may be masked by the Orchestrator

	// Operational state of the instance.
	State             string // "Master", "Backup", or "Init" ("mode" in the API)
	MasterIP          string // Interface or local IP address of the current VRRP master
	MasterTransitions int    // Number of Master/Backup transitions
	Uptime            string // Time in the current state (e.g. "0 days 11 hrs 49 mins 41 secs")
	VirtualMAC        string // MAC address the instance is using ("vmac")
	VIPOwner          bool   // True if the appliance owns the virtual IP (always false on EC appliances)
	PacketTrace       bool   // True if VRRP packet tracing is enabled
}

// ApplianceVRRP combines the appliance inventory data with the VRRP
// instances configured on it.
type ApplianceVRRP struct {
	Appliance
	Instances []VRRPInstance // Sorted by interface, group ID, and virtual IP; empty if none are configured
}

// vrrpGetItem is the wire format of one VRRP instance entry. All scalars use
// the tolerant flex types because the Orchestrator is not always consistent
// about scalar types across versions.
type vrrpGetItem struct {
	GroupID           flexInt    `json:"groupId"`
	Interface         flexString `json:"interface"`
	VIPAddr           flexString `json:"vipaddr"`
	Priority          flexInt    `json:"priority"`
	Enable            flexString `json:"enable"` // "Up" or "Down"
	Preempt           flexBool   `json:"preempt"`
	Holddown          flexInt    `json:"holddown"`
	AdvTimer          flexInt    `json:"adv_timer"`
	Desc              flexString `json:"desc"`
	Auth              flexString `json:"auth"`
	Mode              flexString `json:"mode"` // "Master", "Backup", or "Init"
	MasterIP          flexString `json:"masterip"`
	MasterTransitions flexInt    `json:"master_transitions"`
	Uptime            flexString `json:"uptime"`
	VMAC              flexString `json:"vmac"`
	VIPOwner          flexBool   `json:"vipowner"`
	PktTrace          flexBool   `json:"pkt_trace"`
}

// toVRRPInstance converts the wire format into the public model.
func (v vrrpGetItem) toVRRPInstance() VRRPInstance {
	return VRRPInstance{
		GroupID:           int(v.GroupID),
		Interface:         string(v.Interface),
		VirtualIP:         string(v.VIPAddr),
		Priority:          int(v.Priority),
		Enabled:           strings.EqualFold(string(v.Enable), "up"),
		Preempt:           bool(v.Preempt),
		Holddown:          int(v.Holddown),
		AdvTimer:          int(v.AdvTimer),
		Description:       string(v.Desc),
		Auth:              string(v.Auth),
		State:             string(v.Mode),
		MasterIP:          string(v.MasterIP),
		MasterTransitions: int(v.MasterTransitions),
		Uptime:            string(v.Uptime),
		VirtualMAC:        string(v.VMAC),
		VIPOwner:          bool(v.VIPOwner),
		PacketTrace:       bool(v.PktTrace),
	}
}

// parseVRRPInstances accepts every wire form the VRRP endpoint has been seen
// or documented to return: the documented array of instances, an object
// wrapping that array under a "vrrp" key, and an object keyed by instance
// identifier with instance values.
func parseVRRPInstances(body []byte) ([]VRRPInstance, error) {
	var items []vrrpGetItem
	if err := json.Unmarshal(body, &items); err == nil {
		instances := make([]VRRPInstance, 0, len(items))
		for _, item := range items {
			instances = append(instances, item.toVRRPInstance())
		}
		return instances, nil
	}

	var byKey map[string]json.RawMessage
	if err := json.Unmarshal(body, &byKey); err != nil {
		snippet := string(body)
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		return nil, fmt.Errorf("unrecognized VRRP response format: %s", snippet)
	}

	// Descend into a "vrrp" wrapper object.
	if wrapped, ok := byKey["vrrp"]; ok {
		return parseVRRPInstances(wrapped)
	}

	// Object keyed by instance identifier; keys are sorted for deterministic
	// order before the final sort below.
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sortNumericAware(keys)
	var instances []VRRPInstance
	for _, key := range keys {
		var item vrrpGetItem
		if err := json.Unmarshal(byKey[key], &item); err == nil {
			instances = append(instances, item.toVRRPInstance())
		}
	}
	return instances, nil
}

// getVRRPInstances retrieves the VRRP instances of a single appliance.
//
// API endpoint: GET /gms/rest/vrrp?nePk=<nePk>&cached=<bool>
//
// With cached=true the data is served from the Orchestrator database; with
// cached=false the Orchestrator queries the appliance directly (slower, and
// it fails for unreachable appliances).
func (c *Client) getVRRPInstances(nePk string, cached bool) ([]VRRPInstance, error) {
	path := fmt.Sprintf("/gms/rest/vrrp?nePk=%s&cached=%t", url.QueryEscape(nePk), cached)
	respBody, statusCode, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /vrrp returned status %d: %s", statusCode, string(respBody))
	}

	instances, err := parseVRRPInstances(respBody)
	if err != nil {
		return nil, err
	}

	sort.Slice(instances, func(i, j int) bool {
		if instances[i].Interface != instances[j].Interface {
			return instances[i].Interface < instances[j].Interface
		}
		if instances[i].GroupID != instances[j].GroupID {
			return instances[i].GroupID < instances[j].GroupID
		}
		return instances[i].VirtualIP < instances[j].VirtualIP
	})
	return instances, nil
}

// GetApplianceVRRP retrieves the VRRP instances of the appliances passing
// the filter, combined with their inventory data. Appliances without any
// VRRP instance are included with an empty instance list, so the result
// always covers the complete filtered inventory.
//
// The cached flag is passed through to the per-appliance VRRP endpoint:
// true serves data from the Orchestrator database (fast, works for
// unreachable appliances), false queries each appliance directly.
//
// Failures of the appliance inventory or of any per-appliance VRRP read
// abort the operation with an error, so a partial inventory is never
// silently reported as complete.
func (c *Client) GetApplianceVRRP(filter ApplianceFilter, cached bool) ([]ApplianceVRRP, error) {
	appliances, err := c.GetAppliances()
	if err != nil {
		return nil, fmt.Errorf("error fetching appliance inventory: %w", err)
	}

	filtered := make([]Appliance, 0, len(appliances))
	for _, a := range appliances {
		if filter.matches(a) {
			filtered = append(filtered, a)
		}
	}
	if filter.NePk != "" && len(filtered) == 0 {
		return nil, fmt.Errorf("no appliance with nePk %q found (or it is excluded by the filter)", filter.NePk)
	}

	result := make([]ApplianceVRRP, 0, len(filtered))
	for _, appliance := range filtered {
		instances, err := c.getVRRPInstances(appliance.NePk, cached)
		if err != nil {
			return nil, fmt.Errorf("error fetching VRRP instances of appliance %s (%s): %w", appliance.HostName, appliance.NePk, err)
		}
		result = append(result, ApplianceVRRP{
			Appliance: appliance,
			Instances: instances,
		})
	}
	return result, nil
}
