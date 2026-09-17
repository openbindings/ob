package app

import (
	"encoding/json"
	"errors"
	"github.com/openbindings/openbindings-go/jsonvalue"
)

func (c *EnvConfig) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := jsonvalue.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("environment configuration must be an object")
	}
	type plain EnvConfig
	var value plain
	if err := jsonvalue.Unmarshal(data, &value); err != nil {
		return err
	}
	delete(fields, "delegates")
	delete(fields, "authorizedExec")
	delete(fields, "delegateRegistry")
	*c = EnvConfig(value)
	c.Extra = fields
	return nil
}

func (c EnvConfig) MarshalJSON() ([]byte, error) {
	type plain EnvConfig
	data, err := jsonvalue.Marshal(plain(c))
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := jsonvalue.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for key, value := range c.Extra {
		if key == "delegates" || key == "authorizedExec" || key == "delegateRegistry" {
			continue
		}
		fields[key] = value
	}
	return jsonvalue.Marshal(fields)
}
