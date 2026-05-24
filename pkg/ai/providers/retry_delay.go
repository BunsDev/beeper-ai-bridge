package providers

import (
	"net/http"
	"strconv"
	"time"
)

func RequestedRetryDelay(response *http.Response, now time.Time) (time.Duration, bool) {
	if response == nil {
		return 0, false
	}
	if value := response.Header.Get("Retry-After-Ms"); value != "" {
		millis, err := strconv.ParseFloat(value, 64)
		if err == nil {
			if millis < 0 {
				millis = 0
			}
			return time.Duration(millis * float64(time.Millisecond)), true
		}
	}
	if value := response.Header.Get("Retry-After"); value != "" {
		seconds, err := strconv.ParseFloat(value, 64)
		if err == nil {
			if seconds < 0 {
				seconds = 0
			}
			return time.Duration(seconds * float64(time.Second)), true
		}
		date, err := http.ParseTime(value)
		if err == nil {
			delay := date.Sub(now)
			if delay < 0 {
				delay = 0
			}
			return delay, true
		}
	}
	return 0, false
}

func RetryDelayExceedsCap(delay time.Duration, maxRetryDelayMs *int) bool {
	if maxRetryDelayMs == nil || *maxRetryDelayMs == 0 {
		return false
	}
	return delay > time.Duration(*maxRetryDelayMs)*time.Millisecond
}

func RetryDelayCapError(delay time.Duration, maxRetryDelayMs int) string {
	return "Retry delay " + strconv.FormatInt(delay.Milliseconds(), 10) + "ms exceeds maxRetryDelayMs " + strconv.Itoa(maxRetryDelayMs)
}
