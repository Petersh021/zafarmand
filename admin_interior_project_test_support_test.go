package main

import "context"

// recordingAdminInteriorProjectReader is an in-memory protected read test double.
// Its fields let HTTP and construction tests arrange each operation independently
// without a PostgreSQL connection.
type recordingAdminInteriorProjectReader struct {
	// listResult and listError control List.
	listResult []adminInteriorProjectRecord
	listError  error
	// listCalls and listContext record List invocations.
	listCalls   int
	listContext context.Context
	// findResult and findError control FindByID.
	findResult adminInteriorProjectRecord
	findError  error
	// findCalls, findContext, and findID record FindByID invocations.
	findCalls   int
	findContext context.Context
	findID      int64
	// coverResult and coverError control FindCoverByProjectID.
	coverResult interiorProjectCoverAsset
	coverError  error
	// coverCalls, coverContext, and coordinates record exact-cover invocations.
	coverCalls   int
	coverContext context.Context
	coverID      int64
	coverVersion int64
}

// newRecordingAdminInteriorProjectReader returns a dependency that lists an
// allocated empty collection and performs no automatic lookup behavior.
func newRecordingAdminInteriorProjectReader() *recordingAdminInteriorProjectReader {
	return &recordingAdminInteriorProjectReader{
		listResult: make([]adminInteriorProjectRecord, 0),
	}
}

// List records the caller and returns an isolated configured slice.
func (reader *recordingAdminInteriorProjectReader) List(
	ctx context.Context,
) ([]adminInteriorProjectRecord, error) {
	reader.listCalls++
	reader.listContext = ctx
	if reader.listError != nil {
		return nil, reader.listError
	}

	result := make([]adminInteriorProjectRecord, len(reader.listResult))
	copy(result, reader.listResult)

	return result, nil
}

// FindByID records the protected identity and returns the configured outcome.
func (reader *recordingAdminInteriorProjectReader) FindByID(
	ctx context.Context,
	projectID int64,
) (adminInteriorProjectRecord, error) {
	reader.findCalls++
	reader.findContext = ctx
	reader.findID = projectID

	return reader.findResult, reader.findError
}

// FindCoverByProjectID records exact media coordinates and returns an isolated
// copy of the configured asset bytes.
func (reader *recordingAdminInteriorProjectReader) FindCoverByProjectID(
	ctx context.Context,
	projectID int64,
	coverVersion int64,
) (interiorProjectCoverAsset, error) {
	reader.coverCalls++
	reader.coverContext = ctx
	reader.coverID = projectID
	reader.coverVersion = coverVersion

	return cloneInteriorProjectCoverAsset(reader.coverResult), reader.coverError
}

// recordingAdminInteriorProjectWriter is an in-memory mutation test double. It
// records all supplied values while each operation returns a separately
// configurable result or error.
type recordingAdminInteriorProjectWriter struct {
	// createResult and createError control Create.
	createResult adminInteriorProjectWriteResult
	createError  error
	// createCalls, context, and input record Create invocations.
	createCalls   int
	createContext context.Context
	createInput   adminInteriorProjectWriteInput
	// updateResult and updateError control Update.
	updateResult adminInteriorProjectWriteResult
	updateError  error
	// update call fields record the complete optimistic mutation coordinate.
	updateCalls           int
	updateContext         context.Context
	updateID              int64
	updateExpectedVersion int64
	updateInput           adminInteriorProjectWriteInput
	// coverResult and coverError control UpsertCover.
	coverResult adminInteriorProjectCoverWriteResult
	coverError  error
	// cover call fields record the complete optimistic media coordinate.
	coverCalls           int
	coverContext         context.Context
	coverID              int64
	coverExpectedVersion int64
	coverInput           adminInteriorProjectCoverWriteInput
}

// newRecordingAdminInteriorProjectWriter returns a nonnil dependency with no
// implicit successful mutation result; each test must arrange the operation it
// expects to reach.
func newRecordingAdminInteriorProjectWriter() *recordingAdminInteriorProjectWriter {
	return &recordingAdminInteriorProjectWriter{}
}

// Create records one protected project input and returns the configured result.
func (writer *recordingAdminInteriorProjectWriter) Create(
	ctx context.Context,
	input adminInteriorProjectWriteInput,
) (adminInteriorProjectWriteResult, error) {
	writer.createCalls++
	writer.createContext = ctx
	writer.createInput = input

	return writer.createResult, writer.createError
}

// Update records one version-guarded project edit and returns its configured
// result without applying any implicit mutation.
func (writer *recordingAdminInteriorProjectWriter) Update(
	ctx context.Context,
	projectID int64,
	expectedVersion int64,
	input adminInteriorProjectWriteInput,
) (adminInteriorProjectWriteResult, error) {
	writer.updateCalls++
	writer.updateContext = ctx
	writer.updateID = projectID
	writer.updateExpectedVersion = expectedVersion
	writer.updateInput = input

	return writer.updateResult, writer.updateError
}

// UpsertCover records one version-guarded media mutation and copies its mutable
// input bytes before retaining them for later assertions.
func (writer *recordingAdminInteriorProjectWriter) UpsertCover(
	ctx context.Context,
	projectID int64,
	expectedVersion int64,
	input adminInteriorProjectCoverWriteInput,
) (adminInteriorProjectCoverWriteResult, error) {
	writer.coverCalls++
	writer.coverContext = ctx
	writer.coverID = projectID
	writer.coverExpectedVersion = expectedVersion
	input.Content = append([]byte(nil), input.Content...)
	writer.coverInput = input

	return writer.coverResult, writer.coverError
}
