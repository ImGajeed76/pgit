package util

import (
	"errors"
	"fmt"
	"strings"
)

// Common errors used throughout pgit
var (
	ErrNotARepository      = errors.New("not a pgit repository (or any parent up to mount point)")
	ErrAlreadyInitialized  = errors.New("pgit repository already exists")
	ErrNoContainerRuntime  = errors.New("no container runtime found (docker or podman required)")
	ErrContainerNotRunning = errors.New("local container is not running")
	ErrDatabaseNotFound    = errors.New("database not found")
	ErrNoCommits           = errors.New("no commits yet")
	ErrNothingToCommit     = errors.New("nothing to commit (working tree clean)")
	ErrNothingStaged       = errors.New("nothing staged for commit")
	ErrUncommittedChanges  = errors.New("uncommitted changes would be overwritten")
	ErrMergeConflict       = errors.New("merge conflict detected")
	ErrRemoteNotFound      = errors.New("remote not found")
	ErrRemoteExists        = errors.New("remote already exists")
	ErrNotConnected        = errors.New("not connected to database")
	ErrInvalidCommitID     = errors.New("invalid commit ID")
	ErrCommitNotFound      = errors.New("commit not found")
	ErrFileNotFound        = errors.New("file not found")
	ErrPathNotInRepo       = errors.New("path is outside repository")
)

// PgitError is a structured error with context and suggestions
type PgitError struct {
	Title       string   // Short error title
	Message     string   // Detailed message
	Context     string   // What was being attempted
	Causes      []string // Possible causes
	Suggestions []string // Actionable suggestions with commands
	Err         error    // Wrapped error
}

func (e *PgitError) Error() string {
	return e.Title
}

func (e *PgitError) Unwrap() error {
	return e.Err
}

// Format returns a nicely formatted error message
func (e *PgitError) Format() string {
	var sb strings.Builder

	// Title
	sb.WriteString(fmt.Sprintf("Error: %s\n", e.Title))

	// Context/message
	if e.Message != "" {
		sb.WriteString(fmt.Sprintf("\n  %s\n", e.Message))
	}
	if e.Context != "" {
		sb.WriteString(fmt.Sprintf("\n  %s\n", e.Context))
	}

	// Causes
	if len(e.Causes) > 0 {
		sb.WriteString("\n  Possible causes:\n")
		for _, cause := range e.Causes {
			sb.WriteString(fmt.Sprintf("    • %s\n", cause))
		}
	}

	// Suggestions
	if len(e.Suggestions) > 0 {
		sb.WriteString("\n  Try:\n")
		for _, sug := range e.Suggestions {
			sb.WriteString(fmt.Sprintf("    $ %s\n", sug))
		}
	}

	return sb.String()
}

// NewError creates a new PgitError
func NewError(title string) *PgitError {
	return &PgitError{Title: title}
}

// WithMessage adds a detailed message
func (e *PgitError) WithMessage(msg string) *PgitError {
	e.Message = msg
	return e
}

// WithContext adds context about what was being attempted
func (e *PgitError) WithContext(ctx string) *PgitError {
	e.Context = ctx
	return e
}

// WithCause adds a possible cause
func (e *PgitError) WithCause(cause string) *PgitError {
	e.Causes = append(e.Causes, cause)
	return e
}

// WithCauses adds multiple possible causes
func (e *PgitError) WithCauses(causes ...string) *PgitError {
	e.Causes = append(e.Causes, causes...)
	return e
}

// WithSuggestion adds an actionable suggestion
func (e *PgitError) WithSuggestion(sug string) *PgitError {
	e.Suggestions = append(e.Suggestions, sug)
	return e
}

// WithSuggestions adds multiple suggestions
func (e *PgitError) WithSuggestions(sugs ...string) *PgitError {
	e.Suggestions = append(e.Suggestions, sugs...)
	return e
}

// Wrap wraps an underlying error
func (e *PgitError) Wrap(err error) *PgitError {
	e.Err = err
	return e
}

// ══════════════════════════════════════════════════════════════════════════
// Pre-built error constructors for common cases
// ══════════════════════════════════════════════════════════════════════════

// NotARepoError returns a structured error for "not a repository"
func NotARepoError() *PgitError {
	return NewError("Not a pgit repository").
		WithMessage("No .pgit directory found in current directory or any parent").
		WithSuggestions(
			"pgit init              # Initialize a new repository",
			"cd /path/to/repo       # Change to an existing repository",
		)
}

// NoContainerError returns a structured error for missing container runtime
func NoContainerError() *PgitError {
	return NewError("No container runtime found").
		WithMessage("pgit requires Docker or Podman to run the local database").
		WithCauses(
			"Docker is not installed",
			"Docker daemon is not running",
			"Podman is not installed",
		).
		WithSuggestions(
			"docker info            # Check Docker status",
			"systemctl start docker # Start Docker daemon",
			"pgit doctor            # Run diagnostics",
		)
}

// ContainerNotRunningError returns a structured error for stopped container
func ContainerNotRunningError() *PgitError {
	return NewError("Local container is not running").
		WithSuggestions(
			"pgit local start       # Start the container",
			"pgit local status      # Check container status",
		)
}

// DatabaseConnectionError returns a structured error for DB connection issues
func DatabaseConnectionError(url string, err error) *PgitError {
	return NewError("Cannot connect to database").
		WithContext(url).
		WithCauses(
			"Database server is not running",
			"Invalid connection credentials",
			"Network connectivity issues",
			"Database does not exist",
		).
		WithSuggestions(
			"pgit local status      # Check local container",
			"pgit doctor            # Run diagnostics",
		).
		Wrap(err)
}

// RemoteNotFoundError returns a structured error for missing remote
func RemoteNotFoundError(name string) *PgitError {
	return NewError(fmt.Sprintf("Remote '%s' not found", name)).
		WithSuggestions(
			"pgit remote            # List configured remotes",
			fmt.Sprintf("pgit remote add %s <url>  # Add the remote", name),
		)
}

// CommitNotFoundError returns a structured error for missing commit
func CommitNotFoundError(ref string) *PgitError {
	return NewError(fmt.Sprintf("Commit '%s' not found", ref)).
		WithCauses(
			"The commit ID is incorrect",
			"The commit may have been on a different branch",
		).
		WithSuggestions(
			"pgit log               # View commit history",
		)
}

// MissingArgumentError returns an error for missing required argument
func MissingArgumentError(argName, example string) *PgitError {
	e := NewError(fmt.Sprintf("Missing required argument: <%s>", argName))
	if example != "" {
		e.WithSuggestion(example)
	}
	return e
}

// TooManyArgumentsError returns an error for too many arguments
func TooManyArgumentsError(expected int, got int) *PgitError {
	return NewError(fmt.Sprintf("Too many arguments: expected %d, got %d", expected, got))
}
