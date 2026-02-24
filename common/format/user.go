package format

import "strconv"

// UserTag converts a numeric user ID to the string tag used as the sing-box
// user name (which is how we key traffic counters).
func UserTag(userID int) string {
	return strconv.Itoa(userID)
}

// ParseUserID parses a user tag string back to a numeric user ID.
func ParseUserID(tag string) (int, error) {
	return strconv.Atoi(tag)
}
