-- Reverse only the Product table introduced by version 000004. A strict drop
-- makes unexpected dependencies or schema drift fail visibly.
DROP TABLE public.products;
