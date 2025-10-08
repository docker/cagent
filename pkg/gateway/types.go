package gateway

type topLevel struct {
	Catalog Catalog `json:"registry"`
}

type Catalog map[string]Server

type Server struct {
	Secrets []Secret `json:"secrets,omitempty"`
}

type Secret struct {
	Name    string `json:"name"`
	Env     string `json:"env"`
	Example string `json:"example"`
}
