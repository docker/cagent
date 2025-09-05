package types

import (
	"encoding/json"
	"fmt"
)

func CloneThroughJSON(oldConfig, newConfig any) {
	o, err := json.Marshal(oldConfig)
	if err != nil {
		panic(fmt.Sprintf("marshalling old: %v", err))
	}

	if err := json.Unmarshal(o, newConfig); err != nil {
		panic(fmt.Sprintf("unmarshalling new: %v", err))
	}
}
