package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
)

// compoundPositionServer models the Orchestrator's positional ID semantics
// directly: entries live in a priority-ordered slice, the reported ID of an
// entry is its position (index+1), and deleting an entry renumbers everything
// behind it implicitly. A DELETE for a position that does not exist fails,
// as a request addressing a stale slot would.
type compoundPositionServer struct {
	mu    sync.Mutex
	names []string
}

func (f *compoundPositionServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/gms/rest/applicationDefinition", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if len(f.names) == 0 {
			_, _ = w.Write([]byte(`{"result": "Not found"}`))
			return
		}
		out := make(map[string]map[string]interface{}, len(f.names))
		for i, name := range f.names {
			id := strconv.Itoa(i + 1)
			out[id] = map[string]interface{}{
				"id": json.Number(id), "name": name, "description": "",
				"confidence": json.Number("100"), "disabled": false,
				"dst_ip": "192.0.2.0/24",
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/gms/rest/applicationDefinition/compoundClassification", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id, err := strconv.Atoi(r.URL.Query().Get("id"))
		if err != nil || id < 1 || id > len(f.names) {
			http.Error(w, `{"message":"no such id"}`, http.StatusNotFound)
			return
		}
		f.names = append(f.names[:id-1], f.names[id:]...)
		_, _ = w.Write([]byte("{}"))
	})
	return mux
}

// TestDeleteCompoundByNameParallel drives three concurrent deletes against
// the positional fake, the way one terraform apply destroys three compound
// classifications at once. Every delete renumbers the entries behind it, so
// resolving an ID and deleting must be one serialized step: if all three
// resolved before the first delete shifted the list, the later ones would
// address wrong slots and remove bystanders.
func TestDeleteCompoundByNameParallel(t *testing.T) {
	fake := &compoundPositionServer{names: []string{"KeepFirst", "DelA", "DelB", "DelC", "KeepLast"}}
	server := httptest.NewServer(fake.handler())
	defer server.Close()

	c := NewClient(server.URL, "test-key", false)

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for _, name := range []string{"DelA", "DelB", "DelC"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			<-start
			found, err := c.DeleteCompoundClassificationByName(name)
			if err != nil {
				errs <- fmt.Errorf("delete %s: %v", name, err)
				return
			}
			if !found {
				errs <- fmt.Errorf("delete %s: entry not found", name)
			}
		}(name)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.names) != 2 || fake.names[0] != "KeepFirst" || fake.names[1] != "KeepLast" {
		t.Errorf("surviving entries = %v, want [KeepFirst KeepLast]", fake.names)
	}
}
