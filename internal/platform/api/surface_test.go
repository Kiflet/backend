package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// The public document has the public operations and the webhooks, the
// components they use and no others, and the public tag's description.
func TestDocument(t *testing.T) {
	spec, err := openapi3.NewLoader().LoadFromFile("../../../api/openapi/v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	public := spec.Tags.Get("public").Description
	b, err := Document(spec, "public")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Info       struct{ Description string }
		Tags       []any
		Paths      map[string]map[string]json.RawMessage
		Webhooks   map[string]any
		Components map[string]map[string]any
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Info.Description != public || doc.Tags != nil {
		t.Errorf("description %.40q, tags %v", doc.Info.Description, doc.Tags)
	}
	if doc.Paths["/shipments"] == nil || doc.Paths["/openapi.json"] == nil || len(doc.Webhooks) == 0 {
		t.Errorf("paths %d, webhooks %d", len(doc.Paths), len(doc.Webhooks))
	}
	for path := range doc.Paths {
		if strings.HasPrefix(path, "/driver/") {
			t.Errorf("a driver operation: %s", path)
		}
	}
	for _, name := range []string{"DriverSignedIn"} {
		if doc.Components["responses"][name] != nil {
			t.Errorf("the driver's %s is in the public document", name)
		}
	}
	if doc.Components["schemas"]["Shipment"] == nil || doc.Components["securitySchemes"]["apiKey"] == nil {
		t.Error("a component the public operations use is missing")
	}
	if len(doc.Components["securitySchemes"]) != 1 {
		t.Errorf("security schemes: %v", doc.Components["securitySchemes"])
	}
}
