package template

import (
	"fmt"

	"github.com/Jeffail/benthos/v3/internal/bloblang"
	"github.com/Jeffail/benthos/v3/internal/docs"
)

// FieldConfig describes a configuration field used in the template.
type FieldConfig struct {
	Name        string       `yaml:"name"`
	Description string       `yaml:"description"`
	Default     *interface{} `yaml:"default"`
	// TODO: Add a type field and some other stuff.
}

// Config describes a Benthos component template.
type Config struct {
	Name        string        `yaml:"name"`
	Type        string        `yaml:"type"`
	Summary     string        `yaml:"summary"`
	Description string        `yaml:"description"`
	Fields      []FieldConfig `yaml:"fields"`
	Mapping     string        `yaml:"mapping"`
}

// FieldSpec creates a documentation field spec from a template field config.
func (c FieldConfig) FieldSpec() docs.FieldSpec {
	f := docs.FieldCommon(c.Name, c.Description)
	if c.Default != nil {
		f = f.HasDefault(*f.Default)
	}
	return f
}

// ComponentSpec creates a documentation component spec from a template config.
func (c Config) ComponentSpec() docs.ComponentSpec {
	fields := make([]docs.FieldSpec, len(c.Fields))
	for i, fieldConf := range c.Fields {
		fields[i] = fieldConf.FieldSpec()
	}
	config := docs.FieldComponent().WithChildren(fields...)

	return docs.ComponentSpec{
		Name:        c.Name,
		Type:        docs.Type(c.Type), // Validated elsewhere.
		Status:      docs.StatusPlugin,
		Summary:     c.Summary,
		Description: c.Description,
		Config:      config,
	}
}

// Compile attempts to validate the config and parse any mappings.
func (c Config) Compile() (*Compiled, error) {
	spec := c.ComponentSpec()
	mapping, err := bloblang.NewMapping(c.Mapping, "")
	if err != nil {
		return nil, fmt.Errorf("template mapping: %w", err)
	}
	return &Compiled{spec, mapping}, nil
}

//------------------------------------------------------------------------------

// FieldConfigSpec returns a configuration spec for a field of a template.
func FieldConfigSpec() docs.FieldSpecs {
	return docs.FieldSpecs{
		docs.FieldCommon("name", "The name of the field"),
		docs.FieldCommon("description", "A description of the field."),
		docs.FieldCommon("default", "An optional default value for the field. If a default value is not specified then a configuration without the field is considered incorrect."),
	}
}

// ConfigSpec returns a configuration spec for a template.
func ConfigSpec() docs.FieldSpecs {
	return docs.FieldSpecs{
		docs.FieldCommon("name", "The name of the component this template will create."),
		docs.FieldCommon("type", "The type of the component this template will create.").HasOptions(
			"cache", "input", "output", "processor", "rate_limit",
		),
		docs.FieldCommon("summary", "A short summary of the component."),
		docs.FieldCommon("description", "A longer form description of the component and how to use it."),
		docs.FieldCommon("fields", "The fields of the template.").WithChildren(FieldConfigSpec()...),
		docs.FieldCommon("mapping", "A [Bloblang](/docs/guides/bloblang/about) mapping that translates the fields of the template into a valid Benthos configuration for the target component type."),
	}
}
