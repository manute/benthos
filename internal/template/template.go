package template

import (
	"fmt"

	"github.com/Jeffail/benthos/v3/internal/bloblang/mapping"
	"github.com/Jeffail/benthos/v3/internal/bundle"
	"github.com/Jeffail/benthos/v3/internal/docs"
	"github.com/Jeffail/benthos/v3/lib/message"
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

	msg := message.New(nil)
	part := message.NewPart(nil)
	if err := part.SetJSON(generic); err != nil {
		return nil, err
	}
	msg.Append(part)

	newPart, err := c.mapping.MapPart(0, msg)
	if err != nil {
		return nil, err
	}

	resultGeneric, err := newPart.JSON()
	if err != nil {
		return nil, err
	}

	var resultNode yaml.Node
	if err := resultNode.Encode(resultGeneric); err != nil {
		return nil, err
	}

	return &resultNode, nil
}

// RegisterTemplate attempts to add a template component to the global list of
// component types.
func RegisterTemplate(tmpl *Compiled) error {
	if tmpl.spec.Type == docs.TypeProcessor {
		return registerProcessorTemplate(tmpl, bundle.AllProcessors)
	}
	return fmt.Errorf("unable to register template for component type %v", tmpl.spec.Type)
}

func registerProcessorTemplate(tmpl *Compiled, set *bundle.ProcessorSet) error {
	return set.Add(func(c processor.Config, nm bundle.NewManagement) (processor.Type, error) {
		newNode, err := tmpl.ExpandToNode(c.Plugin.(*yaml.Node))
		if err != nil {
			return nil, err
		}

		conf := processor.NewConfig()
		if err := newNode.Decode(&conf); err != nil {
			return nil, err
		}

		// TODO: Rewrite metrics
		return nm.NewProcessor(conf)
	}, tmpl.spec)
}
