package model

// ActorRef is an upstream actor identity, not a movie or a name-only credit.
// Searching actors never merges local people or changes title bindings.
type ActorRef struct {
	CatalogID, Namespace, RemoteID string
	ProviderRevision               int64
}

type ActorCandidate struct {
	Ref     ActorRef
	Name    string
	Aliases []string
}

type ActorSearch struct {
	Items     []ActorCandidate
	Truncated bool
}

type ActorProfile struct {
	ActorCandidate
	Summary, Homepage, Birthday, DebutDate       string
	Nationality, Height, BloodType, Hobby, Skill string
	Measurements, CupSize                        string
	Images                                       []string
}
