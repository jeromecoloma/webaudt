package ui

import (
	"fmt"
	"time"
)

// RelativeTime turns a Unix epoch into "5m ago" / "2h ago" / "2026-05-20".
func RelativeTime(epoch int64) string {
	if epoch == 0 {
		return "never"
	}
	diff := time.Now().Unix() - epoch
	switch {
	case diff < 60:
		return fmt.Sprintf("%ds ago", diff)
	case diff < 3600:
		return fmt.Sprintf("%dm ago", diff/60)
	case diff < 86400:
		return fmt.Sprintf("%dh ago", diff/3600)
	default:
		return time.Unix(epoch, 0).Format("2006-01-02")
	}
}

// AbsTime formats a Unix epoch as a human-readable absolute timestamp.
func AbsTime(epoch int64) string {
	return time.Unix(epoch, 0).Format("2006-01-02 15:04:05")
}
