-- Reverse only Stage 24. The hero child must disappear before its homepage
-- parent; strict drops expose dependency drift and leave all portfolio data intact.
DROP TABLE public.homepage_hero_images;
DROP TABLE public.contact_content;
DROP TABLE public.homepage_content;
