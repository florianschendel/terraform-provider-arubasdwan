package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// fakeOrchestrator serves just enough of the Orchestrator overlay API for the
// overlay ACL resource: GET returns a single overlay whose match object it
// stores verbatim from the last PUT.
type fakeOrchestrator struct {
	mu    sync.Mutex
	match json.RawMessage // the overlay's match object, as sent by the client
}

func (f *fakeOrchestrator) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/gms/rest/gms/overlays/config", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			overlay := map[string]interface{}{
				"name":  "DefaultOverlay",
				"id":    4,
				"match": json.RawMessage(f.match),
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]interface{}{overlay})
		case http.MethodPut:
			var payload map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if match, ok := payload["match"]; ok {
				f.match = match
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	return mux
}

// setEntries replaces the stored ACL with the given raw entries, keyed by
// sequence number — the format the client itself writes. It simulates changes
// made outside Terraform, e.g. a rule added in the Orchestrator UI.
func (f *fakeOrchestrator) setEntries(t *testing.T, entries map[string]map[string]interface{}) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	acl := map[string]interface{}{
		"data": map[string]interface{}{
			"Overlay_DefaultOverlay": map[string]interface{}{"entry": entries},
		},
	}
	encoded, err := json.Marshal(acl)
	if err != nil {
		t.Fatal(err)
	}
	match, err := json.Marshal(map[string]string{"overlayAcl": string(encoded)})
	if err != nil {
		t.Fatal(err)
	}
	f.match = match
}

// entries returns the sequence numbers currently stored on the fake, sorted
// order not guaranteed.
func (f *fakeOrchestrator) sequences(t *testing.T) []string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	var match struct {
		OverlayACL string `json:"overlayAcl"`
	}
	if err := json.Unmarshal(f.match, &match); err != nil || match.OverlayACL == "" {
		return nil
	}
	var acl struct {
		Data map[string]struct {
			Entry map[string]json.RawMessage `json:"entry"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(match.OverlayACL), &acl); err != nil {
		t.Fatalf("stored ACL does not parse: %v", err)
	}
	var out []string
	for _, doc := range acl.Data {
		for seq := range doc.Entry {
			out = append(out, seq)
		}
	}
	return out
}

func protoFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"arubasdwan": providerserver.NewProtocol6WithError(New("test")()),
	}
}

func overlayACLConfig(serverURL, entries string) string {
	return fmt.Sprintf(`
provider "arubasdwan" {
  orchestrator_url = %q
  api_key          = "test-key"
}

resource "arubasdwan_overlay_acl" "test" {
  overlay_name = "DefaultOverlay"
  entries = [
%s
  ]
}
`, serverURL, entries)
}

const entryCatchAll = `
    {
      sequence  = 4999
      match_all = true
      permit    = true
    },`

const entryDeny1000 = `
    {
      sequence = 1000
      permit   = false
      dst_ip   = "192.0.2.1/32"
    },`

const entryPermit1005 = `
    {
      sequence = 1005
      permit   = true
      dst_ip   = "198.51.100.0/24"
    },`

// A syntactically valid entry the provider rejects during apply: match_all
// combined with another criterion. It reproduces an apply that fails after
// the plan was approved.
const entryInvalid1005 = `
    {
      sequence  = 1005
      match_all = true
      dst_ip    = "198.51.100.0/24"
    },`

// TestOverlayACLRemoveOwnEntry replays the reported sequence: create entries,
// have one apply fail after planning, then drop an entry this resource
// created. The removal must plan with a warning, not abort as foreign — and
// that must survive an intervening failed apply.
func TestOverlayACLRemoveOwnEntry(t *testing.T) {
	fake := &fakeOrchestrator{match: json.RawMessage(`{}`)}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: overlayACLConfig(server.URL, entryCatchAll+entryDeny1000+entryPermit1005),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("arubasdwan_overlay_acl.test", "entries.#", "3"),
				),
			},
			{
				// An apply that fails inside the provider after the plan
				// phase, like the reported "syntax problem" apply.
				Config:      overlayACLConfig(server.URL, entryCatchAll+entryDeny1000+entryInvalid1005),
				ExpectError: regexp.MustCompile(`Conflicting match configuration`),
			},
			{
				// Dropping entry 1005, which the first apply created, must
				// not be treated as a foreign entry.
				Config: overlayACLConfig(server.URL, entryCatchAll+entryDeny1000),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("arubasdwan_overlay_acl.test", "entries.#", "2"),
				),
			},
		},
	})
}

// TestOverlayACLRemoveAfterImport covers a rebuilt state: after an import the
// provider has no record of which entries Terraform created, so removing a
// pre-existing entry aborts until allow_entry_removal confirms it. Once a
// converged apply has recorded the configuration, removals warn instead.
func TestOverlayACLRemoveAfterImport(t *testing.T) {
	fake := &fakeOrchestrator{match: json.RawMessage(`{}`)}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	allowRemoval := `
resource "arubasdwan_overlay_acl" "test" {
  overlay_name        = "DefaultOverlay"
  allow_entry_removal = true
  entries = [
` + entryCatchAll + entryDeny1000 + `
  ]
}
`
	// Entries that already exist on the Orchestrator, from applies whose
	// state is gone — the record of who created them is lost.
	fake.setEntries(t, map[string]map[string]interface{}{
		"1000": {"permit": false, "dst_ip": "192.0.2.1/32"},
		"1005": {"permit": true, "dst_ip": "198.51.100.0/24"},
		"4999": {"permit": true},
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				// Import into an empty state, as after terraform state rm.
				Config:             overlayACLConfig(server.URL, entryCatchAll+entryDeny1000+entryPermit1005),
				ResourceName:       "arubasdwan_overlay_acl.test",
				ImportState:        true,
				ImportStateId:      "DefaultOverlay",
				ImportStatePersist: true,
			},
			{
				// Without a record, entry 1005 counts as foreign.
				Config:      overlayACLConfig(server.URL, entryCatchAll+entryDeny1000),
				ExpectError: regexp.MustCompile(`entries that this configuration does not define`),
			},
			{
				// allow_entry_removal confirms the removal.
				Config: fmt.Sprintf(`
provider "arubasdwan" {
  orchestrator_url = %q
  api_key          = "test-key"
}
`, server.URL) + allowRemoval,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("arubasdwan_overlay_acl.test", "entries.#", "2"),
				),
			},
		},
	})
}

// TestOverlayACLForeignEntryAborts verifies the guard still fires for a rule
// that appeared on the Orchestrator without Terraform.
func TestOverlayACLForeignEntryAborts(t *testing.T) {
	fake := &fakeOrchestrator{match: json.RawMessage(`{}`)}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoFactories(),
		Steps: []resource.TestStep{
			{
				Config: overlayACLConfig(server.URL, entryCatchAll+entryDeny1000),
			},
			{
				PreConfig: func() {
					fake.setEntries(t, map[string]map[string]interface{}{
						"1000": {"permit": false, "dst_ip": "192.0.2.1/32"},
						"2000": {"permit": true, "dst_ip": "203.0.113.0/24", "comment": "added in the UI"},
						"4999": {"permit": true},
					})
				},
				Config:      overlayACLConfig(server.URL, entryCatchAll+entryDeny1000),
				ExpectError: regexp.MustCompile(`entries that this configuration does not define`),
			},
		},
	})
}
