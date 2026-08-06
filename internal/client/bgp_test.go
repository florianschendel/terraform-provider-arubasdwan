package client

import (
	"testing"
)

// bgpAllVrfsSystemResponse is the documented allVrfs wire form: system
// configurations keyed by VRF segment ID.
const bgpAllVrfsSystemResponse = `{
	"0": {
		"asn": 65001,
		"enable": true,
		"rtr_id": "10.0.0.1",
		"redist_ospf": false,
		"redist_ospf_filter": 0,
		"graceful_restart_en": true,
		"max_restart_time": 120,
		"stale_path_time": 300,
		"remote_as_path_advertise": false,
		"route_target": {}
	},
	"1": {
		"asn": "4200000001",
		"enable": "true",
		"rtr_id": "10.0.0.2",
		"redist_ospf": true,
		"redist_ospf_filter": "1",
		"graceful_restart_en": false,
		"max_restart_time": 180,
		"stale_path_time": 360,
		"remote_as_path_advertise": true
	}
}`

func TestParseBGPSystemConfigsAllVrfs(t *testing.T) {
	configs, err := parseBGPSystemConfigs([]byte(bgpAllVrfsSystemResponse))
	if err != nil {
		t.Fatalf("parseBGPSystemConfigs returned error: %v", err)
	}
	if len(configs) != 2 {
		t.Fatalf("expected 2 VRF configs, got %d", len(configs))
	}

	def := configs[0]
	if def.ASN != 65001 || !def.Enabled || def.RouterID != "10.0.0.1" {
		t.Errorf("unexpected default VRF config: %+v", def)
	}
	if !def.GracefulRestart || def.MaxRestartTime != 120 || def.StalePathTime != 300 {
		t.Errorf("unexpected default VRF restart settings: %+v", def)
	}
	if def.RedistOSPF || def.RemoteASPathAdvertise {
		t.Errorf("unexpected default VRF redistribution settings: %+v", def)
	}

	// Numeric strings and 4-byte ASNs are tolerated.
	guest := configs[1]
	if guest.ASN != 4200000001 || !guest.Enabled || guest.RouterID != "10.0.0.2" {
		t.Errorf("unexpected VRF 1 config: %+v", guest)
	}
	if !guest.RedistOSPF || guest.RedistOSPFFilter != 1 || !guest.RemoteASPathAdvertise {
		t.Errorf("unexpected VRF 1 redistribution settings: %+v", guest)
	}
}

