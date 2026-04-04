package handlers

import (
	"strings"
)

// FriendlyComposeMsg turns noisy docker compose stderr into a short UI hint when ps fails.
func FriendlyComposeMsg(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "postgres_password") && strings.Contains(lower, "required") {
		return "Add POSTGRES_PASSWORD (and any other required variables) under Environment, save .env, then deploy again."
	}
	if strings.Contains(lower, "interpolating") && strings.Contains(lower, "missing") {
		return "Compose needs environment variables that are not set. Use the Environment tab to edit .env, save, then deploy or refresh."
	}
	if strings.Contains(lower, "required variable") {
		return "A required variable is missing from .env. Open Environment, add the missing keys, save, then try again."
	}
	if len(msg) > 400 {
		return msg[:400] + "…"
	}
	return msg
}
