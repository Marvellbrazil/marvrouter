package handlers

import (
	_ "embed"
	"encoding/json"
	"net/http"

	"9router-gateway/db"
)

//go:embed static_models.json
var staticModelsJSON []byte

type StaticModel struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type StaticCatalog struct {
	ProviderModels    map[string][]StaticModel `json:"providerModels"`
	ProviderIDToAlias map[string]string        `json:"providerIdToAlias"`
}

var parsedCatalog StaticCatalog

func init() {
	if len(staticModelsJSON) > 0 {
		_ = json.Unmarshal(staticModelsJSON, &parsedCatalog)
	}
	if parsedCatalog.ProviderModels == nil {
		parsedCatalog.ProviderModels = make(map[string][]StaticModel)
	}
	if parsedCatalog.ProviderIDToAlias == nil {
		parsedCatalog.ProviderIDToAlias = make(map[string]string)
	}
}

type ModelItem struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
}

type ModelsResponse struct {
	Object string      `json:"object"`
	Data   []ModelItem `json:"data"`
}

func Models(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Method not allowed",
		})
		return
	}

	var models []ModelItem
	seen := make(map[string]bool)

	combos, err := db.GetCombos()
	if err == nil {
		for _, c := range combos {
			if c.Name != "" && !seen[c.Name] {
				seen[c.Name] = true
				models = append(models, ModelItem{
					ID:      c.Name,
					Object:  "model",
					OwnedBy: "combo",
				})
			}
		}
	}

	connections, err := db.GetProviderConnections()
	if err != nil || len(connections) == 0 {
		for alias, pModels := range parsedCatalog.ProviderModels {
			for _, m := range pModels {
				modelID := alias + "/" + m.ID
				if !seen[modelID] {
					seen[modelID] = true
					models = append(models, ModelItem{
						ID:      modelID,
						Object:  "model",
						OwnedBy: alias,
					})
				}
			}
		}
	} else {
		activeProviders := make(map[string]bool)
		for _, conn := range connections {
			if conn.IsActive {
				activeProviders[conn.Provider] = true
			}
		}

		activeProviders["opencode"] = true

		for providerID := range activeProviders {
			alias := providerID
			if mapped, ok := parsedCatalog.ProviderIDToAlias[providerID]; ok && mapped != "" {
				alias = mapped
			}

			pModels, ok := parsedCatalog.ProviderModels[alias]
			if !ok {
				pModels = parsedCatalog.ProviderModels[providerID]
			}

			for _, m := range pModels {
				modelID := alias + "/" + m.ID
				if !seen[modelID] {
					seen[modelID] = true
					models = append(models, ModelItem{
						ID:      modelID,
						Object:  "model",
						OwnedBy: alias,
					})
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(ModelsResponse{
		Object: "list",
		Data:   models,
	})
}
