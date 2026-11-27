package config

import "fmt"

// IssueKind classifies a configuration load problem by the phase that
// found it. The kinds are stable; tests and documentation rely on them.
type IssueKind string

const (
	// IssueReadFailed: the file exists but could not be read at all,
	// as under a permission error. The in-memory configuration fell
	// back to defaults.
	IssueReadFailed IssueKind = "read_failed"
	// IssueParseFailed: the file is not a JSON object. The original
	// bytes are untouched, a backup copy was attempted, and the
	// in-memory configuration fell back to defaults. When the backup
	// copy failed, overwriting the original is refused until one
	// succeeds: it is the only copy of what the user had.
	IssueParseFailed IssueKind = "parse_failed"
	// IssueSchemaInvalid: one key's value does not fit its field's
	// type, such as a string where a number belongs. The key keeps
	// its default in memory and its original text on write-back.
	IssueSchemaInvalid IssueKind = "schema_invalid"
	// IssueValidationFailed: one key decodes but falls outside its
	// contractual domain, such as an unknown enum name. The key keeps
	// its default in memory. The invalid value is representable in
	// memory, so write-back converges the file to the in-memory truth
	// rather than preserving the value the user is warned about —
	// otherwise a repair to the default could never reach the file.
	IssueValidationFailed IssueKind = "validation_failed"
	// IssueVersionNewer: the file declares a schema version newer
	// than this build understands. Known keys still load; keys the
	// newer schema introduced survive untouched on write-back.
	IssueVersionNewer IssueKind = "version_newer"
)

// Issue is one problem found while loading the configuration. Loading
// never fails outright: every problem degrades exactly the part of the
// configuration it affects and is reported here instead.
type Issue struct {
	Kind   IssueKind
	Key    string // the offending configuration key, when one key is at fault
	Detail string // what was wrong with it
	Backup string // the backup copy of the original file, for parse failures; "" when the copy failed
}

func (i Issue) Error() string {
	if i.Key != "" {
		return fmt.Sprintf("%s: %s: %s", i.Kind, i.Key, i.Detail)
	}
	return fmt.Sprintf("%s: %s", i.Kind, i.Detail)
}

// Issues is what went wrong loading this configuration, in the order the
// problems were found. An empty slice means the file loaded cleanly or
// did not exist, which is not an error: a missing configuration is the
// first-run state.
func (c *Config) Issues() []Issue { return c.issues }
