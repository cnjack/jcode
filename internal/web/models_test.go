package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebSwitchModelSameValueIsNoOp(t *testing.T) {
	s := &Server{
		Engine:   &Engine{providerName: "openai", modelName: "gpt-5"},
		wsBroker: NewWSBroker(),
	}
	rec := httptest.NewRecorder()
	s.handleSwitchModel(rec, httptest.NewRequest(http.MethodPost, "/api/model",
		strings.NewReader(`{"provider":"openai","model":"gpt-5"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("same model: code=%d body=%q", rec.Code, rec.Body.String())
	}
}
