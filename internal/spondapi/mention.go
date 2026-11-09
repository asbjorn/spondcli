package spondapi

// Mention is a person tagged in a message. Type is PROFILE (a profile id)
// or MEMBERSHIP (a member id). Received messages carry the name; the app
// sends only id and type.
type Mention struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	Type string `json:"type"`
}
