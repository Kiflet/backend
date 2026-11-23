package api

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// Surface leaves in spec only the operations with one of tags, and the
// paths that still have one. scripts/api-breaking.sh compares the public
// and driver operations, which may not break, on their own.
func Surface(spec *openapi3.T, tags []string) {
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			if !slices.ContainsFunc(op.Tags, func(t string) bool { return slices.Contains(tags, t) }) {
				item.SetOperation(method, nil)
			}
		}
		if len(item.Operations()) == 0 {
			spec.Paths.Delete(path)
		}
	}
}

// Document is the published description of the operations tagged tag:
// for public, the public API's openapi.json (docs/architecture.md,
// section 11, "Who may call what": the published documentation shows only
// those). It has the operations with that tag, the webhooks, and only the
// components they use; its description is the tag's. make api writes it
// from api/openapi/v1.yaml.
func Document(spec *openapi3.T, tag string) ([]byte, error) {
	Surface(spec, []string{tag})
	t := spec.Tags.Get(tag)
	if t == nil {
		return nil, errors.New("no tag " + tag)
	}
	spec.Info.Description = t.Description
	spec.Tags = nil
	for _, item := range spec.Paths.Map() {
		for _, op := range item.Operations() {
			op.Tags = nil
		}
	}
	if err := prune(spec); err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// prune leaves out the components that nothing left in spec uses, directly
// or through another component.
func prune(spec *openapi3.T) error {
	raw, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}
	components, _ := doc["components"].(map[string]any)
	delete(doc, "components")

	// What is used: every $ref from outside the components, then from the
	// components those name, until there are no more.
	used := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if ref, ok := v["$ref"].(string); ok {
				name, ok := strings.CutPrefix(ref, "#/components/")
				if ok && !used[name] {
					used[name] = true
					kind, key, _ := strings.Cut(name, "/")
					if group, ok := components[kind].(map[string]any); ok {
						walk(group[key])
					}
				}
			}
			for _, x := range v {
				walk(x)
			}
		case []any:
			for _, x := range v {
				walk(x)
			}
		}
	}
	walk(doc)
	// Security schemes are named by requirements, not referenced.
	var requirements []openapi3.SecurityRequirements
	requirements = append(requirements, spec.Security)
	for _, item := range spec.Paths.Map() {
		for _, op := range item.Operations() {
			if op.Security != nil {
				requirements = append(requirements, *op.Security)
			}
		}
	}
	for _, reqs := range requirements {
		for _, req := range reqs {
			for name := range req {
				used["securitySchemes/"+name] = true
			}
		}
	}

	c := spec.Components
	if c == nil {
		return nil
	}
	keep := func(kind string, names []string, drop func(string)) {
		for _, n := range names {
			if !used[kind+"/"+n] {
				drop(n)
			}
		}
	}
	keep("schemas", keys(c.Schemas), func(n string) { delete(c.Schemas, n) })
	keep("parameters", keys(c.Parameters), func(n string) { delete(c.Parameters, n) })
	keep("headers", keys(c.Headers), func(n string) { delete(c.Headers, n) })
	keep("requestBodies", keys(c.RequestBodies), func(n string) { delete(c.RequestBodies, n) })
	keep("responses", keys(c.Responses), func(n string) { delete(c.Responses, n) })
	keep("securitySchemes", keys(c.SecuritySchemes), func(n string) { delete(c.SecuritySchemes, n) })
	keep("examples", keys(c.Examples), func(n string) { delete(c.Examples, n) })
	keep("links", keys(c.Links), func(n string) { delete(c.Links, n) })
	keep("callbacks", keys(c.Callbacks), func(n string) { delete(c.Callbacks, n) })
	return nil
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
