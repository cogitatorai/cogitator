package secretstore

import "errors"

// ErrNotFound is returned by Get when the requested key does not exist in the namespace.
var ErrNotFound = errors.New("secret not found")

// SecretStore defines a simple key/value secret storage interface scoped by namespace.
// Each namespace is an independent keyspace; operations in one namespace never
// affect another.
type SecretStore interface {
	// Get retrieves the value for key in the given namespace.
	// Returns ErrNotFound when the key does not exist.
	Get(namespace, key string) (string, error)

	// Set stores or overwrites value for key in the given namespace.
	Set(namespace, key, value string) error

	// Delete removes key from namespace. Deleting a key that does not exist is
	// a no-op and returns nil.
	Delete(namespace, key string) error

	// List returns the keys present in namespace, in unspecified order.
	// Returns an empty slice (not an error) when the namespace has no keys.
	List(namespace string) ([]string, error)
}
