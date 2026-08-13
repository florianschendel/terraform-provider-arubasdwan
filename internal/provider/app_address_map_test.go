package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// fakeAddressMapServer serves the address map endpoints: listing via
// GET /gms/rest/applicationDefinition and create/update/delete via
// POST/DELETE /gms/rest/applicationDefinition/ipIntelligenceClassification.
// Like the Orchestrator, it keys entries by their start address and answers
// an empty configuration with {"result": "Not found"}.
type fakeAddressMapServer struct {
	mu      sync.Mutex
	entries map[string]map[string]interface{} // keyed by ip_start
}

func (f *fakeAddressMapServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/gms/rest/applicationDefinition", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if len(f.entries) == 0 {
			_, _ = w.Write([]byte(`{"result": "Not found"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(f.entries)
	})
	mux.HandleFunc("/gms/rest/applicationDefinition/ipIntelligenceClassification", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		start := r.URL.Query().Get("ipStart")
		switch r.Method {
		case http.MethodPost:
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// The Orchestrator assigns the service ID itself.
			body["service_id"] = 700000 + len(f.entries)
			f.entries[start] = body
			_, _ = w.Write([]byte("{}"))
		case http.MethodDelete:
			delete(f.entries, start)
			_, _ = w.Write([]byte("{}"))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	return mux
}

// seed adds an entry as the Orchestrator would report it.
func (f *fakeAddressMapServer) seed(ipStart, ipEnd uint32, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries[strconv.FormatUint(uint64(ipStart), 10)] = map[string]interface{}{
		"ip_start": ipStart, "ip_end": ipEnd, "service_id": 700000, "saas_id": 0,
		"country": "Germany", "country_code": "DE", "org": "Example Org",
		"name": name, "description": "seeded", "priority": 100, "search": "no",
		"subattributes": `{"msinstance":"","mscategory":"","proxy":"0"}`,
	}
}

func addressMapConfig(serverURL, name, ipStart, ipEnd string) string {
	return fmt.Sprintf(`
provider "arubasdwan" {
  orchestrator_url = %q
  api_key          = "test-key"
}

resource "arubasdwan_app_address_map" "test" {
  name     = %q
  ip_start = %q
  ip_end   = %q
}
`, serverURL, name, ipStart, ipEnd)
}

// TestAddressMapCreateAndRead covers the round trip: an entry is created,
// read back, and its computed attributes reflect the Orchestrator's view.
func TestAddressMapCreateAndRead(t *testing.T) {
	fake := &fakeAddressMapServer{entries: map[string]map[string]interface{}{}}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: addressMapConfig(server.URL, "host-a", "10.0.13.72", "10.0.13.72"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("arubasdwan_app_address_map.test", "id", "10.0.13.72"),
					resource.TestCheckResourceAttr("arubasdwan_app_address_map.test", "ip_start", "10.0.13.72"),
					resource.TestCheckResourceAttr("arubasdwan_app_address_map.test", "priority", "100"),
					resource.TestCheckResourceAttrSet("arubasdwan_app_address_map.test", "service_id"),
				),
			},
			{
				Config:   addressMapConfig(server.URL, "host-a", "10.0.13.72", "10.0.13.72"),
				PlanOnly: true,
			},
		},
	})
}

// TestAddressMapRangeAboveSignedLimit guards the 32-bit encoding: addresses
// beyond 2^31 must survive the round trip, which a plain int would truncate
// on 32-bit builds.
func TestAddressMapRangeAboveSignedLimit(t *testing.T) {
	fake := &fakeAddressMapServer{entries: map[string]map[string]interface{}{}}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: addressMapConfig(server.URL, "range-b", "165.225.73.68", "165.225.73.72"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("arubasdwan_app_address_map.test", "id", "165.225.73.68-165.225.73.72"),
					resource.TestCheckResourceAttr("arubasdwan_app_address_map.test", "ip_end", "165.225.73.72"),
				),
			},
		},
	})
}

// TestAddressMapExistingRangeRejected verifies that adopting a range the
// Orchestrator already classifies fails during planning: the create would
// overwrite that definition.
func TestAddressMapExistingRangeRejected(t *testing.T) {
	fake := &fakeAddressMapServer{entries: map[string]map[string]interface{}{}}
	fake.seed(167775560, 167775560, "existing-entry") // 10.0.13.72
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config:      addressMapConfig(server.URL, "host-a", "10.0.13.72", "10.0.13.72"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Address map for this range already exists`),
			},
		},
	})
}

// TestAddressMapDuplicateNameRejected verifies the name check: policies and
// overlay ACLs reference applications by name, so a second entry under the
// same name would be ambiguous.
func TestAddressMapDuplicateNameRejected(t *testing.T) {
	fake := &fakeAddressMapServer{entries: map[string]map[string]interface{}{}}
	fake.seed(167772161, 167772161, "taken-name") // 10.0.0.1
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config:      addressMapConfig(server.URL, "taken-name", "10.0.13.72", "10.0.13.72"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Address map name already in use`),
			},
		},
	})
}

// TestAddressMapInvalidRangeRejected verifies that a range ending before it
// starts is caught while planning rather than by the API.
func TestAddressMapInvalidRangeRejected(t *testing.T) {
	fake := &fakeAddressMapServer{entries: map[string]map[string]interface{}{}}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config:      addressMapConfig(server.URL, "backwards", "10.0.0.9", "10.0.0.1"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Range ends before it starts`),
			},
			{
				Config:      addressMapConfig(server.URL, "v6", "2001:db8::1", "2001:db8::1"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`only support IPv4`),
			},
		},
	})
}

