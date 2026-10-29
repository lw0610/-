package sessionfiles

// RegisterProvided adds an input file to the session's material index.
// Local file reads do not depend on this registration.
func (s *Store) RegisterProvided(path string, origin Origin, options ...RegistrationOption) (Item, error) {
	return s.Register(path, origin, options...)
}
