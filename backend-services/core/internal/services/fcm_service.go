// Copyright (c) 2025 WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"google.golang.org/api/option"
)

// FCMService provides Firebase Cloud Messaging functionality for sending push notifications.
// It wraps the Firebase Admin SDK messaging client and provides methods for sending
// notifications to multiple devices with automatic batching and retry logic.
type FCMService struct {
	client *messaging.Client
}

// Configuration constants for FCM service behavior.
const (
	// maxTokensPerBatch is the maximum number of device tokens that can be sent in a single batch.
	// FCM has a limit of 500 tokens per multicast request.
	maxTokensPerBatch = 500

	// maxRetries is the maximum number of retry attempts for failed FCM requests.
	maxRetries = 3

	// initialRetryDelay is the initial delay before the first retry attempt.
	// Subsequent retries use exponential backoff.
	initialRetryDelay = 100 * time.Millisecond

	// maxRetryDelay is the maximum delay between retry attempts.
	// This caps the exponential backoff to prevent excessively long waits.
	maxRetryDelay = 2 * time.Second
)

// NewFCMService initializes a new FCM service with Firebase Admin SDK.
//
// Parameters:
//   - credentialsPath: Path to the Firebase service account credentials JSON file.
//
// Returns:
//   - *FCMService: An initialized FCM service instance
//   - error: An error if initialization fails (e.g., invalid credentials, missing project ID)
//
// The function extracts the project ID from the credentials file and initializes
// the Firebase app with the messaging client.
func NewFCMService(credentialsPath string) (*FCMService, error) {
	ctx := context.Background()

	var app *firebase.App
	var err error

	if credentialsPath != "" {
		// Initialize with credentials file and extract project ID
		opt := option.WithCredentialsFile(credentialsPath)

		// Read project ID from credentials file
		var projectID string
		projectID, err = getProjectIDFromCredentials(credentialsPath)
		if err != nil {
			return nil, fmt.Errorf("error reading project ID from credentials: %w", err)
		}

		config := &firebase.Config{
			ProjectID: projectID,
		}

		app, err = firebase.NewApp(ctx, config, opt)
	} else {
		// Initialize with default credentials (for Cloud Run, etc.)
		app, err = firebase.NewApp(ctx, nil)
	}

	if err != nil {
		return nil, fmt.Errorf("error initializing firebase app: %w", err)
	}

	client, err := app.Messaging(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting messaging client: %w", err)
	}

	slog.Info("FCM service initialized successfully")
	return &FCMService{client: client}, nil
}

// SendMulticastNotification sends a push notification to multiple devices.
//
// This method automatically handles:
//   - Batching tokens into groups of maxTokensPerBatch (500) tokens
//   - Retry logic with exponential backoff for transient failures
//   - Platform-specific configuration (APNS for iOS, Android config)
//
// Parameters:
//   - ctx: Context for request cancellation and timeout control
//   - tokens: List of FCM device tokens to send the notification to
//   - title: Notification title
//   - body: Notification body text
//   - data: Additional key-value data to include in the notification payload
//
// Returns:
//   - int: Total number of successfully delivered notifications
//   - int: Total number of failed deliveries
//   - error: An error if the entire batch processing fails (partial failures are reported in failure count)
//
// The notification includes default sound and badge settings for both iOS and Android.
func (s *FCMService) SendMulticastNotification(
	ctx context.Context,
	tokens []string,
	title string,
	body string,
	data map[string]string,
) (int, int, error) {
	if len(tokens) == 0 {
		return 0, 0, nil
	}

	var totalSuccess, totalFailure int

	// Process tokens in batches of maxTokensPerBatch
	for i := 0; i < len(tokens); i += maxTokensPerBatch {
		end := i + maxTokensPerBatch
		if end > len(tokens) {
			end = len(tokens)
		}

		batch := tokens[i:end]

		message := &messaging.MulticastMessage{
			Tokens: batch,
			Notification: &messaging.Notification{
				Title: title,
				Body:  body,
			},
			Data: data,
			APNS: &messaging.APNSConfig{
				Payload: &messaging.APNSPayload{
					Aps: &messaging.Aps{
						Sound: "default",
						Badge: intPtr(1),
					},
				},
			},
			Android: &messaging.AndroidConfig{
				Priority: "high",
				Notification: &messaging.AndroidNotification{
					Sound:        "default",
					ChannelID:    "default",
					Priority:     messaging.PriorityHigh,
					DefaultSound: true,
				},
			},
		}

		// Send with retry mechanism
		response, err := s.sendMulticastWithRetry(ctx, message, i, end)
		if err != nil {
			return totalSuccess, totalFailure, fmt.Errorf("error sending multicast message (batch %d-%d) after retries: %w", i, end, err)
		}

		totalSuccess += response.SuccessCount
		totalFailure += response.FailureCount

		slog.Info("Sent multicast message batch",
			"batch_start", i,
			"batch_end", end,
			"batch_size", len(batch),
			"success_count", response.SuccessCount,
			"failure_count", response.FailureCount)
	}

	slog.Info("Successfully sent all multicast messages",
		"total_success_count", totalSuccess,
		"total_failure_count", totalFailure,
		"total_tokens", len(tokens))

	return totalSuccess, totalFailure, nil
}

