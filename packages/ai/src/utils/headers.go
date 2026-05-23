package utils

import "net/http"

func HeadersToRecord(headers http.Header) map[string]string {
	result := map[string]string{}
	for key, values := range headers {
		if len(values) > 0 {
			result[key] = values[0]
		}
	}
	return result
}