func TestParseBGPSystemConfigsDefaultVRF(t *testing.T) {
	// Default-VRF form without the VRF wrapper maps to VRF 0.
	body := `{
		"asn": 65010,
		"enable": false,
		"rtr_id": "192.0.2.1",
		"redist_ospf": false,
		"redist_ospf_filter": 0,
		"graceful_restart_en": false,
		"max_restart_time": 120,
		"stale_path_time": 300
	}`
	configs, err := parseBGPSystemConfigs([]byte(body))
	if err != nil {
		t.Fatalf("parseBGPSystemConfigs returned error: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("expected 1 VRF config, got %d", len(configs))
	}
	got := configs[0]
	if got.ASN != 65010 || got.Enabled || got.RouterID != "192.0.2.1" {
		t.Errorf("unexpected config: %+v", got)
	}
}

func TestParseBGPSystemConfigsNotAvailable(t *testing.T) {
	for _, body := range []string{`{"ERROR": "NOT_AVAILABLE"}`, `{}`} {
		configs, err := parseBGPSystemConfigs([]byte(body))
		if err != nil {
			t.Fatalf("parseBGPSystemConfigs(%s) returned error: %v", body, err)
		}
		if len(configs) != 0 {
			t.Errorf("parseBGPSystemConfigs(%s) = %d configs, want 0", body, len(configs))
		}
	}
}

func TestParseBGPSystemConfigsInvalid(t *testing.T) {
	if _, err := parseBGPSystemConfigs([]byte(`"not-bgp"`)); err == nil {
		t.Error("expected error for unrecognized response format")
	}
}

// bgpAllVrfsNeighborResponse is the documented allVrfs wire form: neighbor
// maps keyed by VRF segment ID, neighbors keyed by IP address.
const bgpAllVrfsNeighborResponse = `{
	"0": {
		"10.0.0.10": {
			"self": "10.0.0.10",
			"remote_as": 65001,
			"type": "Branch",
			"enable": true,
			"import_rtes": true,
			"export_map": 511,
			"hold": 180,
			"ka": 60,
			"med": 0,
			"in_med": 0,
			"loc_pref": 100,
			"as_prepend": 0,
			"password": ""
		},
		"10.0.0.2": {
			"self": "10.0.0.2",
			"remote_as": "4200000002",
			"type": "PE-router",
			"enable": "true",
			"import_rtes": false,
			"export_map": 4294967295,
			"hold": "90",
			"ka": 30,
			"med": 10,
			"in_med": 20,
			"loc_pref": 200,
			"as_prepend": 2,
			"next_hop_self": true,
			"directly_connected": true,
			"bfd_desired": true,
			"evpn": false,
			"password": "secret"
		}
	},
	"1": {}
}`

func TestParseBGPNeighborsAllVrfs(t *testing.T) {
	neighbors, err := parseBGPNeighbors([]byte(bgpAllVrfsNeighborResponse))
	if err != nil {
		t.Fatalf("parseBGPNeighbors returned error: %v", err)
	}
	if len(neighbors) != 2 {
		t.Fatalf("expected 2 VRF entries, got %d", len(neighbors))
	}
	if len(neighbors[1]) != 0 {
		t.Errorf("expected no neighbors in VRF 1, got %d", len(neighbors[1]))
	}

	def := neighbors[0]
	if len(def) != 2 {
		t.Fatalf("expected 2 neighbors in VRF 0, got %d", len(def))
	}

	// Neighbors are sorted numerically by IP: 10.0.0.2 before 10.0.0.10.
	first := def[0]
	if first.IP != "10.0.0.2" || first.RemoteAS != 4200000002 || first.Type != "PE-router" {
		t.Errorf("unexpected first neighbor: %+v", first)
	}
	if !first.Enabled || first.ImportRoutes || first.ExportMap != 4294967295 {
		t.Errorf("unexpected first neighbor policies: %+v", first)
	}
	if first.HoldTimer != 90 || first.KeepAliveTimer != 30 || first.MED != 10 || first.InboundMED != 20 {
		t.Errorf("unexpected first neighbor timers/metrics: %+v", first)
	}
	if first.LocalPreference != 200 || first.ASPrependCount != 2 || first.Password != "secret" {
		t.Errorf("unexpected first neighbor attributes: %+v", first)
	}
	if !first.NextHopSelf || !first.DirectlyConnected || !first.BFDEnabled || first.EVPN {
		t.Errorf("unexpected first neighbor flags: %+v", first)
	}

	second := def[1]
	if second.IP != "10.0.0.10" || second.RemoteAS != 65001 || second.Type != "Branch" {
		t.Errorf("unexpected second neighbor: %+v", second)
	}
	if !second.Enabled || !second.ImportRoutes || second.ExportMap != 511 {
		t.Errorf("unexpected second neighbor policies: %+v", second)
	}
}

func TestParseBGPNeighborsDefaultVRF(t *testing.T) {
	// Default-VRF form keyed by neighbor IP maps to VRF 0. The IP falls back
	// to the map key when "self" is missing.
	body := `{
		"192.168.1.1": {
			"remote_as": 65001,
			"type": "Branch",
			"enable": true,
			"import_rtes": true,
			"export_map": 511,
			"hold": 180,
			"ka": 60
		}
	}`
	neighbors, err := parseBGPNeighbors([]byte(body))
	if err != nil {
		t.Fatalf("parseBGPNeighbors returned error: %v", err)
	}
	if len(neighbors) != 1 || len(neighbors[0]) != 1 {
		t.Fatalf("expected 1 neighbor in VRF 0, got %+v", neighbors)
	}
	got := neighbors[0][0]
	if got.IP != "192.168.1.1" || got.RemoteAS != 65001 || !got.Enabled {
		t.Errorf("unexpected neighbor: %+v", got)
	}
}

func TestParseBGPNeighborsNotAvailable(t *testing.T) {
	for _, body := range []string{`{"ERROR": "NOT_AVAILABLE"}`, `{}`} {
		neighbors, err := parseBGPNeighbors([]byte(body))
		if err != nil {
			t.Fatalf("parseBGPNeighbors(%s) returned error: %v", body, err)
		}
		if len(neighbors) != 0 {
			t.Errorf("parseBGPNeighbors(%s) = %d entries, want 0", body, len(neighbors))
		}
	}
}

func TestParseBGPNeighborsInvalid(t *testing.T) {
	if _, err := parseBGPNeighbors([]byte(`[1, 2]`)); err == nil {
		t.Error("expected error for unrecognized response format")
	}
}
