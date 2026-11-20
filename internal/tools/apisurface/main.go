// Command apisurface writes the part of the API's specification that has
// the given tags, as JSON: the operations tagged otherwise are left out,
// with the paths left empty by it. scripts/api-breaking.sh compares the
// public and driver operations, which may not break, on their own.
//
//	go run ./internal/tools/apisurface -tags public,driver api/openapi/v1.yaml
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

func main() {
	tags := flag.String("tags", "public,driver", "the tags to keep, separated by commas")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: apisurface -tags public,driver specification.yaml")
		os.Exit(2)
	}
	spec, err := openapi3.NewLoader().LoadFromFile(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	keep := strings.Split(*tags, ",")
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			if !slices.ContainsFunc(op.Tags, func(t string) bool { return slices.Contains(keep, t) }) {
				item.SetOperation(method, nil)
			}
		}
		if len(item.Operations()) == 0 {
			spec.Paths.Delete(path)
		}
	}
	out := json.NewEncoder(os.Stdout)
	out.SetIndent("", "  ")
	if err := out.Encode(spec); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
