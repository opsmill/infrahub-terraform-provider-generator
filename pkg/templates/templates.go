// Package templates holds the Go source templates the generator renders into a
// Terraform provider. Each template lives in its own embedded *.gotmpl file;
// base.gotmpl defines partials (e.g. the shared Configure method) shared across
// them.
package templates

import "embed"

// FS holds the embedded template files, parsed by the parser package.
//
//go:embed *.gotmpl
var FS embed.FS

// Template file names, referenced when rendering.
const (
	Resource   = "resource.gotmpl"
	DataSource = "data_source.gotmpl"
	Provider   = "provider.gotmpl"
	Artifact   = "artifact.gotmpl"
	Base       = "base.gotmpl"
)
