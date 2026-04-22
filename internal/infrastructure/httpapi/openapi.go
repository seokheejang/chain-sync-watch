package httpapi

import (
	"fmt"

	"github.com/go-chi/chi/v5"
)

// OpenAPIYAML returns the server's OpenAPI 3.1 document as YAML,
// which is the format CI pipelines and most frontend codegen tools
// prefer for committed spec files.
//
// The function assembles a throwaway chi router with every route
// registered under `deps` so the returned document matches exactly
// what the live server would serve at /openapi.json — no separate
// spec file to keep in sync.
func OpenAPIYAML(deps Deps) ([]byte, error) {
	api := mountAPI(chi.NewRouter(), deps)
	b, err := api.OpenAPI().YAML()
	if err != nil {
		return nil, fmt.Errorf("openapi: marshal yaml: %w", err)
	}
	return b, nil
}

// OpenAPIJSON returns the server's OpenAPI 3.1 document as JSON.
// Same assembly logic as OpenAPIYAML.
func OpenAPIJSON(deps Deps) ([]byte, error) {
	api := mountAPI(chi.NewRouter(), deps)
	b, err := api.OpenAPI().MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("openapi: marshal json: %w", err)
	}
	return b, nil
}
