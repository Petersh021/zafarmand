package main

import "context"

// recordingAdminArchitectureProjectReader is an in-memory protected read test
// double. HTTP and construction tests can arrange each operation independently
// without opening PostgreSQL.
type recordingAdminArchitectureProjectReader struct {
	// listResult and listError control List.
	listResult []adminArchitectureProjectRecord
	listError  error
	// listCalls and listContext record List invocations.
	listCalls   int
	listContext context.Context
	// findResult and findError control FindByID.
	findResult adminArchitectureProjectRecord
	findError  error
	// findCalls, findContext, and findID record FindByID invocations.
	findCalls   int
	findContext context.Context
	findID      int64
	// coverResult and coverError control FindCoverByProjectID.
	coverResult architectureProjectCoverAsset
	coverError  error
	// coverCalls, coverContext, and coordinates record exact-cover invocations.
	coverCalls   int
	coverContext context.Context
	coverID      int64
	coverVersion int64
}

// newRecordingAdminArchitectureProjectReader returns a dependency whose list
// result is an allocated empty collection.
func newRecordingAdminArchitectureProjectReader() *recordingAdminArchitectureProjectReader {
	return &recordingAdminArchitectureProjectReader{
		listResult: make([]adminArchitectureProjectRecord, 0),
	}
}

// List records its caller and returns an isolated configured slice.
func (reader *recordingAdminArchitectureProjectReader) List(
	ctx context.Context,
) ([]adminArchitectureProjectRecord, error) {
	reader.listCalls++
	reader.listContext = ctx
	if reader.listError != nil {
		return nil, reader.listError
	}

	result := make([]adminArchitectureProjectRecord, len(reader.listResult))
	copy(result, reader.listResult)
	return result, nil
}

// FindByID records the protected identity and returns the configured outcome.
func (reader *recordingAdminArchitectureProjectReader) FindByID(
	ctx context.Context,
	projectID int64,
) (adminArchitectureProjectRecord, error) {
	reader.findCalls++
	reader.findContext = ctx
	reader.findID = projectID
	return reader.findResult, reader.findError
}

// FindCoverByProjectID records exact media coordinates and returns an isolated
// copy of the configured asset bytes.
func (reader *recordingAdminArchitectureProjectReader) FindCoverByProjectID(
	ctx context.Context,
	projectID int64,
	coverVersion int64,
) (architectureProjectCoverAsset, error) {
	reader.coverCalls++
	reader.coverContext = ctx
	reader.coverID = projectID
	reader.coverVersion = coverVersion
	return cloneArchitectureProjectCoverAsset(reader.coverResult), reader.coverError
}

// recordingAdminArchitectureProjectWriter is an in-memory mutation test
// double. Each operation records every input and returns a configurable result.
type recordingAdminArchitectureProjectWriter struct {
	// createResult and createError control Create.
	createResult adminArchitectureProjectWriteResult
	createError  error
	// createCalls, context, and input record Create invocations.
	createCalls   int
	createContext context.Context
	createInput   adminArchitectureProjectWriteInput
	// updateResult and updateError control Update.
	updateResult adminArchitectureProjectWriteResult
	updateError  error
	// update call fields record the complete optimistic coordinate.
	updateCalls           int
	updateContext         context.Context
	updateID              int64
	updateExpectedVersion int64
	updateInput           adminArchitectureProjectWriteInput
	// coverResult and coverError control UpsertCover.
	coverResult adminArchitectureProjectCoverWriteResult
	coverError  error
	// cover call fields record the complete optimistic media coordinate.
	coverCalls           int
	coverContext         context.Context
	coverID              int64
	coverExpectedVersion int64
	coverInput           adminArchitectureProjectCoverWriteInput
}

// newRecordingAdminArchitectureProjectWriter returns a nonnil dependency with
// no implicit successful mutation result.
func newRecordingAdminArchitectureProjectWriter() *recordingAdminArchitectureProjectWriter {
	return &recordingAdminArchitectureProjectWriter{}
}

// Create records one protected project input and returns the arranged result.
func (writer *recordingAdminArchitectureProjectWriter) Create(
	ctx context.Context,
	input adminArchitectureProjectWriteInput,
) (adminArchitectureProjectWriteResult, error) {
	writer.createCalls++
	writer.createContext = ctx
	writer.createInput = input
	return writer.createResult, writer.createError
}

// Update records a version-guarded edit and returns the arranged result.
func (writer *recordingAdminArchitectureProjectWriter) Update(
	ctx context.Context,
	projectID int64,
	expectedVersion int64,
	input adminArchitectureProjectWriteInput,
) (adminArchitectureProjectWriteResult, error) {
	writer.updateCalls++
	writer.updateContext = ctx
	writer.updateID = projectID
	writer.updateExpectedVersion = expectedVersion
	writer.updateInput = input
	return writer.updateResult, writer.updateError
}

// UpsertCover records one media mutation and copies its mutable bytes before
// retaining them for assertions.
func (writer *recordingAdminArchitectureProjectWriter) UpsertCover(
	ctx context.Context,
	projectID int64,
	expectedVersion int64,
	input adminArchitectureProjectCoverWriteInput,
) (adminArchitectureProjectCoverWriteResult, error) {
	writer.coverCalls++
	writer.coverContext = ctx
	writer.coverID = projectID
	writer.coverExpectedVersion = expectedVersion
	input.Content = append([]byte(nil), input.Content...)
	writer.coverInput = input
	return writer.coverResult, writer.coverError
}
