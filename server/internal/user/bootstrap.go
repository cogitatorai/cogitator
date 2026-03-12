package user

import "fmt"

// Bootstrap creates the initial admin account if no users exist.
// Returns the created user, nil if users already exist or no credentials
// are provided, or an error.
func (s *Store) Bootstrap(email, password string) (*User, error) {
	count, err := s.Count()
	if err != nil {
		return nil, fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return nil, nil // already bootstrapped
	}
	if email == "" || password == "" {
		return nil, nil // no credentials provided; setup via dashboard
	}
	return s.Create(CreateUserInput{
		Email:    email,
		Name:     "Admin",
		Password: password,
		Role:     RoleAdmin,
	})
}
