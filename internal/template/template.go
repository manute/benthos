package template

import (
	"errors"

	"github.com/Jeffail/benthos/v3/internal/bloblang/mapping"
	"github.com/Jeffail/benthos/v3/internal/bundle"
	"github.com/Jeffail/benthos/v3/internal/docs"
	"github.com/Jeffail/benthos/v3/lib/processor"
	"gopkg.in/yaml.v3"
)

// Compiled is a template that has been compiled from a config.
type Compiled struct {
	spec    docs.ComponentSpec
	mapping *mapping.Executor
}

// ExpandToNode attempts to apply the template to a provided YAML node and
// returns the new expanded configuration.
func (c *Compiled) ExpandToNode(node *yaml.Node) (*yaml.Node, error) {
	generic, err := c.spec.Config.Children.NodeToMap(node)
	if err != nil {
		return nil, err
	}

	_ = generic

	// 1. Apply mapping from generic to something else
	// 2. Convert something else into yaml node
	// 3. Parse processor config from yaml node
	// 4. Construct new component from config using nm
	// 5. Return if successful

	// TODO: Adjust metrics
	return nil, nil
}

// RegisterProcessorTemplate adds a template config to a set.
func RegisterProcessorTemplate(tmpl *Compiled, set *bundle.ProcessorSet) error {
	return set.Add(func(c processor.Config, nm bundle.NewManagement) (processor.Type, error) {
		newNode, err := tmpl.ExpandToNode(c.Plugin.(*yaml.Node))
		if err != nil {
			return nil, err
		}
		_ = newNode
		return nil, errors.New("TODO")
	}, tmpl.spec)
}
