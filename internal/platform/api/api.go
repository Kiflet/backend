// Package api is the API's contract in Go (docs/architecture.md, section
// 11): the types and the strict server interface generated from
// api/openapi/v1.yaml by make api, into api.gen.go. Don't edit that file;
// change the specification and generate it again.
//
// The operations implement StrictServerInterface as they move to it
// (docs/migration.md, phase 2, step 6, and phase 3), so that an operation
// missing or misnamed fails to compile.
package api