// TestAddressMapImport verifies both import forms: a single address and an
// explicit range. The entries already exist on the Orchestrator, so they
// cannot be created through a configuration first — the range guard rejects
// that on purpose. The imported attributes are therefore checked directly
// instead of via ImportStateVerify, which would need a prior apply.
func TestAddressMapImport(t *testing.T) {
	fake := &fakeAddressMapServer{entries: map[string]map[string]interface{}{}}
	fake.seed(167775560, 167775560, "host-a")  // 10.0.13.72
	fake.seed(167772416, 167772671, "range-c") // 10.0.1.0 - 10.0.1.255
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	checkImported := func(wantID, wantStart, wantEnd, wantName string) resource.ImportStateCheckFunc {
		return func(states []*terraform.InstanceState) error {
			if len(states) != 1 {
				return fmt.Errorf("expected 1 imported state, got %d", len(states))
			}
			attrs := states[0].Attributes
			for attr, want := range map[string]string{
				"id": wantID, "ip_start": wantStart, "ip_end": wantEnd, "name": wantName,
			} {
				if got := attrs[attr]; got != want {
					return fmt.Errorf("%s = %q, want %q", attr, got, want)
				}
			}
			return nil
		}
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config:           addressMapConfig(server.URL, "host-a", "10.0.13.72", "10.0.13.72"),
				ResourceName:     "arubasdwan_app_address_map.test",
				ImportState:      true,
				ImportStateId:    "10.0.13.72",
				ImportStateCheck: checkImported("10.0.13.72", "10.0.13.72", "10.0.13.72", "host-a"),
			},
			{
				Config:           addressMapConfig(server.URL, "range-c", "10.0.1.0", "10.0.1.255"),
				ResourceName:     "arubasdwan_app_address_map.test",
				ImportState:      true,
				ImportStateId:    "10.0.1.0-10.0.1.255",
				ImportStateCheck: checkImported("10.0.1.0-10.0.1.255", "10.0.1.0", "10.0.1.255", "range-c"),
			},
		},
	})
}

// TestAddressMapImportInvalidID verifies that a malformed import ID is
// rejected with guidance on the accepted forms.
func TestAddressMapImportInvalidID(t *testing.T) {
	fake := &fakeAddressMapServer{entries: map[string]map[string]interface{}{}}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config:        addressMapConfig(server.URL, "host-a", "10.0.13.72", "10.0.13.72"),
				ResourceName:  "arubasdwan_app_address_map.test",
				ImportState:   true,
				ImportStateId: "not-an-address",
				ExpectError:   regexp.MustCompile(`Invalid import ID`),
			},
		},
	})
}

// TestAddressMapOverlapWithExistingRejected verifies that a range partially
// overlapping an entry on the Orchestrator is refused while planning. Unlike
// an exact match this cannot be resolved by importing, so the diagnostic
// differs.
func TestAddressMapOverlapWithExistingRejected(t *testing.T) {
	fake := &fakeAddressMapServer{entries: map[string]map[string]interface{}{}}
	fake.seed(167772416, 167772671, "existing-block") // 10.0.1.0 - 10.0.1.255
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				// Reaches into the existing block from below.
				Config:      addressMapConfig(server.URL, "new-range", "10.0.0.128", "10.0.1.10"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Address map overlaps another entry`),
			},
			{
				// A single address inside the existing block.
				Config:      addressMapConfig(server.URL, "new-host", "10.0.1.99", "10.0.1.99"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`overlaps the address map "existing-block"`),
			},
		},
	})
}

// TestAddressMapAdjacentRangesAllowed guards the counter-case: ranges that
// merely touch without sharing an address are legitimate and must apply.
func TestAddressMapAdjacentRangesAllowed(t *testing.T) {
	fake := &fakeAddressMapServer{entries: map[string]map[string]interface{}{}}
	fake.seed(167772416, 167772671, "existing-block") // 10.0.1.0 - 10.0.1.255
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				// Ends one address before the existing block starts.
				Config: addressMapConfig(server.URL, "below-block", "10.0.0.0", "10.0.0.255"),
				Check: resource.TestCheckResourceAttr(
					"arubasdwan_app_address_map.test", "id", "10.0.0.0-10.0.0.255"),
			},
		},
	})
}

// TestAddressMapOverlapWithinConfiguration covers two overlapping entries
// declared in the same configuration. A provider cannot see sibling
// resources while planning, so the first one applies and the second fails
// against the state the Orchestrator has by then — the overlap is caught,
// just during apply rather than plan.
func TestAddressMapOverlapWithinConfiguration(t *testing.T) {
	fake := &fakeAddressMapServer{entries: map[string]map[string]interface{}{}}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	config := fmt.Sprintf(`
provider "arubasdwan" {
  orchestrator_url = %q
  api_key          = "test-key"
}

resource "arubasdwan_app_address_map" "first" {
  name     = "block-one"
  ip_start = "10.0.1.0"
  ip_end   = "10.0.1.255"
}

resource "arubasdwan_app_address_map" "second" {
  name       = "block-two"
  ip_start   = "10.0.1.128"
  ip_end     = "10.0.2.0"
  depends_on = [arubasdwan_app_address_map.first]
}
`, server.URL)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`Address map overlaps another entry`),
			},
		},
	})
}
