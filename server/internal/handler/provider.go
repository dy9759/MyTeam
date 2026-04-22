package handler

import (
	"encoding/json"
	"net/http"

	"github.com/MyAIOSHub/MyTeam/server/pkg/provider"
)

// ProviderHandler exposes the static Provider registry.
type ProviderHandler struct{}

func NewProviderHandler() *ProviderHandler {
	return &ProviderHandler{}
}

// List returns the registered provider catalog.
func (h *ProviderHandler) List(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(provider.List()); err != nil {
		http.Error(w, "encode provider list", http.StatusInternalServerError)
	}
}
