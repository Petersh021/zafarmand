-- Reverse only Stage 23. The dependent one-cover relation must be removed
-- before its owning Architecture project table; strict drops expose drift.
DROP TABLE public.architecture_project_cover_images;
DROP TABLE public.architecture_projects;
