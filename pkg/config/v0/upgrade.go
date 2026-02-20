package v0

func Register(parsers map[string]func([]byte) (any, error), upgraders *[]func(any, []byte) (any, error)) {
	parsers[Version] = func(d []byte) (any, error) { return Parse(d) }
	*upgraders = append(*upgraders, UpgradeIfNeeded)
}

func UpgradeIfNeeded(old any, _ []byte) (any, error) {
	return old, nil
}
