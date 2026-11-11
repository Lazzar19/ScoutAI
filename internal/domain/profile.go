package domain

// CandidateProfile is the stored preference set.
// Version increases on every change. Evaluations keep the version they used.
type CandidateProfile struct {
	ID      string
	Name    string
	Version int
	Active  bool

	PreferredRoles     []string
	RoleSynonyms       []string
	PreferredSkills    []string
	AdditionalSkills   []string
	PreferredLocations []Location
	PreferredCompanies []string
	ExcludedCompanies  []string
	ExcludedRoles      []string
	ExperienceLevel    string
}