// sendMulticastWithRetry sends a multicast message with exponential backoff retry logic.
//
// This internal method implements the retry mechanism for FCM requests. It retries on
// transient errors like network issues, timeouts, or server errors (5xx), but fails
// immediately on non-retryable errors like invalid tokens or authentication failures.
//
// The retry behavior:
//   - Up to maxRetries (3) attempts
//   - Exponential backoff starting at initialRetryDelay (100ms)
//   - Maximum delay capped at maxRetryDelay (2 seconds)
//   - Respects context cancellation during retry delays
//
// Parameters:
//   - ctx: Context for request cancellation
//   - message: The multicast message to send
//   - batchStart: Starting index of the batch (for logging)
//   - batchEnd: Ending index of the batch (for logging)
//
// Returns:
//   - *messaging.BatchResponse: The FCM response containing success/failure details
//   - error: An error if all retry attempts fail or a non-retryable error occurs
func (s *FCMService) sendMulticastWithRetry(
	ctx context.Context,
	message *messaging.MulticastMessage,
	batchStart, batchEnd int,
) (*messaging.BatchResponse, error) {
	var response *messaging.BatchResponse
	var err error
	retryDelay := initialRetryDelay

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			slog.Warn("Retrying FCM multicast send",
				"attempt", attempt,
				"batch_start", batchStart,
				"batch_end", batchEnd,
				"delay_ms", retryDelay.Milliseconds())

			// Wait before retrying
			select {
			case <-time.After(retryDelay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		response, err = s.client.SendEachForMulticast(ctx, message)
		if err == nil {
			// Success
			if attempt > 0 {
				slog.Info("FCM multicast send succeeded after retry",
					"attempt", attempt,
					"batch_start", batchStart,
					"batch_end", batchEnd)
			}
			return response, nil
		}

		// Check if error is retryable
		if !isRetryableError(err) {
			slog.Error("Non-retryable FCM error",
				"error", err,
				"batch_start", batchStart,
				"batch_end", batchEnd)
			return nil, err
		}

		if attempt < maxRetries {
			slog.Warn("Retryable FCM error encountered",
				"error", err,
				"batch_start", batchStart,
				"batch_end", batchEnd,
				"will_retry", true)

			// Exponential backoff with max cap
			retryDelay *= 2
			if retryDelay > maxRetryDelay {
				retryDelay = maxRetryDelay
			}
		} else {
			slog.Error("Max retries exceeded for FCM send",
				"error", err,
				"batch_start", batchStart,
				"batch_end", batchEnd,
				"attempts", attempt+1)
		}
	}

	return nil, fmt.Errorf("failed after %d attempts: %w", maxRetries+1, err)
}

// isRetryableError determines if an FCM error should be retried.
//
// This function examines the error message to identify transient failures that
// are likely to succeed on retry, including:
//   - Network errors: timeouts, connection refused/reset
//   - HTTP server errors: 500, 502, 503, 504
//   - gRPC status codes: UNAVAILABLE, INTERNAL, RESOURCE_EXHAUSTED
//
// Parameters:
//   - err: The error to evaluate
//
// Returns:
//   - bool: true if the error is transient and should be retried, false otherwise
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()

	// Check for common retryable error patterns
	retryablePatterns := []string{
		"deadline exceeded",
		"timeout",
		"connection refused",
		"connection reset",
		"temporary failure",
		"server unavailable",
		"internal error",
		"503",
		"500",
		"502",
		"504",
		"UNAVAILABLE",
		"INTERNAL",
		"RESOURCE_EXHAUSTED",
	}

	for _, pattern := range retryablePatterns {
		if contains(errMsg, pattern) {
			return true
		}
	}

	return false
}

// intPtr returns a pointer to the provided integer value.
// This is a helper function for creating inline integer pointers,
// commonly needed for FCM notification badge counts.
func intPtr(i int) *int {
	return &i
}

// getProjectIDFromCredentials reads the project_id from the Firebase credentials JSON file.
//
// Firebase credentials files are JSON documents containing a "project_id" field
// along with authentication information. This function extracts only the project ID.
//
// Parameters:
//   - credentialsPath: Path to the Firebase service account JSON file
//
// Returns:
//   - string: The extracted project ID
//   - error: An error if the file cannot be read, parsed, or doesn't contain a project_id
func getProjectIDFromCredentials(credentialsPath string) (string, error) {
	data, err := os.ReadFile(credentialsPath)
	if err != nil {
		return "", fmt.Errorf("failed to read credentials file: %w", err)
	}

	var creds struct {
		ProjectID string `json:"project_id"`
	}

	if err := json.Unmarshal(data, &creds); err != nil {
		return "", fmt.Errorf("failed to parse credentials JSON: %w", err)
	}

	if creds.ProjectID == "" {
		return "", fmt.Errorf("project_id not found in credentials file")
	}

	return creds.ProjectID, nil
}

// contains checks if a string contains a substring using case-insensitive comparison.
func contains(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
