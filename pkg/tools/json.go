package tools

import (
	"encoding/json"
)

func JSONRoundtrip(params, v any) error {
	buf, err := json.Marshal(params)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(buf, v); err != nil {
		return err
	}

	return nil
}
