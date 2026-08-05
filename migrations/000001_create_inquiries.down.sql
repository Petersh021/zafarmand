-- Reverse only the exact table introduced by version 000001. Its strict form
-- makes unexpected schema drift fail visibly instead of removing dependencies.
DROP TABLE public.inquiries;
