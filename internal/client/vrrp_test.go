package client

import (
	"testing"
)

// vrrpArrayResponse is the documented wire form: a JSON array of VRRP_GET
// objects.
const vrrpArrayResponse = `[
	{
		"groupId": 10,
		"interface": "lan0",
		"vipaddr": "10.1.2.254",
		"priority": 200,
		"enable": "Up",
		"preempt": true,
		"holddown": 10,
		"adv_timer": 1,
		"desc": "LAN gateway",
		"auth": "",
		"mode": "Master",
		"masterip": "10.1.2.2",
		"master_transitions": 3,
		"uptime": "0 days 11 hrs 49 mins 41 secs",
		"vmac": "00-00-5E-00-01-0A",
		"vipowner": false,
		"pkt_trace": false
	},
	{
		"groupId": 1,
		"interface": "lan0",
		"vipaddr": "10.1.3.254",
		"priority": "100",
		"enable": "Down",
		"preempt": false,
		"holddown": "10",
		"adv_timer": 1,
		"desc": "",
		"auth": "",
		"mode": "Init",
		"masterip": "",
		"master_transitions": 0,
		"uptime": "",
		"vmac": "",
		"vipowner": false,
		"pkt_trace": false
	}
]`

func TestParseVRRPInstancesArray(t *testing.T) {
	instances, err := parseVRRPInstances([]byte(vrrpArrayResponse))
	if err != nil {
		t.Fatalf("parseVRRPInstances returned error: %v", err)
	}
	if len(instances) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(instances))
	}

	first := instances[0]
	if first.GroupID != 10 || first.Interface != "lan0" || first.VirtualIP != "10.1.2.254" {
		t.Errorf("unexpected first instance: %+v", first)
	}
	if first.Priority != 200 || !first.Enabled || !first.Preempt {
		t.Errorf("unexpected first instance settings: %+v", first)
	}
	if first.State != "Master" || first.MasterIP != "10.1.2.2" || first.MasterTransitions != 3 {
		t.Errorf("unexpected first instance state: %+v", first)
	}
	if first.Description != "LAN gateway" || first.VirtualMAC != "00-00-5E-00-01-0A" {
		t.Errorf("unexpected first instance details: %+v", first)
	}

	// Numeric strings ("priority": "100") are tolerated and "Down" maps to
	// Enabled == false.
	second := instances[1]
	if second.GroupID != 1 || second.Priority != 100 || second.Holddown != 10 {
		t.Errorf("unexpected second instance: %+v", second)
	}
	if second.Enabled || second.Preempt {
		t.Errorf("expected second instance to be disabled without preempt: %+v", second)
	}
}

func TestParseVRRPInstancesWrappedAndKeyed(t *testing.T) {
	// Object wrapping the instances under a "vrrp" key, with the instances
	// keyed by an identifier instead of an array.
	body := `{
		"vrrp": {
			"lan0:10": {
				"groupId": 10,
				"interface": "lan0",
				"vipaddr": "10.1.2.254",
				"priority": 200,
				"enable": "up",
				"preempt": true,
				"holddown": 10,
				"adv_timer": 1,
				"mode": "Backup",
				"masterip": "10.1.2.3",
				"master_transitions": 1,
				"vipowner": false,
				"pkt_trace": false
			}
		}
	}`
	instances, err := parseVRRPInstances([]byte(body))
	if err != nil {
		t.Fatalf("parseVRRPInstances returned error: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}
	got := instances[0]
	if got.GroupID != 10 || got.VirtualIP != "10.1.2.254" || got.State != "Backup" {
		t.Errorf("unexpected instance: %+v", got)
	}
	if !got.Enabled {
		t.Errorf("expected lowercase \"up\" to map to Enabled == true: %+v", got)
	}
}

func TestParseVRRPInstancesEmpty(t *testing.T) {
	for _, body := range []string{`[]`, `{}`} {
		instances, err := parseVRRPInstances([]byte(body))
		if err != nil {
			t.Fatalf("parseVRRPInstances(%s) returned error: %v", body, err)
		}
		if len(instances) != 0 {
			t.Errorf("parseVRRPInstances(%s) = %d instances, want 0", body, len(instances))
		}
	}
}

func TestParseVRRPInstancesInvalid(t *testing.T) {
	if _, err := parseVRRPInstances([]byte(`"not-vrrp"`)); err == nil {
		t.Error("expected error for unrecognized response format")
	}
}
