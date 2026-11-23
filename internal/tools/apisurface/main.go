// Command apisurface writes part of the API's specification as JSON.
//
// With -tags, the operations with those tags, and the paths still having
// one (api.Surface): scripts/api-breaking.sh compares the public and
// driver operations, which may not break, on their own. With -document,
// the published description of the operations with that tag (api.Document):
// make api writes the public API's openapi.json with it.
//
//	go run ./internal/tools/apisurface -tags public,driver api/openapi/v1.yaml
//	go run ./internal/tools/apisurface -document public api/openapi/v1.yaml
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/kiflet/backend/internal/platform/api"
)

func main() {
	tags := flag.String("tags", "public,driver", "the tags to keep, separated by commas")
	document := flag.String("document", "", "write the published description of the operations with this tag")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: apisurface [-tags public,driver | -document public] specification.yaml")
		os.Exit(2)
	}
	spec, err := openapi3.NewLoader().LoadFromFile(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *document != "" {
		b, err := api.Document(spec, *document)
		if err == nil {
			_, err = os.Stdout.Write(b)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	api.Surface(spec, strings.Split(*tags, ","))
	out := json.NewEncoder(os.Stdout)
	out.SetIndent("", "  ")
	if err := out.Encode(spec); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
