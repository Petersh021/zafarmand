package main

import (
	"context"
	"sync"
)

// recordingAdminProductWriter is the database-free mutation dependency shared
// by application and HTTP tests.
type recordingAdminProductWriter struct {
	// mu protects configured outcomes and recorded calls.
	mu sync.Mutex
	// createResult controls the next successful CREATE value.
	createResult adminProductWriteResult
	// createErr controls the next CREATE error category.
	createErr error
	// updateResult controls the next successful UPDATE value.
	updateResult adminProductWriteResult
	// updateErr controls the next UPDATE error category.
	updateErr error
	// createCalls preserves isolated insertion invocations.
	createCalls []recordingAdminProductCreateCall
	// updateCalls preserves isolated edit invocations.
	updateCalls []recordingAdminProductUpdateCall
}

// recordingAdminProductCreateCall captures one Product insertion request.
type recordingAdminProductCreateCall struct {
	// Input is the validated Product data supplied by the handler.
	Input adminProductWriteInput
	// HasDeadline proves the HTTP layer bounded the write operation.
	HasDeadline bool
}

// recordingAdminProductUpdateCall captures one concurrency-aware Product edit.
type recordingAdminProductUpdateCall struct {
	// ID is the canonical protected Product identity.
	ID int64
	// ExpectedVersion is the revision submitted by the browser form.
	ExpectedVersion int64
	// Input is the validated replacement Product data.
	Input adminProductWriteInput
	// HasDeadline proves the HTTP layer bounded the write operation.
	HasDeadline bool
}

// newRecordingAdminProductWriter returns deterministic successful outcomes.
func newRecordingAdminProductWriter() *recordingAdminProductWriter {
	return &recordingAdminProductWriter{
		createResult: adminProductWriteResult{ID: 20, Version: 1},
		updateResult: adminProductWriteResult{ID: 1, Version: 2},
	}
}

// Create implements adminProductWriter and records only the validated boundary.
func (writer *recordingAdminProductWriter) Create(
	ctx context.Context,
	input adminProductWriteInput,
) (adminProductWriteResult, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	_, hasDeadline := ctx.Deadline()
	writer.createCalls = append(writer.createCalls, recordingAdminProductCreateCall{
		Input:       input,
		HasDeadline: hasDeadline,
	})

	return writer.createResult, writer.createErr
}

// Update implements adminProductWriter and records optimistic-lock arguments.
func (writer *recordingAdminProductWriter) Update(
	ctx context.Context,
	id int64,
	expectedVersion int64,
	input adminProductWriteInput,
) (adminProductWriteResult, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	_, hasDeadline := ctx.Deadline()
	writer.updateCalls = append(writer.updateCalls, recordingAdminProductUpdateCall{
		ID:              id,
		ExpectedVersion: expectedVersion,
		Input:           input,
		HasDeadline:     hasDeadline,
	})

	return writer.updateResult, writer.updateErr
}

// setCreateOutcome configures the next deterministic insertion result.
func (writer *recordingAdminProductWriter) setCreateOutcome(
	result adminProductWriteResult,
	err error,
) {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	writer.createResult = result
	writer.createErr = err
}

// setUpdateOutcome configures the next deterministic edit result.
func (writer *recordingAdminProductWriter) setUpdateOutcome(
	result adminProductWriteResult,
	err error,
) {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	writer.updateResult = result
	writer.updateErr = err
}

// createCallSnapshot returns an isolated insertion-call history.
func (writer *recordingAdminProductWriter) createCallSnapshot() []recordingAdminProductCreateCall {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	return append([]recordingAdminProductCreateCall(nil), writer.createCalls...)
}

// updateCallSnapshot returns an isolated edit-call history.
func (writer *recordingAdminProductWriter) updateCallSnapshot() []recordingAdminProductUpdateCall {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	return append([]recordingAdminProductUpdateCall(nil), writer.updateCalls...)
}
