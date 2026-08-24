-- Reverse only the replay tombstones and partial retention index introduced by
-- version 000010. Strict drops expose schema drift and never remove inquiries.
DROP INDEX public.inquiries_archived_updated_at_id_idx;
DROP TABLE public.inquiry_submission_tombstones;
