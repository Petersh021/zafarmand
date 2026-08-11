-- Reverse only Stage 22. The dependent one-cover relation must be removed
-- before its owning Interior project table; strict drops expose schema drift.
DROP TABLE public.interior_project_cover_images;
DROP TABLE public.interior_projects;
