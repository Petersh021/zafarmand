package main

import (
	"context"
	"sort"
	"sync"
)

// recordingAdminInquiryReader is a concurrency-safe, database-free test double
// for protected inbox handlers and shared application construction helpers.
type recordingAdminInquiryReader struct {
	// mu keeps fixture data, failures, and call evidence race-free.
	mu sync.Mutex
	// inquiries is an independent set of complete records used by both methods.
	inquiries []adminInquiryDetail
	// listCalls records each exclusive upper-bound cursor in request order.
	listCalls []int64
	// findCalls records every exact detail identity in request order.
	findCalls []int64
	// listErr injects one data-free list failure when non-nil.
	listErr error
	// findErr injects one data-free detail failure when non-nil.
	findErr error
}

// newRecordingAdminInquiryReader returns an empty reader whose list behaves
// like a descending primary-key query and whose detail reads return not found.
func newRecordingAdminInquiryReader() *recordingAdminInquiryReader {
	return &recordingAdminInquiryReader{}
}

// List implements adminInquiryReader with the same exclusive keyset cursor and
// page-size contract as PostgreSQL.
func (reader *recordingAdminInquiryReader) List(
	_ context.Context,
	beforeID int64,
) (adminInquiryPage, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	reader.listCalls = append(reader.listCalls, beforeID)
	if reader.listErr != nil {
		return adminInquiryPage{}, reader.listErr
	}
	if beforeID < 0 {
		return adminInquiryPage{}, errAdminInquiryInvalidQuery
	}

	ordered := append([]adminInquiryDetail(nil), reader.inquiries...)
	sort.Slice(ordered, func(left int, right int) bool {
		return ordered[left].ID > ordered[right].ID
	})

	items := make([]adminInquirySummary, 0, adminInquiryPageSize)
	for _, inquiry := range ordered {
		if beforeID > 0 && inquiry.ID >= beforeID {
			continue
		}
		if len(items) == adminInquiryPageSize {
			return adminInquiryPage{
				Items:        items,
				HasMore:      true,
				NextBeforeID: items[len(items)-1].ID,
			}, nil
		}
		items = append(items, adminInquirySummary{
			ID:         inquiry.ID,
			Name:       inquiry.Name,
			Discipline: inquiry.Discipline,
			Status:     inquiry.Status,
			CreatedAt:  inquiry.CreatedAt,
		})
	}

	return adminInquiryPage{Items: items}, nil
}

// FindByID implements the exact protected detail lookup without exposing one
// inquiry through a different identifier.
func (reader *recordingAdminInquiryReader) FindByID(
	_ context.Context,
	inquiryID int64,
) (adminInquiryDetail, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	reader.findCalls = append(reader.findCalls, inquiryID)
	if reader.findErr != nil {
		return adminInquiryDetail{}, reader.findErr
	}
	if inquiryID <= 0 {
		return adminInquiryDetail{}, errAdminInquiryInvalidQuery
	}
	for _, inquiry := range reader.inquiries {
		if inquiry.ID == inquiryID {
			return inquiry, nil
		}
	}

	return adminInquiryDetail{}, errAdminInquiryNotFound
}

// setInquiries replaces repository state with an independent slice so a test
// cannot mutate a stored fixture after arranging the request.
func (reader *recordingAdminInquiryReader) setInquiries(
	inquiries ...adminInquiryDetail,
) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	reader.inquiries = append([]adminInquiryDetail(nil), inquiries...)
}

// callSnapshot returns independent list and detail call slices for assertions.
func (reader *recordingAdminInquiryReader) callSnapshot() ([]int64, []int64) {
	reader.mu.Lock()
	defer reader.mu.Unlock()

	return append([]int64(nil), reader.listCalls...),
		append([]int64(nil), reader.findCalls...)
}
