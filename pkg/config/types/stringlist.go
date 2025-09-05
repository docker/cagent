package types

type StringOrList []string

func (sm *StringOrList) UnmarshalYAML(unmarshal func(any) error) error {
	var multi []string
	if err := unmarshal(&multi); err != nil {
		var single string
		if err := unmarshal(&single); err != nil {
			return err
		}

		*sm = []string{single}
		return nil
	}

	*sm = multi
	return nil
}
