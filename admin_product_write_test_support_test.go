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
	// coverResult controls the next successful cover mutation result.
	coverResult adminProductCoverWriteResult
	// coverErr controls the next cover mutation error category.
	coverErr error
	// createCalls preserves isolated insertion invocations.
	createCalls []recordingAdminProductCreateCall
	// updateCalls preserves isolated edit invocations.
	updateCalls []recordingAdminProductUpdateCall
	// coverCalls preserves isolated cover replacement invocations.
	coverCalls []recordingAdminProductCoverCall
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

// recordingAdminProductCoverCall captures one concurrency-aware cover write.
type recordingAdminProductCoverCall struct {
	// ID is the canonical protected Product identity.
	ID int64
	// ExpectedVersion is the Product revision submitted by the cover form.
	ExpectedVersion int64
	// Input contains validated and decoded cover bytes plus reviewed metadata.
	Input adminProductCoverWriteInput
	// HasDeadline proves the HTTP layer bounded the write operation.
	HasDeadline bool
}

// newRecordingAdminProductWriter returns deterministic successful outcomes.
func newRecordingAdminProductWriter() *recordingAdminProductWriter {
	return &recordingAdminProductWriter{
		createResult: adminProductWriteResult{ID: 20, Version: 1},
		updateResult: adminProductWriteResult{ID: 1, Version: 2},
		coverResult: adminProductCoverWriteResult{
			ProductID:      1,
			ProductVersion: 2,
			CoverVersion:   1,
		},
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

// UpsertCover implements adminProductWriter and records an isolated image copy.
func (writer *recordingAdminProductWriter) UpsertCover(
	ctx context.Context,
	id int64,
	expectedVersion int64,
	input adminProductCoverWriteInput,
) (adminProductCoverWriteResult, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	_, hasDeadline := ctx.Deadline()
	input.Content = append([]byte(nil), input.Content...)
	writer.coverCalls = append(
		writer.coverCalls,
		recordingAdminProductCoverCall{
			ID:              id,
			ExpectedVersion: expectedVersion,
			Input:           input,
			HasDeadline:     hasDeadline,
		},
	)

	return writer.coverResult, writer.coverErr
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

// setCoverOutcome configures the next deterministic cover mutation result.
func (writer *recordingAdminProductWriter) setCoverOutcome(
	result adminProductCoverWriteResult,
	err error,
) {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	writer.coverResult = result
	writer.coverErr = err
}

// coverCallSnapshot returns isolated cover calls and their mutable image bytes.
func (writer *recordingAdminProductWriter) coverCallSnapshot() []recordingAdminProductCoverCall {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	calls := make([]recordingAdminProductCoverCall, len(writer.coverCalls))
	copy(calls, writer.coverCalls)
	for index := range calls {
		calls[index].Input.Content = append(
			[]byte(nil),
			calls[index].Input.Content...,
		)
	}

	return calls
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
