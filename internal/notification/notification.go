// Package notification is the notification domain boundary.
// Auth enqueues email jobs here; River workers render templates and send.
package notification

import "os"

// AppName is the product name used in email templates.
func AppName() string {
	if name := os.Getenv("APP_NAME"); name != "" {
		return name
	}
	return "Rallya"
}
