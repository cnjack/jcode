package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPairingEndpoints(t *testing.T) {
	pending := Pairing{ID: "p1", Label: "chrome on mac", PubKey: "cHVia2V5", Status: "pending", CreatedAt: "2026-07-20T10:00:00Z"}
	var gotRespond struct {
		called  bool
		Approve bool     `json:"approve"`
		Wrap    *CEKWrap `json:"wrap"`
	}
	setRespond := func(approve bool, wrap *CEKWrap) {
		gotRespond.called = true
		gotRespond.Approve = approve
		gotRespond.Wrap = wrap
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/v1/device/pairings", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("pairings: Authorization = %q", r.Header.Get("Authorization"))
		}
		if r.URL.Query().Get("status") != "pending" {
			t.Errorf("pairings: status query = %q, want pending", r.URL.Query().Get("status"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"pairings": []Pairing{pending}})
	})
	mux.HandleFunc("GET /internal/v1/device/pairings/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") != "p1" {
			t.Errorf("get pairing id = %q, want p1", r.PathValue("id"))
		}
		_ = json.NewEncoder(w).Encode(pending)
	})
	mux.HandleFunc("POST /internal/v1/device/pairings/{id}/respond", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Approve bool     `json:"approve"`
			Wrap    *CEKWrap `json:"wrap"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("respond: decode body: %v", err)
		}
		setRespond(body.Approve, body.Wrap)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := NewClient(srv.URL)
	ctx := context.Background()

	list, err := client.ListPairings(ctx, "tok", "pending")
	if err != nil {
		t.Fatalf("ListPairings: %v", err)
	}
	if len(list) != 1 || list[0].ID != "p1" || list[0].PubKey != "cHVia2V5" {
		t.Fatalf("ListPairings = %+v", list)
	}

	got, err := client.GetPairing(ctx, "tok", "p1")
	if err != nil {
		t.Fatalf("GetPairing: %v", err)
	}
	if got.Label != "chrome on mac" || got.CreatedAt == "" {
		t.Fatalf("GetPairing = %+v", got)
	}

	wrap := &CEKWrap{EphemeralPubKey: "ZXBo", Nonce: "bm9uY2U=", CT: "Y3Q="}
	if err := client.RespondPairing(ctx, "tok", "p1", true, wrap); err != nil {
		t.Fatalf("RespondPairing approve: %v", err)
	}
	if !gotRespond.called || !gotRespond.Approve || gotRespond.Wrap == nil || gotRespond.Wrap.EphemeralPubKey != "ZXBo" {
		t.Fatalf("approve respond body = %+v", gotRespond)
	}

	if err := client.RespondPairing(ctx, "tok", "p1", false, nil); err != nil {
		t.Fatalf("RespondPairing deny: %v", err)
	}
	if gotRespond.Approve || gotRespond.Wrap != nil {
		t.Fatalf("deny respond body = %+v, want approve=false and no wrap", gotRespond)
	}
}
