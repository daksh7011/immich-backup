// internal/config/schedule.go
package config

import (
	"fmt"
	"strconv"
	"strings"
)

// scheduleExample is shown in every schedule error so the fix is obvious.
const scheduleExample = `use "MINUTE HOUR * * *", e.g. "0 3 * * *" for 03:00 every day`

// ParseDailySchedule parses backup.schedule into the hour and minute of the
// daily run. The daemon writes one systemd OnCalendar entry or one launchd
// StartCalendarInterval, so only "MINUTE HOUR * * *" with plain integers is
// accepted: steps, ranges, lists, macros (@daily) and any day, month or
// weekday restriction are rejected rather than silently turned into "daily".
func ParseDailySchedule(expr string) (hour, minute int, err error) {
	fields := strings.Fields(expr)
	if len(fields) == 0 {
		return 0, 0, fmt.Errorf("schedule is empty; %s", scheduleExample)
	}
	if len(fields) != 5 {
		return 0, 0, fmt.Errorf("schedule %q must have 5 cron fields; %s", expr, scheduleExample)
	}
	minute, err = scheduleField(expr, "minute", fields[0], 59)
	if err != nil {
		return 0, 0, err
	}
	hour, err = scheduleField(expr, "hour", fields[1], 23)
	if err != nil {
		return 0, 0, err
	}
	for i, name := range []string{"day-of-month", "month", "day-of-week"} {
		if f := fields[2+i]; f != "*" {
			return 0, 0, fmt.Errorf(
				"schedule %q: %s must be \"*\" (got %q); backups run once a day, %s",
				expr, name, f, scheduleExample)
		}
	}
	return hour, minute, nil
}

// ValidateDaemonSchedule reports whether expr is a schedule the daemon can
// install. Config validation, the setup wizard, and the systemd and launchd
// generators all use it, so a schedule that saves also installs.
func ValidateDaemonSchedule(expr string) error {
	_, _, err := ParseDailySchedule(expr)
	return err
}

// scheduleField parses one minute or hour field: digits only, 0..max.
func scheduleField(expr, name, f string, max int) (int, error) {
	n, err := strconv.Atoi(f)
	if err != nil || strings.ContainsAny(f, "+-") || n < 0 || n > max {
		return 0, fmt.Errorf(
			"schedule %q: %s must be a number from 0 to %d (got %q); steps, ranges and lists are not supported, %s",
			expr, name, max, f, scheduleExample)
	}
	return n, nil
}
