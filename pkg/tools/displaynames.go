package tools

func (t *Tool) DisplayName() string {
	title := t.Annotations.Title
	if title != "" {
		return title
	}

	return t.Name
}
