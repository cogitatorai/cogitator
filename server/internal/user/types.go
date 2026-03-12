package user

import "time"

// Role represents a user's authorization level.
type Role string

const (
	RoleAdmin     Role = "admin"
	RoleModerator Role = "moderator"
	RoleUser      Role = "user"
)

// User represents an authenticated user in the system.
type User struct {
	ID               string    `json:"id"`
	Email            string    `json:"email"`
	Name             string    `json:"name"`
	PasswordHash     string    `json:"-"`
	Role             Role      `json:"role"`
	ProfileOverrides string    `json:"profile_overrides"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// InviteCode represents a one-time-use registration code.
type InviteCode struct {
	Code       string     `json:"code"`
	CreatedBy  string     `json:"created_by"`
	Role       Role       `json:"role"`
	RedeemedBy *string    `json:"redeemed_by"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// OAuthLink represents a linked social provider identity.
type OAuthLink struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Provider  string    `json:"provider"`
	Subject   string    `json:"subject"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateUserInput holds the fields required to create a new user.
type CreateUserInput struct {
	Email    string
	Name     string
	Password string
	Role     Role
}

// CreateInviteInput holds the fields required to create an invite code.
type CreateInviteInput struct {
	CreatedBy string
	Role      Role
	ExpiresAt *time.Time
}
