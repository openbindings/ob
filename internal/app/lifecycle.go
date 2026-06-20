package app

import (
	"fmt"
	"os"

	openbindings "github.com/openbindings/openbindings-go"
)

// --- new ---

// NewInterfaceInput represents input for creating an empty interface document.
type NewInterfaceInput struct {
	Path         string
	Name         string
	Version      string
	Description  string
	OpenBindings string // target spec version; defaults to the max tested version
	Force        bool   // overwrite an existing file
}

// NewInterfaceOutput represents the result of `ob new`.
type NewInterfaceOutput struct {
	Path         string `json:"path"`
	Name         string `json:"name,omitempty"`
	Version      string `json:"version,omitempty"`
	OpenBindings string `json:"openbindings"`
}

// Render returns a human-friendly representation.
func (o NewInterfaceOutput) Render() string {
	s := Styles
	header := o.Name
	if header == "" {
		header = "(unnamed)"
	}
	if o.Version != "" {
		header += " " + o.Version
	}
	return s.Header.Render("Created interface") + " " + s.Key.Render(o.Path) +
		"\n  " + s.Dim.Render(fmt.Sprintf("%s (openbindings %s)", header, o.OpenBindings))
}

// NewInterface creates an empty OpenBindings interface document — only the core
// fields (openbindings, name, version) and an empty operations map. It is the
// authorship entry point; populate it with `operation add` / `source add` /
// `source pull` / `operation bind`.
func NewInterface(input NewInterfaceInput) (NewInterfaceOutput, error) {
	if input.Path == "" {
		return NewInterfaceOutput{}, fmt.Errorf("output path is required")
	}
	if !input.Force {
		if _, err := os.Stat(input.Path); err == nil {
			return NewInterfaceOutput{}, fmt.Errorf("%s already exists; use --force to overwrite", input.Path)
		}
	}

	obVersion := input.OpenBindings
	if obVersion == "" {
		obVersion = openbindings.MaxTestedVersion
	}

	iface := &openbindings.Interface{
		OpenBindings: obVersion,
		Name:         input.Name,
		Version:      input.Version,
		Description:  input.Description,
		Operations:   map[string]openbindings.Operation{},
	}
	if err := WriteInterfaceFile(input.Path, iface); err != nil {
		return NewInterfaceOutput{}, fmt.Errorf("write OBI: %w", err)
	}
	return NewInterfaceOutput{
		Path:         input.Path,
		Name:         input.Name,
		Version:      input.Version,
		OpenBindings: obVersion,
	}, nil
}

// --- meta set ---

// MetaSetInput holds the interface-level fields to change. Only non-nil fields
// are applied. (homepage/repository/maintainer are software-descriptor fields,
// not interface metadata, so they are not set here.)
type MetaSetInput struct {
	Path        string
	Name        *string
	Version     *string
	Description *string
}

// MetaSetOutput represents the result of `ob meta set`.
type MetaSetOutput struct {
	Name        string `json:"name,omitempty"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
}

// Render returns a human-friendly representation.
func (o MetaSetOutput) Render() string {
	s := Styles
	header := o.Name
	if header == "" {
		header = "(unnamed)"
	}
	if o.Version != "" {
		header += " " + o.Version
	}
	out := s.Header.Render("Updated metadata") + "\n  " + s.Key.Render(header)
	if o.Description != "" {
		out += "\n  " + s.Dim.Render(o.Description)
	}
	return out
}

// MetaSet edits an interface's top-level metadata (name, version, description).
func MetaSet(input MetaSetInput) (MetaSetOutput, error) {
	iface, err := loadInterfaceFile(input.Path)
	if err != nil {
		return MetaSetOutput{}, fmt.Errorf("load OBI: %w", err)
	}
	if input.Name != nil {
		iface.Name = *input.Name
	}
	if input.Version != nil {
		iface.Version = *input.Version
	}
	if input.Description != nil {
		iface.Description = *input.Description
	}
	if err := WriteInterfaceFile(input.Path, iface); err != nil {
		return MetaSetOutput{}, fmt.Errorf("write OBI: %w", err)
	}
	return MetaSetOutput{Name: iface.Name, Version: iface.Version, Description: iface.Description}, nil
}
