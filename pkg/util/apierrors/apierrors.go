package apierrors

import "strings"

// objectModifiedText is an error indicating that we are trying to update a resource that has since been updated.
// in this case we just want to retry but not log it as an error.
var objectModifiedText = "the object has been modified; please apply your changes to the latest version and try again"

// IsTransientError returns a boolean indicating if a given error is transient.
func IsTransientError(err error) bool {
	if err == nil {
		return false
	}
	return IsTransientMessage(err.Error())
}

// IsTransientMessage returns a boolean indicating if a given error message is transient.
func IsTransientMessage(msg string) bool {
	return strings.Contains(strings.ToLower(msg), objectModifiedText)
}
