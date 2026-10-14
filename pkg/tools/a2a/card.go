package a2a

import (
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2acompat/a2av0"
)

// parseAgentCard reads A2A 1.0 and 0.3 cards. The v0 parser handles both shapes.
func parseAgentCard(body []byte) (*a2a.AgentCard, error) {
	card, err := a2av0.NewAgentCardParser()(body)
	if err != nil {
		return nil, err
	}
	if err := validateInterfaces(card.SupportedInterfaces); err != nil {
		return nil, err
	}
	return card, nil
}

// validateInterfaces rejects entries the SDK client factory would dereference or cannot dial.
func validateInterfaces(interfaces []*a2a.AgentInterface) error {
	for i, iface := range interfaces {
		switch {
		case iface == nil:
			return fmt.Errorf("supportedInterfaces[%d] is null", i)
		case iface.URL == "":
			return fmt.Errorf("supportedInterfaces[%d] has no url", i)
		case iface.ProtocolBinding == "":
			return fmt.Errorf("supportedInterfaces[%d] has no protocolBinding", i)
		}
	}
	return nil
}
