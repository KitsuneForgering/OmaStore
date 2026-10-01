// Package repoid validates GitHub repository names ("owner/repo"). Every
// entry point (RPC, CLI, GitHub client, installer) uses the same rule, so a
// name accepted for an API call is also safe as a local path component.
package repoid

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrInvalid means the name is not a valid "owner/repo".
var ErrInvalid = errors.New("invalid repository name")

var (
	// GitHub user and organization names: letters, digits and hyphens, up to 39.
	reOwner = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)
	// Repository names start with a letter or digit here (no ".", ".." or
	// hidden names), since they become directory names on disk.
	reRepo = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
)

// Split validates fullName and returns its owner and repository.
func Split(fullName string) (owner, repo string, err error) {
	owner, repo, ok := strings.Cut(fullName, "/")
	if !ok || !reOwner.MatchString(owner) || !reRepo.MatchString(repo) || strings.Contains(repo, "..") {
		return "", "", fmt.Errorf("%w: %q", ErrInvalid, fullName)
	}
	return owner, repo, nil
}

// Validate reports whether fullName is a valid "owner/repo".
func Validate(fullName string) error {
	_, _, err := Split(fullName)
	return err
}

// Equal compares two repository names the way GitHub does: ignoring case.
func Equal(a, b string) bool { return strings.EqualFold(a, b) }
